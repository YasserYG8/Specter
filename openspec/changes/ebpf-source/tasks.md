## 1. Proof before building (stop point)

- [x] 1.1 Ask the maintainer before installing anything in WSL. Then install `clang`, `llvm` and `libbpf-dev` (for `bpf_helpers.h`), and add `github.com/cilium/ebpf` to `go.mod`.
- [x] 1.2 In `experiments/ebpf-spike/ringbuf/` (its own `go.mod`), write a small Go program with `bpf2go` that attaches to `sys_enter_write` and `sys_exit_write`, stores the call per thread on enter, and on exit sends the written bytes through a ring buffer in up to 4 chunks of 16 KB. Prove that a 50 KB line arrives whole and in order, and that a 100 KB write reports 34,464 bytes left out.
- [x] 1.3 In the same program, filter by PID in Specter's namespace with `bpf_get_ns_current_pid_tgid`, and add a child from `sched_process_fork` with its PID read from `task->thread_pid->numbers[]`. Prove it in WSL2 against the test server and a forked worker.
- [x] 1.4 In the same program, read the device and inode of `current->files->fdt->fd[fd]` with CO-RE, and compare them with Go's `stat` of `/proc/<pid>/fd/1` after converting the device number to the kernel's encoding. Prove a match for a pipe and for `/dev/pts/N`, and no match for `/dev/null` and a private pipe.
- [x] 1.5 Run 1.2 to 1.4 on a GitHub `ubuntu-24.04` runner as root as well (a manual workflow run on a branch, after asking the maintainer to approve the push). Write the results at the end of `experiments/ebpf-spike/RESULT.md`. If any proof fails, stop and ask the maintainer before going on.

## 2. Package and build

- [x] 2.1 Create `internal/source/ebpf` with the BPF C program in `bpf/` (minimal kernel type definitions marked for CO-RE, no full `vmlinux.h`), a `go:generate` line for `bpf2go` with `-target bpfel`, and the generated files committed. A Linux loader file and a stub for other systems that reports "eBPF needs Linux". Check `go build` without `clang` and the cross-builds for macOS and Windows.
- [x] 2.2 Move `ExitStatus` from `internal/source/strace` to `internal/source`, and update the strace source and `main.go`. No behavior change, and all existing tests pass.

## 3. Capture

- [x] 3.1 BPF program: `write` and `writev` enter and exit handlers, the per-thread call map, reading only the bytes returned, up to 4 chunks of 16 KB per call and up to 16 `iovec` entries, and the "bytes left out" count past 64 KB. A per-CPU counter for writes dropped when the ring buffer is full.
- [x] 3.2 BPF program: the followed-process map with depths, seeded from Go with the target and the children found at attach, the fork handler with the `--tree` depth rule, and the exit handler that removes processes and sends the target leader's exit code.
- [x] 3.3 BPF program: the destination rules from the `ebpf-source` spec, with the target's fd 1, fd 2 and `/dev/null` device and inode passed from Go, and one event per regular file for the log file notice.
- [x] 3.4 Go source: `Name()`, `Start`, `Stop`, reading the ring buffer into `source.Chunk` values (thread ID, real fd, time), appending the truncation marker, reading the lost-writes counter every second, reporting `ExitStatus`, and closing everything on context cancel.
- [x] 3.5 Unit tests that run on any OS: event decoding, chunk order and the marker, device number conversion, depth rules, exit code and signal decoding.

## 4. Selection, fallback and messages

- [x] 4.1 `ebpf.Available()`: Linux, kernel 5.8 or later, `/sys/kernel/btf/vmlinux`, and `CAP_BPF` with `CAP_PERFMON` or `CAP_SYS_ADMIN`, returning a plain reason when a check fails. Table tests with an injectable kernel version, file check and capability set.
- [x] 4.2 Selector: try eBPF before strace, keep the reason when it is not used, add `ebpf` to `--source` and to `SupportedSources`, update the flag help and shell completions.
- [x] 4.3 `main.go`: when an automatically selected eBPF source fails at start, fall back to strace with the kernel's message as the reason, and print one slowdown notice with the reason instead of the "Auto-detected source unavailable" line. No reason or tip when `--source strace` was given. `--source ebpf` turns the fallback off and shows the forced eBPF diagnostic.
- [x] 4.4 Lost-writes notice: at most once per second, with the number, on stderr, never when nothing was lost. Unit tests.

## 5. Tests and CI

- [x] 5.1 Run the E2E capture tests once per engine. The eBPF run skips with a reason without root, kernel 5.8 or BTF, unless `SPECTER_REQUIRE_EBPF=1` is set, which makes it fail instead.
- [x] 5.2 eBPF-only E2E tests: an already traced target, a 100,000-byte write with the marker, a burst against a small ring buffer (test-only option) that produces the lost-writes notice, and no loaded programs within one second after `SIGKILL`.
- [x] 5.3 CI: a job that runs the eBPF E2E run as root with `SPECTER_REQUIRE_EBPF=1` on `ubuntu-24.04` and `ubuntu-24.04-arm`, and a job that regenerates the BPF files with a pinned `clang` and fails on any difference. The distribution container jobs stay strace-only.
- [x] 5.4 Check the workflow file locally, then stop and ask the maintainer to approve pushing, read the CI result, and fix what fails. Do not push without their go-ahead.

## 6. Benchmark, docs and final checks

- [x] 6.1 `scripts/bench-overhead.sh`: add `ENGINE=strace|ebpf`, passed as `--source`, and name the engine in the report.
- [x] 6.2 Run the benchmark for both engines on a native Linux machine (a GitHub runner run by hand, or a machine the maintainer names), and once in WSL2 for comparison. Keep the reports in `test/bench/results/`.
- [x] 6.3 README: the engines and how Specter picks one, the `sudo` tip, `--source ebpf` and `--source strace`, the lost-writes notice, and the performance section per engine with the machine and date. Change the production wording only as the `overhead-benchmark` spec allows. `CHANGELOG.md` entry, including the binary size change.
- [x] 6.4 `gofmt -l .` (with LF endings), `go vet ./...`, `go test ./cmd/... ./internal/... ./test/stress/...` with and without `-race`, the cross-builds, and the E2E suite in WSL for both engines as root. Report the results.
