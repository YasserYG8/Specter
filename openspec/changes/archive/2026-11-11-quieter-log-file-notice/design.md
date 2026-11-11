## Context

Both capture engines tell the user once when the target writes to its own log file. The check is copied in two places: `checkLogFile` in `internal/source/strace/spawn.go` and `checkLogFile` in `internal/source/ebpf/source_linux.go`. Both resolve `/proc/<pid>/fd/<fd>` to a path and accept it when `strings.Contains(strings.ToLower(base), "log")`. The strace engine also checks that the path is a regular file (through the injectable `isRegularFile`). The eBPF program already sends only regular-file writes to Go.

On a Next.js 16 dev server the Turbopack cache database at `.next/dev/cache/turbopack/<id>/` holds `CURRENT`, `LOG` and numbered `.sst`/`.meta`/`.del` files, the LevelDB/RocksDB layout. Its `LOG` passes the current rule.

## Goals / Non-Goals

**Goals:**
- No notice for embedded database logs (`LOG` next to `CURRENT`).
- No notice for names that only contain the letters `log` (`catalog.json`).
- One rule, in one place, used by both engines.
- Real log files keep their notice: `/var/log/app.log`, `access_log`, `app.log.1`, `~/.pm2/logs/api-out.txt`.

**Non-Goals:**
- A list of build or cache folders to skip (`.next`, `node_modules`). This is not needed for the Turbopack case, and the list would go out of date.
- Changing the notice text, or following log files.
- A verbose mode that lists skipped files.

## Decisions

**1. A shared helper in `internal/source`.** Add `IsLogFile(path string, exists func(string) bool) bool` in a new file `internal/source/logfile.go`. `exists` reports whether a path exists. Production code passes a function built on `os.Stat`, and tests pass a map. The helper only decides the name rule and the database exception. The regular-file check and the once-per-path memory stay in each engine, because they differ: strace stats the file, and eBPF already filters in the kernel.
- Alternative: a method on a shared struct that holds `seenLogPaths`. Rejected: the two engines keep their state differently, and moving it is a refactor this change doesn't need.

**2. Whole-word match on name parts and folders.** Split the base name on `.`, `-` and `_`, and compare each part, lower-cased, with `log` and `logs`. Then compare each folder in the path (`strings.Split` on `/`) the same way, without splitting folder names. This covers `.log`, `access_log`, `error-logs.txt`, rotated `app.log.1`, `/var/log/syslog` and PM2's `~/.pm2/logs/`.
- Alternative: a regular expression with `\b`. Rejected: `\b` treats `_` as a word character, so `access_log` would not match.
- Accepted loss: names with no separator, such as `applog.txt` or `serverlog`, no longer trigger the notice.

**3. Database exception by layout, not by folder name.** When the base name is exactly `LOG`, `LOG.old` or matches `LOG.old.<digits>` (case-sensitive, as LevelDB and RocksDB write them), and `exists(filepath.Join(dir, "CURRENT"))` is true, the file is not a log file. This check runs only for those names, so it costs one `stat` at most, once per path, because both engines already remember paths they have checked.
- Alternative: skip any path containing `/.next/` or `/cache/`. Rejected: it fixes Turbopack only, and a hidden-folder version would hide PM2 logs.

**4. Paths come from `/proc/<pid>/fd` as seen by Specter.** The `CURRENT` check uses the same path the notice prints. When Specter and the target are in different mount namespaces, the stat may fail. The helper then treats `CURRENT` as missing and keeps the notice, which is today's behavior.

## Risks / Trade-offs

- [A real log named only `LOG` next to an unrelated `CURRENT` file is silenced] → very unlikely, and the target's stdout and stderr are still captured. Only the hint is lost.
- [`applog.txt`-style names lose their notice] → stated in the spec as accepted. The notice is a hint, not capture.
- [A stat on each new `LOG` path adds a small cost on the capture goroutine] → at most one stat per distinct path in a session, because both engines check each path once.

## Migration Plan

No migration. The behavior change is limited to which paths produce the notice. To roll back, revert the commit.

## Open Questions

None.
