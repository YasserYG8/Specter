#!/bin/sh
# ci-annotate.sh: turns the end of a log into an error annotation of the GitHub Actions run.
#
# Job logs need a GitHub login to read, but annotations are shown on the run page and through the public API
# (GET /repos/<owner>/<repo>/check-runs/<job id>/annotations), so a failure can be read without one.
#
#   scripts/ci-annotate.sh <title> <file> [lines]     the last [lines] (default 40) of file
#
# It only prints a workflow command and exits 0. Callers decide whether the step fails.
set -eu

title=${1:?usage: ci-annotate.sh <title> <file> [lines]}
file=${2:?usage: ci-annotate.sh <title> <file> [lines]}
lines=${3:-40}

[ -f "$file" ] || exit 0

# A workflow command needs % and line breaks escaped. The message is kept short: the limit is 64 KB, and a long
# annotation is hard to read.
message=$(tail -n "$lines" "$file" | cut -c1-300 | sed -e 's/%/%25/g' -e 's/\r//g' | awk '{printf "%s%%0A", $0}')
printf '::error title=%s::%s\n' "$title" "$message"
