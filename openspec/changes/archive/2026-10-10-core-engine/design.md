## Context

Specter is a Linux-first developer CLI that attaches to any server process by port or PID and streams its stdout and stderr in real time. The Core Engine implements the first milestone: port resolution, process inspection, syscall interception via `strace`, and line reassembly.

The system is implemented in Go 1.24+ to deliver a single standalone binary with zero runtime dependencies and sub-10ms startup latency.

## Goals / Non-Goals

**Goals:**
- Discover the PID listening on a TCP port using `lsof` (fallback `ss`).
- Read `/proc/<PID>` metadata (`cmdline`, `cwd`, `fd/1`, `fd/2`).
- Attach non-invasively using `strace -f -qq -xx -s 8192 -e trace=write -p <PID>`.
- Parse hex-encoded `write` syscalls on fd 1 and 2, reassembling `<unfinished ...>` and `<... resumed>` calls across threads.
- Buffer bytes per `(pid, fd)` and emit complete lines, with a 150ms idle flush timer for interactive prompts.
- Ensure graceful detachment on `Ctrl+C` (SIGINT/SIGTERM) so target processes never freeze or terminate.

**Non-Goals:**
- History or retroactive scrollback buffer (live streaming only).
- Non-terminal sources (Docker, systemd, log files are deferred to Phase 2).
- Advanced log parsing, JSON syntax highlighting, and multiline stack trace grouping (deferred to Phase 3).
- Windows or macOS native tracing without WSL2.

## Decisions

### 1. Hex output mode (`-xx`) for strace parsing
- **Choice**: Invoke strace with `-xx` so all string arguments are printed purely as hex escapes (`\x68\x65\x6c\x6c\x6f`).
- **Rationale**: Standard strace string formatting uses C-style escaping (`\n`, `\t`, octal escapes, quotes), which creates parsing ambiguities when user logs contain arbitrary binary or quote characters. Hex decoding is completely unambiguous and preserves arbitrary binary and multibyte UTF-8 byte sequences.
- **Alternatives Considered**: Raw string output (`-s 8192`) — dropped due to edge cases with embedded quotes and ambiguous escape sequences.

### 2. Channel-based asynchronous streaming
- **Choice**: `Source.Start(ctx, target)` returns `(<-chan Chunk, <-chan error)`.
- **Rationale**: Go channels provide native concurrency and backpressure. If the consumer stops reading, the reader goroutine can handle buffering or drop strategies without blocking the entire runtime.
- **Alternatives Considered**: Callback interfaces or synchronous `io.Reader` — channels integrate more cleanly with Go `select` statements and timeout timers.

### 3. Per-(PID, FD) buffering in LineAssembler
- **Choice**: Maintain a `map[[2]int]*bytes.Buffer` indexed by `[PID, FD]`.
- **Rationale**: A process with multiple threads writing concurrently will interleave stdout writes. Per-(PID, FD) buffers ensure that a thread's line is not corrupted by writes from another thread.
- **Alternatives Considered**: Single buffer per process — rejected because multi-threaded servers produce garbled interleaved text.

### 4. 150ms Idle Flush Timer
- **Choice**: Start a timer on incoming partial bytes that flushes the buffer if no new bytes arrive within 150ms.
- **Rationale**: Command-line prompts (e.g. `Password: ` or `[Y/n]`) or progress bars do not write trailing newlines. Without an idle flush, the user would see nothing until the next newline.

## Risks / Trade-offs

- **[Risk] ptrace permission restrictions (`ptrace_scope > 0`)**:
  - *Mitigation*: Inspect `/proc/sys/kernel/yama/ptrace_scope` upfront. When ptrace is denied, output a clear `DiagnosticError` instructing the user to run with `sudo` or configure `setcap cap_sys_ptrace+ep $(which strace)`.
- **[Risk] Target process log flooding**:
  - *Mitigation*: Use buffered channels. If channel capacity is exceeded, drop oldest chunks and log a rate-limiting warning rather than growing memory unbounded.
- **[Risk] Strace child process orphaned on SIGINT**:
  - *Mitigation*: Use `syscall.SysProcAttr{Setpgid: true}` and forward signals to the process group, ensuring `strace` detaches cleanly when Specter exits.
