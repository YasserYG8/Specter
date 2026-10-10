## Context

Specter is executed via the terminal, where developers often need to quickly attach to services without leaving their prompt to run separate network diagnostics tools (`ss`, `lsof`, `netstat`). While Specter provides detailed diagnostic errors on port misses, providing interactive tab completions allows developers to see active targets proactively before running the command.

## Goals / Non-Goals

**Goals:**
- Provide a `specter completion [bash|zsh|fish|powershell]` subcommand to generate standard shell completion scripts.
- Implement dynamic positional argument autocompletion (`ValidArgsFunction`) that queries active listening ports and formats completions with process name, PID, and security status.
- Implement dynamic flag completion for `--source` and `--level`.
- Enrich CLI `--help` with practical, real-world examples in `cmd.Example`.
- Ensure autocompletion is snappy (<300ms) with strict timeout boundaries.

**Non-Goals:**
- Interactive fuzzy search UI / TUI selection (e.g. fzf / survey prompt) - Specter remains a single-command stream viewer.
- Completing arbitrary process PIDs unrelated to network listeners.

## Decisions

### 1. Dedicated `completion` Subcommand
- **Decision**: Add `newCompletionCmd(rootCmd)` under `cmd/specter` supporting `bash`, `zsh`, `fish`, and `powershell`.
- **Rationale**: Standard Cobra convention familiar to developers across Kubernetes, Docker, and GitHub CLI tools.
- **Alternatives Considered**: Manually distributed completion files only (less discoverable and harder to install across varied shell configurations).

### 2. Dynamic Port Completion with Strict Timeout
- **Decision**: In `cmd.ValidArgsFunction`, call `resolver.DiscoverListeningPorts(ctx)` with a 300ms timeout context. Format completions as `<port>\t<processName> (PID <pid>)`.
- **Rationale**: Shell autocompletion must never hang or feel sluggish. 300ms provides sufficient time for `ss` on Linux while preventing perceived terminal freezes.
- **Directive**: Return `cobra.ShellCompDirectiveNoFileComp` to prevent shells from attempting file path completion on port arguments.

### 3. Flag Value Completions
- **Decision**: Register completion functions for `--source` (`strace`, `file`, `docker`, `journal`) and `--level` (`debug`, `info`, `warn`, `error`) with inline tab descriptions.
- **Rationale**: Enhances flag discoverability without requiring users to refer back to `--help`.

### 4. Rich Real-World Help Recipes
- **Decision**: Populate `cmd.Example` on the root command showing concrete port attachments, filter patterns, and tree tracing examples.
- **Rationale**: Immediate documentation at the terminal prompt speeds up onboarding.

## Risks / Trade-offs

- **[Risk] Shell completion latency** → **Mitigation**: 300ms context timeout on active port discovery. If `ss`/`lsof` fails or times out, completions gracefully return empty without breaking shell input.
- **[Risk] Execution on non-Linux platforms** → **Mitigation**: `DiscoverListeningPorts` checks OS / commands safely; returns empty candidate list on unsupported systems.
