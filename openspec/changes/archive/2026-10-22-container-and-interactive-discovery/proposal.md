## Why

Specter currently requires a mandatory positive numeric port or PID argument, failing with `accepts 1 arg(s), received 0` if invoked without arguments. Furthermore, in containerized and WSL2 environments, Specter fails to resolve ports mapped by Docker containers (e.g. `specter 8080` for phpMyAdmin or `specter 3306` for MySQL), misidentifying them as "protected root processes" because container processes exist in isolated namespaces outside the host `/proc` table.

Adding Docker container port resolution, named container target arguments (`specter phpmyadmin`), and an interactive zero-argument discovery picker transforms Specter into a frictionless universal dev-server detective for local, containerized, and WSL environments.

## What Changes

- **Zero-Argument Interactive Discovery Picker**: When invoked without arguments in a terminal (`specter`), dynamically scan active host listening ports and running Docker containers, presenting an interactive arrow-key selection menu (`↑`/`↓` and `<Enter>`) to stream immediately. In non-TTY environments, output an informative table.
- **Docker Port Fallback in Resolver**: When `ResolvePort` detects a port without a host PID (or associated with container proxies like `docker-proxy`), automatically query Docker to map the published port to its active container and attach via Specter's Docker source.
- **Direct Container Name / ID Arguments**: Allow non-numeric target arguments (e.g. `specter phpmyadmin`, `specter mysql_db`, `specter 13cbb067d0f2`). Verify container state via Docker Engine and attach directly.
- **Unified Autocompletion**: Enhance shell completions (`specter <TAB>`) to autocomplete active host ports, container ports, and container names with descriptions.

## Capabilities

### New Capabilities
- `interactive-picker`: Interactive terminal picker allowing developers to browse, select, and attach to active host ports and Docker containers when running `specter` with zero arguments.

### Modified Capabilities
- `port-resolver`: Expand port resolution to check Docker mapped ports when local `/proc` inspection yields no host PID, and include active Docker containers in `DiscoverListeningPorts`.
- `source-selector`: Support direct resolution of container name and container ID targets to `docker` source without requiring a host PID.

## Impact

- **Affected Code**: `cmd/specter/main.go`, `internal/resolver/port.go`, `internal/resolver/discovery.go`, `internal/resolver/docker.go`, `internal/source/selector/selector.go`, and new `internal/ui/picker.go`.
- **Dependencies**: No heavy external UI frameworks. Uses lightweight terminal raw-mode navigation with standard ANSI sequences and graceful non-TTY fallbacks.
- **Breaking Changes**: None. Existing syntax (`specter <port>` and `specter <pid>`) continues to work with enhanced container awareness.
