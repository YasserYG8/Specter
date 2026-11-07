## Why

When the server Specter is watching stops, Specter often says nothing true about it. A test in WSL with strace 6.8 showed:

| How the target stopped | Source | What Specter printed |
|---|---|---|
| `kill -9` | strace | `Target +++ killed by SIGKILL +++` |
| `kill -TERM` | strace | `Target +++ killed by SIGTERM +++` |
| normal exit with code 3 | strace | `Detached from PID 617 cleanly.` |
| normal exit with code 3 | file | nothing: Specter kept waiting until it was interrupted, then printed `Detached from PID 595 cleanly.` |

Two causes: strace runs with `-qq`, which hides `+++ exited with N +++` lines but not `+++ killed by` lines, and the file source reads the target's log file without watching the process at all. A crashed server that Specter reports as "detached cleanly", or not at all, breaks the promise that Specter tells you why it shows nothing. The same detection is needed later for "port 4000 stopped" in multi-streaming and for following restarts.

## What Changes

- strace runs with `-q` instead of `-qq`, so normal exits are reported. Only the exit line of the target PID counts, so a helper or worker that exits does not get reported as the target.
- A process watcher runs alongside every source that follows a host PID (strace, file, and journal when it follows `_PID`). It checks once per second whether the target still exists, treating a zombie or a reused PID as gone. When the target is gone, Specter stops reading and reports it, even if the source itself would keep waiting.
- The final line names what happened in plain words: `Target PID 617 exited with code 3.`, `Target PID 595 was killed by SIGKILL.`, or `Target PID 595 exited.` when the exit code is unknown. `SIGKILL` adds a hint that it often comes from the kernel's out-of-memory killer, with the command to check.
- For Docker, when `docker logs -f` ends without the user interrupting, Specter reports `Container <name> exited with code N.`, using `docker inspect`, or `Container <name> stopped.` if the code can't be read.
- "Detached from ... cleanly." is printed only when the user interrupted Specter.
- **BREAKING**: Specter exits with status 1 when the target ended on its own, and 0 only when the user interrupted it. Today it exits with 0 in both cases.
- The `ptrace_scope` warning is not shown when Specter runs as root, since root can attach at scope 1 and 2.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `capture-notices`: a new requirement for reporting how the target ended, and the exit status.
- `strace-source`: `-q` replaces `-qq` in the attachment requirement, exit lines are taken from the target PID only, and the `ptrace_scope` warning is skipped for root.

## Impact

- `internal/source/strace/spawn.go`: `-q`, and exit lines filtered by PID and parsed into a structured status.
- A new small package or file for the process watcher, using the injectable proc root so it is testable on Windows.
- `internal/source/docker`: the container state after the log stream ends.
- `cmd/specter/main.go`: a context that the watcher can cancel, the final message, the exit status, and the root check for the `ptrace_scope` warning.
- `internal/source/strace/permissions.go` or its caller: skip the warning for root.
- Tests, `CHANGELOG.md` (Fixed and Breaking), and the README if it describes what happens when the target stops.
- Not changed: journal by systemd unit. systemd restarts the unit under a new PID and `journalctl -u` keeps following it, which is already the right behavior.
