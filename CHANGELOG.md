# Changelog

## Unreleased

### Breaking

- `--level` with an unknown value is now an error that lists the valid levels. It used to be ignored, so `--level banana` showed every line. Aliases such as `warning`, `err` and `fatal` still work.

### Fixed

- Overlapping writes from threads and worker processes are no longer lost. With 4 worker processes writing 12,000 lines, Specter used to capture only a few hundred.
- `writev` calls are now captured.
- Lines up to 64 KB now arrive whole. A longer write keeps its line boundary and ends with `[specter: N more bytes not captured]`.
- A very long line of `strace` output no longer stops the capture.
- Every worker process of a server is now traced. Specter used to trace only the first 16 and printed no warning, so with 20 workers running before it attached, the last 4 lost all their logs. While attached, all workers are slowed, not just 16 of them.

### Added

- Notices on stderr when the target writes to its own log file, when nothing has been captured after 10 seconds, and when `--grep` is not a valid regular expression and is matched as literal text.
- Short flags `-l` (`--level`), `-g` (`--grep`), `-t` (`--tree`) and `-s` (`--source`). The README already listed them, but they were rejected with `unknown shorthand flag`.
