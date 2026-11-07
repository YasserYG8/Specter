# log-filter-renderer Specification

## Purpose
Filters log streams by severity threshold, regex/grep patterns, and standard error descriptor, and renders colorized terminal output respecting NO_COLOR and non-TTY redirection.

## Requirements

### Requirement: Minimum Log Level Filtering
The filter SHALL discard log entries below the configured `--level` threshold (ordering: `DEBUG` < `INFO` < `WARN` < `ERROR`).

#### Scenario: Filtering by warn level
- **WHEN** `--level warn` is specified
- **THEN** `DEBUG` and `INFO` entries are suppressed while `WARN` and `ERROR` entries are preserved

#### Scenario: Preserving unclassified entries
- **WHEN** an entry has level `NONE` (unclassified plain text)
- **THEN** it is passed through so application messages are not lost

### Requirement: Atomic Multi-Line Filter Matching
The filter SHALL evaluate `--grep` patterns so that a stack trace is shown whole or not at all. While `--grep` is set, the lines of each stack trace SHALL be held per process and stream until the trace ends, and the trace SHALL then be output complete if any of its lines matches, and dropped if none matches. A trace ends when its closing exception line arrives (the trailer of a Python traceback), when the same process and stream writes a line that does not continue it, when that stream has written nothing for 200 ms, or when the input ends. For other multi-line records, such as .NET two-line records, continuation lines SHALL inherit their parent record's match state, and a matching continuation line SHALL print even if its parent line did not match. Without `--grep`, no line SHALL be held.

#### Scenario: Continuation frames inherit header grep match
- **WHEN** a record header matches `--grep`
- **THEN** all subsequent continuation frames for that record are admitted through the filter

#### Scenario: Python traceback matched on its last line
- **WHEN** `--grep ZeroDivision` is set and a process writes `Traceback (most recent call last):`, two frame lines, and `ZeroDivisionError: division by zero`
- **THEN** all four lines are output, in order

#### Scenario: Trace matched on a frame
- **WHEN** `--grep test_server` is set and only the frame line `File "/app/test_server.py", line 40, in do_GET` of a traceback matches
- **THEN** the whole traceback is output, including its header

#### Scenario: Trace with no matching line
- **WHEN** `--grep timeout` is set and no line of a Java stack trace contains `timeout`
- **THEN** no line of that trace is output

#### Scenario: Trace without a closing line
- **WHEN** `--grep db` is set and a process writes a Node.js trace whose header matches, followed by 200 ms of silence on that stream
- **THEN** the trace is output when the 200 ms pass

#### Scenario: Next line closes the trace
- **WHEN** `--grep db` is set and a process writes a matching Java trace, then a plain log line on the same stream
- **THEN** the trace is output before the plain line is evaluated

#### Scenario: Other streams keep flowing
- **WHEN** `--grep` is set, process 1 is in the middle of a held trace, and process 2 writes a matching line
- **THEN** process 2's line is output without waiting for process 1's trace to end

#### Scenario: Very long trace
- **WHEN** `--grep` is set and a trace has 1500 lines
- **THEN** the first 1000 lines are matched and output as a block if any of them matches, and each later line is admitted if that block matched or if the line itself matches

#### Scenario: Input ends during a trace
- **WHEN** `--grep` is set and the stream ends while a trace is held
- **THEN** the held trace is matched and output if it matches

#### Scenario: Grep on a multi-line record that is not a trace
- **WHEN** a .NET record header did not match `--grep`, but its continuation line matches
- **THEN** the matching continuation line is admitted through the filter

#### Scenario: No grep means no holding
- **WHEN** `--grep` is not set and a process writes a stack trace
- **THEN** every line is output as soon as it arrives

### Requirement: ANSI Formatting and Non-TTY Plain Mode
The renderer SHALL colorize log levels, HTTP status codes, and keys using ANSI codes, render continuation frames in dimmed styling, preserve pre-existing SGR colors, and guarantee lossless output.

#### Scenario: Dimmed continuation frame rendering
- **WHEN** a line is marked as a continuation frame of an active trace
- **THEN** it is rendered in dim gray styling unless pre-colored

#### Scenario: Lossless text guarantee
- **WHEN** any line up to 64 KB is processed by the pipeline
- **THEN** stripping ANSI decoration from the rendered output produces the exact original raw input line

#### Scenario: Terminal ANSI output
- **WHEN** stdout is a terminal and colors are enabled
- **THEN** ERROR entries render in red, WARN in yellow, INFO in green/cyan, and DEBUG in gray

#### Scenario: Piped plain text output
- **WHEN** stdout is piped to another command or `--no-color` is set
- **THEN** plain text is emitted without ANSI escape characters

### Requirement: Filter Flag Validation
The system SHALL reject an unknown `--level` value with an error that lists the valid levels, before resolving the target. An empty value and every alias the severity parser recognizes SHALL be accepted. When `--grep` is not a valid regular expression, the system SHALL match it as literal text and print one notice to stderr saying so.

#### Scenario: Unknown level
- **WHEN** the user runs `specter 3000 --level banana`
- **THEN** Specter exits with an error listing the valid levels and does not attach

#### Scenario: Level alias
- **WHEN** the user runs `specter 3000 --level warning`
- **THEN** the value is accepted and treated as `warn`

#### Scenario: Invalid regular expression
- **WHEN** the user runs `specter 3000 --grep "[error"`
- **THEN** a notice is printed to stderr once, and lines containing the literal text `[error` are matched
