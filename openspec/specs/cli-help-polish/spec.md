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

