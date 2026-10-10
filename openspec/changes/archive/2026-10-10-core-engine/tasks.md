## 1. Port Resolver Implementation

- [x] 1.1 Implement port-to-PID resolution in `internal/resolver/port.go` using `lsof` with fallback to `ss`
- [x] 1.2 Implement target process metadata and descriptor inspection in `internal/resolver/process.go`
- [x] 1.3 Add unit tests with mocked command outputs in `internal/resolver/resolver_test.go`

## 2. Strace Source Implementation

- [x] 2.1 Implement strace hex decoder and syscall parser in `internal/source/strace/parse.go`
- [x] 2.2 Implement multi-thread `<unfinished ...>` and `<... resumed>` state reassembly in `internal/source/strace/parse.go`
- [x] 2.3 Implement `StraceSource` spawner with context cancellation in `internal/source/strace/spawn.go`
- [x] 2.4 Add unit tests using recorded strace fixtures in `internal/source/strace/parse_test.go`

## 3. Line Assembler Implementation

- [x] 3.1 Implement per-(PID, FD) byte buffer and newline splitting in `internal/pipeline/assembler.go`
- [x] 3.2 Implement UTF-8 stream decoding handling multibyte splits across writes
- [x] 3.3 Implement 150ms idle timer flush for interactive prompt output
- [x] 3.4 Add unit tests for `LineAssembler` in `internal/pipeline/assembler_test.go`

## 4. CLI Wiring and Integration

- [x] 4.1 Wire Resolver, StraceSource, and LineAssembler in `cmd/specter/main.go`
- [x] 4.2 Handle `os.Interrupt` (SIGINT/SIGTERM) to guarantee graceful strace detachment
- [x] 4.3 Validate end-to-end attach and stream against a live local test server
