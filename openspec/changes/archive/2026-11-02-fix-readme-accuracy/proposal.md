## Why

The README is the first thing a new user reads, and `v0.1.0` will send more people to it. Four of its claims are wrong, and following them either fails or gives a false picture of what Specter does:

| README line | Claim | What really happens |
|---|---|---|
| 149-153 | `-l`, `-g`, `-t` and `-s` work | `specter 3000 -l warn` fails with `unknown shorthand flag: 'l' in -l` |
| 22, 116 | Specter attaches "non-invasively" | `strace` slows the target, and the README never says so |
| 53, 92, 152 | `--tree` is how workers get traced | Direct workers are always traced, and `--tree` only adds deeper descendants |
| 237 | `sudo specter 3000` | After `go install`, this usually fails with `command not found` |

The slowdown matters most, because someone could attach to a busy production server trusting the word "non-invasive". Two benchmarks measured it: about 2,135 req/s dropping to 813, and about 14,000 req/s dropping to about 1,500.

## What Changes

- Add the short flags `-l`, `-g`, `-t` and `-s` to the CLI, so the README's flag table becomes true.
- Remove "non-invasive" from the README, and add a section on the slowdown while `strace` is attached.
- Correct the description of `--tree` in the README and in `specter --help`.
- Use `sudo "$(which specter)" 3000` everywhere in the README.
- Say that Docker is used through its CLI, and list the sources in the order the code checks them.
- Remove the notes in `CLAUDE.md` and `CONTRIBUTING.md` that describe the short flags as missing.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cli-help-polish`: Adds short flags, and requires the `--tree` help text to describe what the flag really does.

## Impact

- **Code:** Flag definitions and help text in `cmd/specter/main.go`.
- **Tests:** New cases in `cmd/specter/main_test.go`.
- **Docs:** `README.md`, `CLAUDE.md`, `CONTRIBUTING.md`, `CHANGELOG.md`.
- **Dependencies:** None.
- **Compatibility:** Nothing is removed. The short flags are new, and every long flag keeps working.
