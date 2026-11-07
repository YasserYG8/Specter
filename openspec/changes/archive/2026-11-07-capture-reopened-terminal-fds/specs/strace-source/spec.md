## ADDED Requirements

### Requirement: Output Through Reopened Descriptors
A write to a descriptor other than 1 and 2 SHALL be output when its `-y` destination equals the destination of the target's fd 1 or fd 2, except when that destination is `/dev/null`. Such a write SHALL keep its real descriptor number, so that line assembly keeps it apart from fd 1 and fd 2, and it SHALL count as stdout for filtering. A write to a descriptor other than 1 and 2 without a destination, or with any other destination, SHALL NOT be output. The same rule SHALL apply to `writev` and to calls split into unfinished and resumed entries, and the rules for processes other than the target SHALL apply unchanged.

#### Scenario: Node.js writes through a reopened terminal
- **WHEN** the target's fd 1 and fd 2 point to `/dev/pts/1` and strace prints `[pid 1306] write(22</dev/pts/1>, " GET /en/graphs 200 in 628ms\n", 29) = 29`
- **THEN** Specter outputs the line `GET /en/graphs 200 in 628ms`

#### Scenario: Reopened pipe
- **WHEN** the target's fd 1 points to `pipe:[4021]` and the target writes a line to fd 9, whose destination is `pipe:[4021]`
- **THEN** Specter outputs the line

#### Scenario: Reopened descriptor kept apart from fd 1
- **WHEN** the target writes `hel` to fd 1 and `abc\n` to fd 22, both leading to `/dev/pts/1`, then `lo\n` to fd 1
- **THEN** Specter outputs the lines `abc` and `hello`, not a mix of the two

#### Scenario: Reopened descriptor and --stderr
- **WHEN** `--stderr` is set and the target writes a line to fd 22, whose destination is the target's stderr terminal
- **THEN** the line is not output, because writes through other descriptors count as stdout

#### Scenario: Unfinished write through a reopened descriptor
- **WHEN** strace prints `[pid 101] write(22</dev/pts/1>, "\x61\x0a", 2 <unfinished ...>` and later `[pid 101] <... write resumed>) = 2`, and the target's stdout is `/dev/pts/1`
- **THEN** Specter outputs the line `a`

#### Scenario: Other descriptor to /dev/null
- **WHEN** the target's fd 1 points to `/dev/null` and the target writes to fd 7, whose destination is `/dev/null`
- **THEN** the write is not output

#### Scenario: Other descriptor to a different destination
- **WHEN** the target's stdout is `/dev/pts/1` and the target writes to fd 5, whose destination is `socket:[51769]`
- **THEN** the write is not output

#### Scenario: Other descriptor without a destination
- **WHEN** strace prints a write to fd 22 without a `<...>` annotation
- **THEN** the write is not output

#### Scenario: Log file notice still works
- **WHEN** the target writes to fd 8, whose destination is the regular file `/var/log/app.log`, and that file is not the target's stdout or stderr
- **THEN** the write is not output and the "writes to its own log file" notice is printed once
