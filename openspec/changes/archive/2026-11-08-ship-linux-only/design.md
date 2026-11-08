## Context

`.goreleaser.yaml` builds `goos: [linux, darwin, windows]` × `goarch: [amd64, arm64]`, ignores `windows/arm64`, packs Windows as `.zip` through `format_overrides`, and builds `.deb` and `.rpm` with `nfpms`. `ci.yaml` cross-compiles for `linux/amd64`, `linux/arm64`, `darwin/arm64` and `windows/amd64` as a build check.

`RunE` in `cmd/specter/main.go` already refuses to attach on a system other than Linux, after flag validation and before resolving the target, with:

- Title: `Specter requires Linux kernel interfaces (running on <os>)`
- Remediation: `On Windows, run inside WSL2: wsl specter <target> (or within a Linux container)`

The remediation says "On Windows" on every system, including macOS. `version` and `completion` are separate subcommands, so they don't reach this check.

## Goals / Non-Goals

**Goals:**
- Release only binaries that can do their job.
- Give the right advice on Windows and on macOS.
- Keep the code compiling on Windows and macOS.

**Non-Goals:**
- Any macOS or Windows feature, such as a Docker-only mode or `specter run`. If one comes, it re-adds those builds.
- An install script (`curl ... | sh`). That's planned for the v0.1 release step.
- Changing the `.deb` and `.rpm` packages beyond what follows from Linux-only builds.

## Decisions

### 1. GoReleaser

```yaml
builds:
  - goos: [linux]
    goarch: [amd64, arm64]
archives:
  - formats: [tar.gz]        # format_overrides for windows removed
```

The `ignore` block is removed, since it only excluded `windows/arm64`. `nfpms` already builds Linux-only formats and needs no change. Run `goreleaser check` and a snapshot build (`goreleaser release --snapshot --clean`) to confirm the output: 2 archives, 2 `.deb`, 2 `.rpm`, and `checksums.txt`.

### 2. CI stays as it is

The macOS and Windows build steps in `ci.yaml` compile and discard the binary. They catch code that would break on the developer's Windows machine, which is worth keeping. They upload nothing.

### 3. One message function per system

Move the message into `unsupportedPlatformError(goos, target string) error` so tests can call it with `"windows"` and `"darwin"` on any host. Text:

- Title: `Specter needs Linux to attach to a process (this is <system name>)`, where `darwin` is shown as `macOS`.
- Explanation: `It reads a running process through Linux kernel interfaces (ptrace and /proc), which <system> does not provide.`
- Remediation for Windows: `Run it inside WSL2: wsl specter <target>`
- Remediation for macOS: `Run your server and Specter inside a Linux VM (for example OrbStack, UTM or Lima) or a Docker container.`
- Any other system: `Run your server and Specter on Linux.`

## Risks / Trade-offs

- [macOS developers can't download Specter at all] → That's accepted. They couldn't use the old binary either. The README and the message point to Linux VMs, and `linux/arm64` covers VMs on Apple Silicon.
- [Someone relied on the macOS or Windows archive for `specter version` or completions] → There's no release yet, so nobody has.

## Migration Plan

None. The next tagged release has the new set of files.

## Open Questions

None.
