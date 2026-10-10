## 1. Shell Completion Subcommand

- [x] 1.1 Implement `newCompletionCmd(rootCmd)` in `cmd/specter/completion.go` supporting `bash`, `zsh`, `fish`, and `powershell`
- [x] 1.2 Register `completion` subcommand in `newRootCmd()` in `cmd/specter/main.go`
- [x] 1.3 Add unit tests in `cmd/specter/completion_test.go` verifying completion script generation for all 4 shells

## 2. Dynamic Port & Flag Autocompletion

- [x] 2.1 Implement `ValidArgsFunction` on the root command querying active listening ports via `DiscoverListeningPorts` with a 300ms timeout
- [x] 2.2 Format dynamic completions with port number and process description (`<port>\t<processName> (PID <pid>)`) with `ShellCompDirectiveNoFileComp`
- [x] 2.3 Register dynamic flag completion functions for `--source` and `--level` with inline descriptions
- [x] 2.4 Add unit tests for `ValidArgsFunction` and flag completions using mock listeners

## 3. CLI Help Screen Polish & Real-World Examples

- [x] 3.1 Populate `cmd.Example` on `rootCmd` with real-world port attachment, level filtering, grep, and tree tracing recipes
- [x] 3.2 Add unit test verifying `--help` output renders examples cleanly

## 4. Verification & Validation

- [x] 4.1 Run full test suite with race detector: `go test -v -race ./...`
- [x] 4.2 Verify code formatting: `gofmt -l .`

