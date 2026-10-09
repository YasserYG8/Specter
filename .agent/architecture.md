# Specter: Architecture

Working name: Specter. Linux-first CLI. Draft v1.

## 1. Goal

`specter 3000` prints the live stdout/stderr of whatever server is listening on port 3000, in your terminal, with readable colors. No wrapper, no history, and the tool itself never opens a port.

Out of scope for the MVP: history/scrollback, web UI, macOS and Windows, production use.

## 2. Design principles

- **Read-only:** never modifies, signals or restarts the target process.
- **Zero setup:** works on servers you already started in any terminal.
- **Pipeline of small stages:** each stage is a pure transform, so it can be tested with fixtures.
- **Honest failure:** when attaching is impossible (permissions, no output), say exactly why and how to fix it.
- **Local only:** no network, no telemetry. Logs can contain tokens and user data.

## 3. High-level architecture

```
specter 3000
   |
   v
[CLI parser]  ->  [Resolver: port -> PID, cmdline, cwd, fd 1/2 targets]
   |
   v
[Source selector]
   |-- Docker container?     -> DockerSource   (docker logs -f --tail 0)
   |-- fd 1/2 is a file?     -> FileTailSource (tail the file)
   |-- systemd unit?         -> JournalSource  (journalctl -f _PID=...)
   |-- else                  -> StraceSource   (attach, keep write() to fd 1/2)
   |
   v
Chunk stream { bytes, fd, pid, ts }
   v
[LineAssembler]  per-fd buffers -> complete lines
   v
[Parser]  level detection, JSON detection, stack-trace grouping
   v
[Filter]  --grep, --level, --stderr
   v
[Renderer]  ANSI output to stdout
```

Every source emits the same `Chunk` type, so everything after the source is identical for Docker, files and strace.

## 4. Tech stack

| Concern | Choice | Why |
| --- | --- | --- |
| Language | TypeScript on Node 20+ | Your stack, fast to build, easy npm distribution |
| Child processes | `node:child_process` (spawn) | No dependency needed for strace, lsof, docker |
| CLI args | `commander` | Small and standard |
| Colors | `picocolors` | Tiny, respects NO\_COLOR |
| Build | `tsup` | One bundled file with a bin entry |
| Tests | `vitest` | Fast, TypeScript native |
| CI | GitHub Actions | Matrix on Node versions and Ubuntu |

Docker is used through the `docker` CLI (spawned), not an SDK, to keep dependencies at zero for that source.

## 5. Project structure

```
specter/
  src/
    cli.ts                 entry, flags, wiring
    resolver/
      port-to-pid.ts       lsof / ss
      process-info.ts      cmdline, cwd, parent chain
      fd-targets.ts        readlink /proc/PID/fd/1 and /2
    sources/
      types.ts             Source and Chunk interfaces
      select.ts            priority logic
      docker.ts
      file-tail.ts
      journal.ts
      strace/
        spawn.ts           builds and runs the strace command
        parse.ts           strace line -> bytes
        permissions.ts     ptrace_scope checks and fix hints
    pipeline/
      line-assembler.ts
      parser.ts            levels, JSON, stack traces
      filter.ts
      renderer.ts
    util/errors.ts
  test/
    fixtures/              captured strace output, Java/Node/Python logs
  package.json
```

## 6. Core contracts

```ts
type Chunk = {
  bytes: Uint8Array;
  fd: 1 | 2;          // stdout or stderr
  pid: number;        // writing thread or process
  ts: number;         // ms epoch, taken when received
};

interface Source {
  name: string;
  start(target: Target): AsyncIterable<Chunk>;
  stop(): Promise<void>;
}

type Target = {
  port: number;
  pid: number;
  cmdline: string[];
  cwd: string;
  stdout: string;     // readlink of /proc/PID/fd/1
  stderr: string;
  container?: string;
  unit?: string;
};
```

## 7. Resolver

