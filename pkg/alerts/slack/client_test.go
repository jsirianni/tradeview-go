package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jsirianni/tradeview-go/pkg/alerts"
)

type testDoer func(*http.Request) (*http.Response, error)

func (f testDoer) Do(r *http.Request) (*http.Response, error) { return f(r) }

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type testBody struct {
	reader   io.Reader
	read     int
	closed   int
	closeErr error
}

func (b *testBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (b *testBody) Close() error {
	b.closed++
	return b.closeErr
}

type failedReader struct{ err error }

func (r failedReader) Read([]byte) (int, error) { return 0, r.err }

func testResponse(status int, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}
}

func newTestClient(t *testing.T, doer HTTPDoer) *Client {
	t.Helper()
	client, err := NewClient("test-bot-token", WithHTTPClient(doer))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestSendRequestAndRendering(t *testing.T) {
	for _, tt := range []struct {
		name, recipient, text string
		alert                 alerts.Alert
	}{
		{"body only", "C0123456", "A price crossed its threshold.", alerts.Alert{Body: "A price crossed its threshold."}},
		{"title and body", "D0123456", "Price alert\n\nAAPL exceeded 200.", alerts.Alert{Title: "Price alert", Body: "AAPL exceeded 200."}},
		{"user recipient", "U0123456", "Fill received", alerts.Alert{Body: "Fill received"}},
		{"preserve whitespace", "G0123456", "  title\n\n\n \tbody \n", alerts.Alert{Title: "  title\n", Body: " \tbody \n"}},
		{"literal control characters", "C0123456", "A &amp; B &lt; C &gt; D\n\n&lt;@U123&gt; &lt;!channel&gt; *literal* &amp;lt;", alerts.Alert{Title: "A & B < C > D", Body: "<@U123> <!channel> *literal* &lt;"}},
		{"unicode", "C0123456", "日本株 📈\n\n価格：１００円 🦊", alerts.Alert{Title: "日本株 📈", Body: "価格：１００円 🦊"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			body := &testBody{reader: strings.NewReader(`{"ok":true,"channel":"C0123456","ts":"123.456"}`)}
			client := newTestClient(t, testDoer(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.String() != "https://slack.com/api/chat.postMessage" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer test-bot-token" {
					t.Errorf("Authorization = %q", got)
				}
				if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
					t.Errorf("Content-Type = %q", got)
				}
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				for key, want := range map[string]string{"channel": tt.recipient, "text": tt.text, "parse": "none"} {
					var got string
					if err := json.Unmarshal(payload[key], &got); err != nil || got != want {
						t.Errorf("%s = %q, want %q; error = %v", key, got, want, err)
					}
				}
				for _, key := range []string{"mrkdwn", "link_names", "unfurl_links", "unfurl_media"} {
					if string(payload[key]) != "false" {
						t.Errorf("%s = %s, want explicit false", key, payload[key])
					}
				}
				return testResponse(http.StatusOK, body), nil
			}))
			if err := client.Send(context.Background(), tt.recipient, tt.alert); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || body.closed != 1 {
				t.Fatalf("requests = %d, body closes = %d", calls, body.closed)
			}
		})
	}
}

func TestNewClientValidation(t *testing.T) {
	for _, token := range []string{"", " \n\t", "\u2003"} {
		if client, err := NewClient(token); err == nil || client != nil {
			t.Errorf("NewClient(%q) = %v, %v; want nil client and error", token, client, err)
		}
	}
	for _, option := range []ClientOption{nil, WithHTTPClient(nil), WithHTTPClient((*http.Client)(nil)), WithHTTPClient(testDoer(nil))} {
		if client, err := NewClient("token", option); err == nil || client != nil {
			t.Errorf("invalid option = %v, %v; want nil client and error", client, err)
		}
	}
}

func TestSendValidationDoesNotContactSlack(t *testing.T) {
	for _, tt := range []struct {
		name, recipient string
		alert           alerts.Alert
	}{
		{"empty recipient", "", alerts.Alert{Body: "body"}},
		{"blank recipient", " \t\n", alerts.Alert{Body: "body"}},
		{"unicode blank recipient", "\u2003", alerts.Alert{Body: "body"}},
		{"empty body", "C123", alerts.Alert{Title: "title"}},
		{"blank body", "C123", alerts.Alert{Body: " \t\n"}},
		{"unicode blank body", "C123", alerts.Alert{Body: "\u2003"}},
		{"invalid UTF8 recipient", "C\xff", alerts.Alert{Body: "body"}},
		{"invalid UTF8 title", "C123", alerts.Alert{Title: "\xff", Body: "body"}},
		{"invalid UTF8 body", "C123", alerts.Alert{Body: "\xff"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
				calls++
				return testResponse(200, io.NopCloser(strings.NewReader(`{"ok":true}`))), nil
			}))
			if err := client.Send(context.Background(), tt.recipient, tt.alert); err == nil {
				t.Fatal("invalid alert accepted")
			}
			if calls != 0 {
				t.Fatalf("made %d requests for invalid input", calls)
			}
		})
	}
}

func TestSendMessageSizeBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name    string
		alert   alerts.Alert
		wantErr bool
	}{
		{"ascii at limit", alerts.Alert{Body: strings.Repeat("a", 40000)}, false},
		{"ascii over limit", alerts.Alert{Body: strings.Repeat("a", 40001)}, true},
		{"unicode at limit", alerts.Alert{Body: strings.Repeat("🦊", 40000)}, false},
		{"unicode over limit", alerts.Alert{Body: strings.Repeat("🦊", 40001)}, true},
		{"title separator at limit", alerts.Alert{Title: "x", Body: strings.Repeat("a", 39997)}, false},
		{"title separator over limit", alerts.Alert{Title: "x", Body: strings.Repeat("a", 39998)}, true},
		{"escaped at limit", alerts.Alert{Body: strings.Repeat("&", 8000)}, false},
		{"escaped over limit", alerts.Alert{Body: strings.Repeat("&", 8000) + "x"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
				calls++
				return testResponse(200, io.NopCloser(strings.NewReader(`{"ok":true}`))), nil
			}))
			err := client.Send(context.Background(), "C123", tt.alert)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, want error = %t", err, tt.wantErr)
			}
			wantCalls := 1
			if tt.wantErr {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("requests = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestSendAPIErrorsDoNotRetry(t *testing.T) {
	for _, tt := range []struct {
		name, body, code, retry string
		status                  int
		wantDelay               time.Duration
	}{
		{"logical error", `{"ok":false,"error":"channel_not_found"}`, "channel_not_found", "", 200, 0},
		{"unknown field cannot override rejection", `{"ok":false,"error":"invalid_auth","OK":true}`, "invalid_auth", "", 200, 0},
		{"auth envelope", `{"ok":false,"error":"invalid_auth"}`, "invalid_auth", "", 200, 0},
		{"auth status", `{"ok":false,"error":"invalid_auth"}`, "invalid_auth", "", 401, 0},
		{"forbidden", `{"ok":false,"error":"missing_scope"}`, "missing_scope", "", 403, 0},
		{"rate limited", `{"ok":false,"error":"ratelimited"}`, "ratelimited", "30", 429, 30 * time.Second},
		{"rate limited plain response", "rate limited", "", "2", 429, 2 * time.Second},
		{"rate limited empty response", "", "", "0", 429, 0},
		{"invalid retry", `{"ok":false,"error":"ratelimited"}`, "ratelimited", "tomorrow", 429, 0},
		{"negative retry", `{"ok":false,"error":"ratelimited"}`, "ratelimited", "-1", 429, 0},
		{"fractional retry", `{"ok":false,"error":"ratelimited"}`, "ratelimited", "1.5", 429, 0},
		{"overflowing seconds", `{"ok":false,"error":"ratelimited"}`, "ratelimited", "9999999999999999999999999", 429, 0},
		{"overflowing duration", `{"ok":false,"error":"ratelimited"}`, "ratelimited", "9223372037", 429, 0},
		{"server error", "upstream unavailable", "", "", 503, 0},
		{"false success envelope on failure status", `{"ok":true}`, "", "", 500, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			body := &testBody{reader: strings.NewReader(tt.body)}
			client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
				calls++
				response := testResponse(tt.status, body)
				response.Header.Set("Retry-After", tt.retry)
				return response, nil
			}))
			err := client.Send(context.Background(), "C123", alerts.Alert{Body: "body"})
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want *APIError", err)
			}
			if apiErr.StatusCode != tt.status || apiErr.Code != tt.code || apiErr.RetryAfter != tt.wantDelay {
				t.Fatalf("APIError = %+v, want status=%d code=%q delay=%s", apiErr, tt.status, tt.code, tt.wantDelay)
			}
			if calls != 1 || body.closed != 1 {
				t.Fatalf("requests = %d, body closes = %d", calls, body.closed)
			}
		})
	}
}

