## 1. strace exit status

- [x] 1.1 Switch `BuildStraceArgs` from `-qq` to `-q`, and update its test.
- [x] 1.2 Parse `+++ exited with N +++` and `+++ killed by SIG... +++` lines, with or without `[pid N]`, into a structured status (PID, code or signal). Tests use the exact lines captured in WSL.
- [x] 1.3 Keep only the status whose PID is the target PID, and expose it from the source in place of the raw `processStatus` string. Tests: a helper exits before the target; a child exits after the target; a line without a prefix.

## 2. Process watcher

- [x] 2.1 Add the watcher: record the start time at attach, then poll `/proc/<pid>/stat` once per second, and treat a missing file, state `Z` or `X`, or a different start time as gone. It uses an injectable proc root and poll period.
- [x] 2.2 Tests with a fake proc tree: process present, removed, zombie, PID reused with a new start time, and stop on context cancel.

## 3. Wiring and the final report

- [x] 3.1 In `main.go`, derive a context the watcher can cancel with a cause, separate from the user's interrupt. Start the watcher for strace, file and journal by `_PID`, but not for journal by unit or for Docker.
- [x] 3.2 After the watcher fires, let the pipeline drain for up to about 500 ms before exiting, so the last lines are printed.
- [x] 3.3 Add `reportEnd` with the four cases from the design and the SIGKILL hint, and return exit status 1 when the target ended and 0 when the user interrupted.
- [x] 3.4 Docker: when the log stream ends without an interrupt, run `docker inspect` once for the state and exit code, and report `exited with code N.` or `stopped.`
- [x] 3.5 Unit tests for `reportEnd`, covering each case and its exit status, and for the Docker state parsing with a fake command runner.

## 4. `ptrace_scope` warning

- [x] 4.1 Skip the warning for root at scope 1 and 2, and keep it at scope 3. Add tests for both.

## 5. Real check, docs and checks

- [x] 5.1 In WSL as root, repeat the earlier test: `kill -9`, `kill -TERM`, a normal exit with code 3 through a pipe (strace), a normal exit with code 3 writing to a file (file source), and the "child outlives the target" case. Each must print the right line and exit with status 1. Ctrl+C on a live target must print "Detached ... cleanly." and exit with 0. No `ptrace_scope` warning should appear as root.
- [x] 5.2 If Docker is available in WSL, stop a container while attached and check the message and exit code.
- [x] 5.3 Add `CHANGELOG.md` entries: under Fixed for the reports, and under Breaking for the exit status. Update the README if it says what happens when the target stops.
- [x] 5.4 `gofmt -l .`, `go vet ./...`, and `go test ./cmd/... ./internal/... ./test/stress/...` pass, and E2E tests that expect "Detached" or exit status 0 after a target exit are updated.
