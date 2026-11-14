package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
)

// groupLines runs lines through the grouper, as one stream, and returns the entries it emits.
func groupLines(t testing.TB, lines ...string) []Entry {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	in := make(chan Line, len(lines))
	out := NewStackTraceGrouper(0).Group(ctx, in)
	ts := time.Now()
	for _, l := range lines {
		in <- Line{Text: l, FD: 1, PID: 7, TS: ts}
	}
	close(in)

	var entries []Entry
	for e := range out {
		entries = append(entries, e)
	}
	return entries
}

// withoutCarriedJSON returns the entry as the renderer would see one built outside the grouper.
func withoutCarriedJSON(e Entry) Entry {
	e.json = nil
	e.jsonKnown = false
	return e
}

var renderOptionSets = []RendererOptions{
	{NoColor: true},
	{},
	{Raw: true},
	{ForceJSON: true},
	{ForceJSON: true, NoColor: true},
}

// checkRenderEquivalence is the check behind the render fuzz test. It runs the lines through the grouper and, for
// every entry, compares what the grouper and the renderer did with what the full path does: the record decision
// against refIsRecordStart, and the rendered text with the carried JSON result against the text rendered when
// the renderer decodes the line itself.
func checkRenderEquivalence(t testing.TB, lines ...string) {
	t.Helper()

	// Colors are on only when the output is a terminal, so turn them on to compare the colored paths as well.
	saved := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = saved }()

	entries := groupLines(t, lines...)
	if len(entries) != len(lines) {
		t.Fatalf("%d lines gave %d entries", len(lines), len(entries))
	}

	for _, e := range entries {
		for _, opts := range renderOptionSets {
			r := NewRenderer(opts)
			got := r.Render(&e)
			ref := withoutCarriedJSON(e)
			want := r.Render(&ref)
			if got != want {
				t.Fatalf("lines %q, entry %q, options %+v:\nrendered %q\nwithout the carried result %q", lines, e.Text(), opts, got, want)
			}
		}
	}

	// The first line has no record before it, so its entry follows the record decision directly.
	first := entries[0]
	if first.IsPartial || first.IsRecordContinuation || first.IsFrame {
		return
	}
	clean := StripANSI(SanitizeTerminalOutput(capCapturedLine(lines[0])))
	isStart, lvl, inTrace, _ := refIsRecordStart(clean)
	wantLevel, wantTrace, wantJSON := DetectSeverity(clean), false, false
	if isStart {
		wantLevel, wantTrace, wantJSON = lvl, inTrace, ParseJSONLog(clean) != nil
	}
	if first.Level != wantLevel || first.IsTrace != wantTrace || first.IsJSON != wantJSON {
		t.Fatalf("line %q: entry has level %v, trace %v, json %v; the full path gives %v, %v, %v",
			lines[0], first.Level, first.IsTrace, first.IsJSON, wantLevel, wantTrace, wantJSON)
	}
}

var renderSeeds = []string{
	`{"level":"info","msg":"request processed","path":"/json","status":200,"ts":1731585000123456789}`,
	`{"level":50,"msg":"boom","time":1731585000123}`,
	`{"level":"error","msg":"db timeout","query_id":1234}`,
	"\x1b[32m" + `{"level":"warn","msg":"colored json"}` + "\x1b[0m",
	`{"msg":"goroutine 1 [running]","level":"debug"}`,
	`{"msg":"Traceback (most recent call last):"}`,
	`{ invalid json }`,
	`{}`,
	"{\"a\":\"\x1b[3\x1b[0m1m\"}",
	"\x1b[3\x1b[1L1m" + `{"level":"info","msg":"strip builds a new sequence"}`,
	"\x1b[3\x1b[1L1m{\"level\":\"info\"}\x1b[0m",
	"\x1b]0;title\x07" + `{"level":"info","msg":"after a title"}`,
	"ding\x07" + `{"level":"info","msg":"bell"}`,
	"Traceback (most recent call last):",
	"panic: runtime error: index out of range",
	"goroutine 1 [running]:",
	`2026-11-14 10:12:50,516 - app - INFO - Task finished`,
	`127.0.0.1 - - [14/Nov/2026:10:12:50 +0000] "GET /api/users HTTP/1.1" 500 1532`,
	"Error: Connection lost",
	"    at Socket.onClose (/app/net.js:42:15)",
	"plain text",
	"",
	"  {\"level\":\"info\"}  ",
}

func FuzzRenderCarriedJSON(f *testing.F) {
	for _, s := range renderSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		checkRenderEquivalence(t, line)
		// The same line after a trace header, where it is read as a continuation or a new record by the rules.
		checkRenderEquivalence(t, "Traceback (most recent call last):", line)
	})
}

// TestRenderCarriedJSON_Seeds runs the seeds in a normal `go test`, where the fuzz function only replays them.
func TestRenderCarriedJSON_Seeds(t *testing.T) {
	for _, s := range renderSeeds {
		checkRenderEquivalence(t, s)
		checkRenderEquivalence(t, "Traceback (most recent call last):", s)
		checkRenderEquivalence(t, "plain text", s, s)
	}
}

func TestGrouper_PinoNumericLevelKeepsSeverityFromDetectSeverity(t *testing.T) {
	line := `{"level":50,"msg":"boom","time":1731585000123}`
	entries := groupLines(t, line)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if want := DetectSeverity(line); e.Level != want {
		t.Errorf("severity is %v, want %v, the one DetectSeverity gives", e.Level, want)
	}
	if !e.IsJSON {
		t.Error("a JSON line is not marked IsJSON")
	}
	// The JSON level still shows on the rendered line: the badge comes from the decoded line, not from e.Level.
	saved := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = saved }()
	if got := NewRenderer(RendererOptions{}).Render(&e); !strings.Contains(got, "[ERROR]") {
		t.Errorf("rendered %q, want the [ERROR] badge from the numeric level", got)
	}
}

func TestGrouper_CarriesTheJSONDecision(t *testing.T) {
	entries := groupLines(t,
		`{"level":"info","msg":"a json line"}`,
		"a plain line",
		// SanitizeTerminalOutput keeps \x1b[1L, StripANSI removes it, and what is left (\x1b[31m) is a new sequence.
		"\x1b[3\x1b[1L1m"+`{"level":"info"}`,
	)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if !entries[0].jsonKnown || entries[0].json == nil {
		t.Error("a JSON line carries no decoded result")
	}
	if !entries[1].jsonKnown || entries[1].json != nil {
		t.Error("a plain line should carry the decision that it is not JSON")
	}
	// A line that still has an escape byte after one strip is left to the renderer's own decode.
	if entries[2].jsonKnown {
		t.Error("a line that still has an escape byte after one strip carries a result")
	}
}

// TestRender_EntryWithoutCarriedResultDecodesItself covers an Entry built outside the grouper.
func TestRender_EntryWithoutCarriedResultDecodesItself(t *testing.T) {
	e := &Entry{Lines: []string{`{"level":"error","msg":"fail","code":7}`}}
	got := NewRenderer(RendererOptions{NoColor: true}).Render(e)
	if got != "[ERROR] fail code=7" {
		t.Errorf("got %q", got)
	}
}
