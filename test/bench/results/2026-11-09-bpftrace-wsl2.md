# bpftrace reader overhead (eBPF spike), WSL2, 2026-11-09

Made with `READER=bpftrace scripts/bench-overhead.sh` (5 runs of 10 s after 2 s of warm-up, for each load, with and without the reader attached). The reader is `experiments/ebpf-spike/capture.bt`: it prints the `write()` calls on fd 1 and 2 of the processes named `testserver`, with the filter running inside the kernel. The server is `test/e2e/testserver`, as in the `strace` report next to this file.

## Table

Machine: AMD Ryzen 5 3500U (4 cores, 8 threads), WSL2 on Windows 11, kernel 6.6.87.2-microsoft-standard-WSL2, bpftrace v0.20.2, run as root, 2026-11-09

| Load | Median latency without bpftrace | Median latency with bpftrace | Change | p99 without / with | Rate without / with | bpftrace CPU | strace CPU | Lines captured |
|---|---|---|---|---|---|---|---|---|
| 100 requests/s | 2.32 ms (spread 7%) | 2.42 ms (spread 4%) | +4% | 3.57 / 3.68 ms | 100 / 100 requests/s | 2.1% | 0.0% | 6000 of 6000 (100.00%) |
| 1000 requests/s | 1.37 ms (spread 2%) | 1.45 ms (spread 4%) | +6% | 2.74 / 3.00 ms | 1000 / 1000 requests/s | 13.3% | 0.0% | 60000 of 60000 (100.00%) |
| closed loop (peak) | 1.56 ms (spread 5%) | 1.84 ms (spread 5%) | +18% | 9.22 / 11.51 ms | 29023 / 23983 requests/s | 30.8% | 0.0% | 1440552 of 1440552 (100.00%) |


## Full report

```
Machine: AMD Ryzen 5 3500U (4 cores, 8 threads), WSL2 on Windows 11, kernel 6.6.87.2-microsoft-standard-WSL2, bpftrace v0.20.2, run as root, 2026-11-09

Load: 100 requests per second, 5 runs each
  median latency   2.32 ms (spread 7%)  ->  2.42 ms (spread 4%)   +4%
  99th percentile  3.57 ms  ->  3.68 ms
  achieved rate    100 requests/s  ->  100 requests/s
  CPU use          bpftrace 2.1% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   6000 of 6000 (100.00%)
  target           MET (median latency +4%, below 10%, and 100.0% of the offered rate)

Load: 1000 requests per second, 5 runs each
  median latency   1.37 ms (spread 2%)  ->  1.45 ms (spread 4%)   +6%
  99th percentile  2.74 ms  ->  3.00 ms
  achieved rate    1000 requests/s  ->  1000 requests/s
  CPU use          bpftrace 13.3% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   60000 of 60000 (100.00%)
  target           MET (median latency +6%, below 10%, and 100.0% of the offered rate)

Load: closed loop (as fast as the server goes), 5 runs each
  median latency   1.56 ms (spread 5%)  ->  1.84 ms (spread 5%)   +18%
  99th percentile  9.22 ms  ->  11.51 ms
  achieved rate    29023 requests/s  ->  23983 requests/s
  CPU use          bpftrace 30.8% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   1440552 of 1440552 (100.00%)
  target           not judged (the target is for fixed-rate loads)

```
