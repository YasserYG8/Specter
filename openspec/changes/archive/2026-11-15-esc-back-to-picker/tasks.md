## 1. Key reader

- [x] 1.1 Add `internal/ui/keys.go` with the `KeyReader` type, the `KeyEsc` and `KeyEnter` keys, and the decoding logic (lone Esc versus an Esc sequence, Enter as CR or LF) on top of a small byte-source interface, so it can be tested without a terminal.
- [x] 1.2 Add `keys_linux.go`: save the termios state, turn off `ICANON` and `ECHO` with `VMIN=1`/`VTIME=0`, keep `ISIG` and `OPOST`, read with `unix.Poll` (100 ms) so `Stop()` returns promptly, use a 30 ms poll after an Esc byte, and restore the saved state in `Stop()`.
- [x] 1.3 Add `keys_other.go` (build tag `!linux`) whose `Start` returns "not supported".
- [x] 1.4 Add `CanReadKeys(stdin, stderr)`: both terminals, and the process group is the terminal's foreground group.
- [x] 1.5 Unit-test the decoding: lone Esc → `KeyEsc`; `ESC [ A`, `ESC [ B` and `ESC x` → nothing; CR and LF → `KeyEnter`; other bytes → nothing.
- [x] 1.6 Linux test with a pseudo-terminal (`/dev/ptmx`): after `Start`, `ECHO` and `ICANON` are off and `ISIG` and `OPOST` are on; after `Stop`, the settings equal the saved ones; `Stop` returns within 200 ms while no key is pressed.

## 2. Passes in RunE

- [x] 2.1 Move "resolve → children → dedupe → start sessions → stream → end reports" from `RunE` into `runPass`, which returns how the pass ended (ended with or without a self-ended target, interrupted, back to the picker, or an error). No behavior change yet; all existing tests pass.
- [x] 2.2 Give each pass its own context; make the `--duration` timer per pass.
- [x] 2.3 Add the `errBackToPicker` cause, and make the end report treat it like an interrupt ("Detached ... cleanly.").
- [x] 2.4 Make `RunE` loop: first pass from the command line (or the picker with no targets); after `backToPicker`, the next pass from `pickTarget`; closing the picker ends Specter with the last pass's exit status.

## 3. Keys during a stream

- [x] 3.1 In `runPass`, start the key reader after the sessions started (only when `CanReadKeys`), print the hint on stderr, and `defer` its `Stop()`.
- [x] 3.2 On `KeyEsc`, cancel the pass with `errBackToPicker`. Make sure the reader has stopped (and the terminal is restored) before the picker opens.
- [x] 3.3 On `KeyEnter`, write an empty line through the output writer when stdout is a terminal; write nothing when it is not.
- [x] 3.4 Unit-test the loop with a fake key source and fake sessions: Esc → "detached" lines → picker called; picker closed → exit status of the last pass; Ctrl+C → no picker; stdin not a terminal → no reader, no hint.

## 4. Docs

- [x] 4.1 Update `--help` (Long text) and the README: Esc returns to the picker, Ctrl+C quits, Enter adds an empty line, and `stty sane` if Specter was killed with SIGKILL.
- [x] 4.2 Add a CHANGELOG entry under `## Unreleased`.

## 5. Checks

- [x] 5.1 Run `gofmt -l .`, `go vet ./...` and `go test -race ./cmd/... ./internal/... ./test/stress/... ./test/bench/...`.
- [x] 5.2 Cross-compile for Windows and macOS.
- [x] 5.3 Manual test in WSL2 with CarHub: `sudo specter 4200 8082` → Esc → pick 8082 → Esc → pick 4200 → Ctrl+C; check the "detached" lines, that no `strace` stays attached (also with `--source strace`), and that the shell echoes typed text afterwards.
- [x] 5.4 Repeat Esc → pick → stream 20 times with the eBPF engine in WSL2 and record whether any start hangs.
- [x] 5.5 Check `specter 3000 < /dev/null`, `specter 3000 &` and `specter 3000 | grep x` behave as the spec says.
- [x] 5.6 Run `openspec validate esc-back-to-picker`.

## 6. A clean screen for the picker (added after the first try)

- [x] 6.1 After Esc, when every session has stopped, clear the screen (not the scrollback) on stderr before the picker opens. Only when stderr is a terminal; not for the first picker of a run and not after Ctrl+C.
- [x] 6.2 Unit-test: cleared once per Esc, after the "detached" lines and before the new stream; not cleared after Ctrl+C or when stderr is not a terminal.
- [x] 6.3 Check with the real binary in a pseudo-terminal in WSL2: after Esc the first text after the clear is the picker, with no old log line.
- [x] 6.4 Update the README and the CHANGELOG.
- [x] 6.5 Erase the scrollback too (`ESC [3J`), like the `clear` command: erasing only the screen worked like Ctrl+L and left the old logs above the picker. Update the spec, the design, the test, the README and the CHANGELOG.
