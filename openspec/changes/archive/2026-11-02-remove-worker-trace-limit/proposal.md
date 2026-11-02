## Why

Specter traces only the first 16 worker processes it finds, and says nothing about the rest. Its startup banner lists every worker, so it looks as if all of them are being watched. Worker pools are a headline use case in the README, and a log line from an untraced worker is lost with no message.

Measured on 2026-11-02 under WSL2 with strace 6.8, with 20 workers already running before Specter attached, each writing 50 lines:

| Tool | Workers with all their lines | Lines captured |
|---|---|---|
| Specter today | 16 of 20 | 800 of 1,000 |
| `strace` with one `-p` per worker | 20 of 20 | 1,000 of 1,000 |

The code comment says the limit avoids exceeding `strace` argument constraints. That doesn't hold up: `strace` captured every line with 500 workers, using about 4.5 KB of arguments.

## What Changes

- Remove the limit of 16 in `BuildStraceArgs`, so every discovered worker is traced.
- Keep the existing removal of duplicates and invalid PIDs.
- Add a test with more than 16 workers already running before Specter attaches. The current worker E2E test starts its workers after attaching, where `strace -f` follows them, so it can't reach this limit.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `strace-source`: Attachment covers every discovered worker with no upper limit, and a worker that has already exited doesn't stop the capture.

## Impact

- **Code:** `internal/source/strace/spawn.go`.
- **Tests:** The `caps children at max 16` case in `internal/source/strace/spawn_test.go` is replaced, because it encodes the bug. `test/e2e/testserver` gains a way to start workers before Specter attaches, and `test/e2e/capture_test.go` gains a test that uses it.
- **Docs:** A `CHANGELOG.md` entry.
- **Dependencies:** None.
- **Compatibility:** Output changes only where logs were being lost. On servers with more than 16 workers, every worker is now slowed while Specter is attached, instead of only the first 16.
