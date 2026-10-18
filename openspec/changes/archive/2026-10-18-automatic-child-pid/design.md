## Context

In supervisor/worker architectures (FastAPI with Uvicorn `--reload`, Gunicorn, Celery, Node.js `cluster`), the process listening on the network port is often a parent supervisor that does not directly log HTTP requests or business logic. Instead, request handling is delegated to one or more child worker processes.

Because Linux `strace -f -p <PID>` attaches to threads and follows *future* child processes spawned *after* attachment, pre-existing child processes spawned *before* attachment are not captured. To capture all output without requiring the developer to hunt for child PIDs manually, Specter must automatically discover existing child worker processes and attach to all of them concurrently.

## Goals / Non-Goals

**Goals:**
- Automatically scan the Linux `/proc` filesystem to identify active child processes of the target PID (`PPid == target.PID`).
- Pass all discovered child PIDs alongside the parent PID to `strace` using multiple `-p` arguments (`strace -p <parent> -p <child1> -f ...`).
- Work seamlessly with `--tree` to trace nested sub-workers if multi-level trees exist.
- Display discovered worker PIDs in the attachment banner for user transparency.
- Maintain pure zero-overhead read-only operation with clean SIGINT detachment.

**Non-Goals:**
- Modifying or wrapping the target process execution or environment variables.
- Dynamic polling loop for detached independent processes not part of the target's process tree (e.g. separate daemon containers).

## Decisions

### 1. Child Discovery Strategy: `/proc/[pid]/status` Scanning
- **Choice**: Scan `/proc/[pid]/status` for `PPid: <target.PID>` matching the parent PID.
- **Rationale**:
  - Direct, fast, and supported across all standard Linux distributions, WSL2, and Docker containers without external tool dependencies.
  - Unlike `/proc/<pid>/task/<tid>/children` (which requires kernel `CONFIG_CHECKPOINT_RESTORE`), reading `/proc/*/status` is universally enabled in every Linux kernel.
- **Alternatives Considered**:
  - `pgrep -P <pid>`: Requires `procps` installed, which might be absent in minimal container environments.
  - Tracing parent `fork`/`clone` syscalls only: Misses pre-existing workers spawned before Specter attached.

### 2. Strace Multi-PID Invocation
- **Choice**: Append `-p <child_pid>` for each discovered child to the `strace` command:
  ```go
  args = []string{"-f", "-qq", "-xx", "-s", "8192", "-e", "trace=write", "-p", strconv.Itoa(target.PID)}
  for _, child := range target.Children {
      args = append(args, "-p", strconv.Itoa(child))
  }
  ```
- **Rationale**: Linux `strace` natively supports multiple `-p` arguments (up to 32 processes). It multiplexes their syscall streams and tags each write with `[pid N]`.
- **Alternatives Considered**: Spawning separate `strace` instances per PID: wasteful of system resources, complicates signal handling and stream merging. A single `strace` process handles detachment atomically on SIGINT.

### 3. Parser Compatibility
- **Choice**: Reuse existing `strace.Parser`.
- **Rationale**: `internal/source/strace/parse.go` already parses `[pid N]` prefixes (`pidPrefixRegex = regexp.MustCompile(`^\[pid\s+(\d+)\]\s+`)`). The parser and downstream `LineAssembler` and `StackTraceGrouper` are already fully thread- and PID-aware.

## Risks / Trade-offs

- **[Risk] Large Process Trees (e.g., >32 workers)**: `strace` has a default limit on concurrent `-p` flags on some older distros.
  - *Mitigation*: Cap discovered PIDs to the first 16 active worker processes, prioritizing immediate children.
- **[Risk] Short-lived Workers Exiting During Attachment**: A child worker might exit right as `strace` executes.
  - *Mitigation*: Ignore non-fatal `strace: attach: ... No such process` warnings during startup so remaining active processes continue streaming uninterrupted.
