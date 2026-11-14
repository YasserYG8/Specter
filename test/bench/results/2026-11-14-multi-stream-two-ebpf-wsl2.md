# Two eBPF sessions (multi-stream), WSL2, 2026-11-14

Made for the `multi-stream` change. With several targets, each target that uses the `ebpf` engine loads its own kernel program, so N targets run N programs on every `write` and `writev` on the machine. This report answers two questions: does starting two eBPF sessions in a row ever hang, and what does a second program cost?

Machine: AMD Ryzen 5 3500U (8 threads), WSL2 on Windows 11, kernel 6.6.87.2-microsoft-standard-WSL2, run as root, `ptrace_scope=0`.

## Does starting two eBPF sessions hang?

`specter P1 P2 --source ebpf --duration 3s` on two test servers, 20 times in a row (each run starts two new test servers, attaches, and detaches by `--duration`).

| Runs | Both sessions started | Hangs | Other failures | Time of one run (3 s of capture) |
|---|---|---|---|---|
| 20 | 20 | **0** | 0 | 3.6 to 4.1 s (so a start takes under 1.1 s) |

Every run printed two `Source: ebpf` lines and exited with status 0. The parked start-hang bug ("attaches seconds after another one stopped") did not show up. The case is not the same as that bug (here the first program is still loaded while the second one starts, and the two start in one process), so this shows only that two-at-once is not affected on this machine, not that the bug is gone.

The E2E tests in `test/e2e/multistream_test.go` also start two sessions for each engine (`strace` and `ebpf`) and passed.

## What does the second program cost?

`ENGINE=ebpf RATES=1000 CLOSED=0 RUNS=7 scripts/bench-overhead.sh`, twice:

- **one session**: Specter attached to the benchmark server (`--pid SERVER`).
- **two sessions**: the same, plus a second eBPF session in the same Specter process on an idle second test server (`--pid SERVER --pid IDLE`). The idle server writes nothing, so the second program adds only its hook on every write the benchmark server makes.

7 runs of 10 s (after 2 s of warm-up) with Specter and 7 without, alternating.

| Condition | Median latency without Specter | Median latency with Specter | Change | p99 without / with | Rate | Lines captured | Specter CPU |
|---|---|---|---|---|---|---|---|
| one eBPF session | 1.20 ms (spread 13%) | 1.32 ms (spread 28%) | +10% | 2.84 / 15.75 ms | 1000 / 1000 requests/s | 84000 of 84000 | 68.0% |
| two eBPF sessions | 1.19 ms (spread 14%) | 1.45 ms (spread 23%) | +22% | 2.65 / 14.26 ms | 1000 / 1000 requests/s | 84000 of 84000 | 71.6% |

**Read this first:** the machine is too noisy to judge. The runs without Specter differ by 13% and 14%, and the runs with Specter by 28% and 23%, which is as large as the difference between the two conditions. The script itself says "cannot judge" for both. What the numbers can show: no line was lost with two sessions, the offered rate was met, and the median latency was 0.13 ms higher with the second program (1.45 ms against 1.32 ms), which is the direction expected. The size of the cost is not known. The p99 did not get worse (14.3 ms against 15.8 ms), which also points to noise.

**Decision for the design (Decision 6):** the evidence does not show a clear cost, and it does not rule one out. Before a shared eBPF engine is considered, repeat this on native Linux (the GitHub runner used in `2026-11-10-both-engines-github-runner.md` is the right place), with more runs. Until then, the limit of 8 targets keeps the worst case bounded, and one target costs the same as before.

To repeat it: start a second test server (`testserver -port=0`, its stdout a pipe), then run the script twice with `SPECTER_FLAGS=""` and `SPECTER_FLAGS="--pid <its PID>"`.