1. Port to PID: `lsof -tiTCP:PORT -sTCP:LISTEN`, with `ss -ltnp` as fallback. If several PIDs are returned, pick the oldest one and print a notice.
2. Read `/proc/PID/cmdline`, `/proc/PID/cwd` and the parent chain.
3. `readlink /proc/PID/fd/1` and `/fd/2` to learn what stdout and stderr really are: a pty (`/dev/pts/N`), a pipe, a regular file, or `/dev/null`.
4. Check Docker: if the PID belongs to a container cgroup, or `docker ps` maps the port, record the container.
5. Check systemd: `/proc/PID/cgroup` containing a `.service` unit.

Typical dev case: `npm run dev` starts a shell, then npm, then node. The listening PID is the node process, and that is the one whose writes matter.

## 8. Source selection

First match wins:

1. **Docker:** `docker logs -f --tail 0 CONTAINER`.
2. **Regular file:** if fd 1 or 2 points to a regular file, tail that file.
3. **systemd:** `journalctl -f _PID=PID`.
4. **`/dev/null`:** nothing to see; tell the user.
5. **Otherwise:** strace attach, which covers ptys and pipes (the normal terminal case).

A `--source docker|file|journal|strace` flag overrides the choice.

## 9. StraceSource (the core)

Command:

```
strace -f -qq -xx -s 8192 -e trace=write -p PID
```

- `-f`: attach to all threads and follow new ones. Node and Java write from non-main threads.
- `-qq`: no attach or detach messages.
- `-xx`: print every byte as hex. This avoids parsing C escapes and keeps UTF-8 multibyte characters intact.
- `-s 8192`: raise the string limit. When strace truncates, the line ends with `...` after the string. Detect it, keep what you have, and count the loss.
- `-e trace=write`: only the write syscall, to reduce overhead.

strace prints to stderr, so spawn it and read its stderr as the data stream.

**Parsing one line:** an optional `[pid N]` prefix, then `write(FD, "HEX BYTES", LEN) = RET`.

- Keep only FD 1 and 2.
- Decode the hex into bytes. If RET is smaller than LEN, cut the bytes to RET. If RET is negative, drop the line.
- Emit `Chunk { bytes, fd, pid, ts }`.

**Gotchas to handle explicitly:**

- **Unfinished calls:** under multi-thread contention strace prints `<unfinished ...>` on one line and `<... write resumed>` on a later line. Keep a map per PID, and complete the call when the resumed line arrives.
- **Partial lines:** a write rarely ends on a newline boundary. That is the LineAssembler's job, not the parser's.
- **Interleaving:** stdout and stderr, and different threads, must have separate buffers, or lines get mixed.
- **Backpressure:** if the target logs faster than the terminal prints, drop the oldest queued chunks and print a `[specter: N lines skipped]` marker instead of growing memory.
- **Cleanup:** on Ctrl+C, kill the strace child and wait for it to detach. Never leave the target traced.

## 10. Permissions

Attaching needs ptrace permission. Read `/proc/sys/kernel/yama/ptrace_scope` before starting:

| Value | Meaning | What Specter does |
| --- | --- | --- |
| 0 | Same user can trace | Just attach |
| 1 | Only descendants | Suggest `sudo specter ...`, or `sudo setcap cap_sys_ptrace+ep $(which strace)` |
| 2 | Admin only | Require sudo |
| 3 | No attach at all | Explain that it cannot work |

If the target belongs to another user, require sudo. Never edit system settings automatically; print the exact command and let the user decide.

## 11. LineAssembler

- One buffer per `(pid, fd)`.
- Append bytes, split on `\n`, keep the trailing partial piece.
- Flush a partial line after a short idle timeout (about 150 ms), so prompts and progress output still appear.
- Decode as UTF-8 with a streaming decoder, so multibyte characters split across writes survive.

## 12. Parser and renderer

The parser tags each line; the renderer turns tags into ANSI.

