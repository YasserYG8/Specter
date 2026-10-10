package pipeline

import (
	"context"
	"testing"
	"time"
)

func TestStackTraceGrouper_NodeJSError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	grouper := NewStackTraceGrouper(100 * time.Millisecond)
	in := make(chan Line, 10)

	entries := grouper.Group(ctx, in)

	now := time.Now()
	in <- Line{Text: "Error: Connection lost", FD: 2, PID: 100, TS: now}
	in <- Line{Text: "    at Socket.onClose (/app/net.js:42:15)", FD: 2, PID: 100, TS: now}
	in <- Line{Text: "    at Socket.emit (events.js:315:20)", FD: 2, PID: 100, TS: now}
	in <- Line{Text: "Server restarted successfully", FD: 2, PID: 100, TS: now}
	close(in)

	var result []Entry
	for e := range entries {
		result = append(result, e)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 entries (1 trace group + 1 normal line), got %d", len(result))
	}

	traceEntry := result[0]
	if !traceEntry.IsTrace {
		t.Errorf("expected IsTrace to be true")
	}
	if len(traceEntry.Lines) != 3 {
		t.Errorf("expected 3 lines in trace group, got %d", len(traceEntry.Lines))
	}
	if traceEntry.Level != SeverityError {
		t.Errorf("expected SeverityError, got %v", traceEntry.Level)
	}

	normalEntry := result[1]
	if normalEntry.IsTrace {
		t.Errorf("expected normal entry to not be a trace")
	}
	if len(normalEntry.Lines) != 1 || normalEntry.Lines[0] != "Server restarted successfully" {
		t.Errorf("unexpected normal entry: %+v", normalEntry)
	}
}

func TestStackTraceGrouper_PythonTraceback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	grouper := NewStackTraceGrouper(100 * time.Millisecond)
	in := make(chan Line, 10)

	entries := grouper.Group(ctx, in)

	now := time.Now()
	in <- Line{Text: "Traceback (most recent call last):", FD: 2, PID: 200, TS: now}
	in <- Line{Text: `  File "server.py", line 14, in <module>`, FD: 2, PID: 200, TS: now}
	in <- Line{Text: `    app.run()`, FD: 2, PID: 200, TS: now}
	in <- Line{Text: `  File "app.py", line 88, in run`, FD: 2, PID: 200, TS: now}
	close(in)

	var result []Entry
	for e := range entries {
		result = append(result, e)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 grouped entry, got %d", len(result))
	}
	if !result[0].IsTrace {
		t.Errorf("expected IsTrace true")
	}
	if len(result[0].Lines) != 4 {
		t.Errorf("expected 4 lines in group, got %d", len(result[0].Lines))
	}
}

func TestStackTraceGrouper_IdleFlush(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Short 50ms timeout
	grouper := NewStackTraceGrouper(50 * time.Millisecond)
	in := make(chan Line, 10)

	entries := grouper.Group(ctx, in)

	in <- Line{Text: "panic: runtime error: index out of range", FD: 2, PID: 300, TS: time.Now()}

	select {
	case entry := <-entries:
		if !entry.IsTrace {
			t.Errorf("expected IsTrace to be true")
		}
		if len(entry.Lines) != 1 {
			t.Errorf("expected 1 line in flushed group, got %d", len(entry.Lines))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for idle flush of trace")
	}
	close(in)
}
