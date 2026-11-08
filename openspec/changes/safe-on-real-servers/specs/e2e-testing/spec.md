## ADDED Requirements

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
