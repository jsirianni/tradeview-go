# Development

Development requires Linux, Go 1.27.3 or a newer patch in the Go 1.27 series, Git, and GNU Make 4.2 or newer on your PATH.

## Packages

- `pkg/tradeview/` contains the public client, generated bindings, and client tests.
- `internal/generate/` contains schema generation support and its tests.
- `api/` contains the pinned schema, generation rules, and operation inventory.

The repository root holds module metadata, build tasks, and documentation. All Go source belongs to packages; there is no application entry point.

## Common tasks

Run commands from the repository root:

```sh
make help
make check
make ci
make test TEST_ARGS="-run TestOperationTransport -count=1"
```

| Target | Task |
| --- | --- |
| `help` | List available targets; also the default for `make`. |
| `build` | Compile all packages. |
| `test` | Run all tests; accepts `TEST_ARGS`. |
| `test-race` | Run tests with the race detector. |
| `fuzz` | Run four parser/stream fuzz targets with bounded exploration. |
| `coverage` | Write `coverage.out` and print function coverage. |
| `coverage-html` | Write `coverage.html` without opening a browser. |
| `coverage-check` | Run the full uncached test suite with temporary coverage and check grouped regression baselines. |
| `coverage-smoke` | Verify weighted classification, regression failures, and temporary profile cleanup. |
| `fmt` / `fmt-check` | Format Go sources or check formatting. |
| `vet` | Run Go's static checks. |
| `staticcheck` | Run pinned Staticcheck analysis on all packages and normal tests. |
| `lint` | Run Revive v1.17.1 with the committed three-rule configuration. |
| `lint-smoke` | Prove each configured Revive rule rejects a violation. |
| `errcheck` | Run errcheck v1.30.0 on handwritten runtime, generator, and test code, including explicit blank error assignments. |
| `errcheck-smoke` | Prove unchecked statements, blank assignments, and test-file errors fail beside accepted buffer writes. |
| `imports-check` | Run goimports v0.52.0 without modifying Go source, including generated files. |
| `imports-smoke` | Prove missing imports fail for handwritten and generated files without changing the input. |
| `security` | Run gosec v2.30.0 on handwritten production code. |
| `security-smoke` | Prove an unsuppressed finding fails beside a narrow accepted case. |
| `govulncheck` | Record tool versions and scan all packages for reachable known vulnerabilities. |
| `generate` | Regenerate bindings and the operation inventory. |
| `generate-check` | Regenerate and reject differences in tracked generated outputs. |
| `deps` / `tidy` | Download dependencies or tidy module metadata. |
| `check` | Check formatting/imports, vet, Staticcheck, Revive, errcheck, gosec, and the full test suite with coverage regression checks. |
| `ci` | Check generation and Go fixes, then run checks, build, race tests, vulnerability scans, gate regression fixtures, and bounded fuzzing. |
| `clean` | Clean package build artifacts and remove coverage reports. |

Override `GO` and `GOFMT` to select tool executables. `TEST_ARGS` passes additional arguments to Go tests. For example, `make test TEST_ARGS="-count=1"` runs tests without cached results.

## Generation and validation

The upstream YAML, source metadata, compatibility overlay, generator configuration, and operation inventory are committed under `api/`. See [schema maintenance](../api/README.md) for the exact files and overlay rules. The original upstream file remains unchanged; generation applies documented local corrections separately.

The generator uses `oapi-codegen` **v2.8.1**, pinned as a Go tool dependency. `make generate` explicitly invokes the regeneration hook in the `internal/generate` package. Ordinary test runs leave generated files unchanged. Generated files are committed so module consumers do not need to regenerate them.

Generation runs the pinned tool and every rewrite in a private staging directory.
Only after the resulting Go source and operation inventory are ready does it
prepare replacements and backups beside both output files. A publication error
rolls back earlier replacements. Temporary files are removed on success and
ordinary failure; if rollback itself fails, the error identifies the affected
output and the retained original backup for manual recovery. Run generation
without concurrent writers to the generated files. The two-file publication is
not a crash-atomic transaction; an interrupted process may require regeneration.
Failure tests use copied temporary modules and never rewrite committed artifacts.

