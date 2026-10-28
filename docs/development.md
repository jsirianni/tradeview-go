# Development

Use Go 1.26 or newer, Git, and GNU Make 4.2 or newer on your PATH. The Makefile works with native Windows command shells and Linux. On Windows, GNU Make is available through Chocolatey (`choco install make --version=4.4.1`).

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

The generator is pinned to `oapi-codegen` **v2.8.1** as a Go tool dependency. Generated files are committed so module consumers do not need to run it.

`make generate-check` runs generation and checks only the tracked generated files, `api.gen.go` and `api/operations.json`. Both staged and unstaged differences cause it to fail. Other working-tree changes do not affect that target. `make ci` completes generation checking before starting readers of the generated code, including when Make runs with parallel jobs.

Generation uses the committed schema and does not fetch a new API revision. To update the contract, replace the upstream schema deliberately, record its version/source/checksum, review the compatibility overlay, regenerate, and update the operation inventory and transport fixtures together. Review all resulting API changes before committing.

CI runs the Make targets on Windows and Linux with Go 1.26 and 1.27. Linux additionally runs `make ci` and verifies that the checkout stays clean. Tests use local HTTP fixtures and do not require broker credentials.
