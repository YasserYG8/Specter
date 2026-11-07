# Changelog

## Unreleased

### Breaking

- A bare number from 1 to 65535 is now a port and nothing else. It used to fall back to the process with that PID when nothing listened on the port, so `specter 3000` with your server down could attach `strace` to an unrelated process (and slow it) while only printing `Attaching to PID 3000 (systemd-journald)`. Now Specter prints "No process is listening on port 3000" with the active ports, exits with an error and attaches to nothing. If a process with that PID exists, the message ends with `Hint: PID 3000 exists (systemd-journald). To attach to it: specter --pid 3000`. To attach by PID, use the new `--pid` (`-p`) flag, for example `specter --pid 14280`. A number above 65535 cannot be a port, so `specter 70000` still attaches to PID 70000.
- `--level` with an unknown value is now an error that lists the valid levels. It used to be ignored, so `--level banana` showed every line. Aliases such as `warning`, `err` and `fatal` still work.

### Fixed

- Overlapping writes from threads and worker processes are no longer lost. With 4 worker processes writing 12,000 lines, Specter used to capture only a few hundred.
- `writev` calls are now captured.
- Lines up to 64 KB now arrive whole. A longer write keeps its line boundary and ends with `[specter: N more bytes not captured]`.
- A very long line of `strace` output no longer stops the capture.
- `--grep` now shows a whole stack trace when any of its lines matches. It used to show only the matching line and the lines after it, so `-g TypeError` on a Python traceback printed the last two lines without the header and frames, and a match on a frame lost the header. A trace with no matching line is still hidden. While `--grep` is set, Specter holds a trace until it ends (its last line, the next unrelated line, 200 ms of silence, or the end of the input), so lines from other processes can appear before it. A trace is matched on its first 1,000 lines. Without `--grep`, nothing is held.
- Lines that never reached the server's output are no longer shown. A Next.js dev server starts helper processes (such as `get-registry.js` and `telemetry/project-id.js`) whose output goes to a private pipe or `/dev/null`, but Specter printed it as if the server had logged it. Now a child process is shown only when its output goes to the same place as the server's own stdout or stderr. Workers that share the server's output are still shown, and so is everything the server writes itself, even when its stdout is `/dev/null`. If Specter cannot tell where a write went, it shows it.
- Every worker process of a server is now traced. Specter used to trace only the first 16 and printed no warning, so with 20 workers running before it attached, the last 4 lost all their logs. While attached, all workers are slowed, not just 16 of them.

### Added

- Notices on stderr when the target writes to its own log file, when nothing has been captured after 10 seconds, and when `--grep` is not a valid regular expression and is matched as literal text.
- Short flags `-l` (`--level`), `-g` (`--grep`), `-t` (`--tree`) and `-s` (`--source`). The README already listed them, but they were rejected with `unknown shorthand flag`.