`make generate-check` runs generation and checks only the tracked generated files, `pkg/tradeview/api.gen.go` and `api/operations.json`. Both staged and unstaged differences cause it to fail. Other working-tree changes do not affect that target. `make ci` completes generation checking before starting readers of the generated code, including when Make runs with parallel jobs.

Generation uses the committed schema and does not fetch a new API revision. To update the contract, replace the upstream schema deliberately, record its version/source/checksum, review the compatibility overlay, regenerate, and update the operation inventory and transport fixtures together. Review all resulting API changes before committing.

CI runs one Linux job with Go 1.27, selecting the latest available patch at or above 1.27.3. It runs `make ci`, records market-stream decoding benchmarks, and verifies that the checkout stays clean. Tests use local HTTP fixtures and do not require broker credentials.

## Parser and stream fuzzing

`make fuzz` is part of required PR/main CI and the existing weekly CI schedule.
It runs `FuzzSymbolInfoJSON`, `FuzzSymbolInfoBudget`, `FuzzMarketHeader`, and
`FuzzStreamFrames` sequentially with **1,000 executions per target**, **two
workers**, a **two-second minimization budget**, and a **two-minute process
timeout per target**. Exploration is randomized; the committed seeds and any
saved counterexamples reproduce deterministically in ordinary `go test` and
race-test runs. All targets use in-memory inputs and require no broker, network,
credentials, sleeps, or wall-clock assumptions.

The symbol tests compare decoding with an independent scalar/array oracle and
use big-integer arithmetic to check expansion limits without overflow. Header
decoding is compared with exact-key JSON map lookups. The stream test compares
variable reader chunks (including data returned together with EOF) against a
split-line framing oracle, checking limits, LF/CRLF, terminal classification,
retained values and once-only closure for both account and market events.
Seeds include duplicate, escaped, Unicode and case-folded keys and the protocol
regressions from the consultant audit.

Raw symbol inputs are capped at 2 KiB, header/stream inputs at 4 KiB and chunk
patterns at 64 bytes. The budget target constructs at most 65,537 symbols and
four 1 KiB scalars; it executes large cases only when preflight must reject them,
and caps accepted expansion estimates at 256 KiB. Each iteration terminates
without an unbounded reader or allocation driven by unrestricted input.

For a longer local run or an individual target:

```sh
make fuzz FUZZ_TIME=30s FUZZ_PARALLEL=2
go test ./pkg/tradeview -run '^$' -fuzz '^FuzzStreamFrames$' -fuzztime 30s -parallel 2 -timeout 2m
```

On failure, Go saves the minimized input under
`pkg/tradeview/testdata/fuzz/<target>/<hash>` and prints a reproduction command.
The runner prints the complete native corpus files into retained CI job logs;
it does not upload an artifact or remove the files. Copy a CI counterexample
from those logs to the same local path, reproduce it with
`go test ./pkg/tradeview -run 'FuzzStreamFrames/<hash>'`, fix the defect, and
commit the corpus file as a regression seed. Successful runs change only Go's
external fuzz cache and leave the checkout clean. Fuzzing complements the
explicit contract tests; a bounded successful run is not proof of correctness.

The [finite response fixtures](../pkg/tradeview/testdata/responses/README.md) cover
all 38 finite endpoints with realistic JSON, public union accessors, and request
expectations. Their inventory test requires every declared success alternative
to have a fixture. Add explicit decoded-value assertions when extending this
suite; a non-nil response wrapper alone does not verify a union's contents.

## Coverage regression checks

`make check` and required `make ci` run `make coverage-check` in place of the
plain test pass. It runs all packages uncached with default build tags and
`-covermode=atomic`; `TEST_ARGS` deliberately does not apply to this gate. Use
`make test TEST_ARGS="..."` for selected tests. The race pass remains separate.

The profile lives in a private system temporary directory and is deleted on
success, failure, or a handled interruption. Only aggregate counts and pass/fail
diagnostics appear in command output. Coverage profiles, reports, artifacts,
PR reports, badges, and external-service results are never uploaded or
published. No workflow summary or coverage artifact step is used. The optional
`make coverage` and `make coverage-html` commands retain ignored local files for
developer inspection; they do not upload them.

