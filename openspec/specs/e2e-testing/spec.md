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

