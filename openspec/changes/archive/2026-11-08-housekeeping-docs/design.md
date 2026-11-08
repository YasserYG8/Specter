## Context

Three documents drifted from the code: two state an older Go version, and one design draft describes a layout the code no longer has. `CLAUDE.md` lists the Go mismatch under "Known doc drift" and calls `.agent/architecture.md` "an early draft".

## Goals / Non-Goals

**Goals:**
- One Go version everywhere: 1.26.
- Nobody mistakes `.agent/architecture.md` for the current design.

**Non-Goals:**
- Rewriting `.agent/architecture.md`. It would drift again, and the specs already describe current behavior.
- Filling the "Purpose: TBD" sections of the specs. That's useful for contributors later, not for v0.1.
- Lowering `go.mod` to an older Go.

## Decisions

### 1. Keep Go 1.26 and fix the documents

`go.mod`, the README install section and CI already use 1.26. Lowering `go.mod` would mean checking every newer standard-library call. With `GOTOOLCHAIN=auto` (the default since Go 1.21), `go install ...@latest` downloads Go 1.26 when the local Go is older, so the higher version costs users nothing.

### 2. A banner, not a rewrite or a deletion

The draft still explains why Specter is built the way it is (strace edge cases, unfinished and resumed calls, hex decoding), which is useful history. A banner keeps that and stops it from misleading:

```
> **Historical design notes (draft v2).** Parts of this file, such as the file layout and the order in which
> sources are chosen, are out of date. The code and `openspec/specs/` are the source of truth.
```

## Risks / Trade-offs

- [The banner is ignored and someone follows the old layout anyway] → `CLAUDE.md` says the same thing, and that's what agents read first.

## Migration Plan

None.

## Open Questions

None.
