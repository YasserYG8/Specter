## MODIFIED Requirements

### Requirement: Atomic Multi-Line Filter Matching
The filter SHALL evaluate `--grep` patterns in real time across the streaming state machine such that continuation lines inherit their parent record's match state, and matching continuation frames print even if their parent header was unselected.

#### Scenario: Continuation frames inherit header grep match
- **WHEN** a record header matches `--grep`
- **THEN** all subsequent continuation frames for that record are admitted through the filter

#### Scenario: Grep matching on continuation frame
- **WHEN** a record header did not match `--grep`, but a subsequent continuation frame matches `--grep`
- **THEN** the matching continuation frame is admitted through the filter and styled as a dimmed frame

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
