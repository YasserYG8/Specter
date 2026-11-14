## ADDED Requirements

### Requirement: Several Targets in One Run
The system SHALL accept up to 8 targets in one run, given as positional arguments, as `--pid` flags, or as both. Each positional argument SHALL be resolved by the same rules as a single target (a whole number from 1 to 65535 is a port, a larger one is a PID, anything else is a container or a project name). Each target SHALL have a label: `:<port>` for a port, `pid:<pid>` for a PID (from `--pid` or a number above 65535), and the argument as written for a container or a project. More than 8 targets SHALL be refused before anything is resolved, with an error that names the limit. With no argument and no `--pid`, the interactive picker SHALL work as today and select one target.

#### Scenario: Two ports
- **WHEN** servers listen on ports 3000 and 4000 and the user runs `specter 3000 4000`
- **THEN** Specter attaches to both and shows the output of both

#### Scenario: Mixed kinds
- **WHEN** the user runs `specter 3000 web --pid 70123`, where 3000 is a port, `web` is a container and 70123 is a PID
- **THEN** Specter attaches to all three, with the labels `:3000`, `web` and `pid:70123`

#### Scenario: Too many targets
- **WHEN** the user gives 9 targets
- **THEN** Specter exits with a non-zero status before resolving anything, and the error says that at most 8 targets are supported

### Requirement: All or Nothing at Start
The system SHALL resolve every target and start every source before it prints any log line. If any target cannot be resolved, or any source fails to start, the system SHALL stop every source it already started, leave no tracer attached, print the error of the first failure with the label of its target, and exit with a non-zero status. The diagnostic for an empty port (with the list of active ports) SHALL be printed as it is for a single target. Sources SHALL be started one after the other, in the order the targets were given.

#### Scenario: One port is empty
- **WHEN** a server listens on port 3000, nothing listens on port 4000, and the user runs `specter 3000 4000`
- **THEN** Specter prints the "No process is listening on port 4000" diagnostic, attaches to nothing, and exits with a non-zero status

#### Scenario: Second source fails to start
- **WHEN** the user runs `specter 3000 4000 --source ebpf`, the source for :3000 starts, and the eBPF program for :4000 is refused by the kernel
- **THEN** Specter stops the source for :3000, prints the eBPF diagnostic for :4000, and exits with a non-zero status without printing any log line

### Requirement: Duplicate Targets Are Followed Once
When two targets resolve to the same host process, or a target's PID is one of the child processes another target already follows, or two targets name the same container, the system SHALL follow that process or container once, under the label of the first target, and SHALL print one notice to stderr that names both labels. If every target after removing duplicates is one target, the run SHALL behave as a single-target run.

#### Scenario: One server on two ports
- **WHEN** PID 1234 listens on ports 3000 and 9229 and the user runs `specter 3000 9229`
- **THEN** Specter prints a notice that `:9229` is the same process as `:3000` (PID 1234), follows it once without tags, and shows each line once

#### Scenario: Worker of another target
- **WHEN** PID 1300 is a worker of the server on port 3000 and the user runs `specter 3000 --pid 1300`
- **THEN** Specter prints a notice that `pid:1300` is already followed through `:3000`, and shows each line of PID 1300 once

### Requirement: One Capture Session per Target
Each target SHALL have its own capture session, made of its own source, line assembler and stack-trace grouper, and, with `--grep`, its own trace holder. The source of each session SHALL be chosen by the source selector as for a single target, so different targets MAY use different sources. `--source`, `--tree`, `--level`, `--grep`, `--stderr`, `--raw`, `--json`, `--no-color` and `--duration` SHALL apply to every target. Lines and stack traces from different targets SHALL never be joined into one entry.

#### Scenario: Different sources
- **WHEN** the user runs `specter 3000 web`, where :3000 is a host process and `web` is a container
- **THEN** :3000 uses the `ebpf` or `strace` source and `web` uses the Docker source

