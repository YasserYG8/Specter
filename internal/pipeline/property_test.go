package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProperty_LosslessRendering verifies that for any input line <= 64 KB,
// stripped rendered output strictly equals the original raw line.
func TestProperty_LosslessRendering(t *testing.T) {
	// Text and unclassified lines in standard mode
	unclassifiedLines := []string{
		"Simple plain text log line",
		"2026-10-18T12:00:00Z [INFO] Service started on :8080",
		"GET /api/v1/users 200 12.34 ms - 1024",
		"POST /checkout 500 45ms",
		"    at com.example.service.OrderProcessor.process(OrderProcessor.java:54)",
		"	/app/server/worker.go:42 +0x25",
		"SELECT id, name FROM users WHERE active = 1;",
		"#1 download assets from CDN",
		"took 404 ms to complete operation",
		"\x1b[32mPre-colored green log text\x1b[0m",
		"Unicode test: 🚀 Server running in 日本語 with symbols & < > \" '",
	}

	renderer := NewRenderer(RendererOptions{NoColor: false})

	for _, line := range unclassifiedLines {
		entry := Entry{
			Lines: []string{line},
			Safe:  line,
			Raw:   line,
			Level: DetectSeverity(line),
		}
		rendered := renderer.Render(&entry)
		stripped := StripANSI(rendered)

		expected := StripANSI(line)
		if stripped != expected {
			t.Errorf("Lossless violation for line %q: got %q, want %q", line, stripped, expected)
		}
	}

	// In ForceJSON or Raw mode, even structured JSON remains strictly byte-for-byte lossless
	jsonLine := `{"level":"error","msg":"database timeout","query_id":1234}`
	jsonRenderer := NewRenderer(RendererOptions{ForceJSON: true})
	entry := Entry{
		Lines: []string{jsonLine},
		Safe:  jsonLine,
		Raw:   jsonLine,
		Level: DetectSeverity(jsonLine),
	}
	renderedJSON := jsonRenderer.Render(&entry)
	if StripANSI(renderedJSON) != StripANSI(jsonLine) {
		t.Errorf("Lossless violation for JSON line in ForceJSON mode: got %q, want %q", StripANSI(renderedJSON), StripANSI(jsonLine))
	}
}

// TestProperty_TerminalSafety verifies non-SGR control codes (OSC, CSI, BEL) are sanitized.
func TestProperty_TerminalSafety(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		forbidden []string
	}{
		{
			name:      "OSC window title change",
			input:     "Start \x1b]0;MALICIOUS_TITLE\x07 End",
			forbidden: []string{"\x1b]0;", "MALICIOUS_TITLE"},
		},
		{
			name:      "CSI clear screen and cursor home",
			input:     "Start \x1b[2J\x1b[H End",
			forbidden: []string{"\x1b[2J", "\x1b[H"},
		},
		{
			name:      "Terminal bell",
			input:     "Beep alert \x07",
			forbidden: []string{"\x07"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sanitized := SanitizeTerminalOutput(tc.input)
			for _, f := range tc.forbidden {
				if strings.Contains(sanitized, f) {
					t.Errorf("Sanitized output still contains dangerous sequence %q: got %q", f, sanitized)
				}
			}
		})
	}
}

// TestProperty_LineLengthCap verifies lines exceeding 64 KB are truncated with marker.
func TestProperty_LineLengthCap(t *testing.T) {
	longPayload := strings.Repeat("A", 70000)
	capped := CapLineLength(longPayload, MaxLineLength, TruncationMarker)

	if len(capped) > MaxLineLength+len(TruncationMarker) {
		t.Errorf("Capped line length exceeds limit: %d", len(capped))
	}
	if !strings.HasSuffix(capped, TruncationMarker) {
		t.Errorf("Capped line does not end with truncation marker: %q", capped[len(capped)-40:])
	}
}

