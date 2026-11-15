## Context

`RunE` in `cmd/specter/main.go` runs one pass: resolve the targets (from the command line, or from `pickTarget` when there are none) → discover children → remove duplicates → start the sessions on the locked main goroutine → `runSessions` until every session has ended → return the exit status. Ctrl+C and `--duration` end a pass by cancelling the run context; each session then prints its "detached" line.

Today, nothing reads stdin while Specter streams. The terminal stays in its normal (canonical, echo) mode, so typed keys are echoed into the log output, and the only way out is Ctrl+C. The picker (`internal/ui/picker.go`) already puts the terminal in raw mode with `term.MakeRaw`, reads keys, and treats a lone Esc byte (a read of length 1) as "quit".

## Goals / Non-Goals

**Goals:**
- Esc during a stream → clean detach → picker → new stream, in one process.
- Ctrl+C keeps quitting Specter.
- The terminal is always given back as it was, on every exit path Specter can catch.
- No change at all when Specter does not own the terminal (piped stdin, background job, CI).

**Non-Goals:**
- Other shortcuts during a stream (pause, filter, clear). The key reader makes them possible later.
- Choosing several services in the picker.
- Going back automatically when every target has ended by itself (Specter still exits, as today).

## Decisions

### 1. A loop around one pass

```
  first pass: targets from the command line (or the picker when there are none)
       │
       ▼
  ┌─▶ resolve → dedupe → start sessions → stream ──┬─ all ended / Ctrl+C / --duration / error ─▶ exit
  │                                               │
  │                                               └─ Esc ─▶ stop sessions ("detached" lines)
  │                                                          │
  └──────────────── picker (one target) ◀───────────────────┘   picker closed ─▶ exit (last pass's status)
```

The body of `RunE` from "resolve" to "end reports" moves into `runPass(ctx, targets) (passResult)`, where the result says how the pass ended: `ended` (with `anyEnded`), `interrupted`, `backToPicker`, or an error. `RunE` loops: the first pass uses the command line; after `backToPicker` the next pass uses `pickTarget`. Every pass runs on the locked main goroutine, so every `strace` keeps its parent-death signal (as in the multi-stream design).

Each pass gets its own child context of the signal context. Esc cancels the pass context with a cause `errBackToPicker`; Ctrl+C cancels the parent. The end report treats `errBackToPicker` like an interrupt ("Detached ... cleanly."), so the user sees the usual teardown. `--duration` makes its timer per pass.

*Alternative:* `exec` a fresh `specter` after Esc. Rejected: it loses the terminal state handling, repeats startup work, and under `sudo` it would need the binary path again.

### 2. A key reader in "keys without echo" mode

A new `internal/ui/keys.go` defines `KeyReader` with `Start(ctx) (<-chan Key, error)` and `Stop()`, and keys `KeyEsc` and `KeyEnter`.

The terminal mode is set with `golang.org/x/sys/unix` termios calls (`IoctlGetTermios` / `IoctlSetTermios`), not `term.MakeRaw`:
- turn off `ICANON` (line buffering) and `ECHO` (echo), set `VMIN=1`, `VTIME=0`;
- **keep `ISIG`**, so Ctrl+C still sends SIGINT and the existing signal path stays the only quit path;
- **keep `OPOST`**, so `\n` still returns to the first column. `term.MakeRaw` turns both off; with it, every log line would start in the wrong column and Ctrl+C would become a byte.

The termios code lives in `keys_linux.go`; `keys_other.go` returns "not supported", so Windows and macOS still compile and simply have no Esc (Specter does not attach there anyway).

**When keys are read** (all must hold): stdin is a terminal, stderr is a terminal (the picker draws there), and Specter's process group is the terminal's foreground group (`tcgetpgrp(stdin) == getpgrp()`). The last check matters: a background job that reads the terminal or changes its mode is stopped by the kernel (SIGTTIN/SIGTTOU). stdout may be a pipe: `specter 3000 | grep 500` still gets Esc.

