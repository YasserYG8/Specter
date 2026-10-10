## Why

When a developer runs `specter 3000` and nothing is currently listening on port 3000, Specter currently aborts with a generic "no process listening on port" error. Developers are left with zero context about which ports are actually running on their machine, forcing them to manually open another terminal to run `ss`, `lsof`, or `netstat`. By implementing active port discovery and intelligent suggestions ("Did you mean: specter 8080 ?"), Specter turns a dead-end error into an intuitive developer workflow.

## What Changes

- Implement active listening port enumeration in the resolver layer using Linux `ss` and `lsof`.
- Extract metadata for all active listening TCP ports: port number, PID, process name, and command-line summary.
- Implement heuristic suggestion matching:
  - Numerical proximity (e.g., user requested `3000`, active server bumped to `3001` or `3002`).
  - String distance / common developer port classifications (`3000-3999`, `5000-5999`, `8000-8999`).
  - Runtime categorization prioritizing web application servers (Node.js, Python, Go, Java, Docker) over system daemons.
- Enhance CLI diagnostic reporting in `cmd/specter/main.go` and `internal/util/errors.go`:
  - When a target port is not listening, list all active developer ports with PID and process name.
  - Print an explicit high-confidence suggestion: `Did you mean: specter <port> ?`.
  - Handle permission barriers gracefully (e.g., listening port owned by root/another user where PID is hidden).

## Capabilities

### New Capabilities
- `diagnostic-reporter`: Structured terminal reporting for target discovery failures, listing active listening ports and formatting actionable "Did you mean" suggestions.

### Modified Capabilities
- `port-resolver`: Extend resolver to enumerate all active listening TCP ports when a target port is not found, returning rich listening port candidates alongside discovery errors.

## Impact

- **Affected Packages**:
  - `internal/resolver`: Add `DiscoverListeningPorts` and port suggestion ranking heuristics.
  - `internal/util`: Update `DiagnosticError` and add structured port discovery error types.
  - `cmd/specter`: Integrate active port listing and suggestions into the error handling path.
- **Dependencies**: No external Go dependencies added; uses standard library and existing system utilities (`ss`, `lsof`).
- **Breaking Changes**: None. Backwards-compatible enhancement to the error path.
