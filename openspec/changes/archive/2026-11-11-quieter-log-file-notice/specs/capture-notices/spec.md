## MODIFIED Requirements

### Requirement: Own Log File Notice
The system SHALL print a notice to stderr, once per path, when a followed process writes to a regular file that is a log file through a file descriptor other than 1 or 2, with the `strace` or the `ebpf` source. The notice SHALL name the file. A file SHALL count as a log file when `log` or `logs` appears as a whole word in its path, compared without regard to case: either a part of the file name, where parts are separated by `.`, `-` and `_` (so `app.log`, `access_log`, `error-logs.txt` and `app.log.1` count), or a folder in the path named `log` or `logs` (so `/var/log/syslog` and `~/.pm2/logs/app-out.txt` count). Letters inside a longer word SHALL NOT count, so `catalog.json` and `blog.sqlite` are not log files. A file named `LOG`, `LOG.old` or `LOG.old.<digits>` SHALL NOT count when a file named `CURRENT` is in the same folder, because that is the internal log of an embedded database (the LevelDB and RocksDB layout, used by Turbopack and others). Both sources SHALL use the same rule.

#### Scenario: Target writes to its own log file
- **WHEN** a traced process writes to fd 3 and `/proc/<pid>/fd/3` resolves to the regular file `/var/log/app.log`
- **THEN** a single notice naming `/var/log/app.log` is printed to stderr, and nothing is added to stdout

#### Scenario: Repeated writes to the same log file
- **WHEN** a traced process writes to the same log file many times
- **THEN** only one notice is printed for that path

#### Scenario: Write to a socket
- **WHEN** a traced process writes to a file descriptor that resolves to a socket
- **THEN** no notice is printed

#### Scenario: Write to a regular file without log in its name
- **WHEN** a traced process writes to the regular file `/data/app.db`
- **THEN** no notice is printed

#### Scenario: Log as part of a longer word
- **WHEN** a traced process writes to the regular file `/srv/shop/catalog.json`
- **THEN** no notice is printed

#### Scenario: Log folder
- **WHEN** a traced process writes to the regular file `/home/dev/.pm2/logs/api-out.txt`
- **THEN** a single notice naming that file is printed

#### Scenario: Turbopack cache database
- **WHEN** a Next.js dev server writes to `/app/.next/dev/cache/turbopack/f37fad94/LOG`, and that folder also holds a file named `CURRENT`
- **THEN** no notice is printed

#### Scenario: File named LOG without a database
- **WHEN** a traced process writes to the regular file `/srv/app/LOG` and there is no `CURRENT` file in `/srv/app`
- **THEN** a single notice naming `/srv/app/LOG` is printed

#### Scenario: Same rule for the eBPF engine
- **WHEN** Specter attaches with the `ebpf` source and the target writes to `/srv/shop/catalog.json` and to `/var/log/app.log` through descriptors other than 1 and 2
- **THEN** only the notice for `/var/log/app.log` is printed
