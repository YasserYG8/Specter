package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTagger_PlainTagsArePaddedToTheLongestLabel(t *testing.T) {
	tg := newTagger([]string{":3000", "web", "pid:1234"}, false)
	for i, want := range []string{":3000    | ", "web      | ", "pid:1234 | "} {
		if tg.prefixes[i] != want {
			t.Errorf("prefix %d = %q, want %q", i, tg.prefixes[i], want)
		}
	}
	if strings.Contains(tg.prefixes[0], "\x1b") {
		t.Error("a plain tag must have no escape codes, so grep works on piped output")
	}
}

func TestTagger_ColorGoesOnTheLabelOnly(t *testing.T) {
	tg := newTagger([]string{":3000", "web"}, true)
	p := tg.prefixes[1]
	if !strings.Contains(p, "\x1b[") {
		t.Fatalf("a colored tag should have escape codes, got %q", p)
	}
	if !strings.HasSuffix(p, "   | ") {
		t.Errorf("the padding and the bar must come after the colored label, got %q", p)
	}
	if tg.prefixes[0] == tg.prefixes[1] {
		t.Error("every target should have its own color")
	}
}

// Red and yellow mean error and warning in the output, so no tag may use them.
func TestTagColors_NeverRedOrYellow(t *testing.T) {
	for i := range tagColors {
		got := colorLabel(i, "x", true)
		for _, bad := range []string{"\x1b[31m", "\x1b[33m", "\x1b[91m", "\x1b[93m"} {
			if strings.Contains(got, bad) {
				t.Errorf("target %d uses %q", i, bad)
			}
		}
	}
	// More targets than colors reuse the colors
	if colorLabel(0, "x", true) != colorLabel(len(tagColors), "x", true) {
		t.Error("colors should repeat after the last one")
	}
}

func TestTagger_EveryPhysicalLineIsTagged(t *testing.T) {
	tg := newTagger([]string{"a", "bb"}, false)
	got := tg.tag(0, "one\ntwo\n\nfour")
	want := "a  | one\na  | two\na  | \na  | four"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutputWriter_OneTargetWritesWithoutTags(t *testing.T) {
	var buf bytes.Buffer
	ow := &outputWriter{w: &buf}
	ow.write(0, "hello", false)
	ow.write(0, "Continue? ", true)
	if got, want := buf.String(), "hello\nContinue? "; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutputWriter_PartialRowIsClosedWhenAnotherTargetWrites(t *testing.T) {
	var buf bytes.Buffer
	ow := &outputWriter{w: &buf, tag: newTagger([]string{":3000", ":4000"}, false)}
	ow.write(0, "Continue? ", true)
	ow.write(1, "ready", false)
	if got, want := buf.String(), ":3000 | Continue? \n:4000 | ready\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutputWriter_SameTargetContinuesItsRowWithoutANewTag(t *testing.T) {
	var buf bytes.Buffer
	ow := &outputWriter{w: &buf, tag: newTagger([]string{":3000", ":4000"}, false)}
	ow.write(0, "Continue? ", true)
	ow.write(0, "yes", false)
	ow.write(1, "ready", false)
	if got, want := buf.String(), ":3000 | Continue? yes\n:4000 | ready\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutputWriter_ContinuedMultiLineTextTagsTheLinesAfterTheFirst(t *testing.T) {
	var buf bytes.Buffer
	ow := &outputWriter{w: &buf, tag: newTagger([]string{"a", "b"}, false)}
	ow.write(0, "start ", true)
	ow.write(0, "end\nnext", false)
	if got, want := buf.String(), "a | start end\na | next\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutputWriter_TwoPartialRowsOfDifferentTargets(t *testing.T) {
	var buf bytes.Buffer
	ow := &outputWriter{w: &buf, tag: newTagger([]string{"a", "b"}, false)}
	ow.write(0, "one ", true)
	ow.write(1, "two ", true)
	ow.write(0, "three", false) // a's row was closed, so this is a new row with a new tag
	if got, want := buf.String(), "a | one \nb | two \na | three\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPrefixWriter_PrefixesNonEmptyLinesOnly(t *testing.T) {
	var buf bytes.Buffer
	w := &prefixWriter{w: &buf, prefix: ":3000 "}
	n, err := w.Write([]byte("\nhello\nworld\n"))
	if err != nil || n != len("\nhello\nworld\n") {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if got, want := buf.String(), "\n:3000 hello\n:3000 world\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The merger holds the other targets while one stack trace is open, and keeps arrival order otherwise.
func TestMerger_HoldsOtherTargetsDuringATrace(t *testing.T) {
	var order []string
	m := newMerger(2, func(ev sessionEvent) {
		if ev.end != nil {
			order = append(order, "end")
			return
		}
		order = append(order, ev.entry.Text())
	})
	line := func(idx int, text string, trace, endsTrace bool) sessionEvent {
		e := sessionEvent{idx: idx}
		e.entry.Lines = []string{text}
		e.entry.IsTrace, e.entry.EndsTrace = trace, endsTrace
		return e
	}

	m.accept(line(0, "a1", false, false))
	m.accept(line(1, "trace-head", true, false)) // opens the trace of target 1
	m.accept(line(0, "a2", false, false))        // waits
	m.accept(line(1, "trace-frame", true, false))
	m.accept(line(0, "a3", false, false))      // waits
	m.accept(line(1, "trace-end", true, true)) // closes the trace; a2 and a3 follow
	m.accept(line(0, "a4", false, false))

	want := "a1,trace-head,trace-frame,trace-end,a2,a3,a4"
	if got := strings.Join(order, ","); got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
}

func TestMerger_AQuietTraceIsLetGoAfterTheHold(t *testing.T) {
	var order []string
	m := newMerger(2, func(ev sessionEvent) { order = append(order, ev.entry.Text()) })
	head := sessionEvent{idx: 1}
	head.entry.Lines, head.entry.IsTrace = []string{"java-trace"}, true // a trace with no closing line
	other := sessionEvent{idx: 0}
	other.entry.Lines = []string{"a"}

	m.accept(head)
	m.accept(other)
	if len(order) != 1 {
		t.Fatalf("the other target should wait, got %v", order)
	}
	<-m.wait() // returns after traceHold
	m.expire()
	if got := strings.Join(order, ","); got != "java-trace,a" {
		t.Errorf("order = %s, want java-trace,a", got)
	}
}

func TestMerger_OneSessionIsNeverHeld(t *testing.T) {
	n := 0
	m := newMerger(1, func(sessionEvent) { n++ })
	e := sessionEvent{idx: 0}
	e.entry.IsTrace = true
	m.accept(e)
	if m.wait() != nil || n != 1 {
		t.Error("a single session must be handled at once, with no trace to wait for")
	}
}
