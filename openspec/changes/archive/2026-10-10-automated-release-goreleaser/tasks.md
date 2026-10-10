## 1. Build Metadata & CLI Entrypoint

- [x] 1.1 Declare build metadata variables (`version`, `commit`, `date`, `builtBy`) in `cmd/specter/main.go`
- [x] 1.2 Format Cobra version output or template to display detailed build metadata (`version`, `commit`, `date`, `builtBy`, `runtime.Version`, `GOOS/GOARCH`)
- [x] 1.3 Add unit test in `cmd/specter` verifying build metadata rendering

## 2. GoReleaser Configuration

- [x] 2.1 Create `.goreleaser.yaml` (v2 schema) with multi-arch builds (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`) and `CGO_ENABLED=0`
- [x] 2.2 Configure `-ldflags` injection for `main.version`, `main.commit`, `main.date`, and `main.builtBy`
- [x] 2.3 Configure distribution archives (`.tar.gz` and `.zip`), bundling `LICENSE` and `README.md`
- [x] 2.4 Configure `nfpms` generating `.deb` and `.rpm` packages with `/usr/bin/specter` binary placement and `strace`, `lsof`, `iproute2` recommendations
- [x] 2.5 Configure SHA-256 checksumming (`checksums.txt`) and changelog generation

## 3. GitHub Actions Workflows

- [x] 3.1 Create `.github/workflows/release.yaml` triggered on tag pushes (`v*`) using `goreleaser/goreleaser-action` with `permissions: contents: write`
- [x] 3.2 Update `.github/workflows/ci.yaml` to validate GoReleaser configuration on pull requests

## 4. Verification & Testing

- [x] 4.1 Run `go test -v ./...` and verify test suite passes
- [x] 4.2 Validate cross-compilation with `-ldflags` metadata injection
- [x] 4.3 Verify code formatting with `gofmt -l .`
