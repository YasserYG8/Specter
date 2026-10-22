## Context

Specter attaches to live server processes via `ptrace` (strace engine) or `/proc/<pid>/fd` tailing, capturing raw stdout and stderr byte chunks. The current pipeline assembles these chunks into lines and passes them to `StackTraceGrouper`. The grouper buffers lines in a map while awaiting a 200ms idle timeout to flush complete stack traces.

This model violates real-time guarantees, delays error output, and relies on brittle regex patterns that fail across non-Node/non-Python frameworks (e.g. Laravel `#0 ...`, Gin, Next.js, Go goroutines, .NET). Furthermore, bare status code matching causes false positives, unbounded lines risk terminal freezing, and unstripped terminal control codes (OSC, CSI cursor moves) introduce terminal escape injection risks.

## Goals / Non-Goals

**Goals:**
- **Zero Buffering / Real-Time**: Emit every line to stdout immediately upon newline assembly. Never wait for an idle timeout to flush errors or traces.
- **Lossless Stream Guarantee**: No line is ever dropped, reordered, or modified in content. Unclassified lines print unchanged. Property test: `strip_decoration(output) == input` for all lines $\le$ 64 KB.
- **Graceful Degradation**: Treat all pattern matching as heuristics. Misclassifications degrade safely to plain uncolored text, never dropped or hidden.
- **Streaming Record / Continuation Machine**: Group multi-line events (Node, Python, Java, Go, .NET) via stateful context inheritance per (PID, FD). Continuation lines inherit parent level and filter status, and render dimmed.
- **Confidence-Ranked Severity**: Rank detection (1. Structured JSON/logfmt key $\rightarrow$ 2. Positional prefix/channel token $\rightarrow$ 3. Weak body keyword). No arbitrary benign word strip lists and no bare `\berror\b` scans across arbitrary body text.
- **Bounded Safety**: Cap line length at 64 KB with a visible truncation marker (`... [specter: truncated at 64KB]`). Sanitize dangerous control characters (OSC, CSI cursor movements, BEL) while preserving SGR color sequences.
- **Strict Guardrails**: Protect indented SQL queries and numbered lists (`#1 download assets`) from false trace continuation.

**Non-Goals:**
- Interactive folding or collapsing of stack traces in terminal output (stdout is a sequential stream).
- Offline historical log indexing or schema enforcement.
- Automated framework profile sniffing (deferred to post-MVP).
- Custom user rule file (`~/.config/specter/rules.toml`) parsing (deferred to post-MVP).

## Architecture & State Machine

```
   Raw Chunk Stream
          │
          ▼
┌────────────────────────────────────────┐
│         Line Assembler                 │ (Splits on \n, enforces 64 KB cap)
└─────────────────┬──────────────────────┘
                  │ Line {Raw, Stripped, Safe}
                  ▼
┌────────────────────────────────────────┐
│   Record / Continuation State Machine  │ (Tracks active record per PID/FD)
│                                        │
│   if isRecordStart(line):              │
│       rec = classify(line)             │
│   else if rec && isContinuation(line): │
│       inherit rec (dimmed frame)       │
│   else:                                │
│       rec = none (plain pass-through)  │
└─────────────────┬──────────────────────┘
                  │ Stream Entry (Line + Rec Metadata)
                  ▼
┌────────────────────────────────────────┐
│           Streaming Filter             │ (Evaluates --level, --grep, --stderr)
└─────────────────┬──────────────────────┘
                  │ Passed Line
                  ▼
┌────────────────────────────────────────┐
│          Streaming Renderer            │ (SGR color, HTTP tints, dimmed frames)
└─────────────────┬──────────────────────┘
                  │
                  ▼
           stdout (Terminal)
```

### 1. Record / Continuation State Machine

For each independent stream `(pid, fd)`, Specter maintains an active record state:

