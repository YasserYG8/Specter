## 1. Process Tree Child Discovery

- [x] 1.1 Add `Children []int` field to `source.Target` in `internal/source/types.go`
- [x] 1.2 Implement `FindChildPIDs(parentPID int) ([]int, error)` in `internal/resolver/children.go` that inspects `/proc/*/status` for matching PPid
- [x] 1.3 Add unit tests in `internal/resolver/children_test.go` verifying child PID discovery and edge cases

## 2. Multi-PID Strace Attachment

- [x] 2.1 Update `strace.Source.Start` in `internal/source/strace/spawn.go` to append `-p <child_pid>` for all discovered children in `target.Children`
- [x] 2.2 Handle transient startup attach notices when child processes exit or recycle
- [x] 2.3 Add unit test in `internal/source/strace/spawn_test.go` verifying multi-PID argument construction

## 3. CLI Integration & Verification

- [x] 3.1 Update `cmd/specter/main.go` to discover child processes and populate `target.Children` upon target resolution
- [x] 3.2 Update attachment diagnostic header in `cmd/specter/main.go` to inform the user when child worker PIDs are detected
- [x] 3.3 Add E2E integration test in `test/e2e/e2e_test.go` verifying automatic log capture from pre-existing child worker processes
- [x] 3.4 Build and verify against a live Uvicorn `--reload` server in WSL
