# eBPF spike: result

**Status: steps 1 to 3 are done; step 4 (the Go program) was not started; the go or no-go is the maintainer's.**

The question: can a reader built on eBPF capture a server's output with under 10% slowdown at 1,000 requests per second, where the `strace` source cannot? (See `openspec/changes/safe-on-real-servers/design.md`, decision 7.)

**Short answer: yes, on this machine.** At 1,000 requests per second the median latency rose by 6% with the eBPF reader attached (1.37 ms to 1.45 ms), the full rate was kept, and all 60,000 lines were captured. The `strace` source, on the same machine and the same server, went from 1.28 ms to 819 ms. The reader here is a `bpftrace` program that covers only the simplest case, and the gaps below say what a real reader would still have to solve.

## 1. What this machine supports (checked 2026-11-09)

| Check | Result |
| :--- | :--- |
| Machine | AMD Ryzen 5 3500U, 4 cores / 8 threads, WSL2 on Windows 11 |
| Kernel | 6.6.87.2-microsoft-standard-WSL2 (x86_64) |
| Run as | root (all capabilities) |
| BTF | present: `/sys/kernel/btf/vmlinux` (6 MB) |
| tracefs | mounted at `/sys/kernel/tracing`; `events/syscalls/sys_enter_write` exists |
| Kernel config | `CONFIG_BPF`, `BPF_SYSCALL`, `BPF_JIT`, `BPF_EVENTS`, `DEBUG_INFO_BTF`, `FTRACE_SYSCALLS`, `KPROBES`, `UPROBES`, `TRACEPOINTS` all `=y` |
| `unprivileged_bpf_disabled` | 2 (only privileged users can load programs) |
| `bpftrace` | not installed at first; installed for the spike with `apt-get install bpftrace` (v0.20.2). Remove it with `apt-get remove bpftrace` |
| `clang`, `bpftool`, BCC | not installed |
| Node.js, Python 3 | installed |

**Verdict for step 1:** WSL2 on this machine can run eBPF tracing, so no VM or CI runner was needed. Writing the Go reader (step 4) would also need `clang`, because `cilium/ebpf` loads a compiled BPF object.

## 2. Proof with bpftrace

`capture.bt` attaches to the `sys_enter_write` tracepoint and keeps only calls on file descriptors 1 and 2 of processes with the server's name. The filter runs in the kernel, so the server's other system calls cost nothing. `prove.sh` runs it against two servers whose stdout is a pipe, with 200 requests each (one log line per request):

| Server | Requests | Lines captured | `write` calls seen | Bytes |
| :--- | :--- | :--- | :--- | :--- |
| Go test server (`test/e2e/testserver`) | 200 | **200** | 200 | 19,200 |
| Node.js HTTP server (`node-server.js`, `console.log` per request) | 200 | **200** | 200 | 15,892 |

Node.js writes its log lines with `write()` on fd 1 when stdout is a pipe, so the lines of a Node.js server were captured. The case Specter fixed in v0.1.0, a Node.js server in a terminal writing through a reopened descriptor, was **not** tested here (see the gaps).

## 3. Measurement

