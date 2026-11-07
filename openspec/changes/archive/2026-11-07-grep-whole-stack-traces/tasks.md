## 1. Tests that show the bug

- [x] 1.1 Add table tests in `internal/pipeline` that run lines through the grouper, the new holder and the filter with `--grep`: a Python traceback matched on its header, a frame and its trailer; a chained Python traceback; Java and Node traces matched on the header and a frame; a trace with no matching line. Without the fix, the trailer and frame cases fail.

## 2. Grouper

- [x] 2.1 Add `EndsTrace` to `Entry`, and set it on the entry whose Python trailer line closes a trace.
- [x] 2.2 Remove `streamRecord.matchedGrep` and the mutex inside `Group`. Existing grouper tests still pass.

## 3. Trace holder

- [x] 3.1 Add the `TraceHolder` stage with the rules from the design: start on a trace header, add continuations, release on `EndsTrace`, on a non-continuation line on the same key, after 200 ms of silence on that key, at 1000 lines, and on input close. Entries on other keys pass through at once.
- [x] 3.2 On release, set `MatchedGrep = true` on every held entry when any of them matches, using the filter's own matching so literal and regex patterns behave the same.
- [x] 3.3 Tests for the timeout (with an injectable clock or a short timeout), the 1000-line cap, input close, and two keys where one holds a trace and the other keeps flowing.

## 4. Filter and wiring

- [x] 4.1 In `Filter.Matches`, admit entries with `MatchedGrep` in the grep check and set `activeMatch` for their key. Checks for `--stderr` and `--level` still run first.
- [x] 4.2 In `main.go`, add the holder between the grouper and the output loop only when `--grep` is set.
- [x] 4.3 A test that without `--grep` the pipeline output is unchanged, line for line, for the existing fixtures.

## 5. Real check, docs and checks

- [x] 5.1 In WSL, run `python3 test_server.py`, then `specter 8080 -g TypeError` (the exception that `GET /error` raises), and request `GET /error`. The whole traceback must be shown. Done: also checked with `-g handle_request` (a frame) and a pattern that matches nothing.
- [x] 5.2 Add an entry to `CHANGELOG.md` under Unreleased → Fixed. Update the README's `--grep` description if it explains how traces are matched.
- [x] 5.3 `gofmt -l .`, `go vet ./...`, and `go test -race ./cmd/... ./internal/... ./test/stress/...` pass (`-race` in WSL, since it needs cgo).
