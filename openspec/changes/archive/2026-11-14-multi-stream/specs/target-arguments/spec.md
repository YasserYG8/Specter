## MODIFIED Requirements

### Requirement: Explicit PID Flag
The system SHALL accept `--pid <PID>`, with the shorthand `-p`, to attach to a process by PID, for any PID value. The flag SHALL be repeatable, and SHALL be allowed together with positional targets. Each `--pid` SHALL be one target, as defined by the multi-stream capability.

#### Scenario: Attach by PID below the port range
- **WHEN** a process with PID 3000 exists and the user runs `specter --pid 3000`
- **THEN** Specter attaches to PID 3000 without checking port 3000

#### Scenario: Short form
- **WHEN** the user runs `specter -p 3000`
- **THEN** Specter behaves as with `specter --pid 3000`

#### Scenario: PID does not exist
- **WHEN** no process has PID 3000 and the user runs `specter --pid 3000`
- **THEN** Specter exits with a non-zero status and an error that says no process with PID 3000 exists

#### Scenario: Two PIDs
- **WHEN** processes with PIDs 3000 and 4000 exist and the user runs `specter --pid 3000 --pid 4000`
- **THEN** Specter attaches to both, with the labels `pid:3000` and `pid:4000`

#### Scenario: PID and a positional target
- **WHEN** a server listens on port 8080, a process with PID 3000 exists, and the user runs `specter 8080 --pid 3000`
- **THEN** Specter attaches to both, with the labels `:8080` and `pid:3000`

#### Scenario: Invalid PID value
- **WHEN** the user runs `specter --pid 0` or `specter --pid abc`
- **THEN** Specter exits with a non-zero status and an error that the PID must be a positive whole number
