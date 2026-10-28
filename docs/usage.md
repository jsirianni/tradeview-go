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

Optional fields retain presence information. Set a pointer to zero or false when that value must be transmitted; leave it nil to omit it. Numeric schema values use `float64`.

## Authentication and HTTP configuration

- `WithBearerToken(token)` supplies a static bearer token.
- `WithTokenSource(source)` asks the caller's `Token(context.Context) (string, error)` implementation for a token on each request.
- `WithHTTPClient(client)` supplies an HTTP client or compatible doer.
- `WithRequestEditorFn(editor)` adds a request editor; generated methods also accept per-request editors.
- `WithLocale(locale)` supplies a locale default when the request has no locale parameter.

Bindings include `/authorize` and `/logout`. Your application owns OAuth/PKCE/JWT token acquisition and renewal. Token endpoints and credentials depend on the broker. The package does not persist credentials or select a broker endpoint.

The library-created HTTP client rejects HTTPS-to-HTTP redirects and stops after ten redirects. A client supplied through `WithHTTPClient` retains its own redirect policy; configure it to reject HTTPS-to-HTTP redirects before sending credentials.

Set finite-request deadlines through contexts or your HTTP client. Streaming contexts must remain active for the desired connection lifetime. An HTTP client's overall timeout also limits a streaming connection.

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

`WithMaxFrameBytes(n)` adjusts the default 1 MiB maximum frame size. `WithStreamRequestEditor(editor)` adds an editor to a stream request. Close a stream when finished, or cancel its context. The package does not reconnect or accumulate snapshots into account state.

## Errors and form fields

Typed finite methods return `*APIError` for an `s: "error"` response, including HTTP-200 errors. Its fields are `StatusCode`, `Code` (the upstream `errtype`), `Message` (the upstream `errmsg`), and `Body`. Non-success HTTP responses return `*HTTPError` with `StatusCode`, `Status`, and `Body`. Use `errors.As` to inspect either type. Malformed responses and transport failures also return errors.

Generated forms cover POST, PUT, and DELETE requests. Scalar arrays use repeated form keys; nested values such as `exitLevels` use a compact JSON field. Instrument `symbols` queries use comma-separated values as documented upstream.

For broker-defined order fields, pass `WithExtraOrderFields(url.Values)` as a per-request editor to the relevant order method. The helper adds fields to a form body and rejects collisions with standard fields. Standard generated models continue to follow the upstream schema.

Execution and order-history methods expose the API's time-window, page-size, and cursor fields. Callers choose when to request another page. The library does not automatically retry, reconnect, paginate, cache, or perform trading/account business logic.
