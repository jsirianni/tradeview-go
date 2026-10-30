# Development

Development requires Linux, the Go version declared in `go.mod`, Git, and GNU Make 4.2 or newer on your PATH.

## Packages

- `pkg/tradeview/` contains the public client, generated bindings, and client tests.
- `pkg/alerts/` contains the provider-neutral alert value and sender interface.
- `pkg/alerts/slack/` contains the Slack bot sender, its tests, and a composition example.
- `internal/generate/` contains schema generation support and its tests.
- `internal/updatego/` contains the scheduled Go version updater command and its tests.
- `api/` contains the pinned schema, generation rules, and operation inventory.

The repository root holds module metadata, build tasks, and documentation. All Go source belongs to packages; the updater is a maintenance command, while the public client remains a library.

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

CI runs one Linux job with the Go version declared in `go.mod`. It runs `make ci`, records market-stream decoding benchmarks, and verifies that the checkout stays clean. Tests use local HTTP fixtures and do not require broker or Slack credentials.

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

Public union contract tests exercise all 146 `From...`, `As...`, and `Merge...`
methods across 61 generated union types. The examples use independently authored
wire fixtures, including nested and streaming models. Tests check replacement,
round trips, preservation of unrelated fields, nested merges, explicit zero
values, integers beyond float64 precision, and invalid JSON/non-finite numbers.
An inventory check inspects generated method declarations and rejects unions
without registered tests. It does not derive expected behavior from method bodies.

Telemetry contract tests use fresh subprocesses for OpenTelemetry's process-wide
globals. They verify unconfigured defaults, global providers configured before
option application, individual explicit overrides, remote parent propagation,
and unsampled parents. Separate finite/stream tests assert instrument scope,
units, aggregation types, cumulative temporality, histogram boundaries, exact
attribute sets, byte/frame counts, and active-operation balance. No exporter,
collector, broker credentials, or external network is required.

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
| Handwritten runtime | Direct `.go` files in `pkg/tradeview`, `pkg/alerts`, and `pkg/alerts/slack`, excluding `pkg/tradeview/api.gen.go` | 821/875 (93.83%) | Gate regressions |
| Generator | Direct `.go` files in `internal/generate` | 511/579 (88.26%) | Gate regressions |
| Automation | Direct `.go` files in `internal/updatego` | 38/43 (88.37%) | Gate regressions |
| Generated bindings | Exactly `pkg/tradeview/api.gen.go` | 4303/5913 (72.77%) | Gate regressions |

The baseline in `scripts/coverage-baseline.tsv` uses Linux/amd64, Go 1.27.3, and
the uncached full suite. CI selects a supported `1.27.x` patch and prints the
actual version and platform. Other local platforms can have different covered
branches; the Linux result is authoritative. New files in the named packages
automatically count. Missing groups, malformed/duplicate blocks, and unexpected
packages fail instead of silently shrinking the denominator; update the
classification deliberately when adding a package.

A decrease exceeding **0.5 percentage points** from any group's
baseline fails, using unrounded ratios. This allows small instrumentation or
platform changes while catching meaningful regression; it is a fixed allowance
from the committed baseline, not a fresh allowance per PR. Generated coverage
has its own gate and cannot mask a runtime, generator, or automation regression.
Review baseline changes in the same PR as their cause, using Linux measurements
from the supported toolchain. Improve behavioral tests first; explain justified
decreases or denominator changes and ratchet the baseline upward when sustained
improvements are established. Never lower it automatically to make CI pass.
Go/toolchain or schema updates require the same deliberate remeasurement.

Coverage is evidence of execution, not proof of correct assertions. The
contract, stream, generator staging/publication, and metric-instrument failure
tests retain their behavioral assertions. `make coverage-smoke` proves weighted
classification across the broker and alert packages, independent group gates, rounding boundaries, malformed
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

Contributors and coding agents must run `make fix` before submitting every Go code change, review the diff, and commit the result. Development and CI use the Go version declared in `go.mod`; CI reads it with `go-version-file`. Both fix targets print `go version` so version-dependent rewrites can be diagnosed.

`make fix-check` runs `go fix ./...`, then prints and rejects any tracked-file diff, including staged changes. Start from a clean checkout for this gate; during development, use `make fix` and review its changes alongside your own before committing. CI never commits or pushes these rewrites, and retains its final clean-checkout check.

