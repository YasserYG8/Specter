## Context

`Parser.ParseLineDest` returns early for any descriptor other than 1 and 2: it drops pending state for the PID, calls `OnOtherFD` (which drives the "writes to its own log file" notice), and emits nothing. Since `ignore-private-child-writes`, strace runs with `-y`, and the parser already decodes each call's destination (`decodeDest`). `Source` reads the target's fd 1 and fd 2 destinations once at start (`readStreamRefs`) and uses them in `shouldEmit` to drop writes from other processes that go elsewhere.

A Next.js 16 dev server run in a terminal wrote its request lines to fd 22, which pointed to the same `/dev/pts/1` as fd 1 and 2. This is libuv behavior: for a TTY, `uv_tty_init` opens the terminal again to get a descriptor it can make non-blocking, and Node writes stdout and stderr through those descriptors. Specter attaches after startup, so it never sees which reopened descriptor came from stdout and which from stderr.

Downstream, `filter.go` checks only `entry.FD != 2` for `--stderr`, and the renderer and assembler key on `(PID, FD)` without assuming the value is 1 or 2.

## Goals / Non-Goals

**Goals:**
- Output that reaches the target's own terminal, pipe or socket is shown, whatever descriptor number carries it.
- No new false output: a descriptor that leads somewhere else stays ignored.
- No change to the pipeline.

**Non-Goals:**
- Telling stdout from stderr for reopened descriptors. That information is gone by the time Specter attaches.
- Following other places a program writes to, such as its own log files. The existing notice covers those.
- Changing the rules for processes other than the target. `shouldEmit` already applies to them.

## Decisions

### 1. The parser asks a callback whether a destination is output

Add `IsOutputDest func(dest string) bool` to `Parser`, next to `OnOtherFD`. For a descriptor other than 1 and 2, the parser calls it with the decoded destination. If it returns true, the call is parsed like fd 1 and 2, keeping its real descriptor number, including the `<unfinished ...>` path. If it returns false, or the destination is empty, the current early return runs, so `OnOtherFD` and the log-file notice behave as before.

`Source` sets the callback from `s.refs`: true when `dest != ""`, `dest != "/dev/null"`, and `dest` equals `refs[0]` or `refs[1]`.

Alternative: return every write with its destination and let `Source` decide. That would decode payloads of every socket and eventfd write. Next.js wrote 23 eventfd writes for one request, and the parser would hex-decode payloads that are thrown away.

### 2. Keep the real descriptor number

The chunk carries `FD: 22`. The assembler keys line buffers on `(PID, FD)`, so a partial line on fd 1 and a line on fd 22 never merge. The filter treats it as stdout because it isn't 2, and the renderer styles it as stdout. No pipeline change is needed.

Alternative: rewrite the number to 1 or 2. That would merge buffers, and it would have to guess the stream when both lead to one terminal.

### 3. Exclude `/dev/null`

When the target's stdout is `/dev/null`, other descriptors pointing there are almost always intentional discards, such as a library opening `/dev/null` to silence a child. The target's own fd 1 and 2 writes there are still shown, as before.

### 4. Other processes

A child's write to fd 22 that leads to the target's terminal passes `IsOutputDest` and then `shouldEmit`, which returns true because the destination equals a reference. That's the same answer as for a child writing to fd 1 on that terminal: it reaches the user's screen.

## Risks / Trade-offs

- [`--stderr` hides Node's `console.error` when the server runs in a terminal] → It's a documented limit. Running the server with stderr redirected separately (for example `2>err.log`, or behind a pipe) gives Specter distinct destinations again.
- [Something writes to the terminal that the user would not call output, such as a progress bar on a reopened `/dev/tty`] → It appears on the user's screen too, so showing it is consistent with "what reaches your terminal". A reopened `/dev/tty` has a different path from `/dev/pts/N` and is not matched anyway.
- [`readStreamRefs` failed for both streams] → `IsOutputDest` returns false for every other descriptor, which is today's behavior.

## Migration Plan

No user action. Output from Node servers in a terminal appears where it did not before.

## Open Questions

None.
