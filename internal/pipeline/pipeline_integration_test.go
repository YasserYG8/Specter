package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/specter-cli/specter/internal/source"
)

func TestGrouper_JavaExceptionWithCausedBy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	grouper := NewStackTraceGrouper(50 * time.Millisecond)
	in := make(chan Line, 10)
	out := grouper.Group(ctx, in)

	now := time.Now()
	javaLines := []string{
		"java.lang.NullPointerException: Cannot invoke method on null object",
		"\tat com.example.service.UserService.getUser(UserService.java:42)",
		"\tat com.example.controller.UserController.handle(UserController.java:18)",
		"Caused by: java.io.IOException: Database connection timed out",
		"\tat com.example.db.ConnectionPool.acquire(ConnectionPool.java:99)",
		"\t... 15 more",
	}

	for _, text := range javaLines {
		in <- Line{
			Text: text,
			FD:   2,
			PID:  500,
			TS:   now,
		}
	}
	close(in)

	var received []Entry
	for entry := range out {
		received = append(received, entry)
	}

	if len(received) != 1 {
		t.Fatalf("expected 1 atomic Entry for Java exception with caused by, got %d", len(received))
	}

	entry := received[0]
	if !entry.IsTrace {
		t.Errorf("expected entry.IsTrace to be true")
	}
	if entry.Level != SeverityError {
		t.Errorf("expected entry.Level to be SeverityError, got %v", entry.Level)
	}
	if entry.FD != 2 {
		t.Errorf("expected FD 2, got %d", entry.FD)
	}
	if len(entry.Lines) != len(javaLines) {
		t.Fatalf("expected %d lines in entry, got %d", len(javaLines), len(entry.Lines))
	}
	if !strings.HasPrefix(entry.Lines[0], "java.lang.NullPointerException") {
		t.Errorf("expected first line to be exception header, got %q", entry.Lines[0])
	}
	if !strings.HasPrefix(entry.Lines[3], "Caused by:") {
		t.Errorf("expected line 4 to be Caused by frame, got %q", entry.Lines[3])
	}
}

func TestGrouper_InterleavedStreamIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	grouper := NewStackTraceGrouper(50 * time.Millisecond)
	in := make(chan Line, 10)
	out := grouper.Group(ctx, in)

	now := time.Now()

	// 1. Stderr starts a multi-line error trace
	in <- Line{Text: "Error: Connection lost", FD: 2, PID: 100, TS: now}

	// 2. Stdout emits an interleaved normal line on the same PID
	in <- Line{Text: "User request processed 200 OK", FD: 1, PID: 100, TS: now}

	// 3. Stderr continues its stack trace frames
	in <- Line{Text: "    at Socket.onClose (net.js:42)", FD: 2, PID: 100, TS: now}
	in <- Line{Text: "    at Socket.emit (events.js:18)", FD: 2, PID: 100, TS: now}

	close(in)

	var entries []Entry
	for e := range out {
		entries = append(entries, e)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 distinct entries (1 stdout, 1 stderr trace), got %d", len(entries))
	}

	var stdoutEntry, stderrEntry *Entry
	for i := range entries {
		if entries[i].FD == 1 {
			stdoutEntry = &entries[i]
		} else if entries[i].FD == 2 {
			stderrEntry = &entries[i]
		}
	}

	if stdoutEntry == nil {
		t.Fatal("expected stdout entry on fd 1, but none found")
	}
	if stdoutEntry.IsTrace {
		t.Errorf("stdout entry should not be classified as stack trace")
	}
	if len(stdoutEntry.Lines) != 1 || stdoutEntry.Lines[0] != "User request processed 200 OK" {
		t.Errorf("unexpected stdout lines: %v", stdoutEntry.Lines)
	}

	if stderrEntry == nil {
		t.Fatal("expected stderr entry on fd 2, but none found")
	}
	if !stderrEntry.IsTrace {
		t.Errorf("stderr entry should be classified as stack trace")
	}
	if len(stderrEntry.Lines) != 3 {
		t.Fatalf("expected 3 lines in stderr error trace, got %d: %v", len(stderrEntry.Lines), stderrEntry.Lines)
	}
	if stderrEntry.Lines[0] != "Error: Connection lost" {
		t.Errorf("unexpected header: %q", stderrEntry.Lines[0])
	}
}

