#!/bin/sh
# Exercise the real Makefile gate in a disposable tracked module.
set -eu

go_command=${1:-go}
make_command=${2:-make}
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT HUP INT TERM

cat > "$fixture/go.mod" <<'EOF'
module example.com/fixgate

go 1.27.0
EOF
cat > "$fixture/fixture.go" <<'EOF'
package fixture

import "strings"

func First(value string) string {
	first := strings.Split(value, ",")[0]
	return first
}
EOF

git -C "$fixture" init -q --initial-branch=main
git -C "$fixture" add go.mod fixture.go
git -C "$fixture" -c user.name=Fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false commit -qm 'Track modernization fixture'

if (cd "$fixture" && "$make_command" -f "$root/Makefile" GO="$go_command" fix-check) > "$fixture/check.log" 2>&1; then
	cat "$fixture/check.log"
	echo 'fix-check unexpectedly accepted a Go modernization rewrite' >&2
	exit 1
fi
cat "$fixture/check.log"
# Require evidence of the intended rewrite, rather than accepting any failure.
grep -F 'diff --git a/fixture.go b/fixture.go' "$fixture/check.log"
grep -F 'strings.Cut' "$fixture/check.log"

git -C "$fixture" restore -- fixture.go
(cd "$fixture" && "$make_command" -f "$root/Makefile" GO="$go_command" fix)
if git -C "$fixture" diff --quiet -- fixture.go; then
	echo 'make fix did not apply the expected modernization' >&2
	exit 1
fi
git -C "$fixture" add fixture.go
git -C "$fixture" -c user.name=Fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false commit -qm 'Apply Go modernization'
(cd "$fixture" && "$make_command" -f "$root/Makefile" GO="$go_command" fix-check)
echo 'Go modernization gate rejects rewrites and accepts committed fixes'
