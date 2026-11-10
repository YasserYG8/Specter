# e2e-testing Specification

## Purpose
TBD - created by archiving change e2e-integration-stress. Update Purpose after archive.
## Requirements
### Requirement: Live Process Port Attachment
The test suite SHALL execute the compiled Specter CLI binary against an active listening TCP server on Linux and verify real-time log capture.

#### Scenario: Attaching to live server by port
- **WHEN** Specter is invoked targeting the listening port of the test server
- **THEN** Specter attaches to the target process and captures stdout logs emitted upon HTTP requests

### Requirement: Multi-Line Error Stack Trace Grouping in E2E
The test suite SHALL verify that multi-line stack traces emitted by a live process on stderr are grouped into atomic entries.

#### Scenario: Capturing live multi-line stack trace
- **WHEN** the live target server emits an unhandled exception or multi-line trace to stderr
- **THEN** Specter captures and renders the full stack trace as an unbroken atomic unit

### Requirement: Live CLI Filtering Flags Verification
The test suite SHALL verify CLI filtering flags (`--level`, `--grep`, `--stderr`, `--json`, `--no-color`) against live process output streams.

#### Scenario: Filtering live output by severity level
- **WHEN** Specter is executed with `--level warn` against a live server emitting debug, info, and warn logs
- **THEN** only logs with severity warning or higher are emitted to standard output

#### Scenario: Filtering live output by text or regex pattern
- **WHEN** Specter is executed with `--grep` matching a specific endpoint pattern
- **THEN** non-matching requests are suppressed and matching requests are rendered

#### Scenario: Streaming stderr exclusively
- **WHEN** Specter is executed with `--stderr`
- **THEN** standard output logs are suppressed and only standard error entries are streamed

### Requirement: Multi-Process Tree Attachment
The test suite SHALL verify that Specter attaches to child worker processes when the `--tree` flag is enabled.

#### Scenario: Tracing child processes with tree flag
- **WHEN** the target server forks a child worker that emits logs and Specter is executed with `--tree`
- **THEN** logs from the child process are captured and displayed

### Requirement: Clean Process Detachment on Cancellation
The test suite SHALL verify that Specter detaches cleanly from target processes without leaving orphan `strace` or zombie processes.

#### Scenario: Detaching upon interrupt signal
- **WHEN** Specter receives a termination or interrupt signal during live tracing
- **THEN** the tracer detaches cleanly, exits with code 0, and leaves the target process running unharmed


### Requirement: Test Coverage on Other Platforms in CI
CI SHALL run the unit tests and the E2E tests on a native `arm64` Linux runner, because `ptrace` does not work under emulation. CI SHALL also run the E2E tests and the tests of the `strace` and resolver packages in Ubuntu, Debian, Fedora and Alpine containers, with the tools Specter needs installed through each distribution's package manager. A failure on any of them SHALL fail the build.

#### Scenario: arm64
- **WHEN** CI runs on a push or pull request
- **THEN** the unit and E2E tests run on an `arm64` Linux runner and fail the build if they fail

#### Scenario: Distributions
- **WHEN** CI runs on a push or pull request
- **THEN** the E2E tests, and the tests of the `strace` and resolver packages, run in Ubuntu, Debian, Fedora and Alpine containers, using static binaries built once, with `strace`, `lsof` and `iproute2` installed in each container

#### Scenario: Limits are documented
- **WHEN** the container jobs pass
- **THEN** they are named so that they test each distribution's tools and libraries, not its kernel, which is the runner's

### Requirement: Kill Tests
The E2E tests SHALL include tests that kill Specter with `SIGKILL`, and kill the `strace` it started, while it is attached to the test server, and check the outcomes required by the `strace-source` capability: no `strace` left within one second, no tracer on the server, the server still answering, and the exit status of Specter.

#### Scenario: Killing Specter
- **WHEN** the E2E suite kills Specter while it is attached
- **THEN** it checks that no `strace` remains within one second, the server has no tracer, and a request to the server succeeds

#### Scenario: Killing strace
- **WHEN** the E2E suite kills the `strace` process of an attached Specter
- **THEN** it checks that the server has no tracer, a request to the server succeeds, and Specter exited with status 1

### Requirement: Engine Parity Tests
The E2E suite SHALL run its capture tests once with `--source strace` and once with `--source ebpf`, with the same expected output. The eBPF run SHALL skip, not fail, with the reason in the test output, when the machine is not Linux, the kernel is older than 5.8, `/sys/kernel/btf/vmlinux` is missing, or the tests do not run as root. CI SHALL run the eBPF run as root on native `amd64` and `arm64` Linux runners, where it SHALL NOT skip, and a failure SHALL fail the build.

#### Scenario: Same output from both engines
- **WHEN** the E2E capture tests run on a CI runner as root
- **THEN** each test passes with `--source strace` and with `--source ebpf`

#### Scenario: Developer machine without root
- **WHEN** a developer runs the E2E suite as a normal user
- **THEN** the eBPF run is skipped with a message that it needs root, and the strace run is unaffected

#### Scenario: CI cannot skip
- **WHEN** CI runs the eBPF tests and they would skip because a requirement is missing
- **THEN** the CI job fails, so that eBPF coverage is never lost silently

### Requirement: eBPF-Only E2E Tests
The E2E suite SHALL include tests for behavior that only the `ebpf` engine has: capturing a target that already has a tracer, a single write of 100,000 bytes, a burst that overflows a deliberately small ring buffer and produces the lost-writes notice, and removal of the BPF programs after Specter is killed with `SIGKILL`.

#### Scenario: Already traced target
- **WHEN** the test server is traced by another process and Specter attaches with `--source ebpf`
- **THEN** Specter captures the server's lines

#### Scenario: Programs removed after SIGKILL
- **WHEN** Specter attached with `--source ebpf` is killed with `SIGKILL`
- **THEN** within one second none of its programs is listed as loaded, and the server still answers
