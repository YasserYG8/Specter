## ADDED Requirements

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
