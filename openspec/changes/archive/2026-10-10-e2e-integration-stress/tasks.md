## 1. Test Server Fixture

- [x] 1.1 Implement pure Go HTTP test server fixture in `test/e2e/testserver/main.go` supporting `/json`, `/error`, `/burst`, and `/fork` worker processes
- [x] 1.2 Implement test harness helpers for server lifecycle, ephemeral port discovery, and client request firing

## 2. Live Process End-to-End Test Suite

- [x] 2.1 Implement `TestE2E_PortAttachment` verifying live stdout JSON log capture against running server in `test/e2e/e2e_test.go`
- [x] 2.2 Implement `TestE2E_MultilineErrorTrace` verifying atomic stderr stack trace grouping
- [x] 2.3 Implement `TestE2E_FilteringFlags` verifying `--level`, `--grep`, `--stderr`, `--json`, and `--no-color` against live streams
- [x] 2.4 Implement `TestE2E_TreeTracer` verifying child process log capture with `--tree`
- [x] 2.5 Implement `TestE2E_CleanDetachment` verifying clean teardown on context cancellation without orphan processes

## 3. High-Throughput Pipeline Stress Suite

- [x] 3.1 Implement `TestStress_PipelineBurst10k` in `test/stress/pipeline_stress_test.go` verifying 10,000 log line reconstruction without dropped frames
- [x] 3.2 Implement `TestStress_ConcurrentInterleaved` verifying multi-threaded concurrent stream safety under `-race`
- [x] 3.3 Implement `TestStress_ResourceTeardown` asserting bounded memory and zero goroutine leaks post-shutdown

## 4. CI Workflow Integration & Verification

- [x] 4.1 Update `.github/workflows/ci.yaml` to run E2E and stress test suites in CI
- [x] 4.2 Run full test suite with race detector across all packages and verify code formatting

