# Using tradeview-go

The client sends requests to a broker base URL provided by your application.

## Installation

Linux and Go **1.27** are required. This repository is private; Git must be authenticated to GitHub, and Go must treat the module as private. For example:

```sh
gh auth login
gh auth setup-git
go env -w GOPRIVATE=github.com/jsirianni/tradeview-go
go get github.com/jsirianni/tradeview-go/pkg/tradeview@<tag-or-commit>
```

Include any existing `GOPRIVATE` patterns in the comma-separated value. Replace `<tag-or-commit>` with the revision you want to consume.

Import the public client package:

```go
import "github.com/jsirianni/tradeview-go/pkg/tradeview"
```

## Finite requests

Use `NewClientWithResponses` for generated request types and typed responses. Endpoint method names follow the upstream operation IDs. Form-body methods use a `WithFormdataBody` suffix, such as `PlaceOrderWithFormdataBodyWithResponse`.

```go
client, err := tradeview.NewClientWithResponses(
    "https://broker.example/api",
    tradeview.WithBearerToken(token),
)
if err != nil {
    return err
}

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

response, err := client.GetAccountsWithResponse(ctx, &tradeview.GetAccountsParams{
    Locale: tradeview.Locale("en"),
})
if err != nil {
    return err
}
fmt.Println(response.StatusCode())
```

`NewClient` provides raw methods returning `*http.Response` when the caller needs direct access to the response. Raw callers must close response bodies and handle response status/envelopes themselves. Typed finite methods read and close the response body and decode the declared response union.

Finite parsers read at most 16 MiB of decoded response data by default, including error responses and unknown-length bodies. `WithFiniteResponseLimit(bytes)` sets a positive client budget (less than `math.MaxInt64`); overflow returns `*ResponseSizeError` without retaining the oversized payload and closes the body. Standalone `Parse...Response` functions also enforce the default. Raw methods allow callers to process larger bodies under their own limits.

Optional fields retain presence information. Set a pointer to zero or false when that value must be transmitted; leave it nil to omit it. Numeric schema values use `float64`.

Symbol-info responses accept either per-symbol arrays or a shared scalar metadata value. Typed `SymbolInfoArrays` and `SymbolInfoResponse` values expand shared metadata to one entry per returned symbol; a scalar `symbol` represents one symbol. Existing arrays, nulls, and omitted fields retain their usual behavior. Shared scalar metadata requires at least one symbol. Typed symbol models reject more than 65,536 symbols or compact expansion above a 16 MiB budget (serialized values plus estimated element storage) with `*SymbolInfoSizeError`. The budget is checked across all scalar fields and duplicate properties before repeated JSON is allocated. Raw unions remain available for applications needing a different representation. Marshaling a typed model emits arrays; the finite response `Body` and response union retain the original broker JSON.

## Authentication and HTTP configuration

- `WithBearerToken(token)` supplies a static bearer token.
- `WithTokenSource(source)` asks the caller's `Token(context.Context) (string, error)` implementation for a token on each request.
- `WithHTTPClient(client)` supplies an HTTP client or compatible doer.
- `WithRequestEditorFn(editor)` adds a request editor; generated methods also accept per-request editors.
- `WithLocale(locale)` supplies a locale default when the request locale is absent or empty, including when other filters or pagination parameters are supplied. Explicit nonempty per-request locales take precedence.

Bindings include `/authorize` and `/logout`. Your application owns OAuth/PKCE/JWT token acquisition and renewal. Token endpoints and credentials depend on the broker. The package does not persist credentials or select a broker endpoint.

The library-created HTTP client permits redirects only within the original origin (scheme, hostname, and effective port), rejects HTTPS-to-HTTP redirects, and stops after ten redirects. This prevents redirects from replaying passwords and other request bodies to another origin. A client supplied through `WithHTTPClient` retains its own redirect policy; configure equivalent protections before sending credentials. Cross-origin migrations require an explicitly trusted policy on that custom client.

Set finite-request deadlines through contexts or your HTTP client. Streaming contexts must remain active for the desired connection lifetime. An HTTP client's overall timeout also limits a streaming connection.

Use `WithLogger(logger)` for structured operation logs. Configure the standard
`log/slog` handler to write to stdout or an open file and select its format and
severity level. Logging is disabled by default and works independently of
OpenTelemetry. See [logging configuration and records](logging.md).

## HTTP streaming

Streams carry newline-delimited JSON over HTTP. They are not WebSocket connections. Open them through the typed `Open...` helpers on `*Client`:

