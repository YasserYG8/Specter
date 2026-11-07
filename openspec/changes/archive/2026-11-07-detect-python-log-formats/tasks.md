## 1. Tests first

- [x] 1.1 Add a table test in `internal/pipeline` with the 16 probe lines from the proposal and design: the 6 Python formats with `INFO`, `WARNING` and `CRITICAL` variants, the timestamp variants with `,` and `.`, and the 2 lines that must not get a level (`Processing item 3 - INFO - ...` and `GET /api/ERROR-codes 200`). Each row checks `DetectSeverity` and `isRecordStart`. The cookbook, Celery and spaced-timestamp rows fail before the fix.

## 2. Regexes

- [x] 2.1 Widen the timestamp part of `positionalWordRegex` as in the design.
- [x] 2.2 Add `cookbookLevelRegex` and `celeryLevelRegex` to `rules.go`, anchored at the start of the line.
- [x] 2.3 Use them in the positional rank of `DetectSeverity` and in `isRecordStart`.

## 3. Checks

- [x] 3.1 Run the existing parser, grouper, filter and property tests unchanged, so every format that worked still works.
- [x] 3.2 A filter test: with `--level warn`, the cookbook `INFO` line is hidden and the `WARNING` line is shown.
- [x] 3.3 `CHANGELOG.md` under Fixed, and the README if it lists recognized formats.
- [x] 3.4 `gofmt -l .`, `go vet ./...`, and `go test ./cmd/... ./internal/... ./test/stress/...` pass.
