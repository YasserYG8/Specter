## ADDED Requirements

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
