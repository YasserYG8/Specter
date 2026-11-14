# stress-testing Specification

## Purpose
TBD - created by archiving change e2e-integration-stress. Update Purpose after archive.
## Requirements
### Requirement: High-Throughput Log Reassembly Under Burst Load
The stress test harness SHALL verify that the pipeline accurately reassembles and processes high-throughput log bursts without dropping lines or deadlocking.

#### Scenario: Processing burst of 10,000 log lines
- **WHEN** 10,000 log lines are emitted rapidly into the pipeline
- **THEN** all 10,000 lines are reconstructed, processed, and rendered without data corruption or lost messages

### Requirement: Multi-Threaded Interleaved Stream Concurrency
The stress test harness SHALL verify concurrent pipeline safety across interleaved file descriptors and multi-process threads under race conditions.

#### Scenario: High concurrency with multiple emitting threads
- **WHEN** multiple concurrent workers emit interleaved logs across stdout and stderr simultaneously
- **THEN** the pipeline separates streams without cross-thread interleaving corruption and passes race detection

### Requirement: Channel Buffer Stability and Bounded Resource Usage
The stress test harness SHALL ensure channel buffers handle backpressure and do not leak goroutines post-shutdown.

#### Scenario: Resource teardown after burst completion
- **WHEN** a high-throughput burst completes and context is closed
- **THEN** all pipeline goroutines terminate cleanly within timeout and memory remains bounded


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
