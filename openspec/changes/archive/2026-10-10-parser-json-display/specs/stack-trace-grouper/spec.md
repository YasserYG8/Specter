## ADDED Requirements

### Requirement: Multi-Line Stack Trace Grouping
The grouper SHALL detect consecutive lines belonging to an exception or error traceback and bind them into a single multi-line entry.

#### Scenario: Node.js error trace grouping
- **WHEN** an initial error line is followed by lines matching `^\s+at\s+.*`
- **THEN** all frames are grouped with the initiating error header into one multi-line entry

#### Scenario: Python traceback grouping
- **WHEN** a line starts with `Traceback (most recent call last):`
- **THEN** following indented file lines and the trailing exception name are grouped as a single entry

#### Scenario: Java exception grouping
- **WHEN** a line contains an exception and is followed by indented `at ...` or `Caused by:` lines
- **THEN** all cause chains and frames are grouped together

### Requirement: Idle Flush on Incomplete Stack Traces
The grouper SHALL flush any accumulated stack trace lines if no continuation line arrives within 200 milliseconds.

#### Scenario: Trace completed on stream silence
- **WHEN** a stack trace is accumulating and 200ms passes without a new line
- **THEN** the grouper flushes the accumulated group immediately
