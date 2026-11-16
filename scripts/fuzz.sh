#!/bin/sh
# Run every fuzz target in the module for a fixed time each.
#
# Usage: scripts/fuzz.sh [FUZZTIME]     (default 10s, any go -fuzztime value)
set -eu

fuzztime=${1:-10s}
status=0
for pkg in $(go list ./...); do
	targets=$(go test -list '^Fuzz' "$pkg" | grep '^Fuzz' || true)
	for target in $targets; do
		echo "=== $pkg $target ($fuzztime)"
		if ! go test -run '^$' -fuzz "^${target}\$" -fuzztime "$fuzztime" "$pkg"; then
			status=1
		fi
	done
done
exit $status
