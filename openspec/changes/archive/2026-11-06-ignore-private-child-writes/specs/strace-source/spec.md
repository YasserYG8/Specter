## MODIFIED Requirements

### Requirement: Strace Attachment and Subprocess Management
The system SHALL spawn `strace` with `-qq -xx -y -s 65536 -e trace=write,writev -p <PID>`, attaching to the target process and every discovered active child worker process concurrently via multiple `-p` arguments, and including `-f` to follow all process threads and forks. The `-y` flag makes strace print the destination of each descriptor. The system SHALL NOT limit how many discovered workers it attaches to.

#### Scenario: Successful attachment to single process
- **WHEN** a valid target PID is provided and permissions allow ptrace
- **THEN** strace is spawned as a child process with `-y` and `-p <PID>` and its stderr stream is consumed

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

### Requirement: Syscall Write Hex Decoding
The parser SHALL parse `write(FD, "HEX", LEN) = RET` lines and decode hex byte sequences into raw chunks. The parser SHALL also accept the descriptor annotation that `-y` adds, as in `write(FD<PATH>, "HEX", LEN) = RET`, where PATH may itself be hex-encoded, and SHALL keep the decoded PATH with the call, including when the call is split into unfinished and resumed entries.

#### Scenario: Stdout write decoded
- **WHEN** strace outputs `[pid 100] write(1, "\x68\x65\x6c\x6c\x6f", 5) = 5`
- **THEN** the parser emits a `Chunk` with FD=1, PID=100, and decoded bytes `hello`

#### Scenario: Stderr write decoded
- **WHEN** strace outputs `[pid 100] write(2, "\x65\x72\x72", 3) = 3`
- **THEN** the parser emits a `Chunk` with FD=2, PID=100, and decoded bytes `err`

#### Scenario: Write with a descriptor annotation
- **WHEN** strace outputs `[pid 100] write(1<pipe:[4021]>, "\x68\x69\x0a", 3) = 3`
- **THEN** the parser decodes FD=1, PID=100, bytes `hi` followed by a newline, and destination `pipe:[4021]`

#### Scenario: Unfinished write with a descriptor annotation
- **WHEN** strace outputs `[pid 101] write(1</dev/pts/3>, "\x61\x0a", 2 <unfinished ...>` and later `[pid 101] <... write resumed>) = 2`
- **THEN** the parser decodes the call when the resumed entry arrives, with destination `/dev/pts/3`

#### Scenario: Ignored file descriptors
- **WHEN** strace outputs a write to fd 3 or higher (sockets, files)
- **THEN** the parser ignores the line and emits no chunk

## ADDED Requirements

### Requirement: Output Only Writes That Reach the Target's Own Streams
When Specter starts tracing, the system SHALL read the destinations of the target's fd 1 and fd 2 from `/proc/<pid>/fd`. A write to fd 1 or 2 by the target process itself SHALL always be output. A write to fd 1 or 2 by any other traced process SHALL be output only when its destination equals the target's fd 1 or fd 2 destination, and SHALL be dropped otherwise. When the destination of the write, or either destination of the target, cannot be determined, the write SHALL be output, because the unreadable destination could be the one the write belongs to.

#### Scenario: Helper writes to a private pipe
- **WHEN** the target's stdout is `pipe:[100]` and a child process writes a line to fd 1, whose destination is `pipe:[200]`
- **THEN** Specter does not output the line

#### Scenario: Helper writes to /dev/null
- **WHEN** the target's stdout is `/dev/pts/3` and a child process writes to fd 1, whose destination is `/dev/null`
- **THEN** Specter does not output the line

#### Scenario: Worker shares the target's stdout
- **WHEN** the target's stdout is `pipe:[100]` and a worker process writes a line to fd 1, whose destination is `pipe:[100]`
- **THEN** Specter outputs the line

#### Scenario: Child writes to its stdout, which is the target's stderr
- **WHEN** the target's stderr is `/dev/pts/3` and a child process writes to fd 1, whose destination is `/dev/pts/3`
- **THEN** Specter outputs the line

#### Scenario: Target writes to /dev/null
- **WHEN** the target process writes a line to fd 1, whose destination is `/dev/null`
- **THEN** Specter outputs the line

#### Scenario: Short-lived helper
- **WHEN** a child process writes to a private pipe and exits within a millisecond
- **THEN** Specter does not output the line, because the destination comes from strace at the moment of the call

#### Scenario: Destination unknown
- **WHEN** strace prints a write by a child process without a descriptor annotation
- **THEN** Specter outputs the line

#### Scenario: One target destination unreadable
- **WHEN** the target's stdout is `pipe:[100]`, reading `/proc/<pid>/fd/2` of the target fails, and a child process writes to fd 2, whose destination is `pipe:[300]`
- **THEN** Specter outputs the line

#### Scenario: Target destinations unreadable
- **WHEN** reading `/proc/<pid>/fd/1` and `/proc/<pid>/fd/2` of the target fails
- **THEN** Specter outputs every write to fd 1 and 2, as it did before this change
