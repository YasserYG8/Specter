## 1. Log Level and Format Parser

- [x] 1.1 Implement severity levels (`SeverityDebug`, `Info`, `Warn`, `Error`, `None`) and token parser in `internal/pipeline/parser.go`
- [x] 1.2 Implement HTTP status code classification (2xx, 4xx, 5xx) and ANSI escape preservation
- [x] 1.3 Add unit tests for level detection and token classification in `internal/pipeline/parser_test.go`

## 2. JSON Log Formatter

- [x] 2.1 Implement fast JSON object detection and field extractor (timestamp, level, message, metadata) in `internal/pipeline/json.go`
- [x] 2.2 Implement single-line structured formatter with colored keys and `--json` raw flag support
- [x] 2.3 Add unit tests with real JSON logs (Pino, Winston, Zap, logrus) in `internal/pipeline/json_test.go`

## 3. Stack Trace Grouper

- [x] 3.1 Implement pattern matchers for Node.js, Python, and Java stack frames in `internal/pipeline/grouper.go`
- [x] 3.2 Implement trace line accumulation into atomic `Entry` objects with 200ms idle flush
- [x] 3.3 Add unit tests for multi-line error grouping in `internal/pipeline/grouper_test.go`

## 4. Log Filter and ANSI Renderer

- [x] 4.1 Implement `--level` threshold filtering and atomic `--grep` matching in `internal/pipeline/filter.go`
- [x] 4.2 Implement ANSI terminal renderer in `internal/pipeline/renderer.go` respecting `NO_COLOR` and non-TTY pipes
- [x] 4.3 Add unit tests for filtering and ANSI rendering in `internal/pipeline/filter_test.go` and `internal/pipeline/renderer_test.go`

## 5. CLI Wiring and Integration

- [x] 5.1 Wire Grouper, Parser, Filter, and Renderer stages into `cmd/specter/main.go`
- [x] 5.2 Validate end-to-end formatted output and filtering against test servers
