## MODIFIED Requirements

### Requirement: Strace Attachment and Subprocess Management
The system SHALL spawn `strace` with `-q -xx -y -s 65536 -e trace=write,writev -p <PID>`, attaching to the target process and every discovered active child worker process concurrently via multiple `-p` arguments, and including `-f` to follow all process threads and forks. The `-y` flag makes strace print the destination of each descriptor. A single `-q` hides attach and detach messages but keeps the `+++ exited with N +++` and `+++ killed by SIG +++` lines, which report how traced processes end. The system SHALL NOT limit how many discovered workers it attaches to.

#### Scenario: Successful attachment to single process
- **WHEN** a valid target PID is provided and permissions allow ptrace
- **THEN** strace is spawned as a child process with `-q`, `-y` and `-p <PID>` and its stderr stream is consumed

#### Scenario: Attachment to supervisor with existing child workers
- **WHEN** a target supervisor process has pre-existing child worker processes
- **THEN** strace is spawned with multiple `-p` flags (`-p <target_pid> -p <child1_pid> ...`) to capture writes from both supervisor and child workers

#### Scenario: More than 16 existing child workers
- **WHEN** a target has 20 pre-existing child workers and each writes lines to stdout while Specter is attached
- **THEN** strace is spawned with a `-p` flag for the target and for all 20 workers, and Specter outputs every line from every worker

#### Scenario: Worker exits before it is attached
- **WHEN** a discovered worker has already exited and strace prints `strace: attach: ptrace(PTRACE_SEIZE, <PID>): No such process`
- **THEN** Specter keeps capturing writes from the other processes

#### Scenario: Ptrace permission denied
- **WHEN** ptrace attachment fails due to kernel `ptrace_scope`
- **THEN** the system returns an informative error recommending `sudo` or capability configuration

#### Scenario: Tree tracing enabled
- **WHEN** `--tree` is specified
- **THEN** strace is spawned with `-f` and traces the entire recursive process tree across forks

## ADDED Requirements

### Requirement: Target Exit Status From Strace
The strace source SHALL report the target's end from the exit line whose PID is the target PID, as an exit code (`+++ exited with N +++`) or a signal name (`+++ killed by SIG... +++`). Exit lines of other traced processes, such as workers, threads and helpers, SHALL NOT be reported as the target's end. strace leaves the `[pid N]` prefix off while only one process is traced, so a line without a prefix belongs to the target only if the target's own end has not been reported yet. After that it is the last child that outlived the target, and SHALL NOT replace the target's status.

#### Scenario: Target exits with a code
- **WHEN** strace prints `+++ exited with 3 +++` for the target PID
- **THEN** the source reports that the target exited with code 3

#### Scenario: Target killed
- **WHEN** strace prints `[pid 595] +++ killed by SIGKILL +++` and 595 is the target PID
- **THEN** the source reports that the target was killed by SIGKILL

#### Scenario: Helper exits before the target
- **WHEN** strace prints `[pid 5101] +++ exited with 0 +++` for a child, and later `[pid 5000] +++ exited with 3 +++` for target PID 5000
- **THEN** the source reports exit code 3

#### Scenario: Child exits after the target
- **WHEN** strace prints `[pid 5000] +++ exited with 3 +++` for target PID 5000, and later `[pid 5101] +++ exited with 0 +++` for a child that outlived it
- **THEN** the source still reports exit code 3

#### Scenario: Last surviving child prints no prefix
- **WHEN** strace prints `[pid 5000] +++ exited with 3 +++` for target PID 5000, and then `+++ exited with 0 +++` without a prefix, because the child that is left is the only process traced
- **THEN** the source still reports exit code 3

#### Scenario: Target alone prints no prefix
- **WHEN** the target is the only process traced and strace prints `+++ exited with 3 +++` without a prefix
- **THEN** the source reports that the target exited with code 3

### Requirement: Ptrace Restriction Warning
Before attaching with strace, the system SHALL warn when Yama `ptrace_scope` may block the attach, except when Specter runs as root at scope 1 or 2, where root can attach. At scope 3 the warning SHALL be shown for every user.

#### Scenario: Normal user at scope 1
- **WHEN** a non-root user runs Specter and `ptrace_scope` is 1
- **THEN** Specter prints the `ptrace_scope = 1` warning with its remedy

#### Scenario: Root at scope 1
- **WHEN** root runs Specter and `ptrace_scope` is 1
- **THEN** Specter prints no `ptrace_scope` warning

#### Scenario: Root at scope 3
- **WHEN** root runs Specter and `ptrace_scope` is 3
- **THEN** Specter prints the `ptrace_scope = 3` warning