```go
type StreamRecord struct {
    Level       Severity
    Kind        RecordKind // KindHTTP, KindTrace, KindStructured, KindPlain
    MatchedGrep bool       // Whether the record header matched --grep
    InTrace     bool       // True if currently inside a stack trace / exception block
    TraceType   TraceType  // TracePython, TraceGo, TraceJava, TraceNode, TraceGeneric
    Alternating bool       // For Go goroutines: tracks alternating func / file:line lines
}
```

#### Evaluation Rules:
1. **`isRecordStart(line)`**:
   - Structured JSON object `^\s*\{.*"level":...`.
   - Timestamp followed by token: ISO8601 (`2026-10-18T...`), date-time (`2026-10-18 12:00:00`), Syslog, or bracketed timestamp `[2026-10-18 ...]`.
   - Positional level token or framework tag at line start: `[GIN]`, `[ERROR]`, `ERROR:`, `local.ERROR:`, `info: Category[1]`.
   - HTTP request access line: `METHOD /path STATUS` or `STATUS ... METHOD /path`.
   - Root trace header: `Traceback (most recent call last):`, `panic: ...`, `goroutine \d+ \[...`, `\b(?:[a-zA-Z0-9_.]+\.)?[A-Z][a-zA-Z0-9_]*(?:Error|Exception|Panic|Fatal):`.
2. **`isContinuation(line, rec)`**:
   - **Java / JVM**: Indented `^\s+at\s+.*`, `^\s*Caused by:.*`, `^\s*\.\.\.\s+\d+\s+more`.
   - **Python**: Indented `^\s+File\s+"[^"]+",\s+line\s+\d+`, `^\s+(?:[A-Za-z0-9_]+\s*=.*|[a-zA-Z_]\w*\(.*\))`. When in a Python trace, the first subsequent unindented line matching `^(?:[A-Z]\w*(?:Error|Exception|Warning)|During handling of the above exception|The above exception was the direct cause)` continues and either chains or closes the trace.
   - **Node.js**: Indented `^\s+at\s+(?:async\s+)?[^\s(]+(?:\s*\(.*\))?`.
   - **Go Goroutines**: Alternates unindented symbol `created by main.main` and tab-indented file location `\t/app/main.go:42 +0x1a`. Blank lines inside an active goroutine dump continue the trace record.
   - **PHP / Laravel / Symfony / GDB**: Numbered stack frames `^#\d+\s+` containing file paths or class methods.
   - **.NET 2-Line Records**: An indented line immediately following an `info: Category[1]` record header is treated as the message continuation of the record (inheriting `INFO`), NOT as a stack frame.
3. **Continuation Guardrails**:
   - An indented line (spaces or tabs) is only treated as a trace continuation if `rec.InTrace == true` AND it contains code references (`at `, `File "`, `.go:`, `.java:`, `(#\d+)`, `in `).
   - Indented raw SQL statements (`SELECT ... \n FROM ...`) or numbered build lists (`#1 download assets`) occurring outside an active trace header transition `rec = none` and emit as plain text.

---

### 2. Severity: Ranked by Confidence (First Match Wins)

Severity classification follows strict hierarchical precedence. First match wins:

```
┌─────────────────────────────────────────────────────────────┐
│ 1. Structured Key (Confidence: Highest)                     │
│    - JSON: "level", "lvl", "severity"                       │
│    - Logfmt: level=error, lvl=warn, severity=info           │
├─────────────────────────────────────────────────────────────┤
│ 2. Positional Prefix / Token (Confidence: High)             │
│    - First token after timestamp or at start of line:       │
│      [ERROR], [WARN ], [INFO ], [DEBUG]                     │
│      ERROR, WARN, WARNING, INFO, DEBUG, TRACE               │
│      local.ERROR:, app.CRITICAL: (Monolog / Laravel)        │
│      | ERROR | (Columnar loggers)                           │
│      info: Category[1] (.NET Kestrel)                       │
├─────────────────────────────────────────────────────────────┤
│ 3. HTTP Status Hint (Confidence: Medium)                    │
│    - 500-599 -> ERROR                                       │
│    - 400-499 -> WARN                                        │
│    - 200-399 -> INFO                                        │
├─────────────────────────────────────────────────────────────┤
│ 4. Weak Body Keyword (Confidence: Lowest, Last Resort)      │
│    - Uncaught exception anchors: Exception:, Fatal:         │
│    - Never scans bare \berror\b anywhere in line            │
└─────────────────────────────────────────────────────────────┘
```

