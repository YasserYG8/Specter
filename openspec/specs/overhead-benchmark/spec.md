# overhead-benchmark Specification

## Purpose
Measure how much Specter slows a target while attached, with a repeatable benchmark, and publish the measured numbers.

## Requirements

### Requirement: Repeatable Overhead Benchmark
The repository SHALL provide a benchmark that measures how much Specter slows a target while attached. It SHALL run the same HTTP load against the test server twice, once without Specter and once with Specter attached, at three loads: a light fixed rate, a fixed rate of 1,000 requests per second, and a closed loop with a fixed number of connections that finds the peak throughput. It SHALL repeat each measurement at least 5 times, and SHALL report the median and the spread of the achieved rate, the median and 99th percentile latency, and Specter's CPU use. For a fixed rate, latency SHALL be measured from the time each request was scheduled, not from the time it was sent.

#### Scenario: Comparison table
- **WHEN** the benchmark finishes
- **THEN** it prints one table with, for each load, the result without Specter, the result with Specter, and the relative difference

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
The README SHALL state the overhead measured by this benchmark, with the machine and the date, in place of numbers that were not produced by it. Until a reader exists that passes the 10% target, the README SHALL say that production use is not recommended yet.

#### Scenario: README shows measured numbers
- **WHEN** the benchmark has been run for the `strace` source
- **THEN** the performance section of the README shows its table or a summary of it, with the machine and the date

#### Scenario: Production warning
- **WHEN** no reader passes the 10% target
- **THEN** the README says that production use is not recommended yet
