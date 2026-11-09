## Context

The `strace` source stops the target at every system call, including the ones Specter does not print. v0.1.0 therefore says "development servers only". The README gives two overhead numbers from a one-off manual test, and no later change can show that it helps.

Facts from tests on 2026-11-08 (WSL2, Ubuntu, `strace` 6.8, running as root):

- `kill -9` on Specter leaves `strace` running as an orphan, still tracing the server. The orphan ends when the server next writes a line, because its output pipe is broken, so a busy server frees itself quickly and an idle one stays traced. While it is there, a new Specter on the same process stops with "already traced".
- `kill -9` on `strace` is safe. The kernel releases the server, which keeps answering, and Specter exits with status 1 and "strace exited unexpectedly".
- `strace` starts as a child of Specter with `exec.CommandContext`. Cancelling the context sends it `SIGINT`, which makes it detach cleanly.
- The CI job runs on `ubuntu-latest` (amd64) only, and sets `ptrace_scope` on the runner. The `linux/arm64` binary has never been run.

## Goals / Non-Goals

**Goals:**
- Measure the overhead in a way anyone can repeat, and publish the numbers.
- No `strace` outlives Specter, whatever kills Specter.
- Tell users about the slowdown when they attach, and let them limit an attach in time.
- Run the tests on `arm64` and on four Linux distributions.
- Decide with evidence whether an `eBPF` source is worth building.

**Non-Goals:**
- The full `eBPF` source, and the multi-hour soak test. Both depend on the spike's answer and become later changes.
- Making the `strace` source faster. Its cost comes from the kernel stopping the target, so a flag cannot remove it.
- Telemetry or any report that leaves the machine.
- Claiming Specter is safe for production. The README says it is not recommended yet until the later changes land.

## Decisions

### 1. The benchmark is a small Go load generator and a script

Tools like `wrk` or `ab` are not installed everywhere, so `test/bench/loadgen` is a Go program with no dependencies. It sends HTTP requests to the test server's `/json` endpoint, which writes one JSON log line to stdout per request, like an access log, and prints a JSON result: requests, achieved rate, median and 99th percentile latency.

It has two modes:
- **Open loop at a fixed rate** (for example 100 or 1,000 requests per second). Requests are sent on a schedule, and latency is measured from the scheduled time, so a slow server cannot hide its queue.
- **Closed loop** with a fixed number of connections, which finds the peak throughput.

`scripts/bench-overhead.sh` starts the test server with its stdout on a pipe (otherwise Specter would choose the file source and trace nothing), runs the load without Specter, attaches Specter with its output sent away, runs the same load again, detaches, and prints a table. Each cell is the median of 5 runs, with the spread beside it. It needs ptrace permission, so it runs as root or with `ptrace_scope` 0, and says so when it does not have it.

Alternative: a shell loop around `curl`. It cannot hold a fixed rate and its own cost would hide the difference.

### 2. What "under 10% slowdown at 1,000 requests per second" means

At a fixed 1,000 requests per second, slowdown is the relative increase of the **median request latency** with the reader attached, and the achieved rate must stay at 99% or more of the offered rate. The 99th percentile and the peak throughput are reported next to it but are not part of the test. Specter's own CPU use is reported too.

If the spread between baseline runs is itself above 10%, the machine is too noisy to judge. WSL2 was noisy in earlier tests, so the spike's final numbers come from a quieter Linux machine or a CI runner, and the result says which.

### 3. `strace` must not outlive Specter: parent-death signal

On Linux, set `SysProcAttr.Pdeathsig = SIGINT` on the `strace` command. If Specter dies for any reason, the kernel sends `SIGINT` to `strace`, which detaches from the server the same way as on a normal stop.

The signal is tied to the **thread** that created the child, not the process. Go can move work between threads, and the signal would fire if that thread ended. So the goroutine that starts `strace` calls `runtime.LockOSThread()` for its whole life. In `RunE` that is the main goroutine, which stays alive as long as Specter does.

`Pdeathsig` exists only on Linux. The code goes in `spawn_linux.go`, with a stub in `spawn_other.go` that does nothing, so the Windows and macOS builds in CI still compile. These are the repository's first build tags, added on purpose.

The test starts Specter on the test server, kills Specter with `SIGKILL`, and checks within one second that no `strace` is left and the server has no tracer and still answers. It repeats this 20 times, because thread problems show up as occasional failures. A second test kills `strace` and checks that the server keeps answering and Specter exits with status 1.

Alternatives:
- A watchdog helper process that waits on a pipe. More code, and it can die too.
- Relying on the broken pipe, as today. It does not end `strace` on an idle server.
- `Pdeathsig = SIGKILL`. Also safe, as the `kill -9` test shows, but `SIGINT` is the clean path and is what Specter sends on a normal stop.

