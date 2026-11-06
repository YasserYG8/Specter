## Context

The strace source runs `strace -f -qq -xx -s 65536 -e trace=write,writev -p <target> [-p <worker>...]`. Because of `-f`, every process the target starts is traced too, and the parser emits a chunk for every write to fd 1 or 2, from any of them.

Some of those processes write to an fd 1 that is not the server's output. A Next.js dev server starts helpers like `get-registry.js` and `telemetry/project-id.js` with their stdout connected to a pipe that Next.js reads, or to `/dev/null`. In a real test on port 3000, 12 captured lines came from such helpers and never appeared in the terminal that started the server.

Workers, on the other hand, usually inherit the target's stdout (gunicorn, Node cluster, Celery), and their writes are real server output. So "drop all children" is wrong. The test is where the write goes, not who makes it.

## Goals / Non-Goals

**Goals:**
- Drop writes to fd 1 or 2 whose destination differs from the target's own fd 1 and fd 2.
- Get this right for helpers that live only a few milliseconds.
- Never drop a write when the answer is unknown.
- No change for single-process servers and for workers that share the target's stdout.

**Non-Goals:**
- Multi-server streaming (`specter 3000 4000`). This change is the groundwork for it.
- Following output that a parent reads from a child pipe and prints again. The parent's own write already shows it once.
- Changing the "writes to its own log file" notice. It could also use the `-y` path instead of `readlink`, but that is a separate cleanup.
- Docker, journal and file sources, which do not see child processes.

## Decisions

### 1. Take the destination from strace (`-y`), not from `/proc` after the fact

With `-y`, strace prints `write(1<pipe:[4021]>, ...)`. It resolves the path while the tracee is stopped in the syscall, so the answer is correct even if the process exits a moment later.

Alternative: when a new PID writes for the first time, `readlink /proc/<pid>/fd/1` from Specter. That races with the process: Specter reads strace's output some time after the call, and short-lived helpers are exactly the ones we want to drop. If the readlink fails we would have to show the line, so the fix would miss the main case.

Cost: strace looks up the path on each traced write. That adds a little to the ptrace stop that already happens on every write. It is measured in the stress test (task 4.2). Specter cannot reduce this cost on its side, so if it is large, the number goes into the README next to the existing slowdown warning.

### 2. Reference destinations from `/proc/<target>/fd/{1,2}` at start

The target is long-lived and already attached, so reading its links once at start has no race. Specter reads them before strace starts, which is also before any helper can write.

Alternative: learn the reference from the target's first write. A helper can write before the target does, so this would leave early writes unclassified.

If the target later redirects its own stdout (`dup2` after daemonizing), the reference is stale. This is rare for servers that are already running and listening. Writes from the target itself are always shown, so only children of such a target could be misclassified.

### 3. The target's own writes always pass

A daemon with stdout at `/dev/null` is a key case for Specter: its output goes nowhere, and Specter is how you see it. So the rule applies only to other PIDs. The PID to compare is `target.PID`. Pre-discovered workers in `target.Children` follow the same rule as forked children, because a pre-existing helper (for example a long-lived compile worker) can also have a private pipe.

### 4. Unknown means show

If a write line has no annotation, or the annotation cannot be decoded, or neither reference destination could be read, the chunk is emitted. This keeps the promise of not losing real lines, at the cost of the old behavior in rare cases.

### 5. Where the decision lives

- The parser only parses: it adds a `dest` field to the parsed call and to `pendingCall`, so it survives `<unfinished ...>` and `<... resumed>`.
- `Source` owns the rule, since it knows the target and the reference destinations. A small pure function `shouldEmit(pid, dest, targetPID, refs)` keeps the rule easy to test.
- `source.Chunk` does not change, so nothing downstream changes.

### 6. Parsing the annotation

`callStartRegex` becomes `^(writev?)\((\d+)(?:<([^>]*)>)?,\s*`. With `-xx`, strace may print the path hex-encoded (`1<\x2f\x64\x65\x76...>`), in which case it is decoded with `DecodeStraceHex`. Pipe and socket names like `pipe:[4021]` may also appear in plain text or hex. Both forms are handled the same way: decode, then compare as strings. Real output from the strace version in WSL decides which form the tests use (task 1.1).

## Risks / Trade-offs

- [`-xx` changes how the path is printed in a way not expected] → Task 1.1 captures real output first, and the tests are built from it.
- [`-y` makes tracing slower] → Measure in the stress test. Report the number in the change. It only affects the target while attached.
- [A helper prints to a pipe that the parent forwards to its stdout with changes, for example a prefix] → The forwarded version is shown once, by the parent's write. That is what the real terminal shows.
- [PID reuse during a long session gives a new process the same PID as the target] → Practically impossible while the target is alive, since PIDs are not reused while the process exists.
- [The target's stdout is a terminal that a child also opens separately, for example `/dev/tty`] → The paths differ (`/dev/tty` vs `/dev/pts/3`), so the write is dropped, though it reaches the same screen. This is rare for servers, and it is noted as a known limit.
- [Older strace without `-y`] → `-y` has existed since strace 4.7 (2012). No fallback.

## Migration Plan

No user action. The change only removes lines that did not reach the server's output. Rollback is reverting the commit.

## Open Questions

- Does strace 6.8 with `-xx` print the `-y` path in hex? **Yes.** Pipes, `/dev/null`, files, terminals and sockets are all hex-encoded (`1<\x70\x69\x70\x65\x3a...>`), and `DecodeStraceHex` decodes them. TCP sockets print as `socket:[N]` under `-y` (the `TCP:[a->b]` form needs `-yy`), so the `>` of `->` never reaches the regex. The decoded text equals what `readlink /proc/<pid>/fd/N` returns.
- How much does `-y` slow tracing? **Not measurably.** 20,000 separate stdout writes in the test server under `strace -f -qq -xx -s 65536 -e trace=write,writev`: mean 5.13 s without `-y` and 4.90 s with `-y` (6 alternating runs each, range 4.2 to 6.8 s in WSL2). Without strace the same burst takes 0.05 to 0.16 s. The cost is the ptrace stop that already happens on every traced write, so no new README warning is needed.

## Added during implementation

- If only one of the target's two stream references can be read, a write that matches neither is still output. An unreadable reference could be the one the write belongs to, and losing a real line is worse than showing an extra one.
