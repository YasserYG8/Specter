## ADDED Requirements

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