- **Level detection:** match `INFO`, `WARN`, `ERROR`, `DEBUG` tokens near the line start. Express/Morgan style lines are classified by HTTP status (5xx red, 4xx yellow).
- **JSON lines:** if a line parses as a JSON object, pretty-print it on a single line with colored keys, with `--json` to force the raw output.
- **Stack traces:** group lines into one entry by heuristics: a Java line starting with ` at  ` after whitespace, `Caused by:`, `... N more`, Python's `Traceback (most recent call last):` followed by indented lines, and Node's indented `at` frames. Keep the group together so filtering does not split it.
- **Source prefix:** `[node :3000]`, taken from the resolver.
- **Existing ANSI codes:** pass them through unchanged. If the app already colors its output, do not recolor it.
- Respect `NO_COLOR` and non-TTY output (plain text when piped).

## 13. CLI surface

```
specter <port|pid> [options]

  --grep <text|/regex/>   keep matching lines (stack traces stay whole)
  --level <lvl>           minimum level: debug|info|warn|error
  --stderr                only stderr
  --json                  raw JSON lines, no pretty-printing
  --raw                   no parsing, no colors
  --source <name>         force docker|file|journal|strace
  --tree                  also attach to child processes
  --no-color
```

Because output is a plain stream, `specter 3000 | grep 500` works.

## 14. Error handling

| Situation | Message |
| --- | --- |
| Nothing listens on the port | Say so, and list the nearby listening ports |
| strace not installed | Print the install command for the detected distro |
| ptrace denied | Explain `ptrace_scope` and show the fix |
| stdout is `/dev/null` | The process has no output to show |
| Target exits | Print `process exited (code N)` and stop cleanly |
| Non-Linux OS | Say it is Linux-only for now, pointing to the roadmap |

## 15. Testing

- **Unit tests:** strace parser, hex decoding, unfinished/resumed merging, line assembler, level detection and stack-trace grouping, all from fixture files captured from real runs (Express, Spring Boot, FastAPI).
- **Integration test:** spawn a tiny Node server that prints known lines, run Specter against its port, and assert the output.
- **CI caveat:** ptrace is blocked in many containers. In Docker, run the job with `--cap-add SYS_PTRACE` (and an unconfined seccomp profile if needed), or run the integration test on a normal runner VM.
- **Stress test:** a process that logs 100k lines per second, to verify backpressure and memory stay bounded.

## 16. Performance and limits

- strace stops the target on every traced syscall, so a chatty server slows down. Acceptable for development, not for production.
- Only `write` is traced to keep this small.
- Live attach only: output printed before Specter started cannot be recovered.
- If the app logs to a file instead of stdout, strace sees nothing. The resolver detects open `.log` files in `/proc/PID/fd` and suggests tailing them.

## 17. Security notes

- Logs may contain secrets; Specter writes only to your terminal and stores nothing on disk.
- It needs elevated ptrace rights only when the kernel requires them, and says so.
- No auto-update and no telemetry in the MVP.

## 18. Roadmap

| Phase | Content | Time |
| --- | --- | --- |
| 0. Spike | Shell one-liner to confirm attach works on your machine | 1 hour |
| 1. Core | Resolver, StraceSource, LineAssembler, plain output | 1 to 2 days |
| 2. Sources | Docker, file tail, journald, source selection | 1 day |
| 3. Display | Levels, JSON, stack-trace grouping, `--grep`, `--level` | 1 to 2 days |
| 4. Polish | Error messages, tests, CI, README, npm publish | 1 to 2 days |
| 5. Later | Optional `--ui` (random localhost port), eBPF source, VS Code extension for macOS and Windows | open |

## 19. Open decisions

- Final name (check npm and GitHub availability before committing).
- Whether to later add a Go rewrite for a single static binary.
- Whether to contribute the strace fallback to an existing tool such as port-whisperer, or ship it as a standalone project.
