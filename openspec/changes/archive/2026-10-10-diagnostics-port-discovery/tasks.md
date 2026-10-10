## 1. Core Port Discovery & Ranking

- [x] 1.1 Define `ListeningPort` struct and `DiscoverListeningPorts` in `internal/resolver` using `ss -H -ltnp` with `lsof` fallback
- [x] 1.2 Implement process metadata enrichment (reading `/proc/<pid>/cmdline` or parsing `users:(...)` in `ss`)
- [x] 1.3 Implement heuristic ranking algorithm calculating numeric proximity, edit distance, and runtime weightings
- [x] 1.4 Write comprehensive unit tests for `DiscoverListeningPorts` and suggestion ranking using mock runner fixtures

## 2. Structured Diagnostics & Error Types

- [x] 2.1 Define `NoProcessOnPortError` in `internal/resolver` embedding requested port, candidate ports, and top suggestion
- [x] 2.2 Update `ResolvePort` in `internal/resolver/resolver.go` to bundle discovered ports into `NoProcessOnPortError` on miss
- [x] 2.3 Implement terminal formatting helper producing formatted active listener bullets and `Did you mean: specter <port> ?`

## 3. CLI Integration & Terminal Reporting

- [x] 3.1 Update `cmd/specter/main.go` to detect `NoProcessOnPortError` and print formatted diagnostic report to `os.Stderr`
- [x] 3.2 Ensure port discovery reporting strictly respects `--no-color`, `NO_COLOR` environment variable, and non-TTY pipes
- [x] 3.3 Add integration test asserting CLI diagnostic output and suggestion when targeting an unallocated port

## 4. Verification & Validation

- [x] 4.1 Verify all tests compile and pass with race detector: `go test -v -race ./...`
- [x] 4.2 Verify canonical code formatting: `gofmt -l .` returns zero differences
