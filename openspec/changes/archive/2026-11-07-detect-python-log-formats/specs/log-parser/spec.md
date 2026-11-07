## MODIFIED Requirements

### Requirement: Log Severity Level Classification
The log parser SHALL inspect line tokens using confidence-ranked rules (Structured Key > Positional Prefix/Token > HTTP Status Hint > Weak Body Keyword) to classify severity into `DEBUG`, `INFO`, `WARN`, `ERROR`, or `NONE`.

#### Scenario: Structured JSON and logfmt classification
- **WHEN** a line contains a structured key `level=`, `lvl=`, `"level":`, `"lvl":`, or `"severity":`
- **THEN** the parser assigns the corresponding severity level based on the structured value

#### Scenario: Positional severity token detection
- **WHEN** a line starts with or has an immediate post-timestamp token like `[ERROR]`, `ERROR`, `local.ERROR:`, `| WARN |`, or `info: Category[1]`
- **THEN** the parser assigns the corresponding severity level without scanning arbitrary line body text

#### Scenario: HTTP access log status classification
- **WHEN** a line contains a recognized HTTP method (`GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `HEAD`, `OPTIONS`) and path (`/...`) with an adjacent or columnar status code
- **THEN** status codes 500-599 are classified as `ERROR`, 400-499 as `WARN`, and 200-399 as `INFO`

#### Scenario: No bare status false positives
- **WHEN** a line contains numbers like "took 404 ms" or "200 users" without an HTTP method and path
- **THEN** the parser does NOT classify the line as an HTTP log or assign a severity based on the number

#### Scenario: Timestamp with a space and a comma
- **WHEN** a line starts with `2026-11-07 10:12:50,516 WARNING [app] disk almost full` or `2026-11-07 10:12:50.516 INFO app: Starting`, with a space between date and time and a comma or dot before the fraction
- **THEN** the parser treats the date and time as one timestamp and assigns the level of the word after it (`WARN`, `INFO`)

#### Scenario: Python logging cookbook format
- **WHEN** a line matches `<timestamp> - <logger name> - <LEVEL> - <message>`, such as `2026-11-07 10:12:50,516 - TajweedPipeline - INFO - Starting pipeline processing`
- **THEN** the parser assigns `INFO`, and the line starts a new record

#### Scenario: Celery worker format
- **WHEN** a line starts with `[2026-11-07 10:12:50,516: ERROR/ForkPoolWorker-1]`
- **THEN** the parser assigns `ERROR`, and the line starts a new record

#### Scenario: Python level names
- **WHEN** a recognized level token is `WARNING` or `CRITICAL`
- **THEN** the parser assigns `WARN` or `ERROR`

#### Scenario: Level words inside the message are not levels
- **WHEN** a line is `Processing item 3 - INFO - not a level` with no timestamp before it, or `GET /api/ERROR-codes 200`
- **THEN** the dashed `INFO` and the `ERROR` in the path are not used as the line's level

#### Scenario: Formats that already work keep working
- **WHEN** a line is `INFO:app:Starting pipeline`, `[2026-11-07 10:12:50 +0000] [1234] [INFO] Booting worker`, or `2026-11-07 10:12:50.516 | WARNING  | app.main:run:43 - disk almost full`
- **THEN** the parser assigns `INFO`, `INFO` and `WARN`, as before
