## Why

While `strace` provides a reliable universal attachment mechanism, running ptrace on production or containerized environments incurs syscall interception overhead and requires `ptrace_scope` capabilities. In modern Linux environments, servers frequently run inside Docker containers, log to disk files, or run under systemd service managers where direct native stream acquisition is dramatically faster, zero-overhead, and unconstrained by ptrace restrictions. Furthermore, multi-process servers (such as Node.js clusters, Gunicorn, or Celery) spawn child workers that are missed unless process tree tracing (`--tree`) is enabled.

## What Changes

- Implement `DockerSource` to detect containerized processes from `/proc/<pid>/cgroup` and stream output via `docker logs -f --tail 0`.
- Implement `FileTailSource` to follow log files directly when `/proc/<pid>/fd/1` or `fd/2` point to regular disk files.
- Implement `JournalSource` to stream logs via `journalctl -f _PID=<pid>` when the target process is managed by systemd.
- Implement `SourceSelector` to automatically detect target runtime environments and pick the lowest-overhead capture source, with `--source` flag override support (`docker`, `file`, `journal`, `strace`).
- Update `StraceSource` to support `--tree` (`-f`) for tracing forks, clones, and child worker processes.

## Capabilities

### New Capabilities
- `docker-source`: Acquire live stream from Docker/containerd containers using `docker logs -f --tail 0` without ptrace overhead.
- `file-source`: Acquire live stream by directly tailing disk files when `/proc/<pid>/fd/{1,2}` are regular files.
- `journal-source`: Acquire live stream from systemd journal using `journalctl -f _PID=<pid>`.
- `source-selector`: Determine the optimal log acquisition mechanism for a target based on process metadata and user overrides.

### Modified Capabilities
- `strace-source`: Add `--tree` support to follow child worker processes and threads via `strace -f`.

## Impact

- **New Packages / Files**:
  - `internal/source/docker/`: Docker container detection and streaming.
  - `internal/source/file/`: Disk file follower and non-blocking stream reader.
  - `internal/source/journal/`: Systemd journal follower.
  - `internal/source/selector/`: Source detection heuristics and selection dispatcher.
- **Modified Packages**:
  - `internal/source/strace/`: Spawn configuration with `-f` when tree tracing is requested.
  - `cmd/specter/main.go`: Wire SourceSelector and `--source`, `--tree` flags.
- **Dependencies**: Uses standard Go libraries and calls existing system utilities (`docker`, `journalctl`, `strace`) when present.
