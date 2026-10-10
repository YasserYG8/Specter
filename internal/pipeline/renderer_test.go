package pipeline

import (
	"strings"
	"testing"
)

func TestRenderer_RawMode(t *testing.T) {
	renderer := NewRenderer(RendererOptions{Raw: true})
	entry := &Entry{Lines: []string{`{"level":"error","msg":"fail"}`}}

	output := renderer.Render(entry)
	if output != `{"level":"error","msg":"fail"}` {
		t.Errorf("expected raw output, got %q", output)
	}
}

func TestRenderer_JSONMode(t *testing.T) {
	renderer := NewRenderer(RendererOptions{ForceJSON: true})
	rawJSON := `{"level":"info","msg":"connected"}`
	entry := &Entry{Lines: []string{rawJSON}}

	output := renderer.Render(entry)
	if output != rawJSON {
		t.Errorf("expected raw JSON when ForceJSON=true, got %q", output)
	}

	// Normal JSON pretty printing
	prettyRenderer := NewRenderer(RendererOptions{NoColor: true})
	output = prettyRenderer.Render(entry)
	if !strings.Contains(output, "[INFO] connected") {
		t.Errorf("expected '[INFO] connected', got %q", output)
	}
}

func TestRenderer_StackTraceHighlight(t *testing.T) {
	renderer := NewRenderer(RendererOptions{NoColor: true})
	entry := &Entry{
		Lines: []string{
			"TypeError: undefined is not a function",
			"    at main.js:10:4",
		},
		IsTrace: true,
	}

	output := renderer.Render(entry)
	lines := strings.Split(output, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines in output, got %d", len(lines))
	}
	if lines[0] != "TypeError: undefined is not a function" {
		t.Errorf("unexpected first line: %q", lines[0])
	}
}
