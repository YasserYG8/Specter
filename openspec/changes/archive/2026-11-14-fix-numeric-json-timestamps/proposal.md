## Why

When a JSON log line has a numeric timestamp, Specter reads every value above `1e11` as milliseconds. So a timestamp in microseconds or nanoseconds shows the wrong time. The time still looks normal, because only `HH:MM:SS.mmm` is printed, so nobody notices. Specter's own test server writes `ts` in nanoseconds (`time.Now().UnixNano()`), so every bench line shows a wrong time today. The same code also takes any number in a `time` key as a timestamp. A field like `"time": 45`, which usually means a duration, turns into a fake clock time and disappears from the output.

## What Changes

- Choose the unit of a numeric timestamp by its size: seconds, milliseconds, microseconds or nanoseconds.
- Accept a numeric value as a timestamp only when it falls in a sane date range (the years 2000 to 2099). Otherwise the key stays a normal field, and it is shown as `key=value`.
- When a timestamp key is not accepted, try the next key in the list (`timestamp`, `time`, `ts`, `@timestamp`). A line like `{"time":45,"ts":1789000000123}` uses `ts` as its timestamp and keeps `time=45` as a field.
- An empty string, or a value that is neither a string nor a number (an object, an array, a boolean or null), is not a timestamp either. It stays a field.
- Time zones do not change. A numeric timestamp is still shown in local time, and an ISO string is still shown as written. This is left to the multi-stream change, where merged streams make it matter.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `json-formatter`: the extraction requirement gains rules for numeric timestamp units, the sane range, and which key becomes the timestamp.

## Impact

- Code: `ParseJSONLog` in `internal/pipeline/json.go` (the timestamp step only). The grouper and the renderer call it, and they do not change.
- Tests: new table tests in `internal/pipeline/json_test.go`. No test covers the numeric path today.
- Output: lines with microsecond or nanosecond timestamps show the right time. Lines with a numeric `time` (or other key) outside the range now show it as a field instead of a fake time. String timestamps and `--json` output do not change.
- No new dependencies.