No benign word lists (`0 errors`, `without error`) are used. By eliminating unbounded keyword searches (`\berror\b`), benign phrases naturally produce `SeverityNone` without special exceptions.

---

### 3. HTTP Access Log Detection

To eliminate false matches on arbitrary numbers ("took 404 ms", "200 users"):
- **Rule**: Require both a recognized HTTP Method (`GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS`) AND a URI Path (starting with `/`), with the HTTP Status Code (`100-599`) either adjacent or in a recognized columnar slot.

Supported Formats:
- **Quoted Combined / Common**: `"GET /api/v1/users HTTP/1.1" 200 1024` (uvicorn, nginx, apache)
- **Express Morgan Dev**: `GET /api/v1/users 200 12.34 ms - 1024`
- **Gin**: `[GIN] 2026/10/18 - 12:00:00 | 200 | 1.2ms | 127.0.0.1 | GET "/users"`
- **Next.js**: `GET /dashboard 200 in 14ms`
- **Spring Boot**: `o.s.w.s.DispatcherServlet : Completed 200 OK`

---

### 4. ANSI & Terminal Safety

1. **Dual Representation**:
   - `line.Raw`: unmodified byte representation from source.
   - `line.Stripped`: stripped of ANSI escape codes for regex matching.
   - `line.Safe`: sanitized for terminal emission.
2. **Terminal Injection Protection**:
   - Permitted: SGR sequences (`\x1b[[0-9;]*m`) for foreground/background colors and styles.
   - Stripped: OSC sequences (`\x1b]...`), CSI cursor movements (`\x1b[H`, `\x1b[2J`), alternate screen buffer switches, and BEL (`\x07`).
3. **Bounded Line Length**:
   - Maximum line length capped at 64 KB (65,536 bytes).
   - If a line exceeds 64 KB, it is truncated at 64 KB and suffixed with ` ... [specter: truncated at 64KB]\n`.

---

### 5. Streaming Grep Semantics Under Zero Buffering

Under zero-buffering real-time streaming:
- When a record starts, it is evaluated against `--grep`. If it matches, `rec.MatchedGrep = true`.
- If `rec.MatchedGrep == true`, subsequent continuation frames inherit the match and print (dimmed).
- If the record header did NOT match `--grep`, but an individual continuation frame matches `--grep`, that frame prints (dimmed), ensuring users searching for a specific stack trace file or function (e.g. `--grep OrderService`) see the matching frame.

---

### 6. Explicit Limitations: What This Design Cannot Detect

No heuristic system can be 100% omniscient without language runtimes. We explicitly document what this engine will NOT detect:
1. **Unmarked Custom Crash Formats**: An application that prints a crash message with no error keyword, no stack trace, no exception suffix, and no standard level token (e.g. `something went bad today`) will be emitted as plain text.
2. **Tabular/Columnar Logs Without Level Words**: In-house formats that encode log levels as numbers (e.g. `pri=3`) without standard words or logfmt keys.
3. **Mixed Stdout/Stderr Concurrency Collisions**: If a single process concurrently writes stdout and stderr from different threads without newlines, lines interleaved at the syscall chunk boundary may split across streams.
4. **Retroactive Header Retrieval on Late Grep Matches**: If a record header did not match `--grep` and was suppressed, a subsequent matching continuation frame cannot retroactively print the suppressed header because it has already passed out of the pipeline without buffering.
