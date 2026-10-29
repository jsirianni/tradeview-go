# Finite API response fixtures

These are synthetic, independently authored wire payloads for the pinned
TradingView Broker API **1.21.0** in `api/broker-rest-api.yaml`. They contain no
real credentials or customer data. Values follow the declared schemas and the
documented local overlay; documentation examples are adjusted where their JSON
types disagree with the declared field type. JSON files are not marshaled from
the generated Go types, so a changed JSON tag cannot silently change both the
client and its test input.

History and symbol-info responses follow the documented combined `allOf`
payloads. The upstream `SuccessResponse` branch declares
`additionalProperties: false`, which conflicts with fields contributed by the
other branch under strict schema validation; the fixtures retain those required
market-data fields, as the generated client does.

`TestFiniteResponseFixtures` exercises all 38 finite endpoints through a local
`httptest.Server` and concrete public client calls. There are 51 success cases:
46 operation/schema pairs, plus five additional cases for pagination endings,
symbol metadata encoding, and disclaimer representations. Shared success and
transaction fixtures are reused, with each endpoint retaining its own client
call and request checks. Every case checks status, raw response bytes, and
concrete decoded values, including public `As...` union accessors.

`TestFiniteResponseFixtureInventory` checks the operation table against the
committed manifest and the schema's HTTP 200 success alternatives. Adding an
endpoint or an alternative without adding a case fails the test. Nested unions
are asserted explicitly: instrument validation rules, order-info row types,
preview row types, text/markup disclaimers, warnings, errors, and hyperlinks.

Important fixture pairs:

- `executions` / `executions-empty` and `orders-history` / `orders-history-empty`:
  populated collections with cursors versus empty final pages without cursors.
- `success` / `transaction`: both success alternatives for all seven applicable
  modification/cancellation endpoints.
- `history` / `history-empty`: populated bars and the separate empty-bars schema.
- `symbol-info-arrays` / `symbol-info-scalars`: the same two symbols, with compact
  scalar metadata expanded according to the documented symbol-info overlay.
- `order-info-text` / `order-info-markup` and `preview-close-text` /
  `preview-close-markup`: both disclaimer representations.
- `broker-error`: intentional HTTP 200 API error; `wrong-account-field`:
  intentionally invalid account ID type rejected by the finite parser before it
  returns a successful response union.

To add coverage, author a JSON payload, register its operation and original
upstream response schema name in `finiteResponseOperations`, and add explicit
typed assertions. Use distinct nonzero values to detect swapped or lost fields,
and include omitted fields and explicit zero/false where presence matters. Keep
the original upstream name in the inventory even if the overlay renames the Go
type, such as `OrderInfoResponse` becoming `OrderInfoPayload`.

Run from the repository root:

```sh
go test ./pkg/tradeview -run '^TestFiniteResponseFixture' -count=1
go test ./... -count=1
```

The tests are part of normal and race-enabled CI runs. They require no external
server, broker credentials, sleeps, or fixture regeneration. Existing transport,
protocol, stream, and body-limit tests remain responsible for their broader
negative-case coverage.
