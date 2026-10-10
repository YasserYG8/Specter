# Specter: Architecture

Working name: Specter. Linux-first CLI in Go. Draft v2.

## 1. Goal

`specter 3000` prints the live stdout/stderr of whatever server is listening on port 3000, in your terminal, with readable colors. No wrapper, no history, and the tool itself never opens a port.

Out of scope for the MVP: history/scrollback, web UI, native macOS and Windows (runs on Linux and WSL2), production use.

## 2. Design principles

- **Read-only:** never modifies, signals or restarts the target process.
- **Zero setup:** works on servers you already started in any terminal.
- **Pipeline of small stages:** each stage is a pure transform, tested with fixtures.
- **Honest failure:** when attaching is impossible (permissions, no output), say exactly why and how to fix it.
- **Local only:** no network, no telemetry. Logs can contain tokens and user data.
- **Zero runtime dependencies:** single standalone static binary distributed via Go / GitHub Releases.

## 3. High-level architecture

```
specter 3000
   │
   ▼
[CLI parser] (cobra)  ->  [Resolver: port -> PID, cmdline, cwd, fd 1/2 targets]
   │
   ▼
[Source selector]
   ├── Docker container?     -> DockerSource   (docker logs -f --tail 0)
   ├── fd 1/2 is a file?     -> FileTailSource (tail the file)
   ├── systemd unit?         -> JournalSource  (journalctl -f _PID=...)
   └── Default               -> StraceSource   (attach, keep write() to fd 1/2)
   │
   ▼
Chunk stream { Bytes, FD, PID, TS }
   │
   ▼
[LineAssembler]  per-(pid, fd) byte buffers -> complete lines (streaming UTF-8)
   │
   ▼
[Parser]  level detection, JSON detection, stack-trace grouping
   │
   ▼
[Filter]  --grep, --level, --stderr
   │
   ▼
[Renderer]  ANSI output to stdout (respects NO_COLOR and non-TTY pipes)
```

Every source emits the same `Chunk` type, so everything after the source is identical for Docker, files, and strace.

## 4. Tech stack

| Concern | Choice | Why |
| --- | --- | --- |
| Language | Go 1.24+ | Single static binary, <10ms startup, robust concurrency, no runtime needed |
| Child processes | `os/exec` with `context.Context` | Native, clean cancellation and signal termination |
| CLI flags | `spf13/cobra` | De-facto standard for production CLIs (gh, kubectl, docker) |
| Colors | `fatih/color` or `muesli/termenv` | Robust, respects `NO_COLOR` and terminal capability detection |
| Build & Packaging | `go build` / `goreleaser` | Cross-compiles Linux binaries directly from Windows with zero tooling |
| Tests | `testing` (Go standard library) | Fast, table-driven tests with fixture files |
| CI | GitHub Actions | Matrix across Go versions on Ubuntu |

Docker is used through the `docker` CLI (`os/exec`), not an SDK, to keep dependencies minimal.

## 5. Project structure

```
specter/
  cmd/
    specter/
      main.go                // CLI entry point, cobra wiring, signal trap
  internal/
    resolver/
      resolver.go            // port -> PID coordinator
      port.go                // lsof / ss TCP port resolution
      process.go             // /proc/<pid>/cmdline, cwd, parent chain
      fd.go                  // readlink /proc/<pid>/fd/1 and /fd/2
    source/
      types.go               // Source interface, Chunk, Target structs
      select.go              // priority selection logic
      docker.go              // DockerSource
      filetail.go            // FileTailSource
      journal.go             // JournalSource
      strace/
        spawn.go             // builds and runs strace with context
        parse.go             // hex decoding & unfinished/resumed state
        permissions.go       // ptrace_scope checks and remediation guidance
    pipeline/
      assembler.go           // per-(pid, fd) byte buffer & idle flush timer
      parser.go              // log level tagging, JSON pretty-print, stack traces
      filter.go              // --grep, --level, --stderr filtering
      renderer.go            // ANSI terminal formatter
    util/
      errors.go              // structured user-friendly error types
  test/
    fixtures/                // captured strace output, Java/Node/Python logs
  go.mod
  go.sum
  README.md
  LICENSE
```

