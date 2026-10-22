## 1. Real-World Test Fixtures

- [x] 1.1 Create fixture files in `test/fixtures/` capturing Python chained exceptions (`During handling of the above exception...` and trailing exception)
- [x] 1.2 Create fixture files capturing Go panic with goroutine dumps and alternating function/file lines
- [x] 1.3 Create fixture files capturing Java stack traces with `Caused by:` and `... N common frames omitted`
- [x] 1.4 Create fixture files capturing .NET Kestrel two-line records (`info: Category[1]` + indented message)
- [x] 1.5 Create fixture files capturing HTTP access logs across morgan dev, Next.js, Gin, uvicorn, and Spring Boot
- [x] 1.6 Create edge-case fixture files: indented SQL queries, `#1 download assets` numbered lists, lines exceeding 64 KB, and ANSI/OSC terminal injection payloads

## 2. Record / Continuation Streaming State Machine

- [x] 2.1 Refactor `StackTraceGrouper` in `internal/pipeline/grouper.go` into a zero-buffering streaming state machine
- [x] 2.2 Implement per-(PID, FD) stream context tracking (`StreamRecord`) with immediate line emission
- [x] 2.3 Implement `isRecordStart` detecting JSON objects, ISO/bracketed timestamps, positional level tokens, HTTP requests, and trace root headers
- [x] 2.4 Implement `isContinuation` handling Node `at `, Python indent + trailing exception, Java `Caused by:`, Go goroutines, and PHP `#N` frames
- [x] 2.5 Implement guardrails ensuring indented SQL statements and numbered lists outside trace headers remain unclassified plain lines
- [x] 2.6 Wire streaming state machine in `cmd/specter/main.go` removing any idle timeout buffer wait

## 3. Confidence-Ranked Severity Parser

- [x] 3.1 Update `DetectSeverity` in `internal/pipeline/parser.go` to use confidence-ranked evaluation order
- [x] 3.2 Implement Rank 1: Structured key extraction for JSON (`level`, `lvl`, `severity`) and logfmt (`level=`, `lvl=`)
- [x] 3.3 Implement Rank 2: Positional level token detection immediately following timestamps or line-start (`[ERROR]`, `local.ERROR:`, `| WARN |`, etc.)
- [x] 3.4 Implement Rank 3: Weak body keyword check (last resort, uncaught exception headers only) without bare `\berror\b` matching
- [x] 3.5 Remove brittle benign word strip regexes (`0 errors`, `without error`)

## 4. Robust HTTP Heuristics & Terminal Safety

- [x] 4.1 Implement HTTP access heuristic requiring both method (`GET|POST|...`) and path (`/...`) with adjacent or columnar status code (100-599)
- [x] 4.2 Add status mapping (5xx ERROR, 4xx WARN, 2xx/3xx INFO) as a secondary hint when no higher-priority level token exists
- [x] 4.3 Add support for unquoted morgan dev, Next.js (`GET /x 200 in 12ms`), Gin columnar, and Spring Boot `Completed 200 OK`
- [x] 4.4 Implement 64 KB line length cap with `... [specter: truncated at 64KB]` suffix
- [x] 4.5 Implement non-SGR terminal sequence sanitizer (stripping OSC, CSI cursor movements, and BEL while preserving SGR color codes)

## 5. Streaming Renderer & Lossless Property Test

- [x] 5.1 Update `Renderer` in `internal/pipeline/renderer.go` to render continuation frames in dim gray (`HiBlackString`)
- [x] 5.2 Pass through pre-colored SGR lines without re-coloring
- [x] 5.3 Update `Filter` in `internal/pipeline/filter.go` so continuation lines inherit parent record level and grep match status
- [x] 5.4 Allow matching continuation frames to print dimmed even when their parent header did not match `--grep`
- [x] 5.5 Add automated property test in `internal/pipeline/property_test.go` verifying `strip_ansi(rendered) == raw_input` for all lines $\le$ 64 KB

## 6. Rules-as-Data Foundation & Table Architecture

- [x] 6.1 Organize built-in regex and heuristic definitions into structured pattern tables in `internal/pipeline/rules.go`
- [x] 6.2 Precompile all regexes and verify absence of catastrophic backtracking or nested quantifiers
- [x] 6.3 Document data structures and extensibility points for follow-up user config (`~/.config/specter/rules.toml`) and profile sniffing
