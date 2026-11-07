## ADDED Requirements

### Requirement: Numeric Argument Is a Port
A positional argument that is a whole number from 1 to 65535 SHALL be resolved as a TCP port only, including through Docker published ports. When no process or container listens on that port, the system SHALL exit with a non-zero status and print the "No process is listening on port N" diagnostic with the active listening ports on stderr, and SHALL NOT attach to any process.

#### Scenario: Port with a listening server
- **WHEN** a server listens on port 3000 and the user runs `specter 3000`
- **THEN** Specter attaches to that server

#### Scenario: Empty port whose number is also a PID
- **WHEN** nothing listens on port 3000, a process with PID 3000 exists, and the user runs `specter 3000`
- **THEN** Specter prints "No process is listening on port 3000." with the active ports, exits with a non-zero status, and attaches to nothing

#### Scenario: Empty port whose number is not a PID
- **WHEN** nothing listens on port 3000, no process has PID 3000, and the user runs `specter 3000`
- **THEN** Specter prints "No process is listening on port 3000." with the active ports, without a hint, and exits with a non-zero status

#### Scenario: Port error other than an empty port
- **WHEN** resolving port 3000 fails for a reason other than no listening process, such as a permission error
- **THEN** Specter returns that error and does not try a PID

### Requirement: Hint for a Matching PID
When a numeric argument from 1 to 65535 finds no listening port and a process with that PID exists, the diagnostic SHALL end with one line that names the process and gives the command to attach to it by PID. Finding the name SHALL only read `/proc`.

#### Scenario: Hint names the process
- **WHEN** nothing listens on port 3000 and PID 3000 is `/usr/lib/systemd/systemd-journald`
- **THEN** the last line on stderr is `Hint: PID 3000 exists (systemd-journald). To attach to it: specter --pid 3000`

### Requirement: Large Numbers Are PIDs
A positional argument that is a whole number above 65535 SHALL be resolved as a PID, because it cannot be a port.

#### Scenario: PID above the port range
- **WHEN** a process with PID 70000 exists and the user runs `specter 70000`
- **THEN** Specter attaches to PID 70000

#### Scenario: Missing PID above the port range
- **WHEN** no process has PID 70000 and the user runs `specter 70000`
- **THEN** Specter exits with a non-zero status and an error that says no process with PID 70000 exists

### Requirement: Explicit PID Flag
The system SHALL accept `--pid <PID>`, with the shorthand `-p`, to attach to a process by PID, for any PID value. The flag SHALL be declared as repeatable. Until several targets are supported, the system SHALL reject more than one `--pid`, and `--pid` combined with a positional argument, with an error before resolving anything.

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
- **WHEN** the user runs `specter --pid 3000 --pid 4000`
- **THEN** Specter exits with a non-zero status and an error that says only one target is supported for now

#### Scenario: PID and a positional target
- **WHEN** the user runs `specter 8080 --pid 3000`
- **THEN** Specter exits with a non-zero status and an error that says to give either a target or `--pid`, not both

#### Scenario: Invalid PID value
- **WHEN** the user runs `specter --pid 0` or `specter --pid abc`
- **THEN** Specter exits with a non-zero status and an error that the PID must be a positive whole number
