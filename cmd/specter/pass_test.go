package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fatih/color"

	"github.com/YasserYG8/Specter/internal/pipeline"
	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/ui"
)

// fakeKeys is a key reader that needs no terminal. The test presses keys with press.
type fakeKeys struct {
	log *eventLog

	ch        chan ui.Key
	closeOnce sync.Once
	mu        sync.Mutex
	started   bool
}

func newFakeKeys(log *eventLog) *fakeKeys { return &fakeKeys{log: log, ch: make(chan ui.Key, 8)} }

func (f *fakeKeys) Start(context.Context) (<-chan ui.Key, error) {
	f.mu.Lock()
	f.started = true
	f.mu.Unlock()
	f.log.add("keys-start")
	return f.ch, nil
}

func (f *fakeKeys) Stop() {
	f.log.add("keys-stop")
	f.closeOnce.Do(func() { close(f.ch) })
}

func (f *fakeKeys) press(k ui.Key) { f.ch <- k }

// passHarness runs passes over scripted sources, with fake keys and a fake picker.
type passHarness struct {
	t      *testing.T
	log    *eventLog
	out    *syncBuffer
	errOut *syncBuffer
	pe     *passEnv

	mu      sync.Mutex
	current map[int]*scriptedSource // the latest source made for each port
	keys    []*fakeKeys             // the key readers made, one per pass
	picks   []*source.Target        // what the picker returns, one per call; when empty, the picker is closed
	pickN   int
}

func newPassHarness(t *testing.T, cfg runConfig, withKeys bool) *passHarness {
	t.Helper()
	old := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = old })

	if cfg.NoOutputDelay == 0 {
		cfg.NoOutputDelay = time.Hour
	}
	h := &passHarness{t: t, log: &eventLog{}, out: &syncBuffer{}, errOut: &syncBuffer{}, current: map[int]*scriptedSource{}}
	h.pe = &passEnv{
		cfg:          cfg,
		rendererOpts: pipeline.RendererOptions{NoColor: true},
		out:          h.out,
		errOut:       h.errOut,
		stdoutIsTTY:  true,
		configure: func(env *runEnv) {
			env.selectSource = func(target *source.Target) (source.Source, string, string, error) {
				f := newScripted(fmt.Sprint(target.Port), h.log)
				h.mu.Lock()
				h.current[target.Port] = f
				h.mu.Unlock()
				return f, "test reason", "", nil
			}
			env.watch = func(context.Context, int) <-chan struct{} { return nil }
			env.checkPtrace = func() error { return nil }
		},
	}
	if withKeys {
		h.pe.newKeys = func() keySource {
			k := newFakeKeys(h.log)
			h.mu.Lock()
			h.keys = append(h.keys, k)
			h.mu.Unlock()
			return k
		}
	}
	return h
}

func target(port int) *source.Target {
	return &source.Target{Port: port, Cmdline: []string{"app"}}
}

// src returns the latest source made for the port, waiting for it to exist.
func (h *passHarness) src(port int) *scriptedSource {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		f := h.current[port]
		h.mu.Unlock()
		if f != nil {
			return f
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("no source was made for port %d", port)
	return nil
}

// lastKeys returns the key reader of the n-th pass (1-based), waiting for it.
func (h *passHarness) keysOf(n int) *fakeKeys {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		if len(h.keys) >= n {
			k := h.keys[n-1]
			h.mu.Unlock()
			return k
		}
		h.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("pass %d never made a key reader", n)
	return nil
}

func (h *passHarness) pick(context.Context) (*source.Target, error) {
	h.log.add("pick")
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pickN++
	if len(h.picks) == 0 {
		return nil, nil // the picker was closed
	}
	t := h.picks[0]
	h.picks = h.picks[1:]
	return t, nil
}

func (h *passHarness) pickCalls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pickN
}

// run starts the loop for the first pass on the given targets. The result comes on the channel.
func (h *passHarness) run(ctx context.Context, first ...*source.Target) <-chan passResult {
	resolved := make([]resolvedTarget, len(first))
	for i, t := range first {
		resolved[i] = resolvedTarget{Label: fmt.Sprintf(":%d", t.Port), Target: t}
	}
	done := make(chan passResult, 1)
	go func() {
		done <- runLoop(ctx,
			func(ctx context.Context) passResult { return runPass(ctx, resolved, h.pe) },
			func(ctx context.Context) (passResult, bool) { return pickPass(ctx, h.pick, h.pe) })
	}()
	return done
}

