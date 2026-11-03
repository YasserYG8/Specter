## Context

`main.go` defines every flag with the long-only form, such as `flags.StringVar`. The README, `PRODUCT.md` and the `Known doc drift` section of `CLAUDE.md` disagree with it about short flags, and `CONTRIBUTING.md` lists the gap as work for contributors.

`--tree` only changes which existing processes get discovered. `main.go` always calls `FindChildPIDs`, and `--tree` swaps it for `FindChildProcessTree` with a depth of 3. `strace` always gets `-f`, whatever the flag says, and the `FollowTree` option is stored but never read.

The slowdown applies only when the `strace` source is in use. The `docker`, `file` and `journal` sources read existing output and don't stop the target.

## Goals / Non-Goals

**Goals:**

- Every flag and command in the README works as written.
- A reader learns about the slowdown before attaching to anything important.

**Non-Goals:**

- A warning printed at attach time. This change only documents the slowdown, and a runtime warning can follow as its own change.
- A sample of real output at the top of the README. This would be useful, but nothing in the README is wrong without it, so it's a separate small follow-up.
- Changing what `--tree` does, or removing the unused `FollowTree` option.
- The `Go 1.24+` drift in `PRODUCT.md` and `openspec/config.yaml`.
- Rewriting sections that are already accurate.

## Decisions

### Add the short flags rather than remove them from the docs

`PRODUCT.md` lists them as a product capability, and they are the forms people type most. Adding them is a one-word change per flag using `StringVarP` and `BoolVarP`. None of the four letters conflict with cobra's `-h`, or with `-v` for `--version`.

*Alternative considered:* deleting the column from the README. This is smaller, but it removes something the product says it offers.

### State the slowdown with both measurements, as a range

The two benchmarks used different servers and load generators, and they don't agree: about 62% fewer requests in one and about 89% in the other. The README gives the range of 60% to 90% and says that it was measured on test servers, because a single number would claim more precision than the data has. It says that the slowdown applies to the `strace` source and ends on detach.

The word "read-only" stays, since Specter still never signals or changes the target. Only "non-invasive" goes, because it implies there is no effect at all.

*Alternative considered:* leaving the numbers out. "May slow the target" is too easy to ignore for a tool people could point at production.

### Use the `which` form of `sudo`

Install section line 44 already uses `sudo "$(which specter)" 3000`, because Ubuntu's default `secure_path` leaves out the Go binary folder. Line 237 and any other `sudo specter` example change to match.

## Risks / Trade-offs

- [The stated range may not match a reader's workload] → The text says it was measured on test servers, and that a real server's result depends on how often it writes.
- [`-s` could be read as a short form of `--stderr`] → The flag table already maps it to `--source`, and `--stderr` has no short form. `specter --help` shows the mapping too.
- [The docs drift again] → A test checks each short flag against its long flag, so removing one fails CI.

## Migration Plan

Nothing to migrate. To roll back, revert the change.

## Open Questions

None.
