package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMedian(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{
		{nil, 0},
		{[]float64{4}, 4},
		{[]float64{3, 1, 2}, 2},
		{[]float64{4, 1, 3, 2}, 2.5},
		{[]float64{5, 5, 5, 5, 100}, 5},
	}
	for _, tc := range cases {
		if got := median(tc.in); !approx(got, tc.want) {
			t.Errorf("median(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	in := []float64{3, 1, 2}
	median(in)
	if in[0] != 3 {
		t.Error("median reordered its input")
	}
}

func TestSpreadPct(t *testing.T) {
	if got := spreadPct([]float64{10, 10, 10}); got != 0 {
		t.Errorf("equal runs have no spread, got %v", got)
	}
	// median 10, range 9 to 12: 3 / 10
	if got := spreadPct([]float64{9, 10, 12}); !approx(got, 30) {
		t.Errorf("spread = %v, want 30", got)
	}
	if got := spreadPct([]float64{7}); got != 0 {
		t.Errorf("one run has no spread, got %v", got)
	}
	if got := spreadPct([]float64{0, 0}); got != 0 {
		t.Errorf("a zero median must not divide by zero, got %v", got)
	}
}

func runs(median, p99, rate float64, offered float64, n int) []run {
	out := make([]run, n)
	for i := range out {
		out[i] = run{MedianMs: median, P99Ms: p99, AchievedRate: rate, OfferedRate: offered, Mode: "open"}
	}
	return out
}

func TestCompare_IncreaseAndRate(t *testing.T) {
	l := &load{Name: "rate-1000", Runs: map[string][]run{
		condBase: runs(1.0, 2.0, 1000, 1000, 5),
		condSpec: runs(1.5, 6.0, 990, 1000, 5),
	}}
	r := compare(l, 10)
	if !approx(r.IncreasePct, 50) {
		t.Errorf("increase = %v, want 50", r.IncreasePct)
	}
	if !approx(r.RateKept, 99) {
		t.Errorf("rate kept = %v, want 99", r.RateKept)
	}
	if r.Noisy {
		t.Error("identical runs are not noisy")
	}
	if r.Runs != 5 {
		t.Errorf("runs = %d", r.Runs)
	}
}

func TestCompare_NoisyBaselineIsFlagged(t *testing.T) {
	base := runs(1.0, 2.0, 1000, 1000, 5)
	base[0].MedianMs, base[1].MedianMs = 0.7, 1.4 // range 0.7 to 1.4 around a median of 1.0 is 70%
	l := &load{Name: "rate-1000", Runs: map[string][]run{condBase: base, condSpec: runs(1.05, 2, 1000, 1000, 5)}}
	r := compare(l, 10)
	if !r.Noisy {
		t.Fatal("a baseline that spread 70% should be flagged as noisy")
	}
	if got := verdict(r, settings{TargetPct: 10, NoisePct: 10, MinRate: 99}); !strings.Contains(got, "cannot judge") {
		t.Errorf("a noisy machine must not give a verdict, got %q", got)
	}
}

// A noisy baseline hides small differences, not large ones.
func TestVerdict_NoiseDoesNotHideALargeIncrease(t *testing.T) {
	s := settings{TargetPct: 10, NoisePct: 10, MinRate: 99}
	noisy := func(increase float64) row {
		return row{Mode: "open", Offered: 100, IncreasePct: increase, RateKept: 100, Noisy: true,
			BaseMedian: stat{Spread: 11}, BaseRate: stat{Spread: 0}}
	}
	if got := verdict(noisy(153), s); !strings.Contains(got, "NOT MET") {
		t.Errorf("+153%% with an 11%% spread is not noise, got %q", got)
	}
	if got := verdict(noisy(15), s); !strings.Contains(got, "cannot judge") {
		t.Errorf("+15%% with an 11%% spread could be noise, got %q", got)
	}
}

func TestVerdict(t *testing.T) {
	s := settings{TargetPct: 10, NoisePct: 10, MinRate: 99}
	mk := func(increase, kept float64) row {
		return row{Mode: "open", Offered: 1000, IncreasePct: increase, RateKept: kept}
	}
	cases := []struct {
		name string
		r    row
		want string
	}{
		{"met", mk(4, 99.9), "MET"},
		{"too slow", mk(60, 99.9), "NOT MET (median latency +60%, limit 10%)"},
		{"rate fell", mk(4, 90), "NOT MET (only 90.0% of the offered rate"},
		{"both", mk(60, 90), "NOT MET (median latency +60%, limit 10%; only 90.0%"},
		{"exactly at the limit is not under it", mk(10, 100), "NOT MET"},
	}
	for _, tc := range cases {
		if got := verdict(tc.r, s); !strings.Contains(got, tc.want) {
			t.Errorf("%s: verdict %q does not contain %q", tc.name, got, tc.want)
		}
	}
	if got := verdict(row{Mode: "closed"}, s); !strings.Contains(got, "not judged") {
		t.Errorf("closed loop should not be judged, got %q", got)
	}
}

func TestCompare_MissingConditionIsReported(t *testing.T) {
	l := &load{Name: "rate-100", Runs: map[string][]run{condBase: runs(1, 2, 100, 100, 3)}}
	r := compare(l, 10)
	if len(r.Warnings) == 0 || r.Runs != 0 {
		t.Errorf("expected a warning and no runs, got %+v", r)
	}
}

func TestReadRunsAndRender(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 3; i++ {
		write(fmt.Sprintf("rate-1000.base.%d.json", i),
			`{"mode":"open","offered_rate":1000,"achieved_rate":1000,"requests":10000,"sent":12000,"median_ms":1.0,"p99_ms":2.0}`)
		write(fmt.Sprintf("rate-1000.spec.%d.json", i),
			`{"mode":"open","offered_rate":1000,"achieved_rate":995,"requests":10000,"sent":12000,"median_ms":1.5,"p99_ms":3.0}`)
		write(fmt.Sprintf("rate-1000.spec.%d.kv", i), "specter_cpu_pct=4.5\nstrace_cpu_pct=20\ncaptured_lines=11990\n")
		write(fmt.Sprintf("rate-100.base.%d.json", i),
			`{"mode":"open","offered_rate":100,"achieved_rate":100,"requests":1000,"sent":1200,"median_ms":0.8,"p99_ms":1.5}`)
		write(fmt.Sprintf("rate-100.spec.%d.json", i),
			`{"mode":"open","offered_rate":100,"achieved_rate":100,"requests":1000,"sent":1200,"median_ms":0.9,"p99_ms":1.6}`)
		write(fmt.Sprintf("closed.base.%d.json", i), `{"mode":"closed","achieved_rate":9000,"median_ms":3.5,"p99_ms":9}`)
		write(fmt.Sprintf("closed.spec.%d.json", i), `{"mode":"closed","achieved_rate":3000,"median_ms":10,"p99_ms":30}`)
	}
	write("notes.txt", "not a run")
	write("other.json", `{}`) // too few parts in the name: ignored

	loads, err := readRuns(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range loads {
		names = append(names, l.Name)
	}
	if got := strings.Join(names, ","); got != "rate-100,rate-1000,closed" {
		t.Fatalf("loads in order = %q, want rate-100,rate-1000,closed", got)
	}

	s := settings{TargetPct: 10, NoisePct: 10, MinRate: 99}
	rows := make([]row, len(loads))
	for i, l := range loads {
		rows[i] = compare(l, s.NoisePct)
	}

	var text bytes.Buffer
	renderText(&text, rows, "test machine", s)
	out := text.String()
	for _, want := range []string{
		"Machine: test machine",
		"Load: 1000 requests per second, 3 runs each",
		"+50%",
		"Specter 4.5%, strace 20.0%",
		"lines captured   35970 of 36000 (99.92%)",
		"NOT MET (median latency +50%, limit 10%)",
		"Load: closed loop",
		"not judged",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text report is missing %q.\n%s", want, out)
		}
	}
	// 100 requests per second: +12.5% is above the limit too, and CPU data is absent there
	if !strings.Contains(out, "Load: 100 requests per second") {
		t.Errorf("missing the 100/s load.\n%s", out)
	}

	var md bytes.Buffer
	renderMarkdown(&md, rows, "", s)
	if !strings.Contains(md.String(), "| 1000 requests/s |") || !strings.Contains(md.String(), "| closed loop (peak) |") {
		t.Errorf("markdown table is missing rows.\n%s", md.String())
	}
	if strings.Count(md.String(), "\n|") < 4 {
		t.Errorf("markdown table looks too short.\n%s", md.String())
	}
}

func TestReadRunsRejectsBadJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rate-10.base.1.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRuns(dir); err == nil {
		t.Error("a file that is not JSON should be an error, not silently skipped")
	}
}

func TestRenderNamesTheReader(t *testing.T) {
	r := row{Name: "rate-1000", Runs: 5, Mode: "open", Offered: 1000, HasKV: true, SpecterCPU: 3.2, Captured: 9, Sent: 10,
		BaseMedian: stat{Median: 1}, SpecMedian: stat{Median: 1.05}, BaseRate: stat{Median: 1000}, SpecRate: stat{Median: 1000}, RateKept: 100, IncreasePct: 5}
	s := settings{TargetPct: 10, NoisePct: 10, MinRate: 99, Reader: "bpftrace"}

	var text, md bytes.Buffer
	renderText(&text, []row{r}, "", s)
	renderMarkdown(&md, []row{r}, "", s)

	if !strings.Contains(text.String(), "CPU use          bpftrace 3.2%") || strings.Contains(text.String(), "strace") {
		t.Errorf("the text report should name bpftrace and not mention strace.\n%s", text.String())
	}
	if !strings.Contains(md.String(), "without bpftrace") || !strings.Contains(md.String(), "bpftrace CPU") {
		t.Errorf("the table should name bpftrace.\n%s", md.String())
	}
	if (settings{}).reader() != "Specter" || (settings{Reader: "specter"}).reader() != "Specter" {
		t.Error("the default reader is Specter")
	}
}