func TestSendRejectsMalformedSuccessResponses(t *testing.T) {
	for _, bodyText := range []string{
		"", "not JSON", `{`, `null`, `[]`, `{}`, `{"OK":true}`, `{"ok":null}`, `{"ok":"true"}`, `{"ok":1}`,
		`{"ok":false}`, `{"ok":false,"error":null}`, `{"ok":false,"error":1}`, `{"ok":false,"error":""}`,
		`{"ok":false,"error":" "}`, `{"ok":true} {"ok":false,"error":"invalid_auth"}`,
	} {
		t.Run(bodyText, func(t *testing.T) {
			calls := 0
			body := &testBody{reader: strings.NewReader(bodyText)}
			client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
				calls++
				return testResponse(200, body), nil
			}))
			if err := client.Send(context.Background(), "C123", alerts.Alert{Body: "body"}); err == nil {
				t.Fatal("malformed success response accepted")
			}
			if calls != 1 || body.closed != 1 {
				t.Fatalf("requests = %d, body closes = %d", calls, body.closed)
			}
		})
	}
	for _, status := range []int{201, 204} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
				return testResponse(status, io.NopCloser(strings.NewReader(`{"ok":true}`))), nil
			}))
			if err := client.Send(context.Background(), "C123", alerts.Alert{Body: "body"}); err == nil {
				t.Fatal("unexpected successful HTTP status accepted")
			}
		})
	}
}

func TestSendResponseLimitAndCleanup(t *testing.T) {
	const limit = 1 << 20
	for _, size := range []int{limit, limit + 1, limit * 2} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			content := `{"ok":true}`
			body := &testBody{reader: strings.NewReader(content + strings.Repeat(" ", size-len(content)))}
			client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
				return testResponse(200, body), nil
			}))
			err := client.Send(context.Background(), "C123", alerts.Alert{Body: "body"})
			if (err != nil) != (size > limit) {
				t.Fatalf("size=%d error=%v", size, err)
			}
			if body.read > limit+1 || body.closed != 1 {
				t.Fatalf("bytes read=%d body closes=%d", body.read, body.closed)
			}
		})
	}
}

func TestSendPreservesReadError(t *testing.T) {
	want := errors.New("read failed")
	body := &testBody{reader: failedReader{err: want}}
	client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
		return testResponse(200, body), nil
	}))
	if err := client.Send(context.Background(), "C123", alerts.Alert{Body: "body"}); !errors.Is(err, want) {
		t.Fatalf("error=%v, want wrapped read error", err)
	}
	if body.closed != 1 {
		t.Fatalf("body closes=%d", body.closed)
	}
}

func TestSendCloseErrorDoesNotUndoAcceptance(t *testing.T) {
	body := &testBody{reader: strings.NewReader(`{"ok":true}`), closeErr: errors.New("close failed")}
	client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
		return testResponse(200, body), nil
	}))
	if err := client.Send(context.Background(), "C123", alerts.Alert{Body: "body"}); err != nil {
		t.Fatalf("accepted delivery turned into failure: %v", err)
	}
	if body.closed != 1 {
		t.Fatalf("body closes=%d", body.closed)
	}
}

func TestSendInvalidClientAndContext(t *testing.T) {
	client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
		t.Error("invalid client or context reached the transport")
		return nil, errors.New("unexpected request")
	}))
	for _, tt := range []struct {
		name   string
		client *Client
		ctx    context.Context
	}{
		{"nil client", nil, context.Background()},
		{"zero client", &Client{}, context.Background()},
		{"nil context", client, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.client.Send(tt.ctx, "C123", alerts.Alert{Body: "body"}); err == nil {
				t.Fatal("invalid client or context accepted")
			}
		})
	}
}

func TestSendMissingTransportResponse(t *testing.T) {
	for _, response := range []*http.Response{nil, {StatusCode: 200}, {StatusCode: 503}} {
		client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) { return response, nil }))
		if err := client.Send(context.Background(), "C123", alerts.Alert{Body: "body"}); err == nil {
			t.Fatalf("missing response or body accepted: %+v", response)
		}
	}
}

func TestSendPreservesPrimaryAndCleanupFailures(t *testing.T) {
	for _, transportFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("transport=%t", transportFails), func(t *testing.T) {
			primary := errors.New("private primary error details")
			cleanup := errors.New("private cleanup error details")
			body := &testBody{reader: failedReader{err: primary}, closeErr: cleanup}
			client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
				response := testResponse(429, body)
				response.Header.Set("Retry-After", "3")
				if transportFails {
					return response, primary
				}
				return response, nil
			}))
			err := client.Send(context.Background(), "C123", alerts.Alert{Body: "body"})
			if !errors.Is(err, primary) || !errors.Is(err, cleanup) || body.closed != 1 {
				t.Fatalf("error=%v body closes=%d; want both causes and closed body", err, body.closed)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatal("joined error exposed private cause details")
			}
			if !transportFails {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 || apiErr.RetryAfter != 3*time.Second {
					t.Fatalf("lost HTTP failure and retry delay: %v", err)
				}
			}
		})
	}
}

