#!/bin/sh
# ci-distro-tests.sh: runs inside a distribution container (see the "distros" job in .github/workflows/ci.yaml).
# It installs the tools Specter needs with the distribution's package manager, then runs the test binaries that
# were built once, statically, outside the container.
#
# Environment:
#   INSTALL   the command that installs strace, lsof and iproute2 (required)
# The repository is mounted at /work and the binaries are in /work/bin.
set -eu

[ -n "${INSTALL:-}" ] || {
  echo "ci-distro-tests: INSTALL is not set" >&2
  exit 2
}

sh -c "$INSTALL"
# shellcheck disable=SC1091 # /etc/os-release exists in the container, not where this file is checked
echo "--- $(. /etc/os-release && echo "$PRETTY_NAME"), $(uname -m), kernel $(uname -r)"
strace -V | head -n 1

failed=0
for t in strace:internal/source/strace resolver:internal/resolver e2e:test/e2e; do
  name=${t%%:*}
  dir=${t#*:}
  echo "=== $name tests"
  log="/tmp/$name.log"
  if (cd "/work/$dir" && "/work/bin/$name.test" -test.timeout=10m -test.v >"$log" 2>&1); then
    passed=$(grep -c -- '--- PASS' "$log" || true)
    skipped=$(grep -c -- '--- SKIP' "$log" || true)
    echo "passed: $passed, skipped: $skipped"
    # A green run in which the live tests all skipped proves nothing, for example when ptrace is not allowed
    if [ "$name" = e2e ] && [ "$passed" -lt 1 ]; then
      echo "the E2E tests did not run: every test was skipped"
      grep -- '--- SKIP' "$log" | head -n 5 || true
      failed=1
    fi
  else
    echo "FAILED. The failing tests:"
    grep -E -- '^(--- FAIL|panic:|FAIL)|test timed out|^=== RUN' "$log" | grep -E -- '--- FAIL|panic:|^FAIL|timed out' | head -n 20 || true
    echo "The last lines of the log:"
    tail -n 60 "$log"
    failed=1
  fi
done
exit "$failed"
