## 1. Short flags

- [x] 1.1 Add tests in `cmd/specter/main_test.go` that `-l`, `-g`, `-t` and `-s` map to their long flags, and that `-l banana` gives the same error as `--level banana`, and confirm they fail today
- [x] 1.2 Change the four flag definitions in `main.go` to `StringVarP` and `BoolVarP`, and confirm the tests pass
- [x] 1.3 Reword the `--tree` flag help and the `--tree` example comment in `main.go`, and check them in `specter --help`

## 2. README

- [x] 2.1 Remove "non-invasively" and "non-invasive" from lines 22 and 116
- [x] 2.2 Add a performance section saying the `strace` source slowed test servers by about 60% to 90%, that it depends on how often the server writes, and that it ends on detach, and link to it from the Install section
- [x] 2.3 Correct `--tree` in lines 53, 92 and 152, saying direct workers are always traced and the flag adds deeper descendants
- [x] 2.4 Change `sudo specter 3000` on line 237 to `sudo "$(which specter)" 3000`
- [x] 2.5 Replace the Docker API and socket wording on lines 88, 113 and 217 with the `docker` CLI
- [x] 2.6 List the sources on lines 96-98 and 113-115 in the order the code checks them: Docker, systemd, file, then `strace`

## 3. Other docs

- [x] 3.1 Remove the `Flag shorthands` entry from `Known doc drift` in `CLAUDE.md`
- [x] 3.2 Remove the short flags example from `CONTRIBUTING.md`, keeping the rest of that list item accurate
- [x] 3.3 Add the short flags to the `Unreleased` section of `CHANGELOG.md`

## 4. Verify

- [x] 4.1 Run each `specter` command and flag in the README's CLI Reference on Linux, and confirm none fails with a flag error
  - Run 2026-11-02 in WSL2: 35 commands and flag combinations from the CLI Reference, none failed with a flag error. A `--bogus` control was rejected, and `-l banana` gave the same error as `--level banana`.
- [x] 4.2 Run `gofmt -l .`, `go vet ./...`, and `go test -race` for unit and stress tests on Linux
  - Run 2026-11-02 in WSL2 (Go 1.27): gofmt clean with CRLF stripped, vet clean, Windows and macOS builds pass, `-race` unit and stress tests pass.
- [x] 4.3 Run the E2E tests with `strace` and `ptrace_scope=0`, after asking the user to allow the setting change
  - Run 2026-11-02 in WSL2 on Go 1.27 (CI uses 1.26), strace 6.8, `ptrace_scope=0`: all 12 E2E tests pass. On a real target, `-l warn -s strace -t -g keep-me` showed only the matching warning, and 0 `strace` processes were left.
