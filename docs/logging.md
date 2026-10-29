# Logging

Enable client logging with `WithLogger(*slog.Logger)`. Logging is disabled by
default, does not use or replace the process-wide default logger, and works
independently of `WithTelemetry`. The application owns the logger, its output,
and any file rotation or retention policy.

## Select stdout or a file

Use the standard library's `log/slog` package. This example reads an optional
file path from application configuration; an empty path selects stdout:

```go
// Imports: io, log/slog, os, and the tradeview package.
var output io.Writer = os.Stdout
if logPath != "" {
    file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
    if err != nil {
        return err
    }
    defer file.Close() // Close after all client operations and streams finish.
    output = file
}

logger := slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
client, err := tradeview.NewClientWithResponses(brokerURL,
    tradeview.WithLogger(logger),
    tradeview.WithBearerToken(token),
)
if err != nil {
    return err
}
```

The file is opened in append mode and created with owner-only permissions on
systems that support them. Existing file permissions are preserved. Use
`slog.NewTextHandler` for human-readable output, or supply an existing logger
with application fields. The client never closes the writer. File-open errors
are handled by the application before creating the client. As with ordinary
`slog` calls, handler/write errors do not change the API operation's result.

`slog.HandlerOptions.Level` controls filtering. Successful operations are
`INFO`; failures, including cancellation and deadlines, are `ERROR`. Set the
level to `slog.LevelError` to retain only failures, or use `slog.LevelVar` to
change it at runtime. Custom handlers must support concurrent calls as required
by `slog.Handler`. A nil logger is rejected; omit the option to disable logging.

## Records and lifetime

Each operation emits one `tradeview operation completed` record containing
`operation`, `method`, `route`, `streaming`, `duration`, and `response_bytes`.
`status_code` is included when an HTTP response exists; failures also have
`error_type` (`api`, `http`, `protocol`, `response_size`, `canceled`,
`deadline_exceeded`, or `client`). The handler supplies the timestamp and level.
JSON handlers encode `duration` as nanoseconds; text handlers use Go duration
strings.

- Typed finite operations log after parsing and closing the response.
- Raw operations log on body EOF, a read error, or `Body.Close`. Always close
  raw bodies. They record HTTP/transport outcomes without interpreting payloads.
- Typed streams log once at EOF, a terminal error, cancellation, or `Close`.
  Individual frames do not produce logs. Duration includes time between reads.
- Request construction, token-source, and request-editor failures produce an
  error record. Invalid stream options fail before an operation starts.
- Standalone response parsers do not start operations. Parsing an unconsumed raw
  response transfers its existing operation's completion to that parser.
  Deferred union-accessor
  errors occur after the typed operation has already completed.

Requests made inside editors, token sources, or client adapters log their own
completion. Reusing an outer typed request's context does not turn nested raw
requests into typed operations.

Routes are fixed templates from the pinned schema. Client-generated attributes
exclude actual URLs, IDs, symbols, query values, headers, request/response
bodies, credentials, and raw error text. The byte count measures consumed bytes,
including stream read-ahead. Application-supplied logger fields and custom
handler behavior remain under the application's control.

Use both `WithLogger` and `WithTelemetry` when both outputs are needed. The
logging option alone does not create spans, record metrics, or inject tracing
headers. No OpenTelemetry logging provider is required.
