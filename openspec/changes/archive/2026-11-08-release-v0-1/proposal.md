## Why

Specter has no release, so the only way to install it is `go install`, which needs Go 1.26. The stage 1 fixes are done: lost lines, fake lines, whole stack traces with `--grep`, `--pid`, exit reports, Node.js terminal output, already-traced targets, Python log levels and Linux-only downloads. Publishing v0.1.0 lets anyone on Linux or WSL2 install Specter with one command, and gives multi-streaming a stable base.

## What Changes

- New `install.sh` at the repository root: a POSIX `sh` script that checks the system is Linux, picks `amd64` or `arm64`, downloads the release archive and `checksums.txt`, verifies the SHA-256, installs `specter` to `/usr/local/bin` (with `sudo` only when needed), and says how to install `strace` and `lsof` if they are missing. `SPECTER_VERSION` and `SPECTER_INSTALL_DIR` allow a pinned version and another directory.
- README install section: three ways, in order: the one-line `curl` command (plus "download and read it first" for people who don't pipe scripts into `sh`), the `.deb`/`.rpm` packages, and `go install`.
- CHANGELOG: `## Unreleased` becomes `## 0.1.0 (<date>)`.
- Release v0.1.0 as a normal release, not a pre-release, so the `latest` download links point to it. The maintainer pushes the `v0.1.0` tag. Nothing in this change pushes it.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-automation`: an install script requirement, and the rule that releases are normal releases with version-free archive names so `latest` links work.

## Impact

- New `install.sh`.
- `README.md`, `CHANGELOG.md`.
- No Go code changes. `.goreleaser.yaml` already names archives `specter_linux_<arch>.tar.gz` and writes `checksums.txt`. Releases are only marked as pre-releases if configured, and they aren't.
