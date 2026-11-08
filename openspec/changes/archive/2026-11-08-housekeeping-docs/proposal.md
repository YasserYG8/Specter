## Why

Before the v0.1 release, a few documents disagree with the code. `go.mod` and the README require Go 1.26, but `openspec/config.yaml` and `PRODUCT.md` say 1.24+, and `CLAUDE.md` carries a "Known doc drift" note about it. `.agent/architecture.md` is an early draft (v2) whose file layout and source order no longer match the code, yet `CLAUDE.md` points readers to it. CI is green, so these are the only items left before the release.

## What Changes

- `openspec/config.yaml` and `PRODUCT.md` say Go 1.26+, matching `go.mod` and the README. `go.mod` stays at 1.26. Since Go 1.21, `go install` fetches the required toolchain automatically, so users with an older Go still install Specter.
- `.agent/architecture.md` gets a banner at the top: historical design notes, with the code and `openspec/specs/` as the source of truth. Its content is not rewritten.
- `CLAUDE.md`: the "Known doc drift" section is removed now that the Go versions agree, and its mention of `.agent/architecture.md` says the same thing as the banner.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. Documentation only. No behavior or requirement changes.

## Impact

- `openspec/config.yaml`, `PRODUCT.md`, `.agent/architecture.md`, `CLAUDE.md`.
- No code, tests or specs change.
