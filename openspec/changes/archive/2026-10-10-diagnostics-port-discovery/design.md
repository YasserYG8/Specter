## Context

Specter currently resolves a port by querying `lsof -tiTCP:<port> -sTCP:LISTEN` with `ss` as a fallback. When the target port has no listening process, the resolver returns `ErrNoProcessFound`. In `cmd/specter/main.go`, this triggers a fallback check treating the argument as a PID, which typically fails as well with `target process not found`.

The developer is left with zero information regarding what is actually listening on the machine, forcing them to switch terminals and run diagnostic commands manually.

## Goals / Non-Goals

**Goals:**
- Lazy-evaluate active listening ports on discovery failure (zero latency penalty on happy path).
- Parse and aggregate active TCP listening ports from `ss -H -ltnp` and `lsof -iTCP -sTCP:LISTEN -P -n`.
- Extract PID, process name, and port numbers for each listener.
- Rank candidates using proximity heuristics and compute the highest-confidence suggestion ("Did you mean: specter <port> ?").
- Present a clean, readable diagnostic report in the terminal honoring `--no-color` and non-TTY redirection.

**Non-Goals:**
- Interactive prompts to automatically select a port (Specter remains non-interactive and script-friendly).
- UDP or Unix domain socket port scanning (Specter targets TCP web/RPC server processes).
- Scanning remote networks or ports outside the local machine.

## Decisions

### Decision 1: Lazy Port Enumeration on Resolution Miss
- **Rationale**: When `ResolvePortToPID(targetPort)` succeeds, no additional overhead is incurred. We only trigger full socket table enumeration if the target port is unallocated.
- **Alternatives Considered**:
  - *Scan all ports on startup*: Rejected because it introduces unnecessary 5–20ms latency to every command invocation.

### Decision 2: `ss -H -ltnp` as Primary Enumeration Tool
- **Rationale**: `ss` reads socket statistics directly from kernel Netlink / sock_diag tables, completing in <5ms. `lsof` scans all open file descriptors across `/proc` which can take 50–100ms on machines with many processes.
- **Fallback**: If `ss` is absent, fall back to `lsof -iTCP -sTCP:LISTEN -P -n`.

### Decision 3: Multi-Factor Suggestion Ranking
- **Scoring Heuristics**:
  1. *Numerical Proximity*: Ports in the same block (e.g. `3000` $\rightarrow$ `3001` or `3002` due to port-in-use auto-increment).
  2. *String Edit Distance*: Typo detection (e.g. `8000` $\rightarrow$ `8080`, `5000` $\rightarrow$ `5001`).
  3. *Application Runtime Weighting*: High bonus for recognized web backend runtimes (`node`, `python`, `go`, `ruby`, `java`, `uvicorn`, `flask`, `docker-proxy`); penalty for system daemons (`rpcbind`, `systemd-resolved`, `cupsd`).
- **Cap Output**: Display at most 5–8 relevant active listening ports to prevent terminal clutter if many ephemeral ports exist.

### Decision 4: Structured Error Representation
- Define `ListeningPort` and `NoProcessOnPortError` in `internal/resolver`:
  ```go
  type ListeningPort struct {
      Port        int
      PID         int
      ProcessName string
      IsProtected bool // True if owned by root/another user and PID is hidden
  }

  type NoProcessOnPortError struct {
      RequestedPort int
      ActivePorts   []ListeningPort
      Suggestion    *ListeningPort
  }
  ```
- Integrates with `util.DiagnosticError` so `main.go` formats the output cleanly.

## Risks / Trade-offs

- **[Risk] Sudo/Root Processes Hide PIDs in `ss`**: Regular users see the port in LISTEN state, but `users:(...)` is empty.
  - *Mitigation*: Flag `IsProtected = true` and display `:80 (process owned by root - run with sudo)` so the user is informed rather than confused.
- **[Risk] High Port Density**: On systems running dozens of containers or ephemeral microservices, listing all ports could flood the terminal.
  - *Mitigation*: Sort by score and limit display to the top 6 closest developer ports.
