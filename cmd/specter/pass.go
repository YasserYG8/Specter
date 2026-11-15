package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/fatih/color"

	"github.com/YasserYG8/Specter/internal/pipeline"
	"github.com/YasserYG8/Specter/internal/resolver"
	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/ui"
)

// errBackToPicker is the cause of the stop that Esc triggers. Like errDurationElapsed it ends the pass through the
// same path as Ctrl+C, so every session detaches the usual way and prints "Detached ... cleanly."
var errBackToPicker = errors.New("back to the picker")

// keyHint is the one line that tells the user about the keys. It is printed once per pass, after the attach lines.
const keyHint = "Press Esc to pick another service, Ctrl+C to quit."

// clearScreen erases the terminal the way the `clear` command does: cursor to the top left, erase the screen, and
// erase the scrollback (ESC [3J). Without the last part the old logs are only pushed up, as with Ctrl+L, and the
// user still finds them above the picker when scrolling. A terminal cannot erase only a part of its scrollback, so
// what was above Specter's own output goes too.
const clearScreen = "\x1b[H\x1b[2J\x1b[3J"

// keySource is what a pass needs from the key reader. *ui.KeyReader is one; tests use a fake that needs no
// terminal.
type keySource interface {
	// Start takes over the terminal and returns the keys. The channel closes when the reader stops.
	Start(ctx context.Context) (<-chan ui.Key, error)
	// Stop gives the terminal back. When it returns, nothing reads the terminal any more.
	Stop()
}

// passEnv is what one pass needs from the command line and the terminal. A test builds its own.
type passEnv struct {
	cfg          runConfig
	filterOpts   pipeline.FilterOptions
	rendererOpts pipeline.RendererOptions
	useColor     bool // colors are on, so the tags are colored
	out, errOut  io.Writer
	stdoutIsTTY  bool // Enter adds an empty line only when stdout is a terminal

	// newKeys returns the key reader for this pass, or nil when Specter may not take over the terminal (stdin is
	// not a terminal, or Specter is a background job). A nil newKeys means the same.
	newKeys func() keySource

	// clearBeforePicker erases the terminal (screen and scrollback) before the picker opens after Esc, so the logs
	// of the stream the user just left are gone and the picker starts at the top. It is set when stderr, where the picker draws, is a terminal.
	clearBeforePicker bool

	// configure lets a test replace the parts of the run environment that touch the real system.
	configure func(*runEnv)
}

// passResult is how one pass ended.
type passResult struct {
	anyEnded bool // a target ended by itself: the exit status of Specter is 1
	back     bool // the user pressed Esc: open the picker and start a new pass
	err      error
	// silenceErr is set when the error has already been printed in full, so cobra must not print it again.
	silenceErr bool
}

// resolveSpecs resolves every target before anything is attached. The first failure stops the run. The error of an
// empty port is returned as it is, because its diagnostic is printed by the caller; the others name their target
// when several targets run.
func resolveSpecs(ctx context.Context, res fullResolver, specs []targetSpec) ([]resolvedTarget, error) {
	var resolved []resolvedTarget
	for _, spec := range specs {
		t, err := resolveSpec(ctx, res, spec)
		if err != nil {
			var noProcErr *resolver.NoProcessOnPortError
			if errors.As(err, &noProcErr) || len(specs) == 1 {
				return nil, err
			}
			return nil, fmt.Errorf("%s: %w", spec.Label, err)
		}
		if t == nil {
			return nil, errors.New("failed to resolve target")
		}
		resolved = append(resolved, resolvedTarget{Label: spec.Label, Target: t})
	}
	return resolved, nil
}

// runLoop runs passes until one of them ends the run. The first pass is the one from the command line (or from the
// picker, when no target was given). While the user ends a pass with Esc, the next pass comes from the picker. When
// the picker is closed without a choice, the run ends with the result of the last pass, so its exit status counts.
func runLoop(ctx context.Context, first func(context.Context) passResult, pick func(context.Context) (passResult, bool)) passResult {
	res := first(ctx)
	for res.back && ctx.Err() == nil {
		next, chosen := pick(ctx)
		if !chosen {
			return res
		}
		res = next
	}
	return res
}

