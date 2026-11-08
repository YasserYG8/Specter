## MODIFIED Requirements

### Requirement: Cross-Platform Build Matrix
The build automation SHALL configure GoReleaser v2 to compile statically linked binaries without CGO for Linux (`amd64`, `arm64`) only. It SHALL NOT publish macOS or Windows binaries, because Specter cannot attach to processes on those systems. CI SHALL still compile the code for macOS and Windows, without publishing the result, to keep it portable.

#### Scenario: Multi-architecture static builds
- **WHEN** GoReleaser runs a release or snapshot build
- **THEN** it produces standalone executables for Linux amd64 and Linux arm64 with `CGO_ENABLED=0`, and none for macOS or Windows

#### Scenario: Portability check in CI
- **WHEN** CI runs on a push or pull request
- **THEN** it compiles Specter for macOS and Windows and fails if that does not compile, without uploading those binaries anywhere

### Requirement: Package and Archive Generation
The release automation SHALL package `.tar.gz` archives for each Linux build containing the executable, `LICENSE`, and `README.md`, and generate native `.deb` and `.rpm` Linux packages.

#### Scenario: Generating distribution archives and packages
- **WHEN** a release build executes
- **THEN** it generates `.tar.gz` archives for Linux amd64 and arm64 alongside `.deb` and `.rpm` packages containing the binary placed at `/usr/bin/specter` and listing recommended packages (`strace`, `lsof`, `iproute2`), and no `.zip` archive
