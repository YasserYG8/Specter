# cli-help-polish Specification

## Purpose
TBD - created by archiving change shell-completions-ux. Update Purpose after archive.
## Requirements
### Requirement: Practical Command Usage Examples
The CLI SHALL display comprehensive, real-world command usage examples on `--help`.

#### Scenario: Displaying real-world recipes in help output
- **WHEN** user executes `specter --help` or `specter -h`
- **THEN** the help output includes an `Examples:` section demonstrating port attachment, PID targeting, level filtering, grep regex searching, and tree attachment

#### Scenario: Help formatting without errors
- **WHEN** user requests help without arguments
- **THEN** the CLI renders help information to stdout and exits cleanly with code 0

### Requirement: Short Flags
The CLI SHALL accept `-l` for `--level`, `-g` for `--grep`, `-t` for `--tree` and `-s` for `--source`, and each short flag SHALL behave the same as its long flag. The help output SHALL show both forms.

#### Scenario: Short flag with a value
- **WHEN** the user runs `specter 3000 -l warn`
- **THEN** the value is accepted and treated the same as `--level warn`

#### Scenario: Short flag validated like the long flag
- **WHEN** the user runs `specter 3000 -l banana`
- **THEN** Specter exits with the same error that `--level banana` gives, listing the valid levels

#### Scenario: Short boolean flag
- **WHEN** the user runs `specter 3000 -t`
- **THEN** it is treated the same as `--tree`

#### Scenario: Help shows both forms
- **WHEN** the user runs `specter --help`
- **THEN** the flags section shows `-l, --level`, `-g, --grep`, `-t, --tree` and `-s, --source`

### Requirement: Accurate Tree Flag Help
The help text for `--tree` SHALL state that it also traces deeper descendants of the target, and SHALL NOT imply that direct workers need the flag.

#### Scenario: Reading the tree flag help
- **WHEN** the user runs `specter --help`
- **THEN** the `--tree` description says it adds descendants beyond direct child processes, which are always traced
