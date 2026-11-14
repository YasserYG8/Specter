## Context

Every captured line goes through `LineAssembler` → `StackTraceGrouper` → (`TraceHolder` with `--grep`) → `Filter` → `Renderer` → stdout. A profile on WSL2 (`research_notes/Specter performance improvements/pipeline_hotspots.md`) measured about 100 µs of CPU per JSON line. The grouper goroutine takes about 55% of it and caps the pipeline near 15,000–20,000 lines per second, which is why the `ebpf` engine loses about 25% of lines at peak.

Two kinds of repeated work dominate:

- **ANSI regexes.** For one JSON line, `StripANSI` runs 5 times (grouper, `ParseJSONLog` ×3, `DetectSeverity`) and `SanitizeTerminalOutput` once. Each call is a `ReplaceAllString`, which scans the whole line from every byte (the patterns have no common literal prefix) and always returns a copy, even when nothing matched: 3.0–4.6 µs and 3 allocations per call, measured.
- **JSON decoding.** `ParseJSONLog` runs `json.Unmarshal` into a map. It is called in `isRecordStart`, again in the grouper to set `IsJSON`, and again in `Renderer.Render`: three decodes of the same text.

A prototype of the two fixes in this design cut the JSON line from 70.8 µs / 123 allocs to 10.8 µs / 46 allocs, the access-log line from 61.5 to 32.9 µs, and the plain line from 59.5 to 43.3 µs (measured, one goroutine, `Group`+`Render` work).

## Goals / Non-Goals

**Goals:**
- Remove the repeated ANSI and JSON work with output that is identical for every input.
- Prove the equivalence with fuzz tests, and record the gain with benchmarks and the overhead benchmark.

**Non-Goals:**
- Literal checks in front of the severity and classification regexes (fix 3 of the research). It touches many more regexes and needs its own differential test.
- Buffered stdout, batching between stages, fewer goroutines, `ReadInto` in the eBPF reader.
- Any change to output: the terminal-safety gap (`\x1b[?25l`, `\x1bc`, DCS strings pass through today) and the nanosecond `ts` shown as milliseconds stay as they are and get their own changes.
- Faster handling of lines that do contain escape codes (colored dev output). They keep today's cost.

## Decisions

### 1. Guard the three ANSI helpers with a byte check

At the top of `StripANSI` and `SanitizeTerminalOutput`: if the line has no `0x1b` and no `0x07` byte, return it as it is. At the top of `HasANSI`: return `false` in the same case. Otherwise run the regexes exactly as today.

Why it is exact: every alternative of `ansiSGRRegex`, `anyANSIRegex` and `nonSGRControlRegex` begins with `\x1b` or is `\x07`, so none can match a line that has neither byte. The check uses two `strings.IndexByte` calls, which are vectorized in Go's runtime (about 16 ns on an 80-byte line, measured).

The check must test **both** bytes. A check for ESC alone would skip lines with a lone BEL, which `SanitizeTerminalOutput` removes today; the terminal would beep. A fuzz seed and a unit test cover this.

Alternatives:
- Compute "has ESC/BEL" once per line and carry it on the `Line` or `Entry`. Saves a few more nanoseconds, but changes more signatures for no measurable gain.
- Replace the three regexes with one hand-written scanner. It would also speed up colored lines, but copying the regex semantics exactly is riskier. Left for later if colored output turns out to matter.

### 2. Decode JSON at most once per line, lazily, and carry the result

The grouper keeps a small per-line memo: "not tried yet" or "tried, result `*JSONLog` (maybe nil)". `isRecordStart` receives the memo and asks it at step 2 instead of calling `ParseJSONLog`. The grouper's `IsJSON` reads the same memo. The decode happens only when something asks, so a line is never decoded more often than today, in any mode (for example a continuation line in `--raw` mode, which today is never decoded, still is not).

When the memo was used, the grouper puts the result on the `Entry` in two unexported fields, `json *JSONLog` and `jsonKnown bool`. `Render` uses `entry.json` when `jsonKnown` is true and the entry has one line; otherwise it calls `ParseJSONLog(text)` as today. That fallback keeps entries built outside the grouper (tests, multi-line entries) working unchanged, and it is also the reference path for the fuzz test.

