## Why

Modern web servers frequently employ multi-process supervisor/worker architectures:
- **FastAPI / Uvicorn**: The reloader process (`WatchFiles`) holds the port and spawns a worker child process to handle HTTP requests.
- **Gunicorn / Celery / Node.js Cluster**: A master supervisor process binds the listening socket and forks worker processes to execute workloads.

Currently, when a developer attaches Specter to a port (`specter 8000`), Specter discovers the parent supervisor PID. However, in Linux, `strace -p <PID> -f` only traces *future* forks and clones created *after* attachment — it does not attach to child worker processes that were already running before Specter started. Consequently, developers observing Uvicorn or Gunicorn see a silent terminal because HTTP requests are processed exclusively by the child worker.

Automating pre-existing child worker discovery and multi-PID attachment eliminates manual PID lookup and ensures zero-configuration log streaming across all multi-process server architectures.

## What Changes

- **Automatic Child Process Discovery**: Inspect Linux `/proc` to recursively discover all active child processes belonging to the target process tree (`PPid == target.PID`).
- **Multi-PID Strace Attachment**: Pass multiple `-p <pid>` arguments to `strace` (`-p <parent_pid> -p <child1_pid> ...`) so both the supervisor and its active child workers are traced simultaneously.
- **Dynamic Tree Tracing via `--tree`**: When `--tree` is specified, child discovery is enabled comprehensively across deep process hierarchies.
- **Diagnostic UX**: Display discovered child worker PIDs in the attachment banner (e.g. `[specter] Detected 1 active child worker: PID 629`).

## Capabilities

### Modified Capabilities
- `strace-source`: Update strace source attachment to automatically discover pre-existing child worker processes and trace all target PIDs concurrently.

## Impact

- **Affected Code**:
  - `internal/source/strace/spawn.go`: Add child PID discovery and support multiple `-p` arguments when invoking `strace`.
  - `internal/source/types.go` & `internal/resolver`: Provide helper to scan `/proc` for child PIDs by parent PID.
  - `cmd/specter/main.go`: Update attachment message to report discovered child worker PIDs.
- **Dependencies**: No external dependencies; utilizes native Linux `/proc` filesystem.
