## MODIFIED Requirements

### Requirement: Strace Attachment and Subprocess Management
The system SHALL spawn `strace` with `-qq -xx -s 65536 -e trace=write,writev -p <PID>`, attaching to the target process and every discovered active child worker process concurrently via multiple `-p` arguments, and including `-f` to follow all process threads and forks. The system SHALL NOT limit how many discovered workers it attaches to.

#### Scenario: Successful attachment to single process
- **WHEN** a valid target PID is provided and permissions allow ptrace
- **THEN** strace is spawned as a child process with `-p <PID>` and its stderr stream is consumed

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
