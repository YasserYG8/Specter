## ADDED Requirements

### Requirement: Direct Container Target Selection
The source selector SHALL select `DockerSource` when the target process represents a container identified by name or container ID, without requiring an associated host PID.

#### Scenario: Container target without host PID
- **WHEN** target specifies a non-empty `Container` identifier and PID is 0 or uninspected
- **THEN** the selector picks `DockerSource` and sets the target container identifier

#### Scenario: Container target with forced source override
- **WHEN** user specifies `--source docker` with a named container target
- **THEN** the selector uses `DockerSource` and attaches to the specified container
