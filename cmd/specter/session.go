package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fatih/color"

	"github.com/YasserYG8/Specter/internal/pipeline"
	"github.com/YasserYG8/Specter/internal/procwatch"
	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/source/selector"
	"github.com/YasserYG8/Specter/internal/source/strace"
)

// runConfig is the part of the command line that every session needs.
type runConfig struct {
	Source        string        // --source: force one capture engine
	Tree          bool          // --tree
	Grep          string        // --grep
	Duration      time.Duration // --duration, for the closing message
	NoOutputDelay time.Duration // how long a session may stay silent before the "nothing captured" notice
}

// runEnv is what all sessions of one run share. The function fields are the places where a test replaces the
// real system (the kernel, /proc, the engine selector) with a fake.
type runEnv struct {
	cfg        runConfig
	errOut     io.Writer              // stderr: status messages go here
	filterOpts pipeline.FilterOptions // every session builds its own Filter from these, see Session.filter
	multi      bool                   // two or more targets: status messages name their target

	ptraceOnce sync.Once // the ptrace warning is about the machine, so it is printed once per run

	selectSource func(t *source.Target) (src source.Source, reason, ebpfNote string, err error)
	newStrace    func() source.Source
	checkPtrace  func() error
	watch        func(ctx context.Context, pid int) <-chan struct{}
}

func newRunEnv(cfg runConfig, errOut io.Writer, filterOpts pipeline.FilterOptions, multi bool) *runEnv {
	e := &runEnv{cfg: cfg, errOut: errOut, filterOpts: filterOpts, multi: multi}
	e.selectSource = func(t *source.Target) (source.Source, string, string, error) {
		sel := selector.NewSelector(cfg.Source, cfg.Tree)
		src, reason, err := sel.Select(t)
		return src, reason, sel.EBPFNote, err
	}
	e.newStrace = func() source.Source { return strace.NewSource(strace.Options{FollowTree: cfg.Tree}) }
	e.checkPtrace = func() error {
		// Root can attach at ptrace_scope 1 and 2, so it gets no warning there
		return strace.CheckPtracePermissionsAs("", os.Geteuid())
	}
	e.watch = func(ctx context.Context, pid int) <-chan struct{} {
		return procwatch.Watch(ctx, pid, procwatch.Options{})
	}
	return e
}

// Session is the capture of one target, from its source to the entries the stack-trace grouper (and, with
// --grep, the trace holder) produces. A run has one Session per target. Their entries meet after the grouper, in
// one filter and one renderer, so a stack trace of one target is never mixed with the lines of another and the
// (pid, fd) keys of the pipeline stay separate per target.
type Session struct {
	Index  int
	Label  string
	Target *source.Target

	// Entries is the session's output. It is closed when the session's pipeline has drained.
	Entries <-chan pipeline.Entry

	env *runEnv
	// filter is the session's own. A Filter remembers, per (pid, fd), whether the last record matched --grep, so that
	// its continuation lines are admitted. Two targets can use the same (pid, fd) (containers have PID 0), so they
	// must not share that memory.
	filter     *pipeline.Filter
	tag        string    // the start of the session's attach and end lines: "[specter]", or its label with several targets
	noticeOut  io.Writer // where notices go: stderr, and with several targets also the label in front
	src        source.Source
	errs       <-chan error
	cancelSrc  context.CancelCauseFunc
	cancelPipe context.CancelFunc

	// The process watcher sets watcherGone when the target is gone.
	watcherGone atomic.Bool
	sawEntry    func()
	stopNotice  func()
	stopOnce    sync.Once
}

func newSession(env *runEnv, index int, label string, target *source.Target) *Session {
	s := &Session{Index: index, Label: label, Target: target, env: env, filter: pipeline.NewFilter(env.filterOpts)}
	if env.multi {
		s.tag = colorLabel(index, label, false)
		s.noticeOut = &prefixWriter{w: env.errOut, prefix: s.tag + " "}
	} else {
		s.tag = color.CyanString("[specter]")
		s.noticeOut = env.errOut
	}
	return s
}

