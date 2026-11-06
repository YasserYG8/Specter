# strace-source Specification

## Purpose
TBD - created by archiving change core-engine. Update Purpose after archive.
## Requirements
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

### Requirement: Child Process Tree Discovery
The system SHALL scan the Linux `/proc` filesystem to identify all active child processes whose parent process ID matches the target process PID.

#### Scenario: Supervisor process with child worker
- **WHEN** a target process has child processes with `PPid == target.PID` in `/proc/<child>/status`
- **THEN** the system populates the target's child list with the active child PIDs

#### Scenario: Process with no children
- **WHEN** a target process has no active child processes
- **THEN** the system returns an empty child list and traces only the primary target PID

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

### Requirement: Multi-Thread Unfinished and Resumed Call Merging
The parser SHALL handle the `<unfinished ...>` and `<... write resumed>` entries that `strace` prints when calls from concurrent threads or processes overlap. The unfinished entry carries the full payload and the resumed entry carries the return value, and a chunk SHALL be emitted when the resumed entry arrives.

#### Scenario: Overlapping writes from two threads
- **WHEN** strace outputs `[pid 101] write(1, "\x61\x62\x0a", 3 <unfinished ...>`, then a complete write from pid 102, then `[pid 101] <... write resumed>)        = 3`
- **THEN** the parser emits the chunk for pid 102, and then a chunk for pid 101 with FD=1 and decoded bytes `ab` followed by a newline

#### Scenario: Unfinished write that fails
- **WHEN** an unfinished write is followed by a resumed entry returning `-1`
- **THEN** the parser emits no chunk for that write

#### Scenario: Unfinished write to an ignored descriptor
- **WHEN** an unfinished write to fd 3 is followed by its resumed entry
- **THEN** the parser emits no chunk

#### Scenario: No lines lost from concurrent workers
- **WHEN** 4 worker processes each write 3000 lines to stdout while Specter is attached
- **THEN** Specter outputs all 12000 lines, each one intact

### Requirement: Clean Process Detachment on Cancellation
The system SHALL ensure `strace` detaches cleanly from the target process upon context cancellation or interrupt signal.

#### Scenario: Graceful detachment on SIGINT
- **WHEN** the user interrupts the CLI with SIGINT or context is canceled
- **THEN** the strace subprocess is sent SIGINT/SIGTERM and awaited, leaving the target process running undisturbed

### Requirement: Writev Decoding
The parser SHALL decode `writev` calls to fd 1 and 2 by concatenating their `iov_base` values in order and limiting the result to the return value, including when the call is split into unfinished and resumed entries.

#### Scenario: Complete writev
- **WHEN** strace outputs `writev(1, [{iov_base="\x68\x69", iov_len=2}, {iov_base="\x0a", iov_len=1}], 2) = 3`
- **THEN** the parser emits a chunk with FD=1 and decoded bytes `hi` followed by a newline

#### Scenario: Overlapping writev
- **WHEN** strace outputs `[pid 101] writev(2, [{iov_base="\x68\x69", iov_len=2}], 1 <unfinished ...>` followed by `[pid 101] <... writev resumed>) = 2`
- **THEN** the parser emits a chunk with FD=2, PID=101 and decoded bytes `hi`

#### Scenario: Writev to an ignored descriptor
- **WHEN** strace outputs a `writev` to fd 3 or higher
- **THEN** the parser emits no chunk

### Requirement: Visible Truncation of Oversized Writes
When a write returns more bytes than `strace` captured, the parser SHALL emit the captured bytes followed by a marker stating how many bytes were not captured and a newline, so that the following line stays separate. The marker SHALL also appear with `--raw`.

#### Scenario: Write over the capture limit
- **WHEN** a target writes 100001 bytes to stdout in one call and strace captures the first 65536
- **THEN** the chunk holds the 65536 captured bytes, then the marker `[specter: 34465 more bytes not captured]`, then a newline

#### Scenario: Line after a cut write stays separate
- **WHEN** a write over the capture limit is followed by a short write
- **THEN** the short write appears on its own line

#### Scenario: Write within the capture limit
- **WHEN** a target writes a 60000-byte line
- **THEN** the line is output complete, with no marker

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
