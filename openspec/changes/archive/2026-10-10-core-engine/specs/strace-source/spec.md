## ADDED Requirements

### Requirement: Strace Attachment and Subprocess Management
The system SHALL spawn `strace -f -qq -xx -s 8192 -e trace=write -p <PID>` to capture write system calls non-invasively.

#### Scenario: Successful attachment
- **WHEN** a valid target PID is provided and permissions allow ptrace
- **THEN** strace is spawned as a child process and its stderr stream is consumed

#### Scenario: Ptrace permission denied
- **WHEN** ptrace attachment fails due to kernel `ptrace_scope`
- **THEN** the system returns an informative error recommending `sudo` or capability configuration

### Requirement: Syscall Write Hex Decoding
The parser SHALL parse `write(FD, "HEX", LEN) = RET` lines and decode hex byte sequences into raw chunks.

#### Scenario: Stdout write decoded
- **WHEN** strace outputs `[pid 100] write(1, "\x68\x65\x6c\x6c\x6f", 5) = 5`
- **THEN** the parser emits a `Chunk` with FD=1, PID=100, and decoded bytes `hello`

#### Scenario: Stderr write decoded
- **WHEN** strace outputs `[pid 100] write(2, "\x65\x72\x72", 3) = 3`
- **THEN** the parser emits a `Chunk` with FD=2, PID=100, and decoded bytes `err`

#### Scenario: Ignored file descriptors
- **WHEN** strace outputs a write to fd 3 or higher (sockets, files)
- **THEN** the parser ignores the line and emits no chunk

### Requirement: Multi-Thread Unfinished and Resumed Call Merging
The parser SHALL handle `<unfinished ...>` and `<... write resumed>` syscall entries across concurrent threads.

#### Scenario: Interleaved thread writes reassembled
- **WHEN** thread 101 emits `write(1, "\x61\x62" <unfinished ...>` followed by thread 102 emitting complete writes, and then thread 101 emits `<... write resumed> "\x63\x64", 4) = 4`
- **THEN** the parser combines the hex payloads and emits a complete chunk for thread 101

### Requirement: Clean Process Detachment on Cancellation
The system SHALL ensure `strace` detaches cleanly from the target process upon context cancellation or interrupt signal.

#### Scenario: Graceful detachment on SIGINT
- **WHEN** the user interrupts the CLI with SIGINT or context is canceled
- **THEN** the strace subprocess is sent SIGINT/SIGTERM and awaited, leaving the target process running undisturbed
