## 1. Tests for today's behavior

- [x] 1.1 Extract the numeric branch of `RunE` into `resolveNumericTarget(ctx, res, val)` behind a narrow resolver interface, without changing behavior, and add a test with a fake resolver that shows the current fallback: an empty port 3000 plus an existing PID 3000 attaches to the PID.

## 2. Port only for 1–65535

- [x] 2.1 Remove the PID fallback for numbers from 1 to 65535. An empty port returns `NoProcessOnPortError`, and other port errors are returned as before.
- [x] 2.2 When the port is empty and `ResolvePID(val)` succeeds, add the hint line `Hint: PID N exists (<name>). To attach to it: specter --pid N` to the diagnostic, with the name taken from the process's command line.
- [x] 2.3 Numbers above 65535 go straight to `ResolvePID`. A missing PID returns "no process with PID N exists", wrapping `ErrProcessNotFound`.
- [x] 2.4 Update the test from 1.1 so it expects the diagnostic and no target, and add tests for: an empty port without a matching PID (no hint), a non-empty-port error (returned, no PID lookup), a PID above 65535, and a missing PID above 65535.

## 3. `--pid` flag

- [x] 3.1 Add `--pid` / `-p` as an `IntSlice` flag.
- [x] 3.2 Validate it before resolving: more than one value, combined with a positional argument, or a value that isn't positive is an error.
- [x] 3.3 `--pid N` resolves with `ResolvePID` only, and never checks port N.
- [x] 3.4 Tests: `--pid 3000` with a port 3000 that also listens still attaches to the PID; `-p` is the same as `--pid`; two values; `--pid 3000,4000`; `--pid` with a positional argument; `--pid 0`; a missing PID.

## 4. Help and docs

- [x] 4.1 Update `Use`, `Short`, `Long` and the examples in `main.go`: the PID example becomes `specter --pid 14280`, and the flag shows in help with its short form.
- [x] 4.2 Update `README.md`: the usage line, the PID example and the flags table.
- [x] 4.3 Add a `CHANGELOG.md` entry under Unreleased → Breaking, and say how to attach by PID now.

## 5. Real check and checks

- [x] 5.1 In WSL, run `specter 1` (PID 1 exists, nothing listens on port 1): the diagnostic and the hint must print, and nothing is attached. Then run `specter --pid <a test server PID>` and confirm it attaches.
- [x] 5.2 `gofmt -l .`, `go vet ./...`, and `go test ./cmd/... ./internal/... ./test/stress/...` pass. E2E tests that attach by bare PID are updated to use `--pid`.
