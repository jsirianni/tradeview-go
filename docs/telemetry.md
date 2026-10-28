# OpenTelemetry

Enable instrumentation on a raw or typed client with `WithTelemetry`. This
repository is a client library, so the application owns SDK providers, exporters,
sampling, resource attributes, metric readers, and shutdown. The library uses
OpenTelemetry APIs only in production and does not install or shut down global
providers. Logging is unaffected.

```go
client, err := tradeview.NewClientWithResponses(brokerURL,
    tradeview.WithTelemetry(tradeview.TelemetryOptions{
        TracerProvider: tracerProvider,
        MeterProvider:  meterProvider,
        Propagator:     propagation.TraceContext{},
    }),
)
```

Import `go.opentelemetry.io/otel/propagation` for the W3C trace-context propagator.
Pass the application's SDK providers; use its exporter/reader configuration to
send data to an OpenTelemetry Collector or another backend. Follow the official
[Go instrumentation guide](https://opentelemetry.io/docs/languages/go/instrumentation/)
for SDK setup. Flush and shut down the providers when the application exits.
Nil option fields use the corresponding global OpenTelemetry instance at client
construction. `WithTelemetry(TelemetryOptions{})` therefore uses global setup;
the default global providers and propagator produce no exported data.

Without `WithTelemetry`, instrumentation and propagation are disabled. Existing
HTTP clients, request editors, authentication, and cancellation continue to work.
The option can appear before or after `WithHTTPClient`. Propagation runs after
request editors so the operation span reaches custom HTTP doers too. Application
contexts retain their parent spans. Using an independently instrumented HTTP
transport can produce an additional child HTTP span.

## Traces and lifecycle

The scope is `github.com/jsirianni/tradeview-go/pkg/tradeview`. Each operation
creates one client span named `tradeview.<operationId>`, including raw body
variants and all nine streaming operations. The span begins before request
construction, authentication, and request editors.

- Typed finite calls end after parsing and closing the response, recording HTTP,
  broker, protocol, size-limit, transport, and context errors.
- Raw calls end on response EOF, a read error, or `Body.Close`. They report
  transport/HTTP outcomes; they do not interpret the payload. Always close raw
  bodies, including bodies you choose not to read.
- Typed streams remain open until EOF, a terminal frame/decode error, context
  cancellation, or `Close`. Closing is idempotent and ends telemetry once.
- Standalone response parsers do not start new spans. Deferred union accessor
  errors occur after the typed call returns and are outside its operation span.
- Invalid stream options fail before opening an operation and produce no span.

Error status uses a bounded category: `api`, `http`, `protocol`, `response_size`,
`canceled`, `deadline_exceeded`, or `client`. Error messages and exception events
are deliberately omitted because they may contain credentials or broker data.

## Metrics

| Instrument | Type | Unit | Meaning |
| --- | --- | --- | --- |
| `tradeview.client.operations` | Counter | `{operation}` | Completed operations |
| `tradeview.client.active_operations` | UpDownCounter | `{operation}` | Operations awaiting completion |
| `tradeview.client.operation.duration` | Histogram | `s` | Time through response consumption or stream completion |
| `tradeview.client.response.body.size` | Histogram | `By` | Bytes consumed from the response body |
| `tradeview.client.stream.frames` | Counter | `{frame}` | Decoded typed stream frames, including ping and broker error frames |

All instruments carry `tradeview.operation`, `http.request.method`,
`url.template`, and `tradeview.streaming`. Completion, duration, and body-size
metrics also include the HTTP status when available and `error.type` on errors.
Active-operation and frame metrics use only the starting dimensions so active
increments/decrements balance. Body size measures bytes actually read (including
scanner read-ahead), not the declared Content-Length or unread bytes. Duration
includes time the caller spends between stream reads or before closing a raw body.

Route templates come from the pinned schema. Telemetry never records actual
URLs, account/order IDs, symbol names, query values, request/response bodies,
headers, tokens, broker error codes/messages, or arbitrary exception text.
The selected propagator controls which context headers are sent; choose
`propagation.TraceContext{}` to propagate traces without baggage.
