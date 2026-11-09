# attach-duration Specification

## Purpose
Let Specter detach by itself after a set time, so a target is slowed only for as long as the user needs.

## Requirements

### Requirement: Automatic Detach After a Duration
The system SHALL accept `--duration <time>`, for example `--duration 30s` or `--duration 2m`. The time SHALL start when the source has started. When it ends, Specter SHALL stop as it does on Ctrl+C, print that it detached after that time, and exit with status 0. If the target ends before the time is over, the system SHALL report that as it does today and exit with status 1. A value that is negative, or is not a duration, SHALL be rejected before anything is attached, with a message that shows valid examples. When the flag is not set, or is 0, there SHALL be no time limit. The flag SHALL work with every source.

#### Scenario: Detach after the time
- **WHEN** the user runs `specter 3000 --duration 30s` and the target keeps running
- **THEN** after 30 seconds Specter prints `Detached from PID <pid> after 30s.` and exits with status 0, and the target has no tracer

#### Scenario: Target ends first
- **WHEN** the target exits 10 seconds after Specter attached, with `--duration 30s`
- **THEN** Specter prints how the target ended and exits with status 1

#### Scenario: Ctrl+C before the time
- **WHEN** the user presses Ctrl+C before the time is over
- **THEN** Specter detaches as usual and prints that it detached cleanly

#### Scenario: Missing unit
- **WHEN** the user runs `specter 3000 --duration 30`
- **THEN** Specter exits with a non-zero status before attaching, and the message shows `30s` and `2m` as examples

#### Scenario: Negative value
- **WHEN** the user runs `specter 3000 --duration -5s`
- **THEN** Specter exits with a non-zero status before attaching, and says the duration must be positive

#### Scenario: Container
- **WHEN** the user runs `specter web --duration 10s` and the source is Docker
- **THEN** Specter stops following the container after 10 seconds and exits with status 0
