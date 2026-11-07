## ADDED Requirements

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
