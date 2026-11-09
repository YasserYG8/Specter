# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Specter is a Linux-first Go CLI that attaches to a running server by port, PID, container name, or project name, and streams its live stdout/stderr to the terminal. Running it with no arguments opens an interactive picker.

## Commands

```bash
go build -o specter ./cmd/specter             # build
gofmt -l .                                    # CI fails if this lists any file
go vet ./...

go test ./cmd/... ./internal/... ./test/stress/... ./test/bench/...  # unit, stress and benchmark-tool tests
go test -race ./cmd/... ./internal/... ./test/stress/... ./test/bench/...  # as CI runs them (-race needs cgo)
go test -timeout=10m ./test/e2e/...                              # live E2E
go test ./internal/pipeline -run TestName -v                     # single test
go test ./test/e2e -run TestE2E_PortAttachment -v                # single E2E test
```

```powershell
# Cross-compile for Linux from Windows PowerShell
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o specter ./cmd/specter
```

## Platform constraints

- Development happens on Windows, but the tool only runs on Linux or WSL2. `RunE` in `cmd/specter/main.go` returns an error on any other OS.
- The code still has to compile for Windows and macOS: CI cross-compiles both to check portability, but releases (GoReleaser) publish Linux binaries only, since Specter cannot attach on those systems. The only build tags are in `internal/source/strace/spawn_linux.go` and `spawn_other.go` (the parent-death signal that makes `strace` detach when Specter is killed, which exists only on Linux); elsewhere keep code portable, or add tags deliberately and a stub for the other systems.
- E2E tests skip themselves (not fail) when not on Linux, when `strace` is missing, or when ptrace is restricted. A passing run on Windows says nothing about them, so run them in WSL2 with `strace` and `lsof` installed and `kernel.yama.ptrace_scope=0`.
- To measure how much Specter slows a server, run `scripts/bench-overhead.sh` in WSL2 or on Linux as root (see its header; `READER=bpftrace` measures the eBPF spike in `experiments/ebpf-spike/`). Past reports are in `test/bench/results/`.
- E2E tests compile their own binaries, or use prebuilt ones from `SPECTER_BIN` and `TESTSERVER_BIN`.
- For manual testing in WSL2, `python3 test_server.py` listens on port 8080 and emits JSON logs on stdout. `GET /error` writes a traceback to stderr. Then run `specter 8080`.

## Architecture

The flow is resolver, then source, then pipeline, and it is wired together in the single `RunE` in `cmd/specter/main.go`.

```
arg or picker -> resolver -> source.Target -> selector -> Source
              -> chan Chunk -> LineAssembler -> StackTraceGrouper -> Filter -> Renderer -> stdout
```

**Argument resolution** (`main.go`): a positive integer of 65535 or lower is a port and nothing else: an empty port returns `NoProcessOnPortError` (with a `Hint:` line when a process has that PID) and never falls back to a PID. A larger integer is a PID. `--pid` / `-p` attaches by PID for any value and cannot be combined with a positional target. The numeric rules live in `cmd/specter/target.go`. Port errors other than "no process found" are returned immediately. Anything else is tried as a container, then matched case-insensitively against discovered project names.

**`internal/resolver`** produces a `source.Target`. It shells out to `lsof`, `ss`, and `docker`, and reads `/proc`. External commands and the proc root are injectable (`CommandRunner`, `procRoot`), which is how tests run on Windows without those tools. `discovery.go` and `project.go` supply the data for the picker and shell completions.

**`internal/source/selector`** picks the capture engine, and the first match wins: `--source` override, Docker, systemd unit, regular file, then strace. `.agent/architecture.md` lists file before systemd, but the code is authoritative.

**`internal/source/*`** contains the `docker`, `file`, `journal`, and `strace` implementations of the `source.Source` interface. They all emit the same `Chunk{Bytes, FD, PID, TS}`, so nothing downstream knows which source ran. If an auto-selected non-strace source fails immediately at startup, `main.go` falls back to strace. That fallback does not apply when `--source` is set.

**`internal/pipeline`** has two channel stages, `LineAssembler` (per-`(pid, fd)` buffers) and `StackTraceGrouper`. `Filter` and `Renderer` are plain calls in the output loop in `main.go`. Severity detection in `parser.go` and JSON handling in `json.go` are helpers rather than stages of their own. Filtering runs on grouped entries, which keeps stack traces whole.

**`internal/util/errors.go`** defines `DiagnosticError` (title, explanation, remediation). Use it for failures the user can act on.

## Invariants

- **Read-only:** never signal, modify, or restart a target process, and never open a network port. On SIGINT, tear down cleanly so no `strace` stays attached.
- **Output streams:** while attached, log content goes to stdout, and Specter's status messages and the picker go to stderr. This is what keeps `specter 3000 | grep 500` working.
- **Dependencies:** external tools are called through `os/exec` with a context. Docker is used through its CLI, not an SDK.

## Specs and workflow

- `openspec/specs/<capability>/spec.md` holds the requirements for each capability, and is worth reading before changing behavior. Past changes are in `openspec/changes/archive/`.
- New changes follow the OpenSpec flow (propose, apply, sync, archive), which uses the `openspec` CLI. The steps are in `.agent/workflows/opsx-*.md`.
- `.agent/architecture.md` explains the design rationale and strace edge cases such as unfinished/resumed syscalls and hex decoding. It holds historical design notes (draft v2), so its file layout and some details are out of date. The code and `openspec/specs/` are the source of truth.
