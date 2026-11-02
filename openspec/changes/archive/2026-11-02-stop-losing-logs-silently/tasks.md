## 1. Reproduce with failing tests

- [x] 1.1 Replace `TestParser_MultiThreadUnfinishedAndResumed` and parts 2 and 3 of `TestParser_TruncatedOutput` with fixtures copied from real `strace` output, and confirm they fail against the current parser
- [x] 1.2 Add an endpoint or role to `test/e2e/testserver` that has 4 worker processes each write a known number of numbered lines to stdout
- [x] 1.3 Add an E2E test that compares lines written with lines captured for those workers, and confirm it fails today
- [x] 1.4 Add an E2E test for a 20000-byte line followed by a short line, and confirm it fails today

## 2. Overlapping writes

- [x] 2.1 Rewrite the unfinished pattern to accept the real `write(FD, "HEX", LEN <unfinished ...>` form, including `"HEX"..., LEN` for cut payloads
- [x] 2.2 Store the decoded payload and fd per PID at entry, and emit it from the `<... write resumed>) = RET` line, tolerating the padding `strace` adds before `=`
- [x] 2.3 Remove the patterns for resumed lines that carry data, which `strace` does not print
- [x] 2.4 Cover failed resumed writes, fd 3 and higher, and a resumed line with no pending call
- [x] 2.5 Run the worker E2E test from 1.3 and confirm all lines arrive intact

## 3. writev

- [x] 3.1 Add `writev` to the `-e trace=` argument in `BuildStraceArgs`, and update `TestBuildStraceArgs_MultiPID`
- [x] 3.2 Parse complete `writev` lines by joining `iov_base` values in order and limiting to the return value, using fixtures from real output
- [x] 3.3 Handle `writev` in the unfinished and resumed paths, including `<... writev resumed>`
- [x] 3.4 Add an E2E test that a `writev` to stdout is captured

## 4. Long writes

- [x] 4.1 Change `-s` to 65536 in `BuildStraceArgs`
- [x] 4.2 When the return value is larger than the decoded payload, append `[specter: N more bytes not captured]` and a newline, in both the complete and resumed paths
- [x] 4.3 Add unit tests for a cut write, a write inside the limit, and a partial write whose return value is smaller than its length
- [x] 4.4 Confirm the E2E test from 1.4 passes, and that a 60000-byte line arrives whole with no marker

## 5. Keep reading strace output

- [x] 5.1 Replace the `bufio.Scanner` in `readLoop` with a `bufio.Reader` loop that discards an over-limit line to its end and continues
- [x] 5.2 Add a unit test showing writes after an over-limit line are still parsed

## 6. Notices

- [x] 6.1 Add an optional notice method on the `strace` source, following the `ProcessStatus()` pattern, and wire it in `main.go` to print to stderr, including for the fallback source
- [x] 6.2 Report writes to fd 3 and higher from the parser, resolve them through an injectable `procRoot` once per PID and fd, and notify once per path for regular files with `log` in the name
- [x] 6.3 Add unit tests for a log file, a repeated path, a socket and a `.db` file
- [x] 6.4 Print the over-limit line notice from 5.1
- [x] 6.5 Add the 10-second no-output notice in `main.go`, counting entries before filters, and printing at most once
- [x] 6.6 Add E2E tests that the no-output notice goes to stderr, that it doesn't appear for filtered output, and that stdout stays clean

## 7. Flag validation

- [x] 7.1 Validate `--level` before target resolution, returning a `DiagnosticError` that lists the valid levels and accepts all aliases
- [x] 7.2 Print one stderr notice when `--grep` isn't a valid regex, keeping the literal match
- [x] 7.3 Add tests for an unknown level, an alias, an empty value, and an invalid regex

## 8. Verify and document

- [x] 8.1 Rerun the probes from this proposal and record the before and after numbers: 1 thread, 4 threads, 4 processes, and a 20000-byte line
  - Measured 2026-11-02 on Ubuntu/WSL2, strace 6.8, target stdout is a pipe, 3000 lines per writer (old = `HEAD` before this change):

    | Target | Wrote | Old | New |
    |---|---|---|---|
    | 1 thread | 3,000 | 3,000 | 3,000 |
    | 4 threads | 12,000 | 10,129 | 12,000 |
    | 4 processes | 12,000 | 192 | 12,000 |
    | 14 writes of 20,000 bytes, then `after` | 15 lines | 1 line of 114,693 bytes | 14 lines of 20,000, then `after` |
  - Rechecked with the final code: 1 thread 3,000 of 3,000, 4 threads 12,000 of 12,000, all unique. A target whose own child is `cat` (`cmd > >(cat) &`) shows each line twice, because Specter correctly traces that child too.
- [x] 8.2 Rerun the HTTP benchmark and compare with about 2,135 req/s detached and 813 req/s attached, and raise it with the user if the slowdown is noticeably worse
  - Measured with an 8-connection Go load generator against the test server's `/json` endpoint, 8 s per run, two rounds. This is not the benchmark behind the 2,135 and 813 req/s figures, so only old against new is comparable: detached 14,154 and 14,626 req/s, attached with old Specter 1,538 and 1,507, attached with new Specter 1,547 and 1,503. No measurable change in slowdown from `writev` tracing or `-s 65536`.
- [x] 8.3 Confirm there are no extra `strace` processes left and that `specter <port> | grep` stdout has no notice text
  - Measured 2026-11-02: 0 `strace` processes left after detach. `specter <port> | grep` stdout had 0 status lines while the no-output notice went to stderr once.
- [x] 8.4 Run the commands CI runs on Linux: `gofmt -l .`, `go vet ./...`, `go test -race` for unit and stress tests, and the E2E tests with `strace` and `ptrace_scope=0`
  - Run 2026-11-02 in WSL2 on Go 1.27 (CI uses 1.26), strace 6.8, `ptrace_scope=0`: gofmt clean with CRLF stripped, vet clean, `-race` unit and stress pass, all 11 E2E tests pass. Against the old build, the worker, long-line and writev tests fail, with 11,581 of 12,000 lines missing.
- [x] 8.5 Update the README's `trace=write` description, and document the truncation marker and the notices
- [x] 8.6 Add the `--level` behaviour change to the release notes for the next release
