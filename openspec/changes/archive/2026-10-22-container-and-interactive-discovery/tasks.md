## 1. Docker Discovery and Resolver Integration

- [x] 1.1 Create `DockerContainer` model and JSON parser for `docker ps --format "{{json .}}"` in `internal/resolver/docker.go`
- [x] 1.2 Implement container port mapping parser (handling IPv4, IPv6, and multiple port bindings) and container existence inspection
- [x] 1.3 Update `PortResolver` to fallback to Docker container lookup when host PID is absent or permission-masked
- [x] 1.4 Update `DefaultResolver.ResolvePort` and `DefaultResolver.DiscoverListeningPorts` to return unified `DiscoveredService` objects combining host ports and active containers
- [x] 1.5 Add table-driven unit tests for Docker container discovery, port extraction, and resolver fallbacks in `internal/resolver/docker_test.go`

## 2. Source Selector and Direct Container Targeting

- [x] 2.1 Update `internal/source/selector/selector.go` to support direct container targets without requiring a host PID
- [x] 2.2 Add unit tests in `internal/source/selector/selector_test.go` verifying that targets with `PID == 0` and non-empty `Container` select `DockerSource`

## 3. Interactive Picker & Non-TTY Service Table

- [x] 3.1 Implement terminal interactive picker in `internal/ui/picker.go` using ANSI escape codes and `golang.org/x/term` for arrow key navigation (`↑`/`↓`), selection (`Enter`), and cancellation (`Ctrl+C`/`Esc`/`q`)
- [x] 3.2 Implement non-TTY fallback table renderer in `internal/ui/picker.go` that prints formatted active services when standard input/output is not a terminal
- [x] 3.3 Add unit tests for service list formatting and navigation bounds in `internal/ui/picker_test.go`

## 4. CLI Entrypoint and Unified Autocompletion

- [x] 4.1 Update `cmd/specter/main.go` argument acceptance to `cobra.RangeArgs(0, 1)`
- [x] 4.2 Implement zero-argument handling in `main.go` to invoke the interactive picker or non-TTY table and attach to the chosen target
- [x] 4.3 Enhance single-argument handling to support alphanumeric container names or IDs (e.g. `specter phpmyadmin`), verifying via Docker inspect
- [x] 4.4 Update `ValidArgsFunction` shell completions to suggest active host ports, container mapped ports, and container names with descriptions
- [x] 4.5 Verify full test suite passes (`go test ./...`) with zero regressions
