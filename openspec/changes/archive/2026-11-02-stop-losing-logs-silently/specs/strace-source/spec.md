## MODIFIED Requirements

### Requirement: Strace Attachment and Subprocess Management
The system SHALL spawn `strace` with `-qq -xx -s 65536 -e trace=write,writev -p <PID>`, attaching to the target process and any discovered active child worker processes concurrently via multiple `-p` arguments, and including `-f` to follow all process threads and forks.

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

## ADDED Requirements

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
