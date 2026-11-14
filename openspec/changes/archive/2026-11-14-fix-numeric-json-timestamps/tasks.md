## 1. Tests first

- [x] 1.1 Add a table test for the numeric helper in `internal/pipeline/json_test.go`: seconds with a fraction, milliseconds, microseconds and nanoseconds of the same moment (all show `.123` or `.500`), plus `0`, `-5`, `45` and a value after 2099 (all refused). Make the expected strings independent of the machine's time zone.
- [x] 1.2 Add `ParseJSONLog` tests for key selection: `{"msg":"GET /users","time":45,"ts":1789000000123}` takes its time from `ts` and keeps `time` = 45 in `Fields`; `timestamp` (string) wins over `ts`; `"time":{"ms":12}` and `"time":""` give no timestamp and stay in `Fields`.
- [x] 1.3 Check that the new tests fail on the current code for the microsecond, nanosecond and key selection cases.

## 2. Fix

- [x] 2.1 Add a helper in `internal/pipeline/json.go` that turns a number into a time: choose the unit by size (`1e11`, `1e14`, `1e17`), convert, and accept it only for the years 2000 to 2099 in UTC.
- [x] 2.2 Rewrite the timestamp step of `ParseJSONLog`: go through `timestamp`, `time`, `ts`, `@timestamp` in order, accept a non-empty string or a number the helper accepts, delete only the accepted key, and keep every other key in the fields. Format numeric times as `15:04:05.000` in local time, as before.
- [x] 2.3 Confirm that the existing tests still pass (Winston string timestamp, Pino milliseconds, Zap seconds) and that `--json` output does not change.

## 3. Finish

- [x] 3.1 Add an `## Unreleased` section with a `### Fixed` entry to `CHANGELOG.md`: microsecond and nanosecond timestamps now show the right time, and a number in `time` (or another timestamp key) outside 2000 to 2099 now stays a field, for example `time=45`.
- [x] 3.2 Run `gofmt -l .`, `go vet ./...` and `go test ./cmd/... ./internal/... ./test/stress/... ./test/bench/...`.
- [x] 3.3 Run `openspec validate fix-numeric-json-timestamps`.
