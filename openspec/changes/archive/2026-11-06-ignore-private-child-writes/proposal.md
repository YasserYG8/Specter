## Why

Specter shows lines that never reach the server's real output. On a Next.js dev server (port 3000 in a real project), 12 of the captured lines came from helper processes such as `get-registry.js`, `get-pkg-manager.js` and `telemetry/project-id.js`. Those helpers write to their own stdout, which is a private pipe back to Next.js or `/dev/null`, but `strace -f` reports every write to fd 1 or 2, so Specter prints them as if the server had logged them. This breaks trust in the output, and it must be fixed before multi-server streaming, where the fake lines would carry a server tag and look even more real.

## What Changes

- Specter only outputs a write from a process other than the target when the descriptor it writes to leads to the same place as the target's own stdout or stderr (the same pipe, terminal, socket or file).
- Writes to fd 1 or 2 that lead somewhere else, such as a private pipe to a parent process or `/dev/null`, are dropped. This includes output that a parent reads from a child and prints again, which is then shown once instead of twice.
- Writes from the target process itself are always output, even when its stdout is `/dev/null`, because showing output that goes nowhere is part of what Specter is for.
- The destination of each descriptor is taken from strace at the moment of the call (the `-y` flag), so short-lived helpers that exit before Specter could look at `/proc` are still classified correctly.
- When the destination cannot be determined, the write is output. Losing a real line is worse than showing a rare extra one.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `strace-source`: strace is spawned with `-y`, the parser reads the descriptor path that strace prints, and a new requirement defines which writes from processes other than the target are output.

## Impact

- `internal/source/strace/spawn.go`: `BuildStraceArgs` adds `-y`; at start the source reads the target's fd 1 and fd 2 destinations from `/proc/<pid>/fd`; the decision whether to emit a chunk.
- `internal/source/strace/parse.go`: `callStartRegex` and the write and writev parsing accept `write(1<pipe:[123]>, ...)`, and the descriptor path is kept for unfinished calls until they resume.
- `internal/source/strace/*_test.go`: parser tests use real `-y` output captured from strace; tests for the emit rule.
- `test/e2e`: a test where the target starts a child whose stdout is a private pipe, and a child that shares the target's stdout.
- Docker, journal and file sources are not affected.
- `CHANGELOG.md` (Fixed) and the strace section of `README.md` if it describes which processes are captured.
