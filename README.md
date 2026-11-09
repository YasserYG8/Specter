# Specter

A zero-configuration developer CLI in Go that attaches to any active server process or container by **port**, **PID**, **container name**, or **project name**, and streams live stdout and stderr directly to your terminal.

```bash
# Launch interactive service picker
specter

# Attach directly by port, project, container, or PID
specter 3000
specter my-api-project
specter api-gateway-container
specter --pid 18492
```

---

## Overview

When running backend microservices, dev servers, or background containers across multiple terminal sessions, locating logs often requires hunting down hidden terminal tabs, querying container IDs, or restarting processes with verbose log wrappers.

**Specter eliminates this workflow friction.** Given a port, PID, container, or project name—or simply running `specter` to open the interactive picker—Specter resolves the target, identifies where its standard streams are directed, attaches read-only (see [Performance While Attached](#performance-while-attached)), and streams the output in real time with line reassembly, JSON formatting, and log-level coloring.

---

## Install

Specter runs on Linux, and on Windows through WSL2. Release files are Linux only: `.tar.gz` archives and `.deb` and `.rpm` packages for `amd64` and `arm64`. There is no macOS or Windows download, because attaching to a process needs Linux kernel interfaces. On Windows, install it inside WSL2. On macOS, run your server and Specter in a Linux VM (for example OrbStack, UTM or Lima) or a Docker container.

**1. One command** (needs `curl` or `wget`):

```bash
curl -sSL https://raw.githubusercontent.com/YasserYG8/Specter/main/install.sh | sh
```

The script checks that you are on Linux, downloads the release for your chip (`amd64` or `arm64`), checks its SHA-256 against `checksums.txt`, and installs `specter` to `/usr/local/bin` (it uses `sudo` only if that folder is not writable). It installs nothing else: if `strace` or `lsof` is missing, it prints the command to install them. Set `SPECTER_VERSION=v0.1.0` to pick a release, or `SPECTER_INSTALL_DIR=$HOME/.local/bin` to install without `sudo`.

**2. Read the script first**, if you do not like piping a script into `sh`:

```bash
curl -sSLO https://raw.githubusercontent.com/YasserYG8/Specter/main/install.sh
less install.sh
sh install.sh
```

**3. A package** from the [latest release](https://github.com/YasserYG8/Specter/releases/latest) page. The packages list `strace`, `lsof` and `iproute2` as recommended packages:

```bash
sudo apt install ./specter_0.1.0_linux_amd64.deb     # Debian, Ubuntu
sudo dnf install ./specter_0.1.0_linux_amd64.rpm     # Fedora, RHEL
```

**4. With Go** (1.26 or higher):

```bash
go install github.com/YasserYG8/Specter/cmd/specter@latest
```

This puts `specter` in `$(go env GOPATH)/bin`, so make sure that folder is on your `PATH`.

To uninstall what the script installed: `sudo rm /usr/local/bin/specter`.

If you did not use a package, set up the tools Specter uses:

```bash
sudo apt install strace lsof iproute2
```

Specter has two ways to capture a terminal or a pipe. The **`ebpf` engine** reads the output inside the kernel and never stops the target, and it needs `sudo` and Linux 5.8 or later. The **`strace` engine** works everywhere, but it slows the target. Specter picks `ebpf` when it can run, and otherwise falls back to `strace` and tells you why in one line. Read [Performance While Attached](#performance-while-attached) before pointing it at a busy server.

Ubuntu and WSL2 restrict `ptrace` by default, so attaching to a running process fails with `Operation not permitted` until you allow it. Either run `sudo "$(which specter)" 3000`, or see [Kernel Permissions](#kernel-permissions-ptrace_scope) for the other options.

---

## Key Features

- **Interactive Discovery Picker**: Run `specter` with zero arguments to browse all active dev servers, listening ports, Docker containers, and project directories in a comfortable, keyboard-driven UI (`↑`/`↓` or `k`/`j`, `Enter` to select).
- **Project-Aware Resolution**: Automatically infers project names from your working directory, git repositories, or container labels (like the `ports` CLI). Attach instantly via `specter <project-name>`.
- **Flexible Target Resolution**: Attach by listening port (`specter 3000`), process ID (`specter --pid 14280`), Docker container name or ID (`specter web_app`), or project name (`specter example`).
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
       ├── eBPF                ─── Kernel programs read write/writev of the target; it is never stopped (root, Linux 5.8+)
       └── PTY / Syscall Trace ─── Attaches via strace (trace=write,writev on fds 1 & 2); slows the target
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
   - **eBPF** (terminal or pipe, when this machine can run it: root or `CAP_BPF` with `CAP_PERFMON`, Linux 5.8 or later, and `/sys/kernel/btf/vmlinux`): small programs in the kernel keep only the target's own `write()` and `writev()` output, up to 64 KB per write, and hand it to Specter through a ring buffer. The target is never stopped and shows no tracer, so a process that `gdb` or another `strace` already traces can still be watched. Workers, `--tree`, writes through reopened terminal descriptors (Node.js) and the exit code work as with `strace`. If the target writes faster than Specter reads, Specter says how many writes were lost.
   - **Terminal / PTY, as the fallback**: Uses `strace` to watch `write()` and `writev()` syscalls for descriptors 1 and 2, including writes from concurrent threads and worker processes. A child process is shown only when its output goes to the same place as the server's own stdout or stderr. Helpers that write to a private pipe or to `/dev/null` (for example the tools a Next.js dev server starts) are not shown, because their lines never reach your terminal either. Node.js servers started in a terminal (`npm run dev`, Next.js, Express, Vite) write through a descriptor that Node opened again from the terminal, such as fd 22, so Specter also follows any other descriptor that leads to the same terminal, pipe or socket as the server's stdout or stderr.
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
specter --pid 18492
specter -p 18492
```

A bare number is always a **port**. If nothing listens on it, Specter says so and attaches to nothing. It never falls back to the process whose PID happens to be the same number, because that is almost never the server you meant. When such a process exists, the message names it and shows the command to attach to it:

```
[specter] No process is listening on port 3000.
...
Hint: PID 3000 exists (systemd-journald). To attach to it: specter --pid 3000
```

A number above 65535 cannot be a port, so `specter 70000` attaches to PID 70000.

### Flags

| Flag | Shorthand | Description |
| :--- | :--- | :--- |
| `--pid <pid>` | `-p` | Attach to the process with this PID, whatever its value. It is never checked as a port. Give one PID, and not together with a port, container or project. |
| `--level <lvl>` | `-l` | Minimum log level to display (`debug`, `info`, `warn`, `error`). An unknown value is an error. A line whose level Specter cannot find is always shown, never hidden. Levels are read from JSON and `level=` fields, `[INFO]`, `INFO:`, `| INFO |`, `app.INFO:`, `<timestamp> INFO ...`, and the common Python formats: the `logging` cookbook (`2026-11-07 10:12:50,516 - app - INFO - ...`), Gunicorn, loguru and Celery (`[2026-11-07 10:12:50,516: ERROR/ForkPoolWorker-1] ...`). A level word inside a message is not used. |
| `--grep <pattern>` | `-g` | Filter output by text or regular expression. A stack trace is shown whole when any of its lines matches, and not at all otherwise. To do this Specter holds a trace until it ends (up to 200 ms after its last line), so lines of other processes may appear before it. |
| `--stderr` | | Stream only standard error (fd 2). Useful for isolating errors/crashes. Limit: when a Node.js server runs in a terminal, its output goes through reopened descriptors, and when stdout and stderr are the same terminal Specter cannot tell which one a line came from. Those lines count as stdout, so `--stderr` hides them. Redirecting the server's stderr separately (for example `2>err.log`) makes the two distinct again. |
| `--tree` | `-t` | Also trace deeper descendants of the target (up to 3 levels). Direct child workers are always traced. |
| `--source <name>` | `-s` | Force a specific capture engine (`docker`, `file`, `journal`, `ebpf`, `strace`). `--source ebpf` never falls back: if eBPF cannot run, Specter says why and exits before attaching. `--source strace` keeps the engine that slows the target, and prints no tip. |
| `--duration <time>` | | Detach after this time, for example `30s`, `2m` or `1h30m`. The time starts when the capture starts. Specter prints `Detached from PID N after 30s.` and exits with status 0. A number without a unit (`30`) or a negative value is refused before anything is attached. Without the flag, or with `0`, there is no limit. Works with every source. |
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

# Look for 30 seconds, then detach by itself (the target is slowed only for that long)
specter 3000 --duration 30s

# Trace a Docker container with raw JSON output
specter api-container --json
```


### Notices and truncation

Specter prints its own status messages to stderr, so `specter 3000 | grep 500` is never polluted by them. You may see:

- `[specter:notice] strace slows the target while Specter is attached. Use it on development and test servers, and use --duration to limit the time.` Printed once, right after the `Source:` line, and only for the `strace` engine. When `strace` was chosen because the fast engine could not run, the same line ends with the reason, for example `Run with sudo to use the fast eBPF engine.` or `eBPF needs Linux 5.8 or later (this kernel is 5.4).` The other sources, including `ebpf`, do not slow the target.
- `[specter:notice] 120 writes were lost because the target wrote faster than Specter could read.` With the `ebpf` engine only: the ring buffer between the kernel and Specter was full. Printed at most once per second, with the number of writes lost since the last notice, and never when nothing was lost. See [Performance While Attached](#performance-while-attached).
- `[specter:notice] Target writes to its own log file <path>, which Specter does not follow.` The process logs to a file instead of stdout/stderr. Specter names the file once, so you can `tail -f` it.
- `[specter:notice] Nothing captured yet.` Nothing was captured in the first 10 seconds. The target may be idle, buffering its output, or logging somewhere else. Output hidden by `--level` or `--grep` does not trigger this.
- `[specter:notice] --grep "..." is not a valid regular expression` The pattern is matched as literal text instead.

A single write larger than 64 KB cannot be captured whole. Specter keeps the first 64 KB and marks the cut in the stream itself, on stdout, so you can see which line is damaged:

```
AAAA...AAAA[specter: 34465 more bytes not captured]
```

The marker is also shown with `--raw`. A line over 64 KB that was built from several smaller writes is cut when rendered and ends with `... [specter: truncated at 64KB]`.

### When the target stops

If the server you are watching stops, Specter stops too, prints one line about how it ended, and exits with status **1**:

```
[specter] Target PID 617 exited with code 3.
[specter] Target PID 595 was killed by SIGTERM.
[specter] Target PID 595 was killed by SIGKILL.
[specter] SIGKILL often comes from the kernel's out-of-memory killer. To check: dmesg | grep -i oom
[specter] Container billing-api exited with code 137.
```

When the exit code cannot be known (a process that writes to a log file, for example, because only a parent can read the exit code of a process after it is gone), the line is `Target PID 595 exited.` Specter notices within about a second.

`Detached from PID N cleanly.` is printed only when **you** stop Specter with Ctrl+C, and then the exit status is **0**. After `--duration` the line is `Detached from PID N after 30s.`, also with status **0**. A script can tell the two apart by the exit status. A server that systemd restarts is not reported as stopped: when Specter follows a systemd unit, it keeps following it.

---

## Performance While Attached

Specter has two engines for a terminal or a pipe, and they cost very different amounts.

- **`ebpf`** reads the output inside the kernel and never stops the target. It needs root and Linux 5.8 or later, and Specter picks it when it can.
- **`strace`** stops the process at every system call, including the ones it does not print, so the cost depends on how often the server makes calls and writes. Specter uses it when `ebpf` cannot run, and says so in a notice.

**Production use is not recommended yet.** Use Specter on development and test servers. The `strace` engine should never be attached to a server you care about. The `ebpf` engine is much lighter, but its numbers below come from WSL2 on a laptop, which is noisy, and the rule is to confirm them on a native Linux machine first (`scripts/bench-overhead.sh` and the `bench` workflow do that).

These were measured with [`scripts/bench-overhead.sh`](scripts/bench-overhead.sh) on 2026-11-09: a small HTTP server that writes one log line per request, on a 4-core laptop (AMD Ryzen 5 3500U) under WSL2, run as root. Each figure is the median of 5 runs of 10 seconds, with and without Specter attached. Latency is counted from the time each request was due, so a server that falls behind shows its queue.

| Engine | Load | Median latency without → with Specter | Requests per second without → with |
| :--- | :--- | :--- | :--- |
| `strace` | 100 requests/s | 2.1 ms → 5.4 ms | 100 → 100 |
| `strace` | 1,000 requests/s | 1.3 ms → 819 ms | 1,000 → 885 |
| `strace` | As fast as the server goes | 3.9 ms → 243 ms | 10,400 → 225 |
| `ebpf` | 100 requests/s | 2.25 ms → 2.35 ms (+4%) | 100 → 100 |
| `ebpf` | 1,000 requests/s | 1.37 ms → 1.50 ms (+9.7%) | 1,000 → 1,000 |
| `ebpf` | As fast as the server goes | 2.6 ms → 4.0 ms | 16,000 → 10,200 |

What the numbers say:

- With `strace`, at 1,000 requests per second the server can no longer answer on time, so requests queue and latency grows by hundreds of milliseconds. At full speed throughput fell by about 98%.
- With `ebpf`, the server keeps its rate at 100 and 1,000 requests per second, and the median latency rises by under 10%. The 1,000 requests per second result is only just under that line, and the runs with Specter varied a lot (from 1.29 to 1.86 ms), so treat it as "small", not as an exact figure. The 99th percentile went from 4.9 ms to 23 ms.
- Every line was captured at 100 and 1,000 requests per second, with both engines (60,000 of 60,000).
- At full speed, the `ebpf` engine captured 79% of the lines (509,403 of 646,084). Specter's own processing of each line (parsing, formatting and printing) used about 1.5 cores at 16,000 lines per second and could not keep up, so the buffer between the kernel and Specter filled and the kernel dropped writes. The target was never slowed by this. Specter counts the lost writes and prints `N writes were lost because the target wrote faster than Specter could read.`, so a loss is never silent. At 1,000 lines per second Specter used about 75% of one core, with either engine.

Your server will give different numbers. Run the script on a copy of it, or on a machine like it, before deciding: `ENGINE=ebpf scripts/bench-overhead.sh` or `ENGINE=strace scripts/bench-overhead.sh`. The full reports are in [`test/bench/results/`](test/bench/results/). Two ways to limit the effect of `strace`: attach for a short time with `--duration 30s`, and use a source that does not slow the target when you can.

The slowdown ends when Specter detaches (Ctrl+C or the end of `--duration`). If Specter is killed, for example with `kill -9`, the kernel removes the eBPF programs, and makes `strace` detach by itself, so the server is not left traced or hooked. The `docker`, `file` and `journal` sources read output that already exists and do not slow the target. The `Source:` line Specter prints when it attaches tells you which one is in use.

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
- **macOS**: Not supported natively. macOS System Integrity Protection (SIP) blocks `ptrace` on other processes. Run your server and Specter inside a Linux VM (for example OrbStack, UTM or Lima) or a Docker container. The `linux/arm64` build covers VMs on Apple Silicon.

Running `specter <target>` on Windows or macOS stops with a message that says so and what to do. `specter version`, `specter completion` and `--help` work everywhere.

### Dependencies

- **Runtime**: `lsof` (or `iproute2`/`ss`), `strace` (for PTY/terminal snooping, and as the fallback when eBPF cannot run), and optional `docker` CLI.
- **For the `ebpf` engine** (optional, and faster): Linux 5.8 or later, kernel type information at `/sys/kernel/btf/vmlinux`, and root or the capabilities `CAP_BPF` and `CAP_PERFMON`. Most current Ubuntu, Debian and Fedora kernels have the type information. The compiled kernel program is built into the `specter` binary, so no compiler is needed.
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

Contributions are welcome. Native macOS and Windows attach is not possible with `ptrace`, so the open ideas for those systems are a Docker-only mode and a `specter run` mode that starts the server itself. See [CONTRIBUTING.md](CONTRIBUTING.md) to get started.

---

## License

[MIT](LICENSE)