### 3. Reading without a stuck goroutine

The picker also reads stdin, so the stream's key reader must have **stopped reading** before the picker starts, or the two would steal each other's keys. A plain blocking `read` cannot be cancelled. The reader therefore loops on `unix.Poll` with a 100 ms timeout and checks its context between polls; `Stop()` cancels and waits for the loop to return, then restores the terminal.

**Lone Esc versus a sequence:** after an Esc byte, the reader polls once more for 30 ms. If more bytes arrive, it reads and drops the whole sequence (arrow keys `ESC [ A`, Alt+letter `ESC x`). If nothing arrives, it is a lone Esc. This is the same rule terminals and editors use; 30 ms is far below human typing speed and above local terminal latency.

### 4. Restoring the terminal

The saved termios state is restored by `Stop()`, which `runPass` calls with `defer`, so it runs on a normal end, on Ctrl+C and SIGTERM (they cancel the context and `RunE` returns), on `--duration`, on errors, and on a panic in the main goroutine. It is also restored before the picker opens (the picker sets its own raw mode and restores it).

SIGKILL or a crash in another goroutine cannot be caught. The terminal is then left with echo off. Because `ISIG` and `OPOST` were kept, it stays usable, and `stty sane` (or `reset`) fixes echo. This is documented in the README's troubleshooting section.

### 5. Enter and the hint

Enter writes `\n` through the output writer when stdout is a terminal (the writer closes an open partial row first, as for another target). When stdout is a pipe, Enter writes nothing, so the pipe gets only log lines. Other keys are ignored.

The hint `Press Esc to pick another service, Ctrl+C to quit.` is printed on stderr once per pass, after the attach and `Source:` lines, only when keys are read.

### 6. A clean screen for the picker

Added after the first try: with the picker drawn under the old logs, the screen was hard to read. So after Esc, when every session has stopped and printed its "detached" line, Specter writes `ESC [H ESC [2J ESC [3J` (cursor to the top left, erase the screen, erase the scrollback: what the `clear` command sends) to stderr and then opens the picker. It is done at that point, in `pickPass`, because nothing is written after it: clearing at the moment Esc is pressed would let late log lines and the "detached" lines land on the clean screen. The "detached" lines are still printed (they are visible while a slow teardown runs), and then cleared with the rest. The scrollback is erased too. A first version erased only the screen (`ESC [2J`), which is what Ctrl+L does: the logs were still there after scrolling up, and the user did not want them. A terminal cannot erase only a part of its scrollback, so the lines from before the `specter` command are erased as well; this is the trade-off of a really clean picker, and it is the same as running `clear`. The first picker of a run (`specter` with no target) clears nothing, and neither does Ctrl+C.

### 7. Exit status

The exit status is the last pass's: 1 if a target ended by itself in that pass, 0 otherwise. A target that ended in an earlier pass, before the user moved on with Esc, does not count: the user already saw its end report and chose to continue.

## Risks / Trade-offs

- [Esc, then picking an eBPF target seconds later, is close to the parked eBPF start-hang case ("attaches seconds after another one stopped").] → That bug was seen only at full benchmark load; dev servers are mostly idle. The manual test repeats Esc → pick → stream many times with eBPF in WSL2 and records any hang.
- [Typed characters are no longer echoed during a stream; a user who types notes into the terminal loses that.] → Enter keeps its "spacer" use. The README mentions it.
- [The terminal stays without echo after SIGKILL.] → `ISIG` and `OPOST` are kept, so it stays usable; README says `stty sane`.
- [A slow remote terminal (SSH with high latency) may split an arrow-key sequence across more than 30 ms, so it could look like Esc.] → Rare on local terminals; the cost is only an unwanted return to the picker. The delay is one constant, easy to raise.
- [Splitting `RunE` into passes can change the single-run behavior by accident.] → The existing main, session and E2E tests run unchanged; a test checks that a run with stdin not a terminal behaves exactly as before.

## Open Questions

None.
