## Why

To switch to another server today, the user presses Ctrl+C (sometimes several times while the targets detach), types `specter` again, and waits for the picker. Watching one server, then another, is a common loop during development, and it should be one key: Esc stops the stream and opens the picker.

## What Changes

- While Specter streams, pressing **Esc** detaches cleanly from every target (the same teardown as Ctrl+C, so no tracer stays attached), prints the usual "detached" line per target, and opens the interactive picker. Choosing a service streams it; Esc or `q` in the picker quits Specter, as today.
- This works in every run: `specter` (from the picker), `specter 3000`, and `specter 4200 8082`. After Esc, the picker selects one target.
- **Ctrl+C does not change:** it still quits Specter.
- The keys are only read when Specter has the terminal: stdin and stderr are terminals and Specter runs in the foreground. Otherwise (piped stdin, a background job, CI) nothing changes.
- While Specter reads keys, typed characters are no longer echoed into the log output. **Enter** still adds an empty line on screen, so users can still space out the logs.
- After attaching, one line on stderr tells the user: `Press Esc to pick another service, Ctrl+C to quit.`
- `--duration` counts per stream: after Esc and a new choice, the time starts again.
- The exit status is the one of the last stream.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `interactive-picker`: Esc during a stream returns to the picker, with the terminal rules (when keys are read, echo, Enter, restoring the terminal) that this needs.

## Impact

- Code: `cmd/specter/main.go` wraps "resolve → start sessions → stream → end reports" in a loop; the first pass uses the command line, later passes use the picker. A new key reader in `internal/ui` puts the terminal in a "keys without echo" mode (Linux only, through `golang.org/x/sys/unix`, with a stub for other systems so the code still compiles) and reports Esc and Enter.
- `golang.org/x/sys` becomes a direct dependency. It is already in `go.mod` as an indirect one, so no new module is downloaded.
- Read-only rule: unchanged. Esc uses the same teardown as Ctrl+C.
- Output streams: unchanged. The hint, the "detached" lines and the picker go to stderr; logs stay on stdout.
- Tests: unit tests for the key reader (lone Esc versus arrow keys) and the run loop; a Linux test with a pseudo-terminal; a manual check in WSL2.
