# port-resolver Specification

## Purpose
TBD - created by archiving change core-engine. Update Purpose after archive.
## Requirements
### Requirement: Port to PID Resolution
The resolver SHALL discover the active listening process PID for a specified TCP port using `lsof` with `ss` as a fallback. When the requested port is not listening, the resolver SHALL enumerate active listening TCP ports on the system to provide diagnostic context.

#### Scenario: Port listening process identified via lsof
- **WHEN** port 3000 is listening and `lsof -tiTCP:3000 -sTCP:LISTEN` is executed
- **THEN** the resolver returns the active PID

#### Scenario: Fallback to ss when lsof is unavailable
- **WHEN** `lsof` is not found on the system
- **THEN** the resolver invokes `ss -ltnp` and parses the output to identify the listening PID

#### Scenario: No process listening on port
- **WHEN** no process is listening on the requested port
- **THEN** the resolver queries active listening TCP ports and returns `ErrNoProcessFound` bundled with the list of discovered listening ports and ranked suggestions

### Requirement: Target Process Metadata Inspection
The resolver SHALL inspect the target PID to determine its command line, current working directory, and stdout/stderr file descriptor targets.

#### Scenario: Reading proc metadata
- **WHEN** a valid PID is resolved
- **THEN** the resolver reads `/proc/<PID>/cmdline`, `/proc/<PID>/cwd`, and inspects `/proc/<PID>/fd/1` and `/proc/<PID>/fd/2` via `os.Readlink`

#### Scenario: Target process terminates during inspection
- **WHEN** the process exits before `/proc` can be read
- **THEN** the resolver returns `ErrProcessNotFound`

### Requirement: Active Listening Ports Enumeration
The resolver SHALL discover and parse all active listening TCP ports across the system using `ss -H -ltnp` or `lsof -iTCP -sTCP:LISTEN -P -n`.

#### Scenario: Enumerate active ports with process metadata
- **WHEN** active port discovery is invoked
- **THEN** the resolver returns a list of active ports including port number, PID, process name, and command-line summary

#### Scenario: Listening port owned by privileged user
- **WHEN** a port is listening in `ss` but process information is hidden due to permissions
- **THEN** the resolver includes the port in the active list with a flag indicating the process requires root privileges to inspect

### Requirement: Intelligent Port Suggestion Ranking
The resolver SHALL rank active listening ports against the requested target port using proximity heuristics and suggest the most likely match.

#### Scenario: Numerical proximity match
- **WHEN** user requests port 3000 and port 3001 is actively listening
- **THEN** the resolver identifies port 3001 as the top suggestion

#### Scenario: Web application priority
- **WHEN** multiple ports are active (e.g. 5432 postgres and 8080 python) and user requested 8000
- **THEN** the resolver prioritizes user application servers (e.g., node, python, go) over system daemons

