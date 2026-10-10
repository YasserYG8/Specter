## ADDED Requirements

### Requirement: Structured JSON Log Detection and Extraction
The JSON formatter SHALL detect lines that represent valid JSON objects and extract standard log fields (level, message, timestamp, extra keys).

#### Scenario: Standard structured JSON line detected
- **WHEN** a line parses as a valid JSON object containing keys like `"level"`, `"message"` or `"msg"`, and `"time"`
- **THEN** the formatter extracts these canonical fields and stores extra attributes separately

#### Scenario: Non-JSON line ignored
- **WHEN** a line does not parse as a JSON object
- **THEN** the formatter returns the raw text untouched

### Requirement: Single-Line Pretty Printing
The JSON formatter SHALL render structured JSON records onto a compact single line with colorized keys and values.

#### Scenario: Formatted single-line rendering
- **WHEN** a structured JSON log is formatted
- **THEN** it renders in the format `TIMESTAMP [LEVEL] MESSAGE key1=val1 key2=val2`

#### Scenario: Force raw JSON output via flag
- **WHEN** the `--json` flag is supplied by the user
- **THEN** the JSON line is emitted in raw unformatted JSON without syntax modification
