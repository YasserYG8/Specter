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

### Requirement: Target Exit Report
When the target stops while Specter is attached, and the user did not interrupt Specter, the system SHALL stop reading, print one line on stderr that says how the target ended, and exit with status 1. When the user interrupts Specter, the system SHALL print that it detached cleanly and exit with status 0. For a host process the line SHALL name the PID and, when known, the exit code or the signal. For a container it SHALL name the container and, when known, the exit code.

#### Scenario: Normal exit with a code under strace
- **WHEN** the target PID 617 exits with code 3 while traced by strace
- **THEN** Specter prints `Target PID 617 exited with code 3.` and exits with status 1

#### Scenario: Killed by a signal
- **WHEN** the target PID 595 is killed by SIGTERM
- **THEN** Specter prints `Target PID 595 was killed by SIGTERM.` and exits with status 1

#### Scenario: Killed by SIGKILL
- **WHEN** the target PID 595 is killed by SIGKILL
- **THEN** Specter prints `Target PID 595 was killed by SIGKILL.` followed by a hint that SIGKILL often comes from the kernel's out-of-memory killer and that `dmesg | grep -i oom` shows it

#### Scenario: Target writing to a file exits
- **WHEN** the target writes its output to a regular file, so the file source is used, and the target exits
- **THEN** within about one second Specter stops reading the file, prints `Target PID <pid> exited.`, and exits with status 1

#### Scenario: Exit code unknown
- **WHEN** the target is gone and no source reported its exit code or signal
- **THEN** Specter prints `Target PID <pid> exited.`

#### Scenario: Source stops without a known reason
- **WHEN** the log source of a host process ends on its own, the target is still running, and no exit status was reported
- **THEN** Specter prints `Capture of PID <pid> ended because its log source stopped.` and exits with status 1, and does not say that it detached cleanly

#### Scenario: Zombie target
- **WHEN** the target has exited but its parent has not reaped it yet, so `/proc/<pid>` still exists in state `Z`
- **THEN** Specter treats the target as gone

#### Scenario: PID reused by another process
- **WHEN** the target exits and a new process gets the same PID before the next check
- **THEN** Specter treats the target as gone, because the process start time differs from the one recorded at attach

#### Scenario: Container exits
- **WHEN** Specter follows container `billing-api` and the container exits with code 137
- **THEN** Specter prints `Container billing-api exited with code 137.` and exits with status 1

#### Scenario: Container state unreadable
- **WHEN** the container's log stream ends on its own and `docker inspect` fails
- **THEN** Specter prints `Container billing-api stopped.` and exits with status 1

#### Scenario: User interrupts
- **WHEN** the user presses Ctrl+C while the target is still running
- **THEN** Specter prints `Detached from PID <pid> cleanly.` (or the container form) and exits with status 0

#### Scenario: Journal by systemd unit
- **WHEN** Specter follows a systemd unit with `journalctl -u` and the unit's main process exits and systemd restarts it
- **THEN** Specter keeps following the unit's journal and does not report the target as ended