`READER=bpftrace scripts/bench-overhead.sh`: the same benchmark as for `strace` (5 runs of 10 seconds after 2 seconds of warm-up, with and without the reader attached, runs alternating, latency counted from each request's scheduled time). The full reports are in `test/bench/results/`.

| Load | Median latency without → with | Rate without → with | Lines captured | Baseline spread |
| :--- | :--- | :--- | :--- | :--- |
| 100 requests/s | 2.32 → 2.42 ms (**+4%**) | 100 → 100 | 6,000 of 6,000 | 7% |
| **1,000 requests/s** | **1.37 → 1.45 ms (+6%)** | **1,000 → 1,000 (100%)** | 60,000 of 60,000 | 2% |
| Closed loop (peak) | 1.56 → 1.84 ms (+18%) | 29,023 → 23,983 (-17%) | 1,440,552 of 1,440,552 | 5% |

Against the exit criterion (under 10% higher median latency at 1,000 requests per second, with 99% or more of the offered rate): **met**. The baseline spread was 2%, so the machine was quiet enough to judge that figure.

For comparison, the `strace` source on the same machine and server:

| Load | Median latency without → with | Rate without → with |
| :--- | :--- | :--- |
| 100 requests/s | 2.13 → 5.38 ms (+153%) | 100 → 100 |
| 1,000 requests/s | 1.28 → 819 ms | 1,000 → 885 |
| Closed loop (peak) | 3.94 → 243 ms | 10,403 → 225 (-98%; baseline spread 134%, so read it as the size of the effect) |

The `bpftrace` process itself used 13% of one core at 1,000 requests per second. The eBPF program's own cost runs in the server's context, so it shows up as latency, not in that figure.

**Limits of these numbers:** one laptop under WSL2, one workload (a server that writes one short line per request), and one run of the experiment. The target should be confirmed on a native Linux kernel before building on it.

## 4. Go program with a ring buffer

**Not started.** Steps 2 and 3 look good, so the plan allowed it, but it needs two things that are not on this machine yet: `clang` (to compile the BPF program) and the `cilium/ebpf` Go library. Both are installs, so they wait for the maintainer's approval. The question this step would answer is the 200-byte limit below: whether a reader with a ring buffer can carry whole lines up to 64 KB, as `strace` does today.

## 5. Gaps, privileges and recommendation

**Gaps found, in the order they would matter:**

1. **Long lines.** `bpftrace` strings are limited to 200 bytes (`BPFTRACE_MAX_STRLEN` cannot go higher, because strings live on the 512-byte BPF stack). A longer write is cut. `strace` captures up to 64 KB per write today. A reader with a ring buffer can carry larger events, but that is not proven yet (step 4).
2. **Where a descriptor leads.** The program keeps writes on fd 1 and 2 only. `strace -y` tells Specter where any descriptor leads, which it needs for output through reopened terminal descriptors (the Node.js dev server in a terminal). An eBPF reader would have to read that from `/proc/<pid>/fd` or from the kernel's file structures.
3. **`writev`.** Only `write()` is traced. `writev` (used by some runtimes) needs a second tracepoint and reading an array of buffers, which is harder in eBPF.
4. **Worker processes.** The program filters by process name, so workers with the same name are caught, but a real reader must follow `fork` and `clone` to cover Node cluster, Gunicorn and Celery workers.
5. **Which process.** Inside WSL2, the PID that eBPF sees is the host's, not the one the distribution shows (`pid == N` never matched), so the spike filters by process name. On native Linux `pid` works. In containers, a reader needs the namespace-aware helper.
6. **Lost lines.** A `sys_enter_write` program reads the user buffer before the kernel does. If the page is not in memory the read fails and the line is lost. No line was lost in 1.4 million, but the risk is real under memory pressure.

**Privileges.** Loading eBPF programs needs root, or `CAP_BPF` and `CAP_PERFMON` (kernel 5.8 and later). Here `unprivileged_bpf_disabled` is 2. `strace` needs only ptrace rights over the target (the same user, or `ptrace_scope` 0, or root). So eBPF asks for more than `strace`, and it would sit next to `strace` as an option, not replace it.

**Kernel needs.** `CONFIG_BPF_SYSCALL` and tracepoints for `bpftrace`. A Go reader that is compiled once and runs anywhere (CO-RE) would also need BTF (`/sys/kernel/btf/vmlinux`). Both exist on this WSL2 kernel. They are on by default in recent Ubuntu, Debian and Fedora kernels too, but that was not checked here.

**Recommendation: go, with conditions.** The measured gap is large enough to justify building an optional eBPF source: +6% against a 819 ms latency, at the load where `strace` falls over. The conditions:

- Keep `strace` as the default and as the fallback, and select eBPF only when the process has the privileges and the kernel supports it.
- Make the first eBPF change small: `write` on fd 1 and 2, whole lines up to the ring-buffer event size, one target process, then workers. Do not promise the Node.js terminal case until gap 2 is solved.
- Confirm the numbers on a native Linux machine (not WSL2) before the change is approved, and run step 4 first to see whether long lines are solvable.

This is a recommendation. The decision to start the full eBPF source belongs to the maintainer, and this change does not start it.

## 6. Proofs for the full reader (ebpf-source change, tasks 1.1 to 1.5)

After the maintainer decided to build the eBPF source, the three gaps that `bpftrace` could not close were tried with a small Go program (`cilium/ebpf`, in `experiments/ebpf-spike/ringbuf/`, its own module). It loads a BPF program written in C and compiled with clang 18, and the compiled object is committed. The proofs are Go tests that start a small writer process which does exactly one `write(2)` on command. They ran as root in WSL2, and then on GitHub's native `ubuntu-24.04` (amd64) and `ubuntu-24.04-arm` (arm64) virtual machines, where a skipped test fails the job. **All of them passed in both places.**

| Proof | What the test does | Result |
| :--- | :--- | :--- |
| Long writes (gap 1) | One `write` of 90, 16,384, 16,385, 50,000, 65,536 and 100,000 bytes to a pipe | The bytes arrive whole and in order, in chunks of 16 KB through the ring buffer. The 100,000 byte write gives the first 65,536 bytes and reports **34,464 bytes left out**. No write was lost and none was unreadable |
| PIDs in our namespace (gap 5) | Follow one process by the PID Specter sees; a second process of the same program is not followed | Only the chosen process is captured. The PIDs in the events equal the PIDs in this namespace, which in WSL2 differ from the kernel's top-level PIDs. The real Specter test server was captured too |
| Forked workers (gap 4) | A process starts a child, which starts a child, which starts a child, with a depth limit of 1, 2 and 3 | Exactly the generations inside the limit are captured, each with its own PID. Threads are not mistaken for processes |
| Where a descriptor leads (gap 2) | Read the device and inode of the file behind a descriptor in the kernel, and compare with `stat` of `/proc/<pid>/fd/N` | They match for a pipe and for a terminal (`/dev/pts/N`), for a private pipe and for `/dev/null`. The kernel's device number needs the conversion `MAJOR<<20 \| MINOR` from `stat`'s |

What this changes in the gaps above:

- **Gap 1 (long lines) is closed:** a ring buffer reservation does not use the 512-byte BPF stack, and the program reads each write when the call returns, so up to 64 KB arrive whole.
- **Gap 5 (PIDs) is closed,** with `bpf_get_ns_current_pid_tgid` and the namespace's inode, and the child's PID found by searching its `struct pid` for the entry that belongs to Specter's namespace.
- **Gap 4 (workers) is closed** with a `sched_process_fork` program.
- **Gap 2 (descriptor destinations) is closed** by comparing device and inode, for the cases above. A descriptor that is closed before the write returns is read as unknown, and the rules for unknown destinations are in the design.
- Gaps 3 (`writev`) and 6 (reading the buffer at exit instead of at entry) are not part of these proofs. Reading at exit is how the program works, and `writev` is task 3.1.

The runners' kernel versions are in the job log of the `ebpf-spike-proof` branch.

## 7. Linux 7.0: the per-syscall tracepoints miss running processes (found 2026-11-10)

The first native benchmark of the real engine, on a GitHub `ubuntu-24.04` runner with kernel 7.0.0-1017-azure, captured only 24% of the lines. The program was attached to `syscalls:sys_enter_write` and `sys_exit_write`, as the spike's was.

A diagnostic test (`diag.c` on the `ebpf-spike-proof` branch, commit 26c1d05) attached five hooks at once and counted the writes of a process started before them and one started after:

| Hook | Process already running (10, then 20 expected) | New process (10 expected) |
| :--- | :--- | :--- |
| `tracepoint/syscalls/sys_enter_write` | 6, then stuck at 6 (arm64) or 10 (amd64) | 10 |
| `tp_btf/sys_enter` | 10, 20 | 10 |
| `kprobe/vfs_write` | 10, 20 | 10 |
| `kprobe/__x64_sys_write` (arm64: `__arm64_sys_write`) | 10, 20 | 10 |
| `tracepoint/raw_syscalls/sys_enter` | 10, 20 | 10 |

Only the per-syscall tracepoint lost writes, and only for processes that were already running, which is every process Specter attaches to. The spike's proofs did not catch it because their writer was started after the program was attached. On the WSL2 kernel (6.6) all hooks saw every write.

The engine now uses `raw_syscalls:sys_enter` and `sys_exit` (commit aec6fd9). They fire for every system call, so the program compares the call's number with the numbers of `write` and `writev` first, which Go sets for the architecture (amd64: 1 and 20, arm64: 64 and 66). With it, the same runner captured every line at 100 and 1,000 requests per second, and the median latency rose by 4% to 5% (`test/bench/results/2026-11-10-both-engines-github-runner.md`).
