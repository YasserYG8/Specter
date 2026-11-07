## ADDED Requirements

### Requirement: Already Traced Target
Before starting strace, the system SHALL read `TracerPid` from `/proc/<pid>/status` for the target. When it is not 0, the system SHALL NOT start strace, and SHALL exit with a non-zero status and a diagnostic that names the target PID, the tracer PID and the tracer's command. When the tracer's parent process is named `specter`, the diagnostic SHALL also name that Specter's PID. The diagnostic SHALL say that Linux allows one tracer per process and that the other tracer must stop first, and SHALL NOT recommend `sudo`. The system SHALL NOT signal the tracer.

#### Scenario: Target traced by another Specter
- **WHEN** the target PID 773 has `TracerPid: 844`, PID 844 is `strace -f ... -p 773`, and its parent PID 825 is `specter`
- **THEN** Specter prints a diagnostic saying PID 773 is already traced by PID 844 (strace), started by specter PID 825, that only one tracer is allowed per process, and to stop the other Specter first, and exits with a non-zero status without starting strace

#### Scenario: Target traced by a debugger
- **WHEN** the target has `TracerPid: 900` and PID 900 is `gdb -p 773`
- **THEN** the diagnostic names PID 900 and `gdb`, without mentioning Specter

#### Scenario: Target not traced
- **WHEN** the target has `TracerPid: 0`
- **THEN** Specter starts strace as usual

#### Scenario: Tracer details unreadable
- **WHEN** the target's `TracerPid` is 844 and `/proc/844/cmdline` cannot be read
- **THEN** the diagnostic still names PID 844 and says the target is already traced

#### Scenario: Tracer appears after the check
- **WHEN** the target was not traced at the check, and strace then fails with "Operation not permitted" while `TracerPid` is now 844
- **THEN** Specter shows the "already traced" diagnostic instead of the permission diagnostic, and exits with a non-zero status

#### Scenario: Permission problem without a tracer
- **WHEN** strace fails with "Operation not permitted" and the target's `TracerPid` is 0
- **THEN** Specter shows the existing permission diagnostic

### Requirement: Already Traced Workers
The system SHALL leave discovered workers whose `TracerPid` is not 0 out of strace's `-p` arguments, and SHALL print one notice on stderr that lists them with their tracer PIDs and says their output is not captured.

#### Scenario: One worker already traced
- **WHEN** the target has workers 1019 and 1039, and worker 1039 has `TracerPid: 1500`
- **THEN** strace is started with `-p` for the target and 1019 only, and Specter prints a notice that worker 1039 is already traced by PID 1500 and its output is not captured
