#!/usr/bin/env bash
# bench-overhead.sh: how much does Specter slow a server while it is attached?
#
# For each load it starts the test server, then alternates runs without Specter and with Specter attached
# (RUNS of each), and prints the median of the runs with their spread. Latency is measured by test/bench/loadgen
# from the scheduled time of each request, so a slow server cannot hide its queue.
#
#   scripts/bench-overhead.sh                       # 100 and 1,000 requests/s, then the closed loop
#   RATES="1000" CLOSED=0 scripts/bench-overhead.sh # only 1,000 requests/s
#   FORMAT=markdown OUT_DIR=./bench-out scripts/bench-overhead.sh
#
# It needs Linux, strace, and permission to ptrace: run it as root, or set kernel.yama.ptrace_scope to 0.
# Go is needed unless SPECTER_BIN, TESTSERVER_BIN, LOADGEN_BIN and BENCHREPORT_BIN are all set.
#
# Settings (environment variables, defaults in brackets):
#   RUNS [5]         runs per condition and load
#   DURATION [10s]   measured time of one run
#   WARMUP [2s]      time before each run is measured
#   RATES ["100 1000"]  fixed-rate loads, in requests per second
#   CLOSED [1]       1 also runs the closed loop (peak throughput); 0 does not
#   CONNS [64]       connections of the load generator
#   FORMAT [text]    text or markdown
#   OUT_DIR []       keep the raw results here; by default they are deleted
#   SPECTER_FLAGS [] extra flags for specter, for example "--tree"
set -euo pipefail

RUNS=${RUNS:-5}
DURATION=${DURATION:-10s}
WARMUP=${WARMUP:-2s}
RATES=${RATES:-"100 1000"}
CLOSED=${CLOSED:-1}
CONNS=${CONNS:-64}
FORMAT=${FORMAT:-text}
OUT_DIR=${OUT_DIR:-}
SPECTER_FLAGS=${SPECTER_FLAGS:-}

die() {
  echo "bench-overhead: $*" >&2
  exit 1
}

# ---- checks ---------------------------------------------------------------------------------------------------

[ "$(uname -s)" = "Linux" ] || die "this needs Linux: Specter attaches with ptrace, which exists only there. On Windows, run it inside WSL2."
command -v strace >/dev/null 2>&1 || die "strace is not installed. Install it first, for example: sudo apt install strace"
[[ "$RUNS" =~ ^[0-9]+$ ]] && [ "$RUNS" -ge 1 ] || die "RUNS must be a positive whole number, got '$RUNS'"

scope=0
[ -r /proc/sys/kernel/yama/ptrace_scope ] && scope=$(cat /proc/sys/kernel/yama/ptrace_scope)
if [ "$scope" = "3" ] || { [ "$scope" != "0" ] && [ "$(id -u)" != "0" ]; }; then
  die "ptrace is not permitted here (kernel.yama.ptrace_scope is $scope and you are not root).
  Run this script as root, or lower the setting for the test and restore it afterwards:
    sudo sysctl -w kernel.yama.ptrace_scope=0
    sudo sysctl -w kernel.yama.ptrace_scope=$scope"
fi

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK=$(mktemp -d)
RESULTS=${OUT_DIR:-$WORK/results}
mkdir -p "$RESULTS"

SERVER_PID=""
SPECTER_PID=""

# shellcheck disable=SC2329 # called by the EXIT trap
cleanup() {
  [ -n "$SPECTER_PID" ] && kill -INT "$SPECTER_PID" 2>/dev/null || true
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  wait 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# ---- binaries -------------------------------------------------------------------------------------------------

build() { # name, package, env var
  local var=${3:-}
  if [ -n "${!var:-}" ]; then
    echo "${!var}"
    return
  fi
  command -v go >/dev/null 2>&1 || die "Go is needed to build $1. Install Go, or set $var to a built binary."
  (cd "$ROOT" && go build -buildvcs=false -o "$WORK/$1" "$2") >&2 || die "could not build $2"
  echo "$WORK/$1"
}

SPECTER=$(build specter ./cmd/specter SPECTER_BIN)
TESTSERVER=$(build testserver ./test/e2e/testserver TESTSERVER_BIN)
LOADGEN=$(build loadgen ./test/bench/loadgen LOADGEN_BIN)
BENCHREPORT=$(build benchreport ./test/bench/benchreport BENCHREPORT_BIN)

# ---- helpers --------------------------------------------------------------------------------------------------

CLK_TCK=$(getconf CLK_TCK)

# cpu_ticks <pid>: user and system time of the process so far, in clock ticks
cpu_ticks() {
  sed 's/.*) //' "/proc/$1/stat" 2>/dev/null | awk '{print $12 + $13}' || echo 0
}

tracer_of() {
  awk '/^TracerPid:/ {print $2}' "/proc/$1/status" 2>/dev/null || echo 0
}

start_server() {
  : >"$WORK/ready"
  # The server's stdout is a pipe, as under a process manager. If it were a file, Specter would choose the file
  # source and trace nothing. The first line says the port and the PID; the rest is read and thrown away.
  "$TESTSERVER" -port=0 2>/dev/null | { head -n 1 >"$WORK/ready"; cat >/dev/null; } &
  for _ in $(seq 1 100); do
    grep -q SPECTER_TEST_SERVER_READY "$WORK/ready" 2>/dev/null && break
    sleep 0.1
  done
  grep -q SPECTER_TEST_SERVER_READY "$WORK/ready" || die "the test server did not start"
  PORT=$(sed -n 's/.*port=\([0-9]*\).*/\1/p' "$WORK/ready")
  SERVER_PID=$(sed -n 's/.*pid=\([0-9]*\).*/\1/p' "$WORK/ready")
}

stop_server() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  wait 2>/dev/null || true
  SERVER_PID=""
}

