package main

import (
	"io"
	"strings"

	"github.com/fatih/color"
)

// tagColors are the colors of the target tags, in the order targets get them. Red and yellow are left out on
// purpose: in the log output they mean error and warning.
var tagColors = []color.Attribute{
	color.FgCyan, color.FgMagenta, color.FgBlue, color.FgGreen, color.FgHiCyan, color.FgHiMagenta,
}

// colorLabel paints a target label with the color of target number i. With force it paints even when the global
// color switch is off, which is how the tests (and a decision made by the caller) get a color.
func colorLabel(i int, label string, force bool) string {
	c := color.New(tagColors[i%len(tagColors)])
	if force {
		c.EnableColor()
	}
	return c.Sprint(label)
}

// tagger writes the tag at the start of every line, when a run follows two or more targets.
type tagger struct {
	prefixes []string // one per target: label, padding, " | "
}

// newTagger pads every label to the width of the longest one. The color goes on the label only, and only when
// useColor is set, so a piped line is plain text that `grep ':4000 |'` finds.
func newTagger(labels []string, useColor bool) *tagger {
	width := 0
	for _, l := range labels {
		width = max(width, len([]rune(l)))
	}
	t := &tagger{prefixes: make([]string, len(labels))}
	for i, l := range labels {
		pad := strings.Repeat(" ", width-len([]rune(l)))
		if useColor {
			t.prefixes[i] = colorLabel(i, l, true) + pad + " | "
		} else {
			t.prefixes[i] = l + pad + " | "
		}
	}
	return t
}

// tag puts target i's tag at the start of every physical line of text.
func (t *tagger) tag(i int, text string) string {
	return t.prefixes[i] + strings.ReplaceAll(text, "\n", "\n"+t.prefixes[i])
}

// continueRow tags text that continues a row the same target already started: the first line is left alone, and
// the lines after it get the tag.
func (t *tagger) continueRow(i int, text string) string {
	return strings.ReplaceAll(text, "\n", "\n"+t.prefixes[i])
}

// outputWriter writes rendered entries to stdout. With a tagger it also keeps lines of different targets apart.
type outputWriter struct {
	w   io.Writer
	tag *tagger // nil with one target: nothing is tagged and the output is the same as ever

	// A partial line (a prompt with no line break) leaves its row open. rowOpen says there is one, and rowOwner
	// says whose it is.
	rowOpen  bool
	rowOwner int
}

// write prints one rendered entry of target i. A partial entry is printed without a line break, and a complete
// one with it.
//
// With a tagger: if the row is open and another target writes, the row is closed with a line break first. If the
// same target continues the row, its text follows on the same row, without a new tag.
func (o *outputWriter) write(i int, rendered string, partial bool) {
	if o.tag == nil {
		if partial {
			io.WriteString(o.w, rendered)
		} else {
			io.WriteString(o.w, rendered+"\n")
		}
		return
	}

	var sb strings.Builder
	continuing := o.rowOpen && o.rowOwner == i
	if o.rowOpen && !continuing {
		sb.WriteString("\n")
	}
	if continuing {
		sb.WriteString(o.tag.continueRow(i, rendered))
	} else {
		sb.WriteString(o.tag.tag(i, rendered))
	}
	if partial {
		o.rowOpen, o.rowOwner = true, i
	} else {
		sb.WriteString("\n")
		o.rowOpen = false
	}
	io.WriteString(o.w, sb.String()) // one write, so a tag and its text are never split
}

// prefixWriter puts a label at the start of every non-empty line written through it. A status message goes through
// one when several targets run, so it starts with the name of its target.
type prefixWriter struct {
	w      io.Writer
	prefix string
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	var sb strings.Builder
	for _, line := range strings.SplitAfter(string(b), "\n") {
		if strings.TrimRight(line, "\n") != "" {
			sb.WriteString(p.prefix)
		}
		sb.WriteString(line)
	}
	if _, err := io.WriteString(p.w, sb.String()); err != nil {
		return 0, err
	}
	return len(b), nil
}
