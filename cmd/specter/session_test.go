package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fatih/color"

	"github.com/YasserYG8/Specter/internal/pipeline"
	"github.com/YasserYG8/Specter/internal/source"
)

// eventLog records what the fake sources were asked to do, in order.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(e string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

// scriptedSource is a source that emits what the test gives it. It ends when the test calls end, when its context
// ends, or never.
type scriptedSource struct {
	id     string // names the source in the event log
	name   string // what Name() returns, which decides some behavior (for example "strace" prints the ptrace warning)
	events *eventLog

	chunks    chan source.Chunk
	errs      chan error
	closeOnce sync.Once
	startErr  error // sent on the error channel as soon as Start is called
	exit      *source.ExitStatus

	mu       sync.Mutex
	running  bool
	notifier func(string)
}

func newScripted(id string, log *eventLog) *scriptedSource {
	return &scriptedSource{id: id, name: "fake", events: log, chunks: make(chan source.Chunk, 64), errs: make(chan error, 1)}
}

func (f *scriptedSource) Name() string { return f.name }

func (f *scriptedSource) Start(ctx context.Context, _ source.Target) (<-chan source.Chunk, <-chan error) {
	f.events.add("start:" + f.id)
	f.mu.Lock()
	f.running = true
	f.mu.Unlock()
	if f.startErr != nil {
		f.errs <- f.startErr
		f.end()
		return f.chunks, f.errs
	}
	go func() {
		<-ctx.Done()
		f.end()
	}()
	return f.chunks, f.errs
}

func (f *scriptedSource) Stop() error {
	f.events.add("stop:" + f.id)
	f.mu.Lock()
	f.running = false
	f.mu.Unlock()
	return nil
}

func (f *scriptedSource) isRunning() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

func (f *scriptedSource) SetNotifier(fn func(string)) {
	f.mu.Lock()
	f.notifier = fn
	f.mu.Unlock()
}

func (f *scriptedSource) notify(msg string) {
	f.mu.Lock()
	fn := f.notifier
	f.mu.Unlock()
	fn(msg)
}

func (f *scriptedSource) ExitStatus() (source.ExitStatus, bool) {
	if f.exit == nil {
		return source.ExitStatus{}, false
	}
	return *f.exit, true
}

// end closes the source's channels, as a source does when its target is gone.
func (f *scriptedSource) end() {
	f.closeOnce.Do(func() {
		close(f.chunks)
		close(f.errs)
	})
}

// send gives the source some output on stdout.
func (f *scriptedSource) send(text string) {
	f.chunks <- source.Chunk{Bytes: []byte(text), FD: 1, TS: time.Now()}
}

// harness runs sessions over fake sources and collects what Specter prints.
type harness struct {
	t      *testing.T
	log    *eventLog
	out    *syncBuffer // stdout
	errOut *syncBuffer // stderr
	env    *runEnv
	fakes  map[int]*scriptedSource // by the port of the target
}

func newHarness(t *testing.T, multi bool, cfg runConfig) *harness {
	t.Helper()
	old := color.NoColor
	color.NoColor = true // the tests compare plain text
	t.Cleanup(func() { color.NoColor = old })

	if cfg.NoOutputDelay == 0 {
		cfg.NoOutputDelay = time.Hour
	}
	h := &harness{t: t, log: &eventLog{}, out: &syncBuffer{}, errOut: &syncBuffer{}, fakes: map[int]*scriptedSource{}}
	h.env = newRunEnv(cfg, h.errOut, pipeline.FilterOptions{}, multi)
	h.env.selectSource = func(target *source.Target) (source.Source, string, string, error) {
		f, ok := h.fakes[target.Port]
		if !ok {
			return nil, "", "", fmt.Errorf("no fake source for port %d", target.Port)
		}
		return f, "test reason", "", nil
	}
	h.env.watch = func(context.Context, int) <-chan struct{} { return nil } // the fake target never goes away
	h.env.checkPtrace = func() error { return nil }
	return h
}

