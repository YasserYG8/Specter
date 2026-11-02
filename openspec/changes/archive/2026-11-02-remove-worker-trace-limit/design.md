## Context

Workers that exist before Specter attaches need their own `-p` argument. `main.go` discovers them with `FindChildPIDs`, or `FindChildProcessTree` under `--tree`, and `BuildStraceArgs` stops adding them after 16. The banner in `main.go` prints the full list, so it overstates what is traced.

Workers started after attaching aren't affected, because `strace -f` follows forks. This is why the worker E2E test added in `stop-losing-logs-silently` passes with 12,000 lines and still can't catch this bug.

These results come from real `strace` 6.8 under WSL2 on 2026-11-02, with test processes that opted in through `PR_SET_PTRACER`, so `ptrace_scope` stayed at 1:

| Workers | `-p` arguments | Argument size | Lines captured |
|---|---|---|---|
| 20 | 21 | about 190 bytes | 1,000 of 1,000 |
| 100 | 101 | about 900 bytes | 5,000 of 5,000 |
| 500 | 501 | about 4.5 KB | 25,000 of 25,000 |

Attaching finished in under 4 seconds for 100 workers and under 9 seconds for 500. These are upper bounds set by when the test workers started writing, not measured attach times.

A PID that no longer exists prints `strace: attach: ptrace(PTRACE_SEIZE, N): No such process`. `strace` keeps tracing the other processes and exits 0 when they finish. `readLoop` already stores that line as `capturedError` and carries on, and it only reports the stored error if `strace` exits with a failure.

## Goals / Non-Goals

**Goals:**

- Trace every worker Specter discovers.
- Make the startup banner accurate.
- Cover the bug with a test that has workers running before Specter attaches.

**Non-Goals:**

- A warning that tracing slows the target. Specter already slows every process it traces without saying so, so a warning belongs on every `strace` attach and is a separate change.
- Discovering workers that are not found today, such as grandchildren without `--tree`, or sibling workers when the port resolves to a worker rather than the main process. The second case may be a larger gap and needs its own investigation.
- Changing the `--tree` depth limit of 3.
- Reducing the slowdown caused by `strace`.

## Decisions

### Remove the limit rather than raise it

Any fixed number brings the same silent loss back at a larger pool size, and the limit's stated reason doesn't hold: 500 workers used about 4.5 KB of arguments, far below the Linux argument limit of about 2 MB.

*Alternative considered:* a higher limit with a notice. This keeps a code path and a notice for a limit that nothing requires.

*Alternative considered:* keeping 16 to protect busy servers from slowdown. This trades lost logs for speed without telling the user, and Specter exists to show the logs.

### Let `strace` handle workers that exit during attach

No new code handles this, because `strace` already skips a missing PID and keeps tracing the others. A unit test pins Specter's side of it: a `strace: attach:` line must not stop later writes from being parsed.

### Start test workers before attaching, and release them with a file

`test/e2e/testserver` gets a flag that starts N workers before it prints its ready line, so Specter's discovery finds them. Each worker waits for a file to appear and then writes its numbered lines. The test creates the file after Specter has attached.

*Alternative considered:* having workers sleep for a fixed time. That would make the test depend on how fast the machine attaches.

## Risks / Trade-offs

- [Every worker on a large pool is slowed while attached, not just 16] → Accepted: this matches how Specter treats smaller pools today, and the slowdown ends on detach. It is listed in the compatibility notes.
- [One `strace` process handles every tracee, so very large pools may lag] → Measured clean up to 500 workers writing 50 lines each. Sustained load at that scale is untested.
- [A failed attach line remains in `capturedError`] → If `strace` later fails for another reason, the message could mention the wrong PID. This already happens today and is out of scope.

## Migration Plan

Nothing to migrate. To roll back, revert the change.

## Open Questions

None block this change. The sibling-worker question in Non-Goals is the most useful follow-up.