// TestFixtures_RealWorldTraces verifies state machine and classification on real fixture files.
func TestFixtures_RealWorldTraces(t *testing.T) {
	fixturesDir := filepath.Join("..", "..", "test", "fixtures")

	t.Run("Python Chained Exceptions", func(t *testing.T) {
		content, err := os.ReadFile(filepath.Join(fixturesDir, "python_chained.log"))
		if err != nil {
			t.Fatalf("Failed to read fixture: %v", err)
		}

		lines := strings.Split(string(content), "\n")
		grouper := NewStackTraceGrouper(DefaultTraceIdleTimeout)

		inCh := make(chan Line, len(lines))
		for _, l := range lines {
			inCh <- Line{Text: l, PID: 100, FD: 2}
		}
		close(inCh)

		outCh := grouper.Group(context.Background(), inCh)
		var entries []Entry
		for e := range outCh {
			entries = append(entries, e)
		}

		// Verify Python frames are detected and styled
		foundFrame := false
		for _, e := range entries {
			if e.IsFrame {
				foundFrame = true
				break
			}
		}
		if !foundFrame {
			t.Error("Expected Python frames to be marked with IsFrame = true")
		}
	})

	t.Run("Go Panic with Goroutines", func(t *testing.T) {
		content, err := os.ReadFile(filepath.Join(fixturesDir, "go_panic.log"))
		if err != nil {
			t.Fatalf("Failed to read fixture: %v", err)
		}

		lines := strings.Split(string(content), "\n")
		grouper := NewStackTraceGrouper(DefaultTraceIdleTimeout)

		inCh := make(chan Line, len(lines))
		for _, l := range lines {
			inCh <- Line{Text: l, PID: 101, FD: 2}
		}
		close(inCh)

		outCh := grouper.Group(context.Background(), inCh)
		var entries []Entry
		for e := range outCh {
			entries = append(entries, e)
		}

		foundGoFrame := false
		for _, e := range entries {
			if e.IsFrame && strings.Contains(e.Text(), ".go:") {
				foundGoFrame = true
				break
			}
		}
		if !foundGoFrame {
			t.Error("Expected Go goroutine file:line lines to be marked with IsFrame = true")
		}
	})

	t.Run("Java Caused By", func(t *testing.T) {
		content, err := os.ReadFile(filepath.Join(fixturesDir, "java_caused_by.log"))
		if err != nil {
			t.Fatalf("Failed to read fixture: %v", err)
		}

		lines := strings.Split(string(content), "\n")
		grouper := NewStackTraceGrouper(DefaultTraceIdleTimeout)

		inCh := make(chan Line, len(lines))
		for _, l := range lines {
			inCh <- Line{Text: l, PID: 102, FD: 2}
		}
		close(inCh)

		outCh := grouper.Group(context.Background(), inCh)
		var entries []Entry
		for e := range outCh {
			entries = append(entries, e)
		}

		foundCausedBy := false
		for _, e := range entries {
			if strings.Contains(e.Text(), "Caused by:") {
				foundCausedBy = true
				if e.Level != SeverityError {
					t.Errorf("Expected Caused by line to inherit SeverityError, got %v", e.Level)
				}
			}
		}
		if !foundCausedBy {
			t.Error("Expected to find Caused by entry in Java trace")
		}
	})

	t.Run("HTTP Access Logs", func(t *testing.T) {
		content, err := os.ReadFile(filepath.Join(fixturesDir, "http_access.log"))
		if err != nil {
			t.Fatalf("Failed to read fixture: %v", err)
		}

		lines := strings.Split(string(content), "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			code, ok := DetectHTTPStatus(l)
			if !ok {
				t.Errorf("Failed to detect HTTP status in line: %q", l)
				continue
			}
			lvl := DetectSeverity(l)
			if code >= 500 && lvl != SeverityError {
				t.Errorf("Expected SeverityError for 5xx line %q, got %v", l, lvl)
			}
			if code >= 400 && code < 500 && lvl != SeverityWarn {
				t.Errorf("Expected SeverityWarn for 4xx line %q, got %v", l, lvl)
			}
			if code >= 200 && code < 400 && lvl != SeverityInfo {
				t.Errorf("Expected SeverityInfo for 2xx/3xx line %q, got %v", l, lvl)
			}
		}
	})

	t.Run("Edge Cases Guardrails", func(t *testing.T) {
		// 1. False positive numbers must NOT trigger HTTP severity
		falsePositives := []string{
			"Query returned 200 users from regional cache",
			"Operation took 404 ms to complete database handshake",
			"Warning: memory limit at 500 MB threshold",
		}
		for _, fp := range falsePositives {
			if _, ok := DetectHTTPStatus(fp); ok {
				t.Errorf("False positive HTTP status detected in non-HTTP line: %q", fp)
			}
		}

		// 2. Indented SQL lines must NOT be marked as stack frames
		sqlLines := []string{
			"SELECT users.id, users.email",
			"  FROM users",
			" WHERE users.status = 'active'",
		}
		grouper := NewStackTraceGrouper(DefaultTraceIdleTimeout)
		inCh := make(chan Line, len(sqlLines))
		for _, l := range sqlLines {
			inCh <- Line{Text: l, PID: 103, FD: 1}
		}
		close(inCh)
		for e := range grouper.Group(context.Background(), inCh) {
			if e.IsFrame {
				t.Errorf("SQL line erroneously classified as stack frame: %q", e.Text())
			}
		}

		// 3. Numbered build list must NOT be marked as stack frame
		buildSteps := []string{
			"Step summary:",
			"#1 download assets from CDN",
			"#2 compile typescript sources",
		}
		inCh2 := make(chan Line, len(buildSteps))
		for _, l := range buildSteps {
			inCh2 <- Line{Text: l, PID: 104, FD: 1}
		}
		close(inCh2)
		for e := range grouper.Group(context.Background(), inCh2) {
			if e.IsFrame {
				t.Errorf("Build step erroneously classified as stack frame: %q", e.Text())
			}
		}
	})
}