// Start selects and starts the session's source and builds its pipeline. The caller must call it on the main
// goroutine, which locks its OS thread (see RunE).
//
// That matters because strace gets a parent-death signal (Pdeathsig, see spawn_linux.go) that the kernel ties to
// the thread that started it. If another goroutine started strace and later its thread ended, the signal would
// make strace detach in the middle of the run. So sessions are started one after the other, here, and only the
// reading (the source's own goroutines and the pipeline stages) runs elsewhere.
func (s *Session) Start(ctx context.Context) error {
	cfg := s.env.cfg
	target := s.Target

	// Notice if stdout and stderr are discarded to /dev/null
	if strings.Contains(target.Stdout, "/dev/null") && strings.Contains(target.Stderr, "/dev/null") {
		printNotice(s.noticeOut, "Target process has stdout/stderr redirected to /dev/null; no console logs are emitted.")
	}

	// Select capture source
	src, reason, ebpfNote, selectErr := s.env.selectSource(target)
	if selectErr != nil {
		return selectErr
	}
	s.src = src // Stop uses it, and the fallback below may replace it

	// The source and the pipeline get their own contexts. The process watcher stops the source when the
	// target is gone and gives the pipeline a moment to print what was already read, while the user's
	// interrupt (ctx) stops both at once.
	srcCtx, cancelSrc := context.WithCancelCause(ctx)
	s.cancelSrc = cancelSrc
	pipeCtx, cancelPipe := context.WithCancel(ctx)
	s.cancelPipe = cancelPipe

	// eBPF passes the selector's checks without having been loaded: the kernel decides when the program is
	// loaded. So it is started now, before Specter says which source it uses, and a refusal ends in the one
	// line of the slowdown notice instead of a "Source: ebpf" line followed by a second source.
	var chunks <-chan source.Chunk
	started := false
	if src.Name() == "ebpf" {
		wireNotices(src, s.noticeOut)
		c, e, startErr := startNow(srcCtx, src, *target)
		if startErr != nil {
			_ = src.Stop()
			if cfg.Source == "ebpf" {
				return selector.ForcedEBPFError(startErr) // the user asked for eBPF: no fallback
			}
			ebpfNote = startErr.Error()
			src = s.env.newStrace()
			s.src = src
			reason = "standard process tracing (strace)"
		} else {
			chunks, s.errs, started = c, e, true
		}
	}

	// Check ptrace permissions upfront if strace source is selected
	if src.Name() == "strace" {
		s.env.ptraceOnce.Do(func() {
			if permErr := s.env.checkPtrace(); permErr != nil {
				fmt.Fprintf(s.env.errOut, "%s %v\n", color.YellowString("[specter:warning]"), permErr)
			}
		})
	}

	// Header notice
	cmdSummary := strings.Join(target.Cmdline, " ")
	if len(cmdSummary) > 60 {
		cmdSummary = cmdSummary[:57] + "..."
	}
	out := s.env.errOut
	if target.Container != "" {
		if target.Port > 0 {
			fmt.Fprintf(out, "%s Attaching to container %s on port :%d (%s)\n", s.tag, target.Container, target.Port, cmdSummary)
		} else {
			fmt.Fprintf(out, "%s Attaching to container %s (%s)\n", s.tag, target.Container, cmdSummary)
		}
	} else if target.Port > 0 {
		fmt.Fprintf(out, "%s Attaching to PID %d on port :%d (%s)\n", s.tag, target.PID, target.Port, cmdSummary)
	} else {
		fmt.Fprintf(out, "%s Attaching to PID %d (%s)\n", s.tag, target.PID, cmdSummary)
	}
	if len(target.Children) > 0 {
		childStrList := make([]string, len(target.Children))
		for i, c := range target.Children {
			childStrList[i] = strconv.Itoa(c)
		}
		fmt.Fprintf(out, "%s Discovered active child worker(s): PID %s\n", s.tag, strings.Join(childStrList, ", "))
	}
	fmt.Fprintf(out, "%s Source: %s (%s)\n", s.tag, color.GreenString(src.Name()), reason)
	if msg := slowdownNoticeFor(src.Name(), ebpfNote); msg != "" {
		printNotice(s.noticeOut, msg)
	}

	if !started {
		wireNotices(src, s.noticeOut)
		chunks, s.errs = src.Start(srcCtx, *target)

		// Fallback: If an auto-detected non-strace source fails immediately to start, fall back to strace
		if cfg.Source == "" && src.Name() != "strace" {
			select {
			case err, ok := <-s.errs:
				if ok && err != nil {
					fmt.Fprintf(out, "%s Auto-detected source '%s' unavailable (%v); falling back to strace\n",
						s.tag, src.Name(), err)
					src = s.env.newStrace()
					s.src = src
					wireNotices(src, s.noticeOut)
					printNotice(s.noticeOut, slowdownNotice) // the first source did not slow the target, this one does
					chunks, s.errs = src.Start(srcCtx, *target)
				}
			default:
				// Started normally
			}
		}
	}

	// Watch the target process, for sources that cannot tell when it ends. strace usually does, with the
	// exit code, but the file source only tails a file and would wait forever.
	if watchesPID(src, target) {
		gone := s.env.watch(pipeCtx, target.PID)
		go func() {
			select {
			case <-gone:
				s.watcherGone.Store(true)
				cancelSrc(errTargetGone)
				select {
				case <-time.After(drainGrace):
					cancelPipe()
				case <-pipeCtx.Done():
				}
			case <-pipeCtx.Done():
			}
		}()
	}

	// 1. Line assembler
	assembler := pipeline.NewLineAssembler(pipeline.DefaultIdleTimeout)
	lines := assembler.Assemble(pipeCtx, chunks)

	// 2. Multi-line stack trace grouper
	grouper := pipeline.NewStackTraceGrouper(pipeline.DefaultTraceIdleTimeout)
	entries := grouper.Group(pipeCtx, lines)

	// With --grep, hold each stack trace until it ends, so a match on any of its lines shows the whole
	// trace. Without --grep nothing is held and output stays immediate.
	if cfg.Grep != "" {
		entries = pipeline.NewTraceHolder(s.filter.MatchText, pipeline.DefaultTraceIdleTimeout, pipeline.MaxHeldTraceLines).Hold(pipeCtx, entries)
	}
	s.Entries = entries

	s.sawEntry, s.stopNotice = startNoOutputNotice(pipeCtx, cfg.NoOutputDelay, s.noticeOut)
	return nil
}

