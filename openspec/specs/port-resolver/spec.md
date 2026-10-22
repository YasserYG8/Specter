# port-resolver Specification

## Purpose
TBD - created by archiving change core-engine. Update Purpose after archive.
## Requirements
### Requirement: Port to PID Resolution
The resolver SHALL discover the active listening process PID for a specified TCP port using `lsof` with `ss` as a fallback. If no host PID is found, or if the port is listening without process metadata due to container network forwarding, the resolver SHALL query running Docker containers to check for a matching published port. When the requested port is not listening and not mapped by any container, the resolver SHALL enumerate active listening TCP ports and containers on the system to provide diagnostic context.

#### Scenario: Port listening process identified via lsof
- **WHEN** port 3000 is listening and `lsof -tiTCP:3000 -sTCP:LISTEN` is executed
- **THEN** the resolver returns the active PID

#### Scenario: Fallback to ss when lsof is unavailable
- **WHEN** `lsof` is not found on the system
- **THEN** the resolver invokes `ss -ltnp` and parses the output to identify the listening PID

#### Scenario: Fallback to Docker container when no host PID is found
- **WHEN** a port is listening without a local host PID or is forwarded by Docker
- **THEN** the resolver queries running Docker containers and matches the port against container port mappings

#### Scenario: No process or container listening on port
- **WHEN** no process or container is listening on the requested port
- **THEN** the resolver queries active listening TCP ports and containers and returns `ErrNoProcessFound` bundled with discovered services and ranked suggestions

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

### Requirement: Docker Container Service Enumeration
The resolver SHALL discover and parse running Docker containers and their published host ports using `docker ps --format "{{json .}}"`, combining them with host listening ports to produce a unified list of active services.

#### Scenario: Discover containers with published ports
- **WHEN** active service discovery is executed and containers are running with published ports (e.g. `0.0.0.0:8080->80/tcp`)
- **THEN** the resolver returns discovered services including host port, container port, container ID, container name, and image

#### Scenario: Graceful fallback when Docker is unavailable
- **WHEN** Docker daemon is stopped, inaccessible, or `docker` command is not in PATH
- **THEN** the resolver completes discovery using host `ss`/`lsof` without returning an error

### Requirement: Project Metadata Resolution
The resolver SHALL inspect the process working directory, root manifest files, and container Compose labels to associate a human-readable project name and sanitized directory path with discovered services.

#### Scenario: Resolving project name from directory basename
- **WHEN** a process has a valid working directory (e.g. `/home/user/projects/example`) without root manifest files
- **THEN** the resolver assigns the directory basename (`example`) as the project name

#### Scenario: Resolving project name from manifest files
- **WHEN** a process working directory contains a standard manifest file (`package.json`, `pom.xml`, `go.mod`, or `pyproject.toml`)
- **THEN** the resolver parses and extracts the declared package/project name

#### Scenario: Resolving Docker Compose project labels
- **WHEN** an active Docker container has `com.docker.compose.project` and `com.docker.compose.service` labels
- **THEN** the resolver assigns the combined Compose project and service name to the discovered service