// session adds a target on the given port with a fake source, and returns the session.
func (h *harness) session(index int, label string, port int) (*Session, *scriptedSource) {
	f := newScripted(label, h.log)
	h.fakes[port] = f
	target := &source.Target{Port: port, Cmdline: []string{"app"}}
	return newSession(h.env, index, label, target), f
}

type runResult struct {
	anyEnded bool
	err      error
}

// start starts the sessions and runs the output loop in the background. The returned channel gets the result.
func (h *harness) start(ctx context.Context, sessions []*Session, raw bool) <-chan runResult {
	h.t.Helper()
	if err := startSessions(ctx, sessions); err != nil {
		h.t.Fatalf("start failed: %v", err)
	}
	ow := &outputWriter{w: h.out}
	if len(sessions) > 1 {
		labels := make([]string, len(sessions))
		for i, s := range sessions {
			labels[i] = s.Label
		}
		ow.tag = newTagger(labels, false)
	}
	renderer := pipeline.NewRenderer(pipeline.RendererOptions{NoColor: true, Raw: raw})
	done := make(chan runResult, 1)
	go func() {
		ended, err := runSessions(ctx, sessions, h.env, renderer, ow)
		done <- runResult{ended, err}
	}()
	h.t.Cleanup(func() {
		for _, s := range sessions {
			s.Stop()
		}
	})
	return done
}

func (h *harness) result(done <-chan runResult) runResult {
	h.t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(5 * time.Second):
		h.t.Fatalf("the run did not end\nstdout: %q\nstderr: %q", h.out.String(), h.errOut.String())
		return runResult{}
	}
}

// waitFor waits until buf contains text.
func waitFor(t *testing.T, buf *syncBuffer, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), text) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %q", text, buf.String())
}

// The output of a run with one target must stay what it was before several targets were possible: no tag, the same
// status lines, a partial line without a line break, and the same end report.
func TestSingleTarget_OutputIsUnchanged(t *testing.T) {
	h := newHarness(t, false, runConfig{})
	target := &source.Target{Port: 3000, PID: 4242, Cmdline: []string{"app"}}
	f := newScripted("A", h.log)
	h.fakes[3000] = f
	s := newSession(h.env, 0, ":3000", target)

	done := h.start(context.Background(), []*Session{s}, false)
	f.send("hello\n")
	f.send("Continue? ")
	waitFor(t, h.out, "Continue? ")
	f.end()

	res := h.result(done)
	if !res.anyEnded || res.err != nil {
		t.Fatalf("result = %+v, want the source stopping by itself", res)
	}
	if got, want := h.out.String(), "hello\nContinue? "; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	wantErr := "[specter] Attaching to PID 4242 on port :3000 (app)\n" +
		"[specter] Source: fake (test reason)\n" +
		"\n[specter] Capture of PID 4242 ended because its log source stopped.\n"
	if got := h.errOut.String(); got != wantErr {
		t.Errorf("stderr = %q, want %q", got, wantErr)
	}
}

func TestSingleTarget_SourceErrorIsReturned(t *testing.T) {
	h := newHarness(t, false, runConfig{})
	s, f := h.session(0, ":3000", 3000)
	done := h.start(context.Background(), []*Session{s}, false)
	f.errs <- errors.New("boom")
	f.end()

	res := h.result(done)
	if res.err == nil || res.err.Error() != "boom" {
		t.Errorf("err = %v, want the source error", res.err)
	}
	if strings.Contains(h.errOut.String(), "Capture of") {
		t.Errorf("no end report should be printed when the error is returned, got %q", h.errOut.String())
	}
}