func TestPipeline_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	chunks := make(chan source.Chunk, 10)
	now := time.Now()

	// 1. LineAssembler
	assembler := NewLineAssembler(30 * time.Millisecond)
	lines := assembler.Assemble(ctx, chunks)

	// 2. StackTraceGrouper
	grouper := NewStackTraceGrouper(40 * time.Millisecond)
	entries := grouper.Group(ctx, lines)

	// 3. Filter (MinLevel: SeverityInfo suppresses DEBUG)
	filter := NewFilter(FilterOptions{
		MinLevel: SeverityInfo,
	})

	// 4. Renderer (NoColor: true for deterministic text assertions)
	renderer := NewRenderer(RendererOptions{
		NoColor: true,
	})

	// Feed raw chunks:
	// A. DEBUG log on fd 1 (should be suppressed by filter)
	chunks <- source.Chunk{
		Bytes: []byte("{\"level\":\"debug\",\"msg\":\"Cache lookup query\",\"key\":\"user:1\"}\n"),
		FD:    1,
		PID:   10,
		TS:    now,
	}

	// B. INFO JSON log split across chunk boundary on fd 1 (should be reassembled and single-line formatted)
	chunks <- source.Chunk{
		Bytes: []byte("{\"level\":\"info\",\"msg\":\"Incoming HTTP request\",\"path\":\"/api/v1/users\""),
		FD:    1,
		PID:   10,
		TS:    now,
	}
	chunks <- source.Chunk{
		Bytes: []byte(",\"status\":200,\"duration\":\"12ms\"}\n"),
		FD:    1,
		PID:   10,
		TS:    now,
	}

	// C. Multi-line error stack trace on fd 2 (should be preserved atomically)
	chunks <- source.Chunk{
		Bytes: []byte("TypeError: Cannot read properties of undefined (reading 'id')\n"),
		FD:    2,
		PID:   10,
		TS:    now,
	}
	chunks <- source.Chunk{
		Bytes: []byte("    at getUserById (/app/server.js:42:15)\n    at handleRequest (/app/router.js:18:9)\n"),
		FD:    2,
		PID:   10,
		TS:    now,
	}

	close(chunks)

	var renderedOutput []string
	for entry := range entries {
		if !filter.Matches(&entry) {
			continue
		}
		renderedOutput = append(renderedOutput, renderer.Render(&entry))
	}

	if len(renderedOutput) != 2 {
		t.Fatalf("expected exactly 2 rendered outputs (1 info json, 1 error trace), got %d:\n%v", len(renderedOutput), renderedOutput)
	}

	// Check 1: Info JSON log
	infoLog := renderedOutput[0]
	if strings.Contains(infoLog, "Cache lookup query") {
		t.Errorf("DEBUG log should have been suppressed, found in output: %s", infoLog)
	}
	if !strings.Contains(infoLog, "[INFO]") || !strings.Contains(infoLog, "Incoming HTTP request") {
		t.Errorf("expected formatted INFO log line, got: %s", infoLog)
	}
	if !strings.Contains(infoLog, "path=/api/v1/users") || !strings.Contains(infoLog, "status=200") {
		t.Errorf("expected key-value attributes in single-line JSON output, got: %s", infoLog)
	}
	if strings.Contains(infoLog, "\n") {
		t.Errorf("structured JSON output must be a single line, got multiline: %s", infoLog)
	}

	// Check 2: Error stack trace
	errorTrace := renderedOutput[1]
	if !strings.Contains(errorTrace, "TypeError: Cannot read properties of undefined") {
		t.Errorf("expected error header in trace, got: %s", errorTrace)
	}
	if !strings.Contains(errorTrace, "at getUserById (/app/server.js:42:15)") ||
		!strings.Contains(errorTrace, "at handleRequest (/app/router.js:18:9)") {
		t.Errorf("expected all stack frames to be preserved in trace output, got: %s", errorTrace)
	}
}

