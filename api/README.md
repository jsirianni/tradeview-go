# Pinned API contract

`broker-rest-api.yaml` is the unmodified TradingView broker OpenAPI 3.0.0 contract, API version 1.21.0. `upstream.json` records its official source URL and SHA-256 checksum. Generation verifies both the checksum and version before reading it.

`overlay.yaml` contains local, machine-applied generation rules:

- Use double precision for number schemas whose upstream format is unspecified.
- Encode the instruments `symbols` query as the comma-separated list specified in its description.
- Move the misplaced `items` property on the shared `orderIds` parameter into its array schema.
- Accept compact scalar symbol metadata in both `SymbolInfoArrays` and `SymbolInfoResponse`, expanding it across the returned symbols while keeping the public slice fields. The generator derives every metadata array field from the generated models; the pinned upstream schema remains unchanged.
- Give a small set of Go types distinct names where upstream component names collide with flattened enums or generated HTTP response wrappers. Wire names remain unchanged.

`oapi-codegen.yaml` preserves all component models, including those not referenced by operations. `operations.json` maps all 47 upstream operation IDs to the generated raw method, finite response method or streaming opener, and transport fixture test. Form operations use the generator's `WithFormdataBody` method suffix.

Run `go generate ./...` from the repository root. The `internal/generate` package runs regeneration through an explicit opt-in test; ordinary tests never regenerate files. Its pinned generator reads only the local schema, applies these rules in a temporary file, generates `pkg/tradeview/api.gen.go`, then deterministically removes finite response readers for streaming operations and adds the common form/error helpers. Finite parsers validate the declared status and JSON media type, preserve broker error classification, and wrap decoding failures with response diagnostics. Generation rejects unexpected finite parser shapes so a future contract change requires an explicit update to these rules. Named request body wrappers become type aliases so JSON union marshal/unmarshal methods remain available to form encoders. Path parameters also protect literal dot identifiers from URL relative-path resolution. The generated default HTTP client uses the package redirect policy to block HTTPS-to-HTTP downgrades and retain the standard redirect limit. Schema updates are explicit: replace the upstream file from its official source, update `upstream.json`, review the overlay and inventory, regenerate, and run the full suite. No source is downloaded by generation.
