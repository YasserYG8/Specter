## Context

`DetectSeverity` in `internal/pipeline/parser.go` ranks its rules: structured keys (`"level":`, `level=`), then positional tokens (bracketed `[INFO]`, Monolog `app.INFO:`, columnar `| INFO |`, .NET `info:`, and `positionalWordRegex`), then the HTTP status. `isRecordStart` in `grouper.go` uses the same regexes to decide that a line starts a new record.

`positionalWordRegex` is:

```
^(?:\[[^\]]+\]\s+)?(?:[0-9T:.\-+Z]{10,35}\s+)?(?:\[[^\]]+\]\s+)?(LEVEL)\b
```

Its timestamp part allows no space and no comma, so `2026-11-07 10:12:50,516 INFO` never matches: the date ends at the space, and `10:12:50,516` is not the level. A probe with 16 lines confirmed which formats work and which don't (see the proposal).

## Goals / Non-Goals

**Goals:**
- Give a level to the three failing Python formats.
- Never take a level word from inside a message.
- Keep every format that works today working.

**Non-Goals:**
- A general "any level word near the start" rule. It was rejected because a wrong ERROR is worse than a missing INFO.
- Changing how HTTP status codes rank against level words.
- Formats from other languages, except where the timestamp fix helps them for free.

## Decisions

### 1. Widen only the timestamp part

Replace the timestamp alternative with one that also accepts a date, one space, and a time with an optional `,` or `.` fraction and an optional zone:

```
(?:\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?|[0-9T:.\-+Z]{10,35})
```

The rest of the regex is unchanged: anchored at the start, the level word must follow the timestamp and optional bracket groups. The new branch is tried first, and the old branch stays for other timestamp shapes.

### 2. Two anchored patterns for the dashed and Celery formats

- `cookbookLevelRegex`: `^<timestamp> - [^ ]+(?: [^ ]+)* - (LEVEL) - `. The logger name may hold dots and underscores. The pattern requires the timestamp at the start, so `Processing item 3 - INFO - ...` doesn't match.
- `celeryLevelRegex`: `^\[<timestamp>: (LEVEL)/[^\]]+\]`.

Both join the positional rank in `DetectSeverity` and the record-start checks in `isRecordStart`, after the existing positional patterns. They can't match lines the older patterns match, so order only matters for speed.

Alternative: one loose regex for "level between separators". It was rejected for the false-positive reason above.

### 3. The level list

`WARNING` and `CRITICAL` are already in the level alternation, and `ParseRankedSeverity` maps them to `WARN` and `ERROR`. The new regexes reuse the same alternation.

## Risks / Trade-offs

- [The wider timestamp makes `positionalWordRegex` match lines that start with a date and then a word that happens to be `INFO`, such as a report line] → The level word must come right after the timestamp, or after one bracket group. That's the same strictness as today, only for a second timestamp shape.
- [Regex cost on every line] → Both new patterns are anchored at the start and fail on the first characters for most lines.

## Migration Plan

None. Lines that had no level now have one, so `--level` hides more of the lines it should.

## Open Questions

None.