#### Scenario: Stack traces stay whole
- **WHEN** the target on :4000 writes a 20-line stack trace while the target on :3000 writes request lines
- **THEN** the trace is shown as one entry under the `:4000` tag, with no line from :3000 inside it

#### Scenario: Filters apply to all
- **WHEN** the user runs `specter 3000 4000 --level error`
- **THEN** only error entries are shown, from both targets

### Requirement: Tagged Output
With two or more targets, the system SHALL start every line it writes to stdout with the label of its target, padded with spaces to the width of the longest label, followed by ` | `. Every line of a multi-line entry SHALL carry the tag. When colors are enabled, each target's tag SHALL have its own color, which SHALL NOT be red or yellow, as these mean error and warning. When colors are off (piped output or `--no-color`), the tag SHALL be plain text. The tag SHALL also be written with `--raw` and `--json`. With one target, no tag SHALL be written, and the output SHALL be the same as before this change. Entries SHALL be written in the order they arrive.

#### Scenario: Two targets on a terminal
- **WHEN** the user runs `specter 3000 web` on a terminal
- **THEN** lines look like `:3000 | GET /api 200` and `web   | started`, and the two tags have different colors

#### Scenario: Piped output can be filtered by target
- **WHEN** the user runs `specter 3000 4000 | grep ':4000 |'`
- **THEN** only the lines from :4000 pass, and they contain no escape codes

#### Scenario: One target is unchanged
- **WHEN** the user runs `specter 3000`
- **THEN** the output has no tag

### Requirement: Partial Lines Do Not Mix
When a target's partial line (output without a final line break, shown after a short wait) is on screen and a different target writes, the system SHALL first end the row with a line break. When the rest of that partial line arrives from the same target and nothing else was written in between, it SHALL continue the same row without a new tag.

#### Scenario: Prompt interrupted by another target
- **WHEN** the target on :3000 writes `Continue? ` with no line break, and the target on :4000 then writes `ready`
- **THEN** stdout shows `:3000 | Continue? ` on one row and `:4000 | ready` on the next row

### Requirement: Per-Target Status Messages
With two or more targets, every status message on stderr that is about one target (the attach line, the child workers line, the `Source:` line, the slowdown notice, the source's own notices, the "nothing captured yet" notice, and the end report) SHALL start with that target's label. The "nothing captured yet" notice SHALL be given per target. The ptrace restriction warning SHALL be printed at most once per run. With one target, these messages SHALL be the same as before this change.

#### Scenario: Attach lines
- **WHEN** the user runs `specter 3000 4000`
- **THEN** stderr has one attach line and one `Source:` line for each target, each starting with its label

#### Scenario: One silent target
- **WHEN** :3000 writes lines and :4000 writes nothing for 10 seconds
- **THEN** one "nothing captured yet" notice is printed for :4000 only

### Requirement: Ending a Multi-Target Run
When one target ends by itself, the system SHALL print that target's end report (as defined for a single target, with its label), stop its session after the usual drain time, and keep streaming the other targets. The run SHALL end when every target has ended, when the user interrupts Specter, or when `--duration` runs out. On an interrupt or at the end of `--duration`, the system SHALL stop every session that is still running, leave no tracer attached, and print one "detached" line per such target. The exit status SHALL be 1 if any target ended by itself during the run, and 0 otherwise.

#### Scenario: One target crashes
- **WHEN** Specter follows :3000 and :4000 and the target on :4000 exits with code 1
- **THEN** Specter prints `:4000 Target PID <pid> exited with code 1.` and keeps showing lines from :3000

#### Scenario: Interrupt after one target ended
- **WHEN** :4000 has ended, and the user then presses Ctrl+C
- **THEN** Specter prints that it detached from :3000 cleanly, and exits with status 1

#### Scenario: Interrupt with all targets running
- **WHEN** the user presses Ctrl+C while both targets run
- **THEN** Specter prints a "detached cleanly" line for each target, no `strace` stays attached, and Specter exits with status 0

#### Scenario: All targets end
- **WHEN** both targets exit by themselves
- **THEN** Specter prints both end reports and exits with status 1
