## ADDED Requirements

### Requirement: Filter Flag Validation
The system SHALL reject an unknown `--level` value with an error that lists the valid levels, before resolving the target. An empty value and every alias the severity parser recognizes SHALL be accepted. When `--grep` is not a valid regular expression, the system SHALL match it as literal text and print one notice to stderr saying so.

#### Scenario: Unknown level
- **WHEN** the user runs `specter 3000 --level banana`
- **THEN** Specter exits with an error listing the valid levels and does not attach

#### Scenario: Level alias
- **WHEN** the user runs `specter 3000 --level warning`
- **THEN** the value is accepted and treated as `warn`

#### Scenario: Invalid regular expression
- **WHEN** the user runs `specter 3000 --grep "[error"`
- **THEN** a notice is printed to stderr once, and lines containing the literal text `[error` are matched
