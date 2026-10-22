## Context

Specter provides an interactive discovery picker and non-interactive table when executed without arguments. However, in real-world local development workflows, developers frequently run multiple microservices, backends, and frontend applications simultaneously. Under the current implementation, every Node.js or Python server appears identically as `node (PID xxxx)` or `python3 (PID yyyy)`, requiring developers to memorize raw port numbers or guess which process corresponds to which repository.

Tools like `ports-cli` and `port-whisperer` solve this by presenting the project or repository name alongside the port. By combining `/proc/<PID>/cwd` inspection, project manifest sniffing (`package.json`, `pom.xml`, `go.mod`, `pyproject.toml`), and Docker Compose label resolution, Specter can deliver a high-polish, comfortable, project-aware service discovery experience.

## Goals / Non-Goals

**Goals:**
- **Project Name Detection**: Automatically detect and display project names for host processes via `/proc/<PID>/cwd` and root manifest inspection (`package.json`, `pom.xml`, `go.mod`, `pyproject.toml`), falling back to `filepath.Base(cwd)`.
- **Docker Compose Awareness**: Parse `com.docker.compose.project` and `com.docker.compose.service` labels from Docker containers to display structured project names like `soa / phpmyadmin`.
- **Comfortable Column-Aligned Layout**: Upgrade both the interactive ANSI picker and the non-TTY table to use clean, fixed-width columns (`TYPE`, `PORT`, `PROJECT`, `PROCESS / IMAGE`, `PATH / TARGET`).
- **Path Sanitization**: Format directory paths nicely (e.g., abbreviating `$HOME` to `~`) so paths fit comfortably on standard terminal windows.
- **Project-Aware CLI Arguments & Completions**: Allow users to attach by project name (e.g., `specter example`) and tab-complete project names in their shell.

**Non-Goals:**
- Deep recursive filesystem searches or git tree analysis that would introduce perceptible latency (>10ms).
- Replacing the lightweight raw-terminal UI with heavy full-screen terminal frameworks.

## Decisions

### 1. CWD-Centric Fast Project Detection with Shallow Manifest Sniffing
- **Decision**: Read `/proc/<PID>/cwd` via `os.Readlink` (or existing `ProcessInspector`). If accessible:
  1. Base candidate: `filepath.Base(cwd)` (e.g. `example`).
  2. Shallow manifest check: Inspect if `package.json`, `pom.xml`, `go.mod`, or `pyproject.toml` exists directly in `cwd`. Read at most 4KB and parse the project or package name using lightweight extraction.
  3. Path presentation: Format `cwd` replacing `/home/<user>` with `~`.
- **Rationale**: Keeps detection nearly instantaneous (<1ms per process) with zero overhead.

### 2. Docker Compose Labels Parsing
- **Decision**: Parse the `Labels` field from `docker ps --format "{{json .}}"` for `com.docker.compose.project` and `com.docker.compose.service`.
- **Rationale**: Containers spun up via `docker compose up` carry these labels automatically. Displaying `soa / phpmyadmin` is significantly more informative than generic container hashes.

### 3. Column-Aligned Terminal Layout
- **Decision**: Redesign `FormatServiceItem` and `RenderNonInteractiveTable` to use structured columns:
  - Cursor: `> ` (2 chars)
  - Badge: `[HOST]  ` or `[DOCKER]` (8 chars)
  - Port: `:%-5d` (7 chars)
  - Project: `%-16s` (16 chars)
  - Process/Image: `%-16s` (16 chars)
  - Path/Target: `%-24s` (24 chars)
- **Rationale**: Eliminates ragged text and provides a modern, comfortable developer dashboard experience.

### 4. Direct Project Name Attachment & Completion
- **Decision**: In `main.go`, if single argument does not parse as an integer or match a raw container ID, check if it matches an active project name from discovered services. If matched, attach immediately to that port or container.
- **Rationale**: Enables developers to run `specter example` or `specter soa` effortlessly.

## Risks / Trade-offs

- **[Risk] Manifest file unreadable or malformed**
  $\rightarrow$ **Mitigation**: Manifest parsing errors are silently ignored and fall back immediately to `filepath.Base(cwd)`.
- **[Risk] Terminal width too narrow for all columns**
  $\rightarrow$ **Mitigation**: Query terminal width (`term.GetSize`). If columns exceed available width, truncate or omit the trailing path column gracefully.
- **[Risk] Process working directory is root (`/`) or system daemon**
  $\rightarrow$ **Mitigation**: If `cwd == "/"` or system directories (`/var`, `/usr`), omit the project column or leave empty.
