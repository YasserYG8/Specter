package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// run is one load run: the loadgen result plus what the script measured around it.
type run struct {
	AchievedRate float64 `json:"achieved_rate"`
	OfferedRate  float64 `json:"offered_rate"`
	Requests     int     `json:"requests"`
	Errors       int     `json:"errors"`
	Sent         int     `json:"sent"`
	MedianMs     float64 `json:"median_ms"`
	P99Ms        float64 `json:"p99_ms"`
	Mode         string  `json:"mode"`

	// From the .kv file next to the JSON, only for runs with Specter attached
	SpecterCPU float64 // percent of one CPU
	StraceCPU  float64 // percent of one CPU
	Captured   int     // lines Specter printed
	hasKV      bool
}

// condition is "base" (no Specter) or "spec" (Specter attached).
const (
	condBase = "base"
	condSpec = "spec"
)

// load is every run of one load, by condition, in order.
type load struct {
	Name string
	Runs map[string][]run
}

// readRuns reads dir for files named <load>.<condition>.<n>.json, with an optional <load>.<condition>.<n>.kv of
// key=value lines. It returns the loads sorted by name.
func readRuns(dir string) ([]*load, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	byName := map[string]*load{}
	var names []string
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".json")
		parts := strings.Split(base, ".")
		if len(parts) < 3 {
			continue
		}
		cond := parts[len(parts)-2]
		name := strings.Join(parts[:len(parts)-2], ".")
		if cond != condBase && cond != condSpec {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var r run
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		if kv, err := os.ReadFile(strings.TrimSuffix(f, ".json") + ".kv"); err == nil {
			parseKV(string(kv), &r)
		}
		l := byName[name]
		if l == nil {
			l = &load{Name: name, Runs: map[string][]run{}}
			byName[name] = l
			names = append(names, name)
		}
		l.Runs[cond] = append(l.Runs[cond], r)
	}
	sort.Slice(names, func(i, j int) bool { return loadOrder(names[i]) < loadOrder(names[j]) })
	out := make([]*load, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n])
	}
	return out, nil
}

// loadOrder sorts "rate-100" before "rate-1000" before "closed" by the number in the name.
func loadOrder(name string) float64 {
	if n, ok := strings.CutPrefix(name, "rate-"); ok {
		if v, err := strconv.ParseFloat(n, 64); err == nil {
			return v
		}
	}
	return 1e12
}

func parseKV(text string, r *run) {
	for line := range strings.SplitSeq(text, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			continue
		}
		switch k {
		case "specter_cpu_pct":
			r.SpecterCPU, r.hasKV = f, true
		case "strace_cpu_pct":
			r.StraceCPU, r.hasKV = f, true
		case "captured_lines":
			r.Captured, r.hasKV = int(f), true
		}
	}
}

// median is the middle value, or the mean of the two middle ones.
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := append([]float64(nil), values...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// spreadPct is how far apart the runs are, as a percentage of their median: (max - min) / median.
func spreadPct(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	m := median(values)
	if m == 0 {
		return 0
	}
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo, hi = min(lo, v), max(hi, v)
	}
	return (hi - lo) / m * 100
}

func pick(runs []run, f func(run) float64) []float64 {
	out := make([]float64, len(runs))
	for i, r := range runs {
		out[i] = f(r)
	}
	return out
}

// stat is the median of one figure over the runs of a condition, with the spread.
type stat struct {
	Median, Spread float64
}

func statOf(runs []run, f func(run) float64) stat {
	v := pick(runs, f)
	return stat{Median: median(v), Spread: spreadPct(v)}
}

// row is the comparison for one load.
type row struct {
	Name        string
	Runs        int // runs per condition
	Offered     float64
	BaseMedian  stat
	SpecMedian  stat
	BaseP99     stat
	SpecP99     stat
	BaseRate    stat
	SpecRate    stat
	IncreasePct float64 // relative increase of the median latency with Specter
	RateKept    float64 // achieved rate with Specter as a percentage of the offered rate (open loop) or of the baseline (closed)
	SpecterCPU  float64
	StraceCPU   float64
	Captured    int // lines Specter printed, summed over its runs
	Sent        int // requests sent in the same runs, so every one wrote a line
	HasKV       bool
	Noisy       bool // the baseline runs differ too much to judge a small increase
	Mode        string
	Warnings    []string
}

// compare builds the row of a load. noisePct is the baseline spread above which the machine is too noisy.
func compare(l *load, noisePct float64) row {
	base, spec := l.Runs[condBase], l.Runs[condSpec]
	r := row{Name: l.Name, Runs: min(len(base), len(spec))}
	if len(base) == 0 || len(spec) == 0 {
		r.Warnings = append(r.Warnings, "needs runs both without and with Specter")
		return r
	}
	if len(base) != len(spec) {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%d runs without Specter but %d with it", len(base), len(spec)))
	}
	r.Mode = base[0].Mode
	r.Offered = base[0].OfferedRate
	med := func(x run) float64 { return x.MedianMs }
	p99 := func(x run) float64 { return x.P99Ms }
	rate := func(x run) float64 { return x.AchievedRate }
	r.BaseMedian, r.SpecMedian = statOf(base, med), statOf(spec, med)
	r.BaseP99, r.SpecP99 = statOf(base, p99), statOf(spec, p99)
	r.BaseRate, r.SpecRate = statOf(base, rate), statOf(spec, rate)
	if r.BaseMedian.Median > 0 {
		r.IncreasePct = (r.SpecMedian.Median - r.BaseMedian.Median) / r.BaseMedian.Median * 100
	}
	switch {
	case r.Offered > 0:
		r.RateKept = r.SpecRate.Median / r.Offered * 100
	case r.BaseRate.Median > 0:
		r.RateKept = r.SpecRate.Median / r.BaseRate.Median * 100
	}
	if r.BaseMedian.Spread > noisePct || r.BaseRate.Spread > noisePct {
		r.Noisy = true
	}
	var cpuS, cpuT []float64
	for _, x := range spec {
		if x.hasKV {
			r.HasKV = true
			cpuS = append(cpuS, x.SpecterCPU)
			cpuT = append(cpuT, x.StraceCPU)
			r.Captured += x.Captured
			r.Sent += x.Sent
		}
	}
	r.SpecterCPU, r.StraceCPU = median(cpuS), median(cpuT)
	for _, x := range append(append([]run{}, base...), spec...) {
		if x.Errors > 0 {
			r.Warnings = append(r.Warnings, "some requests failed; the figures include only the ones that succeeded")
			break
		}
	}
	return r
}
