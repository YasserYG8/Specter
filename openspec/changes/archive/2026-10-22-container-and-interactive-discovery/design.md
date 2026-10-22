## Context

Specter is a Linux-first developer CLI designed for zero-config live log streaming by port or PID. In its current implementation, the CLI enforces `cobra.ExactArgs(1)` and strictly parses the argument as an integer port or PID (`strconv.Atoi`). Running `specter` with zero arguments fails with an unhelpful usage error, and passing a container name (such as `specter phpmyadmin`) fails validation immediately.

In modern containerized development setups (including Linux Docker engines and Windows WSL2 + Docker Desktop environments), servers frequently run inside containers whose published ports are forwarded across network namespaces or host proxies. On WSL2, for instance, `ss -ltnp` detects listening sockets forwarded from Windows or the Docker WSL distro without associating any host PID in `/proc`. Specter previously treated these ports as inaccessible root processes, directing users to run with `sudo`, which still failed because no host PID exists in the distro's process table.

At the same time, Specter already possesses a robust `docker` log engine (`internal/source/docker`) capable of streaming container stdout/stderr cleanly via `docker logs -f --tail 0 <container>`. Connecting port resolution to Docker container discovery and introducing an interactive zero-argument server picker bridges this gap, making Specter universally usable across host and containerized workflows.

## Goals / Non-Goals

**Goals:**
- **Zero-Argument Server Picker**: When `specter` is invoked without arguments in an interactive terminal (TTY), scan active host listening ports and running Docker containers, presenting a clean keyboard-navigable menu (`↑`/`↓` and `<Enter>`) to select and stream a target immediately.
- **Non-TTY Fallback**: When `specter` is invoked without arguments in a non-interactive or piped environment (CI, scripts, redirects), output an organized, non-interactive status table of active services and exit gracefully.
- **Docker Port Fallback**: When resolving a port (e.g. `specter 8080`), if local `/proc` inspection fails to find a host PID or indicates an unmapped proxy socket, query Docker to find any running container mapping that port and attach via the Docker source.
- **Named Container Targets**: Allow passing container names or container IDs directly as CLI arguments (e.g. `specter phpmyadmin`, `specter mysql_db`, `specter 13cbb067d0f2`), verifying container existence and streaming its logs.
- **Unified Autocompletion**: Update shell completions to dynamically suggest active host ports, container mapped ports, and running container names with descriptive metadata.

**Non-Goals:**
- Heavyweight TUI dependencies: Avoid large framework libraries (like Bubbletea or tview) that increase binary footprint and complicate signal handling.
- Docker process management: Specter is strictly a non-invasive log streaming tool; it will not start, stop, restart, or kill containers.
- Replacing specialized container managers: Specter focuses on immediate developer stream access, not full dashboard container management (e.g., lazydocker).

## Decisions

### 1. Lightweight Raw-Mode Terminal Picker vs External TUI Framework
- **Decision**: Implement the interactive picker using standard ANSI escape codes (`\033[?25l` to hide cursor, `\033[A` to move up, `\r\033[K` to clear line) paired with `golang.org/x/term` for raw input handling (`term.MakeRaw`).
- **Rationale**: Keeps the compiled binary lean (~4MB), preserves fast instantaneous startup (<10ms), and prevents event loop conflicts with Specter's pipeline signal handling.
- **Alternatives Considered**:
  - *Bubbletea / tview*: High feature set, but introduces substantial binary bloat, dozens of transitive dependencies, and complex alternate screen-buffer state management for what is a simple single-select menu.
  - *Plain prompt / `bufio.Scanner`*: Requires users to type out port numbers or container names manually, losing the convenience of arrow-key browsing.

### 2. Docker Container & Port Mapping Discovery via CLI
- **Decision**: Query Docker using `docker ps --format "{{json .}}"` and `docker inspect <target>` through Specter's existing `CommandRunner` abstraction with strict context timeouts (500ms).
- **Rationale**: Reuses the tested `CommandRunner` pattern from `PortResolver`. It inherits user Docker context, rootless settings, and WSL2 interoperability without pulling in the massive Docker Go SDK (`github.com/docker/docker`).
- **Parsing Strategy**: Parse JSON lines returned by `docker ps`. Extract container ID, primary Name (stripping leading `/`), Image, and parse the published host ports from the `Ports` string (e.g., `0.0.0.0:8080->80/tcp` $\rightarrow$ host port 8080).
- **Graceful Fallback**: If `docker` is not installed, not in `PATH`, or the daemon is not running (e.g., daemon socket unreachable), queries time out or return empty results without erroring out host process discovery.

### 3. Unified Multi-Tier Target Resolution Pipeline
- **Decision**: Restructure argument handling in `cmd/specter/main.go` from `cobra.ExactArgs(1)` to `cobra.RangeArgs(0, 1)`:
  - **0 arguments**: Run interactive discovery picker (or print status table in non-TTY). If an item is selected, proceed with streaming that target.
  - **1 argument (numeric $\le 65535$)**:
    1. Attempt host port resolution via `lsof` / `ss`.
    2. If host PID is resolved, attach to host PID.
    3. If host PID is missing, unresolvable, or permission-masked, check Docker mapped container ports. If found, attach to container.
    4. If no container matches, return standard port diagnostic with proximity suggestions.
  - **1 argument (numeric $> 65535$)**: Attempt host PID inspection.
  - **1 argument (alphanumeric string or container name/ID)**: Inspect via Docker. If valid running container, attach via `docker` source. If invalid, return descriptive target error.
- **Rationale**: Preserves 100% backward compatibility for existing port and PID workflows while seamlessly accommodating container names and WSL2 forwarded ports.

### 4. Consolidated Service Model for Picker and Completions
- **Decision**: Introduce a unified `DiscoveredService` model in `internal/resolver` that merges host listening sockets and active Docker containers:
  ```go
  type DiscoveredService struct {
      Kind          ServiceKind // HostPort or DockerContainer
      Port          int
      ContainerPort int
      PID           int
      ProcessName   string
      ContainerID   string
      ContainerName string
      Image         string
      Description   string
      IsProtected   bool
  }
  ```
- **Rationale**: Provides a single source of truth for both the interactive picker and shell autocompletion (`ValidArgsFunction`), avoiding duplicate network scans and formatting logic.

## Risks / Trade-offs

- **[Risk] Docker daemon is stopped, slow, or inaccessible**
  $\rightarrow$ **Mitigation**: Docker discovery queries enforce a strict 500ms timeout context. If `docker` returns an error or times out, the resolver silently falls back to host `/proc` and network discovery without failing or lagging.
- **[Risk] Raw terminal mode left active if process is terminated unexpectedly**
  $\rightarrow$ **Mitigation**: Terminal state restoration (`term.Restore`) is deferred immediately upon entering raw mode, and OS signals (SIGINT, SIGTERM) trigger restoration before exit.
- **[Risk] High port count or container count overflowing small terminal windows**
  $\rightarrow$ **Mitigation**: The picker computes visible page windows based on terminal height (`term.GetSize`), scrolling smoothly when list items exceed available lines.
- **[Risk] Non-TTY / Piped execution hanging on raw input**
  $\rightarrow$ **Mitigation**: Detect terminal availability via `term.IsTerminal(int(os.Stdin.Fd()))`. If false, bypass raw mode completely and print an informative, non-interactive table to stdout.
