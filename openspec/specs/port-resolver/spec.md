# port-resolver Specification

## Purpose
TBD - created by archiving change core-engine. Update Purpose after archive.
## Requirements
### Requirement: Port to PID Resolution
The resolver SHALL discover the active listening process PID for a specified TCP port using `lsof` with `ss` as a fallback.

#### Scenario: Port listening process identified via lsof
- **WHEN** port 3000 is listening and `lsof -tiTCP:3000 -sTCP:LISTEN` is executed
- **THEN** the resolver returns the active PID

#### Scenario: Fallback to ss when lsof is unavailable
- **WHEN** `lsof` is not found on the system
- **THEN** the resolver invokes `ss -ltnp` and parses the output to identify the listening PID

#### Scenario: No process listening on port
- **WHEN** no process is listening on the requested port
- **THEN** the resolver returns `ErrNoProcessFound` with a message indicating the port is free

### Requirement: Target Process Metadata Inspection
The resolver SHALL inspect the target PID to determine its command line, current working directory, and stdout/stderr file descriptor targets.

#### Scenario: Reading proc metadata
- **WHEN** a valid PID is resolved
- **THEN** the resolver reads `/proc/<PID>/cmdline`, `/proc/<PID>/cwd`, and inspects `/proc/<PID>/fd/1` and `/proc/<PID>/fd/2` via `os.Readlink`

#### Scenario: Target process terminates during inspection
- **WHEN** the process exits before `/proc` can be read
- **THEN** the resolver returns `ErrProcessNotFound`

