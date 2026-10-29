#!/bin/sh
# Synthetic weighted profiles prove the gate, including failures, without rerunning tests.
set -eu
root=$(pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
cat > "$fixture/baseline" <<'BASELINE'
runtime 9000 10000 50
generator 80 100 50
automation 90 100 50
generated 1 100 -
BASELINE
cat > "$fixture/profile" <<'PROFILE'
mode: atomic
github.com/jsirianni/tradeview-go/pkg/tradeview/client.go:1.1,2.1 8950 1
github.com/jsirianni/tradeview-go/pkg/tradeview/client.go:3.1,4.1 1050 0
github.com/jsirianni/tradeview-go/internal/generate/generate.go:1.1,2.1 80 1
github.com/jsirianni/tradeview-go/internal/generate/generate.go:3.1,4.1 20 0
github.com/jsirianni/tradeview-go/internal/updatego/main.go:1.1,2.1 90 1
github.com/jsirianni/tradeview-go/internal/updatego/main.go:3.1,4.1 10 0
github.com/jsirianni/tradeview-go/pkg/tradeview/api.gen.go:1.1,2.1 100 0
PROFILE
check() {
    awk -f "$root/scripts/coverage-summary.awk" "$fixture/baseline" "$1"
}
check "$fixture/profile" > "$fixture/output"
grep -F '8950/10000 = 89.50%' "$fixture/output"
grep -F '100 = 0.00%' "$fixture/output"
reject() {
    if check "$fixture/invalid" > "$fixture/output" 2>&1; then
        echo "Coverage accepted $1" >&2
        exit 1
    fi
    grep -F "$2" "$fixture/output"
}
sed -e 's/8950 1/8949 1/' -e 's/1050 0/1051 0/' "$fixture/profile" > "$fixture/invalid"
reject 'a runtime regression hidden by rounding' 'runtime regressed beyond'
sed -e 's/80 1/79 1/' -e 's/20 0/21 0/' "$fixture/profile" > "$fixture/invalid"
reject 'a generator regression' 'generator regressed beyond'
sed -e 's/90 1/89 1/' -e 's/10 0/11 0/' "$fixture/profile" > "$fixture/invalid"
reject 'an automation regression' 'automation regressed beyond'
sed '/api.gen.go/d' "$fixture/profile" > "$fixture/invalid"
reject 'missing generated coverage' 'missing profile statements for generated'
sed 's@pkg/tradeview/client.go@internal/new/package.go@' "$fixture/profile" > "$fixture/invalid"
reject 'an unclassified package' 'unclassified file'
sed 's/mode: atomic/mode: count/' "$fixture/profile" > "$fixture/invalid"
reject 'a changed coverage mode' 'expected an atomic coverage profile'
sed 's/8950 1/invalid 1/' "$fixture/profile" > "$fixture/invalid"
reject 'malformed statement counts' 'invalid or duplicate profile block'
cat "$fixture/profile" > "$fixture/invalid"
sed -n '2p' "$fixture/profile" >> "$fixture/invalid"
reject 'duplicate profile blocks' 'invalid or duplicate profile block'
printf 'runtime 1 2 50\n' >> "$fixture/baseline"
cp "$fixture/profile" "$fixture/invalid"
reject 'a duplicate baseline group' 'invalid baseline row'

# The real runner must delete its private profile directory on both test success
# and failure, and preserve the failing test exit code. No module files are used.
mkdir "$fixture/temp"
cat > "$fixture/fake-go" <<'GO'
#!/bin/sh
case "$1" in
    version|env) exit 0 ;;
    test)
        for arg in "$@"; do
            case "$arg" in -coverprofile=*) profile=${arg#-coverprofile=} ;; esac
        done
        cat > "$profile" <<'PROFILE'
mode: atomic
github.com/jsirianni/tradeview-go/pkg/tradeview/client.go:1.1,2.1 1 1
github.com/jsirianni/tradeview-go/internal/generate/generate.go:1.1,2.1 1 1
github.com/jsirianni/tradeview-go/internal/updatego/main.go:1.1,2.1 1 1
github.com/jsirianni/tradeview-go/pkg/tradeview/api.gen.go:1.1,2.1 1 1
PROFILE
        exit "${FIXTURE_TEST_EXIT:-0}"
        ;;
    *) exit 99 ;;
esac
GO
chmod +x "$fixture/fake-go"
TMPDIR="$fixture/temp" sh scripts/check-coverage.sh "$fixture/fake-go" > "$fixture/output"
test -z "$(find "$fixture/temp" -mindepth 1 -print)"
if TMPDIR="$fixture/temp" FIXTURE_TEST_EXIT=29 sh scripts/check-coverage.sh "$fixture/fake-go" > "$fixture/output" 2>&1; then
    echo 'Coverage runner accepted failed tests' >&2
    exit 1
else
    result=$?
fi
test "$result" -eq 29
test -z "$(find "$fixture/temp" -mindepth 1 -print)"
echo 'Coverage classification and regression fixtures passed'