`scripts/coverage-summary.awk` sums statement counts from the profile rather
than averaging line, function, or file percentages. Classification is explicit:

| Group | Selection | Baseline covered/total | Policy |
| --- | --- | --- | --- |
| Handwritten runtime | Direct `.go` files in `pkg/tradeview`, excluding `api.gen.go` | 817/872 (93.69%) | Gate regressions |
| Generator | Direct `.go` files in `internal/generate` | 511/579 (88.26%) | Gate regressions |
| Generated bindings | Exactly `pkg/tradeview/api.gen.go` | 2753/5913 (46.56%) | Informational |

The baseline in `scripts/coverage-baseline.tsv` uses Linux/amd64, Go 1.27.3, and
the uncached full suite. CI selects a supported `1.27.x` patch and prints the
actual version and platform. Other local platforms can have different covered
branches; the Linux result is authoritative. New files in the named packages
automatically count. Missing groups, malformed/duplicate blocks, and unexpected
packages fail instead of silently shrinking the denominator; update the
classification deliberately when adding a package.

A decrease exceeding **0.5 percentage points** from either handwritten
baseline fails, using unrounded ratios. This allows small instrumentation or
platform changes while catching meaningful regression; it is a fixed allowance
from the committed baseline, not a fresh allowance per PR. Generated coverage
cannot mask a runtime or generator regression and has no minimum target.
Review baseline changes in the same PR as their cause, using Linux measurements
from the supported toolchain. Improve behavioral tests first; explain justified
decreases or denominator changes and ratchet the baseline upward when sustained
improvements are established. Never lower it automatically to make CI pass.
Go/toolchain or schema updates require the same deliberate remeasurement.

Coverage is evidence of execution, not proof of correct assertions. The
contract, stream, generator staging/publication, and metric-instrument failure
tests retain their behavioral assertions. `make coverage-smoke` proves weighted
classification, independent handwritten gates, rounding boundaries, malformed
input rejection, and profile cleanup without modifying source files. The final
CI clean-checkout check remains in place.

## Known vulnerability scanning

`make govulncheck` runs **govulncheck v1.9.0** through a versioned `go run`, independently of client dependencies. It prints the actual Go and scanner versions and scans `./...`, including runtime and generator packages, using the live Go vulnerability database. The supported development baseline is Go **1.27.3**, which fixes standard-library advisories reported with Go 1.27.0. The module directive remains a language/toolchain minimum; consumers should use a supported patched toolchain.

The scan is part of `make ci` on PRs and main. CI also runs every Monday at 09:23 UTC and can be dispatched manually, so new advisories are checked without source changes. CI explicitly selects the live database at `https://vuln.go.dev` and retains scanner output in job logs. Reachable vulnerability findings, scanner failures, database failures, and package-loading failures fail the job. Imported-but-unreachable advisories are informational and are not evidence of an exploitable application path.

Source analysis does not execute the application or discover new application-specific vulnerabilities. `./...` covers this module's packages, not every dependency of the separately invoked `oapi-codegen` binary or the analysis tool itself. Review those tools separately when updating their pinned versions. No advisories are suppressed; any future exception must name the advisory, rationale, owner, and review date or expiry condition. Keep the Go patch baseline and scanner compatible and current when upgrading.

## Security analysis

`make security` runs `github.com/securego/gosec/v2/cmd/gosec@v2.30.0` with default rules and severities. It scans handwritten production code; generated files are covered by reproducible generation checks and test fixtures remain outside the production scan. Each accepted `#nosec` location names its rule and explains the trusted path. `make security-smoke` confirms that an unsuppressed file access fails alongside a narrowly accepted location.
## Revive

`make lint` runs pinned Revive with the configured unchecked assertion, builtin shadowing, and unnecessary formatting rules. The smoke target requires the expected diagnostic for each intentional violation and checks corrected code.

## Staticcheck

`make staticcheck` runs Staticcheck **v0.8.2** through a versioned `go run`, independently of client dependencies. It analyzes `./...`, including the runtime, generator, and normal tests, with recommended default checks and no exclusions or custom configuration. Diagnostics and analyzer failures fail `make check` and the required PR/main CI job. `STATICCHECK_PACKAGES` defaults to `./...`; CI overrides it only for a temporary negative fixture that must report SA1000 for an invalid regular expression. The fixture is removed afterward.

