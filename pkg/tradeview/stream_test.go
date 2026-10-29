package tradeview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type streamObservedBody struct {
	io.Reader
	closed  atomic.Int32
	closeFn func() error
}

func (body *streamObservedBody) Close() error {
	body.closed.Add(1)
	if body.closeFn != nil {
		return body.closeFn()
	}
	return nil
}

type chunksReader struct{ chunks []string }

func (reader *chunksReader) Read(dst []byte) (int, error) {
	if len(reader.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(dst, reader.chunks[0])
	reader.chunks[0] = reader.chunks[0][n:]
	if reader.chunks[0] == "" {
		reader.chunks = reader.chunks[1:]
	}
	return n, nil
}

func accountTestStream(t *testing.T, ctx context.Context, body *streamObservedBody, options ...StreamOption) *Stream[AccountEvent[json.RawMessage]] {
	t.Helper()
	config, err := configureStream(options)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := openHTTPStream(ctx, &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": {"application/json"}}, Body: body}, nil, config, decodeAccountEvent[json.RawMessage])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestBody(t, stream) })
	return stream
}

func TestStreamChunkedMessagesAndError(t *testing.T) {
	body := &streamObservedBody{Reader: &chunksReader{chunks: []string{"[", "{\"id\":\"1\",\"custom\":7}]\n{\"s\":", "\"ok\"}\n{\"s\":\"error\",\"errmsg\":\"expired\",\"errtype\":\"invalid_token\"}\n[]\n"}}}
	stream := accountTestStream(t, context.Background(), body)
	if !stream.Next() {
		t.Fatalf("snapshot: %v", stream.Err())
	}
	snapshot := stream.Value()
	if snapshot.Kind != StreamData || snapshot.Data == nil || !strings.Contains(string(snapshot.Raw), `"custom":7`) {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if !stream.Next() || stream.Value().Ping == nil || stream.Value().Kind != StreamPing {
		t.Fatalf("ping: %+v / %v", stream.Value(), stream.Err())
	}
	if !stream.Next() || stream.Value().Error == nil || stream.Value().Kind != StreamError {
		t.Fatalf("error frame: %+v / %v", stream.Value(), stream.Err())
	}
	if stream.Next() {
		t.Fatal("read after terminal API error")
	}
	var apiErr *APIError
	if !errors.As(stream.Err(), &apiErr) || apiErr.Code != "invalid_token" || apiErr.Message != "expired" || apiErr.StatusCode != 200 {
		t.Fatalf("error = %#v", stream.Err())
	}
	if body.closed.Load() != 1 {
		t.Fatalf("close count = %d", body.closed.Load())
	}
	if err := stream.Close(); err != nil || body.closed.Load() != 1 {
		t.Fatalf("second close = %v, count %d", err, body.closed.Load())
	}
}

func TestStreamEOFAndFrameLimits(t *testing.T) {
	tests := []struct {
		name, payload string
		max           int
		wantFrames    int
		wantErr       bool
	}{
		{"empty", "", 64, 0, false},
		{"multiple and blank lines", "\n[]\r\n[]\n", 64, 2, false},
		{"final frame without newline", "[]", 64, 1, false},
		{"exact limit", "[]\n", 2, 1, false},
		{"oversize", "[123456]\n", 4, 0, true},
		{"oversize whitespace", "[] \n", 2, 0, true},
		{"malformed", "{broken}\n", 64, 0, true},
		{"truncated", "{\"s\":", 64, 0, true},
		{"null", "null\n", 64, 0, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &streamObservedBody{Reader: strings.NewReader(test.payload)}
			stream := accountTestStream(t, context.Background(), body, WithMaxFrameBytes(test.max))
			frames := 0
			for stream.Next() {
				frames++
			}
			if frames != test.wantFrames || (stream.Err() != nil) != test.wantErr {
				t.Fatalf("frames=%d error=%v", frames, stream.Err())
			}
			if body.closed.Load() != 1 {
				t.Fatalf("close count = %d", body.closed.Load())
			}
		})
	}
}

func TestStreamCancelAndCloseUnblockNext(t *testing.T) {
	for _, cancelContext := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelContext), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader, writer := io.Pipe()
			defer closeTestBody(t, writer)
			body := &streamObservedBody{Reader: reader, closeFn: reader.Close}
			stream := accountTestStream(t, ctx, body)
			next := make(chan bool, 1)
			go func() { next <- stream.Next() }()
			if cancelContext {
				cancel()
			} else if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case value := <-next:
				if value {
					t.Fatal("unexpected frame")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Next remained blocked")
			}
			if cancelContext && !errors.Is(stream.Err(), context.Canceled) {
				t.Fatalf("error=%v", stream.Err())
			}
			if !cancelContext && stream.Err() != nil {
				t.Fatalf("explicit Close error=%v", stream.Err())
			}
			if body.closed.Load() != 1 {
				t.Fatalf("close count=%d", body.closed.Load())
			}
		})
	}
}

func TestStreamCanceledContextBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := &streamObservedBody{Reader: strings.NewReader("[]\n")}
	stream := accountTestStream(t, ctx, body)
	deadline := time.After(5 * time.Second)
	for stream.Err() == nil {
		select {
		case <-deadline:
			t.Fatal("cancellation did not close stream")
		default:
		}
		time.Sleep(time.Millisecond)
	}
	if !errors.Is(stream.Err(), context.Canceled) {
		t.Fatalf("error=%v", stream.Err())
	}
}

func TestStreamHandshakeErrors(t *testing.T) {
	config, err := configureStream(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{401, 503} {
		body := &streamObservedBody{Reader: strings.NewReader(`{"s":"error","errmsg":"denied","errtype":"unauthorized"}`)}
		stream, err := openHTTPStream(context.Background(), &http.Response{StatusCode: status, Status: http.StatusText(status), Body: body}, nil, config, decodeAccountEvent[json.RawMessage])
		if stream != nil || err == nil || body.closed.Load() != 1 {
			t.Fatalf("status=%d stream=%v err=%v closes=%d", status, stream, err, body.closed.Load())
		}
	}
	body := &streamObservedBody{Reader: strings.NewReader("unused")}
	wanted := errors.New("request failed")
	_, err = openHTTPStream(context.Background(), &http.Response{Body: body}, wanted, config, decodeAccountEvent[json.RawMessage])
	if !errors.Is(err, wanted) || body.closed.Load() != 1 {
		t.Fatalf("err=%v closes=%d", err, body.closed.Load())
	}
	if _, err := openHTTPStream[AccountEvent[json.RawMessage]](context.Background(), nil, nil, config, decodeAccountEvent[json.RawMessage]); err == nil {
		t.Fatal("expected empty response error")
	}
}

func TestStreamCleanupErrors(t *testing.T) {
	closeErr := errors.New("body close failed")
	requestErr := errors.New("request failed")
	readErr := errors.New("read failed")
	for _, scenario := range []struct {
		name       string
		status     int
		media      string
		requestErr error
		reader     io.Reader
	}{
		{"request", 200, "application/json", requestErr, strings.NewReader("")},
		{"HTTP", 503, "text/plain", nil, strings.NewReader("unavailable")},
		{"read", 503, "text/plain", nil, finiteFailingReader{err: readErr}},
		{"status", 204, "application/json", nil, strings.NewReader("")},
		{"media", 200, "text/plain", nil, strings.NewReader("")},
		{"EOF", 200, "application/json", nil, strings.NewReader("")},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body := &streamObservedBody{Reader: scenario.reader, closeFn: func() error { return closeErr }}
			config, err := configureStream(nil)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := openHTTPStream(t.Context(), &http.Response{StatusCode: scenario.status, Header: http.Header{"Content-Type": {scenario.media}}, Body: body}, scenario.requestErr, config, decodeAccountEvent[json.RawMessage])
			if stream != nil {
				if stream.Next() || stream.Err() != nil {
					t.Fatalf("empty stream: %v", stream.Err())
				}
				err = stream.Close()
				if !errors.Is(stream.Close(), closeErr) {
					t.Fatal("repeated close lost error")
				}
			}
			if !errors.Is(err, closeErr) || body.closed.Load() != 1 {
				t.Fatalf("err=%v closes=%d", err, body.closed.Load())
			}
			if scenario.requestErr != nil && !errors.Is(err, requestErr) {
				t.Fatal("lost request failure")
			}
			if scenario.name == "read" && !errors.Is(err, readErr) {
				t.Fatal("lost read failure")
			}
		})
	}
}

