## ADDED Requirements

### Requirement: Kernel Capture of Writes
The `ebpf` source SHALL capture the bytes that the processes it follows write with `write` and `writev`, using eBPF programs on syscall tracepoints, without `ptrace` and without stopping the target. It SHALL read the bytes when the call returns, and only the number of bytes the call reports as written. It SHALL emit them as `source.Chunk` values with the thread ID, the real descriptor number and the capture time, as the `strace` source does, so that the rest of the pipeline cannot tell the engines apart. Its `Name()` SHALL be `ebpf`.

#### Scenario: One line on stdout
- **WHEN** the target writes `GET / 200\n` to fd 1 with one `write` call
- **THEN** the source emits a chunk with those bytes, FD 1 and the writing thread's ID

#### Scenario: writev
- **WHEN** the target calls `writev` on fd 2 with the buffers `ERROR ` and `db down\n`
- **THEN** the source emits `ERROR db down\n` for FD 2

#### Scenario: Partial write
- **WHEN** the target asks to write 100 bytes to fd 1 and the call returns 40
- **THEN** the source emits only the first 40 bytes

#### Scenario: Failed write
- **WHEN** a write to fd 1 returns an error
- **THEN** the source emits nothing for that call

#### Scenario: Target is not stopped
- **WHEN** the `ebpf` source is attached to a target
- **THEN** the target's `/proc/<pid>/status` shows `TracerPid: 0`

### Requirement: Followed Processes
The `ebpf` source SHALL follow the target, the child processes found at attach, and children those processes start later, with the same depth rules as the `strace` source: direct children always, and with `--tree` descendants up to 3 levels below the target. PIDs SHALL be compared in Specter's own PID namespace, so that the source works when Specter runs in WSL2 or in a container. A process that is not followed SHALL produce no chunks, even when it has the same name as the target.

#### Scenario: Worker started after attach
- **WHEN** the target forks a worker after the source has started, and the worker writes a line to its fd 1, which leads to the target's stdout
- **THEN** the source emits the line with the worker's PID

#### Scenario: Grandchild without --tree
- **WHEN** a child of the target starts its own child, without `--tree`, and that process writes a line
- **THEN** the source emits nothing for it

#### Scenario: Unrelated process with the same name
- **WHEN** another `node` process that is not a descendant of the target writes to its stdout
- **THEN** the source emits nothing for it

#### Scenario: WSL2
- **WHEN** Specter runs in WSL2, whose PIDs differ from the kernel's top-level PIDs, and attaches to PID 4120
- **THEN** the lines that PID 4120 writes are captured

### Requirement: Output Rules Match the Strace Source
The `ebpf` source SHALL decide which writes to emit with the same rules as the `strace` source's requirements "Output Only Writes That Reach the Target's Own Streams" and "Output Through Reopened Descriptors", comparing destinations by device and inode instead of by path. A write by the target to fd 1 or 2 SHALL always be emitted. A write by another followed process to fd 1 or 2 SHALL be emitted only when its destination equals the target's fd 1 or fd 2 destination, or when either destination is unknown. A write to any other descriptor SHALL be emitted, with its real descriptor number, only when its destination equals the target's fd 1 or fd 2 destination and that destination is not `/dev/null`. A write to another descriptor that leads to a regular file SHALL trigger the existing log file notice, once per file.

#### Scenario: Helper writes to a private pipe
- **WHEN** the target's stdout is pipe 100 and a child writes a line to its fd 1, which leads to pipe 200
- **THEN** the source emits nothing

#### Scenario: Node.js writes through a reopened terminal
- **WHEN** the target's fd 1 and fd 2 lead to `/dev/pts/1` and the target writes ` GET /en/graphs 200 in 628ms\n` to fd 22, which also leads to `/dev/pts/1`
- **THEN** the source emits the line with FD 22

#### Scenario: Other descriptor to /dev/null
- **WHEN** the target's fd 1 leads to `/dev/null` and the target writes to fd 7, which also leads to `/dev/null`
- **THEN** the source emits nothing

#### Scenario: Own log file
- **WHEN** the target writes to fd 8, which leads to the regular file `/var/log/app.log`
- **THEN** the source emits no chunk, and the "writes to its own log file" notice names `/var/log/app.log` once

#### Scenario: Target destinations unreadable
- **WHEN** reading `/proc/<pid>/fd/1` and `/proc/<pid>/fd/2` of the target fails
- **THEN** the source emits every write to fd 1 and 2 by followed processes

### Requirement: Long Writes
The `ebpf` source SHALL capture a single write of up to 64 KB whole. For a write above 64 KB it SHALL emit the first 64 KB followed by the same marker as the `strace` source, `[specter: N more bytes not captured]`, where N is the number of bytes left out.

#### Scenario: 50 KB line
- **WHEN** the target writes a 50 KB JSON line to fd 1 in one call
- **THEN** the whole line reaches the renderer

#### Scenario: 100 KB write
- **WHEN** the target writes 100,000 bytes to fd 1 in one call
- **THEN** the source emits the first 65,536 bytes followed by `[specter: 34464 more bytes not captured]`

### Requirement: Target Exit Status From eBPF
The `ebpf` source SHALL report the end of the target from the exit of the target's thread-group leader, as an exit code or a signal name, in the same form as the `strace` source. Exits of workers, threads and helpers SHALL NOT be reported as the target's end.

#### Scenario: Target exits with a code
- **WHEN** the target exits with code 3
- **THEN** the source reports that the target exited with code 3

#### Scenario: Target killed
- **WHEN** the target is killed by `SIGKILL`
- **THEN** the source reports that the target was killed by SIGKILL

#### Scenario: Worker exits
- **WHEN** a worker of the target exits with code 0 and the target keeps running
- **THEN** the source reports nothing and keeps capturing

### Requirement: Nothing Left Behind
The eBPF programs and maps that Specter loads SHALL be removed when Specter exits for any reason, including `SIGKILL`, and SHALL never change the target's memory, registers or execution. A target that already has a tracer, such as `gdb` or `strace`, SHALL still be captured by the `ebpf` source, without the "already traced" diagnostic.

#### Scenario: Specter is killed
- **WHEN** Specter is attached with the `ebpf` source and receives `SIGKILL`
- **THEN** within one second none of its BPF programs is loaded, and the target still answers requests

#### Scenario: Target already traced
- **WHEN** the target has `TracerPid: 900` because `gdb` is attached, and Specter selects the `ebpf` source
- **THEN** Specter captures the target's output and prints no "already traced" diagnostic

### Requirement: Forced eBPF Diagnostic
When `--source ebpf` is set and the `ebpf` source cannot run, the system SHALL exit with a non-zero status before attaching, with a diagnostic that names the reason and the remedy: missing privileges (run with `sudo`, or grant `CAP_BPF` and `CAP_PERFMON`), a kernel older than 5.8 (name the running version), missing kernel type information (`/sys/kernel/btf/vmlinux`), a system other than Linux, or the kernel refusing to load the program (include the kernel's message). It SHALL NOT fall back to `strace`.

#### Scenario: Forced without root
- **WHEN** a normal user without `CAP_BPF` runs `specter 3000 --source ebpf`
- **THEN** Specter exits with a non-zero status and says that eBPF needs root, and to run it with `sudo` or without `--source ebpf`

#### Scenario: Forced on an old kernel
- **WHEN** root runs `specter 3000 --source ebpf` on kernel 5.4
- **THEN** Specter exits with a non-zero status and says that eBPF needs Linux 5.8 or later and this kernel is 5.4
