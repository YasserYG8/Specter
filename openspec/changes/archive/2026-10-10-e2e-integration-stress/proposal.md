## Why

Specter relies on low-level Linux kernel interfaces (`ptrace`/`strace`, `/proc`, `ss`, `lsof`) and an asynchronous multi-stage log transformation pipeline. While unit tests validate individual components in isolation with static mocks, there is currently no automated end-to-end integration test verifying live process attachment against running services, nor a stress testing harness ensuring stability under high-throughput log bursts. Introducing automated E2E and stress test suites guarantees zero regressions against real Linux processes and validates memory and channel backpressure under real-world server workloads.

## What Changes

- Create a self-contained Go test server fixture (`test/e2e/testserver/main.go`) capable of listening on dynamic ports, emitting structured JSON logs, triggering multi-line error traces, handling burst log loads, and forking child workers.
- Implement an automated End-to-End integration test suite (`test/e2e/e2e_test.go`) executing the real compiled Specter binary against active processes on Linux/WSL.
- Validate live port attachment, multi-line stack trace grouping, filtering flags (`--level`, `--grep`, `--stderr`, `--json`, `--no-color`), multi-process tree attachment (`--tree`), and clean `SIGINT` detachment.
- Implement a high-throughput pipeline stress testing suite (`test/stress/pipeline_stress_test.go`) generating bursts of 10,000+ log lines/sec to assert zero channel buffer overflows, no deadlocks, and zero goroutine leaks.
- Ensure cross-platform portability: native Windows environments skip Linux-only syscall tests cleanly, while in-memory pipeline stress tests run everywhere.
- Update `.github/workflows/ci.yaml` to run the live E2E and stress suites in Ubuntu CI runners with required kernel permissions.

## Capabilities

### New Capabilities
- `e2e-testing`: End-to-end integration testing executing the compiled Specter CLI against real HTTP/TCP processes on Linux/WSL.
- `stress-testing`: High-throughput pipeline stress testing validating backpressure, buffer stability, and zero data races under heavy log bursts.

### Modified Capabilities
<!-- None: Existing CLI requirements remain unchanged -->

## Impact

- **New Test Packages**: `test/e2e` and `test/stress`.
- **Test Server Fixture**: `test/e2e/testserver`.
- **CI Configuration**: `.github/workflows/ci.yaml` updated to execute E2E and stress verification on Ubuntu runners.
- **Dependencies**: No external runtime dependencies introduced (pure Go fixtures and standard library testing).