// Stop ends the session: it stops the notice timer, cancels the pipeline and the source, and detaches the
// source. It is safe to call when Start failed half-way, and more than once.
func (s *Session) Stop() {
	s.stopOnce.Do(func() {
		if s.stopNotice != nil {
			s.stopNotice()
		}
		if s.cancelPipe != nil {
			s.cancelPipe()
		}
		if s.cancelSrc != nil {
			s.cancelSrc(nil)
		}
		if s.src != nil {
			_ = s.src.Stop()
		}
	})
}

// sessionEnd is how one session ended.
type sessionEnd struct {
	Report endReport
	// Err is a source error that ended the session while the target was still there (not an interrupt, and not
	// the target going away).
	Err error
}

// finish says how the session ended. It runs after the session's entries are drained.
func (s *Session) finish(ctx context.Context) sessionEnd {
	// Check for source startup or runtime errors
	srcErr, ok := <-s.errs
	if ok && srcErr != nil && ctx.Err() == nil && !s.watcherGone.Load() {
		return sessionEnd{Err: srcErr}
	}

	// "Detached cleanly" is only for the user's interrupt; when the target ended on its own, Specter says how
	// and exits with status 1.
	report := endReport{
		Interrupted: ctx.Err() != nil,
		PID:         s.Target.PID,
		Container:   s.Target.Container,
		WatcherGone: s.watcherGone.Load(),
	}
	if errors.Is(context.Cause(ctx), errDurationElapsed) {
		report.After = s.env.cfg.Duration
	}
	if es, ok := s.src.(interface {
		ExitStatus() (source.ExitStatus, bool)
	}); ok {
		if status, known := es.ExitStatus(); known {
			report.Exit = &status
		}
	}
	if s.src.Name() == "docker" && !report.Interrupted {
		// docker logs -f ends when the container stops; ask once for the exit code
		report.DockerEnded = true
		if ce, ok := s.src.(interface {
			ContainerExit(context.Context) (int, bool)
		}); ok {
			if code, known := ce.ContainerExit(ctx); known {
				report.ContainerCode = &code
			}
		}
	}
	return sessionEnd{Report: report}
}
