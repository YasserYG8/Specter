# Specter

A zero-configuration developer CLI in Go that attaches to any active server process or container by **port**, **PID**, **container name**, or **project name**, and streams live stdout and stderr directly to your terminal.

```bash
# Launch interactive service picker
specter

# Attach directly by port, project, container, or PID
specter 3000
specter my-api-project
specter api-gateway-container
specter 18492
```

---

## Overview

When running backend microservices, dev servers, or background containers across multiple terminal sessions, locating logs often requires hunting down hidden terminal tabs, querying container IDs, or restarting processes with verbose log wrappers.

**Specter eliminates this workflow friction.** Given a port, PID, container, or project name—or simply running `specter` to open the interactive picker—Specter resolves the target, identifies where its standard streams are directed, attaches read-only (see [Performance While Attached](#performance-while-attached)), and streams the output in real time with line reassembly, JSON formatting, and log-level coloring.

---

## Install

Specter runs on Linux, and on Windows through WSL2.

Install with Go (1.26 or higher):

```bash
go install github.com/YasserYG8/Specter/cmd/specter@latest
```

This puts `specter` in `$(go env GOPATH)/bin`, so make sure that folder is on your `PATH`.

Then set up the tools Specter uses:

```bash
sudo apt install strace lsof iproute2
```

While the `strace` source is attached, Specter slows the target, so read [Performance While Attached](#performance-while-attached) before pointing it at a busy server.

Ubuntu and WSL2 restrict `ptrace` by default, so attaching to a running process fails with `Operation not permitted` until you allow it. Either run `sudo "$(which specter)" 3000`, or see [Kernel Permissions](#kernel-permissions-ptrace_scope) for the other options.

---

## Key Features

- **Interactive Discovery Picker**: Run `specter` with zero arguments to browse all active dev servers, listening ports, Docker containers, and project directories in a comfortable, keyboard-driven UI (`↑`/`↓` or `k`/`j`, `Enter` to select).
- **Project-Aware Resolution**: Automatically infers project names from your working directory, git repositories, or container labels (like the `ports` CLI). Attach instantly via `specter <project-name>`.
- **Flexible Target Resolution**: Attach by listening port (`specter 3000`), process ID (`specter 14280`), Docker container name or ID (`specter web_app`), or project name (`specter example`).
- **Multi-Process / Worker Tracing**: Child worker processes (e.g. Node.js `cluster`, Gunicorn, Celery, Puma, Unicorn) that already exist when you attach are always traced, and workers started later are followed. `--tree` also traces deeper descendants, up to 3 levels down.
- **Smart Log Pipeline**: Reassembles split lines and multi-threaded streams, pretty-prints single-line JSON logs, highlights HTTP status codes, and groups multi-line stack traces (Node.js, Python, Go, Java).
- **Zero Configuration & Strictly Read-Only**: Works against already-running processes without code modifications, SDKs, or restarts. Never injects code or opens inbound network ports.
- **Shell Autocompletion**: Context-aware autocompletion for Bash, Zsh, Fish, and PowerShell that dynamically suggests active ports, container names, and projects.

---

## Interactive Discovery Picker

Running `specter` without arguments scans the system and opens an interactive terminal interface:

```text
[specter] Select an active dev server or container to stream (use ↑/↓ or j/k, Enter to select, Esc to quit):

  TYPE     PORT     PROJECT           PROCESS / IMAGE             PATH / TARGET
  ──────   ──────   ───────────────   ─────────────────────────   ─────────────
❯ host     :3000    Specter           specter-dev (PID 14280)     /home/user/projects/Specter
  docker   :8080    billing-service   node:20-alpine              billing-api (c5e3f1a09d)
  docker   :5432    database          postgres:16                 postgres-db (a1b2c3d4e5)
  host     :6379    -                 redis-server (PID 2104)     /etc/redis/redis.conf
```

In non-interactive environments (CI, pipes, scripts), Specter automatically outputs a cleanly aligned text table.

---

## Architecture & How It Works

```
specter [port | pid | container | project] (or interactive picker)
       │
       ▼
[Target Resolver]
       ├── Port Resolver       ─── Queries lsof / ss / procfs for listening TCP sockets
       ├── Project Resolver    ─── Inspects working directories, git root, package metadata
       ├── Docker Resolver     ─── Asks the `docker` CLI for mapped ports & containers
       └── Process Resolver    ─── Reads /proc/<pid>/cmdline, cwd, fd targets
       │
       ▼
[Worker Discovery]      ─── Finds child worker PIDs; --tree also finds deeper descendants
       │
       ▼
[Source Selector]
       ├── Docker              ─── Streams docker logs -f --tail 0
       ├── Systemd Journal     ─── Reads journalctl -f _PID=<pid>
       ├── Regular File        ─── Follows file descriptor target
       └── PTY / Syscall Trace ─── Attaches via optimized strace (trace=write,writev on fds 1 & 2)
       │
       ▼
[Line Assembler]        ─── Thread/FD buffer assembly, flush timer, streaming UTF-8 decoding
       │
       ▼
[Parser & Formatter]    ─── Log-level detection, single-line JSON formatting, stack trace grouping
       │
       ▼
[Output Renderer]       ─── ANSI colored stdout stream (respects NO_COLOR and pipes)
```

1. **Resolution**: Inspects network state using `lsof` or `ss` to identify the listening PID and checks the local Docker daemon for containerized port mappings. Inspects `/proc/<pid>/cmdline`, `/proc/<pid>/cwd`, and maps `/proc/<pid>/fd/1` and `/proc/<pid>/fd/2`.
2. **Source Selection**: Chooses the optimal acquisition mechanism:
   - **Docker Container** (checked first): Streams through the `docker` CLI (`docker logs`).
   - **Systemd Service**: Streams via `journalctl` if governed by systemd.
   - **File Redirection**: Follows file descriptors if stdout/stderr point to regular log files.
   - **Terminal / PTY**: Uses `strace` to watch `write()` and `writev()` syscalls for descriptors 1 and 2, including writes from concurrent threads and worker processes.
3. **Stream Assembly**: Hex buffers (`-xx`) are decoded into raw byte chunks. The `LineAssembler` handles thread interleaving, reassembles split lines, and flushes interactive prompts promptly.
4. **Formatting & Filtering**: Automatically categorizes log levels (`DEBUG`, `INFO`, `WARN`, `ERROR`), highlights HTTP status codes, and groups multi-line stack traces before rendering.

---

## CLI Reference

### Basic Usage

```bash
# 1. Interactive picker (select from active ports, projects, containers)
specter

# 2. Attach by listening TCP port
specter 3000
specter 8080

# 3. Attach by project name (auto-detected from directory)
specter my-frontend-app

# 4. Attach by Docker container name or ID
specter redis-cache
specter 7a4b1c2d3e4f

# 5. Attach by Process ID (PID)
specter 18492
```

### Flags

| Flag | Shorthand | Description |
| :--- | :--- | :--- |
| `--level <lvl>` | `-l` | Minimum log level to display (`debug`, `info`, `warn`, `error`). An unknown value is an error. |
| `--grep <pattern>` | `-g` | Filter output by text or regular expression. Preserves grouped stack traces. |
| `--stderr` | | Stream only standard error (fd 2). Useful for isolating errors/crashes. |
| `--tree` | `-t` | Also trace deeper descendants of the target (up to 3 levels). Direct child workers are always traced. |
| `--source <name>` | `-s` | Force a specific capture engine (`docker`, `file`, `journal`, `strace`). |
| `--json` | | Output formatted single-line JSON records. |
| `--raw` | | Bypass line parsing and ANSI color formatting (pure raw stream). |
| `--no-color` | | Disable ANSI terminal color codes (also adheres to `NO_COLOR`). |

### Examples

```bash
# Stream warnings and errors only from port 3000
specter 3000 --level warn

# Filter for authentication errors across worker processes (direct workers need no flag)
specter 8000 --grep "auth|unauthorized|invalid token"

# Isolate unhandled exceptions and crashes on stderr
specter 3000 --stderr

# Trace a Docker container with raw JSON output
specter api-container --json
```


### Notices and truncation

Specter prints its own status messages to stderr, so `specter 3000 | grep 500` is never polluted by them. You may see:

- `[specter:notice] Target writes to its own log file <path>, which Specter does not follow.` The process logs to a file instead of stdout/stderr. Specter names the file once, so you can `tail -f` it.
- `[specter:notice] Nothing captured yet.` Nothing was captured in the first 10 seconds. The target may be idle, buffering its output, or logging somewhere else. Output hidden by `--level` or `--grep` does not trigger this.
- `[specter:notice] --grep "..." is not a valid regular expression` The pattern is matched as literal text instead.

A single write larger than 64 KB cannot be captured whole. Specter keeps the first 64 KB and marks the cut in the stream itself, on stdout, so you can see which line is damaged:

```
AAAA...AAAA[specter: 34465 more bytes not captured]
```

The marker is also shown with `--raw`. A line over 64 KB that was built from several smaller writes is cut when rendered and ends with `... [specter: truncated at 64KB]`.

---

## Performance While Attached

When Specter uses the `strace` source, it slows the target. `strace` stops the process at every system call, including the ones it does not print, so the cost depends on how often the server makes calls and writes.

On two test servers, throughput fell by about 60% to 90% while Specter was attached: from about 2,100 to 800 requests per second on one, and from about 14,000 to 1,500 on the other. Your server's result will differ, so try Specter on a staging copy before attaching to anything important.

The slowdown ends when Specter detaches (Ctrl+C). The `docker`, `file` and `journal` sources read output that already exists and do not slow the target. The `Source:` line Specter prints when it attaches tells you which one is in use.

---

## Shell Autocompletion

Specter provides dynamic autocompletion for active ports, container names, and project directories.

```bash
# Bash
source <(specter completion bash)

# Zsh
source <(specter completion zsh)
# Or save to your zsh completion directory:
specter completion zsh > "${fpath[1]}/_specter"

# Fish
specter completion fish | source

# PowerShell (WSL2 / Windows)
specter completion powershell | Out-String | Invoke-Expression
```

---

## System Requirements & Platform Support

Specter is engineered as a Linux-first systems tool leveraging Linux kernel interfaces (`/proc`, `ptrace(2)`, `lsof`, `ss`, and the `docker` CLI).

- **Linux**: Native support on all standard distributions (Ubuntu, Debian, Fedora, Arch, Alpine).
- **Windows**: Fully supported via **WSL2** (Windows Subsystem for Linux).
- **macOS**: Native macOS is currently out of scope due to macOS System Integrity Protection (SIP) restrictions on `ptrace`.

### Dependencies

- **Runtime**: `lsof` (or `iproute2`/`ss`), `strace` (for PTY/terminal snooping), and optional `docker` CLI.
- **Build**: Go 1.26 or higher.

### Kernel Permissions (`ptrace_scope`)

On Linux systems enforcing restricted ptrace access (`/proc/sys/kernel/yama/ptrace_scope`), attaching to non-child processes via strace may require elevated permissions:

```bash
# Check current ptrace scope (0 = unrestricted, 1 = restricted to child processes)
cat /proc/sys/kernel/yama/ptrace_scope

# Option A: Run Specter with sudo
sudo "$(which specter)" 3000

# Option B: Grant strace the ptrace capability permanently
sudo setcap cap_sys_ptrace+ep $(which strace)

# Option C: Temporarily allow ptrace snooping (resets on reboot)
sudo sysctl -w kernel.yama.ptrace_scope=0
```

---

## Building from Source

```bash
# Clone the repository
git clone https://github.com/YasserYG8/Specter.git
cd Specter

# Run tests
go test ./...

# Build binary
go build -o specter ./cmd/specter

# Cross-compile for Linux (from Windows PowerShell)
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o specter ./cmd/specter
```

---

## Contributing

Contributions are welcome, especially native macOS and Windows support. See [CONTRIBUTING.md](CONTRIBUTING.md) to get started.

---

## License

[MIT](LICENSE)
