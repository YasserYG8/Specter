# source-selector Specification

## Purpose
Probes target process environment metadata and selects the lowest-overhead acquisition source according to priority (Docker > Journal > File > Strace) with support for user `--source` overrides.

## Requirements

### Requirement: Automatic Source Selection Priority
The source selector SHALL evaluate target process environment metadata and select the lowest-overhead acquisition source.

#### Scenario: Docker container detected
- **WHEN** the target process is containerized and `docker` executable is available
- **THEN** the selector picks `DockerSource`

#### Scenario: Regular disk file detected
- **WHEN** the process is not containerized and fd 1 or 2 points to a regular file
- **THEN** the selector picks `FileTailSource`

#### Scenario: Systemd service detected
- **WHEN** the process is governed by a systemd unit and `journalctl` is available
- **THEN** the selector picks `JournalSource`

#### Scenario: Terminal or pipe default
- **WHEN** no specialized environment matches
- **THEN** the selector falls back to `StraceSource`

### Requirement: User Source Override Flag
The source selector SHALL honor the user-specified `--source` flag (`docker`, `file`, `journal`, `strace`).

#### Scenario: Valid source specified by user
- **WHEN** the user specifies `--source docker`
- **THEN** the selector uses `DockerSource` regardless of auto-detection heuristics

#### Scenario: Invalid source name provided
- **WHEN** the user provides an unknown source name (e.g., `--source invalid`)
- **THEN** the system returns an informative validation error listing supported sources

### Requirement: Direct Container Target Selection
The source selector SHALL select `DockerSource` when the target process represents a container identified by name or container ID, without requiring an associated host PID.

#### Scenario: Container target without host PID
- **WHEN** target specifies a non-empty `Container` identifier and PID is 0 or uninspected
- **THEN** the selector picks `DockerSource` and sets the target container identifier

#### Scenario: Container target with forced source override
- **WHEN** user specifies `--source docker` with a named container target
- **THEN** the selector uses `DockerSource` and attaches to the specified container
