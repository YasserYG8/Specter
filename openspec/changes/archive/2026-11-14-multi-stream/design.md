## Context

All the wiring for one run lives in the single `RunE` in `cmd/specter/main.go` (about 400 lines). It resolves one target, selects and starts one source (with the eBPF-first start and the non-strace fallback), starts the process watcher, builds the assembler, the grouper and the optional trace holder, runs the output loop, and prints one end report. Every step assumes one target.

Three facts in the code shape this design:

- **strace's parent-death signal is tied to a thread.** `spawn_linux.go` sets `Pdeathsig`, and the kernel ties it to the thread that started `strace`, not to the process. `RunE` calls `runtime.LockOSThread()` for that reason, and `strace.Source.Start` calls `cmd.Start()` synchronously. A `strace` started from another goroutine could get SIGINT when that goroutine's thread exits, and detach in the middle of the run.
- **The pipeline keys its buffers by `(pid, fd)`.** The Docker, file and journal sources stamp chunks with `target.PID`, which is 0 for a container without a host PID. Two such targets in one assembler would share buffers.
- **The grouper is the single-goroutine bottleneck** (see `reports/Specter performance improvements.md`). One grouper for all targets would put every target's lines through one core.

## Goals / Non-Goals

**Goals:**
- `specter 3000 4000` (and `--pid`, and mixed) shows every target's output in one stream, each line tagged.
- One target behaves exactly as today, byte for byte on stdout (except the timestamp change below).
- Each target keeps everything a single target has: source selection, fallback, workers, the process watcher, the end report.
- No new way to leave a tracer behind.

**Non-Goals:**
- Choosing several services in the picker.
- A project name expanding to all of its services (a project still picks one service).
- `--follow` (reattaching after a restart). The `Session` type is built so this can come later.
- A shared eBPF engine. Each session loads its own program for now (see Decision 6).
- Sorting lines by their timestamps.

## Decisions

### 1. A `Session` per target, merged after the grouper

```
  :3000 ─▶ Session A: source ─▶ assembler ─▶ grouper [─▶ trace holder] ─┐
                                                                        ├─▶ fan-in ─▶ filter ─▶ render + tag ─▶ stdout
  :4000 ─▶ Session B: source ─▶ assembler ─▶ grouper [─▶ trace holder] ─┘
```

A new `Session` type in `cmd/specter/session.go` holds one target's label, target, source, contexts (`srcCtx` and `pipeCtx`, as today), process watcher, and pipeline stages up to the grouper (and the trace holder with `--grep`). It exposes its entries channel and an end report. `RunE` becomes: validate flags → build the target list → resolve all → remove duplicates → start sessions one by one → output loop → end reports.

Merging entries, not chunks, keeps the `(pid, fd)` keys separate per session, so PID 0 targets never mix, and it gives each target its own grouper goroutine, so two busy targets use two cores. The trace holder stays in the session because it holds traces per stream; it uses the shared filter's `MatchText`, which only reads.

**Found while building:** the grouper has no buffer and sends one entry per line, not one entry per trace. Two things follow, and both are handled in the code:
- The `Filter` keeps a memory per `(pid, fd)` (whether the last record matched `--grep`, so its continuation lines are admitted). Containers have PID 0, so two of them would share it. Each session therefore has its own `Filter`, built from the same options, and its trace holder uses its own filter.
- Merging line by line could put a line of one target inside the stack trace of another. See the trace guard in Decision 4.

*Alternative:* add a stream ID to `Chunk` and run one pipeline. Rejected: it changes the key in every stage, and it puts all targets through one grouper.

`Session` stays in `cmd/specter` for now, because it uses the flags, the notices and the end report, which live there. It can move to `internal/` once `--follow` needs it elsewhere.

### 2. Start sessions one after the other, on the main goroutine

