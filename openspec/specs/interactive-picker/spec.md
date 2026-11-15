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

### Requirement: Esc Returns to the Picker
While Specter streams and reads keys (see Keys During a Stream), pressing Esc SHALL stop every session the same way as Ctrl+C (no tracer stays attached), print the "detached" line for each target that was still running, and then clear the screen and open the interactive picker, so the logs of the stream the user left are gone. Clearing SHALL erase the screen and the terminal's scrollback, as the `clear` command does, so the old logs cannot be reached by scrolling up, and SHALL happen after every session has stopped. This SHALL work whether the stream was started from the picker, from one target, or from several targets. Choosing a service in the picker SHALL start a new stream for that one target. Closing the picker (Esc, `q` or Ctrl+C in the picker) SHALL end Specter with the exit status of the last stream: 1 if a target of that stream ended by itself, 0 otherwise. Ctrl+C during a stream SHALL quit Specter as before. `--duration` SHALL count from the start of each stream. A key sequence that starts with the Esc byte (such as an arrow key or Alt with a letter) SHALL NOT count as Esc.

#### Scenario: Esc from a picked service
- **WHEN** the user runs `specter`, picks port 4200, and presses Esc while lines stream
- **THEN** Specter prints that it detached from PID <pid> cleanly, clears the screen, and shows the picker again

#### Scenario: The picker opens on a clean screen
- **WHEN** log lines fill the screen and the user presses Esc
- **THEN** the screen and the scrollback are erased once every target has detached, and the picker is drawn at the top, with no log line above it, also after scrolling up

#### Scenario: Esc from several targets
- **WHEN** the user runs `sudo specter 4200 8082` and presses Esc
- **THEN** Specter prints a "detached cleanly" line for `:4200` and for `:8082`, no `strace` stays attached, and the picker is shown

#### Scenario: Pick again
- **WHEN** the picker was opened by Esc and the user selects port 8082
- **THEN** Specter attaches to port 8082 and streams it

#### Scenario: Quit from the picker
- **WHEN** the picker was opened by Esc and the user presses Esc or `q`
- **THEN** Specter exits, the terminal is back to normal, and the exit status is 0 if no target ended by itself in the last stream

#### Scenario: Arrow key is not Esc
- **WHEN** the user presses the up arrow key while lines stream
- **THEN** Specter keeps streaming

#### Scenario: Ctrl+C still quits
- **WHEN** the user presses Ctrl+C while lines stream
- **THEN** Specter detaches and exits as before, without showing the picker

### Requirement: Keys During a Stream
Specter SHALL read keys during a stream only when stdin and stderr are terminals and Specter is in the terminal's foreground process group. In that case, it SHALL turn off line buffering and echo on the terminal for the time of the stream, keep the terminal's signal keys (so Ctrl+C still sends SIGINT) and its output processing (so line breaks still return to the first column), and restore the terminal's previous settings before the picker opens and before Specter exits, on every exit path it can catch (normal end, Ctrl+C, SIGTERM, `--duration`, an error). Pressing Enter SHALL add one empty line on screen when stdout is a terminal, and SHALL write nothing when stdout is not a terminal. Other keys SHALL be ignored. When keys are read, Specter SHALL print one line on stderr after the attach lines: `Press Esc to pick another service, Ctrl+C to quit.` When keys are not read, the terminal SHALL NOT be changed and nothing SHALL be printed about keys.

#### Scenario: Typed text does not mix with logs
- **WHEN** the user types `abc` while lines stream
- **THEN** `abc` does not appear in the output

#### Scenario: Enter spaces out the logs
- **WHEN** the user presses Enter while lines stream to the terminal
- **THEN** one empty line appears between the log lines

#### Scenario: Piped stdout with a terminal
- **WHEN** the user runs `specter 3000 | grep 500` in a terminal and presses Enter
- **THEN** nothing is written to the pipe, and Esc still opens the picker on stderr

#### Scenario: Stdin is not a terminal
- **WHEN** the user runs `specter 3000 < /dev/null`
- **THEN** the terminal is not changed, no key hint is printed, and the run behaves as before this change

#### Scenario: Background job
- **WHEN** the user runs `specter 3000 &` in an interactive shell
- **THEN** Specter does not read keys or change the terminal, and is not stopped by the terminal for doing so

#### Scenario: Terminal restored after Ctrl+C
- **WHEN** the user presses Ctrl+C during a stream and Specter exits
- **THEN** the shell shows typed characters again and line editing works as before
