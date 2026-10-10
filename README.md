# Specter

A zero-configuration developer CLI in Go that attaches to any active server process by port or PID and streams live stdout and stderr directly to the terminal.

```bash
specter 3000
```

---

## Overview

When running backend services across multiple terminal sessions, background tasks, or containers, locating log output requires searching for the active terminal or restarting the service with logging wrappers.

Specter eliminates this workflow friction. Given a listening TCP port or a process ID, Specter resolves the target process, identifies where its standard streams are directed, attaches non-invasively, and streams the output in real time with line reassembly, JSON formatting, and log-level coloring.

---

## Key Principles

- **Zero Configuration**: Functions against already running processes. Does not require SDKs, code changes, wrappers, or environment flags.
- **Strictly Read-Only**: Specter never signals, suspends indefinitely, modifies, or restarts the target process. It does not open incoming network ports.
- **Local and Private**: All inspection and stream handling occur entirely on the local machine. No telemetry or network transmission is performed.
- **Standalone Static Binary**: Compiled in Go with zero runtime dependencies. No Node.js, Python, or JVM installation needed.
- **Honest Diagnostics**: If kernel restrictions (such as `ptrace_scope`) prevent attachment, Specter reports the exact reason and actionable remediation steps.

---

## Architecture & How It Works

```
specter <port|pid>
       │
       ▼
[CLI Parser]  ─── Parses flags (--level, --grep, --source) using spf13/cobra
       │
       ▼
[Resolver]    ─── Resolves port to PID via lsof / ss; reads /proc/<pid>/
       │
       ▼
[Source Selector]
       ├── Docker Container   ─── Spawns docker logs -f --tail 0
       ├── Regular File       ─── Follows file descriptor target
       ├── Systemd Service    ─── Reads journalctl -f _PID=<pid>
       └── Terminal / PTY     ─── Attaches via strace (trace=write)
       │
       ▼
[Line Assembler]  ─── Per-thread/fd buffer assembly, streaming UTF-8 decoding
       │
       ▼
[Parser & Formatter]── Log-level tagging, single-line JSON formatting, stack trace grouping
       │
       ▼
[Output Renderer] ─── ANSI colored stdout stream (respects NO_COLOR and pipes)
```

1. **Resolution**: Inspects network state using `lsof` or `ss` to identify the listening PID. Reads `/proc/<pid>/cmdline`, `/proc/<pid>/cwd`, and resolves `/proc/<pid>/fd/1` and `/proc/<pid>/fd/2`.
2. **Source Selection**: Determines the optimal log acquisition mechanism:
   - If the PID is associated with a Docker container, it streams via the container engine.
   - If stdout/stderr are redirected to a regular file, it follows the file descriptor directly.
   - If governed by systemd, it streams via `journalctl`.
   - By default (standard terminals, ptys, and pipes), it attaches using an optimized `strace` instance monitoring `write()` syscalls on descriptors 1 and 2.
3. **Assembly & Stream Processing**: Strace hex outputs (`-xx`) are decoded into raw byte chunks. The `LineAssembler` handles thread interleaving, reassembles split lines, and provides a short flush timer for interactive prompts.
4. **Formatting**: Output is parsed for standard log levels (`DEBUG`, `INFO`, `WARN`, `ERROR`), HTTP response codes, and multi-line stack traces (Node.js, Python, Java) before ANSI terminal rendering.

---

## CLI Reference

### Basic Usage

```bash
# Attach by listening TCP port
specter 3000

# Attach by Process ID
specter 18492
```

### Flags

| Flag | Description |
| :--- | :--- |
| `--level <lvl>` | Minimum log level to display (`debug`, `info`, `warn`, `error`). |
| `--grep <pattern>` | Filter output by text or regular expression. Preserves grouped stack traces. |
| `--stderr` | Stream only standard error (fd 2). |
| `--json` | Force raw JSON lines without pretty-printing. |
| `--raw` | Bypass line parsing and ANSI color formatting. |
| `--source <name>` | Explicitly select source (`docker`, `file`, `journal`, `strace`). |
| `--tree` | Follow and attach to child processes. |
| `--no-color` | Disable ANSI color codes (also adheres to `NO_COLOR` environment variable). |

---

## System Requirements & Platform Support

Specter is engineered as a Linux-first systems tool leveraging Linux kernel interfaces (`/proc`, `ptrace(2)`, `lsof`, `ss`).

- **Linux**: Native support on all standard distributions (Ubuntu, Debian, Fedora, Arch, Alpine).
- **Windows**: Supported via **WSL2** (Windows Subsystem for Linux).
- **macOS**: Native macOS is currently out of scope for the MVP due to System Integrity Protection (SIP) restrictions on ptrace.

### Dependencies

- Go 1.24 or higher (for building from source)
- System utilities: `lsof` (or `iproute2`/`ss`), `strace`

### Kernel Permissions (ptrace)

On systems enforcing restricted ptrace access (`/proc/sys/kernel/yama/ptrace_scope`), tracing non-child processes may require elevated privileges or capability assignment:

```bash
# Verify ptrace scope
cat /proc/sys/kernel/yama/ptrace_scope

# Option A: Run with sudo
sudo specter 3000

# Option B: Grant strace the ptrace capability
sudo setcap cap_sys_ptrace+ep $(which strace)
```

---

## Development & Building

Specter can be built natively on Linux or cross-compiled directly from Windows for Linux:

```bash
# Run unit and fixture tests
go test ./...

# Build local binary
go build -o specter ./cmd/specter

# Cross-compile for Linux (from Windows PowerShell)
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o specter ./cmd/specter
```

---

## License

MIT
