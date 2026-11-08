## Context

`release.yaml` runs GoReleaser v2 on any pushed `v*` tag. `.goreleaser.yaml` builds `linux/amd64` and `linux/arm64`, packs `specter_linux_<arch>.tar.gz` (the binary, LICENSE and README, not wrapped in a folder), builds `.deb` and `.rpm` packages, and writes `checksums.txt` with SHA-256 lines in the form `<hash>  <file>`. There is no `prerelease` setting, so a `v0.1.0` tag gives a normal release. Archive names carry no version, so GitHub's `releases/latest/download/<name>` link resolves to the newest release.

## Goals / Non-Goals

**Goals:**
- One command installs Specter on any Linux or WSL2 machine with `curl` or `wget`.
- The script never installs a file it didn't verify.
- The script is short enough to read before running.

**Non-Goals:**
- Installing `strace`, `lsof` or `iproute2` for the user. The script prints the command instead, so it never runs a package manager on the user's behalf.
- Homebrew, AUR or npm packages. Those are stage 4.
- An uninstall script. Removing one file is documented instead.
- Pushing the tag. That's a public action for the maintainer.

## Decisions

### 1. Download from `latest/download`, or from a tag when pinned

```
base = https://github.com/YasserYG8/Specter/releases/latest/download         (default)
base = https://github.com/YasserYG8/Specter/releases/download/$SPECTER_VERSION (pinned)
```

No GitHub API call is needed, so there's no rate limit and no `jq`. For tests, `SPECTER_DOWNLOAD_BASE` overrides `base`. It's documented in the script header as a test hook only.

### 2. The flow

```
uname -s = Linux?            else: needs-Linux message, exit 1
uname -m → amd64 | arm64     else: list supported chips, exit 1
curl -fsSL (or wget -qO)     archive + checksums.txt into a mktemp dir, trap removes it
sha256sum -c (grep the line for the archive)   mismatch: name the file, exit 1
tar -xzf, then install -m 0755 specter $DIR    sudo only if $DIR not writable and not root
specter --version            print it
command -v strace / lsof     missing: print apt, dnf or pacman command
```

`set -eu` at the top. The whole body sits in a `main` function called on the last line, so a download cut off halfway through `curl | sh` runs nothing.

### 3. Default directory

`/usr/local/bin`, since it's on every distribution's `PATH`, and Specter often runs with `sudo`, whose `PATH` includes it. `SPECTER_INSTALL_DIR` covers users without `sudo`.

### 4. README order

1. `curl -sSL .../install.sh | sh`
2. "Prefer to read it first:" `curl -sSLO .../install.sh && less install.sh && sh install.sh`
3. `.deb` / `.rpm` from the release page: `sudo apt install ./specter_0.1.0_amd64.deb`
4. `go install ...@latest`

### 5. Testing before a release exists

Build a snapshot with `goreleaser release --snapshot --clean`, serve `dist/` with `python3 -m http.server`, and run the script with `SPECTER_DOWNLOAD_BASE=http://127.0.0.1:8000`. Test a good archive, a changed archive (the checksum must fail), a missing `strace` (simulated with a reduced `PATH`), and a custom install directory. Run `shellcheck install.sh` if available.

## Risks / Trade-offs

- [The `main` branch copy of `install.sh` changes later, and old one-liners then run new code] → That's acceptable for an installer. `SPECTER_VERSION` still pins the binary.
- [A user's system has neither `sha256sum` nor `shasum`] → The script stops with a message rather than installing unverified. Both are in coreutils and macOS, so this is unlikely on Linux.
- [The tag is pushed before `install.sh` is on `main`] → The task order puts the commit of `install.sh` before the tag.

## Migration Plan

1. Merge this change to `main`.
2. Check that CI is green.
3. The maintainer pushes `v0.1.0`.
4. Check the release page, and run the one-liner in a clean WSL distro or a Docker `ubuntu` container.

Rollback: delete the GitHub release and the tag. Nobody depends on it yet.

## Open Questions

None.
