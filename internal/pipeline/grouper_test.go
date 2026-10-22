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

	// In streaming record machine: 4 lines arrive -> 4 entries emitted immediately
	if len(result) != 4 {
		t.Fatalf("expected 4 entries emitted immediately in real-time, got %d", len(result))
	}

	// First line is error header
	if !result[0].IsTrace || result[0].IsFrame {
		t.Errorf("expected result[0] to be trace header: %+v", result[0])
	}
	if result[0].Level != SeverityError {
		t.Errorf("expected SeverityError for trace header, got %v", result[0].Level)
	}

	// Following 2 lines are continuation frames inheriting SeverityError
	for i := 1; i <= 2; i++ {
		if !result[i].IsTrace || !result[i].IsFrame {
			t.Errorf("expected result[%d] to be continuation frame: %+v", i, result[i])
		}
		if result[i].Level != SeverityError {
			t.Errorf("expected result[%d] to inherit SeverityError, got %v", i, result[i].Level)
		}
	}

	// Last line is normal line
	if result[3].IsTrace || result[3].IsFrame {
		t.Errorf("expected result[3] to not be trace or frame: %+v", result[3])
	}
	if result[3].Text() != "Server restarted successfully" {
		t.Errorf("unexpected text in result[3]: %q", result[3].Text())
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

	if len(result) != 4 {
		t.Fatalf("expected 4 streamed entries, got %d", len(result))
	}
	if !result[0].IsTrace {
		t.Errorf("expected IsTrace true on header")
	}
	for i := 1; i < 4; i++ {
		if !result[i].IsTrace || !result[i].IsFrame {
			t.Errorf("expected result[%d] to be continuation frame", i)
		}
		if result[i].Level != SeverityError {
			t.Errorf("expected result[%d] to inherit SeverityError", i)
		}
	}
}

func TestStackTraceGrouper_IdleFlush(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	grouper := NewStackTraceGrouper(50 * time.Millisecond)
	in := make(chan Line, 10)

	entries := grouper.Group(ctx, in)

	in <- Line{Text: "panic: runtime error: index out of range", FD: 2, PID: 300, TS: time.Now()}

	select {
	case entry := <-entries:
		if !entry.IsTrace {
			t.Errorf("expected IsTrace true on immediate emit")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("expected immediate emission of entry without waiting for timeout")
	}
}
