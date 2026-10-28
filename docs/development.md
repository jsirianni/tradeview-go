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
| `coverage` | Write `coverage.out` and print function coverage. |
| `coverage-html` | Write `coverage.html` without opening a browser. |
| `fmt` / `fmt-check` | Format Go sources or check formatting. |
| `vet` | Run Go's static checks. |
| `staticcheck` | Run pinned Staticcheck analysis on all packages and normal tests. |
| `lint` | Run Revive v1.17.1 with the committed three-rule configuration. |
| `lint-smoke` | Prove each configured Revive rule rejects a violation. |
| `govulncheck` | Record tool versions and scan all packages for reachable known vulnerabilities. |
| `generate` | Regenerate bindings and the operation inventory. |
| `generate-check` | Regenerate and reject differences in tracked generated outputs. |
| `deps` / `tidy` | Download dependencies or tidy module metadata. |
| `check` | Check formatting, vet, Staticcheck, Revive, and tests. |
| `ci` | Check generation, then run checks, build, race tests, and govulncheck. |
| `clean` | Clean package build artifacts and remove coverage reports. |

Override `GO` and `GOFMT` to select tool executables. `TEST_ARGS` passes additional arguments to Go tests. For example, `make test TEST_ARGS="-count=1"` runs tests without cached results.

## Generation and validation

The upstream YAML, source metadata, compatibility overlay, generator configuration, and operation inventory are committed under `api/`. See [schema maintenance](../api/README.md) for the exact files and overlay rules. The original upstream file remains unchanged; generation applies documented local corrections separately.

The generator uses `oapi-codegen` **v2.8.1**, pinned as a Go tool dependency. `make generate` explicitly invokes the regeneration hook in the `internal/generate` package. Ordinary test runs leave generated files unchanged. Generated files are committed so module consumers do not need to regenerate them.

`make generate-check` runs generation and checks only the tracked generated files, `pkg/tradeview/api.gen.go` and `api/operations.json`. Both staged and unstaged differences cause it to fail. Other working-tree changes do not affect that target. `make ci` completes generation checking before starting readers of the generated code, including when Make runs with parallel jobs.

Generation uses the committed schema and does not fetch a new API revision. To update the contract, replace the upstream schema deliberately, record its version/source/checksum, review the compatibility overlay, regenerate, and update the operation inventory and transport fixtures together. Review all resulting API changes before committing.

CI runs one Linux job with Go 1.27, selecting the latest available patch at or above 1.27.3. It runs `make ci`, records market-stream decoding benchmarks, and verifies that the checkout stays clean. Tests use local HTTP fixtures and do not require broker credentials.

## Known vulnerability scanning

`make govulncheck` runs **govulncheck v1.9.0** through a versioned `go run`, independently of client dependencies. It prints the actual Go and scanner versions and scans `./...`, including runtime and generator packages, using the live Go vulnerability database. The supported development baseline is Go **1.27.3**, which fixes standard-library advisories reported with Go 1.27.0. The module directive remains a language/toolchain minimum; consumers should use a supported patched toolchain.

The scan is part of `make ci` on PRs and main. CI also runs every Monday at 09:23 UTC and can be dispatched manually, so new advisories are checked without source changes. CI explicitly selects the live database at `https://vuln.go.dev` and retains scanner output in job logs. Reachable vulnerability findings, scanner failures, database failures, and package-loading failures fail the job. Imported-but-unreachable advisories are informational and are not evidence of an exploitable application path.

Source analysis does not execute the application or discover new application-specific vulnerabilities. `./...` covers this module's packages, not every dependency of the separately invoked `oapi-codegen` binary or the analysis tool itself. Review those tools separately when updating their pinned versions. No advisories are suppressed; any future exception must name the advisory, rationale, owner, and review date or expiry condition. Keep the Go patch baseline and scanner compatible and current when upgrading.

## Revive

`make lint` runs pinned Revive with the configured unchecked assertion, builtin shadowing, and unnecessary formatting rules. The smoke target requires the expected diagnostic for each intentional violation and checks corrected code.

## Staticcheck

`make staticcheck` runs Staticcheck **v0.8.2** through a versioned `go run`, independently of client dependencies. It analyzes `./...`, including the runtime, generator, and normal tests, with recommended default checks and no exclusions or custom configuration. Diagnostics and analyzer failures fail `make check` and the required PR/main CI job. `STATICCHECK_PACKAGES` defaults to `./...`; CI overrides it only for a temporary negative fixture that must report SA1000 for an invalid regular expression. The fixture is removed afterward.

When upgrading Go, update the pinned analyzer to a compatible release and verify both the clean baseline and the negative fixture. Keep suppressions narrow and document their justification.

## Market stream benchmarks

Compare the compact market-header decoder with the original map dispatcher on Linux with Go 1.27:

```sh
go test -run '^$' -bench '^BenchmarkDecodeMarketEvent$' -benchmem -benchtime=300ms -count=3 ./pkg/tradeview
```

Both benchmark paths preserve the raw frame, decode the typed message, and perform the error-envelope check used by `Stream.Next`. Quote, trade, daily-bar, and heartbeat results report time, allocated bytes, and allocation count per frame. CI records all three runs; compare allocated bytes and allocation counts between `compact_header` and `map_baseline`. Timing is diagnostic and has no fixed pass threshold. These decoder microbenchmarks do not measure broker or network throughput.
