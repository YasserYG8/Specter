package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// grepRun is a pipeline of grouper, holder and filter, the way main.go builds it when --grep is set.
type grepRun struct {
	in     chan Line
	mu     sync.Mutex
	shown  []string
	done   chan struct{}
	cancel context.CancelFunc
}

// startGrepRun starts the stages. hold false leaves the holder out, which is how the pipeline worked before it
// existed.
func startGrepRun(t *testing.T, grep string, hold bool, idle time.Duration, maxLines int) *grepRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	r := &grepRun{in: make(chan Line, 4096), done: make(chan struct{}), cancel: cancel}
	t.Cleanup(cancel)

	filter := NewFilter(FilterOptions{Grep: grep})
	entries := NewStackTraceGrouper(DefaultTraceIdleTimeout).Group(ctx, r.in)
	if hold {
		entries = NewTraceHolder(filter.MatchText, idle, maxLines).Hold(ctx, entries)
	}
	go func() {
		defer close(r.done)
		for e := range entries {
			if filter.Matches(&e) {
				r.mu.Lock()
				r.shown = append(r.shown, e.Text())
				r.mu.Unlock()
			}
		}
	}()
	return r
}

func (r *grepRun) write(pid, fd int, lines ...string) {
	for _, text := range lines {
		r.in <- Line{Text: text, PID: pid, FD: fd, TS: time.Now()}
	}
}

func (r *grepRun) output() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.shown...)
}

// finish closes the input and returns everything that was shown.
func (r *grepRun) finish(t *testing.T) []string {
	t.Helper()
	close(r.in)
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the pipeline did not finish after its input closed")
	}
	return r.output()
}

// waitFor polls until output has at least n lines, or fails after the timeout.
func (r *grepRun) waitFor(t *testing.T, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(r.output()) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d lines within %v, got %q", n, timeout, r.output())
}

var (
	pythonTrace = []string{
		"Traceback (most recent call last):",
		`  File "/app/test_server.py", line 40, in do_GET`,
		"    result = 1 / 0",
		"ZeroDivisionError: division by zero",
	}
	javaTrace = []string{
		`java.lang.IllegalStateException: db timeout`,
		`    at com.example.Repo.load(Repo.java:42)`,
		`    at com.example.Api.get(Api.java:18)`,
	}
	nodeTrace = []string{
		"Error: db timeout",
		"    at Socket.onClose (/app/net.js:42:15)",
		"    at Socket.emit (events.js:315:20)",
	}
	chainedPython = []string{
		"Traceback (most recent call last):",
		`  File "/app/a.py", line 3, in first`,
		"    d['a']",
		"KeyError: 'a'",
		"During handling of the above exception, another exception occurred:",
		"Traceback (most recent call last):",
		`  File "/app/a.py", line 7, in second`,
		"    raise RuntimeError('request failed')",
		"RuntimeError: request failed",
	}
)

func TestTraceHolder_WholeTraceByAnyMatchingLine(t *testing.T) {
	cases := []struct {
		name  string
		grep  string
		trace []string
		want  []string
	}{
		{"python, match on the header", "Traceback", pythonTrace, pythonTrace},
		{"python, match on a frame", "test_server", pythonTrace, pythonTrace},
		{"python, match on the code line", "1 / 0", pythonTrace, pythonTrace},
		{"python, match on the closing line", "ZeroDivision", pythonTrace, pythonTrace},
		{"python, no match", "no-such-text", pythonTrace, nil},
		{"java, match on the header", "db timeout", javaTrace, javaTrace},
		{"java, match on a frame", "Repo.java", javaTrace, javaTrace},
		{"java, no match", "no-such-text", javaTrace, nil},
		{"node, match on the header", "db timeout", nodeTrace, nodeTrace},
		{"node, match on a frame", "net.js", nodeTrace, nodeTrace},
		{"node, no match", "no-such-text", nodeTrace, nil},
		{"chained python, match in the first part", "KeyError", chainedPython, chainedPython[:4]},
		{"chained python, match on the last line", "request failed", chainedPython, chainedPython[5:]},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := startGrepRun(t, tc.grep, true, 0, 0)
			r.write(100, 2, tc.trace...)
			got := r.finish(t)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("--grep %q\nwant %q\n got %q", tc.grep, tc.want, got)
			}
		})
	}
}

func TestTraceHolder_NextLineEndsTheTrace(t *testing.T) {
	r := startGrepRun(t, "db", true, 0, 0)
	r.write(100, 2, javaTrace...)
	r.write(100, 2, "db connection ok")
	r.write(100, 2, "unrelated line")
	got := r.finish(t)
	want := append(append([]string(nil), javaTrace...), "db connection ok")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %q\n got %q", want, got)
	}
}

func TestTraceHolder_ReleasesAfterIdleTimeout(t *testing.T) {
	r := startGrepRun(t, "db timeout", true, 40*time.Millisecond, 0)
	r.write(100, 2, nodeTrace...)

	// Nothing arrives after the trace and the input stays open, so only the timeout can release it
	r.waitFor(t, len(nodeTrace), 2*time.Second)
	if got := r.output(); !reflect.DeepEqual(got, nodeTrace) {
		t.Errorf("want %q\n got %q", nodeTrace, got)
	}
	r.finish(t)
}

func TestTraceHolder_TimeoutDropsTraceWithoutMatch(t *testing.T) {
	r := startGrepRun(t, "no-such-text", true, 40*time.Millisecond, 0)
	r.write(100, 2, nodeTrace...)
	time.Sleep(300 * time.Millisecond)
	if got := r.output(); len(got) != 0 {
		t.Errorf("expected nothing, got %q", got)
	}
	r.finish(t)
}

