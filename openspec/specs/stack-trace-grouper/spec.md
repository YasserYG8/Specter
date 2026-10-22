# stack-trace-grouper Specification

## Purpose
Associates multi-line exception stack trace continuation frames in real-time with their parent log records across Node.js, Python, Java, Go, and PHP without buffering latency.

## Requirements

### Requirement: Multi-Line Stack Trace Grouping
The grouper SHALL operate as a zero-buffering streaming state machine per (PID, FD) stream, immediately emitting each line while associating continuation frames with the active record so they inherit the record's level, filter fate, and dimmed styling.

#### Scenario: Node.js error trace continuations
- **WHEN** an initial error line is followed by indented lines matching `^\s+at\s+.*`
- **THEN** the continuation frames inherit the parent error level and are styled as dimmed frames

#### Scenario: Python traceback continuations and trailing exception
- **WHEN** a line starts with `Traceback (most recent call last):`
- **THEN** following indented file lines, subsequent exception chains ("During handling of..."), and the trailing unindented exception line inherit the parent trace record

#### Scenario: Java exception continuations and cause chains
- **WHEN** a line initiates an exception and is followed by indented `at ...`, `Caused by:`, or `... N common frames omitted`
- **THEN** all cause chains and frames inherit the active record

#### Scenario: Go panic and goroutine dump
- **WHEN** a goroutine dump starts with `goroutine \d+ \[...`
- **THEN** alternating unindented function calls and tab-indented file:line entries inherit the trace record

#### Scenario: Guarding indented SQL and non-trace numbered lists
- **WHEN** indented SQL statements or numbered list items (`#1 download assets`) occur outside an active trace header or lack code path references
- **THEN** they are emitted as plain unclassified lines without inheriting trace styling