Why the carried result is the same as the renderer's own decode: the grouper decodes `cleanText = StripANSI(safeText)` and the renderer decodes `entry.Text() = safeText`. `ParseJSONLog` strips ANSI first, so both decode the same bytes and give the same fields. They differ only in `JSONLog.Raw`, which nothing reads when rendering.

This holds only when stripping `cleanText` a second time changes nothing. A line such as `\x1b[3\x1b[1L1m{"level":"info"}` keeps `\x1b[1L` through `SanitizeTerminalOutput`, and `StripANSI` then builds a new `\x1b[31m` out of what is left. The grouper's decode strips again and finds JSON, while the renderer, which strips once, does not. So the grouper carries the result only when `cleanText` has no ESC and no BEL byte left. Any other line is left to the renderer's own decode, which is today's behavior. The render fuzz test has seeds for this case, and the test fails if the check is removed.

Unexported fields keep the contract inside `internal/pipeline`: no caller can set a stale result by mistake. `TraceHolder` copies entries by value, which copies the pointer; the `JSONLog` is never changed after it is built.

Alternatives:
- Decode every line eagerly in the grouper. Simpler, but adds a decode to continuation lines in `--raw` mode, so some paths would do more work than today.
- An exported `Entry.JSON` field. Not needed outside the package, and it invites stale values.

### 3. Keep severity and order exactly as today

- A JSON line's `Level` keeps coming from `DetectSeverity(clean)`, not from `JSONLog.Level`. They differ for Pino numeric levels (`"level":50`), which the regex does not read.
- `isRecordStart` keeps its order: the Python and Go trace headers are checked before JSON. (The research prototype skipped the panic check for JSON lines; this design does not.)

Changing either would change output, which this change must not do.

### 4. Cheap rejection in `ParseJSONLog`

Before stripping ANSI, `ParseJSONLog` returns `nil` when the line contains no `{` byte. A JSON object after stripping must start with `{`, and stripping only removes bytes, so the original must contain one. This saves one strip per call on non-JSON lines that do have escape codes.

### 5. How equivalence is tested

- **Reference functions in tests.** The test files keep the old bodies as `refStripANSI`, `refSanitize` and `refHasANSI` (the bare regex calls). Fuzz tests (`testing.F`) assert the new functions equal them for every input. Seeds: empty string, plain text, `\x1b[31mred\x1b[0m`, `\x1b]0;title\x07`, `\x1b]0;title\x1b\\`, lone `\x07`, lone `\x1b`, `\x1b[` without a final byte, and long lines.
- **Render fuzz.** For a random line, run it through the grouper, render the entry, then clear `jsonKnown` and render again. Both strings must be equal, for color on and off, `Raw` and `ForceJSON`. Seeds include JSON objects, colored JSON, a Pino numeric level, a Python traceback, a Go panic, and a JSON line inside a trace.
- **Allocation test.** `testing.AllocsPerRun` on the three helpers with a plain line must be 0.
- **Benchmarks** for JSON, access-log, plain, Python logging and colored lines through `Group`+`Render`, so the before and after numbers are recorded, including the unchanged colored case.
- All existing tests, including `property_test.go` (lossless output, dangerous sequences), pass unchanged.

## Risks / Trade-offs

- [The byte check misses a pattern that can match without ESC or BEL] → All three patterns are listed above; a future edit to them is covered by the fuzz test against the reference regex, which would fail at once.
- [The carried JSON result differs from what the renderer would decode] → The render fuzz test compares the two paths directly on every input; the fallback stays as the reference.
- [Entry grows by a pointer and a bool] → 9 bytes per entry; channels hold at most 1024 entries per stage.
- [Colored lines get no gain] → Accepted and recorded by the colored benchmark. A scanner can follow if users need it.
- [WSL2 numbers are noisy (up to ~35% between runs)] → Report medians of 5 runs as the script does, and also run the overhead benchmark on native Linux (the GitHub runner used for `2026-11-10-both-engines-github-runner.md`) when possible.

## Migration Plan

None. The change is internal and the output does not change. Rollback is a revert of the commit.

## Open Questions

- Will this alone lift capture to 100% at peak? Inferred, not measured: the grouper's cost per JSON line should fall from about 55 µs to well under 10 µs, but the reader side and CPU sharing with the server are unknown. The overhead benchmark after the change answers it; if lines are still lost, fix 3 and buffered stdout are the next candidates.
