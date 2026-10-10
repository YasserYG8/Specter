# log-parser Specification

## Purpose
Classifies log line severity levels (DEBUG, INFO, WARN, ERROR, NONE) and HTTP status codes, stripping or preserving existing ANSI sequences.

## Requirements

### Requirement: Log Severity Level Classification
The log parser SHALL inspect line prefixes and tokens to classify severity levels into `DEBUG`, `INFO`, `WARN`, `ERROR`, or `NONE`.

#### Scenario: Standard severity token detection
- **WHEN** a line starts with or contains prominent tokens like `[ERROR]`, `WARN:`, or `level=info`
- **THEN** the parser assigns the corresponding severity level to the parsed entry
 
#### Scenario: HTTP status code classification
- **WHEN** a line matches an HTTP access log format with a status code
- **THEN** status codes 500-599 are classified as `ERROR`, 400-499 as `WARN`, and 200-399 as `INFO`

### Requirement: ANSI Escape Code Preservation
The log parser SHALL detect existing ANSI escape codes in the raw line and pass them through without double-coloring.

#### Scenario: Pre-colored line pass-through
- **WHEN** an application writes output that already contains ANSI color escapes (`\x1b[31m`)
- **THEN** the parser flags the line as pre-colored and retains the original color sequences
