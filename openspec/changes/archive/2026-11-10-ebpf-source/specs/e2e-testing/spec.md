## ADDED Requirements

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
