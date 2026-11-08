## 1. Strace must not outlive Specter

- [x] 1.1 Add an E2E test that attaches Specter to the test server, kills Specter with `SIGKILL`, and checks within one second that no `strace` is left, the server has no tracer, and a request to the server succeeds. Run it before the fix and confirm that it fails (the orphan `strace` stays while the server is idle).
- [x] 1.2 Add `spawn_linux.go` that sets `SysProcAttr.Pdeathsig = SIGINT` on the `strace` command, and `spawn_other.go` with a stub, so macOS and Windows still compile. Call `runtime.LockOSThread()` in `RunE` before the source starts, as the design explains.
- [x] 1.3 Repeat the Specter kill test 20 times in one test, and confirm it passes every time. Add the test that kills `strace` (server unaffected, Specter exits with status 1 and a message that `strace` ended unexpectedly), and the test that a second Specter attaches after the first was killed.
- [x] 1.4 Check the cross-builds for macOS and Windows, and `go vet`.

## 2. Slowdown notice and `--duration`

- [x] 2.1 Print the slowdown notice after the `Source:` line when the source is `strace`, and for no other source. Unit tests for which sources print it, and an E2E check that it goes to stderr, once.
- [x] 2.2 Add `--duration`: parse it as a `time.Duration`, reject negative values and values without a unit with the examples from the spec, start the timer after the source starts, stop through the same path as Ctrl+C, and print `Detached from PID N after 30s.` (container form for Docker) with exit status 0. A target that ends first is still reported with status 1.
- [x] 2.3 Tests: validation through the command tree (missing unit, negative, zero, valid), `reportEnd` for the duration case, and an E2E test with `--duration 3s` that checks the message, the exit status 0, and that the server has no tracer afterwards.

## 3. Overhead benchmark

- [x] 3.1 Write `test/bench/loadgen`: open loop at a fixed rate with latency measured from the scheduled time, closed loop with a number of connections, JSON result with requests, achieved rate, median and 99th percentile. Unit tests for the percentile and scheduling code, runnable on Windows.
- [x] 3.2 Write `scripts/bench-overhead.sh`: start the test server with stdout on a pipe, run each load without Specter and with Specter attached, 5 runs each, print the comparison table with medians and spread, report Specter's CPU use, stop early with a clear message when ptrace is not permitted, and say when the spread is above 10%.
- [ ] 3.3 Run the benchmark for the `strace` source in WSL as root, and keep the output. If the spread is above 10%, say so next to the numbers.
- [ ] 3.4 README performance section: replace the two old numbers with the measured ones (machine and date), and add that production use is not recommended yet. `CHANGELOG.md` entry.

## 4. CI on `arm64` and four distributions

- [ ] 4.1 Check that an `arm64` runner is available for the repository. If it is not, record `arm64` as a documented gap, since no emulator can run `ptrace`, and skip 4.2.
- [ ] 4.2 Add `ubuntu-24.04-arm` to the test matrix in `ci.yaml`, so the unit and E2E steps run there too.
- [ ] 4.3 Add the distribution job: build static `specter`, `testserver` and the E2E, `strace` and resolver test binaries once, then run them in `ubuntu`, `debian`, `fedora` and `alpine` containers with `--cap-add SYS_PTRACE`, after installing `strace`, `lsof` and `iproute2` with each package manager. Name the job so it says it tests userland and not kernels.
- [ ] 4.4 Check the workflow file for syntax errors locally. The jobs themselves can only run on GitHub, so stop here and ask the maintainer to approve pushing a branch or `main`, then read the CI result and fix what fails. Do not push without their go-ahead.

## 5. `eBPF` spike

- [ ] 5.1 Check what the machine supports: kernel version, `BTF` at `/sys/kernel/btf/vmlinux`, tracefs, `bpftrace`, and root. Write the answer at the top of `experiments/ebpf-spike/RESULT.md`. If WSL2 cannot do it, choose a Linux VM or a runner and say which.
- [ ] 5.2 Prove the idea with `bpftrace`: trace `write` on file descriptors 1 and 2 of the test server, filtering by process inside the kernel, and print the bytes. Confirm that the lines of the test server and of a Node.js server appear.
- [ ] 5.3 Measure with the benchmark at a fixed 1,000 requests per second: baseline, `strace`, and `bpftrace`. Report the median latency increase, the achieved rate, the spread, and how many lines were captured against how many the server wrote. Use the quietest machine available.
- [ ] 5.4 Only if 5.2 and 5.3 look good: a small Go program with `cilium/ebpf` and a ring buffer in `experiments/ebpf-spike/` with its own `go.mod`, measured the same way. Skip otherwise.
- [ ] 5.5 Finish `RESULT.md`: the numbers, the privileges and kernel versions needed, the known gaps (descriptor to destination mapping, writes above the event size, root or `CAP_BPF`), and a recommendation of go or no-go.
- [ ] 5.6 Stop and ask the maintainer for the go or no-go decision. Do not start the full `eBPF` source in this change.

## 6. Docs and checks

- [ ] 6.1 README: document `--duration` and the notice. `CHANGELOG.md` under Fixed (no `strace` left after Specter is killed), and under Added (`--duration`, the notice, the benchmark).
- [ ] 6.2 `gofmt -l .`, `go vet ./...`, `go test ./cmd/... ./internal/... ./test/stress/...` pass, also with `-race` in WSL, and the macOS and Windows cross-builds still compile. Run the E2E suite in WSL with `ptrace_scope` 0 after asking the user, then set it back to 1.
