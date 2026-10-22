## 1. Project Detection Engine

- [ ] 1.1 Implement project detection in `internal/resolver/project.go` extracting directory basename and inspecting `package.json`, `pom.xml`, `go.mod`, `pyproject.toml`
- [ ] 1.2 Implement Docker Compose label parsing in `internal/resolver/docker.go` for `com.docker.compose.project` and `com.docker.compose.service`
- [ ] 1.3 Update `DiscoveredService` and `ListeningPort` in `internal/resolver/discovery.go` to include `ProjectName` and `WorkingDir` fields
- [ ] 1.4 Add unit tests for manifest parsing and Docker Compose label extraction in `internal/resolver/project_test.go` and `internal/resolver/docker_test.go`

## 2. Comfortable Column-Aligned UI Layout

- [ ] 2.1 Refactor `FormatServiceItem` in `internal/ui/picker.go` to render fixed-width, column-aligned rows displaying badge, port, project, process/image, and path/target
- [ ] 2.2 Implement path sanitization in `internal/ui/picker.go` replacing `$HOME` with `~` and responsive terminal width truncation
- [ ] 2.3 Update `RenderNonInteractiveTable` in `internal/ui/picker.go` with matching aligned columns
- [ ] 2.4 Update unit tests in `internal/ui/picker_test.go` for the column-aligned format and project display

## 3. CLI Integration & Project-Target Resolution

- [ ] 3.1 Update `cmd/specter/main.go` single-argument resolution to support attaching directly by project name (e.g. `specter example`)
- [ ] 3.2 Update `ValidArgsFunction` in `cmd/specter/main.go` to autocomplete discovered project names
- [ ] 3.3 Verify full test suite passes (`go test ./...`) with zero regressions
