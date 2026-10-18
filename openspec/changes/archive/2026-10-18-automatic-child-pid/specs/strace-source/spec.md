## MODIFIED Requirements

### Requirement: Strace Attachment and Subprocess Management
The system SHALL spawn `strace` with `-qq -xx -s 8192 -e trace=write -p <PID>`, attaching to the target process and any discovered active child worker processes concurrently via multiple `-p` arguments, and including `-f` to follow all process threads and forks.

#### Scenario: Successful attachment to single process
- **WHEN** a valid target PID is provided and permissions allow ptrace
- **THEN** strace is spawned as a child process with `-p <PID>` and its stderr stream is consumed

#### Scenario: Attachment to supervisor with existing child workers
- **WHEN** a target supervisor process has pre-existing child worker processes
- **THEN** strace is spawned with multiple `-p` flags (`-p <target_pid> -p <child1_pid> ...`) to capture writes from both supervisor and child workers

#### Scenario: Ptrace permission denied
- **WHEN** ptrace attachment fails due to kernel `ptrace_scope`
- **THEN** the system returns an informative error recommending `sudo` or capability configuration

#### Scenario: Tree tracing enabled
- **WHEN** `--tree` is specified
- **THEN** strace is spawned with `-f` and traces the entire recursive process tree across forks

## ADDED Requirements

### Requirement: Child Process Tree Discovery
The system SHALL scan the Linux `/proc` filesystem to identify all active child processes whose parent process ID matches the target process PID.

#### Scenario: Supervisor process with child worker
- **WHEN** a target process has child processes with `PPid == target.PID` in `/proc/<child>/status`
- **THEN** the system populates the target's child list with the active child PIDs

#### Scenario: Process with no children
- **WHEN** a target process has no active child processes
- **THEN** the system returns an empty child list and traces only the primary target PID