The gate covers the generator, public package, and tests selected by `./...` on Linux with default build tags. `make fix-gate-test` uses the real Makefile in a temporary Git fixture: an old `strings.Split` idiom must fail with a visible diff, and `make fix` followed by committing the rewrite must pass.
## Automated dependency updates

`.github/dependabot.yml` checks Go modules and GitHub Actions every Monday at
09:00 America/New_York. OpenTelemetry modules (`go.opentelemetry.io/*`) share a
dedicated group, including indirect modules; other Go modules and Actions each
have their own group. Separate security-update groups use the same boundaries.

The generator currently requires a compatible YAML dependency set:
`speakeasy-api/openapi v1.25.6`, `speakeasy-api/jsonpath v0.6.3`, and
`dprotaso/go-yit v0.0.0-20220510233725-9ba8df137936`. Newer versions migrate
YAML node types from `gopkg.in/yaml.v3` to `go.yaml.in/yaml/v3`, which breaks
compilation of `oapi-codegen v2.8.1` or its `vmware-labs/yaml-jsonpath` dependency.
Dependabot temporarily ignores versions above these ceilings, including the
OpenAPI pseudo-version preceding v1.26.0. When a tagged generator release
includes [upstream fix #2608](https://github.com/oapi-codegen/oapi-codegen/pull/2608),
upgrade the generator and this dependency set together, remove all three ignore
rules, regenerate, and run CI. The existing generation check catches this build
failure before any dependency PR can merge.

Routine version-update groups include only patch and minor releases; major
upgrades open separately for review. The **Dependabot auto-merge** workflow
verifies Dependabot-authored commits and enables squash auto-merge when the
highest update type in the PR is patch or minor. This also covers eligible
security PRs; any group containing a major upgrade remains for manual review.
The workflow runs on `pull_request_target` with narrowly scoped write permissions
and never checks out or executes PR code. It does not approve PRs or bypass checks.

Repository Settings > General > Pull Requests must enable **Allow auto-merge**.
The required `Go 1.27 / Linux` check and the up-to-date branch requirement remain
in force. Failed CI blocks merging. Existing PRs need auto-merge enabled once,
or a new PR event after this workflow lands. Human-authored PRs are unaffected.

Dependabot does not independently track Go releases for the `go` directive
([upstream request](https://github.com/dependabot/dependabot-core/issues/13520)).
The **Update Go** workflow checks the official Go release feed daily at 13:41 UTC
and supports manual dispatch. It proposes the latest stable version, including
new minor releases, by changing only the Go directive in `go.mod`. It ignores
prereleases, never downgrades, and makes no commit when already current.

The workflow creates or refreshes one PR on `automation/update-go` using the
built-in `GITHUB_TOKEN`. Repository Settings > Actions > General > Workflow
permissions must allow GitHub Actions to create pull requests. No additional
secret is required. Changed PR branches explicitly dispatch `ci.yml`, which
reads `go.mod` via `go-version-file`. Other PR workflows may require approval
in GitHub when triggered by `GITHUB_TOKEN`. The existing required status name
`Go 1.27 / Linux` stays stable for branch protection even after a version bump.

Go toolchain version PRs are not automatically merged. Review CI, compatibility, and any
modernization or analyzer changes required by a new Go release before merging.
The updater uses only the Go standard library. Its offline tests run with the
normal Go test suite and have a separate automation coverage regression baseline.
Run just these tests with:

```sh
go test ./internal/updatego
```

## Market stream benchmarks

Compare the compact market-header decoder with the original map dispatcher on Linux with Go 1.27:

```sh
go test -run '^$' -bench '^BenchmarkDecodeMarketEvent$' -benchmem -benchtime=300ms -count=3 ./pkg/tradeview
```

Both benchmark paths preserve the raw frame, decode the typed message, and perform the decoded-event terminal check used by `Stream.Next`. Quote, trade, daily-bar, and heartbeat results report time, allocated bytes, and allocation count per frame. CI records all three runs; compare allocated bytes and allocation counts between `compact_header` and `map_baseline`. Timing is diagnostic and has no fixed pass threshold. These decoder microbenchmarks do not measure broker or network throughput.
