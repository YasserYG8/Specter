## 1. Tracer lookup

- [x] 1.1 Add `util.ErrAlreadyTraced`, and `tracerOf(procRoot, pid)` in `internal/source/strace`, which reads `TracerPid`, the tracer's command line, and its parent's PID and name, leaving any unreadable field empty.
- [x] 1.2 Tests with a fake proc tree: not traced; traced by `strace` whose parent is `specter`; traced by `gdb`; tracer `cmdline` unreadable; `status` unreadable.

## 2. Target and workers

- [x] 2.1 In `Source.Start`, before strace runs, return the "already traced" `DiagnosticError` from the design when the target has a tracer. Build the message variants with and without a `specter` parent.
- [x] 2.2 Filter already traced workers out of `target.Children` before `BuildStraceArgs`, and send one notice naming them and their tracers.
- [x] 2.3 In the "Operation not permitted" path, call `tracerOf` again for the target, and use the "already traced" diagnostic when it finds a tracer.
- [x] 2.4 Unit tests for 2.1–2.3, using the injectable proc root, with no strace needed.

## 3. Exit status

- [x] 3.1 Check that every attach failure (already traced, permission denied, no such process) ends with exit status 1, including the path where the error arrives through the `errs` channel after strace started. Fix it if any path returns 0, and add a test.

## 4. End-to-end and docs

- [x] 4.1 E2E in WSL: start the test server, attach one Specter, then run a second `specter --pid` against it. The second must print the "already traced" diagnostic naming the first Specter, exit with status 1, and leave the first Specter's capture working.
- [x] 4.2 `CHANGELOG.md` under Fixed.
- [x] 4.3 `gofmt -l .`, `go vet ./...`, and `go test ./cmd/... ./internal/... ./test/stress/...` pass.
