#!/bin/sh
# Run the test suite with coverage and fail when total statement coverage is
# below the floor.
#
# Usage: scripts/check-coverage.sh [MIN_PERCENT] [PROFILE]
set -eu

min=${1:-85}
profile=${2:-coverage.out}

# CGO_ENABLED=0 matches the release build and lets the sandbox tests apply
# Landlock (per-thread system calls are unavailable in cgo builds).
CGO_ENABLED=0 go test -count=1 -covermode=atomic -coverprofile="$profile" ./...

total=$(go tool cover -func="$profile" | awk '$1 == "total:" { sub("%", "", $NF); print $NF }')
echo
echo "total statement coverage: ${total}% (floor: ${min}%)"
if awk -v t="$total" -v m="$min" 'BEGIN { exit !(t + 0 < m + 0) }'; then
	echo "FAIL: coverage is below the floor" >&2
	exit 1
fi
