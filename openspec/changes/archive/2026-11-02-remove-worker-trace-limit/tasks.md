## 1. Reproduce with failing tests

- [x] 1.1 Add flags to `test/e2e/testserver` that start N workers before the ready line, and make each worker wait for a given file before writing its numbered lines
- [x] 1.2 Add an E2E test with 20 workers already running before Specter attaches that compares lines written with lines captured
- [x] 1.3 Run that test with `ptrace_scope=0` and confirm it fails today, with workers beyond the first 16 missing
  - Run 2026-11-02 against the build from `1425e71`: wrote 1,000 lines, missing 200 from 4 of 20 workers (w16 to w19).
- [x] 1.4 Replace the `caps children at max 16` case in `TestBuildStraceArgs_MultiPID` with a case where all 30 children get a `-p` flag, and confirm it fails today

## 2. Remove the limit

- [x] 2.1 Remove `maxChildren`, the counter and the comment from `BuildStraceArgs`, keeping the check that skips duplicate and invalid PIDs
- [x] 2.2 Confirm the tests from 1.2 and 1.4 now pass
  - The E2E test passed 5 runs out of 5 on the new build.

## 3. Worker that exits during attach

- [x] 3.1 Add a unit test showing that a `strace: attach: ptrace(PTRACE_SEIZE, N): No such process` line doesn't stop `readLoop` from parsing later writes

## 4. Verify and document

- [x] 4.1 Attach the built Specter to a target with 100 workers and record lines written and captured
  - Measured 2026-11-02: 100 workers wrote 5,000 lines and all 5,000 were captured, none duplicated. With 20 workers, 1,000 of 1,000.
- [x] 4.2 Confirm no `strace` processes are left after detaching from that target
  - 0 `strace` processes left after detaching from the 20 and 100 worker targets.
- [x] 4.3 Run the commands CI runs on Linux: `gofmt -l .`, `go vet ./...`, `go test -race` for unit and stress tests, and the E2E tests with `strace` and `ptrace_scope=0`
  - Run 2026-11-02 in WSL2 on Go 1.27 (CI uses 1.26), strace 6.8, `ptrace_scope=0`: gofmt clean with CRLF stripped, vet clean, `-race` unit and stress pass, all 12 E2E tests pass.
- [x] 4.4 Add the fix to the `Unreleased` section of `CHANGELOG.md`, including that all workers are now slowed while attached
