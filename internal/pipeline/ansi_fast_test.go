package pipeline

import (
	"strings"
	"testing"
)

// ansiSeeds are the starting inputs of the fuzz tests. They cover every way a pattern can start or end, so the
// byte check in front of the regular expressions is tried at its edges.
var ansiSeeds = []string{
	"",
	"plain text",
	`{"level":"info","msg":"request processed","status":200}`,
	"\x1b[31mred\x1b[0m",
	"\x1b[2J\x1b[H",
	"\x1b[?25l",
	"\x1b]0;title\x07",
	"\x1b]0;title\x1b\\",
	"\x07",
	"a\x07b",
	"\x1b",
	"a\x1bb",
	"\x1b[",
	"\x1b[31",
	"\x1b]",
	"\x1b]0;title",
	"\x1b]0;ti\x1btle\x07",
	"before \x1b[32mgreen\x1b[0m after \x1b]0;t\x07 end",
	strings.Repeat("abc \x1b[1m", 50),
	strings.Repeat("x", 4096),
	"caf\xc3\xa9 \xff\xfe broken utf8",
}

func FuzzStripANSI(f *testing.F) {
	for _, s := range ansiSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := StripANSI(s), refStripANSI(s); got != want {
			t.Fatalf("StripANSI(%q) = %q, want %q", s, got, want)
		}
	})
}

func FuzzSanitizeTerminalOutput(f *testing.F) {
	for _, s := range ansiSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := SanitizeTerminalOutput(s), refSanitize(s); got != want {
			t.Fatalf("SanitizeTerminalOutput(%q) = %q, want %q", s, got, want)
		}
	})
}

func FuzzHasANSI(f *testing.F) {
	for _, s := range ansiSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := HasANSI(s), refHasANSI(s); got != want {
			t.Fatalf("HasANSI(%q) = %v, want %v", s, got, want)
		}
	})
}

// TestANSIHelpers_SeedsMatchReference runs the seeds as plain tests, so the comparison also happens in a normal
// `go test` run, where the fuzz functions only replay their seeds.
func TestANSIHelpers_SeedsMatchReference(t *testing.T) {
	for _, s := range ansiSeeds {
		if got, want := StripANSI(s), refStripANSI(s); got != want {
			t.Errorf("StripANSI(%q) = %q, want %q", s, got, want)
		}
		if got, want := SanitizeTerminalOutput(s), refSanitize(s); got != want {
			t.Errorf("SanitizeTerminalOutput(%q) = %q, want %q", s, got, want)
		}
		if got, want := HasANSI(s), refHasANSI(s); got != want {
			t.Errorf("HasANSI(%q) = %v, want %v", s, got, want)
		}
	}
}

// TestANSIHelpers_NoAllocationWithoutEscapeBytes checks that a line with neither ESC nor BEL costs nothing: no
// regular expression runs, and no copy of the line is made.
func TestANSIHelpers_NoAllocationWithoutEscapeBytes(t *testing.T) {
	lines := []string{
		"plain text",
		`{"level":"info","msg":"request processed","path":"/json","status":200,"ts":1731585000123456789}`,
		`127.0.0.1 - - [14/Nov/2026:10:12:50 +0000] "GET /api/users HTTP/1.1" 200 1532`,
		"caf\xc3\xa9 \xff broken utf8",
		"",
	}
	for _, line := range lines {
		if n := testing.AllocsPerRun(100, func() { _ = StripANSI(line) }); n != 0 {
			t.Errorf("StripANSI(%q) allocated %v times, want 0", line, n)
		}
		if n := testing.AllocsPerRun(100, func() { _ = SanitizeTerminalOutput(line) }); n != 0 {
			t.Errorf("SanitizeTerminalOutput(%q) allocated %v times, want 0", line, n)
		}
		if n := testing.AllocsPerRun(100, func() { _ = HasANSI(line) }); n != 0 {
			t.Errorf("HasANSI(%q) allocated %v times, want 0", line, n)
		}
	}
}

// TestANSIHelpers_LoneBELIsStillRemoved guards the reason the byte check tests for BEL as well as ESC: a BEL with
// no ESC is removed today, and a check for ESC alone would let the terminal beep.
func TestANSIHelpers_LoneBELIsStillRemoved(t *testing.T) {
	if got := SanitizeTerminalOutput("ding\x07dong"); got != "dingdong" {
		t.Errorf("SanitizeTerminalOutput kept the BEL: %q", got)
	}
	if got := StripANSI("ding\x07dong"); got != "dingdong" {
		t.Errorf("StripANSI kept the BEL: %q", got)
	}
	if !HasANSI("ding\x07dong") {
		t.Error("HasANSI is false for a line with a BEL")
	}
}
