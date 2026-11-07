## Why

`specter 3000` tries port 3000 first, and when nothing listens there it silently tries PID 3000. If a process with that PID exists, Specter attaches strace to it, an unrelated process, and slows it down. The only sign is a line like `Attaching to PID 3000 (systemd-journald)`. This happens exactly when the user's server is down, which is often why they opened Specter, and it breaks the promise that Specter tells you why it shows nothing. Specter has no release and no users yet, so this is the cheapest moment to make the rule strict.

## What Changes

- **BREAKING**: A bare number from 1 to 65535 is a port only. When nothing listens on that port and no container publishes it, Specter stops with the existing "No process is listening on port N" message and the list of active ports. It never falls back to a PID.
- When a process with that number as its PID exists, the message ends with a hint that names the process and shows the command to attach to it: `Hint: PID 3000 exists (systemd-journald). To attach to it: specter --pid 3000`. Reading its name only reads `/proc`. Nothing is attached.
- A bare number above 65535 cannot be a port, so it is treated as a PID, as before.
- New flag `--pid` (`-p`) attaches to a process by PID, for any PID. It is declared as repeatable so that several targets can be added later without another breaking change. For now, more than one `--pid`, or `--pid` together with a positional target, is an error.
- Help text, examples and README show `--pid` for PIDs.

## Capabilities

### New Capabilities

- `target-arguments`: how Specter interprets its positional argument and `--pid`: numbers as ports, large numbers as PIDs, names as containers or projects, and the errors and hints when nothing matches.

### Modified Capabilities

None. The port resolver itself does not change. It already reports `ErrNoProcessFound` with the active ports, and this change only stops the CLI from trying a PID after it.

## Impact

- `cmd/specter/main.go`: the numeric branch of argument resolution, the new `--pid` flag, the hint, help text and examples. The numeric logic moves into a small function that tests can call with a fake resolver.
- `cmd/specter/main_test.go`: tests for each rule.
- `README.md`: the PID example and the flags table. `CHANGELOG.md`: a Breaking entry.
- Not affected: the interactive picker, which already resolves a chosen PID directly, containers, project names and shell completions.
