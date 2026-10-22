## MODIFIED Requirements

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

## ADDED Requirements

### Requirement: Docker Container Service Enumeration
The resolver SHALL discover and parse running Docker containers and their published host ports using `docker ps --format "{{json .}}"`, combining them with host listening ports to produce a unified list of active services.

#### Scenario: Discover containers with published ports
- **WHEN** active service discovery is executed and containers are running with published ports (e.g. `0.0.0.0:8080->80/tcp`)
- **THEN** the resolver returns discovered services including host port, container port, container ID, container name, and image

#### Scenario: Graceful fallback when Docker is unavailable
- **WHEN** Docker daemon is stopped, inaccessible, or `docker` command is not in PATH
- **THEN** the resolver completes discovery using host `ss`/`lsof` without returning an error
