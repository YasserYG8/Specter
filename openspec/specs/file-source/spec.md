# file-source Specification

## Purpose
Tails regular log files on disk directly when process standard streams (fd 1 and/or fd 2) are redirected to files, providing zero-overhead non-ptrace streaming.

## Requirements

### Requirement: Regular File Descriptor Detection
The file source SHALL inspect the symlink targets of `/proc/<pid>/fd/1` and `/proc/<pid>/fd/2` to determine if standard streams are redirected to regular files on disk.

#### Scenario: Stdout points to disk log file
- **WHEN** `/proc/<pid>/fd/1` resolves to a regular file path (e.g., `/var/log/app.log`) and not a pipe, socket, PTY, or `/dev/null`
- **THEN** the file source identifies fd 1 as a disk file target

#### Scenario: PTY or pipe descriptor detected
- **WHEN** `/proc/<pid>/fd/1` resolves to `/dev/pts/0` or `pipe:[12345]`
- **THEN** the file source reports that the descriptor is not a regular file

### Requirement: Non-Blocking File Tail Streaming
The file source SHALL seek to the end of the target file (`io.SeekEnd`) upon attach and stream newly appended bytes without ptrace intervention.

#### Scenario: Streaming appended content
- **WHEN** a process appends new bytes to the open file
- **THEN** the file source reads the new bytes and emits them as `Chunk` instances with corresponding FD (1 or 2)

#### Scenario: Clean detachment on context cancellation
- **WHEN** the context is canceled
- **THEN** the file reader loop exits and file handles are closed cleanly
