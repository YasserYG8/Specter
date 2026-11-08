## 1. Release config

- [x] 1.1 In `.goreleaser.yaml`, set `builds.goos` to `[linux]`, remove the `ignore` block, and remove the Windows `format_overrides` under `archives`.
- [x] 1.2 Run `goreleaser check`, then `goreleaser release --snapshot --clean` (in WSL or with Docker if GoReleaser isn't installed), and confirm the `dist/` output holds only Linux amd64 and arm64 archives, `.deb` and `.rpm` files, and `checksums.txt`. Don't commit `dist/`.

## 2. Message

- [x] 2.1 Move the non-Linux diagnostic into `unsupportedPlatformError(goos, target string) error` with the Windows, macOS and other-system texts from the design, and call it from `RunE` where the check is today.
- [x] 2.2 Tests: `windows` suggests `wsl specter 3000`; `darwin` names macOS and suggests a Linux VM or Docker; `--pid 14280` as the target is kept in the Windows advice; an unknown system gets the generic advice.
- [x] 2.3 A test that `specter version` and `specter completion bash` don't hit the check, using the command tree rather than the real OS.

## 3. Docs and checks

- [x] 3.1 README: the install section says release files are Linux binaries and `.deb`/`.rpm` packages; the macOS line in platform support points to a Linux VM or Docker; the contributing note asks for Docker-only and `specter run` modes instead of native macOS and Windows support.
- [x] 3.2 `CHANGELOG.md`: releases publish Linux binaries only, and the non-Linux message now has macOS advice.
- [x] 3.3 `gofmt -l .`, `go vet ./...`, `go test ./cmd/... ./internal/... ./test/stress/...` pass, and the macOS and Windows cross-compiles in CI still succeed.
