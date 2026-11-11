## Why

When Specter is attached to a Next.js dev server, it prints `Target writes to its own log file .../.next/dev/cache/turbopack/<id>/LOG`. That file is the internal log of Turbopack's cache database, not the server's log, so the notice sends new users to a file they don't need and makes them think output is missing. The current rule ("the file name contains `log`") also matches names such as `catalog.json`, `blog.sqlite` and `dialog.db`. Next.js is one of the most common targets, so this wrong notice shows up on many first runs.

## What Changes

- The own log file notice matches `log` only as a whole word: a `.log` extension, a part of the file name equal to `log` or `logs` (parts are separated by `.`, `-`, `_`), or a parent folder named `log` or `logs`. Names that only contain the letters, such as `catalog.json`, no longer trigger it.
- A file named `LOG` or `LOG.old` with a file named `CURRENT` in the same folder is treated as the internal log of an embedded database (LevelDB and RocksDB layout, used by Turbopack, Chrome, Electron apps and others) and does not trigger the notice.
- One shared function decides whether a path is a log file. The `strace` and `ebpf` engines both use it, so they can't drift apart. Today the rule is copied in both.
- The notice text does not change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `capture-notices`: the "Own Log File Notice" requirement changes from "a regular file whose name contains `log`" to the whole-word rule, adds the embedded database exception, and says the rule applies to both the `strace` and `ebpf` engines.

## Impact

- Code: `internal/source/strace/spawn.go` (`checkLogFile` name check), `internal/source/ebpf/source_linux.go` (`checkLogFile`), and a new shared helper in `internal/source`.
- Tests: unit tests for the helper, and the existing log notice tests in both engines.
- Docs: the notice description in `README.md` and a `CHANGELOG.md` entry.
- No new dependencies, flags or output changes for real log files such as `/var/log/app.log` or `~/.pm2/logs/app-out.log`.
