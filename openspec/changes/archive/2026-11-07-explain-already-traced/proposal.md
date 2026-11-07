## Why

Linux allows one tracer per process. When the target is already traced, by another Specter session, a plain `strace`, or a debugger, strace fails with "Operation not permitted", and Specter reports it as a permission problem:

```
Error: Cannot attach to PID 773: Operation not permitted: The kernel blocked ptrace attachment. Process may belong to another user or ptrace is restricted.
Fix: Run Specter with sudo: sudo specter --pid 773 (or setcap cap_sys_ptrace+ep on strace)
```

This happened in a real test on a Next.js server while a second Specter, running as root, tried to attach. The real cause was in `/proc/773/status`: `TracerPid: 844`, a `strace` started by another `specter`. The advice to use `sudo` points the user the wrong way, and the exit status was not consistent across runs. Multi-target attach (`specter 3000 4000`) will make this more likely, since two targets can share a process.

## What Changes

- Before starting strace, Specter reads `TracerPid` from `/proc/<pid>/status` for the target. If another process traces it, Specter stops with a diagnostic that names the tracer, its command, and, when the tracer's parent is a `specter` process, that Specter's PID. It also says that only one tracer is allowed and how to free the process. Specter does not signal the other tracer.
- Discovered workers that are already traced are left out of strace's `-p` list, with one notice naming them and their tracer, so their logs are known to be missing.
- If strace still fails with "Operation not permitted" because a tracer appeared between the check and the attach, Specter reads `TracerPid` again and shows the same "already traced" diagnostic instead of the permission advice.
- Failing to attach always ends with a non-zero exit status.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `strace-source`: a new requirement for targets and workers that are already traced.

## Impact

- `internal/source/strace`: a small `tracerOf(procRoot, pid)` helper that reads `TracerPid`, the tracer's command line, and its parent's name; the check before strace starts; filtering traced workers out of the `-p` arguments; and the re-check in the "Operation not permitted" path.
- `cmd/specter/main.go`: only if the exit status after an attach error turns out not to be non-zero.
- Tests with a fake proc tree, plus an E2E test that runs two Specters against one test server.
- `CHANGELOG.md` (Fixed).
