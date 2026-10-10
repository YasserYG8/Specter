# release-automation Specification

## Purpose
TBD - created by archiving change automated-release-goreleaser. Update Purpose after archive.
## Requirements
### Requirement: Cross-Platform Build Matrix
The build automation SHALL configure GoReleaser v2 to compile statically linked binaries without CGO for Linux (`amd64`, `arm64`), macOS (`amd64`, `arm64`), and Windows (`amd64`).

#### Scenario: Multi-architecture static builds
- **WHEN** GoReleaser runs a release or snapshot build
- **THEN** it produces standalone executables for Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 with `CGO_ENABLED=0`

### Requirement: Build Metadata Injection
The build process SHALL inject semantic version, git commit SHA, build timestamp, and builder identity into the application entrypoint via linker flags.

#### Scenario: Stamping version metadata
- **WHEN** the binary is compiled via GoReleaser
- **THEN** `main.version`, `main.commit`, `main.date`, and `main.builtBy` are injected and accessible via `specter --version`

### Requirement: Package and Archive Generation
The release automation SHALL package compressed archives (`.tar.gz` for Linux/macOS, `.zip` for Windows) containing the executable, `LICENSE`, and `README.md`, and generate native `.deb` and `.rpm` Linux packages.

#### Scenario: Generating distribution archives and packages
- **WHEN** a release build executes
- **THEN** it generates `.tar.gz` and `.zip` archives alongside `.deb` and `.rpm` packages containing the binary placed at `/usr/bin/specter` and listing recommended packages (`strace`, `lsof`, `iproute2`)

### Requirement: Checksum Verification and Release Publishing
The release automation SHALL generate SHA-256 cryptographic checksums for all release artifacts and publish them to GitHub Releases upon pushing a semver git tag.

#### Scenario: Tag-triggered automated release
- **WHEN** a git tag matching `v*` is pushed to GitHub
- **THEN** the release workflow executes GoReleaser, validates artifacts, creates a GitHub release with an automated changelog, and uploads the archives, packages, and `checksums.txt`

