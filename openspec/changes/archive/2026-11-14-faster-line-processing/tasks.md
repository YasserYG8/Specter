## 1. Baseline

- [x] 1.1 Add benchmarks in `internal/pipeline` that push one line type at a time through `Group` and `Render` (JSON bench line from `test/e2e/testserver`, access log, plain, Python logging, colored line), and record the baseline ns/op and allocs/op in the change notes
- [x] 1.2 Add test-only reference functions `refStripANSI`, `refSanitize` and `refHasANSI` holding today's bare regex bodies

## 2. ANSI byte check (fix 1)

- [x] 2.1 Add fuzz tests comparing `StripANSI`, `SanitizeTerminalOutput` and `HasANSI` with the reference functions, with the seeds listed in design.md (lone BEL, lone ESC, unterminated `\x1b[` and `\x1b]`, OSC ended by BEL and by `\x1b\\`)
- [x] 2.2 Add an allocation test: the three helpers on a line without ESC or BEL allocate 0 times (`testing.AllocsPerRun`), and a unit test that a lone BEL is still removed
- [x] 2.3 Add the ESC/BEL `strings.IndexByte` check at the top of `StripANSI`, `SanitizeTerminalOutput` (rules.go) and `HasANSI` (parser.go); run the fuzz tests for at least 60 s each

## 3. One JSON decode per line (fix 2)

- [x] 3.1 Add the `{` rejection at the top of `ParseJSONLog`
- [x] 3.2 Add the per-line JSON memo in the grouper and pass it to `isRecordStart` (keep the trace-header checks before JSON; keep `isTraceHeader` working for its test); set `IsJSON` from the same memo
- [x] 3.3 Add the unexported `json` and `jsonKnown` fields to `Entry`, set them in the grouper when the memo was used, and make `Render` use them for one-line entries with `jsonKnown`, falling back to `ParseJSONLog(text)` otherwise
- [x] 3.4 Add the render fuzz test (grouper entry rendered with and without the carried result, color on/off, `Raw`, `ForceJSON`) and a unit test that a Pino `"level":50` line keeps the severity `DetectSeverity` gives

## 4. Verify

- [x] 4.1 `gofmt -l .` is empty, `go vet ./...` passes, and `go test -race ./cmd/... ./internal/... ./test/stress/... ./test/bench/...` passes with no change to existing tests
- [x] 4.2 Cross-compile for Linux, Windows and macOS (`GOOS=linux|windows|darwin go build ./cmd/specter`)
- [x] 4.3 Re-run the benchmarks from 1.1 and record the after numbers next to the baseline
- [x] 4.4 Run the E2E tests in WSL2 (`go test -timeout=10m ./test/e2e/...`) as root, so the `ebpf` tests run too

## 5. Measure and document

- [x] 5.1 Run `ENGINE=ebpf FORMAT=markdown scripts/bench-overhead.sh` in WSL2 as root and save the report as `test/bench/results/<date>-faster-line-processing-wsl2.md`, with the 2026-11-14 run as the before
- [ ] 5.2 If a native Linux runner is available, run the same benchmark there and save the report too
- [x] 5.3 Add a CHANGELOG entry under Unreleased: Specter uses less CPU per line, with the measured capture rate and CPU at peak before and after
