# diagnostic-reporter Specification

## Purpose
TBD - created by archiving change diagnostics-port-discovery. Update Purpose after archive.
## Requirements
### Requirement: Active Port Diagnostic Display
The CLI reporter SHALL display a formatted diagnostic summary when the user specifies a port that has no active listening process.

#### Scenario: Display active ports summary
- **WHEN** port resolution fails because the target port is not listening and other active ports exist
- **THEN** the CLI outputs `[specter] No process is listening on port <port>` followed by an indented list of active listening ports showing port number, PID, and process name

#### Scenario: Display intelligent port suggestion
- **WHEN** a high-confidence suggestion is determined by the resolver
- **THEN** the CLI prints `Did you mean: specter <suggested_port> ?` with terminal styling

#### Scenario: No active ports on the system
- **WHEN** no TCP ports are listening on the system
- **THEN** the CLI reports that no active TCP listeners were found on the machine

#### Scenario: Diagnostic output respects color settings
- **WHEN** `--no-color` is set or stdout/stderr is piped to a non-TTY
- **THEN** the diagnostic report and suggestions are rendered without ANSI color codes

### Requirement: Unsupported Platform Diagnostic
When Specter would attach to a target on a system other than Linux, the system SHALL stop with a diagnostic that names the system, says that attaching needs Linux, and gives advice for that system: on Windows, to run Specter inside WSL2 with the same target; on macOS, to run the server and Specter in a Linux VM or a Docker container. Commands that do not attach, such as `specter version`, `specter completion` and `--help`, SHALL keep working on every system.

#### Scenario: Windows
- **WHEN** `specter 3000` runs on Windows
- **THEN** Specter exits with a non-zero status and a diagnostic that says it needs Linux and suggests `wsl specter 3000`

#### Scenario: macOS
- **WHEN** `specter 3000` runs on macOS
- **THEN** Specter exits with a non-zero status and a diagnostic that says it needs Linux and suggests running the server and Specter in a Linux VM or a Docker container

#### Scenario: Commands that do not attach
- **WHEN** `specter version` or `specter completion bash` runs on macOS or Windows
- **THEN** it works as on Linux, without the diagnostic

