# eBPF engine overhead, WSL2, 2026-11-09

Made with `ENGINE=ebpf scripts/bench-overhead.sh` (5 runs of 10 s after 2 s of warm-up, for each load, with and without Specter attached). The server is `test/e2e/testserver`, whose `/json` endpoint writes one log line to a pipe per request. This is the real Specter binary with its `ebpf` engine. The `strace` engine on the same machine is in `2026-11-09-strace-wsl2.md`.

**Read this first:** this is WSL2 on a laptop, which is noisy. At 1,000 requests per second the unrounded increase of the median latency is +9.66% (1.369 ms to 1.501 ms), which is under the 10% target by a small margin, and the five runs with Specter ranged from 1.29 to 1.86 ms. At the peak rate the engine captured 78.8% of the lines, because Specter's own pipeline could not keep up and the ring buffer filled: the program counts the lost writes and Specter reports them, but they were lost. The target must be confirmed on a native Linux machine.

## Table

Machine: AMD Ryzen 5 3500U (4 cores, 8 threads), WSL2 on Windows 11, kernel 6.6.87.2-microsoft-standard-WSL2, Specter with the ebpf engine, run as root, 2026-11-09

| Load | Median latency without Specter (ebpf) | Median latency with Specter (ebpf) | Change | p99 without / with | Rate without / with | Specter (ebpf) CPU | strace CPU | Lines captured |
|---|---|---|---|---|---|---|---|---|
| 100 requests/s | 2.25 ms (spread 5%) | 2.35 ms (spread 6%) | +4% | 3.65 / 4.09 ms | 100 / 100 requests/s | 22.9% | 0.0% | 6000 of 6000 (100.00%) |
| 1000 requests/s | 1.37 ms (spread 4%) | 1.50 ms (spread 38%) | +10% | 4.87 / 23.33 ms | 1000 / 1000 requests/s | 75.5% | 0.0% | 60000 of 60000 (100.00%) |
| closed loop (peak) | 2.57 ms (spread 12%) | 4.01 ms (spread 34%) | +56% | 20.77 / 30.32 ms | 16025 / 10186 requests/s | 149.0% | 0.0% | 509403 of 646084 (78.84%) |

Note: for closed loop (as fast as the server goes) the runs without Specter (ebpf) differ by 12% (latency) and 19% (rate), so the machine was too noisy for a small change to mean much.

## Full report

```
Machine: AMD Ryzen 5 3500U (4 cores, 8 threads), WSL2 on Windows 11, kernel 6.6.87.2-microsoft-standard-WSL2, Specter with the ebpf engine, run as root, 2026-11-09

Load: 100 requests per second, 5 runs each
  median latency   2.25 ms (spread 5%)  ->  2.35 ms (spread 6%)   +4%
  99th percentile  3.65 ms  ->  4.09 ms
  achieved rate    100 requests/s  ->  100 requests/s
  CPU use          Specter (ebpf) 22.9% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   6000 of 6000 (100.00%)
  target           MET (median latency +4%, below 10%, and 100.0% of the offered rate)

Load: 1000 requests per second, 5 runs each
  median latency   1.37 ms (spread 4%)  ->  1.50 ms (spread 38%)   +10%
  99th percentile  4.87 ms  ->  23.33 ms
  achieved rate    1000 requests/s  ->  1000 requests/s
  CPU use          Specter (ebpf) 75.5% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   60000 of 60000 (100.00%)
  target           MET (median latency +10%, below 10%, and 100.0% of the offered rate)

Load: closed loop (as fast as the server goes), 5 runs each
  median latency   2.57 ms (spread 12%)  ->  4.01 ms (spread 34%)   +56%
  99th percentile  20.77 ms  ->  30.32 ms
  achieved rate    16025 requests/s  ->  10186 requests/s
  CPU use          Specter (ebpf) 149.0% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   509403 of 646084 (78.84%)
  target           not judged (the target is for fixed-rate loads)
  note             the baseline runs spread 12% (latency) and 19% (rate); the machine was busy or noisy

```
