## MODIFIED Requirements

### Requirement: Repeatable Overhead Benchmark
The repository SHALL provide a benchmark that measures how much Specter slows a target while attached. It SHALL run the same HTTP load against the test server twice, once without Specter and once with Specter attached, at three loads: a light fixed rate, a fixed rate of 1,000 requests per second, and a closed loop with a fixed number of connections that finds the peak throughput. It SHALL measure one capture engine per run, selected with `ENGINE=strace` or `ENGINE=ebpf` and passed to Specter as `--source`, and the report SHALL name the engine. It SHALL repeat each measurement at least 5 times, and SHALL report the median and the spread of the achieved rate, the median and 99th percentile latency, and Specter's CPU use. For a fixed rate, latency SHALL be measured from the time each request was scheduled, not from the time it was sent.

#### Scenario: Comparison table
- **WHEN** the benchmark finishes
- **THEN** it prints one table with, for each load, the result without Specter, the result with Specter, and the relative difference

#### Scenario: Engine named in the report
- **WHEN** the benchmark runs with `ENGINE=ebpf`
- **THEN** Specter is started with `--source ebpf`, and the report says the engine was `ebpf`

#### Scenario: Target on a pipe
- **WHEN** the benchmark starts the test server
- **THEN** the server's stdout is a pipe, so that Specter uses the `strace` source and not the file source

#### Scenario: Noisy machine
- **WHEN** the spread between the runs without Specter is above 10%
- **THEN** the benchmark says that the machine is too noisy to judge a difference of 10%

#### Scenario: Missing permission
- **WHEN** the benchmark runs without permission to attach with `strace`
- **THEN** it stops before measuring and says to run as root or to lower `ptrace_scope`

#### Scenario: Slowdown at 1,000 requests per second
- **WHEN** a reader is measured at a fixed 1,000 requests per second
- **THEN** its slowdown is the relative increase of the median request latency, and it passes the 10% target only if that increase is under 10% and the achieved rate stays at 99% or more of the offered rate

### Requirement: Published Overhead Numbers
The README SHALL state the overhead measured by this benchmark for each engine, with the machine and the date, in place of numbers that were not produced by it. The eBPF numbers SHALL come from a native Linux machine, not WSL2. Until an engine passes the 10% target on native Linux, the README SHALL say that production use is not recommended with it. When the `ebpf` engine passes, the README SHALL say that production use needs the `ebpf` engine, and that the `strace` engine is still not recommended there.

#### Scenario: README shows measured numbers
- **WHEN** the benchmark has been run for both engines
- **THEN** the performance section of the README shows a table or summary for each engine, with the machine and the date

#### Scenario: Production warning
- **WHEN** no engine passes the 10% target
- **THEN** the README says that production use is not recommended yet

#### Scenario: eBPF passes
- **WHEN** the `ebpf` engine passes the 10% target on native Linux and the `strace` engine does not
- **THEN** the README says production use needs the `ebpf` engine (root, Linux 5.8 or later), and warns that the `strace` engine slows the server
