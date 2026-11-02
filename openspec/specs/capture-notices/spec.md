# capture-notices Specification

## Purpose
Status messages on stderr that explain missing output, such as a target logging to its own file or nothing being captured yet.

## Requirements

### Requirement: Own Log File Notice
The system SHALL print a notice to stderr, once per path, when a target traced through `strace` writes to a regular file whose name contains `log` through a file descriptor other than 1 or 2. The notice SHALL name the file.

#### Scenario: Target writes to its own log file
- **WHEN** a traced process writes to fd 3 and `/proc/<pid>/fd/3` resolves to the regular file `/var/log/app.log`
- **THEN** a single notice naming `/var/log/app.log` is printed to stderr, and nothing is added to stdout

#### Scenario: Repeated writes to the same log file
- **WHEN** a traced process writes to the same log file many times
- **THEN** only one notice is printed for that path

#### Scenario: Write to a socket
- **WHEN** a traced process writes to a file descriptor that resolves to a socket
- **THEN** no notice is printed

#### Scenario: Write to a regular file without log in its name
- **WHEN** a traced process writes to the regular file `/data/app.db`
- **THEN** no notice is printed

### Requirement: No Output Notice
The system SHALL print one notice to stderr if the capture pipeline has produced no entries 10 seconds after attaching. The notice SHALL say that the target may be idle, buffering its output, or logging elsewhere.

#### Scenario: Target stays silent
- **WHEN** Specter is attached and no entries have been produced after 10 seconds
- **THEN** one notice is printed to stderr, and Specter stays attached

#### Scenario: Target produces output in time
- **WHEN** an entry is produced within 10 seconds of attaching
- **THEN** no notice is printed

#### Scenario: Output is hidden by filters
- **WHEN** entries are produced within 10 seconds but `--grep` or `--level` hides all of them
- **THEN** no notice is printed

#### Scenario: Piped output stays clean
- **WHEN** stdout is piped to another command and the notice is printed
- **THEN** stdout receives no notice text

### Requirement: Oversized Capture Line Notice
The system SHALL print a notice to stderr when a line of `strace` output is too long to process, and SHALL continue capturing.

#### Scenario: Line over the processing limit
- **WHEN** `strace` emits a line longer than the reader's limit
- **THEN** a notice is printed to stderr and later writes are still captured