```go
client, err := tradeview.NewClient(
    "https://broker.example/api",
    tradeview.WithBearerToken(token),
)
if err != nil {
    return err
}

stream, err := client.OpenStreamOrders(ctx, accountID, &tradeview.StreamOrdersParams{
    Locale: tradeview.Locale("en"),
})
if err != nil {
    return err
}
defer stream.Close()

for stream.Next() {
    event := stream.Value()
    fmt.Println(event.Kind)
    if event.Data != nil {
        // Process this message in your application.
    }
}
return stream.Err()
```

The helpers are `OpenStreamState`, `OpenStreamOrders`, `OpenStreamPositions`, `OpenStreamNetPositions`, `OpenStreamIndividualPositions`, `OpenStreamNotifications`, `OpenStreamQuotes`, `OpenStreamDepth`, and `OpenStreaming`.

Account events retain typed data, ping and error variants, plus the raw JSON frame. Market-price events retain quote, trade, daily-bar, heartbeat, and legacy bid/ask variants. An error frame is yielded once and then terminates iteration with an `APIError`. Clean EOF leaves `Err()` nil.

Stream status dispatch uses the exact JSON key `s` (including its escaped JSON spelling); duplicate exact keys use the last value. Case-folded extension keys such as `S` do not select a status. The decoded event's kind also determines terminal API errors, so an extension cannot independently terminate a valid event. Numeric market `s` values remain trade/ask/bid sizes.

Market quotes, trades, daily bars, and legacy ask/bid messages must supply the
selected schema's required fields with non-null values and the declared JSON
types. Explicit numeric zeros and empty strings remain valid; no trading-value
constraints are added. A malformed frame makes `Next` return false with a
decoding error, closes the stream, and retains the last successfully decoded
value. Unknown extension fields remain available in the event's `Raw` bytes.

`WithMaxFrameBytes(n)` adjusts the default 1 MiB maximum frame size. `WithStreamRequestEditor(editor)` adds an editor to a stream request. Close a stream when finished, or cancel its context. The package does not reconnect or accumulate snapshots into account state.

All nine typed stream openers require HTTP 200 and a valid `Content-Type` of
`application/json`, an application media type ending in `+json`,
`application/x-ndjson`, or `application/ndjson`. Media-type parameters and case
differences are accepted. Missing, malformed, or incompatible types (including
`text/plain`, `text/event-stream`, and `application/json-seq`) are rejected because
the decoder expects one JSON value per line. Undeclared 2xx statuses and rejected
media types return `*ProtocolError` before yielding a stream, close the body, and
record a failed operation. Diagnostics retain at most `WithMaxFrameBytes` bytes;
underlying diagnostic-read errors remain accessible through `errors.Is`.
Non-2xx responses retain their bounded `*HTTPError` behavior. Raw methods remain
available to callers handling other handshake or framing conventions.

## Errors and form fields

Typed finite methods return `*APIError` for an `s: "error"` response, including HTTP-200 errors. Its fields are `StatusCode`, `Code` (the upstream `errtype`), `Message` (the upstream `errmsg`), and `Body`. Non-success HTTP responses return `*HTTPError` with `StatusCode`, `Status`, and `Body`. Use `errors.As` to inspect these types. Undeclared successful status codes, missing or non-JSON content types, malformed envelopes, and incompatible model field types return `*ProtocolError` with `StatusCode`, `ContentType`, `Body`, and `Err`. Its `Unwrap` method preserves underlying JSON errors. Typed finite responses require HTTP 200 and `application/json` or an application media type ending in `+json`; media-type parameters are supported. Raw methods remain available for inspecting arbitrary responses. Transport failures also return errors.

Finite parsers require an object and a recognized non-null status, except mapping responses whose schema has no status field. Malformed broker error details cannot appear successful. Before returning a response union, parsers recursively validate supplied field types, including nested unions and richer success alternatives. Unknown extension fields/enums and existing omitted/null model-field behavior remain supported; this does not enforce all schema-required fields or trading rules. Compact symbol metadata also passes its existing expansion and count limits before success. Response bodies and union bytes retain the broker's original JSON, bounded by the configured finite response limit.

Generated forms cover POST, PUT, and DELETE requests. Scalar arrays use repeated form keys; nested values such as `exitLevels` use a compact JSON field. Instrument `symbols` queries use comma-separated values as documented upstream.

For broker-defined order fields, pass `WithExtraOrderFields(url.Values)` as a per-request editor to the relevant order method. The helper adds fields to a form body and rejects collisions with standard fields. Standard generated models continue to follow the upstream schema.

Execution and order-history methods expose the API's time-window, page-size, and cursor fields. Callers choose when to request another page. The library does not automatically retry, reconnect, paginate, cache, or perform trading/account business logic.
