## Context

Argument resolution lives inline in `RunE` in `cmd/specter/main.go` (about lines 268-298). For a whole number `val > 0`:

1. If `val <= 65535`, call `ResolvePort`. Success means attach. An error other than "no process found" is returned at once.
2. If no target was found, call `ResolvePID(val)`. Success means attach. The existing `NoProcessOnPortError` diagnostic, with active ports and suggestions, is printed only when this PID lookup also fails.

Step 2 is the problem: an empty port plus an existing PID with the same number attaches strace to an unrelated process. `ResolvePID` is `InspectPID`, which only reads `/proc`, so calling it to build a hint is read-only.

## Goals / Non-Goals

**Goals:**
- A bare number up to 65535 never attaches to anything except a listener on that port.
- The user who meant a PID learns how to ask for it, in one line.
- `--pid` exists now, in a form that can take several values later.

**Non-Goals:**
- Several targets at once (`specter 3000 4000`). That's a separate change. This one only shapes the flag for it.
- Waiting for a port that is still starting. It belongs with multi-stream, so it can be solved once for both.
- Shell completion of PIDs for `--pid`.
- Changes to the picker, containers or project names.

## Decisions

### 1. Rule A2: numbers above 65535 stay PIDs

A number above 65535 cannot be a port, so treating it as a PID involves no guessing. On systems where `pid_max` is about 4 million, large PIDs are common, and those users keep the short form.

Alternative A1: every bare number is a port, and anything above 65535 is an error pointing to `--pid`. It's one rule with no exception, but it costs the user a retype for no safety gain. The user chose A2.

### 2. Extract the numeric branch

Move the numeric logic into `resolveNumericTarget(ctx, res, val)` and `resolvePIDFlag(ctx, res, pids)`. They take a narrow interface (`ResolvePort`, `ResolvePID`), so `main_test.go` can test every rule with a fake on Windows. The `RunE` body gets shorter, and the behavior is pinned by tests rather than by reading.

### 3. The hint is built only after the port lookup failed

When `ResolvePort` returns `NoProcessOnPortError`, call `ResolvePID(val)` once. If it succeeds, take the base name of `Cmdline[0]` (or the process name if the command line is empty), and pass it to the diagnostic as an extra hint line. The diagnostic is still printed by `FormatDiagnostic`, so color handling and layout stay in one place. If `ResolvePID` fails for any reason, the hint is left out.

### 4. `--pid` as `IntSlice`, single value for now

`flags.IntSliceVarP(&flagPIDs, "pid", "p", nil, ...)`. Validation runs before resolution, next to the existing `validateLevel` check:
- `len > 1` → "only one target is supported for now"
- `len == 1` with a positional argument → "give either a target or --pid, not both"
- a value `<= 0` → "the PID must be a positive whole number"

Cobra already rejects `--pid abc`. With `IntSlice`, `--pid 3000,4000` also parses as two values, and gets the same error as two flags.

`-p` was chosen because strace uses `-p` for a PID, which users of strace already know. It isn't taken by another flag.

### 5. Error for a missing PID

`--pid N` and a bare number above 65535 that don't exist both return the same plain error: "no process with PID N exists". It wraps `ErrProcessNotFound` for tests. There's no port list, because the user asked for a PID.

## Risks / Trade-offs

- [Someone uses `specter <small PID>` from old docs or habits] → They get the port diagnostic plus a hint with the exact `--pid` command. The CHANGELOG lists it under Breaking.
- [A user really meant port 3000, the server is down, and the hint names an unrelated process] → The hint names the process, so it's clear it isn't their server. Nothing is attached.
- [`-p` might be wanted later for "port"] → Ports are the default meaning of a bare number, so a port flag is unlikely to be needed.

## Migration Plan

No release exists, so there are no installed users to migrate. The CHANGELOG entry and the README example are enough.

## Open Questions

None.
