## Why

Specter's one job is to show what a server writes, and today it drops logs without saying so. Tests against real `strace` output on 2026-11-02 measured the following:

| Target | Wrote | Specter captured |
|---|---|---|
| 1 thread | 3,000 lines | 3,000 |
| 4 threads | 12,000 lines | 9,600 |
| 4 worker processes | 12,000 lines | 231 |

The parser was written against an `<unfinished ...>` format that `strace` does not print, so every write that overlaps another one is lost. In one run, 11,877 of 12,000 writes from 4 processes arrived in that form. Worker pools such as Gunicorn, Puma and Node cluster are a headline use case in the README, and they are hit hardest.

A user who sees no error assumes they are seeing everything, so the missing line may be the error they came to find. Trust in the output has to be fixed before new features are added.

## What Changes

- Parse the real `<unfinished ...>` and `<... write resumed>` format, so overlapping writes from threads and worker processes are captured.
- Trace `writev` as well as `write`. Writes made with `writev` currently produce no output at all.
- Raise the `strace` capture limit from 8192 to 65536 bytes, which is what the existing "lossless up to 64 KB" requirement promises.
- When a write is still larger than the limit, keep the line boundary and show how many bytes were cut. Today the rest of the write and its newline vanish, so the next line is glued on: 14 writes of 20,000 bytes arrived as a single line.
- Keep reading `strace` output when one of its lines is too long. Today that ends the read loop.
- Tell the user, on stderr, when the target writes to its own log file, and when nothing has been captured for a while.
- **BREAKING**: `--level` with an unknown value is now an error. It is silently ignored today, so `--level banana` shows everything.
- Print a notice when `--grep` is not a valid regex and is being matched as literal text.

## Capabilities

### New Capabilities

- `capture-notices`: Status messages that explain missing output, such as a target logging to its own file or no output being captured yet.

### Modified Capabilities

- `strace-source`: Changes the `strace` arguments, replaces the unfinished and resumed format, and adds `writev` decoding, visible truncation and resilience to oversized `strace` lines.
- `log-filter-renderer`: Adds validation of `--level` and a notice for a `--grep` pattern that is not a valid regex.

## Impact

- **Code:** `internal/source/strace/parse.go`, `internal/source/strace/spawn.go`, `internal/pipeline/filter.go`, `cmd/specter/main.go`.
- **Tests:** The parser tests for unfinished and resumed calls encode the format `strace` does not print, so they get replaced with captured real output. New E2E coverage compares lines written against lines captured for concurrent workers.
- **Docs:** The README's `trace=write` description, and a note on the truncation marker.
- **Dependencies:** None.
- **Compatibility:** Output changes only where logs were previously lost, plus the `--level` error above. Status messages stay on stderr, so `specter 3000 | grep 500` keeps working.
