#!/bin/sh
# Prove the committed configuration enforces every requested rule.
set -eu
root=$(pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
for rule in unchecked-type-assertion redefines-builtin-id unnecessary-format; do
    case "$rule" in
        unchecked-type-assertion)
            code='package fixture; func assertion(value any) string { return value.(string) }' ;;
        redefines-builtin-id)
            code='package fixture; func shadow() int { len := 1; return len }' ;;
        unnecessary-format)
            code='package fixture; import "fmt"; func constant() error { return fmt.Errorf("constant") }' ;;
    esac
    printf '%s\n' "$code" > "$fixture/fixture.go"
    if "$@" -config "$root/revive.toml" -formatter friendly "$fixture/fixture.go" > "$fixture/output" 2>&1; then
        cat "$fixture/output"
        echo "Revive accepted $rule fixture" >&2
        exit 1
    fi
    cat "$fixture/output"
    # A download/tool failure is not evidence that the configured rule ran.
    grep -F "https://revive.run/r#$rule" "$fixture/output"
done
printf '%s\n' 'package fixture; func checked(value any) (string, bool) { result, ok := value.(string); return result, ok }' > "$fixture/fixture.go"
"$@" -config "$root/revive.toml" -formatter friendly "$fixture/fixture.go"
