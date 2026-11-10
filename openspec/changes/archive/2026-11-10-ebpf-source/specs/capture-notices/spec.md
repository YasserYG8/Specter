## MODIFIED Requirements

### Requirement: Slowdown Notice
When the source is `strace`, the system SHALL print one notice to stderr after the `Source:` line, saying that Specter slows the target while attached and is meant for development and test servers. When the `ebpf` source could not be used, the same line SHALL end with the reason and how to get the fast engine, for example `Run with sudo to use the fast eBPF engine.` or `eBPF needs Linux 5.8 or later (this kernel is 5.4).` When `--source strace` was set by the user, the line SHALL NOT give a reason. The notice SHALL NOT be printed for the `ebpf`, Docker, file and journal sources, which do not slow the target, and SHALL NOT be printed to stdout.

#### Scenario: Strace source
- **WHEN** Specter attaches with the `strace` source
- **THEN** one notice about the slowdown is printed to stderr, after the `Source:` line

#### Scenario: Strace because of missing root
- **WHEN** a normal user runs `specter 3000` and Specter falls back to `strace` because eBPF needs root
- **THEN** the one slowdown notice ends with `Run with sudo to use the fast eBPF engine.`

#### Scenario: Strace chosen by the user
- **WHEN** the user runs `specter 3000 --source strace`
- **THEN** the slowdown notice is printed without a reason or a tip

#### Scenario: Other sources
- **WHEN** Specter attaches with the `ebpf`, Docker, file or journal source
- **THEN** no slowdown notice is printed

#### Scenario: Piped output stays clean
- **WHEN** stdout is piped to another command and the notice is printed
- **THEN** stdout receives no notice text

#### Scenario: Printed once
- **WHEN** Specter runs for a long time with the `strace` source
- **THEN** the notice appears once, when it attaches, and not again

## ADDED Requirements

### Requirement: Lost Writes Notice
When the `ebpf` source could not keep up and dropped writes, the system SHALL print a notice to stderr with the number of writes lost since the last notice, at most once per second while writes are being lost, and SHALL NOT print it when nothing was lost. The notice SHALL NOT be printed to stdout.

#### Scenario: Burst larger than the buffer
- **WHEN** the target writes faster than Specter reads and 120 writes are dropped within one second
- **THEN** Specter prints `[specter:notice] 120 writes were lost because the target wrote faster than Specter could read.` once for that second

#### Scenario: No loss
- **WHEN** no write is dropped
- **THEN** no lost-writes notice is printed
