# Both engines, native Linux (GitHub runner), 2026-11-10

Made by the `bench` workflow (`.github/workflows/bench.yaml`, run 42780073568, commit 2758cb6), which runs `ENGINE=ebpf scripts/bench-overhead.sh` and `ENGINE=strace scripts/bench-overhead.sh` as root on the same runner. Each load is 5 runs of 10 s after 2 s of warm-up, with and without Specter attached. The server is `test/e2e/testserver`, whose `/json` endpoint writes one log line to a pipe per request.

This is a virtual machine on a cloud host, not bare metal, but it is native Linux: no WSL2 layer, and the runs spread far less than on the laptop (1% to 6% at fixed rates).

**Summary:** at 100 and 1,000 requests per second the `ebpf` engine raised the median latency by 5% and 4% and captured every line. The `strace` engine raised it by 51% and 52%. At full speed `ebpf` kept 66% of the server's throughput (`strace`: 12%), and captured 74% of the lines, because Specter's own processing used more than one core and could not keep up.

An earlier run on this kernel (7.0.0-1017-azure) captured only 24% of the lines with the `ebpf` engine. The cause was the `syscalls:sys_enter_write` tracepoint, which on Linux 7.0 misses the writes of processes that were already running when it was attached. The engine now uses `raw_syscalls:sys_enter` and `sys_exit` (commit aec6fd9). Details are in `experiments/ebpf-spike/RESULT.md`.

## Table

Machine: AMD EPYC 7763 64-Core Processor, 4 cores, kernel 7.0.0-1017-azure (ubuntu-24.04 runner), run as root, 2026-11-10

| Engine | Load | Median latency without → with | Change | p99 without / with | Rate without / with | CPU of Specter (and strace) | Lines captured |
|---|---|---|---|---|---|---|---|
| ebpf | 100 requests/s | 0.79 → 0.83 ms | +5% | 1.29 / 1.33 ms | 100 / 100 | 4.7% | 6000 of 6000 (100%) |
| ebpf | 1000 requests/s | 0.65 → 0.68 ms | +4% | 1.28 / 1.37 ms | 1000 / 1000 | 18.9% | 60000 of 60000 (100%) |
| ebpf | closed loop (peak) | 1.44 → 2.26 ms | +57% | 6.82 / 9.95 ms | 35976 / 23687 | 108.0% | 1045819 of 1416484 (73.83%) |
| strace | 100 requests/s | 0.78 → 1.18 ms | +51% | 1.30 / 1.75 ms | 100 / 100 | 3.7% (+ 4.8%) | 6000 of 6000 (100%) |
| strace | 1000 requests/s | 0.67 → 1.01 ms | +52% | 1.28 / 2.02 ms | 1000 / 1000 | 27.9% (+ 37.5%) | 60000 of 60000 (100%) |
| strace | closed loop (peak) | 1.44 → 13.28 ms | +822% | 6.95 / 82.30 ms | 36003 / 4423 | 95.9% (+ 73.4%) | 265670 of 265670 (100%) |

The CPU of the `ebpf` engine counts only Specter. The work its programs do inside the kernel is charged to the server, so it shows up in the server's latency and rate, not in this column.

## Full reports

```
Machine: AMD EPYC 7763 64-Core Processor, 4 cores, kernel 7.0.0-1017-azure, Specter with the ebpf engine, 2026-11-10

Load: 100 requests per second, 5 runs each
  median latency   0.79 ms (spread 2%)  ->  0.83 ms (spread 3%)   +5%
  99th percentile  1.29 ms  ->  1.33 ms
  achieved rate    100 requests/s  ->  100 requests/s
  CPU use          Specter (ebpf) 4.7% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   6000 of 6000 (100.00%)
  target           MET (median latency +5%, below 10%, and 100.0% of the offered rate)

Load: 1000 requests per second, 5 runs each
  median latency   0.65 ms (spread 6%)  ->  0.68 ms (spread 1%)   +4%
  99th percentile  1.28 ms  ->  1.37 ms
  achieved rate    1000 requests/s  ->  1000 requests/s
  CPU use          Specter (ebpf) 18.9% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   60000 of 60000 (100.00%)
  target           MET (median latency +4%, below 10%, and 100.0% of the offered rate)

Load: closed loop (as fast as the server goes), 5 runs each
  median latency   1.44 ms (spread 2%)  ->  2.26 ms (spread 2%)   +57%
  99th percentile  6.82 ms  ->  9.95 ms
  achieved rate    35976 requests/s  ->  23687 requests/s
  CPU use          Specter (ebpf) 108.0% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   1045819 of 1416484 (73.83%)
  target           not judged (the target is for fixed-rate loads)
```

```
Machine: AMD EPYC 7763 64-Core Processor, 4 cores, kernel 7.0.0-1017-azure, Specter with the strace engine, strace -- version 6.8, 2026-11-10

Load: 100 requests per second, 5 runs each
  median latency   0.78 ms (spread 1%)  ->  1.18 ms (spread 6%)   +51%
  99th percentile  1.30 ms  ->  1.75 ms
  achieved rate    100 requests/s  ->  100 requests/s
  CPU use          Specter (strace) 3.7%, strace 4.8% (of one core)
  lines captured   6000 of 6000 (100.00%)
  target           NOT MET (median latency +51%, limit 10%)

Load: 1000 requests per second, 5 runs each
  median latency   0.67 ms (spread 3%)  ->  1.01 ms (spread 1%)   +52%
  99th percentile  1.28 ms  ->  2.02 ms
  achieved rate    1000 requests/s  ->  1000 requests/s
  CPU use          Specter (strace) 27.9%, strace 37.5% (of one core)
  lines captured   60000 of 60000 (100.00%)
  target           NOT MET (median latency +52%, limit 10%)

Load: closed loop (as fast as the server goes), 5 runs each
  median latency   1.44 ms (spread 3%)  ->  13.28 ms (spread 1%)   +822%
  99th percentile  6.95 ms  ->  82.30 ms
  achieved rate    36003 requests/s  ->  4423 requests/s
  CPU use          Specter (strace) 95.9%, strace 73.4% (of one core)
  lines captured   265670 of 265670 (100.00%)
  target           not judged (the target is for fixed-rate loads)
```
