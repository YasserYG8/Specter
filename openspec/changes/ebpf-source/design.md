## Context

The `strace` source attaches with `ptrace`, which stops the target at every system call, not only at writes. The benchmark from `safe-on-real-servers` measured the cost on a laptop under WSL2: at 100 requests per second the median latency rose from 2.13 ms to 5.38 ms, and at 1,000 requests per second from 1.28 ms to 819 ms. `strace --seccomp-bpf`, which would stop only on traced calls, does not apply to processes attached with `-p`, so `strace` cannot be made fast for `specter 3000`.

The eBPF spike (`experiments/ebpf-spike/RESULT.md`) traced `write` on fd 1 and 2 with `bpftrace` on the same machine: +6% median latency at 1,000 requests per second, full rate kept, 60,000 of 60,000 lines captured. It named six gaps that a real reader must close: long lines (200 bytes in `bpftrace`), mapping a descriptor to its destination, `writev`, workers, PIDs in namespaces (WSL2's PIDs differ from the kernel's), and buffers read before the kernel loads them. The maintainer decided to build the source now, before multi-stream.

Specter's pipeline already hides the source behind `source.Source` and `source.Chunk{Bytes, FD, PID, TS}`. `cmd/specter/main.go` already falls back to `strace` when an auto-selected source fails at start.

## Goals / Non-Goals

**Goals:**
- Capture the same lines as the `strace` source, with under 10% median latency increase at 1,000 requests per second on native Linux.
- Keep the user's command unchanged: `specter 3000` picks eBPF when it can and `strace` when it cannot, and never fails because of eBPF.
- Close the six gaps from the spike, or name the ones left with a test that shows the limit.
- Keep releases and `go build` free of `clang` and cgo.

**Non-Goals:**
- Replacing `strace`. It stays for systems without eBPF and for users without root.
- Removing the need for root. Granting capabilities to the binary (`setcap`) is left for a later change.
- eBPF for Docker containers. The Docker source reads logs without slowing the target and keeps its priority.
- Multi-stream, request IDs and other features.
- Kernels older than 5.8.

## Decisions

### 1. `cilium/ebpf` with `bpf2go`, objects committed

The BPF program is written in C in `internal/source/ebpf/bpf/`. `bpf2go` compiles it for little-endian (`amd64`, `arm64`) and generates Go bindings that embed the object. The generated files are committed, so `go build` and GoReleaser keep working without `clang`. A CI job regenerates them and fails if they differ.

