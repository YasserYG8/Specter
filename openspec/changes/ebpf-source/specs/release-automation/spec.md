## ADDED Requirements

### Requirement: Prebuilt BPF Program
The compiled BPF program and its generated Go bindings SHALL be committed to the repository for little-endian targets (`amd64` and `arm64`), so that `go build`, the release build and users need no C compiler, and releases stay static with `CGO_ENABLED=0`. CI SHALL regenerate them from the C source with a pinned `clang` version and fail when the result differs from the committed files. The code SHALL still compile for macOS and Windows, where the `ebpf` source reports that it is unavailable.

#### Scenario: Build without clang
- **WHEN** a contributor without `clang` runs `go build ./cmd/specter` on Linux
- **THEN** the build succeeds and includes the `ebpf` source

#### Scenario: Stale generated files
- **WHEN** a pull request changes the BPF C source without regenerating the committed files
- **THEN** the CI check fails and names the files that differ

#### Scenario: Cross-builds
- **WHEN** CI cross-compiles Specter for macOS and Windows
- **THEN** both builds succeed
