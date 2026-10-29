#!/bin/sh
# Check every versioned/new Go file, including generated source, without writing.
set -eu
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
git ls-files --cached --others --exclude-standard -z -- '*.go' > "$scratch/files"
if [ ! -s "$scratch/files" ]; then
    echo 'No Go files found for goimports' >&2
    exit 1
fi
# NUL-separated paths preserve spaces. xargs propagates tool failures.
xargs -0 "$@" -l -d < "$scratch/files" > "$scratch/output"
if [ -s "$scratch/output" ]; then
    cat "$scratch/output"
    echo 'goimports found import/format differences; run the pinned goimports tool with -w on the listed files' >&2
    exit 1
fi
echo 'Go imports are clean'
