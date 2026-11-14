## 1. Safety net for one target

- [x] 1.1 Add a test that records today's single-target stdout and stderr for a fake source (attach line, `Source:` line, log lines, partial line, end report), so the refactor can be checked against it.
- [x] 1.2 Run the existing unit tests and note the result as the baseline.

## 2. Session type (refactor, no new behavior)

- [x] 2.1 Create `cmd/specter/session.go` with a `Session` type: label, target, source, `srcCtx`/`pipeCtx`, process watcher, assembler, grouper and (with `--grep`) trace holder, an entries channel, and its end report.
- [x] 2.2 Move source selection, the eBPF-first start (`startNow`), the non-strace fallback, the attach and `Source:` lines and the slowdown notice into the session's start. It must be called on the main goroutine; add a comment that explains the `Pdeathsig` reason.
- [x] 2.3 Move the end report data (exit status, Docker exit, watcher) into the session, and add a label field to `endReport`.
- [x] 2.4 Make `RunE` use one `Session` for the single target. Check that the test from 1.1 and all existing tests pass unchanged.

## 3. Target list

- [x] 3.1 Change `Args` to accept any number of positional targets, and add a check that positional targets and `--pid` together are at most 8, with a `DiagnosticError` naming the limit.
- [x] 3.2 Change `validatePIDFlag` to allow several `--pid` and `--pid` with positional targets; keep refusing 0 and non-numbers. Update its tests.
- [x] 3.3 Build the target list with labels (`:3000`, `pid:1234`, the container or project name as written), in the order given (positional first, then `--pid`).
- [x] 3.4 Resolve all targets with the existing functions, stopping at the first failure with its label; keep the empty-port diagnostic as today.
- [x] 3.5 Remove duplicates after child discovery (same PID, PID in an earlier target's children, same container), with one notice per drop. Table-test it.

## 4. Several sessions

- [x] 4.1 Start the sessions one after the other in `RunE`. If one fails, stop the started ones in reverse order and return the error with its label. Test with fake sources, including a check that no source of a failed run is left running.
- [x] 4.2 Fan in the sessions' entries into one channel of (session index, entry), with one forwarding goroutine per session.
- [x] 4.3 Prefix status messages with the label when there are several targets: attach line, workers line, `Source:` line, slowdown notice, source notices (`wireNotices`), and the end report. Print the ptrace warning at most once.
- [x] 4.4 Make the "nothing captured yet" notice per session.

## 5. Tagged output

- [x] 5.1 Write the tag helper: pad labels to the longest, add ` | `, color the label (cyan, magenta, blue, green, hi-cyan, hi-magenta) only when colors are on. Tag every physical line of a rendered entry. Unit-test colored and plain output.
- [x] 5.2 Add the row-owner logic for partial lines: a new line break before another session writes after an open partial row; no new tag when the same session continues. Unit-test both cases.
- [x] 5.3 Check that `--raw` and `--json` get the tag with several targets and no tag with one.

## 6. Ending

- [x] 6.1 When a session's entries channel closes, print its end report at once, tagged, and keep the loop running for the others. A runtime source error ends only that session.
- [x] 6.2 On Ctrl+C or the end of `--duration`, stop all sessions and print one "detached" line per session still running.
- [x] 6.3 Exit with status 1 if any session ended by itself, otherwise 0. Unit-test: one ends then Ctrl+C (1), Ctrl+C with all running (0), all end (1).

## 7. Zoned timestamps

- [x] 7.1 In `FormatSingleLineJSON`, parse a string timestamp with `time.RFC3339Nano`; on success show `.Local()` as `15:04:05.000`; otherwise keep today's handling.
- [x] 7.2 Add tests for `Z`, an offset, the same moment as a numeric timestamp, and a string without a zone. Make them independent of the machine's time zone (set `time.Local` in the test).

## 8. CLI and docs

- [x] 8.1 Update `Use`, `Long` and the examples in `--help` with `specter 3000 4000` and `--pid` used twice.
- [x] 8.2 Make shell completion offer targets for every position, not only the first.
- [x] 8.3 Update the README (a multi-target section with an example of tagged output) and add the CHANGELOG entries under `## Unreleased`, including the two small breaking changes.

## 9. End-to-end and measurement (WSL2 or Linux)

- [x] 9.1 E2E: two test servers on two ports; `specter <p1> <p2>` shows lines from both with the right tags, and a stack trace from one stays whole.
- [x] 9.2 E2E: one server exits while the other runs; Specter reports it, keeps the other, and exits with status 1 after Ctrl+C; no `strace` stays attached.
- [x] 9.3 E2E: `specter <p> <p>` (the same server twice) follows it once, with the notice and no tags.
- [x] 9.4 E2E: one port empty → nothing attached, non-zero exit.
- [x] 9.5 Start two eBPF sessions 20 times in a row in WSL2 and record whether any start hangs.
- [x] 9.6 Measure the cost of a second eBPF program: benchmark at 1,000 requests per second with one eBPF session and with two attached; write the result to `test/bench/results/`.

## 10. Checks

- [x] 10.1 Run `gofmt -l .`, `go vet ./...` and `go test -race ./cmd/... ./internal/... ./test/stress/... ./test/bench/...`.
- [x] 10.2 Cross-compile for Windows and macOS (`GOOS=windows`, `GOOS=darwin`).
- [x] 10.3 Run `go test -timeout=10m ./test/e2e/...` in WSL2 with `strace`, `lsof` and `ptrace_scope=0`.
- [x] 10.4 Run `openspec validate multi-stream`.