func TestTraceHolder_HoldsUntilItEnds(t *testing.T) {
	r := startGrepRun(t, "db timeout", true, 10*time.Second, 0)
	r.write(100, 2, nodeTrace...)
	time.Sleep(100 * time.Millisecond)
	if got := r.output(); len(got) != 0 {
		t.Fatalf("the trace should be held while it may still continue, got %q", got)
	}
	got := r.finish(t)
	if !reflect.DeepEqual(got, nodeTrace) {
		t.Errorf("closing the input should release the trace\nwant %q\n got %q", nodeTrace, got)
	}
}

func TestTraceHolder_LineCap(t *testing.T) {
	// A header and 9 frames. The holder keeps 5 lines.
	trace := []string{"Error: boom"}
	for i := 1; i <= 9; i++ {
		trace = append(trace, fmt.Sprintf("    at frame%d (/app/f.js:%d:1)", i, i))
	}

	t.Run("match inside the kept lines shows the whole trace", func(t *testing.T) {
		r := startGrepRun(t, `frame2\b`, true, 0, 5)
		r.write(100, 2, trace...)
		got := r.finish(t)
		if !reflect.DeepEqual(got, trace) {
			t.Errorf("want all %d lines, got %q", len(trace), got)
		}
	})

	t.Run("match after the kept lines shows that line and the ones after it", func(t *testing.T) {
		r := startGrepRun(t, `frame8\b`, true, 0, 5)
		r.write(100, 2, trace...)
		got := r.finish(t)
		if want := trace[8:]; !reflect.DeepEqual(got, want) {
			t.Errorf("want %q\n got %q", want, got)
		}
	})

	t.Run("no match", func(t *testing.T) {
		r := startGrepRun(t, "no-such-text", true, 0, 5)
		r.write(100, 2, trace...)
		if got := r.finish(t); len(got) != 0 {
			t.Errorf("expected nothing, got %q", got)
		}
	})

	t.Run("a long trace is not kept in memory", func(t *testing.T) {
		r := startGrepRun(t, `frame7\b`, true, 0, 5)
		r.write(100, 2, "Error: boom")
		for i := 1; i <= 3000; i++ {
			r.write(100, 2, fmt.Sprintf("    at frame%d (/app/f.js:%d:1)", i, i))
		}
		got := r.finish(t)
		if len(got) == 0 || !strings.Contains(got[0], "frame7 ") {
			t.Errorf("expected output to start at the matching line, got %d lines starting %q", len(got), got[:min(1, len(got))])
		}
	})
}

func TestTraceHolder_OtherStreamsKeepFlowing(t *testing.T) {
	r := startGrepRun(t, "db", true, 10*time.Second, 0)

	// Process 1 starts a trace and stops in the middle of it
	r.write(1, 2, nodeTrace[:2]...)
	// Process 2 and a different stream of process 1 write matching lines meanwhile
	r.write(2, 1, "db connected")
	r.write(1, 1, "db pool ready")

	r.waitFor(t, 2, 2*time.Second)
	if got := r.output(); !reflect.DeepEqual(got, []string{"db connected", "db pool ready"}) {
		t.Fatalf("other streams should not wait for the held trace, got %q", got)
	}

	got := r.finish(t)
	want := []string{"db connected", "db pool ready", nodeTrace[0], nodeTrace[1]}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %q\n got %q", want, got)
	}
}

func TestTraceHolder_TwoTracesKeepTheirOwnMatch(t *testing.T) {
	r := startGrepRun(t, "ZeroDivision", true, 0, 0)
	other := []string{
		"Traceback (most recent call last):",
		`  File "/app/other.py", line 1, in f`,
		"ValueError: bad value",
	}
	r.write(100, 2, pythonTrace...)
	r.write(100, 2, other...)
	r.write(100, 2, pythonTrace...)
	got := r.finish(t)
	want := append(append([]string(nil), pythonTrace...), pythonTrace...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %q\n got %q", want, got)
	}
}

func TestTraceHolder_InterleavedProcessesStayWhole(t *testing.T) {
	r := startGrepRun(t, "ZeroDivision", true, 0, 0)
	// Two processes write a trace at the same time; only process 1 has a matching one
	for i := range pythonTrace {
		r.write(1, 2, pythonTrace[i])
		if i < 3 {
			r.write(2, 2, javaTrace[i])
		}
	}
	got := r.finish(t)
	if !reflect.DeepEqual(got, pythonTrace) {
		t.Errorf("want %q\n got %q", pythonTrace, got)
	}
}

// Without --grep the holder is not in the pipeline and entries reach the output as before. These checks show
// that the filter ignores MatchedGrep when there is no pattern, and that the holder never changes what a single
// stream writes or its order.
func TestTraceHolder_NoGrepLeavesOutputUnchanged(t *testing.T) {
	if !NewFilter(FilterOptions{}).Matches(&Entry{Lines: []string{"x"}, FD: 1}) {
		t.Error("a filter without --grep should admit every entry")
	}

	files, err := filepath.Glob(filepath.Join("..", "..", "test", "fixtures", "*.log"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.ReplaceAll(strings.TrimRight(string(data), "\r\n"), "\r\n", "\n"), "\n")

			run := func(hold bool) []string {
				r := startGrepRun(t, "", hold, 0, 0)
				r.write(7, 1, lines...)
				return r.finish(t)
			}
			plain, held := run(false), run(true)
			if len(plain) != len(lines) {
				t.Fatalf("expected the %d fixture lines to come out, got %d", len(lines), len(plain))
			}
			// An empty pattern matches every line, so a trace is released whole and nothing is dropped or moved
			if !reflect.DeepEqual(plain, held) {
				t.Errorf("holding changed the output of %s", filepath.Base(file))
			}
		})
	}
}
