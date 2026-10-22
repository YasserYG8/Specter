# interactive-picker Specification

## Purpose
Provides an interactive keyboard-driven terminal selector for active host listening ports and running Docker containers when `specter` is invoked with zero arguments, with automatic graceful non-TTY tabular fallbacks.

## Requirements

### Requirement: Interactive Terminal Service Picker
The system SHALL present an interactive keyboard-navigable selection menu when `specter` is invoked with zero arguments in an interactive terminal environment (TTY).

#### Scenario: Interactive navigation and target selection
- **WHEN** user executes `specter` without arguments in an interactive terminal and active services exist
- **THEN** the system renders the list of active host ports and Docker containers with a selection cursor, supports arrow key navigation (`↑`/`↓`), and executes log streaming for the chosen target when `<Enter>` is pressed

#### Scenario: Clean cancellation on user abort
- **WHEN** user presses `Ctrl+C`, `Esc`, or `q` while navigating the picker
- **THEN** the system restores normal terminal mode and exits cleanly with exit code 0

### Requirement: Non-Interactive Service Listing
The system SHALL output a formatted plaintext table of active services when `specter` is invoked with zero arguments in a non-interactive or redirected environment (non-TTY).

#### Scenario: Piped or redirected zero-argument invocation
- **WHEN** user executes `specter` with zero arguments and standard input or output is not an interactive terminal
- **THEN** the system prints an aligned table of active listening ports and Docker containers with usage instructions and exits with exit code 0

### Requirement: Empty Discovery State Handling
The system SHALL display an informative notification when no active listening ports or running containers are detected.

#### Scenario: No active listening ports or containers detected
- **WHEN** `specter` is invoked without arguments and discovery returns an empty list of services
- **THEN** the system outputs a message indicating that no active listening ports or running containers were found and exits cleanly

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
