## Why

Specter v0.1.0 tells users to try it on development servers only, because the `strace` source slows the target by about 60% to 90% (README: 2,100 to 800 and 14,000 to 1,500 requests per second on two test servers). Before Specter can be called safe on real servers (stage 3 of the plan), four things are unproven or broken:

- The overhead was measured once, by hand, on two servers. There is no repeatable measurement, so no change can be shown to help.
- A real test on 2026-11-08 found that `kill -9` on Specter leaves `strace` attached to the server as an orphan. It exits only when the server next writes a line (the broken output pipe ends it), so an idle server stays traced, and a later Specter reports "already traced". Killing `strace` itself is fine: the kernel releases the server and Specter exits with status 1.
- The `linux/arm64` binary was built and released but never run. Only Ubuntu in WSL2 was tested.
- Nobody knows if an `eBPF` reader could capture the same lines without stopping the server at every system call, which is the only way to remove the slowdown. That needs a cheap experiment before a large build.

## What Changes

- A repeatable overhead benchmark: a load generator and a script that measure the target's throughput and latency with and without Specter attached, at three loads, and print a table. The measured numbers replace the two old figures in the README.
- Fix: when Specter dies for any reason, including `SIGKILL`, the `strace` it started exits and releases the server within about a second. Tests kill Specter and kill `strace` while attached, and check that the server keeps answering and has no tracer.
- A one-line notice when the `strace` source attaches, saying that it slows the target and that Specter is meant for development and test servers.
- New flag `--duration <time>` (for example `--duration 30s`): Specter detaches by itself after that time, prints that it detached, and exits with status 0, so a short look at a problem is safer.
- CI runs the unit tests and the E2E tests on `arm64` and on Ubuntu, Debian, Fedora and Alpine, so an untested platform is a failing job, not a surprise.
- An `eBPF` spike: a time-boxed experiment in its own folder and Go module, outside the shipped code. It must show that an `eBPF` reader captures the lines of a Node.js server with under 10% slowdown at 1,000 requests per second, and it ends with a written go or no-go for the maintainer. If the answer is no-go, the notice and the benchmark still stand.
- Not in this change: the full `eBPF` source and the multi-hour soak test. They become later changes if the spike says go.

## Capabilities

### New Capabilities

- `overhead-benchmark`: the repeatable measurement of how much Specter slows a target, and where its results are published.
- `attach-duration`: the `--duration` flag that detaches Specter automatically.

### Modified Capabilities

- `strace-source`: a requirement that `strace` does not outlive Specter, and the notice printed when the source attaches.
- `capture-notices`: the slowdown notice shown when the `strace` source attaches.
- `e2e-testing`: unit and E2E tests also run on `arm64` and on four Linux distributions in CI, and a test covers killing Specter and `strace`.

## Impact

- `internal/source/strace/spawn.go`, plus a small Linux-only file and a stub for other systems, for the parent-death signal. The repository has no build tags yet, so this adds the first ones on purpose.
- `cmd/specter/main.go`: the `--duration` flag and the notice.
- New `test/bench/` (load generator) and a script that runs the benchmark. A new E2E test for the kill cases.
- `.github/workflows/ci.yaml`: an `arm64` job and a distribution matrix that runs tests in containers.
- New `experiments/ebpf-spike/` with its own `go.mod`, so the main module and `go test ./...` do not depend on it.
- `README.md` (measured overhead, `--duration`, "production use is not recommended yet") and `CHANGELOG.md`.
- No breaking change. The notice goes to stderr, so `specter 3000 | grep 500` stays clean.
