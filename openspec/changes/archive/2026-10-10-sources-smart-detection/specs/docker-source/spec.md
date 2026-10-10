## ADDED Requirements

### Requirement: Container Detection from Cgroup
The Docker source SHALL detect whether a process is running inside a Docker or containerd container by inspecting `/proc/<pid>/cgroup`.

#### Scenario: Cgroup v2 docker scope detected
- **WHEN** `/proc/<pid>/cgroup` contains a line matching `0::/system.slice/docker-<64_hex_id>.scope`
- **THEN** the system extracts the container ID and marks the target as containerized

#### Scenario: Cgroup v1 docker path detected
- **WHEN** `/proc/<pid>/cgroup` contains a line matching `1:name=systemd:/docker/<64_hex_id>`
- **THEN** the system extracts the container ID and marks the target as containerized

#### Scenario: Non-containerized process
- **WHEN** `/proc/<pid>/cgroup` contains standard host user slice paths without container markers
- **THEN** the system returns an empty container ID indicating a native host process

### Requirement: Live Docker Container Log Streaming
The Docker source SHALL execute `docker logs -f --tail 0 <container_id>` to stream standard output and standard error chunks.

#### Scenario: Streaming live container output
- **WHEN** a valid container target is started
- **THEN** `docker logs -f --tail 0 <id>` is spawned with stdout and stderr multiplexed into `Chunk` objects

#### Scenario: Clean detachment on context cancellation
- **WHEN** the context is canceled or SIGINT is received
- **THEN** the `docker logs` subprocess is terminated cleanly without affecting the running container
