package pipeline

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// benchLines are the line types the per-line cost is measured on. The JSON line has the shape of the one the E2E
// test server writes. The colored line has escape codes, so it keeps today's cost and shows that nothing got worse.
var benchLines = []struct {
	name string
	text string
}{
	{"JSON", `{"level":"info","msg":"request processed","path":"/json","status":200,"ts":1731585000123456789}`},
	{"AccessLog", `127.0.0.1 - - [14/Nov/2026:10:12:50 +0000] "GET /api/users HTTP/1.1" 200 1532`},
	{"Plain", `Server listening on port 3000, ready to accept connections`},
	{"PythonLogging", `2026-11-14 10:12:50,516 - app.worker - INFO - Task finished in 0.42s`},
	{"Colored", "\x1b[32mGET /api/users 200 12.4 ms\x1b[0m"},
}

// BenchmarkLine pushes one kind of line at a time through Group and Render, which is the per-line work of the
// pipeline after the line assembler. Run it with -cpu 1 so the grouper and the renderer share one processor and
// ns/op is the CPU spent on a line, not the speed of the slower stage.
func BenchmarkLine(b *testing.B) {
	for _, bl := range benchLines {
		b.Run(bl.name, func(b *testing.B) {
			benchPipeline(b, bl.text, RendererOptions{NoColor: true})
		})
	}
}

// BenchmarkLineColor is BenchmarkLine with colors on, which adds the color work of the renderer.
func BenchmarkLineColor(b *testing.B) {
	for _, bl := range benchLines {
		b.Run(bl.name, func(b *testing.B) {
			benchPipeline(b, bl.text, RendererOptions{})
		})
	}
}

func benchPipeline(b *testing.B, text string, opts RendererOptions) {
	b.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan Line, 1024)
	entries := NewStackTraceGrouper(0).Group(ctx, in)
	renderer := NewRenderer(opts)
	ts := time.Now()

	go func() {
		defer close(in)
		for i := 0; i < b.N; i++ {
			select {
			case in <- Line{Text: text, FD: 1, PID: 100, TS: ts}:
			case <-ctx.Done():
				return
			}
		}
	}()

	b.ReportAllocs()
	b.ResetTimer()
	var sink int
	for e := range entries {
		sink += len(renderer.Render(&e))
	}
	b.StopTimer()
	runtime.KeepAlive(sink)
}

// BenchmarkANSIHelpers measures the three helpers on a line without escape codes, which is almost every line.
func BenchmarkANSIHelpers(b *testing.B) {
	line := `{"level":"info","msg":"request processed","path":"/json","status":200,"ts":1731585000123456789}`
	b.Run("StripANSI", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = StripANSI(line)
		}
	})
	b.Run("SanitizeTerminalOutput", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = SanitizeTerminalOutput(line)
		}
	})
	b.Run("HasANSI", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = HasANSI(line)
		}
	})
}