Sources are selected and started in a loop in `RunE`, which runs on the locked thread. Only reading (the source's own goroutines, the pipeline stages) runs elsewhere. This keeps `Pdeathsig` working for every `strace`. A comment at the loop and a test that `Start` is called in order guard this rule.

The eBPF-first start (`startNow`) and the non-strace fallback move into the session's start, unchanged. If any start fails, `RunE` stops the sessions already started (in reverse order) and returns the error with the label. This is the "all or nothing" rule; it covers errors returned by `Start` and errors that arrive on the error channel during the existing immediate check.

### 3. Resolve, then remove duplicates

All targets are resolved first, with the existing functions (`resolveNumericTarget`, `resolvePIDTarget`, container, then project). The first failure stops the run before any source starts. The empty-port diagnostic is printed as today.

Then duplicates are removed, after child discovery, so worker PIDs are known:
- same host PID as an earlier target → drop;
- PID in an earlier target's `Children` → drop;
- same container ID as an earlier target → drop.

Each drop prints one notice with both labels. If one target is left, the run is a single-target run (no tags, today's messages).

### 4. One output loop, with a "row owner"

Each session's entries are forwarded into one channel of `(session index, Entry)` by a small goroutine per session. The output loop in `RunE` reads it, runs the filter, renders, and writes. With two or more targets it adds the tag to the start of every physical line of the rendered text.

To keep partial lines apart, the loop remembers whether the last write was partial and which session wrote it:
- a different session writes while a partial row is open → write `\n` first, then the tag;
- the same session continues → no new tag on the continuing text.

**Trace guard.** Because entries are single lines, the loop also keeps one stack trace together. While a session is in the middle of a trace (its last entry is a trace line that is not the closing line), the entries of the other sessions wait, in arrival order. They are written when the trace ends (its closing line, the next line of that session that is not part of the trace, or the end of that session) or when the trace has been quiet for 200 ms (`traceHold`, the same wait the grouper uses). A single target is never held. A target that writes a trace without ever closing it delays the other targets by at most 200 ms after its last line.

The tag is `label + padding + " | "`. Padding uses the longest label. Colors cycle through cyan, magenta, blue, green, hi-cyan, hi-magenta (never red or yellow). The color goes on the label only. With colors off, the tag is plain, so `grep ':4000 |'` works.

*Alternative:* `│` (box drawing) instead of `|`. Rejected for now: `|` is what `docker compose` uses, it is plain ASCII, and it is easy to type in `grep`.

### 5. Ending

Each session ends on its own path, as one target does today: the watcher or the source ends it, and its pipeline drains for `drainGrace`. When a session's entries channel closes, `RunE` prints that session's end report, tagged, at once. The loop ends when all channels are closed.

Ctrl+C and `--duration` cancel the shared parent context, so every session stops together. Each session that was still running then prints its "detached" line. The exit status is 1 if any session ended by itself (`reportEnd` returned 1 for it), else 0.

`endReport` gets a label field; `reportEnd` prints the label before the message when there are several targets. A source error at runtime (today returned from `RunE`) is printed for that session as its end, and the others continue.

### 6. eBPF: one program per session, then measure

Each session that uses eBPF loads its own program and ring buffer. This needs no change to the eBPF source. The cost: every program runs on every `write`/`writev` on the machine, so N eBPF targets add about N times the per-write hook cost, and N × 8 MB of locked ring-buffer memory. The 8-target limit keeps this bounded.

The cost is measured before the change is archived (a task): the benchmark at 1,000 requests per second with one eBPF session and with two. If the second program adds a clear cost, a shared engine (one program, one map of followed PIDs per target) becomes its own change.

### 7. Zoned string timestamps in local time

`FormatSingleLineJSON` today cuts an ISO timestamp after `T` and drops a final `Z`, so it shows UTC as written. Now, if the string parses as RFC 3339 (`time.RFC3339Nano`), it is converted with `.Local()` and shown as `15:04:05.000`, like numeric timestamps. Other strings keep today's handling. `JSONLog.Timestamp` keeps the raw string; only the formatter changes. This applies with one target too, so one rule holds everywhere.

### 8. CLI surface

- `Args: cobra.ArbitraryArgs`, plus Specter's own check that positional targets and `--pid` together are at most 8, with a `DiagnosticError` that names the limit (cobra's own message would not).
- `validatePIDFlag` no longer refuses several `--pid` or `--pid` with arguments; it still refuses 0 and non-numbers.
- `Use: "specter [target...]"`, with examples for `specter 3000 4000`.
- Shell completion offers targets for every position, not only the first.

## Risks / Trade-offs

- [A second eBPF program right after the first may hit the parked start-hang bug ("attaches seconds after another one stopped").] → The cases differ (the first program is still loaded, not just removed), but the risk is real. The E2E and manual tests start two eBPF sessions many times in a row in WSL2; if it hangs, the goroutine dump step from the backlog is the first move.
- [N eBPF programs cost N times the kernel hook on every write on the machine.] → Measured before archiving (Decision 6); limit of 8 targets.
- [One output goroutine renders for all targets, so it can become the bottleneck at high rates with several busy targets.] → Rendering is cheaper than grouping, which stays parallel. Buffered stdout (from the performance report) is the next step if needed.
- [A start error that a source reports late (after the immediate check) is not caught by "all or nothing".] → It is then handled like a runtime end of that session: reported with its label, and the others keep running. This matches today's single-target behavior, where a late error ends the run after attach.
- [The tag breaks tools that read `--json` output line by line (for example `jq`).] → Only with two or more targets. One target gives clean JSON as before.
- [Splitting `RunE` can change single-target behavior by accident.] → The existing `main_test.go` and E2E tests run unchanged, and new tests compare single-target stdout and stderr with the old output.
- [Timestamp change is visible: `10:00:00Z` now shows in local time with milliseconds.] → Listed as a small breaking change in the CHANGELOG.

## Open Questions

None.
