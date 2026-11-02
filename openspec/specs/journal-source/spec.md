# journal-source Specification

## Purpose
Streams journal logs for systemd-managed services by detecting systemd slices and spawning `journalctl -f -n 0 _PID=<pid>`.

## Requirements

### Requirement: Systemd Unit and Journal Detection
The journal source SHALL determine if a process is managed by systemd by checking `/proc/<pid>/cgroup` for systemd unit slices or using `systemctl status <pid>`.

#### Scenario: Process managed by systemd unit
- **WHEN** `/proc/<pid>/cgroup` contains a system.slice path corresponding to a service (e.g., `system.slice/nginx.service`)
- **THEN** the journal source records the service unit name

#### Scenario: Process not managed by systemd
- **WHEN** the process runs under a standard user session without systemd service assignment
- **THEN** the journal source indicates no systemd unit is active

#### Scenario: Process inside a service cgroup that does not log to the journal
- **WHEN** `/proc/<pid>/cgroup` names an ignored infrastructure unit such as a CI runner, or the process is neither connected to a journal socket nor the unit's main process
- **THEN** the journal source indicates no systemd unit is active, and source selection continues to the next source

### Requirement: Live Systemd Journal Streaming
The journal source SHALL execute `journalctl -f -n 0 _PID=<pid>` or `journalctl -f -n 0 -u <unit>` to stream journal logs.

#### Scenario: Streaming live systemd logs
- **WHEN** a valid systemd-managed process is attached
- **THEN** `journalctl -f -n 0 _PID=<pid>` is spawned and journal log lines are emitted as `Chunk` items

#### Scenario: Clean detachment on context cancellation
- **WHEN** the context is canceled or SIGINT is received
- **THEN** the `journalctl` process is terminated cleanly
