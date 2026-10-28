#!/bin/sh
# Exercise the real scanner and strict suppression policy outside the checkout.
set -eu
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
printf '%s\n' 'module gosec-fixture' 'go 1.27.0' > "$fixture/go.mod"
cat > "$fixture/fixture.go" <<'GO'
package fixture
import "os"
func accepted(path string) ([]byte, error) {
    // #nosec G304 -- Synthetic accepted path; prove this annotation applies to this call only.
    return os.ReadFile(path)
}
func rejected(path string) ([]byte, error) {
    return os.ReadFile(path)
}
GO
cd "$fixture"
if "$@" -exclude-generated -nosec-require-rules -nosec-require-justification -fmt=text ./... > "$fixture/output" 2>&1; then
    cat "$fixture/output"
    echo 'gosec accepted an unsuppressed file access beside a suppression' >&2
    exit 1
fi
cat "$fixture/output"
# Tool failures do not count as detecting the introduced violation.
grep -F 'G304' "$fixture/output"
# Remove the unrelated violation. The narrowly accepted call should pass.
sed '/^func rejected/,$d' "$fixture/fixture.go" > "$fixture/fixed"
mv "$fixture/fixed" "$fixture/fixture.go"
"$@" -exclude-generated -nosec-require-rules -nosec-require-justification -fmt=text ./...
