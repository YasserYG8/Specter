## Context

Specter previously relied exclusively on `strace` to intercept `write()` syscalls on descriptors 1 and 2. While `strace` is an effective universal fallback, `ptrace(2)` tracing introduces syscall latency and can be blocked by kernel security controls (`yama/ptrace_scope`). In practice, many modern server workloads run in environments where logs are already being collected natively:
- Containerized processes (Docker, Compose) have standard streams aggregated by the container runtime.
- Traditional daemons (Nginx, Apache, custom services) redirect descriptors to regular log files on disk.
- System daemons run as systemd units with output ingested by systemd-journald.
- Multi-process servers (Node.js cluster, Gunicorn, Celery) spawn worker processes whose output is lost without process tree tracing (`--tree`).

This design introduces a pluggable Source layer with smart runtime detection to select the lowest-overhead capture strategy.

## Goals / Non-Goals

**Goals:**
- Provide `DockerSource` to acquire logs directly via `docker logs -f --tail 0 <container>` when a target is containerized.
- Provide `FileTailSource` to stream logs via non-blocking file tailing when `/proc/<pid>/fd/1` or `fd/2` points to regular disk files.
- Provide `JournalSource` to stream logs via `journalctl -f _PID=<pid>` for systemd-managed services.
- Provide `SourceSelector` to automatically probe target `/proc` metadata and choose the optimal source in priority order: Docker > Journal > File > Strace.
- Support `--source <docker|file|journal|strace>` flag to allow users to force a specific capture mechanism.
- Support `--tree` flag in `StraceSource` by passing `-f` to trace child forks, clones, and worker processes.

**Non-Goals:**
- Heavy container SDK dependencies: Docker and systemd interactions use CLI wrappers (`docker`, `journalctl`) via `exec.CommandContext`, avoiding bloated dependencies and preserving a small static binary.
- Backfilling old logs: All sources stream live logs starting from the moment of attachment (`--tail 0` semantics).
- Kubernetes pod log streaming (deferred to a future release).

## Decisions

### 1. External CLI Execution vs Embedded SDKs
- **Choice**: Execute native CLI utilities (`docker`, `journalctl`) with `exec.CommandContext` rather than importing Docker SDK or systemd Go bindings.
- **Rationale**: Keeps the compiled binary under 10MB, eliminates complex CGo dependencies, and seamlessly reuses the user's current environment permissions and Docker socket paths.
- **Alternatives Considered**: Direct Docker engine API via HTTP socket — rejected due to excessive client code and version negotiation complexity.

### 2. Cgroup-Based Container Detection
- **Choice**: Read `/proc/<pid>/cgroup` to detect Docker container IDs. Modern systemd/cgroups v2 paths format container scopes as `docker-<id>.scope` or `actions_runner/.../docker/<id>`, while cgroups v1 formats as `/docker/<id>`.
- **Rationale**: Fast in-memory inspection requiring zero external process execution or privileges.
- **Alternatives Considered**: Querying `docker ps` for all running containers and matching listening ports — slower and requires querying the Docker daemon before inspecting `/proc`.

### 3. File Tail Implementation
- **Choice**: Check if `/proc/<pid>/fd/1` or `/proc/<pid>/fd/2` resolve to regular disk files (excluding `/dev/pts/*`, `/dev/null`, sockets, and pipes). Open the target path, seek to the end (`io.SeekEnd`), and stream newly appended bytes with periodic polling (50ms) on EOF.
- **Rationale**: Provides zero-overhead streaming without touching the target process via ptrace.
- **Alternatives Considered**: Inotify / fsnotify — rejected because simple polling is robust across network mounts, NFS, and WSL shared drives without descriptor leak risks.

### 4. Source Priority Order
- **Choice**:
  1. `--source` flag override (if specified, validates and uses the requested source).
  2. If container cgroup found and `docker` executable exists → `DockerSource`.
  3. If systemd unit found and `journalctl` exists → `JournalSource`.
  4. If `/proc/<pid>/fd/1` or `fd/2` points to a regular file → `FileTailSource`.
  5. Default fallback → `StraceSource` (honoring `--tree`).

## Risks / Trade-offs

- **[Risk]** Process terminates during container or file tailing.
  - **Mitigation**: Detect EOF or process exit by monitoring target PID status concurrently with stream reader.
- **[Risk]** Target writes to both stdout file and stderr pty.
  - **Mitigation**: `FileTailSource` tails both resolved descriptor files concurrently if both point to disk.
- **[Risk]** High-volume child processes in `strace -f` (`--tree`).
  - **Mitigation**: Strace parser already isolates chunks by thread PID and FD, ensuring thread interleaving is properly reassembled by `LineAssembler`.

## Docker Container Research & Edge Cases (Preserved for Reference)

During architectural exploration, several critical real-world Docker edge cases were analyzed to guide both Phase 2 implementation and future enhancements:

### 1. The `docker-proxy` Trap (Bridge Network Port Mapping)
- **Problem**: When a container maps ports to the host (`-p 3000:3000`), `lsof` and `ss` report the listening process as `/usr/bin/docker-proxy`, not the application process. Attaching `strace` to `docker-proxy` yields zero output because it is merely a TCP packet relay.
- **Resolution**: Port resolution detects if the owning PID is `docker-proxy`. If so, it leapfrogs to container discovery via `docker ps --filter publish=<port>` or `docker-proxy` commandline args (`-container-ip`, `-container-port`).

### 2. Custom Logging Drivers (`--log-driver`) & Socket Permissions
- **Problem**: In production or Compose environments with `--log-driver=awslogs`, `fluentd`, `syslog`, or `none`, `docker logs` fails with `configured logging driver does not support reading`. Additionally, users without Docker group membership will get socket permission denied.
- **Resolution**: Because container processes run directly on the Linux host kernel, Specter knows the container's host PID. If `docker logs` fails for any reason (driver unsupported, socket permission denied, daemon paused), Specter falls back seamlessly to attaching `strace` to the host PID.

### 3. Host Networking (`--net=host`)
- **Problem**: In `--net=host` mode, there is no `docker-proxy`. The application process binds the host network interface directly.
- **Resolution**: Port resolution finds the application PID directly on the host. Inspecting `/proc/<pid>/cgroup` detects the `docker-<id>.scope` marker and unlocks `docker logs` streaming or direct tracing.

### 4. Unpublished Internal Ports (Future Extension)
- **Problem**: Containers communicating across private Docker Compose networks (e.g., `web:8080`) do not bind host ports, making resolution by port number impossible from the host network namespace.
- **Resolution**: In a future enhancement, Specter will support passing container names or service names directly (e.g., `specter my-container` or `specter web`) in addition to numeric ports and PIDs.

