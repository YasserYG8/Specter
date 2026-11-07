## 1. Parser

- [x] 1.1 Add `IsOutputDest func(dest string) bool` to `Parser`. For a descriptor other than 1 and 2, when it returns true for the decoded destination, parse the call like fd 1 and 2 and keep its real descriptor number. Otherwise keep the current early return and the `OnOtherFD` call.
- [x] 1.2 Parser tests: the real Next.js line `write(22</dev/pts/1>, ...)` with `-xx` hex encoding; `writev` to fd 22; an unfinished and resumed write to fd 22; fd 22 with a non-matching destination; fd 22 without an annotation; existing fd 3 tests unchanged.

## 2. Source

- [x] 2.1 Set `IsOutputDest` in `Source` from `s.refs`: true only when the destination is not empty, not `/dev/null`, and equals the target's fd 1 or fd 2 destination.
- [x] 2.2 Tests for the callback: terminal match, pipe match, `/dev/null` excluded, different destination, empty references, and the log-file notice still firing for a non-matching regular file.
- [x] 2.3 A pipeline test: `hel` on fd 1, `abc\n` on fd 22, `lo\n` on fd 1 for the same PID give the lines `abc` and `hello`, and `--stderr` hides the fd 22 line.

## 3. End-to-end

- [x] 3.1 Add a testserver role that opens `/proc/self/fd/1` again and writes numbered lines through the new descriptor, like libuv does for a TTY. Add an E2E test where its stdout is a pipe, and Specter must output every line.
- [x] 3.2 In WSL, run the weftmap copy with `pnpm run dev` in a terminal, attach the new build with `specter 3000`, request a few pages, and confirm every `GET` line from the terminal appears in Specter, with no extra lines.

## 4. Docs and checks

- [x] 4.1 `CHANGELOG.md` under Fixed. `README.md`: Node servers in a terminal are captured, and `--stderr` cannot separate stdout from stderr when both are the same terminal.
- [x] 4.2 `gofmt -l .`, `go vet ./...`, and `go test ./cmd/... ./internal/... ./test/stress/...` pass.