## 6. Core contracts

```go
package source

import (
    "context"
    "time"
)

type Chunk struct {
    Bytes []byte    // decoded raw payload
    FD    int       // 1 for stdout, 2 for stderr
    PID   int       // writing thread or process
    TS    time.Time // timestamp when received
}

type Target struct {
    Port      int
    PID       int
    Cmdline   []string
    Cwd       string
    Stdout    string // readlink target of /proc/<PID>/fd/1
    Stderr    string // readlink target of /proc/<PID>/fd/2
    Container string // container ID or name if in Docker
    Unit      string // systemd unit name if present
}

type Source interface {
    Name() string
    Start(ctx context.Context, target Target) (<-chan Chunk, <-chan error)
    Stop() error
}
```

## 7. Resolver

1. **Port to PID**: `lsof -tiTCP:PORT -sTCP:LISTEN`, with `ss -ltnp` as fallback. If several PIDs are returned, pick the oldest one and print a notice.
2. Read `/proc/<PID>/cmdline`, `/proc/<PID>/cwd` and the parent chain.
3. `readlink /proc/<PID>/fd/1` and `/fd/2` to learn what stdout and stderr really are: a pty (`/dev/pts/N`), a pipe, a regular file, or `/dev/null`.
4. Check Docker: if the PID belongs to a container cgroup, or `docker ps` maps the port, record the container.
5. Check systemd: `/proc/<PID>/cgroup` containing a `.service` unit.

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

```bash
strace -f -qq -xx -s 8192 -e trace=write -p <PID>
```

- `-f`: attach to all threads and follow new ones.
- `-qq`: suppress attach and detach messages.
- `-xx`: print every byte as hex (`\x68\x65...`). This avoids parsing C escapes and keeps UTF-8 multibyte characters intact.
- `-s 8192`: raise the string limit. When strace truncates, the line ends with `...` after the string. Detect it, retain captured bytes, and track truncation.
- `-e trace=write`: trace only the write syscall to minimize overhead.

strace prints to stderr, so Go spawns it and pipes its stderr into the parser goroutine.

**Parsing one line:** an optional `[pid N]` prefix, then `write(FD, "HEX BYTES", LEN) = RET`.

- Keep only FD 1 and 2.
- Decode hex into raw `[]byte`. If RET is smaller than LEN, slice the bytes to RET. If RET is negative, drop the line.
- Emit `Chunk { Bytes, FD, PID, TS }`.

**Critical edge cases handled explicitly:**

- **Unfinished calls:** under multi-thread contention strace prints `<unfinished ...>` on one line and `<... write resumed>` on a later line. Keep a map per PID, and complete the call when the resumed line arrives.
- **Partial lines:** a write rarely ends on a newline boundary. That is the LineAssembler's job, not the parser's.
- **Interleaving:** stdout and stderr, and different threads, must have separate byte buffers, or lines get mixed.
- **Backpressure:** if the target logs faster than the terminal prints, drop the oldest queued chunks and print a `[specter: N lines skipped]` marker instead of growing memory unbounded.
- **Cleanup:** on `os.Interrupt` / `SIGINT`, cancel context, kill the strace child process, and wait for it to detach. Never leave the target traced or suspended.

## 10. Permissions

Attaching needs ptrace permission. Read `/proc/sys/kernel/yama/ptrace_scope` before starting:

| Value | Meaning | What Specter does |
| --- | --- | --- |
| 0 | Same user can trace | Just attach |
| 1 | Only descendants | Suggest `sudo specter ...`, or `sudo setcap cap_sys_ptrace+ep $(which strace)` |
| 2 | Admin only | Require sudo |
| 3 | No attach at all | Explain that kernel configuration blocks ptrace |

