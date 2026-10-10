## Context

Specter intercepts standard I/O streams of arbitrary Linux processes using `strace` and transforms them through an asynchronous pipeline:
`Source (strace) -> LineAssembler -> StackTraceGrouper -> Filter -> Renderer`.
While unit tests thoroughly test each pipeline stage in isolation with synthetic chunks, integration between real kernel ptrace mechanics, file descriptor writes, dynamic process lifecycles, and high-frequency stream bursts requires automated end-to-end and stress validation.

## Goals / Non-Goals

**Goals:**
- Provide a self-contained Go HTTP server fixture (`test/e2e/testserver`) supporting configurable endpoints: JSON logs, multi-line error stack traces, rapid log bursts, and worker process forking.
- Implement live End-to-End tests (`test/e2e/e2e_test.go`) executing the compiled `specter` binary against real listening processes on Linux/WSL.
- Validate live attachment, port resolution, stack trace grouping, CLI flag filtering (`--level`, `--grep`, `--stderr`, `--json`, `--no-color`), multi-process tree tracing (`--tree`), and clean `SIGINT` detachment.
- Implement an in-memory and OS-level high-throughput pipeline stress suite (`test/stress/pipeline_stress_test.go`) generating bursts of 10,000+ lines to assert zero dropped chunks, no memory leaks, and thread-safety under `go test -race`.
- Integrate seamlessly into GitHub Actions CI (`ubuntu-latest`).

**Non-Goals:**
- Testing non-Linux operating systems with live `ptrace` (Specter is Linux-first; non-Linux hosts skip live ptrace tests cleanly).
- Long-running soak testing (stress tests run bounded bursts within automated test timeouts).

## Decisions

### 1. Pure Go Test Server Fixture
- **Decision**: Implement `test/e2e/testserver/main.go` in pure Go standard library rather than relying on external Python or Node runtimes.
- **Rationale**: Eliminates external runtime version discrepancies and dependency issues in minimal Linux environments and CI containers.
- **Alternatives Considered**: Using Python `test_server.py` exclusively (less portable in stripped-down environments).

### 2. Test Package Structure
- **Decision**: Separate tests into `test/e2e` for OS-level process attachment and `test/stress` for high-throughput pipeline stress.
- **Rationale**: Keeps fast in-memory pipeline benchmarks runnable anywhere while isolating kernel-dependent ptrace tests.
- **Cross-Platform Handling**: `test/e2e` checks `runtime.GOOS != "linux"` or missing `strace` and calls `t.Skip()` when running on native Windows or environments without ptrace privileges.

### 3. Graceful Process Teardown & Isolation
- **Decision**: E2E test harness manages server and tracer lifecycles using `context.WithTimeout`, explicit process group signaling, and deferred cleanup to guarantee no orphan processes remain.
- **Rationale**: Prevents hanging test runners or locked ports between test runs.

### 4. Stress Threshold
- **Decision**: Stress harness tests bursts of 10,000 lines across single and multi-threaded streams, asserting atomic line reconstruction and zero goroutine leaks.
- **Rationale**: 10,000 lines exceeds typical developer traffic by orders of magnitude while executing in under 2 seconds.

## Risks / Trade-offs

- **[Risk] Ptrace Restrictions in CI / Containers** &rarr; **Mitigation**: GitHub Actions runners provide Ubuntu VMs with ptrace enabled. In constrained container environments, `CheckPtracePermissions()` or `t.Skip` gracefully handles permission denials (`EPERM`).
- **[Risk] Port Clashes During Concurrent Tests** &rarr; **Mitigation**: Test server binds to port `:0` (dynamic OS ephemeral port allocation) or uses isolated ports per test.
