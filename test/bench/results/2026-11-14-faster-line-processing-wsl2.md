# Faster line processing, eBPF engine, WSL2, 2026-11-14

Made with `ENGINE=ebpf scripts/bench-overhead.sh` (runs of 10 s after 2 s of warm-up, with and without Specter attached). The server is `test/e2e/testserver`, whose `/json` endpoint writes one log line to a pipe per request. It measures the change `faster-line-processing`: the pipeline decodes each line once and skips the escape-code regular expressions for lines that have none.

Two real Specter binaries were built, one from git HEAD before the change (`2be2fd8`, called **before**) and one from the working tree with the change (called **after**). Both ran on the same machine in the same session, one after the other.

**Read this first:**

- **At peak load the engine now captures every line.** Closed loop: 64.63% of lines before, 100.00% after. The four measurements of the old code (one of 5 runs, three short ones) gave 64.4% to 65.3%. The seven measurements of the new code (five single pairs and two short sets of 2 runs) all gave 100.00%.
- **Specter uses less CPU for it:** 148.8% of one core before (138% to 140% in three short runs), 101.6% after (94% to 110% in the five single pairs).
- **At 1,000 requests/s** both versions capture 100% of the lines. Specter's CPU fell from 49.7% to 42.4%, and the latency change stayed under 10% (+8% before, +7% after).
- **A cost at peak:** with Specter attached, the server handled 19,733 requests/s after the change and 22,964 before. Specter now keeps every line instead of losing about a third, so each write of the server costs more. This is the likely reason, and it is not proven. Median latency with Specter went from 1.96 ms to 2.19 ms.
- **WSL2 on a laptop is noisy.** The closed-loop baseline spread is 14% in the new series. Treat differences of about 10% with care. The capture numbers (64% against 100%) are far outside that noise.

## Table

Machine: AMD Ryzen 5 3500U (4 cores, 8 threads), WSL2 on Windows 11, kernel 6.6.87.2-microsoft-standard-WSL2, Specter with the ebpf engine, run as root, 2026-11-14

Before (git HEAD `2be2fd8`):

| Load | Median latency without Specter (ebpf) | Median latency with Specter (ebpf) | Change | p99 without / with | Rate without / with | Specter (ebpf) CPU | Lines captured |
|---|---|---|---|---|---|---|---|
| 1000 requests/s | 0.99 ms (spread 2%) | 1.06 ms (spread 1%) | +8% | 1.91 / 2.14 ms | 1000 / 1000 requests/s | 49.7% | 60000 of 60000 (100.00%) |
| closed loop (peak) | 1.34 ms (spread 1%) | 1.96 ms (spread 5%) | +46% | 7.91 / 12.02 ms | 33716 / 22964 requests/s | 148.8% | 874941 of 1353838 (64.63%) |

After (with the change):

| Load | Median latency without Specter (ebpf) | Median latency with Specter (ebpf) | Change | p99 without / with | Rate without / with | Specter (ebpf) CPU | Lines captured |
|---|---|---|---|---|---|---|---|
| 1000 requests/s | 0.99 ms (spread 2%) | 1.06 ms (spread 2%) | +7% | 1.88 / 2.11 ms | 1000 / 1000 requests/s | 42.4% | 60000 of 60000 (100.00%) |
| closed loop (peak) | 1.33 ms (spread 14%) | 2.19 ms (spread 6%) | +65% | 7.86 / 14.05 ms | 34151 / 19733 requests/s | 101.6% | 1180749 of 1180749 (100.00%) |

## How the runs were made, and what is missing

- **Before:** one invocation of the script with `RATES="1000" CLOSED=1` (5 runs per load, alternating the order of the two conditions).
- **After, 1,000 requests/s:** the last of three invocations of the new binary with `RATES="1000" CLOSED=1`. All three stopped in the closed loop, but each had already finished this load, and the raw results of the last one were kept.
- **After, closed loop:** five separate invocations with `RUNS=1`, merged into one set of five. Each ran the load without Specter first, then with Specter, so the order was not alternated. They were made this way because of the failure below.
- **100 requests/s was not measured** in this comparison. It is not where the change matters. Two earlier full runs included it, but they stopped at the failure below before the report was written, and their results were lost.
- **No native Linux runner was used.** Everything here is WSL2.
- **The earlier 75.65% and 151.6% in `proposal.md`** come from a run of 2026-11-14 whose report was not saved. This report replaces it as the "before", measured in the same session as the "after".

## A failure of the benchmark, in both versions

When a Specter with the `ebpf` engine attaches right after another Specter was stopped, which is what happens from the second run of the closed loop, it sometimes prints nothing for 20 seconds. The script then stops with `specter did not start the eBPF engine within 20 seconds`, and Specter's error output is empty.

It happens with the code before the change as well, so the change did not cause it. Counting the invocations that reached such an attach:

- before: 2 failed out of 6
- after: 6 failed out of 8

That is too few to say whether the change makes it more frequent. The cause is not known. It deserves its own investigation, since it means the `ebpf` engine can take more than 20 seconds to start on a busy machine, which matters for the stage "safe on real servers".

## Full report

Before:

```
Machine: AMD Ryzen 5 3500U with Radeon Vega Mobile Gfx, 8 cores, kernel 6.6.87.2-microsoft-standard-WSL2, WSL2, Specter with the ebpf engine, 2026-11-14

Load: 1000 requests per second, 5 runs each
  median latency   0.99 ms (spread 2%)  ->  1.06 ms (spread 1%)   +8%
  99th percentile  1.91 ms  ->  2.14 ms
  achieved rate    1000 requests/s  ->  1000 requests/s
  CPU use          Specter (ebpf) 49.7% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   60000 of 60000 (100.00%)
  target           MET (median latency +8%, below 10%, and 100.0% of the offered rate)

Load: closed loop (as fast as the server goes), 5 runs each
  median latency   1.34 ms (spread 1%)  ->  1.96 ms (spread 5%)   +46%
  99th percentile  7.91 ms  ->  12.02 ms
  achieved rate    33716 requests/s  ->  22964 requests/s
  CPU use          Specter (ebpf) 148.8% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   874941 of 1353838 (64.63%)
  target           not judged (the target is for fixed-rate loads)
```

After:

```
Machine: AMD Ryzen 5 3500U with Radeon Vega Mobile Gfx, 8 cores, kernel 6.6.87.2-microsoft-standard-WSL2, WSL2, Specter with the ebpf engine, 2026-11-14

Load: 1000 requests per second, 5 runs each
  median latency   0.99 ms (spread 2%)  ->  1.06 ms (spread 2%)   +7%
  99th percentile  1.88 ms  ->  2.11 ms
  achieved rate    1000 requests/s  ->  1000 requests/s
  CPU use          Specter (ebpf) 42.4% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   60000 of 60000 (100.00%)
  target           MET (median latency +7%, below 10%, and 100.0% of the offered rate)

Load: closed loop (as fast as the server goes), 5 runs each
  median latency   1.33 ms (spread 14%)  ->  2.19 ms (spread 6%)   +65%
  99th percentile  7.86 ms  ->  14.05 ms
  achieved rate    34151 requests/s  ->  19733 requests/s
  CPU use          Specter (ebpf) 101.6% (of one core; work done inside the kernel is charged to the server and not counted here)
  lines captured   1180749 of 1180749 (100.00%)
  target           not judged (the target is for fixed-rate loads)
  note             the baseline runs spread 14% (latency) and 11% (rate); the machine was busy or noisy
```
