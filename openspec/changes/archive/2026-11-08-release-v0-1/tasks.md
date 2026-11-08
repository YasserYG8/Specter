## 1. Install script

- [x] 1.1 Write `install.sh` as in the design: POSIX `sh`, `set -eu`, everything inside `main` called on the last line, Linux and chip checks, `curl` or `wget`, a temporary directory removed by `trap`, a SHA-256 check against `checksums.txt`, `install -m 0755` into `SPECTER_INSTALL_DIR` (default `/usr/local/bin`, `sudo` only when needed), `specter --version` at the end, and a notice with the `apt`, `dnf` or `pacman` command when `strace` or `lsof` is missing. Support `SPECTER_VERSION` and the test-only `SPECTER_DOWNLOAD_BASE`.
- [x] 1.2 `shellcheck install.sh` passes, if `shellcheck` is available in WSL.

## 2. Test the script against a snapshot

- [x] 2.1 In WSL, build a snapshot with GoReleaser and serve `dist/` with `python3 -m http.server`.
- [x] 2.2 Run the script with `SPECTER_DOWNLOAD_BASE` and `SPECTER_INSTALL_DIR` set to a temporary folder: the binary installs and `--version` prints.
- [x] 2.3 Change one byte of the served archive: the script must refuse and install nothing.
- [x] 2.4 Run it with a `PATH` that has no `strace`: it installs and prints the install command for the missing tools.
- [x] 2.5 Fake `uname -s` as `Darwin` and `uname -m` as `armv7l` (a stub `uname` early on `PATH`): both stop before downloading, with the right messages.
- [x] 2.6 Remove the snapshot `dist/` folder, and don't commit it.

## 3. Docs

- [x] 3.1 README install section in the order from the design: the one-liner, "read it first", `.deb`/`.rpm`, `go install`. Add one line on how to uninstall: `sudo rm /usr/local/bin/specter`.
- [x] 3.2 CHANGELOG: rename `## Unreleased` to `## 0.1.0 (<release date>)`.

## 4. Release (the maintainer)

- [x] 4.1 Commit and push sections 1–3 to `main`, and check that CI is green.
- [x] 4.2 Stop and ask the maintainer to push the tag: `git tag v0.1.0 && git push origin v0.1.0`. Don't push it without their explicit go-ahead.
- [x] 4.3 After the release workflow finishes: the release page shows 2 archives, 2 `.deb`, 2 `.rpm` and `checksums.txt`, and it is not marked as a pre-release.
- [x] 4.4 Run the real one-liner in a clean Docker `ubuntu` container or a fresh WSL distro: Specter installs and `specter --version` prints `0.1.0`.
