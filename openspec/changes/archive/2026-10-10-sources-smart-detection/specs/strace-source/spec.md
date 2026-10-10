## MODIFIED Requirements

### Requirement: Strace Attachment and Subprocess Management
The system SHALL spawn `strace` with `-qq -xx -s 8192 -e trace=write -p <PID>`, including `-f` when process tree tracing is enabled, to capture write system calls non-invasively.

#### Scenario: Successful attachment
- **WHEN** a valid target PID is provided and permissions allow ptrace
- **THEN** strace is spawned as a child process and its stderr stream is consumed

#### Scenario: Ptrace permission denied
- **WHEN** ptrace attachment fails due to kernel `ptrace_scope`
- **THEN** the system returns an informative error recommending `sudo` or capability configuration

#### Scenario: Tree tracing enabled
- **WHEN** `--tree` is specified
- **THEN** strace is spawned with `-f` to follow child processes and threads across forks