attach_specter() {
  # shellcheck disable=SC2086 # SPECTER_FLAGS is a list of words on purpose
  "$SPECTER" --pid "$SERVER_PID" --no-color $SPECTER_FLAGS >"$WORK/specter.out" 2>"$WORK/specter.err" &
  SPECTER_PID=$!
  for _ in $(seq 1 200); do
    [ "$(tracer_of "$SERVER_PID")" != "0" ] && break
    kill -0 "$SPECTER_PID" 2>/dev/null || die "specter exited before it attached:
$(cat "$WORK/specter.err")"
    sleep 0.1
  done
  [ "$(tracer_of "$SERVER_PID")" != "0" ] || die "specter did not attach within 20 seconds:
$(cat "$WORK/specter.err")"
  sleep 1 # let strace attach to every thread
}

detach_specter() {
  kill -INT "$SPECTER_PID" 2>/dev/null || true
  wait "$SPECTER_PID" 2>/dev/null || true
  SPECTER_PID=""
  for _ in $(seq 1 50); do # strace must be gone before the next run, or its detach would count in it
    [ "$(tracer_of "$SERVER_PID")" = "0" ] && return
    sleep 0.1
  done
  die "the server still has a tracer after specter stopped"
}

# one_run <load name> <rate> <condition> <n>
one_run() {
  local name=$1 rate=$2 cond=$3 n=$4
  local prefix="$RESULTS/$name.$cond.$n"
  local url="http://127.0.0.1:$PORT/json?msg=bench"
  local specter_before=0 strace_before=0 strace_pid=""

  if [ "$cond" = spec ]; then
    attach_specter
    strace_pid=$(pgrep -P "$SPECTER_PID" -x strace | head -n 1 || true)
    specter_before=$(cpu_ticks "$SPECTER_PID")
    [ -n "$strace_pid" ] && strace_before=$(cpu_ticks "$strace_pid")
  fi

  local t0 t1
  t0=$(date +%s.%N)
  "$LOADGEN" -url "$url" -rate "$rate" -conns "$CONNS" -duration "$DURATION" -warmup "$WARMUP" >"$prefix.json" ||
    die "the load generator failed:
$(cat "$prefix.json")"
  t1=$(date +%s.%N)

  if [ "$cond" = spec ]; then
    local specter_after strace_after=0
    specter_after=$(cpu_ticks "$SPECTER_PID")
    [ -n "$strace_pid" ] && strace_after=$(cpu_ticks "$strace_pid")
    sleep 1.5 # let the last lines reach Specter's output before they are counted
    local captured
    captured=$(grep -c 'bench' "$WORK/specter.out" || true)
    awk -v t0="$t0" -v t1="$t1" -v sb="$specter_before" -v sa="$specter_after" \
      -v tb="$strace_before" -v ta="$strace_after" -v hz="$CLK_TCK" -v cap="$captured" 'BEGIN {
        secs = t1 - t0
        printf "specter_cpu_pct=%.2f\nstrace_cpu_pct=%.2f\ncaptured_lines=%d\n", (sa - sb) / hz / secs * 100, (ta - tb) / hz / secs * 100, cap
      }' >"$prefix.kv"
    detach_specter
  fi
}

run_load() { # name rate
  local name=$1 rate=$2 n
  echo "load $name: $RUNS runs without and with Specter ($DURATION each, after $WARMUP of warm-up)" >&2
  start_server
  for n in $(seq 1 "$RUNS"); do
    # Alternate the order, so a machine that slowly gets busier or quieter does not favor one condition
    if [ $((n % 2)) -eq 1 ]; then
      one_run "$name" "$rate" base "$n"
      one_run "$name" "$rate" spec "$n"
    else
      one_run "$name" "$rate" spec "$n"
      one_run "$name" "$rate" base "$n"
    fi
    echo "  run $n of $RUNS done" >&2
  done
  stop_server
}

# ---- machine --------------------------------------------------------------------------------------------------

describe_machine() {
  local cpu cores wsl=""
  cpu=$(awk -F': ' '/model name/ {print $2; exit}' /proc/cpuinfo 2>/dev/null || true)
  [ -n "$cpu" ] || cpu=$(uname -m)
  cores=$(nproc 2>/dev/null || echo "?")
  grep -qi microsoft /proc/version 2>/dev/null && wsl=", WSL2"
  echo "$cpu, $cores cores, kernel $(uname -r)$wsl, $(strace -V 2>&1 | head -n 1), $(date +%Y-%m-%d)"
}

# ---- go -------------------------------------------------------------------------------------------------------

for rate in $RATES; do
  [[ "$rate" =~ ^[0-9]+$ ]] && [ "$rate" -ge 1 ] || die "RATES must be whole numbers of requests per second, got '$rate'"
  run_load "rate-$rate" "$rate"
done
[ "$CLOSED" = "1" ] && run_load closed 0

echo >&2
"$BENCHREPORT" -dir "$RESULTS" -format "$FORMAT" -machine "$(describe_machine)"
[ -n "$OUT_DIR" ] && echo "Raw results are in $OUT_DIR" >&2
exit 0
