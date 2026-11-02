## Context

The `strace` source runs `strace -f -qq -xx -s 8192 -e trace=write -p PID`, and `Parser.ParseLine` turns each stderr line into a `Chunk`. Several gaps lose logs with no message. These were observed by running real `strace` and Specter on Ubuntu under WSL2 on 2026-11-02.

**Overlapping writes.** When two tracees are in a syscall at the same time, `strace` splits the call over two lines. This is the real format:

```
[pid 33039] write(1, "\x70\x33\x2d\x30\x0a", 5 <unfinished ...>
[pid 33039] <... write resumed>)        = 5
```

`writeUnfinishedRegex` expects `"..." <unfinished ...>` with no length argument, so it never matches. The later resumed line finds nothing pending and is dropped too. The parser tests and the `strace-source` spec scenario use an invented format where the resumed line carries more data, so they pass while real input fails. With 4 worker processes, 11,877 of 12,000 writes were split and Specter captured 231 lines.

**`writev`.** It isn't traced. Python, Node, Perl and bash were observed using `write` for stdout. Ruby's `puts` and musl libc's stdio are expected to use `writev`, but that wasn't tested because neither was available.

**Long writes.** At `-s 8192`, a longer write is cut and its trailing newline goes with it, so the next line is glued on. The `log-filter-renderer` spec already promises lossless output up to 64 KB, which the source can't deliver.

**Output elsewhere.** A target that logs to its own file produces nothing and no explanation. Specter also looks the same when a target is idle or buffering.

**Oversized `strace` lines.** `readLoop` uses a `bufio.Scanner` with a 1 MB limit, and doesn't check `scanner.Err()`. A longer line ends the loop without any message. This comes from reading the code and wasn't reproduced.

## Goals / Non-Goals

**Goals:**

- Capture every `write` and `writev` to fd 1 and 2 from all tracees, including overlapping ones.
- Make any remaining loss visible at the point where it happens.
- Explain silence when Specter can tell why, or at least say that nothing has been captured.
- Reject flag values that would otherwise be ignored.

**Non-Goals:**

- Following a target across restarts, `--last`, multiple targets, or native macOS and Windows support.
- The startup fallback at `cmd/specter/main.go:404` and `target.Container = "detected"` in `internal/resolver/process.go`. Both are real issues, but both report an error rather than losing logs silently.
- Streaming the target's log files. This change only points the user at them.
- Writes to another fd that refers to stdout, such as a process that opens `/dev/stdout` itself. This is also silent loss, but it needs its own investigation and is a good follow-up.
- Reducing the slowdown caused by `strace` while attached.

## Decisions

### Match the format `strace` really prints, and remove the invented one

For `write`, `strace` prints all arguments at entry, so the unfinished line holds the whole payload and the length, and the resumed line holds only the return value. The parser stores the decoded payload per PID at entry and emits it when the resumed line supplies the return value.

The patterns and tests for a resumed line that carries data are removed rather than kept alongside. They describe a format that was never observed, and keeping them would keep hiding this class of bug. Parser fixtures are copied from real `strace` output.

*Alternative considered:* running `strace` so that calls never split. `strace` has no such option for concurrent tracees, and running one `strace` per process would add cost and lose ordering.

### Treat `writev` the same way as `write`

Add `writev` to `-e trace=`, and concatenate each `iov_base="..."` in order, then trim to the return value. It shares the unfinished and resumed handling. Real output was captured for this format, including a 41-element call that printed every element at `-s 65536`.

Tracing one more syscall should add little. The measured slowdown on a loop of `getppid` shows that an attached `strace` already stops the target on syscalls it isn't printing. The change is still measured before and after.

*Alternative considered:* also tracing `pwrite64`, `sendto` and `sendmsg`. Nothing observed logs to stdout through them, so they aren't added without evidence.

### Use `-s 65536`, and mark what is still cut

