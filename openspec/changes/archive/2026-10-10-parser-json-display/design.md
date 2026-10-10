## Context

Specter streams raw output lines from target servers in real time. In Phase 3, we build the transformation and presentation layer that sits between the `LineAssembler` and the terminal `os.Stdout`.

The pipeline transforms lines through:
`Assembler Lines` ➔ `Grouper (Multi-line Traces)` ➔ `Parser & JSON Formatter` ➔ `Filter (--level, --grep)` ➔ `Renderer (ANSI)`

## Goals / Non-Goals

**Goals:**
- Detect and format structured JSON log lines on a single readable line with syntax-colored keys.
- Tag log severity levels (`DEBUG`, `INFO`, `WARN`, `ERROR`) and HTTP status codes (2xx, 4xx, 5xx).
- Heuristically group multi-line stack traces (Node.js, Python, Java) into atomic `Entry` objects so filters do not break error traces apart.
- Implement `--level` threshold filtering and `--grep` regex/substring filtering.
- Implement ANSI terminal renderer that respects `NO_COLOR`, `--no-color`, and non-TTY pipes (`specter 3000 | grep 500`).

**Non-Goals:**
- Log aggregation, storage, or indexing (Specter remains purely a live stream).
- Arbitrary custom formatting templates or jq-like query syntax (deferred).
- Docker or systemd journal sources (Phase 2).

## Decisions

### 1. Atomic `Entry` Model for Log Records
- **Choice**: Represent each output item as an `Entry` struct containing `Lines []string`, `Level Severity`, `IsJSON bool`, `IsTrace bool`, and `TS time.Time`.
- **Rationale**: If we processed lines one by one, a filter like `--grep "UserNotFound"` would only print the line containing that string, dropping the 15 lines of stack trace following it. Modeling records as `Entry` keeps the error header and frames together.
- **Alternatives Considered**: Line-by-line filtering with lookahead buffers — dropped because state machines over single lines get brittle when streams interleave.

### 2. Single-Line JSON Representation
- **Choice**: Parse JSON objects and format them into:
  `[timestamp] [LEVEL] Message key1=value1 key2=value2`
- **Rationale**: Multiline JSON objects (pretty-printed JSON) blow up the terminal screen and make scrolling unusable for fast-moving logs. Single-line summaries with colored keys give the developer instant readability while keeping screen density high.
- **Alternatives Considered**: Full multi-line pretty-printed JSON — rejected as default because it clutters the terminal. Can be supported in raw mode via `--json` or `--raw`.

### 3. Non-Destructive ANSI Passthrough
- **Choice**: If an application output line already contains ANSI escape codes (e.g. from NestJS, Express, chalk, or colorama), Specter preserves the existing ANSI codes without wrapping them in conflicting colors.

## Risks / Trade-offs

- **[Risk] High-throughput JSON parsing overhead**:
  - *Mitigation*: Fast check `line[0] == '{' && line[len-1] == '}'` before attempting `json.Unmarshal`. Avoid allocating full reflective structs by decoding into lightweight map or searching top-level keys.
- **[Risk] Unfinished stack trace hanging indefinitely**:
  - *Mitigation*: Add an idle flush timer (200ms) to the stack trace grouper so that if a traceback header is emitted without continuation frames, it flushes promptly.
