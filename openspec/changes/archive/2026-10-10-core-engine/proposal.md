## Why

Developers waste time searching for background terminals, hunting down log files, or restarting server processes with wrappers just to observe their stdout and stderr output. Specter solves this by providing a zero-configuration CLI that immediately attaches to any active process by port or PID and streams its output live to the terminal. Implementing the Core Engine is the necessary first step to establish the end-to-end capture, reassembly, and streaming pipeline.

## What Changes

- Implement the port-to-PID resolver using Linux network utilities (`lsof` and `ss`) and `/proc/<pid>` inspection.
- Implement `StraceSource` to attach non-invasively to target processes using `strace -xx`, decode raw hex `write()` syscalls on fd 1 and 2, and handle multi-thread `<unfinished>` / `<resumed>` calls.
- Implement `LineAssembler` to buffer bytes per `(pid, fd)`, split lines on `\n`, stream-decode UTF-8, and flush partial lines after a 150ms idle threshold.
- Wire the pipeline into the CLI entry point (`cmd/specter/main.go`) with clean teardown on `Ctrl+C` (SIGINT/SIGTERM) to guarantee target processes are never left traced or suspended.

## Capabilities

### New Capabilities
- `port-resolver`: Discover listening process PID from a TCP port and inspect `/proc/<pid>` descriptors and metadata.
- `strace-source`: Attach non-invasively to target PIDs via `strace`, parse hex-encoded `write` syscalls, manage concurrent thread state, and cleanly detach.
- `line-assembler`: Buffer raw bytes per `(pid, fd)`, assemble complete lines with streaming UTF-8 decoding, and flush interactive prompt lines on idle timeout.

### Modified Capabilities
<!-- None: this is the initial engine implementation -->

## Impact

- **New Packages**:
  - `internal/resolver`
  - `internal/source/strace`
  - `internal/pipeline`
- **CLI**: `cmd/specter/main.go` will be updated to execute the full resolve-attach-stream workflow.
- **Dependencies**: Uses `os/exec`, `bufio`, `syscall`, and existing CLI dependencies (`cobra`, `color`).
