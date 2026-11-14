## Context

`ParseJSONLog` in `internal/pipeline/json.go` extracts the message, the level and the timestamp from a JSON log line. The timestamp step looks at `timestamp`, `time`, `ts` and `@timestamp` in that order. It takes the first key that exists, whatever its value, and deletes it from the fields. A string is kept as written. A number above `1e11` is read as milliseconds, and any other number as seconds. A value of another type is deleted and gives no timestamp.

This has three effects:

```
   "ts": 1789000000123456789 (ns)  →  read as ms  →  wrong time that looks right
   "time": 45 (a duration)         →  read as s   →  01:00:45.000 (in UTC+1), field lost
   "time": {...}                   →  no timestamp, field lost
```

The grouper and the renderer both get the parsed result from `ParseJSONLog` (the grouper keeps it on the entry since the faster-line-processing change), so a fix in this one function reaches all output. No test covers the numeric path today.

## Goals / Non-Goals

**Goals:**
- Show the right time for numeric timestamps in all four common units.
- Never drop a field that is not really a timestamp.
- Keep the output for string timestamps and for `--json` exactly as it is.

**Non-Goals:**
- Time zones. A numeric timestamp stays in local time, and an ISO string stays as written (often UTC). The multi-stream change will decide this, because merged streams are where two zones would show one moment at two clock times.
- Checking or reformatting string timestamps. Any non-empty string is still taken as written.
- New timestamp keys (for example `t` or `date`).

## Decisions

**1. Choose the unit by the size of the number.**
The thresholds are `1e11`, `1e14` and `1e17`. Each one sits between the values of the years 2000 to 2099 in two neighboring units, so a real timestamp from that range can never land in the wrong unit:

```
   unit   2000 … 2099 in that unit       next threshold
   ────   ─────────────────────────      ──────────────
   s      9.5e8  … 4.1e9                 < 1e11
   ms     9.5e11 … 4.1e12                < 1e14
   µs     9.5e14 … 4.1e15                < 1e17
   ns     9.5e17 … 4.1e18                (rest)
```

The kept `1e11` threshold means today's seconds and milliseconds behave the same as before. Conversion uses `time.Unix` with the fraction for seconds, and `time.UnixMilli`, `time.UnixMicro` and `time.Unix(0, n)` for the other units. A nanosecond value has passed through `float64` in `json.Unmarshal`, so it is exact only to about 256 ns. That is far below the millisecond the output shows.

*Alternative:* a flag or a per-key rule for the unit. Rejected, because loggers do not say which unit they use, and the size works for every real timestamp.

**2. Accept a number only for the years 2000 to 2099 (UTC).**
After the conversion, the year must be in this range. This turns away small numbers such as durations (`45`), counters, `0` and negative values, which all map to 1970. It also turns away absurd future values.

*Alternative:* no range check, only units. Rejected, because `"time": 45` would still become a fake time and lose its field.

**3. A key that is not accepted stays a field, and the search moves to the next key.**
Today the first key that exists wins, even with a useless value. Now only an accepted value is removed from the fields, and the next key is tried. So `{"time":45,"ts":…}` gets its time from `ts` and still shows `time=45`. An empty string, an object, an array, a boolean or null is not accepted either, for the same reason: the field should not vanish without a timestamp in its place.

**4. Keep the code in one small helper.**
A function such as `numericTimestamp(v float64) (time.Time, bool)` holds the unit and range logic. A table test can then cover it without building JSON lines, and `ParseJSONLog` stays short.

## Risks / Trade-offs

- [A number that is not a timestamp can still pass the range check, for example a duration of `2e9` nanoseconds (2 s), which looks like a time in 2033 in seconds.] → This is rare, and it happens today as well. The range check removes the common cases (small numbers). A key name is also needed: only the four timestamp keys are looked at.
- [Tests depend on the local time zone, because the output is local time.] → The tests build the expected string with the same `time` call in local time, or set `time.Local` to UTC for the test.
- [A line that used to show a (wrong) time now shows a field instead, for example `time=45`.] → This is the intended fix. It goes in the CHANGELOG under Fixed.

## Open Questions

None. The time zone question belongs to the multi-stream change.
