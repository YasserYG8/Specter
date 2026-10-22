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
	in := make(chan Line, 20)
	out := grouper.Group(ctx, in)

	now := time.Now()
	javaLines := []string{
		"java.lang.NullPointerException: Cannot invoke \"String.length()\" because \"name\" is null",
		"    at com.example.service.UserService.processUser(UserService.java:42)",
		"    at com.example.controller.UserController.getUser(UserController.java:18)",
		"Caused by: java.lang.IllegalArgumentException: Input buffer cannot be empty",
		"    at com.example.util.Buffer.validate(Buffer.java:99)",
		"    ... 34 more",
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

	// In zero-buffering streaming state machine, all 6 lines are emitted immediately
	if len(received) != len(javaLines) {
		t.Fatalf("expected %d entries emitted immediately, got %d", len(javaLines), len(received))
	}

	// Verify header
	if !received[0].IsTrace || received[0].IsFrame {
		t.Errorf("expected received[0] to be trace header: %+v", received[0])
	}
	if received[0].Level != SeverityError {
		t.Errorf("expected received[0].Level to be SeverityError, got %v", received[0].Level)
	}

	// Verify all continuation frames inherit SeverityError
	for i := 1; i < len(received); i++ {
		if !received[i].IsTrace {
			t.Errorf("expected received[%d].IsTrace to be true", i)
		}
		if received[i].Level != SeverityError {
			t.Errorf("expected received[%d].Level to be SeverityError, got %v", i, received[i].Level)
		}
	}

	if !strings.HasPrefix(received[3].Text(), "Caused by:") {
		t.Errorf("expected line 4 to be Caused by frame, got %q", received[3].Text())
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

	if len(entries) != 4 {
		t.Fatalf("expected 4 streamed entries, got %d", len(entries))
	}

	var stdoutCount, stderrTraceCount int
	for _, e := range entries {
		if e.FD == 1 {
			stdoutCount++
			if e.IsTrace {
				t.Errorf("stdout entry on fd 1 should not be classified as trace: %+v", e)
			}
		} else if e.FD == 2 {
			if e.IsTrace && e.Level == SeverityError {
				stderrTraceCount++
			}
		}
	}

	if stdoutCount != 1 {
		t.Errorf("expected 1 stdout entry, got %d", stdoutCount)
	}
	if stderrTraceCount != 3 {
		t.Errorf("expected 3 stderr trace entries, got %d", stderrTraceCount)
	}
}

func TestPipeline_EndToEnd(t *testing.T) {
	oldNoColor := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = oldNoColor }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 1. Assembler
	assembler := NewLineAssembler(100 * time.Millisecond)

	// 2. Grouper (zero-buffering streaming state machine)
	grouper := NewStackTraceGrouper(100 * time.Millisecond)

	// 3. Filter
	filter := NewFilter(FilterOptions{
		MinLevel:   SeverityInfo,
		OnlyStderr: false,
	})

	// 4. Renderer
	renderer := NewRenderer(RendererOptions{
		NoColor: false,
	})

	chunks := make(chan source.Chunk, 20)
	lines := assembler.Assemble(ctx, chunks)
	entries := grouper.Group(ctx, lines)

	now := time.Now()

	// A. Debug JSON log on fd 1 (should be dropped by filter)
	chunks <- source.Chunk{
		Bytes: []byte("{\"level\":\"debug\",\"msg\":\"Cache lookup query\",\"hits\":0}\n"),
		FD:    1,
		PID:   10,
		TS:    now,
	}

	// B. Info JSON log on fd 1 (should pass filter and format as single-line)
	chunks <- source.Chunk{
		Bytes: []byte("{\"level\":\"info\",\"msg\":\"Incoming HTTP request\",\"status\":200,\"path\":\"/api/v1/users\",\"duration\":\"12ms\"}\n"),
		FD:    1,
		PID:   10,
		TS:    now,
	}

	// C. Multi-line error stack trace on fd 2 (should be preserved and streamed)
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

	// 1 Info line + 3 error trace lines = 4 total rendered outputs
	if len(renderedOutput) != 4 {
		t.Fatalf("expected exactly 4 rendered outputs (1 info json + 3 error trace lines), got %d:\n%v", len(renderedOutput), renderedOutput)
	}

	// Check 1: Info JSON log
	infoLog := renderedOutput[0]
	if strings.Contains(infoLog, "Cache lookup query") {
		t.Errorf("DEBUG log should have been suppressed, found in output: %s", infoLog)
	}
	if !strings.Contains(infoLog, "[INFO]") || !strings.Contains(infoLog, "Incoming HTTP request") {
		t.Errorf("expected formatted INFO log line, got: %s", infoLog)
	}
	infoClean := StripANSI(infoLog)
	if !strings.Contains(infoClean, "path=/api/v1/users") || !strings.Contains(infoClean, "status=200") {
		t.Errorf("expected key-value attributes in single-line JSON output, got: %s", infoLog)
	}

	// Check 2: Error header
	errorHeader := renderedOutput[1]
	if !strings.Contains(errorHeader, "TypeError: Cannot read properties of undefined") {
		t.Errorf("expected error header in trace, got: %s", errorHeader)
	}

	// Check 3: Stack frames (dimmed)
	if !strings.Contains(renderedOutput[2], "at getUserById") {
		t.Errorf("expected first frame, got: %s", renderedOutput[2])
	}
	if !strings.Contains(renderedOutput[3], "at handleRequest") {
		t.Errorf("expected second frame, got: %s", renderedOutput[3])
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

	if len(entries) != 7 {
		t.Fatalf("expected 7 entries streamed in real-time, got %d", len(entries))
	}

	// Verify header through trailing exception inherit SeverityError
	for i := 0; i <= 5; i++ {
		if entries[i].Level != SeverityError {
			t.Errorf("expected entries[%d] to have SeverityError, got %v", i, entries[i].Level)
		}
	}

	// Final recovered line is plain
	recovered := entries[6]
	if recovered.IsTrace || recovered.IsFrame {
		t.Errorf("expected recovered worker line to not be trace or frame: %+v", recovered)
	}
	if recovered.Text() != "Worker process 42 recovered" {
		t.Errorf("unexpected normal entry: %v", recovered)
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
		t.Errorf("expected ANSI color codes in 200 OK HTTP access log rendering")
	}

	// 5xx status -> Red
	entry500 := &Entry{
		Lines: []string{`127.0.0.1 - - [10/Oct/2026:13:00:00] "POST /api/v1/checkout HTTP/1.1" 500 230`},
		Level: SeverityError,
	}
	rendered500 := renderer.Render(entry500)
	if !HasANSI(rendered500) {
		t.Errorf("expected ANSI color codes in 500 ERROR HTTP access log rendering")
	}
}