func TestMultiTarget_LinesAreTaggedAndPadded(t *testing.T) {
	h := newHarness(t, true, runConfig{})
	a, fa := h.session(0, ":3000", 3000)
	b, fb := h.session(1, "web", 4000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.start(ctx, []*Session{a, b}, false)

	fa.send("GET /api 200\n")
	waitFor(t, h.out, ":3000 | GET /api 200\n")
	fb.send("started\n")
	waitFor(t, h.out, "web   | started\n")

	cancel()
	res := h.result(done)
	if res.anyEnded || res.err != nil {
		t.Errorf("result = %+v, want a clean stop", res)
	}
	if got, want := h.out.String(), ":3000 | GET /api 200\nweb   | started\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestMultiTarget_StatusLinesNameTheirTarget(t *testing.T) {
	h := newHarness(t, true, runConfig{NoOutputDelay: 200 * time.Millisecond})
	a, fa := h.session(0, ":3000", 3000)
	b, _ := h.session(1, ":4000", 4000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.start(ctx, []*Session{a, b}, false)

	fa.send("alive\n")
	fa.notify("a source notice")
	waitFor(t, h.errOut, ":4000 [specter:notice] Nothing captured yet")
	time.Sleep(50 * time.Millisecond)
	cancel()
	h.result(done)

	errOut := h.errOut.String()
	for _, want := range []string{
		":3000 Attaching to PID 0 on port :3000 (app)\n",
		":4000 Attaching to PID 0 on port :4000 (app)\n",
		":3000 Source: fake (test reason)\n",
		":4000 Source: fake (test reason)\n",
		":3000 [specter:notice] a source notice\n",
		":3000 Detached from PID 0 cleanly.\n",
		":4000 Detached from PID 0 cleanly.\n",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr is missing %q\n%s", want, errOut)
		}
	}
	// The silent target gets its notice, and only that target: the one that logged does not
	if n := strings.Count(errOut, "Nothing captured yet"); n != 1 {
		t.Errorf("expected exactly one 'Nothing captured yet' notice, got %d\n%s", n, errOut)
	}
	if strings.Contains(errOut, ":3000 [specter:notice] Nothing captured yet") {
		t.Errorf("the target that logged must not get the notice\n%s", errOut)
	}
}

func TestMultiTarget_PtraceWarningIsPrintedOnce(t *testing.T) {
	h := newHarness(t, true, runConfig{})
	h.env.checkPtrace = func() error { return errors.New("ptrace_scope is 1") }
	a, fa := h.session(0, ":3000", 3000)
	b, fb := h.session(1, ":4000", 4000)
	fa.name, fb.name = "strace", "strace"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.start(ctx, []*Session{a, b}, false)
	cancel()
	h.result(done)

	if n := strings.Count(h.errOut.String(), "ptrace_scope is 1"); n != 1 {
		t.Errorf("the ptrace warning should be printed once, got %d times\n%s", n, h.errOut.String())
	}
	// The slowdown notice is about each target, so each one gets it
	if n := strings.Count(h.errOut.String(), "strace slows the target"); n != 2 {
		t.Errorf("the slowdown notice should be printed for each target, got %d\n%s", n, h.errOut.String())
	}
}

func TestMultiTarget_PartialLineIsClosedBeforeAnotherTargetWrites(t *testing.T) {
	h := newHarness(t, true, runConfig{})
	a, fa := h.session(0, ":3000", 3000)
	b, fb := h.session(1, ":4000", 4000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.start(ctx, []*Session{a, b}, false)

	fa.send("Continue? ") // no line break: shown after the idle wait
	waitFor(t, h.out, ":3000 | Continue? ")
	fb.send("ready\n")
	waitFor(t, h.out, ":4000 | ready\n")
	cancel()
	h.result(done)

	if got, want := h.out.String(), ":3000 | Continue? \n:4000 | ready\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestMultiTarget_StackTraceStaysWhole(t *testing.T) {
	h := newHarness(t, true, runConfig{})
	a, fa := h.session(0, ":3000", 3000)
	b, fb := h.session(1, ":4000", 4000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.start(ctx, []*Session{a, b}, false)

	// :4000 writes a traceback while :3000 keeps writing request lines
	fb.send("Traceback (most recent call last):\n")
	waitFor(t, h.out, ":4000 | Traceback")
	for i := 0; i < 5; i++ {
		fa.send(fmt.Sprintf("GET /%d\n", i))
	}
	time.Sleep(40 * time.Millisecond) // the lines of :3000 have reached the output loop and must wait
	fb.send("  File \"app.py\", line 3, in <module>\n    boom()\nValueError: boom\n")
	waitFor(t, h.out, ":3000 | GET /4")
	cancel()
	h.result(done)

	out := h.out.String()
	trace := ":4000 | Traceback (most recent call last):\n" +
		":4000 |   File \"app.py\", line 3, in <module>\n" +
		":4000 |     boom()\n" +
		":4000 | ValueError: boom\n"
	if !strings.Contains(out, trace) {
		t.Errorf("the trace is not in one piece:\n%s", out)
	}
	for i := 0; i < 5; i++ {
		if want := fmt.Sprintf(":3000 | GET /%d\n", i); !strings.Contains(out, want) {
			t.Errorf("the line %q was lost:\n%s", want, out)
		}
	}
}

func TestMultiTarget_RawOutputIsTagged(t *testing.T) {
	h := newHarness(t, true, runConfig{})
	a, fa := h.session(0, ":3000", 3000)
	b, _ := h.session(1, ":4000", 4000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.start(ctx, []*Session{a, b}, true)
	fa.send(`{"msg":"x"}` + "\n")
	waitFor(t, h.out, ":3000 | {\"msg\":\"x\"}\n")
	cancel()
	h.result(done)
}

// One target ends by itself, the other keeps running; then the user stops Specter. The exit status says that a
// target ended by itself.
func TestMultiTarget_OneEndsTheOtherContinues(t *testing.T) {
	h := newHarness(t, true, runConfig{})
	a, fa := h.session(0, ":3000", 3000)
	b, fb := h.session(1, ":4000", 4000)
	fb.exit = &source.ExitStatus{PID: 77, Code: 1}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.start(ctx, []*Session{a, b}, false)

	fb.end() // :4000 exits
	waitFor(t, h.errOut, ":4000 Target PID 0 exited with code 1.")
	fa.send("still here\n")
	waitFor(t, h.out, ":3000 | still here\n")
	cancel() // Ctrl+C
	res := h.result(done)

	if !res.anyEnded {
		t.Error("a target ended by itself, so the exit status must be 1")
	}
	if !strings.Contains(h.errOut.String(), ":3000 Detached from PID 0 cleanly.") {
		t.Errorf("the target that was still running should be detached cleanly\n%s", h.errOut.String())
	}
	if strings.Contains(h.errOut.String(), ":4000 Detached") {
		t.Errorf("the target that ended must not also be reported as detached\n%s", h.errOut.String())
	}
}

func TestMultiTarget_ExitStatus(t *testing.T) {
	t.Run("interrupt with all targets running is 0", func(t *testing.T) {
		h := newHarness(t, true, runConfig{})
		a, _ := h.session(0, ":3000", 3000)
		b, _ := h.session(1, ":4000", 4000)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := h.start(ctx, []*Session{a, b}, false)
		cancel()
		res := h.result(done)
		if res.anyEnded || res.err != nil {
			t.Errorf("result = %+v, want a clean stop", res)
		}
		for _, l := range []string{":3000", ":4000"} {
			if !strings.Contains(h.errOut.String(), l+" Detached from PID 0 cleanly.") {
				t.Errorf("no detached line for %s\n%s", l, h.errOut.String())
			}
		}
	})

	t.Run("all targets end by themselves is 1", func(t *testing.T) {
		h := newHarness(t, true, runConfig{})
		a, fa := h.session(0, ":3000", 3000)
		b, fb := h.session(1, ":4000", 4000)
		done := h.start(context.Background(), []*Session{a, b}, false)
		fa.end()
		fb.end()
		res := h.result(done)
		if !res.anyEnded {
			t.Error("the exit status must be 1")
		}
		if n := strings.Count(h.errOut.String(), "ended because its log source stopped"); n != 2 {
			t.Errorf("expected two end reports, got %d\n%s", n, h.errOut.String())
		}
	})

	t.Run("duration running out is a clean stop with a detached line each", func(t *testing.T) {
		h := newHarness(t, true, runConfig{Duration: 30 * time.Second})
		a, _ := h.session(0, ":3000", 3000)
		b, _ := h.session(1, ":4000", 4000)
		ctx, cancel := context.WithCancelCause(context.Background())
		done := h.start(ctx, []*Session{a, b}, false)
		cancel(errDurationElapsed)
		res := h.result(done)
		if res.anyEnded {
			t.Error("--duration running out is not a target ending")
		}
		if n := strings.Count(h.errOut.String(), "Detached from PID 0 after 30s."); n != 2 {
			t.Errorf("expected a detached line per target, got %d\n%s", n, h.errOut.String())
		}
	})

	t.Run("a source error ends only its own target", func(t *testing.T) {
		h := newHarness(t, true, runConfig{})
		a, fa := h.session(0, ":3000", 3000)
		b, fb := h.session(1, ":4000", 4000)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := h.start(ctx, []*Session{a, b}, false)
		fb.errs <- errors.New("journalctl failed")
		fb.end()
		waitFor(t, h.errOut, ":4000 Capture of :4000 ended with an error: journalctl failed")
		fa.send("fine\n")
		waitFor(t, h.out, ":3000 | fine\n")
		cancel()
		if res := h.result(done); !res.anyEnded || res.err != nil {
			t.Errorf("result = %+v, want anyEnded and no fatal error", res)
		}
	})
}

// Sources start one after the other, in the order the targets were given, from the calling goroutine.
func TestStartSessions_StartsInOrder(t *testing.T) {
	h := newHarness(t, true, runConfig{})
	a, _ := h.session(0, "A", 3000)
	b, _ := h.session(1, "B", 4000)
	c, _ := h.session(2, "C", 5000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := startSessions(ctx, []*Session{a, b, c}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, s := range []*Session{a, b, c} {
			s.Stop()
		}
	}()
	if got, want := strings.Join(h.log.list(), ","), "start:A,start:B,start:C"; got != want {
		t.Errorf("start order = %s, want %s", got, want)
	}
}

// If the second source cannot start, the first is stopped, the error names the target, and nothing is left
// running or printed from a log. This is the case `specter 3000 4000 --source ebpf` where the kernel refuses the
// program of :4000.
func TestStartSessions_AllOrNothing(t *testing.T) {
	h := newHarness(t, true, runConfig{Source: "ebpf"})
	a, fa := h.session(0, ":3000", 3000)
	b, fb := h.session(1, ":4000", 4000)
	c, fc := h.session(2, ":5000", 5000)
	for _, f := range []*scriptedSource{fa, fb, fc} {
		f.name = "ebpf"
	}
	fb.startErr = errors.New("the kernel refused the program")

	err := startSessions(context.Background(), []*Session{a, b, c})
	if err == nil {
		t.Fatal("expected the start to fail")
	}
	if !strings.HasPrefix(err.Error(), ":4000: ") || !strings.Contains(err.Error(), "the kernel refused the program") {
		t.Errorf("the error should name the target and the reason, got %q", err)
	}
	if fa.isRunning() || fb.isRunning() || fc.isRunning() {
		t.Errorf("a source is still running after a failed start: a=%v b=%v c=%v", fa.isRunning(), fb.isRunning(), fc.isRunning())
	}
	if got := strings.Join(h.log.list(), ","); strings.Contains(got, "start::5000") {
		t.Errorf("the third source must not start after the second failed, log: %s", got)
	}
	if got := strings.Join(h.log.list(), ","); !strings.Contains(got, "stop::3000") {
		t.Errorf("the first source must be stopped, log: %s", got)
	}
	if h.out.String() != "" {
		t.Errorf("no log line may be printed before every source has started, got %q", h.out.String())
	}
}

// With one target the error is returned as it is, with no label in front.
func TestStartSessions_SingleTargetErrorHasNoLabel(t *testing.T) {
	h := newHarness(t, false, runConfig{})
	a, _ := h.session(0, ":3000", 3000)
	h.env.selectSource = func(*source.Target) (source.Source, string, string, error) {
		return nil, "", "", errors.New("no engine")
	}
	err := startSessions(context.Background(), []*Session{a})
	if err == nil || err.Error() != "no engine" {
		t.Errorf("err = %v, want the plain error without a label", err)
	}
}
