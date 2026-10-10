# shell-completions Specification

## Purpose
TBD - created by archiving change shell-completions-ux. Update Purpose after archive.
## Requirements
### Requirement: Shell Completion Script Generation
The CLI SHALL provide a `completion` command to output completion scripts for Bash, Zsh, Fish, and PowerShell to stdout.

#### Scenario: Generate Bash completion script
- **WHEN** user executes `specter completion bash`
- **THEN** the command outputs a valid Bash completion script to standard output and exits with code 0

#### Scenario: Generate Zsh completion script
- **WHEN** user executes `specter completion zsh`
- **THEN** the command outputs a valid Zsh completion script to standard output and exits with code 0

#### Scenario: Unsupported shell parameter
- **WHEN** user executes `specter completion unsupported`
- **THEN** the command returns an error indicating the shell is unsupported and lists valid options

### Requirement: Dynamic Port and Process Autocompletion
The CLI SHALL dynamically discover active listening ports and provide them with process descriptions as autocompletion candidates for the target argument.

#### Scenario: Dynamic completion with active listening ports
- **WHEN** shell completion is invoked for the root argument (e.g. `specter <TAB>`)
- **THEN** the completion engine queries active listening ports within a 300ms timeout and returns candidates formatted with port numbers and process descriptions

#### Scenario: Completion when no ports are listening or discovery times out
- **WHEN** no listening ports exist or discovery times out
- **THEN** the completion engine returns an empty completion list without file fallback or errors

### Requirement: Dynamic Flag Value Completion
The CLI SHALL provide autocompletion options with descriptions for enumerated flags `--source` and `--level`.

#### Scenario: Autocompleting source flag
- **WHEN** completion is requested for `--source`
- **THEN** the completion engine returns `strace`, `file`, `docker`, and `journal` with descriptions

#### Scenario: Autocompleting level flag
- **WHEN** completion is requested for `--level`
- **THEN** the completion engine returns `debug`, `info`, `warn`, and `error` with descriptions

