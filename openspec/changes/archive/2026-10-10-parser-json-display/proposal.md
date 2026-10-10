## Why

Raw server output is often noisy, unformatted, and hard to read in real time. Backend applications emit structured JSON logs, HTTP access logs, and multiline stack traces that turn into wall-of-text dumps without syntax coloring or grouping. Specter needs an intelligent parsing and rendering pipeline to highlight log levels, pretty-print JSON on a single line, keep stack traces intact during filtering, and allow filtering by minimum log level.

## What Changes

- Implement log level classification (`DEBUG`, `INFO`, `WARN`, `ERROR`) and HTTP response code detection (2xx green, 4xx yellow, 5xx red) in `internal/pipeline/parser.go`.
- Implement JSON log detector and formatter that pretty-prints structured JSON records (extracting timestamp, level, message, and extra keys) on a single readable line, while honoring the `--json` flag for raw output.
- Implement stack trace grouping for Python, Node.js, and Java error traces to prevent multi-line errors from being fractured by filtering.
- Implement minimum log level filtering (`--level debug|info|warn|error`) in `internal/pipeline/filter.go`.
- Implement the ANSI terminal renderer in `internal/pipeline/renderer.go` that respects `NO_COLOR` and non-TTY stdout pipes.
- Wire parser, filter, and renderer into `cmd/specter/main.go`.

## Capabilities

### New Capabilities
- `log-parser`: Classify log line severity levels, HTTP status codes, and detect structured logging formats.
- `json-formatter`: Parse and format structured JSON log lines with syntax-colored keys and values on a single line.
- `stack-trace-grouper`: Heuristically detect and group multi-line stack traces (Node.js, Python, Java) into single logical entries.
- `log-filter-renderer`: Filter logical log entries by level and regex/substring, and render them with ANSI colors.

### Modified Capabilities
<!-- None: this builds on top of the raw stream assembled in Phase 1 -->

## Impact

- **New Files**:
  - `internal/pipeline/parser.go` and `parser_test.go`
  - `internal/pipeline/json.go` and `json_test.go`
  - `internal/pipeline/grouper.go` and `grouper_test.go`
  - `internal/pipeline/filter.go` and `filter_test.go`
  - `internal/pipeline/renderer.go` and `renderer_test.go`
- **CLI**: `cmd/specter/main.go` will route assembled lines through Grouper -> Parser -> Filter -> Renderer before writing to stdout.
