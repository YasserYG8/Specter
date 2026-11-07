## Context

How Specter learns that its target ended today:

- **strace source:** `readLoop` stores the last line containing `+++ exited with ` or `+++ killed by ` in `processStatus`, from any PID. After the output loop, `main.go` prints `Target <that line>` if it is set and the context was not canceled, and otherwise `Detached from PID N cleanly.`
- **file source:** it tails the target's log file with a poll loop and never looks at the process, so it never ends on its own.
- **journal source:** `journalctl -f -n 0` with `-u <unit>` or `_PID=<pid>`. It ends only when `journalctl` ends.
- **docker source:** `docker logs -f` ends when the container stops, and then `main.go` prints `Detached from container X cleanly.`
- Specter's own exit status is 0 in every one of these cases.

A WSL test with strace 6.8 showed that `-qq` hides `+++ exited with N +++` but still prints `+++ killed by SIG +++`. That's why kills were reported and normal exits were not. With `-q` both lines are printed. The test also showed that the file source keeps running after the target exits.

## Goals / Non-Goals

**Goals:**
- Whatever the source, when the target ends on its own, Specter stops within about a second and says how it ended.
- The exit code or signal is shown whenever it can be known.
- "Detached cleanly" means one thing: the user interrupted.
- A script can tell the two apart by the exit status.

**Non-Goals:**
- Following restarts, or waiting for the port to come back. That's stage 2. This change gives it the signal it needs.
- Reporting workers that keep running after their supervisor exits. strace keeps tracing them until they end, and the report then names the target's own exit.
- An exit code for the file and journal sources. A process that is not Specter's child cannot be asked for its exit code after it's gone, so those sources report `exited.` without a code.

## Decisions

### 1. `-q` instead of `-qq`, with exit lines filtered by PID

A single `-q` still hides attach and detach chatter, and keeps both kinds of exit line. Under `-f`, every helper, worker and thread also prints an exit line, and today's "last line wins" would sometimes report a helper as the target. The parser already resolves `[pid N]` prefixes, so exit lines are parsed there into a structured status (`code` or `signal`), and the source keeps only the one for `target.PID`.

strace drops the `[pid N]` prefix when only one process is traced. The first version of this change took every unprefixed line as the target's, and a real test showed the flaw: with a target and a child, the target's exit line has a prefix (`[pid 1319] +++ exited with 3 +++`), and then the child that is left last prints `+++ exited with 0 +++` with none, which replaced the target's code 3 with 0. The rule is now: an unprefixed line is the target's only while the target's own end has not been recorded. A target that is traced alone has no earlier status, so its unprefixed line still counts.

Alternative: keep `-qq` and rely on the watcher alone. That loses the exit code for the source that can actually provide it.

### 2. One process watcher for all host-PID sources

A small `procwatch` component takes the proc root, the PID, and a poll period (1 s). At start it records the process start time (field 22 of `/proc/<pid>/stat`). On each tick, the target counts as gone when `/proc/<pid>/stat` is missing, when its state is `Z` or `X`, or when its start time differs, which means the PID was reused. When it's gone, the watcher cancels a context that `main.go` passes to the source and the pipeline, with a cause that records "target exited".

It runs for strace, file, and journal by `_PID`. It does not run for journal by unit, where systemd restarts are expected and `journalctl -u` already follows them, or for Docker, where the container is not a host PID that Specter tracks.

For strace, the watcher is a safety net. strace normally ends first, with a status. If the watcher fires first, its cancel makes strace detach. The final report then prefers strace's status if one arrived, and otherwise says `exited.`

Alternative: a separate check after the fact, run only when the stream ends. That can't help the file source, whose stream never ends.

### 3. One place that writes the final line

`main.go` gets a small `reportEnd` function that decides between:

1. the user interrupted → `Detached from PID N cleanly.` / `Detached from container X cleanly.`, status 0
2. strace status → `Target PID N exited with code C.` or `Target PID N was killed by SIG.`, status 1
3. container ended → `Container X exited with code C.`, or `Container X stopped.`, status 1
4. the watcher fired, no status → `Target PID N exited.`, status 1

"The user interrupted" is read from the signal handler's context, not from `ctx.Err()` alone, because the watcher's cancel also sets `ctx.Err()`.

For SIGKILL, a second line: `SIGKILL often comes from the kernel's out-of-memory killer. To check: dmesg | grep -i oom`.

### 4. Exit status 1 when the target ended

The end of the target is not a crash of Specter, but the thing the user was watching is gone, and that's what a script wants to know. Mirroring the target's own exit code was considered. It would be ambiguous: code 1 from Specter could mean "target exited 1" or "Specter failed". A single fixed status is clearer, and the message carries the details.

This is a breaking change for scripts that expect 0. Specter has no release yet.

### 5. Docker exit code

When `docker logs -f` ends without the user interrupting, run `docker inspect -f '{{.State.Status}} {{.State.ExitCode}}' <container>` once, with the same command runner and context timeout the docker source already uses. `exited N` gives the code. Anything else, or an error, gives `stopped.`

### 6. No `ptrace_scope` warning for root

Check `os.Geteuid() == 0` in the caller before printing the warning, and skip it at scope 1 and 2. On Windows `Geteuid` returns -1, so the code stays portable. `CheckPtracePermissions` keeps its current behavior, and a parameter or wrapper makes the root case testable.

## Risks / Trade-offs

- [`-q` prints many more exit lines on busy servers that spawn helpers] → They are short, and they are parsed in the same loop as writes. Their volume is tiny next to the writes themselves.
- [Polling `/proc` every second] → One small file read per second. Detection takes up to a second, which is fine for this purpose.
- [The watcher fires during a burst of final output] → After the cancel, the pipeline gets a short grace period to drain what was already read before exiting, so the last lines, often the crash message, are not lost. The source and assembler already flush on close, and the grace period is bounded (about 500 ms).
- [The exit status change breaks a script] → No release exists. CHANGELOG under Breaking.

## Migration Plan

No user action. The CHANGELOG notes the exit status change.

## Open Questions

None.