func TestGrouper_PythonTracebackCompleteWithFinalException(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	grouper := NewStackTraceGrouper(100 * time.Millisecond)
	in := make(chan Line, 10)
	out := grouper.Group(ctx, in)

	now := time.Now()
	in <- Line{Text: "Traceback (most recent call last):", FD: 2, PID: 42, TS: now}
	in <- Line{Text: `  File "/app/server.py", line 42, in handle_request`, FD: 2, PID: 42, TS: now}
	in <- Line{Text: `    process_data(payload)`, FD: 2, PID: 42, TS: now}
	in <- Line{Text: `  File "/app/utils.py", line 18, in process_data`, FD: 2, PID: 42, TS: now}
	in <- Line{Text: `    raise ValueError("invalid configuration: key missing")`, FD: 2, PID: 42, TS: now}
	in <- Line{Text: `ValueError: invalid configuration: key missing`, FD: 2, PID: 42, TS: now}
	in <- Line{Text: `Worker process 42 recovered`, FD: 2, PID: 42, TS: now}

	close(in)

	var entries []Entry
	for e := range out {
		entries = append(entries, e)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (1 complete python trace + 1 normal recovered line), got %d", len(entries))
	}

	trace := entries[0]
	if !trace.IsTrace {
		t.Errorf("expected trace.IsTrace to be true")
	}
	if trace.Level != SeverityError {
		t.Errorf("expected SeverityError, got %v", trace.Level)
	}
	if len(trace.Lines) != 6 {
		t.Fatalf("expected 6 lines in python trace including final unindented exception, got %d: %v", len(trace.Lines), trace.Lines)
	}
	lastLine := trace.Lines[len(trace.Lines)-1]
	if lastLine != `ValueError: invalid configuration: key missing` {
		t.Errorf("expected final line to be unindented exception, got %q", lastLine)
	}

	normal := entries[1]
	if normal.IsTrace {
		t.Errorf("expected recovered worker line to not be a trace")
	}
	if len(normal.Lines) != 1 || normal.Lines[0] != "Worker process 42 recovered" {
		t.Errorf("unexpected normal entry: %v", normal)
	}
}

func TestParser_BenignErrorPhrases(t *testing.T) {
	benignLines := []string{
		"Build succeeded: 0 errors, 0 warnings",
		"Transaction committed without error",
		"Connection closed with no error",
	}

	for _, line := range benignLines {
		level := DetectSeverity(line)
		if level == SeverityError {
			t.Errorf("expected line %q to not be classified as SeverityError, got %v", line, level)
		}
		if isTraceHeader(line) {
			t.Errorf("expected line %q to not be classified as trace header", line)
		}
	}
}

func TestRenderer_HTTPStatusStyling(t *testing.T) {
	oldNoColor := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = oldNoColor }()

	renderer := NewRenderer(RendererOptions{NoColor: false})

	// 2xx status -> Green
	entry200 := &Entry{
		Lines: []string{`127.0.0.1 - - [10/Oct/2026:13:00:00] "GET /api/v1/users HTTP/1.1" 200 1024`},
		Level: SeverityInfo,
	}
	rendered200 := renderer.Render(entry200)
	if !HasANSI(rendered200) {
		t.Errorf("expected ANSI color in rendered 200 HTTP access log, got plain: %q", rendered200)
	}

	// 3xx status -> Cyan
	entry301 := &Entry{
		Lines: []string{`127.0.0.1 - - [10/Oct/2026:13:00:00] "GET /old-path HTTP/1.1" 301 0`},
		Level: SeverityInfo,
	}
	rendered301 := renderer.Render(entry301)
	if !HasANSI(rendered301) {
		t.Errorf("expected ANSI color in rendered 301 HTTP access log, got plain: %q", rendered301)
	}
}
