## 1. Check real strace output first

- [x] 1.1 In WSL, run `strace -f -qq -xx -y -s 65536 -e trace=write,writev` on a small program that writes to a terminal, a pipe, `/dev/null` and a file, using both `write` and `writev`, and with two threads so that `<unfinished ...>` appears. Save the lines as test fixtures, and record whether the `<...>` path is hex-encoded under `-xx`.
- [x] 1.2 Run the same command on the agentgraph Next.js server on port 3000 while `test_requests.sh` runs, and confirm that the fake helper lines carry a destination that differs from `next-server`'s fd 1. Do not save any config values from that output.

## 2. Parser

- [x] 2.1 Extend `callStartRegex` to accept an optional `<PATH>` after the fd, and decode PATH with `DecodeStraceHex` when it is hex.
- [x] 2.2 Add the destination to what the parser returns for a call, and to `pendingCall`, so it survives `<unfinished ...>` and `<... resumed>`.
- [x] 2.3 Parser tests from the fixtures of 1.1: `write` and `writev` with a pipe, a terminal, `/dev/null`, and a split call. Keep all existing tests passing, since lines without `<PATH>` must still parse.

## 3. Emit rule

- [x] 3.1 Add `-y` to `BuildStraceArgs` and update its test.
- [x] 3.2 At start, read `/proc/<target>/fd/1` and `/proc/<target>/fd/2` through the existing injectable `procRoot` and `readlink`, before strace is started.
- [x] 3.3 Add a pure function `shouldEmit(pid, dest, targetPID, refs)` that applies the rule from the spec, and use it in `readLoop` before a chunk is sent.
- [x] 3.4 Table tests for `shouldEmit` covering each scenario in the spec: private pipe, `/dev/null`, shared stdout, child stdout equal to the target's stderr, target writing to `/dev/null`, unknown destination, and unreadable references.

## 4. End-to-end and stress

- [x] 4.1 Add a testserver role that starts one child with stdout set to a private pipe it drains and discards, and one child that inherits stdout. Add an E2E test that the first child's lines are not output and the second child's lines are.
- [x] 4.2 Run the stress test and `TestE2E_ManyExistingWorkers` with and without `-y`, and write the timing difference into this change.
- [x] 4.3 Run all E2E tests in WSL with `ptrace_scope=0` after asking the user, then set it back to 1.
- [x] 4.4 Repeat the agentgraph comparison from 1.2 with the built binary: `specter 3000` must show no helper lines, and every line in the real terminal must still be shown.

## 5. Docs and checks

- [x] 5.1 Add an entry to `CHANGELOG.md` under Unreleased → Fixed.
- [x] 5.2 Update the README if it describes which processes are captured, and add the `-y` cost if 4.2 found it large.
- [x] 5.3 `gofmt -l .`, `go vet ./...`, and `go test ./cmd/... ./internal/... ./test/stress/...` pass, and the build for Windows and macOS still compiles.
