// Package slack delivers application alerts through the Slack bot Web API.
// Clients use caller-supplied bot tokens and per-call Slack recipient IDs.
// Callers own deadlines, token acquisition, rate limiting, and retry decisions.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/jsirianni/tradeview-go/pkg/alerts"
)

const (
	postMessageURL = "https://slack.com/api/chat.postMessage"
	maxTextRunes   = 40000
)

var escapeText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// HTTPDoer performs an HTTP request. Implementations must honor request contexts,
// and support concurrent calls if their Client is used concurrently.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client delivers alerts using chat.postMessage. Construct it with NewClient.
// It is safe for concurrent use when its configured HTTPDoer is also safe.
type Client struct {
	token      string
	httpClient HTTPDoer
}

var _ alerts.Sender = (*Client)(nil)

// ClientOption configures a Client during construction.
type ClientOption func(*Client) error

// NewClient creates a sender with a bot token. The default HTTP client refuses
// redirects and has no timeout; supply deadlines through Send's context.
// Construction does not make network requests.
func NewClient(token string, opts ...ClientOption) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("slack: bot token must not be blank")
	}
	client := &Client{token: token, httpClient: defaultHTTPClient()}
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("slack: client option must not be nil")
		}
		if err := opt(client); err != nil {
			return nil, &operationError{operation: "configure client", cause: err}
		}
	}
	return client, nil
}

// WithHTTPClient supplies a custom transport. The caller owns its redirect,
// timeout, and retry policies; the Slack client itself never retries.
func WithHTTPClient(client HTTPDoer) ClientOption {
	return func(sender *Client) error {
		if client == nil {
			return errors.New("slack: HTTP client must not be nil")
		}
		// An interface holding a typed nil is non-nil, but cannot safely Do.
		value := reflect.ValueOf(client)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				return errors.New("slack: HTTP client must not be nil")
			}
		}
		sender.httpClient = client
		return nil
	}
}

func defaultHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// Send makes one delivery attempt to a Slack channel, conversation, or user ID.
// It sends plain text with an optional title separated from the body by a blank
// line. Slack control characters are escaped and the escaped text must contain
// at most 40,000 Unicode characters. An error may leave delivery uncertain.
func (client *Client) Send(ctx context.Context, recipient string, alert alerts.Alert) (err error) {
	if client == nil || client.httpClient == nil || strings.TrimSpace(client.token) == "" {
		return errors.New("slack: client must be initialized with NewClient")
	}
	if ctx == nil {
		return errors.New("slack: context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return &operationError{operation: "send request", cause: err}
	}
	if strings.TrimSpace(recipient) == "" {
		return errors.New("slack: recipient must not be blank")
	}
	if strings.TrimSpace(alert.Body) == "" {
		return errors.New("slack: alert body must not be blank")
	}
	if !utf8.ValidString(alert.Title) || !utf8.ValidString(alert.Body) || !utf8.ValidString(recipient) {
		return errors.New("slack: recipient and alert content must be valid UTF-8")
	}
	text := alert.Body
	if alert.Title != "" {
		text = alert.Title + "\n\n" + text
	}
	text = escapeText.Replace(text)
	if utf8.RuneCountInString(text) > maxTextRunes {
		return errors.New("slack: escaped alert text exceeds 40000 characters")
	}
	payload := struct {
		Channel     string `json:"channel"`
		Text        string `json:"text"`
		Markdown    bool   `json:"mrkdwn"`
		Parse       string `json:"parse"`
		LinkNames   bool   `json:"link_names"`
		UnfurlLinks bool   `json:"unfurl_links"`
		UnfurlMedia bool   `json:"unfurl_media"`
	}{Channel: recipient, Text: text, Parse: "none"}
	body, err := json.Marshal(payload)
	if err != nil {
		return &operationError{operation: "encode request", cause: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, postMessageURL, bytes.NewReader(body))
	if err != nil {
		return &operationError{operation: "create request", cause: err}
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := client.httpClient.Do(request)
	if response != nil && response.Body != nil {
		defer func() {
			// A cleanup-only failure must not turn an accepted message into a
			// delivery failure that an application might retry. Preserve cleanup
			// causes when delivery has already failed.
			if closeErr := response.Body.Close(); closeErr != nil && err != nil {
				err = errors.Join(err, &operationError{operation: "close response", cause: closeErr})
			}
		}()
	}
	if err != nil {
		return &operationError{operation: "send request", cause: err}
	}
	if response == nil {
		return errors.New("slack: HTTP client returned no response")
	}
	return checkResponse(response)
}
