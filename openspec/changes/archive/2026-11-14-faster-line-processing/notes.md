# Notes: faster-line-processing

Measured numbers for this change. Machine: Windows 11, AMD Ryzen 5 3500U, Go 1.26.4. Each number is the median of 3 runs.

Command (run from the repo root):

```
go test ./internal/pipeline -run '^$' -bench 'BenchmarkLine$|BenchmarkANSIHelpers' -benchtime=20000x -count=3 -cpu 1
```

`-cpu 1` puts the grouper and the renderer on one processor, so ns/op is the CPU spent on a line, not the speed of the slower stage.

## Baseline (before any code change)

Taken on 2026-11-14.

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| Line/JSON | 53,080 | 4,468 | 124 |
| Line/AccessLog | 54,027 | 1,346 | 24 |
| Line/Plain | 51,798 | 866 | 18 |
| Line/PythonLogging | 71,244 | 1,130 | 22 |
| Line/Colored | 22,673 | 769 | 24 |
| ANSIHelpers/StripANSI | 4,133 | 208 | 3 |
| ANSIHelpers/SanitizeTerminalOutput | 4,070 | 208 | 3 |
| ANSIHelpers/HasANSI | 3,828 | 0 | 0 |

## After (parts 2 and 3 done)

Taken on 2026-11-14, same machine and command as the baseline. Speedup is baseline ns/op divided by after ns/op.

| Benchmark | ns/op | B/op | allocs/op | Baseline ns/op | Allocs before | Speedup |
|---|---|---|---|---|---|---|
| Line/JSON | 14,254 | 1,264 | 46 | 53,080 | 124 | 3.7x |
| Line/AccessLog | 28,106 | 288 | 6 | 54,027 | 24 | 1.9x |
| Line/Plain | 35,045 | 144 | 3 | 51,798 | 18 | 1.5x |
| Line/PythonLogging | 49,409 | 72 | 4 | 71,244 | 22 | 1.4x |
| Line/Colored | 14,311 | 368 | 9 | 22,673 | 24 | 1.6x |
| ANSIHelpers/StripANSI | 14.3 | 0 | 0 | 4,133 | 3 | 289x |
| ANSIHelpers/SanitizeTerminalOutput | 14.4 | 0 | 0 | 4,070 | 3 | 283x |
| ANSIHelpers/HasANSI | 15.5 | 0 | 0 | 3,828 | 0 | 247x |

What the numbers say:

- The JSON line is 3.7x faster, less than the 6.5x of the research prototype (70.8 to 10.8 µs). The prototype also skipped the panic-header check for JSON lines. This change keeps that check, because skipping it would change output (design decision 3).
- Plain, access-log and Python-logging lines are still 35 to 49 µs. What is left in them is the severity and classification regexes, which this change leaves alone on purpose (fix 3 of the research, its own change). That is where the next gain is.
- The colored line also got faster (22.7 to 14.3 µs), although the design expected no gain: `ParseJSONLog` now rejects it before stripping, and the decision is carried to the renderer. The line still runs the ANSI regexes, so it keeps most of its old cost.
- Numbers are from Windows, where the pipeline runs without a server next to it. The effect on lines lost at peak load is measured by task 5.1.

## Overhead benchmark (task 5.1)

Report: `test/bench/results/2026-11-14-faster-line-processing-wsl2.md`. It answers the open question of design.md ("will this alone lift capture to 100% at peak?"): yes on WSL2. Closed loop, `ebpf` engine: 64.63% of lines captured before, 100.00% after (seven measurements of the new code, all 100.00%), Specter CPU 148.8% before and 101.6% after.

Two things to follow up, both outside this change:

- **A cost:** the server handles fewer requests at peak with Specter attached (19,733 against 22,964 per second), because Specter now keeps every line. The README says the server "was not slowed by this", which is no longer true at peak, and its peak numbers (74% and 79%) are out of date. Native Linux has not been re-measured, so the README is left as it is.
- **A failure that exists before this change:** a Specter that attaches right after another was stopped sometimes prints nothing for 20 seconds, and the benchmark stops (2 of 6 invocations before the change, 6 of 8 after; too few to say if it changed). It needs its own investigation.

Task 5.2 (native Linux) was not done: no native Linux runner was available from here, and using the GitHub runner means pushing and starting a workflow.

## Design refinement found while implementing

The carried JSON result is used only when the grouper's clean text has no ESC and no BEL byte left. Without that check, a line such as `\x1b[3\x1b[1L1m{"level":"info"}` would render as JSON where it rendered as plain text before. Recorded in design.md, decision 2. The render fuzz test fails if the check is removed (checked by removing it once).

## Fuzzing

Each fuzz test ran for 60 s with no difference found: `FuzzStripANSI` about 3.1 million inputs, `FuzzSanitizeTerminalOutput` about 3.0 million, `FuzzHasANSI` about 4.0 million, `FuzzRenderCarriedJSON` about 231,000.
