// Command benchreport reads the runs that scripts/bench-overhead.sh saved and prints the comparison of each load
// without and with Specter attached: the median of the runs, their spread, and whether the target is met.
//
// Files in -dir are named <load>.<base|spec>.<n>.json (loadgen's output), with an optional
// <load>.<base|spec>.<n>.kv of key=value lines (specter_cpu_pct, strace_cpu_pct, captured_lines).
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	dir := flag.String("dir", "", "directory with the saved runs (required)")
	format := flag.String("format", "text", "text or markdown")
	machine := flag.String("machine", "", "description of the machine, printed at the top")
	var s settings
	flag.StringVar(&s.Reader, "reader", "specter", "what is attached in the second condition: specter or the name of another reader, such as bpftrace")
	flag.Float64Var(&s.TargetPct, "target", 10, "largest acceptable increase of the median latency, in percent")
	flag.Float64Var(&s.NoisePct, "noise", 10, "baseline spread, in percent, above which the machine is too noisy to judge")
	flag.Float64Var(&s.MinRate, "min-rate", 99, "smallest acceptable achieved rate, in percent of the offered rate")
	flag.Parse()

	if *dir == "" || (*format != "text" && *format != "markdown") {
		fmt.Fprintln(os.Stderr, "usage: benchreport -dir <directory> [-format text|markdown] [-machine <text>] [-target 10] [-noise 10] [-min-rate 99]")
		os.Exit(2)
	}
	loads, err := readRuns(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchreport:", err)
		os.Exit(1)
	}
	if len(loads) == 0 {
		fmt.Fprintf(os.Stderr, "benchreport: no runs found in %s (expected files like rate-1000.base.1.json)\n", *dir)
		os.Exit(1)
	}
	rows := make([]row, len(loads))
	for i, l := range loads {
		rows[i] = compare(l, s.NoisePct)
	}
	if *format == "markdown" {
		renderMarkdown(os.Stdout, rows, *machine, s)
	} else {
		renderText(os.Stdout, rows, *machine, s)
	}
}
