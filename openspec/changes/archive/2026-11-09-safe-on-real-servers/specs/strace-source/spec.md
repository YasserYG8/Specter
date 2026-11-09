## ADDED Requirements

### Requirement: Strace Does Not Outlive Specter
On Linux, the `strace` process that Specter starts SHALL receive `SIGINT` when Specter dies for any reason, including `SIGKILL`, so that it detaches from the target. Within one second after Specter has died, no `strace` started by it SHALL still be running, and the target SHALL have no tracer and SHALL keep running. When `strace` is killed while Specter is attached, the target SHALL keep running, and Specter SHALL exit with a non-zero status and a message that `strace` ended unexpectedly.

#### Scenario: Specter is killed
- **WHEN** Specter is attached to a server and receives `SIGKILL`
- **THEN** within one second the `strace` it started has exited, the server has no tracer, and the server still answers requests

#### Scenario: Specter is killed while the server is idle
- **WHEN** Specter is killed and the server writes nothing afterwards
- **THEN** the `strace` still exits, and does not wait for the server to write a line

#### Scenario: Repeated kills
- **WHEN** the previous scenario is repeated 20 times
- **THEN** every repetition ends with no `strace` left and no tracer on the server

#### Scenario: Strace is killed
- **WHEN** the `strace` started by Specter receives `SIGKILL`
- **THEN** the server has no tracer and still answers requests, and Specter exits with status 1 and says that `strace` exited unexpectedly

#### Scenario: Another Specter can attach afterwards
- **WHEN** Specter was killed and a second Specter is started on the same server
- **THEN** the second Specter attaches, and does not report that the server is already traced

#### Scenario: Systems without the parent-death signal
- **WHEN** Specter is compiled for macOS or Windows
- **THEN** it compiles and behaves as before, without this guarantee