func (h *passHarness) result(done <-chan passResult) passResult {
	h.t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(5 * time.Second):
		h.t.Fatalf("the run did not end\nstdout: %q\nstderr: %q\nlog: %v", h.out.String(), h.errOut.String(), h.log.list())
		return passResult{}
	}
}

// waitCount waits until buf contains text at least n times.
func waitCount(t *testing.T, buf *syncBuffer, text string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(buf.String(), text) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d x %q in %q", n, text, buf.String())
}

func indexOf(events []string, e string) int {
	for i, x := range events {
		if x == e {
			return i
		}
	}
	return -1
}

func TestPass_EscDetachesEverythingAndOpensThePicker(t *testing.T) {
	h := newPassHarness(t, runConfig{}, true)
	h.picks = []*source.Target{target(8082)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.run(ctx, target(4200), target(8082))

	waitCount(t, h.errOut, keyHint, 1)
	h.src(4200).send("one\n")
	waitFor(t, h.out, ":4200 | one\n")
	h.keysOf(1).press(ui.KeyEsc)

	// Both targets detach with the usual line, then the picker opens, then the chosen service streams
	waitCount(t, h.errOut, ":4200 Detached from PID 0 cleanly.", 1)
	waitCount(t, h.errOut, ":8082 Detached from PID 0 cleanly.", 1)
	waitCount(t, h.errOut, keyHint, 2)
	h.src(8082).send("two\n")
	waitFor(t, h.out, "two\n")

	events := h.log.list()
	if stop, pick := indexOf(events, "keys-stop"), indexOf(events, "pick"); stop < 0 || pick < 0 || stop > pick {
		t.Errorf("the key reader must stop (and give the terminal back) before the picker opens, events: %v", events)
	}
	if n := h.pickCalls(); n != 1 {
		t.Errorf("the picker was opened %d times, want 1", n)
	}
	// The second pass has one target, so its lines have no tag
	if strings.Contains(h.out.String(), "8082 |") {
		t.Errorf("a pass with one target must not tag its lines:\n%s", h.out.String())
	}

	cancel() // Ctrl+C
	res := h.result(done)
	if res.err != nil || res.anyEnded || res.back {
		t.Errorf("result = %+v, want a clean end", res)
	}
	if n := h.pickCalls(); n != 1 {
		t.Errorf("Ctrl+C must not open the picker, but it was opened %d times", n)
	}
}

func TestPass_CtrlCQuitsWithoutThePicker(t *testing.T) {
	h := newPassHarness(t, runConfig{}, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.run(ctx, target(3000))
	waitCount(t, h.errOut, keyHint, 1)

	cancel()
	res := h.result(done)
	if res.back || res.anyEnded || res.err != nil {
		t.Errorf("result = %+v, want a clean end", res)
	}
	if n := h.pickCalls(); n != 0 {
		t.Errorf("the picker was opened %d times after Ctrl+C", n)
	}
	if !strings.Contains(h.errOut.String(), "Detached from PID 0 cleanly.") {
		t.Errorf("no detached line\n%s", h.errOut.String())
	}
	if indexOf(h.log.list(), "keys-stop") < 0 {
		t.Errorf("the key reader was not stopped, events: %v", h.log.list())
	}
}

// Esc, then the picker is closed: the exit status is the one of the last pass.
func TestPass_PickerClosedEndsWithTheLastPassStatus(t *testing.T) {
	t.Run("nothing ended by itself is 0", func(t *testing.T) {
		h := newPassHarness(t, runConfig{}, true)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := h.run(ctx, target(3000))
		waitCount(t, h.errOut, keyHint, 1)
		h.keysOf(1).press(ui.KeyEsc)
		res := h.result(done)
		if res.anyEnded || res.err != nil {
			t.Errorf("result = %+v, want status 0", res)
		}
		if h.pickCalls() != 1 {
			t.Errorf("the picker should have been opened once, got %d", h.pickCalls())
		}
	})

	t.Run("a target that ended by itself in the last pass is 1", func(t *testing.T) {
		h := newPassHarness(t, runConfig{}, true)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := h.run(ctx, target(3000), target(4000))
		waitCount(t, h.errOut, keyHint, 1)
		h.src(4000).end() // :4000 ends by itself
		waitCount(t, h.errOut, ":4000 Capture of PID 0 ended", 1)
		h.keysOf(1).press(ui.KeyEsc)
		res := h.result(done)
		if !res.anyEnded {
			t.Errorf("result = %+v, want status 1 because a target ended by itself before Esc", res)
		}
	})

	t.Run("a target that ended in an earlier pass does not count", func(t *testing.T) {
		h := newPassHarness(t, runConfig{}, true)
		h.picks = []*source.Target{target(5000)}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := h.run(ctx, target(3000), target(4000))
		waitCount(t, h.errOut, keyHint, 1)
		h.src(4000).end()
		waitCount(t, h.errOut, ":4000 Capture of PID 0 ended", 1)
		h.keysOf(1).press(ui.KeyEsc) // the user saw the end report and moves on
		waitCount(t, h.errOut, keyHint, 2)
		cancel()
		res := h.result(done)
		if res.anyEnded {
			t.Errorf("result = %+v: the target of the first pass must not count in the second", res)
		}
	})
}

// With stdin not a terminal, the terminal is not touched and nothing is said about keys.
func TestPass_NoKeysWhenSpecterDoesNotHaveTheTerminal(t *testing.T) {
	for name, withKeys := range map[string]bool{"newKeys returns nil": true, "no newKeys at all": false} {
		t.Run(name, func(t *testing.T) {
			h := newPassHarness(t, runConfig{}, withKeys)
			if withKeys {
				h.pe.newKeys = func() keySource { return nil }
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := h.run(ctx, target(3000))
			waitCount(t, h.errOut, "Source:", 1)
			h.src(3000).send("hello\n")
			waitFor(t, h.out, "hello\n")
			cancel()
			h.result(done)

			if strings.Contains(h.errOut.String(), "Esc") {
				t.Errorf("no hint about keys may be printed:\n%s", h.errOut.String())
			}
			for _, e := range h.log.list() {
				if strings.HasPrefix(e, "keys-") {
					t.Errorf("the key reader was used: %v", h.log.list())
				}
			}
			if h.pickCalls() != 0 {
				t.Error("the picker was opened")
			}
		})
	}
}

func TestPass_EnterAddsAnEmptyLineOnlyWhenStdoutIsATerminal(t *testing.T) {
	for _, tty := range []bool{true, false} {
		t.Run(fmt.Sprintf("stdout is a terminal: %v", tty), func(t *testing.T) {
			h := newPassHarness(t, runConfig{}, true)
			h.pe.stdoutIsTTY = tty
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := h.run(ctx, target(3000))
			waitCount(t, h.errOut, keyHint, 1)

			h.src(3000).send("a\n")
			waitFor(t, h.out, "a\n")
			h.keysOf(1).press(ui.KeyEnter)
			time.Sleep(100 * time.Millisecond)
			h.src(3000).send("b\n")
			waitFor(t, h.out, "b\n")
			cancel()
			h.result(done)

			want := "a\nb\n"
			if tty {
				want = "a\n\nb\n"
			}
			if got := h.out.String(); got != want {
				t.Errorf("stdout = %q, want %q", got, want)
			}
		})
	}
}

func TestPass_OtherKeysAreIgnored(t *testing.T) {
	h := newPassHarness(t, runConfig{}, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.run(ctx, target(3000))
	waitCount(t, h.errOut, keyHint, 1)

	h.keysOf(1).press(ui.Key(99))
	time.Sleep(100 * time.Millisecond)
	h.src(3000).send("still streaming\n")
	waitFor(t, h.out, "still streaming\n")
	if h.pickCalls() != 0 {
		t.Error("an ignored key opened the picker")
	}
	cancel()
	h.result(done)
}

// An Enter while a target's partial row is open, with several targets, closes the row first.
func TestOutputWriter_BlankClosesAnOpenRow(t *testing.T) {
	var buf syncBuffer
	ow := &outputWriter{w: &buf, tag: newTagger([]string{"a", "b"}, false)}
	ow.write(0, "Continue? ", true)
	ow.blank()
	ow.write(1, "ready", false)
	if got, want := buf.String(), "a | Continue? \n\nb | ready\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --duration counts from the start of each pass: after Esc and a new choice, the time starts again.
func TestPass_DurationStartsAgainAfterEsc(t *testing.T) {
	h := newPassHarness(t, runConfig{Duration: 500 * time.Millisecond}, true)
	h.picks = []*source.Target{target(8082)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.run(ctx, target(4200))

	waitCount(t, h.errOut, keyHint, 1)
	time.Sleep(300 * time.Millisecond)
	h.keysOf(1).press(ui.KeyEsc) // 300 ms into the first pass
	waitCount(t, h.errOut, keyHint, 2)
	time.Sleep(300 * time.Millisecond) // 600 ms after the start of the run, but 300 ms into the second pass
	select {
	case res := <-done:
		t.Fatalf("the second pass ended too early (%+v): the time of the first pass was counted", res)
	default:
	}

	res := h.result(done) // the second pass ends by --duration
	if res.anyEnded || res.back || res.err != nil {
		t.Errorf("result = %+v, want a clean end", res)
	}
	if n := strings.Count(h.errOut.String(), "Detached from PID 0 after 500ms."); n != 1 {
		t.Errorf("expected one 'after 500ms' line (the second pass), got %d\n%s", n, h.errOut.String())
	}
	if n := h.pickCalls(); n != 1 {
		t.Errorf("--duration must end Specter, not open the picker; the picker was opened %d times", n)
	}
}

func TestCLI_HelpExplainsTheKeys(t *testing.T) {
	long := newRootCmd().Long
	for _, want := range []string{"Esc", "Ctrl+C", "Enter", "stty sane"} {
		if !strings.Contains(long, want) {
			t.Errorf("the help text should mention %q, got:\n%s", want, long)
		}
	}
}

// After Esc the picker opens on a clean screen: the screen is cleared after the "detached" lines and before the
// picker, once for each time the picker opens. A picker that was not opened by Esc clears nothing.
func TestPass_ScreenIsClearedBeforeThePickerOpens(t *testing.T) {
	h := newPassHarness(t, runConfig{}, true)
	h.pe.clearBeforePicker = true
	h.picks = []*source.Target{target(8082)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.run(ctx, target(4200))

	waitCount(t, h.errOut, keyHint, 1)
	if strings.Contains(h.errOut.String(), clearScreen) {
		t.Fatalf("the screen must not be cleared before Esc:\n%q", h.errOut.String())
	}
	h.keysOf(1).press(ui.KeyEsc)
	waitCount(t, h.errOut, keyHint, 2)

	errOut := h.errOut.String()
	if n := strings.Count(errOut, clearScreen); n != 1 {
		t.Fatalf("the screen should be cleared once, it was cleared %d times:\n%q", n, errOut)
	}
	clearAt := strings.Index(errOut, clearScreen)
	if detached := strings.Index(errOut, "Detached from PID 0 cleanly."); detached < 0 || detached > clearAt {
		t.Errorf("the screen must be cleared after the detached line, so nothing is written after it:\n%q", errOut)
	}
	if hint := strings.LastIndex(errOut, keyHint); hint < clearAt {
		t.Errorf("the lines of the new stream must come after the clear:\n%q", errOut)
	}
	events := h.log.list()
	if stop, pick := indexOf(events, "keys-stop"), indexOf(events, "pick"); stop < 0 || stop > pick {
		t.Errorf("the terminal must be given back before the screen is cleared and the picker opens: %v", events)
	}

	cancel() // Ctrl+C quits: no picker, so no clear
	h.result(done)
	if n := strings.Count(h.errOut.String(), clearScreen); n != 1 {
		t.Errorf("Ctrl+C must not clear the screen, cleared %d times", n)
	}
}

func TestPass_ScreenIsNotClearedWhenStderrIsNotATerminal(t *testing.T) {
	h := newPassHarness(t, runConfig{}, true) // clearBeforePicker is off
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := h.run(ctx, target(4200))
	waitCount(t, h.errOut, keyHint, 1)
	h.keysOf(1).press(ui.KeyEsc)
	h.result(done)
	if strings.Contains(h.errOut.String(), "\x1b[") {
		t.Errorf("no escape code may be written:\n%q", h.errOut.String())
	}
}

// Erasing only the screen is what Ctrl+L does: the old logs stay in the scrollback. The picker must get a really
// clean terminal, so the sequence also erases the scrollback, as the `clear` command does.
func TestClearScreen_AlsoErasesTheScrollback(t *testing.T) {
	for name, seq := range map[string]string{"cursor to the top left": "\x1b[H", "erase the screen": "\x1b[2J", "erase the scrollback": "\x1b[3J"} {
		if !strings.Contains(clearScreen, seq) {
			t.Errorf("clearScreen does not %s (%q is missing)", name, seq)
		}
	}
}
