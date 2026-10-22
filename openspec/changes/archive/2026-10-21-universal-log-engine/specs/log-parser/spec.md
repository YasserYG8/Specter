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

### Requirement: ANSI Escape Code Preservation and Terminal Safety
The log parser SHALL preserve standard SGR ANSI color sequences, sanitize non-SGR terminal control sequences, and enforce a 64 KB line length limit.

#### Scenario: Pre-colored line pass-through
- **WHEN** an application writes output that already contains ANSI color escapes (`\x1b[31m`)
- **THEN** the parser flags the line as pre-colored and retains the original color sequences

#### Scenario: Non-SGR terminal control sanitization
- **WHEN** a log line contains non-SGR sequences like OSC window title escapes, CSI cursor manipulation, or terminal bells (`\x07`)
- **THEN** the parser strips the non-SGR sequences to prevent terminal escape injection

#### Scenario: Bounded line length enforcement
- **WHEN** a raw log line exceeds 65,536 bytes (64 KB)
- **THEN** the line is capped at 64 KB with a visible truncation marker appended