When upgrading Go, update the pinned analyzer to a compatible release and verify both the clean baseline and the negative fixture. Keep suppressions narrow and document their justification.

## Error checking and imports

`make errcheck` runs **errcheck v1.30.0** through a versioned `go run`, with
`-blank -excludeonly -ignoregenerated`. It checks all selected handwritten
runtime, generator, and test code in `./...` using the current platform and
default build tags. CI uses the supported Linux Go 1.27 toolchain. Explicit `_`
assignments are checked; there are no package, test, file-wide cleanup, or
default-function exclusions. Type assertion checking remains with the existing
Revive rule, avoiding a duplicate analyzer for that concern.

`scripts/errcheck-excludes.txt` is the complete exclusion policy. Each entry
names a documented infallible `bytes.Buffer`/`strings.Builder` write or limits
`fmt.Fprintf` to one of those concrete destinations. File, network, pipe, and
arbitrary writer errors remain checked. Tests report setup/write/cleanup
errors, with an explicit `os.ErrClosed` allowance only where a file-owning
editor test deliberately closes the file before its safety cleanup. Stream
handshake cleanup preserves both the primary failure and any body-close error;
terminal stream closure retains its error for `Close`.

Errcheck skips standard `// Code generated ... DO NOT EDIT.` source such as
`api.gen.go`; generated error-handling behavior remains covered by the
generator's handwritten implementation, public contract tests, and reproducible
generation gate. Change the generator rather than hand-editing a generated
binding. Never add an exclusion merely to silence a diagnostic; handle the
error or justify the narrowest truly infallible operation.

`make imports-check` runs **goimports v0.52.0** through a versioned `go run`.
It checks all tracked and new non-ignored `.go` files, including tests and
generated files, and prints import/format diffs without writing. Unlike
gofmt, it detects missing or unused imports and import grouping. Correct a
handwritten file with
`go run golang.org/x/tools/cmd/goimports@v0.52.0 -w path/to/file.go`.
If generated output differs, correct its generation path and regenerate.

Both checks run once in `make check` and required `make ci`, alongside the
existing analyzers. Their smoke targets run once in `make ci`: real errcheck
must reject unchecked statements, explicit blank errors, and test-file
violations beside accepted buffer writes, while honoring the generated-file
policy. Real goimports must reject a gofmt-clean missing import in both
handwritten and generated fixtures, leave the original bytes unchanged during
checking, and accept the corrected source. Smoke fixtures use private temporary
directories and are removed on success/failure. Tool/download/load failures
cannot count as a successful negative test. Pins do not add analyzer
dependencies to the client module; update them deliberately with the toolchain.

## Required Go modernization

Contributors and coding agents must run `make fix` before submitting every Go code change, review the diff, and commit the result. Development and CI use the supported Go 1.27 toolchain; CI selects the current `1.27.x` patch. Both fix targets print `go version` so version-dependent rewrites can be diagnosed.

`make fix-check` runs `go fix ./...`, then prints and rejects any tracked-file diff, including staged changes. Start from a clean checkout for this gate; during development, use `make fix` and review its changes alongside your own before committing. CI never commits or pushes these rewrites, and retains its final clean-checkout check.

The gate covers the generator, public package, and tests selected by `./...` on Linux with default build tags. `make fix-gate-test` uses the real Makefile in a temporary Git fixture: an old `strings.Split` idiom must fail with a visible diff, and `make fix` followed by committing the rewrite must pass.
## Market stream benchmarks

Compare the compact market-header decoder with the original map dispatcher on Linux with Go 1.27:

```sh
go test -run '^$' -bench '^BenchmarkDecodeMarketEvent$' -benchmem -benchtime=300ms -count=3 ./pkg/tradeview
```

Both benchmark paths preserve the raw frame, decode the typed message, and perform the decoded-event terminal check used by `Stream.Next`. Quote, trade, daily-bar, and heartbeat results report time, allocated bytes, and allocation count per frame. CI records all three runs; compare allocated bytes and allocation counts between `compact_header` and `map_baseline`. Timing is diagnostic and has no fixed pass threshold. These decoder microbenchmarks do not measure broker or network throughput.
