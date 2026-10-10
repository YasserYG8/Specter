## Why

Developers interacting with Specter frequently need to know what ports are active or what flags are available without checking external tools or running separate commands. Introducing native shell autocompletions for Bash, Zsh, Fish, and PowerShell, combined with dynamic port and process discovery upon pressing `<TAB>` and rich help recipes, significantly streamlines developer workflow and discoverability.

## What Changes

- Add `specter completion [bash|zsh|fish|powershell]` subcommand to generate shell completion scripts.
- Implement dynamic argument autocompletion (`ValidArgsFunction`) querying active listening ports and returning formatted candidates with process descriptions (e.g. `3000\tnode (PID 4920)`).
- Implement dynamic flag completion (`RegisterFlagCompletionFunc`) for `--source` (`docker`, `file`, `journal`, `strace`) and `--level` (`debug`, `info`, `warn`, `error`).
- Add rich command usage examples (`cmd.Example`) demonstrating common port attachment, level filtering, regex search, and tree tracing recipes.
- Include unit tests verifying completion generation and dynamic argument resolution.

## Capabilities

### New Capabilities
- `shell-completions`: Shell completion generation for Bash, Zsh, Fish, and PowerShell, with dynamic active listening port autocompletion and flag value completions.
- `cli-help-polish`: Rich CLI help screens and real-world command usage recipes.

### Modified Capabilities
None.

## Impact

- **CLI Interface**: Adds `completion` subcommand to `newRootCmd()` and registers dynamic completion handlers.
- **Resolver**: Reuses `PortResolver.DiscoverListeningPorts` with a short timeout context to provide rapid tab-completion without lag.
- **Dependencies**: No external dependencies added; uses standard Cobra completion facilities.
