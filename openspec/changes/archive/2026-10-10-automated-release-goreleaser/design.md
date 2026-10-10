## Context

Specter is a Linux-first systems developer tool written in Go that attaches to server processes by port or PID to stream logs. Currently, builds are run ad-hoc without formal cross-platform packaging, version stamping, or automated release publishing.

To support distribution across varied developer environments (Linux x86_64, Linux ARM64, Apple Silicon macOS, Intel macOS, and Windows), Specter requires an automated, reproducible release pipeline that statically compiles binaries, injects git metadata, packages archive artifacts and Linux system packages (`.deb`, `.rpm`), generates cryptographic checksums, and publishes releases to GitHub via Actions.

## Goals / Non-Goals

**Goals:**
- Provide a standardized `.goreleaser.yaml` (v2 schema) configured for pure static compilation (`CGO_ENABLED=0`).
- Target core platforms: Linux (`amd64`, `arm64`), macOS (`amd64`, `arm64`), and Windows (`amd64`).
- Inject build metadata (`version`, `commit`, `date`, `builtBy`) into the executable via `-ldflags`.
- Generate native `.deb` and `.rpm` packages via `nfpm` with recommended system dependencies (`strace`, `lsof`, `iproute2`).
- Package archives (`.tar.gz` and `.zip`) containing the binary, `LICENSE`, and `README.md`.
- Produce cryptographic `checksums.txt` using SHA-256.
- Automate release publication on GitHub Actions (`.github/workflows/release.yaml`) triggered by `v*` semver tags.
- Verify release configuration integrity in CI (`.github/workflows/ci.yaml`).

**Non-Goals:**
- Homebrew Tap repository creation (can be added as an external tap repository later).
- GPG binary signing or Cosign keyless signing in this phase.
- Docker image container registry publishing (handled in containerization phase).

## Decisions

### 1. GoReleaser v2 Syntax
- **Decision**: Use `version: 2` schema for GoReleaser.
- **Rationale**: GoReleaser v2 is the current supported standard with modern syntax for `builds`, `archives`, and `nfpms`.
- **Alternatives Considered**: GoReleaser v1 (deprecated).

### 2. Pure Static Compilation (`CGO_ENABLED=0`)
- **Decision**: Force `CGO_ENABLED=0` across all target platforms.
- **Rationale**: Eliminates dependencies on host `glibc` versions, ensuring a single Linux binary runs on Ubuntu, Debian, Alpine (musl), Arch, and CentOS without dynamic linker failures.
- **Alternatives Considered**: CGO enabled with cross-compilation toolchains (adds severe CI complexity and docker build dependencies).

### 3. Metadata Injection via LDFlags
- **Decision**: Expose `version`, `commit`, `date`, and `builtBy` in package `main` (`cmd/specter/main.go`) and inject values via `-ldflags="-s -w -X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}} -X main.builtBy=goreleaser"`.
- **Rationale**: Strips symbol tables and debug information (`-s -w`) to minimize binary size while embedding exact build provenance.

### 4. Linux Package Generation with NFPM
- **Decision**: Generate `.deb` and `.rpm` packages with `recommends: [strace, lsof, iproute2]`.
- **Rationale**: `recommends` alerts package managers to install key diagnostic utilities without hard-failing if an alternative containerized tool is preferred by the user.

### 5. Release Workflow Trigger and CI Gate
- **Decision**: Trigger full release on tag pushes `v*` with `fetch-depth: 0`, and add `goreleaser check` to `.github/workflows/ci.yaml`.
- **Rationale**: `fetch-depth: 0` is required for GoReleaser git changelog and tag parsing. Running `goreleaser check` on PRs prevents breaking config syntax changes from landing in `main`.

## Risks / Trade-offs

- **[Risk] Missing git tags in local snapshot builds** → **Mitigation**: GoReleaser `--snapshot` mode handles untagged dirty worktrees gracefully during local testing.
- **[Risk] macOS strace differences** → **Mitigation**: Binaries compile for darwin/amd64 and darwin/arm64 for fallback and container modes; Linux remains the primary runtime platform.
- **[Risk] GitHub Token permissions** → **Mitigation**: The release workflow explicitly requests `permissions: contents: write` so the GITHUB_TOKEN has permission to publish release assets.
