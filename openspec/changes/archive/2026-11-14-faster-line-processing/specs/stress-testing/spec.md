## ADDED Requirements

### Requirement: Line Processing Does Not Repeat Work
The pipeline SHALL NOT repeat per-line work that cannot change its result. The ANSI helpers (`StripANSI`, `SanitizeTerminalOutput`, `HasANSI`) SHALL return without running a regular expression and without allocating when the line contains neither an ESC byte (`0x1b`) nor a BEL byte (`0x07`). A line that passes through the grouper SHALL be decoded as JSON at most once on its way to the renderer, and the grouper's record decision, `IsJSON` and the renderer SHALL reuse that one result.

#### Scenario: Line without escape codes
- **WHEN** `StripANSI`, `SanitizeTerminalOutput` or `HasANSI` is called on a line with no ESC and no BEL byte
- **THEN** the call returns the line unchanged (or `false` for `HasANSI`) and allocates nothing, as measured by `testing.AllocsPerRun`

#### Scenario: Lone BEL is still removed
- **WHEN** a line contains a BEL byte and no ESC byte
- **THEN** `SanitizeTerminalOutput` and `StripANSI` remove the BEL exactly as before

#### Scenario: JSON line decoded once
- **WHEN** a JSON log line passes through the grouper and is rendered
- **THEN** the line is decoded as JSON once, and the renderer formats it from the result carried on the `Entry`

#### Scenario: Entry built outside the grouper
- **WHEN** the renderer receives an `Entry` that carries no JSON decision, such as one built directly in a test
- **THEN** the renderer decodes the text itself and renders it exactly as before

### Requirement: Fast Paths Give Identical Output
Every shortcut added to skip per-line work SHALL produce the same result as the full path for every input. Fuzz tests SHALL compare each fast path with the full path it replaces: the ANSI helpers against their regular expressions, and the rendered output of a line through the grouper and renderer against the output of the full decoding path. A JSON line's severity SHALL still come from `DetectSeverity`, so that lines whose JSON level and text level differ (such as Pino numeric levels) keep today's severity.

#### Scenario: Fuzzing the ANSI helpers
- **WHEN** the fuzz tests run on random strings seeded with ESC, BEL, `\x1b[`, `\x1b]` and unterminated sequences
- **THEN** `StripANSI`, `SanitizeTerminalOutput` and `HasANSI` return the same value as their regular expressions for every input

#### Scenario: Fuzzing rendered lines
- **WHEN** the fuzz test renders random lines, including JSON objects, colored JSON, Python tracebacks and Go panics, with and without color and in `--raw` and `--json` modes
- **THEN** the rendered text and the entry's severity, trace and record flags are the same as on the full decoding path

#### Scenario: Pino numeric level keeps its severity
- **WHEN** a JSON line has `"level":50` (a Pino numeric level)
- **THEN** the entry's severity is the one `DetectSeverity` gives, as before this change
