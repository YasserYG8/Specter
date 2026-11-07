## Context

`Source.Start` in `internal/source/strace/spawn.go` runs strace with one `-p` per target and worker. strace's error lines are stored in `capturedError`, which keeps only the last one. When strace exits with an error and the last line contains "Operation not permitted", Specter returns `ErrPtraceDenied` with advice to use `sudo` or `setcap`.

A real test showed the wrong diagnosis. The target `next-server` (PID 773) was already traced by `strace` PID 844, which another `specter` (PID 825) had started. A second Specter, running as root, got the `sudo` advice. The exit status was 1 in one run and 0 in another, and the cause wasn't found then. Since strace runs with several `-p` arguments, it may keep running when only the target fails to attach, and `capturedError` may hold a worker's line rather than the target's.

The kernel exposes the current tracer as `TracerPid:` in `/proc/<pid>/status`, readable by anyone who can read that file.

## Goals / Non-Goals

**Goals:**
- An already traced target is reported as exactly that, with who traces it, before strace runs.
- Workers that are already traced are reported, not silently missing.
- No `sudo` advice when the cause is another tracer.
- A non-zero exit status after any failure to attach.

**Non-Goals:**
- Stopping or detaching the other tracer. Specter never signals other processes.
- Sharing one trace between two Specter sessions.
- Changing the `ptrace_scope` warning.

## Decisions

### 1. Check before starting strace

A helper `tracerOf(procRoot, pid) (tracer int, cmd string, parentName string, parentPID int)` reads `/proc/<pid>/status` for `TracerPid`, then `/proc/<tracer>/cmdline` (base name of the first argument, plus the rest, cut to about 60 characters) and the tracer's `PPid` and `/proc/<ppid>/comm`. Any read that fails leaves its field empty, and the diagnostic uses what is known.

`Start` calls it for the target first. A non-zero tracer returns a `DiagnosticError` wrapping a new `util.ErrAlreadyTraced`, so tests and `main.go` can tell it apart from `ErrPtraceDenied`.

Alternative: only parse strace's error after the fact. That depends on the text of strace's message and on `capturedError` holding the target's line, which isn't guaranteed with several `-p` arguments.

### 2. Workers

`BuildStraceArgs` stays pure. `Start` filters `target.Children` through `tracerOf` before building the arguments, and sends one notice through the existing notifier: `Worker PID 1039 is already traced by PID 1500; its output is not captured.` The notice lists all of them in one line when there are several.

### 3. Re-check on "Operation not permitted"

There is a small window between the check and strace's attach. In the "Operation not permitted" path, call `tracerOf` for the target again. A non-zero tracer gives the "already traced" diagnostic, and zero keeps the existing permission diagnostic.

### 4. Message

```
PID 773 is already being traced by PID 844 (strace -f -qq -xx -s 65536 -e trace=wr...).
Linux allows only one tracer per process. PID 844 was started by specter (PID 825).
Stop the other Specter first (Ctrl+C in its terminal), then run this command again.
```

Without a `specter` parent, the last two lines become `Linux allows only one tracer per process.` and `Stop or detach the other tracer (PID 844) first, then run this command again.`

### 5. Exit status

The pre-check returns before any goroutine runs, so the error goes through `RunE`'s normal return, which is status 1. The task list includes checking the post-attach path, which goes through the `errs` channel, and fixing it if it can end with status 0.

## Risks / Trade-offs

- [`TracerPid` is not readable, for example under a hardened `/proc`] → The check is skipped and today's path runs, so nothing gets worse.
- [The tracer exits between the check and the attach] → strace attaches normally. There's no false error, because the check only blocks on a non-zero tracer at that moment.
- [Spec work runs in parallel with `capture-reopened-terminal-fds`, which also edits `spawn.go`] → The two changes touch different functions (`Start` before strace runs vs. the parser callback). Apply them one after the other, not at the same time.

## Migration Plan

None.

## Open Questions

None.
