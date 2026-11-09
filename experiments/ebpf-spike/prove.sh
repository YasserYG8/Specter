#!/usr/bin/env bash
# prove.sh: step 2 of the eBPF spike. Does a bpftrace program capture the lines of a server's stdout, with the
# filtering done in the kernel? Tries the Go test server and a Node.js server. Run as root.
#
#   experiments/ebpf-spike/prove.sh            # 200 requests to each server
#   REQUESTS=1000 experiments/ebpf-spike/prove.sh
set -uo pipefail

REQUESTS=${REQUESTS:-200}
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
WORK=$(mktemp -d)
PIDS=()
cleanup() {
  for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done
  wait 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT

die() { echo "prove: $*" >&2; exit 1; }
[ "$(id -u)" = 0 ] || die "run as root: loading eBPF programs needs privileges"
command -v bpftrace >/dev/null || die "bpftrace is not installed (sudo apt install bpftrace)"

# check <name> <comm> <port> <marker>: attach capture.bt to the process name, send requests, count the lines captured
check() {
  local name=$1 comm=$2 port=$3 marker=$4
  BPFTRACE_MAX_STRLEN=200 bpftrace "$HERE/capture.bt" "$comm" >"$WORK/$name.out" 2>"$WORK/$name.err" &
  local bt=$!
  PIDS+=("$bt")
  # bpftrace prints "Attaching 1 probe..." on stdout when it starts to load the program; give the load a moment
  for _ in $(seq 1 100); do grep -q "Attaching" "$WORK/$name.out" 2>/dev/null && break; sleep 0.1; done; sleep 1
  grep -q "Attaching" "$WORK/$name.out" || { cat "$WORK/$name.err"; die "bpftrace did not start"; }

  local i
  for i in $(seq 1 "$REQUESTS"); do curl -s -o /dev/null "http://127.0.0.1:$port/$marker?n=$i"; done
  sleep 1
  kill -INT "$bt" 2>/dev/null
  wait "$bt" 2>/dev/null

  local captured
  captured=$(grep -c "$marker" "$WORK/$name.out")
  echo "$name: sent $REQUESTS requests, captured $captured lines containing '$marker'"
  grep -m2 "$marker" "$WORK/$name.out" | sed 's/^/    /'
  grep -E 'write calls seen|bytes written' "$WORK/$name.out" | sed 's/^/    /'
}

# --- the Go test server, stdout on a pipe ---
(cd "$ROOT" && go build -buildvcs=false -o "$WORK/testserver" ./test/e2e/testserver) || die "cannot build the test server"
"$WORK/testserver" -port=0 2>/dev/null | { head -n 1 >"$WORK/go.ready"; cat >/dev/null; } &
for _ in $(seq 1 50); do grep -q READY "$WORK/go.ready" 2>/dev/null && break; sleep 0.1; done
GO_PORT=$(sed -n 's/.*port=\([0-9]*\).*/\1/p' "$WORK/go.ready")
GO_PID=$(sed -n 's/.*pid=\([0-9]*\).*/\1/p' "$WORK/go.ready")
PIDS+=("$GO_PID")
check "go-testserver" "testserver" "$GO_PORT" "json"

# --- a Node.js server, stdout on a pipe ---
if command -v node >/dev/null; then
  node "$HERE/node-server.js" 0 2>/dev/null | { head -n 1 >"$WORK/node.ready"; cat >/dev/null; } &
  for _ in $(seq 1 50); do grep -q listening "$WORK/node.ready" 2>/dev/null && break; sleep 0.1; done
  NODE_PORT=$(sed -n 's/^listening \([0-9]*\) pid.*/\1/p' "$WORK/node.ready")
  NODE_PID=$(sed -n 's/.*pid \([0-9]*\)$/\1/p' "$WORK/node.ready")
  PIDS+=("$NODE_PID")
  check "node-server" "node" "$NODE_PORT" "request"
else
  echo "node is not installed: skipping the Node.js server"
fi
