## Why

Currently, when `specter` discovers active host servers and Docker containers, it only displays low-level binary names (`node`, `python3`) and generic PIDs or raw image tags. When developers run multiple services simultaneously (e.g. frontend, backend API, worker), every Node server is indistinguishably shown as `node (PID xxxx)`. Developers need immediately recognizable project context—such as project folder names, repository descriptors (`package.json`, `pom.xml`, `go.mod`), and Docker Compose project names—presented in a comfortable, clean, column-aligned interactive discovery UI.

## What Changes

- **Project Name Detection for Host Processes**: Inspect the server's working directory (`/proc/<PID>/cwd`) to extract the project folder basename (e.g. `example`) and inspect project manifest files (`package.json`, `pom.xml`, `go.mod`, `pyproject.toml`) to determine the exact project name.
- **Docker Compose Project & Service Resolution**: Parse Docker container labels (`com.docker.compose.project` and `com.docker.compose.service`) to display clean grouped names like `soa / phpmyadmin` instead of isolated container hashes or long images.
- **Comfortable Column-Aligned Interactive Picker**: Redesign the interactive terminal picker with well-proportioned columns: `TYPE`, `PORT`, `PROJECT`, `PROCESS / IMAGE`, and `PATH / TARGET`.
- **Enhanced Non-Interactive Output**: Upgrade the fallback table rendered in non-TTY environments with project names, working directory paths, and container port forwardings.
- **Rich Autocompletion**: Enhance shell tab completions so developers can autocomplete by project name (e.g. `specter ex<TAB>` $\rightarrow$ `example\t:3000 (node)`).

## Capabilities

### Modified Capabilities
- `interactive-picker`: Display project names, formatted directory paths, and comfortable column-aligned service entries in both interactive and non-TTY modes.
- `port-resolver`: Enrich active listening port and service discovery with project names parsed from process `cwd`, manifest files, and Docker Compose labels.

## Impact

- **Affected Code**: `internal/resolver/discovery.go`, `internal/resolver/process.go`, `internal/resolver/docker.go`, `internal/ui/picker.go`, and `cmd/specter/main.go`.
- **Dependencies**: No external dependencies. Uses standard Go standard library (`os`, `path/filepath`, `encoding/json`, `encoding/xml`) and existing ANSI terminal formatting.
- **Breaking Changes**: None. All existing CLI flags and invocation forms remain backward-compatible.
