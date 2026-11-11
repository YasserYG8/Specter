## 1. Shared rule

- [x] 1.1 Add `internal/source/logfile.go` with `IsLogFile(path string, exists func(string) bool) bool`: whole-word `log`/`logs` match on base-name parts split by `.`, `-`, `_` and on folder names, case-insensitive
- [x] 1.2 Add the embedded database exception: base name `LOG`, `LOG.old` or `LOG.old.<digits>` (case-sensitive) with `exists(dir/CURRENT)` true returns false
- [x] 1.3 Add a table-driven test `internal/source/logfile_test.go` covering `/var/log/app.log`, `access_log`, `error-logs.txt`, `app.log.1`, `/var/log/syslog`, `~/.pm2/logs/api-out.txt`, `catalog.json`, `blog.sqlite`, `/data/app.db`, Turbopack `LOG` with `CURRENT`, `LOG.old.1700000000` with `CURRENT`, and `/srv/app/LOG` without `CURRENT`

## 2. Use it in both engines

- [x] 2.1 Replace the `strings.Contains(..., "log")` check in `checkLogFile` in `internal/source/strace/spawn.go` with `source.IsLogFile`, with an injectable `exists` function next to `isRegularFile`
- [x] 2.2 Replace the same check in `checkLogFile` in `internal/source/ebpf/source_linux.go` with `source.IsLogFile`, using an `os.Stat`-based `exists`
- [x] 2.3 Add strace tests in `internal/source/strace/spawn_test.go` (with `logNoticeSource`): no notice for `catalog.json` and for a Turbopack `LOG` next to `CURRENT`, and a notice for `/srv/app/LOG` without `CURRENT`
- [x] 2.4 Add an eBPF test in `internal/source/ebpf/source_linux_test.go` next to the existing log notice test: writes to `catalog.json` and `app.log` give one notice, for `app.log` only (skips without root, as the other eBPF tests do)

## 3. Docs and checks

- [x] 3.1 Update the notice line in `README.md` to say which files count as log files, and that embedded database logs such as Turbopack's are skipped
- [x] 3.2 Add an Unreleased entry to `CHANGELOG.md`
- [x] 3.3 Run `gofmt -l .`, `go vet ./...` and `go test ./cmd/... ./internal/...` on Windows, and the strace and eBPF tests in WSL2
- [x] 3.4 Attach to the WSL copy of weftmap (`~/specter-tests/weftmap`, `pnpm run dev`) and confirm that the Turbopack `LOG` notice no longer appears