// runPass is one stream: children, duplicates, start every session, stream, end reports. It runs on the main
// goroutine, which holds the locked OS thread (see Session.Start), and it gives the terminal back before it
// returns, so the picker can use it.
func runPass(ctx context.Context, resolved []resolvedTarget, pe *passEnv) passResult {
	// Discover pre-existing child processes (only for host PIDs)
	inspector := resolver.NewProcessInspector("")
	for _, r := range resolved {
		if r.Target.PID > 0 {
			if pe.cfg.Tree {
				r.Target.Children = inspector.FindChildProcessTree(r.Target.PID, 3)
			} else if kids, err := inspector.FindChildPIDs(r.Target.PID); err == nil {
				r.Target.Children = kids
			}
		}
	}

	// The same process (or container) given twice is followed once
	resolved, dupNotices := dedupeTargets(resolved)
	for _, n := range dupNotices {
		printNotice(pe.errOut, n)
	}
	multi := len(resolved) > 1

	env := newRunEnv(pe.cfg, pe.errOut, pe.filterOpts, multi)
	if pe.configure != nil {
		pe.configure(env)
	}

	// Each pass has its own context, so Esc and --duration end this pass and not the whole run. Ctrl+C cancels the
	// parent, which ends every pass.
	passCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	sessions := make([]*Session, len(resolved))
	for i, r := range resolved {
		sessions[i] = newSession(env, i, r.Label, r.Target)
	}
	defer func() {
		for i := len(sessions) - 1; i >= 0; i-- {
			sessions[i].Stop()
		}
	}()

	// Start every session, one after the other. If one fails, the others are stopped and the pass ends before any
	// log line is printed.
	if err := startSessions(passCtx, sessions); err != nil {
		return passResult{err: err}
	}

	if err := sessions[0].filter.GrepError(); err != nil {
		printNotice(pe.errOut, fmt.Sprintf("--grep %q is not a valid regular expression (%v); matching it as literal text.", pe.cfg.Grep, err))
	}

	// --duration: the time starts now, when the sources have started, and counts for this pass only
	if pe.cfg.Duration > 0 {
		limit := time.AfterFunc(pe.cfg.Duration, func() { cancel(errDurationElapsed) })
		defer limit.Stop()
	}

	// With several targets every line starts with its tag
	ow := &outputWriter{w: pe.out}
	if multi {
		labels := make([]string, len(resolved))
		for i, r := range resolved {
			labels[i] = r.Label
		}
		ow.tag = newTagger(labels, pe.useColor)
	}

	res := streamPass(passCtx, cancel, sessions, env, pipeline.NewRenderer(pe.rendererOpts), ow, pe)
	// Esc cancels the pass with errBackToPicker. If the user also pressed Ctrl+C (the parent is canceled), quit.
	res.back = errors.Is(context.Cause(passCtx), errBackToPicker) && ctx.Err() == nil
	return res
}

// streamPass reads the keys, if Specter may, and runs the output loop until every session has ended. Esc cancels
// the pass with errBackToPicker; the sessions then detach as for Ctrl+C.
func streamPass(ctx context.Context, cancel context.CancelCauseFunc, sessions []*Session, env *runEnv, renderer *pipeline.Renderer, ow *outputWriter, pe *passEnv) passResult {
	var blanks chan struct{}
	if pe.newKeys != nil {
		if keys := pe.newKeys(); keys != nil {
			if keyCh, err := keys.Start(ctx); err == nil {
				// Whatever way this function ends, the terminal is given back, and nothing reads it afterwards
				var wg sync.WaitGroup
				defer wg.Wait()
				defer keys.Stop()

				blanks = make(chan struct{}, 4)
				wg.Add(1)
				go func() {
					defer wg.Done()
					handleKeys(keyCh, cancel, blanks, pe.stdoutIsTTY)
				}()
				fmt.Fprintf(pe.errOut, "%s %s\n", color.CyanString("[specter]"), keyHint)
			}
		}
	}

	anyEnded, err := runSessions(ctx, sessions, env, renderer, ow, blanks)
	return passResult{anyEnded: anyEnded, err: err}
}

// handleKeys reacts to the keys of one pass. Esc ends the pass. Enter asks the output loop for an empty line, but
// only when stdout is a terminal, so a pipe gets log lines only. Every other key is ignored.
func handleKeys(keys <-chan ui.Key, cancel context.CancelCauseFunc, blanks chan<- struct{}, stdoutIsTTY bool) {
	for k := range keys {
		switch k {
		case ui.KeyEsc:
			cancel(errBackToPicker)
		case ui.KeyEnter:
			if stdoutIsTTY {
				select {
				case blanks <- struct{}{}:
				default: // a burst of Enters needs no more empty lines than the screen can show
				}
			}
		}
	}
}

// pickPass runs the picker and, when the user chooses a service, a pass for it. chosen is false when the picker was
// closed without a choice.
func pickPass(ctx context.Context, pick func(context.Context) (*source.Target, error), pe *passEnv) (res passResult, chosen bool) {
	// Every session has stopped and printed its "detached" line by now, so nothing is written after this
	if pe.clearBeforePicker {
		io.WriteString(pe.errOut, clearScreen)
	}
	t, err := pick(ctx)
	if err != nil {
		return passResult{err: err}, true
	}
	if t == nil {
		return passResult{}, false
	}
	return runPass(ctx, []resolvedTarget{{Label: "target", Target: t}}, pe), true
}
