## Context

The pipeline is `LineAssembler → StackTraceGrouper → output loop (Filter, Renderer)`. The grouper keeps a `streamRecord` per `(PID, fd)` and sends every line out as soon as it arrives, as its own `Entry`, labeled with what it inherits from its record (`Level`, `IsTrace`, `IsFrame`, `IsRecordContinuation`). The filter keeps its own `activeMatch` per `(PID, fd)`: a header that matches `--grep` lets the following continuation lines through, and a continuation line that matches lets itself and the later ones through.

Since nothing is held, a match can only reach forward. A test with the current code gave:

| Input | `--grep` | Lines shown |
|---|---|---|
| Python traceback, 4 lines | `ZeroDivision` (last line) | 1 |
| same | `test_server` (frame) | 3, without the header |
| same | `Traceback` (header) | 4 |
| Python chained traceback, 9 lines | `request failed` (last line) | 1 |
| Java trace, 3 lines | `db timeout` (header) | 3 |
| Node trace, 3 lines | `db timeout` (header) | 3 |

Python is the worst case because its message is last, and `test_server.py`, the server the README suggests for a first try, writes a Python traceback on `GET /error`.

## Goals / Non-Goals

**Goals:**
- With `--grep`, a stack trace is output whole when any line of it matches, and not at all otherwise.
- Without `--grep`, behavior and timing stay exactly as today.
- Memory stays bounded, whatever the target writes.

**Non-Goals:**
- Keeping a chained Python traceback ("During handling of the above exception...") together as one block. The grouper already ends the trace at the first trailer line, so each part of the chain is its own block. A match in the last part shows the last part. This is still much better than one line, and joining the parts belongs to the grouper, not to filtering.
- Holding multi-line records that are not traces, such as .NET two-line records. They keep line-by-line inheritance.
- Changing `--level` or `--stderr`.

## Decisions

### 1. A separate stage, only when `--grep` is set

A new channel stage, `TraceHolder`, sits between the grouper and the output loop. `main.go` adds it only when `--grep` is set, so the path without `--grep` doesn't change at all.

Alternatives:
- **Buffer inside the grouper.** The grouper's promise is "zero buffering", and its labels don't depend on `--grep`. Mixing the two makes both harder to test.
- **Buffer inside `Filter.Matches`.** `Matches` answers yes or no for one entry, and holding needs a way to emit several entries later and a timer. The output loop in `main.go` would have to change shape too.

A stage with its own goroutine fits the existing pattern: `LineAssembler` already uses a ticker for its idle timeout.

### 2. What the stage does

Per `(PID, fd)` key it either passes entries through or holds one trace:

```
 entry with IsTrace on a key with nothing held, not a continuation → start holding
 entry that continues the held trace (IsFrame or IsRecordContinuation, same key) → add to it
 entry on the same key that does not continue it → release the held trace, then handle the entry
 entry marked EndsTrace → add it, then release
 no entry on that key for 200 ms → release
 1000 lines held → release
 input closed → release everything held
 any entry on another key → pass through at once
```

"Release" means: if any held line matches the pattern, every held entry is sent with `MatchedGrep = true`. If none matches, they are sent unchanged, and the filter rejects them line by line, as it does today, because none of them matches.

### 3. The grouper marks the end of a Python trace

The stage can't tell a trailer from a frame by itself, and copying the grouper's regexes would split one rule into two places. The grouper already knows, because it sets `rec.inTrace = false` on the trailer line. It now also sets a new `Entry.EndsTrace` field on that entry. For Java, Node and Go there is no closing line, so the next non-continuation line or the 200 ms timeout ends the trace.

### 4. The filter honors `MatchedGrep`

In `Filter.Matches`, an entry with `MatchedGrep` passes the grep check and sets `activeMatch[key] = true`, so lines after a 1000-line cap keep inheriting the match. `--stderr` and `--level` are checked first, as today. The field already exists on `Entry` and in `streamRecord`, where it's never set. The `streamRecord` copy is removed.

### 5. 200 ms and 1000 lines

- **200 ms** is `DefaultTraceIdleTimeout`, which already exists and is unused. A stack trace is written in one burst, usually within microseconds, so 200 ms is a long margin. A person can barely notice a 200 ms delay, and it only happens with `--grep` and only for traces.
- **1000 lines** covers normal traces in every language. Java traces with many `Caused by:` sections reach a few hundred lines. Go goroutine dumps can have thousands, and those are the case the cap is for. Each line is at most 64 KB, so the worst case held per stream is bounded.

### 6. Order between streams

Holding one key's trace while other keys flow means output order between processes can change by up to 200 ms. Within one process and stream, order is kept. This matches the idea that Specter is a live view: other streams should not wait for a trace that may never end.

### 7. Cleanups in the same files

- Remove `streamRecord.matchedGrep`, which is never set.
- Remove the mutex in `Group`, because `records` is used by one goroutine only.
- Keep the mutex in `Filter`: `Matches` is public, and the lock costs nothing measurable.

## Risks / Trade-offs

- [A line that looks like a frame but belongs to the next record, for example indented SQL after a trace] → It would be held as part of the trace, but the grouper already makes that choice today. The holder only follows the grouper's labels.
- [A trace is cut by an idle gap longer than 200 ms in the middle] → Each part is matched on its own, which is the same as today's line-by-line result or better.
- [The output loop counts "no output" before filters] → `sawEntry()` is called when an entry reaches the loop. While a trace is held for up to 200 ms, nothing reaches the loop, but the notice only fires after 10 seconds, so this doesn't matter.
- [Ctrl+C during a held trace] → The context is canceled, and held lines may not be printed. This is the same as lines still in flight today.

## Migration Plan

No user action. `--grep` output for stack traces gets longer, never shorter, when a trace matches.

## Open Questions

None.