func TestSendPreservesTransportAndContextErrors(t *testing.T) {
	for _, cause := range []error{errors.New("transport failed"), context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			calls := 0
			client := newTestClient(t, testDoer(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, cause
			}))
			err := client.Send(context.Background(), "C123", alerts.Alert{Body: "body"})
			if !errors.Is(err, cause) || calls != 1 {
				t.Fatalf("requests=%d error=%v; want original cause and one attempt", calls, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := newTestClient(t, testDoer(func(r *http.Request) (*http.Response, error) {
		if r.Context() != ctx {
			t.Error("request did not preserve caller context")
		}
		return nil, r.Context().Err()
	}))
	if err := client.Send(ctx, "C123", alerts.Alert{Body: "body"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context.Canceled", err)
	}
}

func TestSendErrorsDoNotExposeSensitiveValues(t *testing.T) {
	const token = "private-bot-credential"
	const message = "private trading strategy and account detail"
	const recipient = "private-recipient-value"
	for _, tt := range []struct {
		name   string
		status int
		body   string
		cause  error
	}{
		{"server body", 500, token + message + recipient, nil},
		{"malformed body", 200, token + message + recipient, nil},
		{"malicious API code", 200, `{"ok":false,"error":"` + token + `"}`, nil},
		{"transport details", 0, "", &url.Error{Op: "Post", URL: "https://example.test/" + token, Err: errors.New(message + recipient)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(token, WithHTTPClient(testDoer(func(*http.Request) (*http.Response, error) {
				if tt.cause != nil {
					return nil, tt.cause
				}
				return testResponse(tt.status, io.NopCloser(strings.NewReader(tt.body))), nil
			})))
			if err != nil {
				t.Fatal(err)
			}
			err = client.Send(context.Background(), recipient, alerts.Alert{Body: message})
			if err == nil {
				t.Fatal("expected failure")
			}
			for _, sensitive := range []string{token, message, recipient} {
				if strings.Contains(err.Error(), sensitive) {
					t.Errorf("error exposed sensitive input: %s", err)
				}
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Error("redaction discarded underlying transport cause")
			}
		})
	}
}

func TestDefaultHTTPClientRejectsRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			body := &testBody{reader: strings.NewReader("redirect")}
			httpClient := defaultHTTPClient()
			httpClient.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					t.Error("redirect destination contacted")
					return testResponse(200, io.NopCloser(strings.NewReader(`{"ok":true}`))), nil
				}
				response := testResponse(status, body)
				response.Header.Set("Location", "https://slack.com/redirected")
				response.Request = r
				return response, nil
			})
			client := newTestClient(t, httpClient)
			if err := client.Send(context.Background(), "C123", alerts.Alert{Body: "body"}); err == nil {
				t.Fatal("redirect accepted")
			}
			if calls != 1 || body.closed < 1 {
				t.Fatalf("requests=%d body closes=%d", calls, body.closed)
			}
		})
	}
}

func TestSendConcurrentLocalHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-bot-token" || r.URL.Path != "/api/chat.postMessage" {
			t.Error("wrong authentication or endpoint")
		}
		var payload struct{ Channel, Text string }
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload.Channel != payload.Text {
			t.Errorf("concurrent requests mixed recipients: %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"ok":true}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := newTestClient(t, localHTTPClient(t, server))
	const count = 20
	var workers sync.WaitGroup
	for i := range count {
		workers.Go(func() {
			recipient := fmt.Sprintf("C%d", i)
			if err := client.Send(context.Background(), recipient, alerts.Alert{Body: recipient}); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if calls.Load() != count {
		t.Fatalf("requests=%d, want %d", calls.Load(), count)
	}
}

func localHTTPClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	transport := client.Transport
	client.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		request := r.Clone(r.Context())
		request.URL.Scheme = target.Scheme
		request.URL.Host = target.Host
		request.Host = target.Host
		return transport.RoundTrip(request)
	})
	return client
}

func TestSendCancelsInFlightHTTP(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	client := newTestClient(t, localHTTPClient(t, server))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- client.Send(ctx, "C123", alerts.Alert{Body: "body"}) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not reach local server")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return after cancellation")
	}
}
