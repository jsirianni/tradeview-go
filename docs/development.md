# Development

## Generation and validation

The upstream YAML, source metadata, compatibility overlay, generator configuration, and operation inventory are committed under `api/`. See [schema maintenance](../api/README.md) for the exact files and overlay rules. The original upstream file remains unchanged; generation applies documented local corrections separately.

The generator is pinned to `oapi-codegen` **v2.8.1** as a Go tool dependency. Generated files are committed so module consumers do not need to run it.

```sh
go generate ./...
go vet ./...
go test ./...
go test -race ./...
git diff --exit-code
```

Generation uses the committed schema. It does not fetch a new API revision. To update the contract, replace the upstream schema deliberately, record its version/source/checksum, review the compatibility overlay, regenerate, and update the operation inventory and transport fixtures together. Review all resulting API changes before committing.

CI checks formatting, vet, and tests on Windows and Linux with Go 1.26 and 1.27. Linux additionally checks reproducible generation and race detection. Tests use local HTTP fixtures and do not require broker credentials.
