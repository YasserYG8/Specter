# Contributing to Specter

Thanks for helping. Bug reports, fixes, tests, docs and new platform support are all welcome.

Specter attaches to a server that is already running and streams its live stdout and stderr to your terminal. See the [README](README.md) for how to use it.

## Where to help

The biggest open job is native macOS and Windows support. Today Specter runs only on Linux and WSL2, because it depends on `/proc`, `lsof` or `ss`, and `strace`. The binaries for other systems build, but exit with an error at startup. Capturing terminal output without `strace` is the hardest part, and it needs research as well as code.

You can also help by:

- Testing Specter with your own stack and reporting what breaks.
- Adding log samples to `test/fixtures/` for frameworks whose logs or stack traces don't group correctly.
- Fixing the places where the docs and the code disagree.

For anything large, open an issue first so we can agree on the approach before you write the code.

## Reporting a bug

Open an [issue](https://github.com/YasserYG8/Specter/issues) and include:

- The output of `specter version`.
- Your distribution, and whether you're on WSL2.
- The command you ran, and what you expected to happen.
- The `[specter] Source:` line from the output, which shows the capture method.

Logs can contain tokens and personal data, so remove anything sensitive before you paste them.

## Setting up

You need the Go version in `go.mod`. To run Specter and its live tests, you also need Linux or WSL2 with `strace`, `lsof` and `iproute2`:

```bash
sudo apt install strace lsof iproute2   # Debian and Ubuntu

git clone https://github.com/YasserYG8/Specter.git
cd Specter
go build -o specter ./cmd/specter
```

You can write code and run the unit tests on Windows, but Specter itself won't attach to anything there.

To try a change by hand, start the test server in one terminal. It listens on port 8080, logs JSON to stdout, and writes a stack trace to stderr when you request `/error`:

```bash
python3 test_server.py
```

Then attach from a second terminal, and send some requests from a third:

```bash
./specter 8080
curl localhost:8080/
curl localhost:8080/error
```

If attaching is denied, your system probably restricts `ptrace`. The [README](README.md#kernel-permissions-ptrace_scope) lists the options.

## Testing

CI runs these checks, so run them before you open a pull request:

```bash
gofmt -l .      # must print nothing
go vet ./...
go test -race ./cmd/... ./internal/... ./test/stress/...
go test -timeout=10m ./test/e2e/...
```

To run one test:

```bash
go test ./internal/pipeline -run TestName -v
```

The E2E tests skip themselves when they aren't on Linux, when `strace` is missing, or when `ptrace` is restricted. A pass with skipped tests doesn't cover live capture, so check for skips with `-v`, and run them in WSL2 or Linux with `kernel.yama.ptrace_scope=0`.

## Rules for code changes

- **Read-only:** never signal, modify or restart a target process, and never open a network port. On interrupt, clean up so no `strace` stays attached.
- **Output streams:** log content goes to stdout. Specter's status messages and the picker go to stderr, which keeps `specter 3000 | grep 500` working.
- **External tools:** call them through `os/exec` with a context. Use the Docker CLI, not an SDK.
- **Portability:** the code must still compile for Windows and macOS, because CI cross-compiles for both.
- **Errors:** when a user can fix a failure, use `DiagnosticError` from `internal/util/errors.go`, with the cause and the exact fix.
- **Dependencies:** keep them to a minimum, and explain any new one in your pull request.

[CLAUDE.md](CLAUDE.md) explains how the resolver, sources and pipeline fit together.

## Changing behavior

Requirements for each feature are in `openspec/specs/<capability>/spec.md`. Read the relevant spec before you change how something behaves, and update it in the same pull request.

Larger changes follow the OpenSpec flow, with a proposal, design and task list. The steps are in `.agent/workflows/`, and past examples are in `openspec/changes/archive/`.

## Pull requests

1. Create a branch from `main`.
2. Keep each pull request to one change, and add tests for new behavior.
3. Write commit messages in the style the project uses, such as `feat(ui): add project column to picker` or `fix(resolver): handle missing lsof`.
4. In the description, explain what changed and why, and say how you tested it, including whether the E2E tests ran or skipped.

## License

Specter is [MIT licensed](LICENSE). By contributing, you agree that your contribution is released under the same license.
