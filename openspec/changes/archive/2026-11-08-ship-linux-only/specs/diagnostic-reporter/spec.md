## ADDED Requirements

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
