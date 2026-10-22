## Why

Specter currently relies on rigid framework-specific regular expressions for HTTP access logs and buffers multi-line stack traces in an accumulator with a 200ms idle timeout. This approach has critical flaws:
1. **Latency & Dropped Stream Semantics**: Waiting for an idle timeout delays terminal output and violates the real-time contract of a live streaming CLI.
2. **Fragility Across Frameworks**: Unquoted HTTP access logs (morgan dev, Next.js, Gin, Laravel, Spring Boot) fail detection, while stack traces in languages like PHP (`#0 ...`), .NET (`info: Category[1]`), Go goroutine dumps, and chained exceptions (`Caused by:`, `During handling of...`) either fragment or fail to group.
3. **Safety Vulnerabilities**: Bare status matching hits false positives ("took 404 ms"), unbounded lines (>64 KB) can hang terminals, and unstripped terminal control codes (OSC, CSI cursor moves) expose users to escape sequence injection attacks from untrusted log payloads.

Moving to a streaming record/continuation state machine with confidence-ranked severity heuristics and bounded line safety ensures Specter works reliably across all servers in the wild without buffering, dropping, or reordering any lines.

## What Changes

- **Stream State Machine (Zero Buffering)**: Replace the `buffer + flush` grouper with a per-(PID, FD) streaming state machine. Every line is classified and emitted immediately to the terminal. Continuation lines inherit the parent record's level and filter match fate and render dimmed.
- **Confidence-Ranked Severity**: Rank severity extraction by confidence (1. Structured JSON/logfmt keys -> 2. Positional tokens following timestamp/prefix -> 3. Body keywords). Eliminate brittle "strip benign words" logic and eliminate matching `\berror\b` anywhere in the line.
- **Robust HTTP Heuristics**: Require both HTTP method and request path, with adjacent or columnar status code. Prevent bare status code false positives. Support morgan dev, Gin, Laravel/Monolog, uvicorn, Next.js, Spring Boot, and Apache/Nginx formats.
- **Terminal & Memory Safety Bounds**: Cap line processing and terminal output at 64 KB (appending a visible truncation indicator). Pass through SGR color sequences while stripping dangerous non-SGR sequences (OSC, CSI cursor manipulation, BEL).
- **Continuation Grammar Support**: Handle Python chained tracebacks (`During handling of...` and trailing exception line), Go goroutine dumps with alternating frames and blank lines, Java `Caused by:` / `... N common frames omitted`, and .NET 2-line records.
- **Strict Guardrails**: Indented lines and numbered frames (`#N`) only continue a trace record if anchored by a valid trace header or if containing file/line references (protecting indented SQL and numbered asset lists).
- **Lossless Property Verification**: Add automated property tests asserting that for all lines up to 64 KB, output with ANSI decoration stripped strictly equals raw input.

## Capabilities

### New Capabilities
- None. (The work refactors existing log ingestion, grouping, filtering, and rendering pipelines into a robust heuristic engine).

### Modified Capabilities
- `log-parser`: Replace rigid regexes with confidence-ranked severity extraction, robust HTTP method+path+status detection, 64 KB line length capping with truncation markers, and non-SGR terminal escape sequence sanitization.
- `stack-trace-grouper`: Replace the 200ms idle-timeout buffering accumulator with a real-time, zero-buffering record/continuation streaming state machine where continuation lines immediately inherit record metadata and filter fate.
- `log-filter-renderer`: Update filtering so continuation lines inherit record level and grep match status, and update renderer to stream continuation frames dimmed without needing multi-line buffered slice entries.

## Impact

- **Affected Code**: `internal/pipeline/parser.go`, `internal/pipeline/grouper.go`, `internal/pipeline/filter.go`, `internal/pipeline/renderer.go`, `cmd/specter/main.go`.
- **Breaking Changes**: None to CLI flags or end-user behavior. Users will observe instant line streaming (no 200ms idle lag on errors) and significantly improved syntax highlighting across modern web frameworks.
- **Dependencies**: No external runtime dependencies added.

## Out of Scope

- Profile sniffing (auto-detecting specific frameworks after 20 lines) is deferred to a follow-up enhancement.
- User-defined rule overrides via `~/.config/specter/rules.toml` is deferred to a follow-up enhancement.
- Dynamic terminal line folding or collapsible TUI widgets (Specter streams directly to stdout).