### 4. A one-line notice, no off switch yet

When the source is `strace`, Specter prints one line to stderr after the `Source:` line: that it slows the target while attached and is meant for development and test servers. It uses the existing notice format, so `specter 3000 | grep 500` is not affected. The other sources do not slow the target and print nothing.

Alternatives: only the README (users who copy the one-line command do not read it), or a guess at "busy server" from live traffic (not reliable). An off switch can be added if users complain, and is not part of this change.

### 5. `--duration`

`--duration 30s` is a `time.Duration` flag. The timer starts after the source has started. When it fires, Specter stops exactly as on Ctrl+C, prints `Detached from PID N after 30s.`, and exits with status 0, because the user asked for that stop. A target that ends first is reported as today, with status 1. A value with no unit, such as `30`, gets a clear message with examples, as `--pid abc` does. A negative value is an error. Not setting the flag, or setting 0, means no limit.

It works with every source and with the picker, because the timer sits above the source in `RunE` and uses the same stop path.

### 6. CI: native `arm64`, and distributions through containers

- **arm64:** add `ubuntu-24.04-arm` as an operating system in the existing test matrix, so the same unit and E2E steps run on a real `arm64` kernel. Emulation does not work, because `ptrace` does not work under QEMU user mode.
- **Distributions:** build the `specter` and `testserver` binaries and the E2E, `strace` and resolver test binaries once on the host, statically (CGO off), then run them in `ubuntu`, `debian`, `fedora` and `alpine` containers with `--cap-add SYS_PTRACE`, after installing `strace`, `lsof` and `iproute2` with each distribution's package manager. This tests what differs between distributions: `strace` version, `libc` (Alpine uses musl) and the tools. It does not test their kernels, which are the runner's.

Alternative: install Go in each container and compile there. Slower, and it adds Go setup problems that have nothing to do with Specter.

### 7. The `eBPF` spike lives outside the shipped code

`experiments/ebpf-spike/` has its own `go.mod`, so the main module, `go test ./...` and releases do not depend on it or on its libraries. Steps, stopping as soon as the answer is clear:

1. Check that the machine can run `eBPF` tracing (kernel version, `BTF` at `/sys/kernel/btf/vmlinux`, tracefs, root). If WSL2 cannot, run the spike on a Linux VM or a CI runner.
2. Prove the idea with `bpftrace` on `sys_enter_write` for one process, filtering by process and file descriptor inside the kernel and printing the bytes. No code to write.
3. Measure it with the benchmark from decision 1, against the target in decision 2, and check that every line of a Node.js server is captured, counted against the server's own count.
4. Only if 2 and 3 look good, a small Go program with `cilium/ebpf` and a ring buffer.
5. Write `RESULT.md`: the numbers, the privileges and kernel versions needed, the known gaps, and a go or no-go.

The known gaps are named in advance so the spike records them and does not hide them: mapping a descriptor to where it leads (what `-y` gives today, needed for output through reopened terminal descriptors), writes above the per-event size (today `strace` captures 64 KB), and `eBPF` needing root or `CAP_BPF`, which is more than `strace` needs. The last one decides whether `eBPF` replaces `strace` or sits next to it as an option.

The change ends at the maintainer's decision. Whether to go on is a product decision and the agent does not take it.

## Risks / Trade-offs

- [The 10% target is below the noise of WSL2] → Report the spread, judge only on a quiet machine, and say which machine in `RESULT.md`.
- [The WSL2 kernel may not support `eBPF` tracing] → Step 1 of the spike checks first, and uses a VM or a runner if needed.
- [`Pdeathsig` fires on the wrong thread] → Lock the thread, and repeat the kill test 20 times.
- [A notice on every attach annoys developers] → One line on stderr. Add an off switch only when users ask.
- [The `arm64` runner is not available to the repository] → Check at the start of the CI task, and if it is missing, leave `arm64` as a documented gap, since no emulator can run `ptrace`.
- [Distribution containers share the runner's kernel] → The README and the CI job name say they test userland, not kernels.
- [`eBPF` is a lot of work for a small gain] → The spike is time-boxed, and a no-go still leaves the benchmark, the fix and the notice.

## Migration Plan

No user action. The notice and `--duration` are new output and a new flag. The `Pdeathsig` change only matters when Specter is killed. If the spike says go, a separate change adds the source.

## Open Questions

- Does the 10% target use the median latency (as decided here), or should it use peak throughput? The spike should report both, so the decision can be revisited with real numbers.
- Should the notice have an off switch from the start? Decided no, for now.
