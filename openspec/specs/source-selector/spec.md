# source-selector Specification

## Purpose
Probes target process environment metadata and selects the lowest-overhead acquisition source according to priority (Docker > Journal > File > eBPF > Strace) with support for user `--source` overrides.

## Requirements

### Requirement: Automatic Source Selection Priority
The source selector SHALL evaluate target process environment metadata and select the lowest-overhead acquisition source, in this order: Docker, systemd journal, regular file, eBPF, then strace. The eBPF source SHALL be selected only when it can run: Linux, kernel 5.8 or later, kernel type information at `/sys/kernel/btf/vmlinux`, and `CAP_BPF` with `CAP_PERFMON` or `CAP_SYS_ADMIN`. When it cannot, the selector SHALL select strace and keep the reason in plain words for the slowdown notice.

#### Scenario: Docker container detected
- **WHEN** the target process is containerized and `docker` executable is available
- **THEN** the selector picks `DockerSource`

#### Scenario: Regular disk file detected
- **WHEN** the process is not containerized and fd 1 or 2 points to a regular file
- **THEN** the selector picks `FileTailSource`

#### Scenario: Systemd service detected
- **WHEN** the process is governed by a systemd unit and `journalctl` is available
- **THEN** the selector picks `JournalSource`

#### Scenario: eBPF available
- **WHEN** no Docker, journal or file source applies, and Specter runs as root on Linux 6.8 with `/sys/kernel/btf/vmlinux` present
- **THEN** the selector picks the `ebpf` source, and the `Source:` line says `ebpf`

#### Scenario: Terminal or pipe without eBPF
- **WHEN** no Docker, journal or file source applies, and Specter runs as a normal user without `CAP_BPF`
- **THEN** the selector falls back to `StraceSource` and keeps the reason that eBPF needs root

#### Scenario: Old kernel
- **WHEN** no Docker, journal or file source applies, and the kernel is 5.4
- **THEN** the selector picks `StraceSource` and keeps the reason that eBPF needs Linux 5.8 or later

### Requirement: User Source Override Flag
The source selector SHALL honor the user-specified `--source` flag (`docker`, `file`, `journal`, `ebpf`, `strace`).

#### Scenario: Valid source specified by user
- **WHEN** the user specifies `--source docker`
- **THEN** the selector uses `DockerSource` regardless of auto-detection heuristics

#### Scenario: Strace forced where eBPF is available
- **WHEN** the user specifies `--source strace` and eBPF could run
- **THEN** the selector uses `StraceSource`

#### Scenario: Invalid source name provided
- **WHEN** the user provides an unknown source name (e.g., `--source invalid`)
- **THEN** the system returns an informative validation error listing supported sources, including `ebpf`

### Requirement: Direct Container Target Selection
The source selector SHALL select `DockerSource` when the target process represents a container identified by name or container ID, without requiring an associated host PID.

#### Scenario: Container target without host PID
- **WHEN** target specifies a non-empty `Container` identifier and PID is 0 or uninspected
- **THEN** the selector picks `DockerSource` and sets the target container identifier

#### Scenario: Container target with forced source override
- **WHEN** user specifies `--source docker` with a named container target
- **THEN** the selector uses `DockerSource` and attaches to the specified container

### Requirement: Fallback From eBPF to Strace
When the selector picked the `ebpf` source automatically and the kernel refuses to load or attach its programs at start, the system SHALL start the `strace` source instead and continue, with the kernel's refusal as the reason shown in the slowdown notice. It SHALL print one line for the fallback, not a separate "source unavailable" line and a notice. When `--source ebpf` is set, the system SHALL NOT fall back.

#### Scenario: Load refused by the kernel
- **WHEN** eBPF passed the selector's checks but loading the program fails because of a kernel lockdown policy
- **THEN** Specter attaches with strace, prints one slowdown notice that names the refusal, and streams the target's output

#### Scenario: Forced eBPF fails to load
- **WHEN** `--source ebpf` is set and loading the program fails
- **THEN** Specter exits with a non-zero status and the forced eBPF diagnostic, and does not start strace
