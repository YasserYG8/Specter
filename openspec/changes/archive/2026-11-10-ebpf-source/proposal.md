## Why

While the `strace` source is attached, the target slows down badly. On the benchmark, at 1,000 requests per second the median latency went from 1.28 ms to 819 ms. Someone who points Specter at a real server gets a server that barely answers, and the first feedback Specter gets will be about that. The eBPF spike (`experiments/ebpf-spike/RESULT.md`) measured +6% at the same load, and the maintainer decided to build an eBPF source now, before multi-stream, because performance and quality come first.

## What Changes

- A new `ebpf` source captures the target's output inside the kernel, with no `ptrace` and without stopping the target on every system call. It produces the same chunks as the `strace` source, so the pipeline, filters and rendering do not change.
- It keeps what users rely on in the `strace` source: workers and `--tree`, `write` and `writev`, writes through reopened terminal descriptors (the Node.js case), dropping helpers' private writes, writes up to 64 KB with the same truncation marker, the target's exit code, and the log file notice.
- Automatic selection prefers `ebpf` over `strace` when it can run (Linux 5.8 or later, kernel type information, and root or `CAP_BPF` with `CAP_PERFMON`). Otherwise Specter uses `strace` as today. It never fails because eBPF is unavailable.
- `--source ebpf` forces it, and explains why when it cannot run. `--source strace` keeps the old engine.
- When Specter falls back to `strace`, the existing slowdown notice adds the reason in the same line, for example "run with sudo to use the fast eBPF engine". Still one line.
- If the server writes faster than Specter reads, Specter says how many writes were lost and never drops them silently.
- eBPF programs are removed by the kernel when Specter exits for any reason, including `SIGKILL`. The target is never stopped, and it shows no tracer, so a target that is already traced (by `gdb` or another `strace`) can still be watched.
- The benchmark measures each engine, CI runs the E2E suite against both engines as root on native Linux runners, and the README publishes the eBPF numbers and changes the "not for production" wording to apply to `strace` only, if eBPF passes the 10% target on native Linux.
- The compiled BPF program is committed and built into the binary, so `go build`, releases and users do not need `clang`.

## Capabilities

### New Capabilities
- `ebpf-source`: capturing the target's output with eBPF: hooks, process set, output rules, long writes, lost writes, exit status, cleanup, and the diagnostic for `--source ebpf`.

### Modified Capabilities
- `source-selector`: automatic selection puts `ebpf` before `strace`, `--source` accepts `ebpf`, and a failed eBPF start falls back to `strace` with a reason.
- `capture-notices`: the slowdown notice gains the fallback reason, and a new notice reports lost writes.
- `overhead-benchmark`: the benchmark measures each engine, and the published numbers and the production warning follow the engine.
- `e2e-testing`: the E2E suite runs against both engines, with eBPF tests as root on native CI runners.
- `release-automation`: the BPF program is compiled ahead of time and committed, and CI checks it is current.

## Impact

- New package `internal/source/ebpf`: a BPF program in C (`bpf/`), generated Go bindings and objects for `amd64` and `arm64`, a Linux loader, and a stub for other systems so macOS and Windows still compile.
- New dependency: `github.com/cilium/ebpf` (pure Go, no cgo). Developers who change the BPF program need `clang` and `llvm`, and users do not.
- `internal/source/selector`, `cmd/specter/main.go` (fallback reason and notices), `internal/source/types.go` (the exit status type moves here so both engines share it).
- `.github/workflows/ci.yaml`: a root job for eBPF tests on `ubuntu-24.04` and `ubuntu-24.04-arm`, and a check that the generated objects match the C source.
- `scripts/bench-overhead.sh`, `README.md`, `CHANGELOG.md`.
- The binary grows by roughly a few hundred KB.
- Multi-stream and the demo video wait until this change is done.
