## ADDED Requirements

### Requirement: Minimum Log Level Filtering
The filter SHALL discard log entries below the configured `--level` threshold (ordering: `DEBUG` < `INFO` < `WARN` < `ERROR`).

#### Scenario: Filtering by warn level
- **WHEN** `--level warn` is specified
- **THEN** `DEBUG` and `INFO` entries are suppressed while `WARN` and `ERROR` entries are preserved

#### Scenario: Preserving unclassified entries
- **WHEN** an entry has level `NONE` (unclassified plain text)
- **THEN** it is passed through so application messages are not lost

### Requirement: Atomic Multi-Line Filter Matching
The filter SHALL evaluate `--grep` patterns against the entire grouped entry so stack traces are never split.

#### Scenario: Grep matching within stack frame
- **WHEN** `--grep "database.go"` matches an inner stack trace frame
- **THEN** the entire multi-line entry including the error header is outputted

### Requirement: ANSI Formatting and Non-TTY Plain Mode
The renderer SHALL colorize log levels, timestamps, and keys using ANSI codes, and suppress all colors when piped or when `NO_COLOR` is present.

#### Scenario: Terminal ANSI output
- **WHEN** stdout is a terminal and colors are enabled
- **THEN** ERROR entries render in red, WARN in yellow, INFO in green/blue, and DEBUG in gray

#### Scenario: Piped plain text output
- **WHEN** stdout is piped to another command or `--no-color` is set
- **THEN** plain text is emitted without ANSI escape characters