func TestStreamOptionValidation(t *testing.T) {
	for _, option := range []StreamOption{nil, WithMaxFrameBytes(0), WithMaxFrameBytes(-1), WithMaxFrameBytes(int(^uint(0) >> 1)), WithStreamRequestEditor(nil)} {
		if _, err := configureStream([]StreamOption{option}); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
}

func TestMarketEventVariants(t *testing.T) {
	fixtures := []struct {
		kind    StreamMessageKind
		frame   string
		present func(MarketEvent) bool
	}{
		{StreamQuote, `{"f":"q","id":"EURUSD","t":1,"ap":1.2,"as":100,"bp":1.1,"bs":90}`, func(e MarketEvent) bool { return e.Quote != nil }},
		{StreamTrade, `{"f":"t","id":"EURUSD","p":1.2,"s":10,"t":1}`, func(e MarketEvent) bool { return e.Trade != nil }},
		{StreamDailyBar, `{"f":"d","id":"EURUSD","o":1,"h":2,"l":0.5,"c":1.5,"v":5,"t":1}`, func(e MarketEvent) bool { return e.DailyBar != nil }},
		{StreamHeartbeat, `{"heartbeat":1609255644}`, func(e MarketEvent) bool { return e.Heartbeat != nil }},
		{StreamAsk, `{"f":"a","id":"EURUSD","p":1.2,"s":10,"t":1}`, func(e MarketEvent) bool { return e.Ask != nil }},
		{StreamBid, `{"f":"b","id":"EURUSD","p":1.1,"s":10,"t":1}`, func(e MarketEvent) bool { return e.Bid != nil }},
		{StreamError, `{"s":"error","errmsg":"failed"}`, func(e MarketEvent) bool { return e.Error != nil }},
	}
	for _, fixture := range fixtures {
		t.Run(string(fixture.kind), func(t *testing.T) {
			event, err := decodeMarketEvent([]byte(fixture.frame))
			if err != nil || event.Kind != fixture.kind || !fixture.present(event) || string(event.Raw) != fixture.frame {
				t.Fatalf("event=%+v err=%v", event, err)
			}
		})
	}
	for _, frame := range []string{`null`, `[]`, `{}`, `{"f":"unknown"}`, `{"heartbeat":null}`, `{"heartbeat":"bad"}`, `{broken}`} {
		if _, err := decodeMarketEvent([]byte(frame)); err == nil {
			t.Fatalf("accepted malformed frame %s", frame)
		}
	}
}

type readStream interface {
	Next() bool
	Err() error
	Close() error
}

func TestTypedStreamEndpoints(t *testing.T) {
	ctx := context.Background()
	editor := WithStreamRequestEditor(func(_ context.Context, request *http.Request) error {
		request.Header.Set("X-Stream-Test", "edited")
		return nil
	})
	tests := []struct {
		name, path, frame string
		query             map[string]string
		open              func(*Client) (readStream, error)
	}{
		{"state", "/accounts/a1/stream/state", `{"balance":1,"unrealizedPl":0}`, map[string]string{"locale": "en"}, func(c *Client) (readStream, error) {
			return c.OpenStreamState(ctx, "a1", &StreamStateParams{Locale: En}, editor)
		}},
		{"orders", "/accounts/a1/stream/orders", `[]`, map[string]string{"locale": "en"}, func(c *Client) (readStream, error) {
			return c.OpenStreamOrders(ctx, "a1", &StreamOrdersParams{Locale: En}, editor)
		}},
		{"positions", "/accounts/a1/stream/positions", `[]`, map[string]string{"locale": "en"}, func(c *Client) (readStream, error) {
			return c.OpenStreamPositions(ctx, "a1", &StreamPositionsParams{Locale: En}, editor)
		}},
		{"net positions", "/accounts/a1/stream/netPositions", `[]`, map[string]string{"locale": "en"}, func(c *Client) (readStream, error) {
			return c.OpenStreamNetPositions(ctx, "a1", &StreamNetPositionsParams{Locale: En}, editor)
		}},
		{"individual positions", "/accounts/a1/stream/individualPositions", `[]`, map[string]string{"locale": "en"}, func(c *Client) (readStream, error) {
			return c.OpenStreamIndividualPositions(ctx, "a1", &StreamIndividualPositionsParams{Locale: En}, editor)
		}},
		{"notifications", "/accounts/a1/stream/notifications", `{"key":"margin","severity":"error"}`, map[string]string{"locale": "en"}, func(c *Client) (readStream, error) {
			return c.OpenStreamNotifications(ctx, "a1", &StreamNotificationsParams{Locale: En}, editor)
		}},
		{"quotes", "/stream/quotes", `[{"n":"EURUSD","v":{"bid":1.1,"ask":1.2}}]`, map[string]string{"locale": "en", "accountId": "a1", "symbols": "EURUSD"}, func(c *Client) (readStream, error) {
			return c.OpenStreamQuotes(ctx, &StreamQuotesParams{Locale: En, AccountId: "a1", Symbols: "EURUSD"}, editor)
		}},
		{"depth", "/stream/depth", `{"asks":[[1.2,2]],"bids":[[1.1,2]]}`, map[string]string{"locale": "en", "accountId": "a1", "symbol": "EURUSD"}, func(c *Client) (readStream, error) {
			return c.OpenStreamDepth(ctx, &StreamDepthParams{Locale: En, AccountId: "a1", Symbol: "EURUSD"}, editor)
		}},
		{"market data", "/streaming", `{"heartbeat":1}`, nil, func(c *Client) (readStream, error) { return c.OpenStreaming(ctx, editor) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.Method != http.MethodGet || request.URL.Path != "/api"+test.path {
					t.Errorf("request=%s %s", request.Method, request.URL.String())
				}
				for key, value := range test.query {
					if request.URL.Query().Get(key) != value {
						t.Errorf("query %s=%q, want %q", key, request.URL.Query().Get(key), value)
					}
				}
				if len(request.URL.Query()) != len(test.query) {
					t.Errorf("query=%v", request.URL.Query())
				}
				if request.Header.Get("Authorization") != "Bearer token" || request.Header.Get("X-Stream-Test") != "edited" {
					t.Error("missing authentication/request editor")
				}
				writer.Header().Set("Content-Type", "application/json")
				_, err := fmt.Fprintln(writer, test.frame)
				checkTestError(t, err)
			}))
			defer server.Close()
			client, err := NewClient(server.URL+"/api", WithBearerToken("token"))
			if err != nil {
				t.Fatal(err)
			}
			stream, err := test.open(client)
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestBody(t, stream)
			if !stream.Next() {
				t.Fatalf("first frame: %v", stream.Err())
			}
			if stream.Next() || stream.Err() != nil {
				t.Fatalf("EOF: %v", stream.Err())
			}
			if requests.Load() != 1 {
				t.Fatalf("requests=%d", requests.Load())
			}
		})
	}
}

func TestStreamOptionsRejectBeforeRequest(t *testing.T) {
	client, err := NewClient("https://example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.OpenStreaming(context.Background(), WithMaxFrameBytes(0)); err == nil {
		t.Fatal("invalid option accepted")
	}
}