65536 matches the existing 64 KB promise. A captured write of 100,001 bytes printed a 262,182-byte `strace` line at this setting. `strace` is expected to copy only the bytes actually written, so short writes shouldn't pay for the larger limit, and this is measured too.

The parser detects a cut write when the return value is larger than the decoded payload. It appends a marker and a newline to the chunk:

```
AAAA...AAAA [specter: 34465 more bytes not captured]
```

The newline protects the next line, and the marker sits at the exact place data is missing.

This does put text Specter wrote into the log stream on stdout, which bends the rule that status messages go to stderr. A message on stderr alone wouldn't show which line was damaged and would be lost in `specter 3000 2>/dev/null | less`. Output that was already damaged is the one case where accuracy wins, and the marker also appears under `--raw` for that reason.

*Alternative considered:* a much larger `-s`. Hex encoding makes each `strace` line 4 times the payload, so very large values cost memory, and some limit would still need the marker.

### Never stop reading `strace` output

Replace the `bufio.Scanner` with a `bufio.Reader` loop. A line over the limit is discarded to its end, and reported with a notice, and reading continues. A `strace` that can't write to its pipe would stop making progress and could hold up the target, so draining the pipe also protects the read-only invariant.

### Add notices through an optional interface

Follow the `ProcessStatus()` pattern already in `main.go`: `main.go` checks for an optional method on the source and hands it a function that prints to stderr. The `source.Source` interface stays unchanged, so `docker`, `file` and `journal` need no edits.

**Own log file.** When the parser sees a write to fd 3 or higher, the source resolves `/proc/<pid>/fd/<n>` once per PID and fd. If it's a regular file with `log` in its name, the source prints one notice per path. Sockets are skipped because they aren't regular files, and the name check keeps database files out. `procRoot` stays injectable, so this can be tested without Linux.

**No output.** If the pipeline has produced no entries 10 seconds after attaching, `main.go` prints one notice saying the target may be idle, buffering its output or logging elsewhere. It counts entries before filters, because filtered output is a different situation. It applies to every source, because all of them can be silent.

### Validate `--level`, and keep literal `--grep`

`--level` is checked before target resolution and accepts everything `ParseRankedSeverity` accepts, plus empty. An unknown value returns a `DiagnosticError` that lists the valid levels.

An invalid `--grep` regex keeps falling back to literal text, which is useful for a pattern like `[error`, and now prints one notice. Making it an error would break that use.

## Risks / Trade-offs

- [`strace` versions may format these lines differently] → Keep the parser tolerant of spacing, such as the padding in `<... write resumed>)        = 5`, and run E2E on CI's `strace` as well as WSL2.
- [The marker changes stdout for over-limit writes] → It only appears when data is already missing, and it is documented in the README.
- [Larger `-s` or `writev` tracing could slow the target more] → Rerun the HTTP benchmark, which measured about 2,135 req/s detached and 813 req/s attached, and compare.
- [Capturing all writes means much more data flowing through the pipeline for busy workers] → Check for races and dropped lines with the new E2E and existing stress tests. The channel stays bounded, so a slow consumer applies backpressure, which needs measuring rather than assuming.
- [The `log` name check can miss a log file, or match a non-log file] → A miss leaves today's behaviour plus the 10-second notice. A wrong match costs one stderr line.
- [The 10-second notice can appear for a server that is simply idle] → It prints once and goes to stderr, and its wording includes idle as a possibility.
- [`--level` scripts that pass an unknown value will now fail] → The error lists the valid values, and failing is more accurate than showing unfiltered output.

## Migration Plan

Nothing to migrate. Ship in the next release, and call out the `--level` change in the release notes. To roll back, revert the change.

## Open Questions

- Does the higher capture rate change the slowdown? The benchmark task answers this. If it gets noticeably worse, reconsider before release.
- Do Ruby and Alpine really use `writev`? This doesn't change the design, but it would confirm how much the `writev` work matters.
