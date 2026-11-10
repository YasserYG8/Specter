# release-automation Specification

## Purpose
TBD - created by archiving change automated-release-goreleaser. Update Purpose after archive.
## Requirements
### Requirement: Cross-Platform Build Matrix
The build automation SHALL configure GoReleaser v2 to compile statically linked binaries without CGO for Linux (`amd64`, `arm64`) only. It SHALL NOT publish macOS or Windows binaries, because Specter cannot attach to processes on those systems. CI SHALL still compile the code for macOS and Windows, without publishing the result, to keep it portable.

#### Scenario: Multi-architecture static builds
- **WHEN** GoReleaser runs a release or snapshot build
- **THEN** it produces standalone executables for Linux amd64 and Linux arm64 with `CGO_ENABLED=0`, and none for macOS or Windows

#### Scenario: Portability check in CI
- **WHEN** CI runs on a push or pull request
- **THEN** it compiles Specter for macOS and Windows and fails if that does not compile, without uploading those binaries anywhere

### Requirement: Build Metadata Injection
The build process SHALL inject semantic version, git commit SHA, build timestamp, and builder identity into the application entrypoint via linker flags.

#### Scenario: Stamping version metadata
- **WHEN** the binary is compiled via GoReleaser
- **THEN** `main.version`, `main.commit`, `main.date`, and `main.builtBy` are injected and accessible via `specter --version`

### Requirement: Package and Archive Generation
The release automation SHALL package `.tar.gz` archives for each Linux build containing the executable, `LICENSE`, and `README.md`, and generate native `.deb` and `.rpm` Linux packages.

#### Scenario: Generating distribution archives and packages
- **WHEN** a release build executes
- **THEN** it generates `.tar.gz` archives for Linux amd64 and arm64 alongside `.deb` and `.rpm` packages containing the binary placed at `/usr/bin/specter` and listing recommended packages (`strace`, `lsof`, `iproute2`), and no `.zip` archive

### Requirement: Checksum Verification and Release Publishing
The release automation SHALL generate SHA-256 cryptographic checksums for all release artifacts and publish them to GitHub Releases upon pushing a semver git tag.

#### Scenario: Tag-triggered automated release
- **WHEN** a git tag matching `v*` is pushed to GitHub
- **THEN** the release workflow executes GoReleaser, validates artifacts, creates a GitHub release with an automated changelog, and uploads the archives, packages, and `checksums.txt`

### Requirement: Install Script
The repository SHALL provide `install.sh`, a POSIX `sh` script that installs the latest released Specter binary with one command: `curl -sSL https://raw.githubusercontent.com/YasserYG8/Specter/main/install.sh | sh`. The script SHALL run only on Linux, map `x86_64` to `amd64` and `aarch64` to `arm64`, download `specter_linux_<arch>.tar.gz` and `checksums.txt` from the release, verify the archive's SHA-256 checksum before installing, install the binary to `/usr/local/bin` (using `sudo` only when that directory is not writable), and remove its temporary files. It SHALL NOT install other packages; when `strace` or `lsof` is missing it SHALL print the command to install them. `SPECTER_VERSION` SHALL select a specific release tag, and `SPECTER_INSTALL_DIR` SHALL select another install directory.

#### Scenario: Install on Linux amd64
- **WHEN** a user on Linux x86_64 with `strace` and `lsof` installed runs the one-line command
- **THEN** the latest `specter_linux_amd64.tar.gz` is downloaded and verified, `specter` is installed to `/usr/local/bin`, and the script prints the installed version

#### Scenario: Install on Linux arm64
- **WHEN** the machine reports `aarch64`
- **THEN** the script downloads `specter_linux_arm64.tar.gz`

#### Scenario: Not Linux
- **WHEN** the script runs on macOS
- **THEN** it exits with a non-zero status before downloading anything, and says Specter needs Linux, pointing to WSL2 on Windows and a Linux VM or Docker on macOS

#### Scenario: Unsupported chip
- **WHEN** the machine reports `armv7l`
- **THEN** the script exits with a non-zero status and lists the supported chips

#### Scenario: Checksum mismatch
- **WHEN** the downloaded archive's SHA-256 does not match its line in `checksums.txt`
- **THEN** the script installs nothing and exits with a non-zero status that names the file

#### Scenario: Missing tools
- **WHEN** `strace` is not installed
- **THEN** the script still installs Specter and ends with a notice to run `sudo apt install strace lsof iproute2` (or the matching command for `dnf` or `pacman` when `apt` is not present)

#### Scenario: Pinned version and custom directory
- **WHEN** the user sets `SPECTER_VERSION=v0.1.0` and `SPECTER_INSTALL_DIR=$HOME/.local/bin`
- **THEN** the script downloads from the `v0.1.0` release and installs to `$HOME/.local/bin` without `sudo`

### Requirement: Latest Release Points to the Newest Version
Releases SHALL be published as normal releases, not pre-releases, and archive names SHALL NOT contain the version, so that `https://github.com/YasserYG8/Specter/releases/latest/download/specter_linux_<arch>.tar.gz` always serves the newest release.

#### Scenario: Latest download link
- **WHEN** release `v0.1.0` is published
- **THEN** `.../releases/latest/download/specter_linux_amd64.tar.gz` downloads the `v0.1.0` archive

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
