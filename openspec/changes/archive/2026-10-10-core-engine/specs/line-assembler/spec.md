## ADDED Requirements

### Requirement: Line Buffering per Thread and Descriptor
The assembler SHALL maintain independent byte buffers for each unique `(PID, FD)` pair to prevent log corruption from interleaved writes.

#### Scenario: Split write reassembly
- **WHEN** chunks `Chunk{Bytes: "foo", FD: 1, PID: 10}` and `Chunk{Bytes: "bar\n", FD: 1, PID: 10}` arrive sequentially
- **THEN** the assembler emits a single line `foobar`

#### Scenario: Multi-thread isolation
- **WHEN** thread 10 writes `"Thread 1: "` and thread 20 writes `"Thread 2: OK\n"` before thread 1 writes `"OK\n"`
- **THEN** lines are emitted without character interleaving

### Requirement: UTF-8 Stream Decoding
The assembler SHALL properly handle multibyte UTF-8 characters that are split across chunk write boundaries.

#### Scenario: Split multibyte sequence
- **WHEN** a 3-byte or 4-byte UTF-8 character is split across two consecutive chunks
- **THEN** the decoded line contains the valid composite rune without replacement character corruption

### Requirement: Idle Timeout Flush for Interactive Prompts
The assembler SHALL flush partial line buffers when no new bytes are received within 150 milliseconds.

#### Scenario: Unfinished prompt flushed on idle
- **WHEN** a target process writes `"Loading... "` without a trailing newline and pauses for over 150ms
- **THEN** the assembler flushes `"Loading... "` marked as partial