Alternatives: `libbpf` through cgo (needs cgo and a C toolchain in every build, and breaks the static, `CGO_ENABLED=0` releases); `bpftrace` as a subprocess (needs `bpftrace` installed, 200-byte strings, and is a second program to ship); BCC (needs kernel headers and a compiler on the user's machine). `cilium/ebpf` is pure Go and is used by Cilium, Tetragon and Pyroscope.

This is a library dependency, not an external tool. The invariant "external tools through `os/exec`" is about tools such as `strace` and `docker`, and is unchanged.

### 2. Syscall tracepoints, read at exit

The program attaches to `syscalls/sys_enter_write`, `sys_exit_write`, `sys_enter_writev` and `sys_exit_writev`. On enter, for a process in the set (decision 3), it stores the fd, buffer pointer and length per thread in a hash map. On exit, if the call returned more than 0 bytes, it reads that many bytes (or the `iovec` entries they cover) and sends them.

Reading at exit closes gap 6: the kernel has just read the same buffer, so its pages are in memory. It also gives the real number of bytes written, as `strace` does with `= N`.

Alternatives: `fentry`/`fexit` on `vfs_write` would hand over the `struct file` directly, but `fentry` on `arm64` needs kernel 6.0; `kprobe` depends on internal function names that change between versions. Syscall tracepoints are a stable interface on both architectures from 5.8.

### 3. Process set in the kernel, with PIDs in Specter's namespace

A hash map holds the processes to follow, with their depth below the target: the target at 0, the children discovered at attach at 1, and with `--tree` their descendants up to depth 3, as the `strace` source does. A handler on `sched/sched_process_fork` adds a new child when its parent is in the set and the depth limit allows, and `sched/sched_process_exit` removes it.

PIDs are compared in Specter's own PID namespace. Go passes the device and inode of `/proc/self/ns/pid`, the program uses `bpf_get_ns_current_pid_tgid` for the current task, and reads the child's PID in that namespace from `task->thread_pid->numbers[]` in the fork handler. This closes gap 5, because Specter running in WSL2 or in a container sees the same PIDs as its `/proc`.

Alternative: filter by process name as the spike did. That catches unrelated processes with the same name and misses workers that rename themselves, so it is not used.

### 4. Destinations compared by device and inode

The `strace` source decides by `-y` paths. Here Go stats the target's `/proc/<pid>/fd/1` and `/fd/2` (and `/dev/null`) and passes their device and inode numbers to the program. For each write, the program follows `current->files->fdt->fd[fd]` to the `struct file` and reads its inode number and device with CO-RE. It then applies the rules of the `strace` source:

- a write to fd 1 or 2 by the target itself: always sent;
- a write to fd 1 or 2 by another process in the set: sent when its destination equals the target's fd 1 or 2 destination, or when either side is unknown;
- a write to another fd: sent when its destination equals the target's fd 1 or 2 destination and that destination is not `/dev/null`, keeping its real fd number (the Node.js terminal case);
- a write to another fd that leads to a regular file: one small event per file without the bytes, so Go can print the existing log file notice.

The kernel keeps device numbers in a different encoding from `stat`. Go converts `stat`'s value to the kernel's (`MAJOR << 20 | MINOR`) before passing it, with a unit test on both encodings.

This closes gap 2 with the same rules as today, decided in the kernel so dropped writes cost almost nothing.

### 5. Long writes as chunks through one ring buffer

Events go through one `BPF_MAP_TYPE_RINGBUF` (default 8 MB). A write is sent as up to 4 chunks of 16 KB, so up to 64 KB, the same as `strace -s 65536`. The chunks of one call are reserved one after another in the same program run, so they arrive in order, and the line assembler joins them as it joins split writes today. Past 64 KB, the last chunk carries the number of bytes not captured, and Go appends the same marker as the `strace` source: `[specter: N more bytes not captured]`. `writev` walks up to 16 `iovec` entries within the same 64 KB.

When the ring buffer is full, the program counts the lost write in a per-CPU counter instead of blocking. Go reads the counter every second and prints a notice when it grows.

This closes gaps 1 and 3. A 16 KB event cannot live on the 512-byte BPF stack, which is why the spike's strings were short. Ring-buffer reservations do not use the stack.

### 6. Exit status from `sched_process_exit`

When the target's thread-group leader exits, the program sends its `exit_code`. Go turns it into a code or a signal name. `ExitStatus` moves from `internal/source/strace` to `internal/source`, so `main.go` reads it the same way for both engines.

### 7. Selection, fallback and messages

The selector tries eBPF where it chooses `strace` today, after Docker, journal and file. `ebpf.Available()` checks, without loading anything: Linux, kernel 5.8 or later, `/sys/kernel/btf/vmlinux` present, and effective `CAP_BPF` and `CAP_PERFMON`, or `CAP_SYS_ADMIN`. When a check fails, it returns the reason in plain words. The real proof is loading the program in `Start`: if that fails, the existing fallback in `main.go` starts `strace`.

Both paths end in one line. The slowdown notice is printed as today, with the reason at the end, for example `Run with sudo to use the fast eBPF engine.` or `eBPF needs Linux 5.8 or later (this kernel is 5.4).` The current "Auto-detected source unavailable" line is not printed for eBPF, so the user sees one line, not two.

`--source ebpf` turns off the fallback. When eBPF cannot run, Specter exits with a diagnostic that names the reason and the remedy.

Alternative: make eBPF opt-in for the first release. Not chosen, because the benefit is for users who do not read flags. The parity tests (decision 9) and `--source strace` are the safety net.

### 8. Cleanup and read-only

The program, its links and maps belong to file descriptors held by Specter. When Specter exits for any reason, including `SIGKILL`, the kernel closes them and removes the program, so nothing is left behind, with no extra mechanism. Tracing programs cannot change the target's memory or registers, and the target does not stop at all, so `TracerPid` stays 0. That means a target already traced by `gdb` or `strace` can still be watched, and the "already traced" diagnostic applies only to the `strace` engine.

### 9. Testing

- Unit tests (any OS): event decoding, chunk joining and the truncation marker, device number encoding, depth rules, reason messages.
- E2E: the existing suite runs once per engine. The eBPF run needs root, a 5.8+ kernel and BTF, and skips with a reason otherwise. CI runs it as root (`sudo -E go test`) on `ubuntu-24.04` and `ubuntu-24.04-arm`, which are virtual machines with their own kernel.
- New E2E tests for what only eBPF can do: an already-traced target, a write of 100 KB, and a burst that fills a small ring buffer, set by a test-only option, to check the lost-writes notice.
- The distribution container jobs keep testing `strace` only, because loading eBPF inside a container needs privileges those jobs do not grant.

### 10. Benchmark and the README

`scripts/bench-overhead.sh` gains `ENGINE=ebpf|strace`, passed as `--source`. The numbers in the README come from a native Linux machine (a CI runner, run by hand), with the machine and date. If eBPF passes the 10% target there, the production warning applies to the `strace` engine only.

## Risks / Trade-offs

- [Gaps 2 and 5 need kernel structure reads (CO-RE) that the spike did not try] → The first tasks are a proof on WSL2 and on a GitHub runner: 64 KB through the ring buffer, PIDs in a namespace, and a device and inode match. They stop and report to the maintainer if any fails.
- [The verifier rejects the program on some kernel versions] → Bounded loops only, CI on two architectures with recent kernels, and fallback to `strace` when loading fails, with the verifier's message in the debug output.
- [Lines are lost in a large burst] → 8 MB ring buffer, a counter, and a notice with the number. Never silent.
- [The first auto-selected eBPF release has a capture bug] → The E2E suite runs against both engines, and `--source strace` is documented as the escape hatch in the notice's README entry.
- [Root is required, which some users avoid] → Ubuntu's `ptrace_scope = 1` already requires `sudo` to attach `strace` to a running server, so most users already run `sudo specter`. Without root, Specter falls back and says how to get eBPF.
- [Kernel lockdown or seccomp profiles block BPF in some environments] → Loading fails, and Specter falls back to `strace` with the reason.
- [A second engine doubles the behavior to keep equal] → One shared E2E suite, and the destination rules stay word for word the same as the `strace-source` spec.
- [The binary grows] → Measured at the end and noted in the changelog.

## Migration Plan

No user action. Users who run Specter as root on Linux 5.8 or later get the eBPF engine on their next update, and the `Source:` line says `ebpf`. Rollback for a user: `--source strace`. Rollback for the project: make the selector skip eBPF, one line, and release again.

## Open Questions

- Should the notice suggest `sudo setcap cap_bpf,cap_perfmon+ep "$(which specter)"` for users who do not want to type `sudo`? It needs testing, and a decision on whether granting capabilities to the binary is acceptable.
- Is 8 MB the right ring-buffer size for busy servers? The burst test and the benchmark at peak load should give the number.
