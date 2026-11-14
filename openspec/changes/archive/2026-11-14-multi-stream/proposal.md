## Why

Specter follows one server at a time. A web app usually has at least two (a frontend dev server and an API), so a developer still needs two terminals, and has to match the frontend's 500 with the backend's error by eye. `specter 3000 4000` should show both streams in one place, each line marked with where it came from. This is also the base for later work that needs more than one target per run, such as `--follow`.

## What Changes

- Specter accepts several targets: several positional arguments (`specter 3000 4000 web`), several `--pid` flags, or both mixed. Each argument is resolved by today's rules (a number up to 65535 is a port, a larger one is a PID, anything else is a container or a project). The limit is 8 targets.
- Every target gets its own capture session: its own source (chosen by today's selector, so one target can use eBPF and another Docker), line assembler and stack-trace grouper. The sessions' entries meet in one shared filter and renderer.
- With two or more targets, every output line starts with a tag naming its target (`:3000 | `, `web | `, `pid:1234 | `), padded to one width and colored per target on a terminal. With one target the output is exactly as today: no tag.
- Start is all or nothing: if any target cannot be resolved or its source cannot start, Specter stops what it already started and exits with an error, before printing any log line.
- When one target ends, the others keep streaming. Specter prints how that target ended, tagged, and exits when every target has ended or the user stops it. The exit status is 1 if any target ended by itself.
- Targets that are the same process (one server on two ports, or a PID that is a worker of another target) are followed once, with a notice.
- A partial line (such as a prompt) from one target is closed with a line break before another target writes, so lines from two targets never share a row.
- Status messages on stderr (attach line, source, notices, end report) name their target when there are several.
- JSON timestamps written as text with a time zone (`2026-10-10T10:00:00.123Z`, `…+02:00`) are shown in local time, as numeric timestamps already are, so two servers show the same moment at the same clock time. This applies with one target too.
- **BREAKING (small):** `specter 8080 --pid 3000` and `specter --pid 3000 --pid 4000` used to be refused; now they attach to both. A JSON timestamp like `…T10:00:00Z` used to be shown as `10:00:00` and is now shown in local time with milliseconds (for example `12:00:00.000` in UTC+2).

## Capabilities

### New Capabilities

- `multi-stream`: following several targets in one run: target list and limit, duplicate targets, all-or-nothing start, per-target sessions, tagged output, partial lines, per-target status messages, and how the run ends.

### Modified Capabilities

- `target-arguments`: `--pid` may be repeated and combined with positional targets.
- `json-formatter`: string timestamps with a time zone are shown in local time.

## Impact

- Code: `cmd/specter/main.go` is split. A new `Session` type (in `cmd/specter`) holds what `RunE` does today for one target, from source selection to the grouper. `RunE` resolves the targets, starts the sessions, and runs one output loop. `end.go` reports per target. `internal/pipeline/json.go` formats zoned timestamps.
- CLI: `Args` accepts several targets, help text and examples show `specter 3000 4000`, and shell completion completes more than the first argument.
- Performance: each eBPF session loads its own kernel program, so two eBPF targets run two programs on every write on the machine. This is a deliberate first step; the cost is measured, and a shared eBPF engine becomes its own change only if the cost is real. One target costs the same as today.
- Tests: unit tests for the session, the tags and partial lines; E2E tests with two test servers.
- Docs: README, CHANGELOG, `--help`.
- No new dependencies.
