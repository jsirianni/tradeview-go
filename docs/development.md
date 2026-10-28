# Development

Development requires Linux, Go 1.27, Git, and GNU Make 4.2 or newer on your PATH.

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
| `generate` | Regenerate bindings and the operation inventory. |
| `generate-check` | Regenerate and reject differences in tracked generated outputs. |
| `deps` / `tidy` | Download dependencies or tidy module metadata. |
| `check` | Check formatting, vet, and tests. |
| `ci` | Check generation, then run checks, build, and race tests. |
| `clean` | Clean package build artifacts and remove coverage reports. |

Override `GO` and `GOFMT` to select tool executables. `TEST_ARGS` passes additional arguments to Go tests. For example, `make test TEST_ARGS="-count=1"` runs tests without cached results.

## Generation and validation

The upstream YAML, source metadata, compatibility overlay, generator configuration, and operation inventory are committed under `api/`. See [schema maintenance](../api/README.md) for the exact files and overlay rules. The original upstream file remains unchanged; generation applies documented local corrections separately.

The generator uses `oapi-codegen` **v2.8.1**, pinned as a Go tool dependency. `make generate` explicitly invokes the regeneration hook in the `internal/generate` package. Ordinary test runs leave generated files unchanged. Generated files are committed so module consumers do not need to regenerate them.

`make generate-check` runs generation and checks only the tracked generated files, `pkg/tradeview/api.gen.go` and `api/operations.json`. Both staged and unstaged differences cause it to fail. Other working-tree changes do not affect that target. `make ci` completes generation checking before starting readers of the generated code, including when Make runs with parallel jobs.

Generation uses the committed schema and does not fetch a new API revision. To update the contract, replace the upstream schema deliberately, record its version/source/checksum, review the compatibility overlay, regenerate, and update the operation inventory and transport fixtures together. Review all resulting API changes before committing.

CI runs one Linux job with Go 1.27. It runs `make ci`, records market-stream decoding benchmarks, and verifies that the checkout stays clean. Tests use local HTTP fixtures and do not require broker credentials.

## Market stream benchmarks

Compare the compact market-header decoder with the original map dispatcher on Linux with Go 1.27:

```sh
go test -run '^$' -bench '^BenchmarkDecodeMarketEvent$' -benchmem -benchtime=300ms -count=3 ./pkg/tradeview
```

Both benchmark paths preserve the raw frame, decode the typed message, and perform the error-envelope check used by `Stream.Next`. Quote, trade, daily-bar, and heartbeat results report time, allocated bytes, and allocation count per frame. CI records all three runs; compare allocated bytes and allocation counts between `compact_header` and `map_baseline`. Timing is diagnostic and has no fixed pass threshold. These decoder microbenchmarks do not measure broker or network throughput.
