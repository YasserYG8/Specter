## Why

Specter currently builds via manual `go build` commands or ad-hoc scripts, with no automated release pipeline, standardized cross-platform distribution archives, binary checksumming, or native package formats (`.deb`, `.rpm`). Automating the release process with GoReleaser v2 and GitHub Actions ensures reproducible, statically linked, multi-architecture binaries and native packages are automatically built, validated, and published whenever a release tag is pushed.

## What Changes

- Add `.goreleaser.yaml` defining GoReleaser v2 builds for Linux (`amd64`, `arm64`), macOS (`amd64`, `arm64`), and Windows (`amd64`).
- Inject build metadata (`version`, `commit`, `date`, `builtBy`) via `-ldflags` into `cmd/specter`.
- Support native Linux packages (`.deb` and `.rpm`) via `nfpm` with package recommendations for `strace`, `lsof`, and `iproute2`.
- Package compressed archive bundles (`.tar.gz` and `.zip`) including `LICENSE` and `README.md`.
- Generate automated cryptographic checksums (`checksums.txt` using SHA-256).
- Create `.github/workflows/release.yaml` triggered on tag pushes (`v*`) to build and publish GitHub release assets.
- Add GoReleaser validation step in CI (`.github/workflows/ci.yaml`) to verify configuration integrity on PRs.

## Capabilities

### New Capabilities
- `release-automation`: Configuration and CI workflows for cross-platform static binary compilation, Linux package generation (.deb, .rpm), build metadata injection, and automated GitHub releases.

### Modified Capabilities
None.

## Impact

- **Build System**: Introduces `.goreleaser.yaml` and `.github/workflows/release.yaml`.
- **Application Entrypoint**: Updates `cmd/specter/main.go` variables (`version`, `commit`, `date`, `builtBy`) to receive `-ldflags` injections.
- **CI Pipeline**: Extends `.github/workflows/ci.yaml` to run `goreleaser check` ensuring release configuration never regresses.
- **Dependencies**: No Go module dependency changes; leverages GoReleaser GitHub Action in CI.
