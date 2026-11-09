## ADDED Requirements

### Requirement: Slowdown Notice
When the source is `strace`, the system SHALL print one notice to stderr after the `Source:` line, saying that Specter slows the target while attached and is meant for development and test servers. The notice SHALL NOT be printed for the Docker, file and journal sources, which do not slow the target, and SHALL NOT be printed to stdout.

#### Scenario: Strace source
- **WHEN** Specter attaches with the `strace` source
- **THEN** one notice about the slowdown is printed to stderr, after the `Source:` line

#### Scenario: Other sources
- **WHEN** Specter attaches with the Docker, file or journal source
- **THEN** no slowdown notice is printed

#### Scenario: Piped output stays clean
- **WHEN** stdout is piped to another command and the notice is printed
- **THEN** stdout receives no notice text

#### Scenario: Printed once
- **WHEN** Specter runs for a long time with the `strace` source
- **THEN** the notice appears once, when it attaches, and not again
