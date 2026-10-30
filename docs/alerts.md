# Sending alerts

Applications use `pkg/alerts` to send notifications independently of the
TradingView client. The application reads broker data, evaluates its own rules,
chooses a recipient, and supplies the alert content. Slack is the first sender;
future email and text senders can implement the same interface.

```go
type Alert struct {
    Title string // Optional, plain text.
    Body  string // Required, plain text.
}

type Sender interface {
    Send(ctx context.Context, recipient string, alert Alert) error
}
```

Receive an `alerts.Sender` in application code and select the implementation
when constructing the application. A recipient is an address understood by
that sender: a Slack ID today, or an email address or telephone number for a
future implementation. The interface does not resolve application user IDs.
See the [composition example](../pkg/alerts/slack/example_test.go) for wiring
the TradingView client and sender into the same application.

## Slack setup

1. Create a Slack app, add the bot token scope `chat:write`, and install it in
   the workspace. Supply the bot token to `slack.NewClient`; your application
   owns token storage and renewal.
2. For channel alerts, invite the bot to the destination channel and use its
   channel ID. Private channels require access. If the app must post to public
   channels it has not joined, Slack offers the additional `chat:write.public`
   scope.
3. For individual alerts, supply the user's Slack ID or an existing direct
   message conversation ID accessible to the bot. Slack determines whether the
   app can send to that destination. The library does not look up users, open
   conversations separately, or manage app installation.

Slack delivery calls [`chat.postMessage`](https://docs.slack.dev/reference/methods/chat.postMessage/)
with the recipient as its `channel` argument. Use Slack IDs instead of display
names; the same sender can target a different ID on every call.

```go
sender, err := slack.NewClient(os.Getenv("SLACK_BOT_TOKEN"))
if err != nil {
    return err
}

var notifications alerts.Sender = sender
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

return notifications.Send(ctx, recipientID, alerts.Alert{
    Title: "Account update",
    Body:  "New account data is available.",
})
```

Import `github.com/jsirianni/tradeview-go/pkg/alerts` and
`github.com/jsirianni/tradeview-go/pkg/alerts/slack`, plus the standard packages
used by your application. No additional Slack SDK dependency is required.

## Content and delivery behavior

The title is optional and the body must contain non-whitespace text. Slack
renders a nonempty title followed by a blank line and the body. Whitespace in
valid content is preserved. Content is plain text: Slack control characters are
escaped, markup parsing and link previews are disabled, and strings resembling
mentions remain text. See [Slack text formatting](https://docs.slack.dev/messaging/formatting-message-text/).

The sender rejects outgoing text above 40,000 Unicode characters, counted after
Slack escaping and joining the title and body. This avoids Slack's documented
message truncation threshold. It does not split alerts into multiple messages.
Blank tokens and recipients are also rejected before sending.

Each `Send` makes one synchronous attempt. A nil error means Slack accepted the
message; it does not confirm that a person saw or read it. Applications own
deadlines, retries, scheduling, recipient lookup, fan-out, and duplicate
prevention. The library contains no alert rules, background queue, or persistence.

The default HTTP client rejects redirects and has no overall timeout; set a
deadline on each call. `slack.WithHTTPClient` accepts any implementation of
`slack.HTTPDoer`, including `*http.Client`. A custom doer controls its own
redirect, timeout, and retry policies, so configure it to protect the bot token
and preserve the intended delivery behavior. Custom doers must be safe for
concurrent use if the application shares the sender across goroutines.

Responses are limited to 1 MiB and response bodies are closed. Provider errors,
malformed responses, and transport failures return errors without including
credentials or message bodies in their error text.

## Error handling

Slack can reject a request with an HTTP error or with `ok: false` inside an
HTTP 200 response. Inspect `*slack.APIError` with `errors.As`; `StatusCode`
contains the HTTP status, `Code` contains the Slack error code when available,
and `RetryAfter` contains a valid retry delay when Slack supplies one.

```go
err := notifications.Send(ctx, recipientID, alert)
if err == nil {
    return nil
}
if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
    return err
}
var apiErr *slack.APIError
if errors.As(err, &apiErr) {
    log.Printf("Slack rejected alert: status=%d code=%s retry_after=%s",
        apiErr.StatusCode, apiErr.Code, apiErr.RetryAfter)
}
return err
```

The sender does not sleep or retry. For HTTP 429, Slack supplies a
`Retry-After` delay in seconds; applications can use it when scheduling later
attempts. See [Slack rate limits](https://docs.slack.dev/apis/web-api/rate-limits/).
An absent or invalid delay is represented by zero; zero is not an instruction
to retry immediately. A transport error or deadline can occur after Slack has
accepted the message, so retrying an uncertain outcome can create duplicates.
Underlying transport and context errors remain available through `errors.Is`
and `errors.As`.
