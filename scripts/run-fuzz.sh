#!/bin/sh
# Keep each exploration bounded; seed cases also run in ordinary go test.
set -eu
go_command=${1:-go}
fuzz_time=${2:-1000x}
fuzz_parallel=${3:-2}
for target in FuzzSymbolInfoJSON FuzzSymbolInfoBudget FuzzMarketHeader FuzzStreamFrames; do
    if "$go_command" test ./pkg/tradeview -run '^$' -fuzz "^${target}$" \
        -fuzztime "$fuzz_time" -fuzzminimizetime 2s -parallel "$fuzz_parallel" -timeout 2m; then
        continue
    else
        result=$?
    fi
    # Go writes a reproducer before failing. Preserve its full native corpus
    # text in CI logs as well as the local file, without uploading artifacts.
    corpus="pkg/tradeview/testdata/fuzz/$target"
    if [ -d "$corpus" ]; then
        find "$corpus" -type f -print -exec cat {} \;
    fi
    exit "$result"
done
