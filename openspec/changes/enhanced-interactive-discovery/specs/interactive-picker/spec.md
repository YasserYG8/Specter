## ADDED Requirements

### Requirement: Column-Aligned Service Layout and Project Presentation
The system SHALL present active services in structured, column-aligned rows displaying the service type badge (`[HOST]` or `[DOCKER]`), port, project name, process/image, and working directory or container target details.

#### Scenario: Displaying host project metadata
- **WHEN** a host server process has a resolved working directory and project name
- **THEN** the picker displays the project name and sanitized working directory path aligned in the list

#### Scenario: Displaying Docker compose project metadata
- **WHEN** an active Docker container belongs to a Compose project with a service label
- **THEN** the picker displays the project and service name (e.g. `soa / phpmyadmin`) in the project column

#### Scenario: Responsive column formatting on narrow terminals
- **WHEN** the terminal window width is too narrow to fit all columns
- **THEN** the system abbreviates or truncates secondary path details while preserving the project and port columns
