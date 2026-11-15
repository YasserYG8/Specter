# Esc back to the picker, manual checks, WSL2, 2026-11-15

Made for the `esc-back-to-picker` change. The real `specter` binary was run in a pseudo-terminal that a script drove like a person at a keyboard (it sends keys, reads the screen, and checks `/proc/<pid>/status` and the terminal settings). Machine: AMD Ryzen 5 3500U, WSL2 on Windows 11, kernel 6.6.87.2-microsoft-standard-WSL2, run as root, `ptrace_scope=0`.

## Esc, pick, Esc, pick, Ctrl+C

`specter P1 P2`, Esc, pick one service, Esc, pick the other, Ctrl+C. Run on two test servers with `--source ebpf` and with `--source strace`, and on two real dev servers (`:4200` Angular and `:8082` Spring Boot, default engine, which was eBPF).

| Check | Result |
|---|---|
| Key hint `Press Esc to pick another service, Ctrl+C to quit.` printed after attaching | yes |
| Esc prints `Detached from PID N cleanly.` for each target, then erases the screen and the scrollback (`ESC [H ESC [2J ESC [3J`, as the `clear` command) and opens the picker (the first text after the clear is the picker title, no old log line) | yes |
| No tracer on the targets after Esc and after Ctrl+C | yes (`TracerPid: 0`) |
| No `strace` process left (`--source strace`) | yes |
| A picked service streams, without a tag (one target) | yes |
| Typed text is not echoed; an arrow key does not open the picker | yes |
| Ctrl+C exits with status 0 and does not open the picker | yes |
| Terminal after exit: echo, canonical mode, ISIG and OPOST all on | yes |

## Esc, pick, stream: 20 times in a row (eBPF)

One Specter process with `--source ebpf`, two idle test servers, 20 times: Esc, pick the other server, wait for the new stream, request one line from it and wait to see it.

| Runs | Started | Hangs | Line captured | Time from pick to a running stream |
|---|---|---|---|---|
| 20 | 20 | **0** | 20 | 0.26 to 0.88 s (median about 0.3 s) |

This is the "attaches seconds after another one stopped" case of the parked eBPF start-hang bug, but with idle servers, not at full benchmark load, where that bug was seen. No hang showed up here, which does not show the bug is gone under load.

## Not a terminal

| Case | Result |
|---|---|
| `specter 3000 < /dev/null --duration 2s` | exits 0, attaches as before, no key hint |
| `specter 3000 --duration 3s &` in an interactive shell | not stopped by the terminal, no key hint, terminal mode unchanged, ends by itself after `--duration` |
| `specter 3000 \| cat` in a terminal | the hint is printed on stderr, Esc opens the picker, closing the picker ends Specter |
