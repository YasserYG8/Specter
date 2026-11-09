package main

import (
	"fmt"
	"io"
	"strings"
)

// settings are the thresholds the report judges by.
type settings struct {
	TargetPct float64 // largest acceptable increase of the median latency, in percent
	NoisePct  float64 // baseline spread above which the machine is too noisy to judge
	MinRate   float64 // smallest acceptable achieved rate, in percent of the offered rate
	Reader    string  // what is attached to the server: "Specter" (default) or another reader, such as "bpftrace"
}

// reader is the name of what is attached to the server, "Specter" unless -reader says otherwise.
func (s settings) reader() string {
	switch strings.ToLower(s.Reader) {
	case "", "specter":
		return "Specter"
	}
	return s.Reader
}

// loadTitle names a load in words.
func loadTitle(r row) string {
	if r.Mode == "closed" || r.Offered == 0 {
		return "closed loop (as fast as the server goes)"
	}
	return fmt.Sprintf("%.0f requests per second", r.Offered)
}

// verdict says whether a row meets the target. The target is defined for fixed-rate loads only.
func verdict(r row, s settings) string {
	if r.Mode == "closed" || r.Offered == 0 {
		return "not judged (the target is for fixed-rate loads)"
	}
	// Noise matters only when the result is close to the target. An increase that is larger than the target plus
	// the spread of the baseline cannot be noise, so it is still a clear miss.
	if r.Noisy && r.IncreasePct <= s.TargetPct+max(r.BaseMedian.Spread, r.BaseRate.Spread) {
		return fmt.Sprintf("cannot judge: the runs without %s differ by more than %.0f%%, so this machine is too noisy", s.reader(), s.NoisePct)
	}
	increaseOK := r.IncreasePct < s.TargetPct
	rateOK := r.RateKept >= s.MinRate
	switch {
	case increaseOK && rateOK:
		return fmt.Sprintf("MET (median latency +%.0f%%, below %.0f%%, and %.1f%% of the offered rate)", r.IncreasePct, s.TargetPct, r.RateKept)
	case !increaseOK && !rateOK:
		return fmt.Sprintf("NOT MET (median latency %+.0f%%, limit %.0f%%; only %.1f%% of the offered rate, minimum %.0f%%)", r.IncreasePct, s.TargetPct, r.RateKept, s.MinRate)
	case !increaseOK:
		return fmt.Sprintf("NOT MET (median latency %+.0f%%, limit %.0f%%)", r.IncreasePct, s.TargetPct)
	default:
		return fmt.Sprintf("NOT MET (only %.1f%% of the offered rate, minimum %.0f%%)", r.RateKept, s.MinRate)
	}
}

func capturedText(r row) string {
	if !r.HasKV || r.Sent == 0 {
		return "not measured"
	}
	return fmt.Sprintf("%d of %d (%.2f%%)", r.Captured, r.Sent, float64(r.Captured)/float64(r.Sent)*100)
}

// renderText writes a report for reading in a terminal.
func renderText(w io.Writer, rows []row, machine string, s settings) {
	if machine != "" {
		fmt.Fprintf(w, "Machine: %s\n\n", machine)
	}
	for _, r := range rows {
		fmt.Fprintf(w, "Load: %s, %d runs each\n", loadTitle(r), r.Runs)
		if len(r.Warnings) > 0 && r.Runs == 0 {
			fmt.Fprintf(w, "  %s\n\n", strings.Join(r.Warnings, "; "))
			continue
		}
		fmt.Fprintf(w, "  median latency   %.2f ms (spread %.0f%%)  ->  %.2f ms (spread %.0f%%)   %+.0f%%\n",
			r.BaseMedian.Median, r.BaseMedian.Spread, r.SpecMedian.Median, r.SpecMedian.Spread, r.IncreasePct)
		fmt.Fprintf(w, "  99th percentile  %.2f ms  ->  %.2f ms\n", r.BaseP99.Median, r.SpecP99.Median)
		fmt.Fprintf(w, "  achieved rate    %.0f requests/s  ->  %.0f requests/s\n", r.BaseRate.Median, r.SpecRate.Median)
		if r.HasKV && r.StraceCPU > 0 {
			fmt.Fprintf(w, "  CPU use          %s %.1f%%, strace %.1f%% (of one core)\n", s.reader(), r.SpecterCPU, r.StraceCPU)
		} else if r.HasKV {
			fmt.Fprintf(w, "  CPU use          %s %.1f%% (of one core; work done inside the kernel is charged to the server and not counted here)\n", s.reader(), r.SpecterCPU)
		}
		fmt.Fprintf(w, "  lines captured   %s\n", capturedText(r))
		fmt.Fprintf(w, "  target           %s\n", verdict(r, s))
		for _, warn := range r.Warnings {
			fmt.Fprintf(w, "  note             %s\n", warn)
		}
		if r.Noisy {
			fmt.Fprintf(w, "  note             the baseline runs spread %.0f%% (latency) and %.0f%% (rate); the machine was busy or noisy\n",
				r.BaseMedian.Spread, r.BaseRate.Spread)
		}
		fmt.Fprintln(w)
	}
}

// renderMarkdown writes the same comparison as a table, for the README or a result file.
func renderMarkdown(w io.Writer, rows []row, machine string, s settings) {
	if machine != "" {
		fmt.Fprintf(w, "Machine: %s\n\n", machine)
	}
	fmt.Fprintf(w, "| Load | Median latency without %[1]s | Median latency with %[1]s | Change | p99 without / with | Rate without / with | %[1]s CPU | strace CPU | Lines captured |\n", s.reader())
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|")
	for _, r := range rows {
		if r.Runs == 0 {
			continue
		}
		cpuS, cpuT := "n/a", "n/a"
		if r.HasKV {
			cpuS, cpuT = fmt.Sprintf("%.1f%%", r.SpecterCPU), fmt.Sprintf("%.1f%%", r.StraceCPU)
		}
		name := fmt.Sprintf("%.0f requests/s", r.Offered)
		if r.Mode == "closed" || r.Offered == 0 {
			name = "closed loop (peak)"
		}
		fmt.Fprintf(w, "| %s | %.2f ms (spread %.0f%%) | %.2f ms (spread %.0f%%) | %+.0f%% | %.2f / %.2f ms | %.0f / %.0f requests/s | %s | %s | %s |\n",
			name, r.BaseMedian.Median, r.BaseMedian.Spread, r.SpecMedian.Median, r.SpecMedian.Spread, r.IncreasePct,
			r.BaseP99.Median, r.SpecP99.Median, r.BaseRate.Median, r.SpecRate.Median, cpuS, cpuT, capturedText(r))
	}
	fmt.Fprintln(w)
	for _, r := range rows {
		if r.Noisy && r.Runs > 0 {
			fmt.Fprintf(w, "Note: for %s the runs without %s differ by %.0f%% (latency) and %.0f%% (rate), so the machine was too noisy for a small change to mean much.\n",
				loadTitle(r), s.reader(), r.BaseMedian.Spread, r.BaseRate.Spread)
		}
	}
}
