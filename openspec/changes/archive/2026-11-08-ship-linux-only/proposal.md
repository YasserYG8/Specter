## Why

GoReleaser is set up to publish five binaries: Linux amd64 and arm64, macOS amd64 and arm64, and Windows amd64. Specter attaches through Linux kernel interfaces (`ptrace`, `/proc`), and on any other system it stops with "Specter requires Linux kernel interfaces". A macOS or Windows user who downloads the file for their system therefore gets a tool that refuses to run. That's the opposite of a first try that works. Attaching to a running process can't be done cleanly on macOS (System Integrity Protection blocks it) or on Windows, so those builds won't become useful by themselves. Support for those systems, if it comes, will be a different feature, such as a Docker-only mode or a `specter run` mode.

## What Changes

- Releases publish Linux binaries only: `linux/amd64` and `linux/arm64`. The `.deb` and `.rpm` packages stay. The macOS and Windows archives, the Windows `.zip` override and the Windows/arm64 ignore rule are removed.
- CI keeps compiling for macOS and Windows, as a check that the code stays portable, since development happens on Windows. It publishes nothing.
- On a system other than Linux, the existing diagnostic gets advice for both Windows (run inside WSL2) and macOS (run the server and Specter in a Linux VM or a Docker container). It still appears only when Specter would attach, so `specter version`, `specter completion` and `--help` keep working everywhere.
- README: the install section says releases are Linux binaries and packages, and the contributing note no longer asks for native macOS or Windows support. It points to Docker-only and `specter run` modes instead.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-automation`: the build matrix and the archive formats become Linux-only.
- `diagnostic-reporter`: a requirement for the unsupported-platform diagnostic, covering Windows and macOS advice and the commands that still work.

## Impact

- `.goreleaser.yaml`: `builds.goos`, `builds.ignore`, `archives.format_overrides`.
- `cmd/specter/main.go`: the remediation text of the non-Linux `DiagnosticError`.
- `cmd/specter/main_test.go`: a test for the message per system, through a small function that takes the OS name.
- `README.md` and `CHANGELOG.md`.
- `.github/workflows/ci.yaml` is unchanged.
