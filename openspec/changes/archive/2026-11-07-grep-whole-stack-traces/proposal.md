## Why

`--grep` shows only part of a stack trace when the match is not on its first line. A Python traceback puts the exception message on its last line, so `specter 8080 -g TypeError` against the bundled `test_server.py` prints the `raise` line and the `TypeError: ...` line, without the header and the frames that say where it happened. A match on a frame has the same problem in every language: `-g test_server` prints the trace from that frame on and drops the header. This was confirmed with the current grouper and filter. The grouper sends every line out at once, so the filter cannot go back to lines it has already rejected.

## What Changes

- While `--grep` is set, Specter holds the lines of each stack trace until the trace ends, and then outputs the whole trace if any of its lines matches, or none of it if no line matches.
- A trace ends when its Python trailer line arrives, when the same stream writes a line that does not continue it, when the stream is quiet for 200 ms, or when the stream ends.
- A trace longer than 1000 lines is matched on its first 1000 lines, and the lines after that follow the current line-by-line rules, so a huge goroutine dump cannot pile up in memory.
- Without `--grep`, nothing is held, and output stays immediate as it is today.
- While a trace is held, lines from other processes and streams keep flowing, so they may print before the trace that was written first.
- Small cleanups in the same files: the grouper's unused `matchedGrep` field and the locks in the grouper and filter that only one goroutine uses.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `log-filter-renderer`: "Atomic Multi-Line Filter Matching" changes from line-by-line inheritance to whole-trace matching for stack traces while `--grep` is set.

## Impact

- `internal/pipeline`: a new channel stage between the grouper and the output loop, used only when `--grep` is set; the grouper marks the line that closes a Python trace; the filter admits entries that the new stage marked as part of a matching trace.
- `cmd/specter/main.go`: wires the new stage when `--grep` is set.
- Tests in `internal/pipeline` for Python, Java, Node and Go traces, matches on the header, a frame and the trailer, the timeout and the size cap.
- `CHANGELOG.md` (Fixed). The README `--grep` description if it says how traces are matched.
- No change to sources, to output without `--grep`, or to `--level` and `--stderr`.
