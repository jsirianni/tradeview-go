# Count statements, not lines, blocks, files, or averages of package percentages.
function reject(message) {
    print "coverage: " message > "/dev/stderr"
    invalid = 1
}
function integer(value) { return value ~ /^[0-9]+$/ }
NR == FNR {
    if ($0 ~ /^[[:space:]]*(#|$)/) next
    if (NF != 4 || $1 !~ /^(runtime|generator|generated)$/ || baseline_seen[$1]++ ||
        !integer($2) || !integer($3) || $3 == 0 || $2 > $3 ||
        ($1 == "generated" ? $4 != "-" : (!integer($4) || $4 > 10000))) {
        reject("invalid baseline row " FNR)
        next
    }
    baseline[$1] = 100 * $2 / $3
    tolerance[$1] = $4 / 100
    next
}
FNR == 1 {
    if ($0 != "mode: atomic") reject("expected an atomic coverage profile")
    next
}
{
    file = $1
    if (NF != 3 || !integer($2) || !integer($3) ||
        !sub(/:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/, "", file) || block_seen[$1]++) {
        reject("invalid or duplicate profile block at row " FNR)
        next
    }
    prefix = "github.com/jsirianni/tradeview-go/"
    if (index(file, prefix) != 1) {
        reject("unclassified file " file)
        next
    }
    file = substr(file, length(prefix) + 1)
    if (file == "pkg/tradeview/api.gen.go") group = "generated"
    else if (file ~ /^pkg\/tradeview\/[^/]+\.go$/) group = "runtime"
    else if (file ~ /^internal\/generate\/[^/]+\.go$/) group = "generator"
    else {
        reject("unclassified file " file)
        next
    }
    total[group] += $2
    if ($3 > 0) covered[group] += $2
}
END {
    count = split("runtime generator generated", groups, " ")
    for (i = 1; i <= count; i++) {
        group = groups[i]
        if (!baseline_seen[group]) reject("missing baseline group " group)
        if (!total[group]) reject("missing profile statements for " group)
    }
    if (invalid) exit 1
    print "Statement-weighted coverage (generated bindings are informational):"
    for (i = 1; i <= count; i++) {
        group = groups[i]
        percentage = 100 * covered[group] / total[group]
        printf "  %-9s %d/%d = %.2f%%; baseline %.2f%%; change %+.2f pp\n", \
            group, covered[group], total[group], percentage, baseline[group], percentage - baseline[group]
        # Compare unrounded ratios. An inclusive 0.5 pp allowance is intentional.
        if (group != "generated" && percentage + tolerance[group] + 0.000000001 < baseline[group]) {
            reject(group " regressed beyond " tolerance[group] " percentage points")
        }
    }
    if (invalid) exit 1
}
