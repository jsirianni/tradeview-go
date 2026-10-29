#!/bin/sh
# The profile exists only for this invocation. Summaries stay in command output.
set -eu
go_command=${1:-go}
profile_dir=$(mktemp -d)
trap 'rm -rf "$profile_dir"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
"$go_command" version
"$go_command" env GOOS GOARCH
# Never accept a partial -run selection or cached results for the baseline gate.
"$go_command" test -count=1 -covermode=atomic -coverprofile="$profile_dir/coverage.out" ./...
awk -f scripts/coverage-summary.awk scripts/coverage-baseline.tsv "$profile_dir/coverage.out"
