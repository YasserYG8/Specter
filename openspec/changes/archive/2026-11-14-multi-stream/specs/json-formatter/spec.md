## ADDED Requirements

### Requirement: Zoned String Timestamps in Local Time
When a JSON log's timestamp is a string in RFC 3339 form with a time zone (ending in `Z` or in an offset such as `+02:00`), the formatter SHALL show it as `HH:MM:SS.mmm` in local time, the same way as a numeric timestamp. A string timestamp without a time zone, or one that is not RFC 3339, SHALL be shown as before this change. `--json` and `--raw` output SHALL NOT change.

#### Scenario: UTC timestamp
- **WHEN** the local time zone is UTC+2 and a JSON line has `"timestamp":"2026-10-10T10:00:00.123Z"`
- **THEN** the timestamp is shown as `12:00:00.123`

#### Scenario: Timestamp with an offset
- **WHEN** the local time zone is UTC and a JSON line has `"time":"2026-10-10T10:00:00+02:00"`
- **THEN** the timestamp is shown as `08:00:00.000`

#### Scenario: Same moment from two formats
- **WHEN** one line has `"ts":1791626400123` and another has `"timestamp":"2026-10-10T10:00:00.123Z"`, which are the same moment
- **THEN** both lines show the same timestamp text

#### Scenario: No time zone
- **WHEN** a JSON line has `"time":"2026-10-10 10:00:00"`
- **THEN** the timestamp is shown as it is shown before this change