If the target belongs to another user, require sudo. Never edit system settings automatically; print the exact command and let the user decide.

## 11. LineAssembler

- One byte buffer per `(pid, fd)`.
- Append bytes, split on `\n`, keep the trailing partial piece.
- Flush a partial line after a short idle timeout (about 150 ms), so prompts and progress output still appear.
- Decode UTF-8 cleanly across write boundaries.

## 12. Parser and renderer

The parser tags each line; the renderer turns tags into ANSI.

- **Level detection:** match `INFO`, `WARN`, `ERROR`, `DEBUG` tokens near the line start. Express/Morgan style lines are classified by HTTP status (5xx red, 4xx yellow).
- **JSON lines:** if a line parses as a JSON object, pretty-print it on a single line with colored keys, with `--json` to force the raw output.
- **Stack traces:** group lines into one entry by heuristics: Java frames starting with ` at `, `Caused by:`, `... N more`, Python's `Traceback (most recent call last):` followed by indented lines, and Node's indented `at` frames. Keep the group together so filtering does not split it.
- **Source prefix:** `[node :3000]`, taken from the resolver.
- **Existing ANSI codes:** pass them through unchanged. If the app already colors its output, do not recolor it.
- Respect `NO_COLOR` and non-TTY output (plain text when piped).

## 13. CLI surface

```
specter <port|pid> [flags]

  --grep <text|/regex/>   keep matching lines (stack traces stay whole)
  --level <lvl>           minimum level: debug|info|warn|error
  --stderr                only stderr
  --json                  raw JSON lines, no pretty-printing
  --raw                   no parsing, no colors
  --source <name>         force docker|file|journal|strace
  --tree                  also attach to child processes
  --no-color              disable ANSI styling
```

Because output is a plain stream, `specter 3000 | grep 500` works out of the box.

## 14. Error handling

| Situation | Action |
| --- | --- |
| Nothing listens on the port | Say so, and list nearby active listening ports |
| strace not installed | Print the install command for the detected distro (apt, dnf, pacman) |
| ptrace denied | Explain `ptrace_scope` and show the remediation command |
| stdout is `/dev/null` | Explain that the target has discarded its output |
| Target exits | Print `process exited (code N)` and terminate cleanly |
| Non-Linux OS | Detect OS, explain Linux-only requirements, and provide WSL2 instructions |

## 15. Testing

- **Unit tests:** strace parser, hex decoding, unfinished/resumed merging, line assembler, level detection, and stack-trace grouping, all driven by fixture files captured from real runs (Express, Spring Boot, FastAPI).
- **Integration test:** spawn a test server in Go that writes known lines, run Specter against its port, and assert the output stream.
- **CI:** Run test suite against Ubuntu runners with `CAP_SYS_PTRACE`.

## 16. Performance and limits

- strace stops the target on every traced syscall, so a chatty server slows down slightly. Acceptable for development, not for production.
- Only `write` is traced to keep overhead minimal.
- Live attach only: output printed before Specter started cannot be recovered.
- If the app logs to a file instead of stdout, strace sees nothing. The resolver detects open `.log` files in `/proc/<PID>/fd` and suggests tailing them.

## 17. Security notes

- Logs may contain secrets; Specter writes only to your terminal and stores nothing on disk.
- It needs elevated ptrace rights only when the kernel requires them, and says so.
- No auto-update and no telemetry.

## 18. Roadmap

| Phase | Content | Time |
| --- | --- | --- |
| 1. Core Engine | Resolver, StraceSource, LineAssembler, raw output CLI | 1 to 2 days |
| 2. Sources | Docker, file tail, journald, source selection | 1 day |
| 3. Display | Levels, JSON, stack-trace grouping, `--grep`, `--level` | 1 to 2 days |
| 4. Polish | Error messages, tests, CI, goreleaser, GitHub release | 1 to 2 days |
| 5. Later | eBPF capture source, VS Code companion extension | open |
