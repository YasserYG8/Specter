# Changelog

## Unreleased

### Breaking

- `--level` with an unknown value is now an error that lists the valid levels. It used to be ignored, so `--level banana` showed every line. Aliases such as `warning`, `err` and `fatal` still work.

### Fixed

- Overlapping writes from threads and worker processes are no longer lost. With 4 worker processes writing 12,000 lines, Specter used to capture only a few hundred.
- `writev` calls are now captured.
- Lines up to 64 KB now arrive whole. A longer write keeps its line boundary and ends with `[specter: N more bytes not captured]`.
- A very long line of `strace` output no longer stops the capture.

### Added

- Notices on stderr when the target writes to its own log file, when nothing has been captured after 10 seconds, and when `--grep` is not a valid regular expression and is matched as literal text.
