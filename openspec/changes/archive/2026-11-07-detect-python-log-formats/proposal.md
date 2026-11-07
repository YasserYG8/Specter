## Why

`--level` does not work on many Python servers. On a FastAPI project (Tajweed AI), `specter 8120 -l warn` still showed 64 of 67 lines, because its log lines look like this:

```
2026-11-07 10:12:50,516 - TajweedPipeline - INFO - Starting pipeline processing
```

Specter found no level in them, and lines without a level always pass `--level`, so that unknown output is never hidden. A probe of common Python formats against the current parser gave:

| Format | Example | Level found |
|---|---|---|
| `basicConfig()` default | `INFO:app:Starting pipeline` | yes |
| Gunicorn | `[2026-11-07 10:12:50 +0000] [1234] [INFO] Booting worker` | yes |
| loguru | `2026-11-07 10:12:50.516 \| INFO     \| app.main:run:42 - Starting` | yes |
| logging cookbook | `2026-11-07 10:12:50,516 - app - INFO - Starting` | **no** |
| Celery | `[2026-11-07 10:12:50,516: ERROR/ForkPoolWorker-1] Task ... raised` | **no** |
| `%(asctime)s %(levelname)s ...` | `2026-11-07 10:12:50,516 WARNING [app] disk almost full` | **no** |

The last row fails because the positional rule only accepts timestamps without spaces or commas, and Python's default `asctime` is `2026-11-07 10:12:50,516`. Other languages print timestamps the same way.

## What Changes

- The positional level rule accepts a timestamp of the form `YYYY-MM-DD HH:MM:SS` with an optional `,` or `.` fraction, so `<timestamp> LEVEL ...` lines get their level.
- Two anchored patterns are added: the logging cookbook format `<timestamp> - <name> - LEVEL - ` and the Celery format `[<timestamp>: LEVEL/<process>]`. Both must start at the beginning of the line, so a level word in the middle of a message is never taken.
- Lines in these formats start a new record, so a following continuation line or traceback groups correctly.
- No change to HTTP status handling: a 4xx response logged at `INFO` still counts as `WARN`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `log-parser`: the severity classification requirement gets scenarios for these Python formats and for level words inside a message.

## Impact

- `internal/pipeline/rules.go`: the timestamp part of `positionalWordRegex`, plus two new regexes.
- `internal/pipeline/parser.go` (`DetectSeverity`) and `grouper.go` (`isRecordStart`): use the new regexes in the positional rank.
- A table test in `internal/pipeline` with one real line per format, including the 5 that already work and 2 lines that must not get a level.
- `CHANGELOG.md` (Fixed). The README, if it lists recognized log formats.
