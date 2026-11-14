# json-formatter Specification

## Purpose
Formats structured JSON log records (from libraries like Winston, Pino, Zap, and Logrus) into readable single-line CLI output with colored keys, preserving raw JSON output when requested.

## Requirements

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

### Requirement: Numeric Timestamp Units and Range
The JSON formatter SHALL read a numeric timestamp in seconds, milliseconds, microseconds or nanoseconds since the Unix epoch, and SHALL choose the unit from the size of the absolute value: below `1e11` is seconds, below `1e14` is milliseconds, below `1e17` is microseconds, and anything larger is nanoseconds. A fraction of a second in a seconds value SHALL be kept. The formatter SHALL accept the value as a timestamp only when the resulting time falls between the start of the year 2000 and the end of the year 2099 (UTC). A numeric timestamp SHALL be shown as `HH:MM:SS.mmm` in local time.

#### Scenario: Seconds with a fraction
- **WHEN** a JSON line has `"ts":1789000000.5`
- **THEN** the timestamp is the local time of Unix second 1789000000 plus 500 ms, shown as `HH:MM:SS.500`

#### Scenario: Milliseconds
- **WHEN** a JSON line has `"time":1789000000123`
- **THEN** the timestamp is the local time of that millisecond, shown with `.123`

#### Scenario: Microseconds
- **WHEN** a JSON line has `"ts":1789000000123456`
- **THEN** the timestamp is the same moment as `1789000000123` milliseconds, shown with `.123`

#### Scenario: Nanoseconds
- **WHEN** a JSON line has `"ts":1789000000123456789`
- **THEN** the timestamp is the same moment as `1789000000123` milliseconds, shown with `.123`

#### Scenario: Number outside the range stays a field
- **WHEN** a JSON line has `"time":45` and no other timestamp key
- **THEN** the line has no timestamp, and `time=45` is shown among the extra fields

#### Scenario: Zero, negative and far-future numbers stay fields
- **WHEN** a JSON line's only timestamp key holds `0`, `-5`, or a value that maps to a year after 2099
- **THEN** the line has no timestamp, and the key is kept among the extra fields with its value

### Requirement: Timestamp Key Selection
The JSON formatter SHALL look for the timestamp in the keys `timestamp`, `time`, `ts` and `@timestamp`, in that order, and SHALL use the first key whose value is accepted. A non-empty string SHALL be accepted as written. A number SHALL be accepted only under the Numeric Timestamp Units and Range requirement. Any other value (an empty string, an object, an array, a boolean or null) SHALL NOT be accepted. Only the accepted key SHALL be removed from the extra fields. Every key that was not accepted SHALL stay among the extra fields.

#### Scenario: A duration in "time" does not hide the real timestamp
- **WHEN** a JSON line is `{"msg":"GET /users","time":45,"ts":1789000000123}`
- **THEN** the timestamp comes from `ts`, and `time=45` is shown among the extra fields

#### Scenario: First accepted key wins
- **WHEN** a JSON line has both `"timestamp":"2026-10-10T10:00:00Z"` and `"ts":1789000000123`
- **THEN** the timestamp comes from `timestamp`, and `ts` stays among the extra fields

#### Scenario: Value of another type stays a field
- **WHEN** a JSON line's only timestamp key is `"time":{"ms":12}` or `"time":""`
- **THEN** the line has no timestamp, and `time` is kept among the extra fields
