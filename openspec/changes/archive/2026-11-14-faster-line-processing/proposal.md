## Why

At peak load the `ebpf` engine captures only about 75% of lines (2026-11-14 benchmark, WSL2: 75.65%, 151.6% Specter CPU), and the cause is Specter's own line processing, not the kernel. A profile of the pipeline (research notes in `research_notes/Specter performance improvements/pipeline_hotspots.md`) measured about 100 µs of CPU per JSON line, most of it the same work done again and again: the ANSI regexes run up to 6 times per line and copy the whole line each time even when it has no escape codes, and a JSON line is decoded with `json.Unmarshal` 3 times (twice in the grouper, once in the renderer). A prototype that removes this repeated work cut a JSON line from 70.8 µs to 10.8 µs (6.5×) with the same output.

## What Changes

- The three ANSI helpers (`StripANSI`, `SanitizeTerminalOutput`, `HasANSI`) return at once, without running a regex and without copying, when the line has no ESC (`0x1b`) and no BEL (`0x07`) byte. Every one of their patterns needs one of those bytes to match, so the result is identical.
- A line is decoded as JSON at most once on its way from the grouper to the renderer. The grouper keeps the result on the `Entry`, and the record decision, `IsJSON` and the renderer reuse it. The renderer still decodes by itself an `Entry` that did not come from the grouper.
- `ParseJSONLog` skips the ANSI strip and the decode when the line has no `{` at all.
- Nothing visible changes: severity, trace grouping, filters, colors, `--raw`, `--json` and terminal-safety sanitizing give the same output for every line. Severity of a JSON line still comes from `DetectSeverity`, as today.
- New tests prove it: fuzz tests that compare each fast path with the old regex path, an allocation test for lines without escape codes, and benchmarks for JSON, access-log, plain and colored lines.
- Not in this change: the other fixes from the research (literal checks before the classification regexes, buffered stdout, fewer allocations in the eBPF reader), the terminal-safety gap found while exploring (`\x1b[?25l`, `\x1bc` and DCS strings pass through today), and the nanosecond `ts` shown as milliseconds. Each changes either more code or the output, so each gets its own change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stress-testing`: adds requirements that each line's work is not repeated (no ANSI regex or copy for a line without ESC or BEL, at most one JSON decode per line) and that the fast paths give output identical to the full path, checked by fuzz tests.

## Impact

- Code: `internal/pipeline/rules.go`, `parser.go` (`HasANSI`), `json.go` (`ParseJSONLog`), `grouper.go` (one decode per line, carried on `Entry`), `renderer.go` (reuse the carried result). `Entry` gets two fields; it is internal to Specter.
- Tests: new fuzz, allocation and benchmark tests in `internal/pipeline`. Existing tests must pass unchanged.
- Measurement: `scripts/bench-overhead.sh` with `ENGINE=ebpf` before and after, on WSL2 and if possible native Linux, with a report in `test/bench/results/`.
- No new dependencies, no change to the CLI, the sources, or the output.
