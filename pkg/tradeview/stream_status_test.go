package tradeview

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicStreamStatusClassification(t *testing.T) {
	for _, scenario := range []struct {
		name, fields string
		kind         StreamMessageKind
		decodeError  bool
	}{
		{"canonical error", `"s":"error"`, StreamError, false},
		{"canonical before folded", `"s":"error","S":"ok"`, StreamError, false},
		{"canonical after folded", `"S":"ok","s":"error"`, StreamError, false},
		{"folded error only", `"S":"error"`, StreamData, false},
		{"folded error after ok", `"s":"ok","S":"error"`, StreamPing, false},
		{"folded error before ok", `"S":"error","s":"ok"`, StreamPing, false},
		{"unicode folded error", `"ſ":"error"`, StreamData, false},
		{"duplicate error last", `"s":"ok","s":"error"`, StreamError, false},
		{"duplicate ok last", `"s":"error","s":"ok"`, StreamPing, false},
		{"duplicate null last", `"s":"error","s":null`, StreamData, false},
		{"duplicate null first", `"s":null,"s":"error"`, StreamError, false},
		{"escaped canonical", `"\u0073":"error"`, StreamError, false},
		{"escaped folded", `"\u0053":"error"`, StreamData, false},
		{"escaped canonical duplicate", `"s":"ok","\u0073":"error"`, StreamError, false},
		{"null status", `"s":null`, StreamData, false},
		{"numeric status", `"s":0`, StreamData, false},
		{"object status", `"s":{}`, StreamData, false},
		{"malformed error message", `"s":"error","errmsg":123`, StreamError, true},
	} {
		for _, market := range []bool{false, true} {
			endpoint := "account"
			if market {
				endpoint = "market"
			}
			t.Run(endpoint+"/"+scenario.name, func(t *testing.T) {
				base := `"balance":0,"unrealizedPl":0`
				first, last := `{"balance":1,"unrealizedPl":0}`, `{"balance":2,"unrealizedPl":0}`
				wantKind := scenario.kind
				if market {
					base = `"f":"q","id":"EURUSD","t":0,"ap":0,"bp":0`
					first, last = `{"heartbeat":1}`, `{"heartbeat":2}`
					if wantKind != StreamError {
						wantKind = StreamQuote
					}
				}
				frame := `{` + base + `,"errmsg":"expired","errtype":"broker_custom","extension":true,` + scenario.fields + `}`
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, first+"\n"+frame+"\n"+last+"\n")
				}))
				defer server.Close()
				var body *streamObservedBody
				doer := finiteResponseDoer(func(request *http.Request) (*http.Response, error) {
					response, err := server.Client().Do(request)
					if err == nil {
						body = &streamObservedBody{Reader: response.Body, closeFn: response.Body.Close}
						response.Body = body
					}
					return response, err
				})
				telemetry := newTelemetryFixture(t)
				var output bytes.Buffer
				client, err := NewClient(server.URL, WithHTTPClient(doer), telemetry.option, WithLogger(slog.New(slog.NewJSONHandler(&output, nil))))
				if err != nil {
					t.Fatal(err)
				}
				if market {
					stream, err := client.OpenStreaming(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					assertPublicStreamStatus(t, stream, func(event MarketEvent) (StreamMessageKind, json.RawMessage, *ErrorMessage) {
						return event.Kind, event.Raw, event.Error
					}, first, frame, last, wantKind, scenario.decodeError)
				} else {
					stream, err := client.OpenStreamState(t.Context(), "account", &StreamStateParams{Locale: En})
					if err != nil {
						t.Fatal(err)
					}
					assertPublicStreamStatus(t, stream, func(event StateEvent) (StreamMessageKind, json.RawMessage, *ErrorMessage) {
						return event.Kind, event.Raw, event.Error
					}, first, frame, last, wantKind, scenario.decodeError)
				}
				if body.closed.Load() != 1 {
					t.Fatalf("body closed %d times", body.closed.Load())
				}
				category, frames := "", int64(3)
				if wantKind == StreamError {
					category, frames = "api", 2
				}
				if scenario.decodeError {
					category, frames = "client", 1
				}
				records := logRecords(t, output.Bytes())
				if len(records) != 1 {
					t.Fatalf("completion logs: %s", output.String())
				}
				gotCategory, hasCategory := records[0]["error_type"].(string)
				if gotCategory != category || hasCategory != (category != "") {
					t.Fatalf("log error type %q, want %q", gotCategory, category)
				}
				spans := telemetry.spans.Ended()
				if len(spans) != 1 || spanAttribute(spans[0], "error.type") != category {
					t.Fatalf("completion spans: %v", spans)
				}
				if telemetry.sum(t, "tradeview.client.operations") != 1 || telemetry.sum(t, "tradeview.client.active_operations") != 0 || telemetry.sum(t, "tradeview.client.stream.frames") != frames {
					t.Fatal("incorrect operation/frame accounting")
				}
			})
		}
	}
}

func assertPublicStreamStatus[T any](t *testing.T, stream *Stream[T], inspect func(T) (StreamMessageKind, json.RawMessage, *ErrorMessage), first, frame, last string, wantKind StreamMessageKind, decodeError bool) {
	t.Helper()
	defer stream.Close()
	if !stream.Next() || stream.Err() != nil {
		t.Fatalf("initial frame: %v", stream.Err())
	}
	wantRaw := frame
	if decodeError {
		if stream.Next() || stream.Err() == nil {
			t.Fatalf("malformed error frame accepted: %v", stream.Err())
		}
		wantRaw = first
	} else {
		if !stream.Next() {
			t.Fatalf("classification frame: %v", stream.Err())
		}
		kind, raw, message := inspect(stream.Value())
		if kind != wantKind || string(raw) != frame {
			t.Fatalf("kind/raw = %s/%s, want %s/%s", kind, raw, wantKind, frame)
		}
		if wantKind == StreamError {
			var apiErr *APIError
			if !errors.As(stream.Err(), &apiErr) || apiErr.StatusCode != http.StatusOK || apiErr.Code != "broker_custom" || apiErr.Message != "expired" || string(apiErr.Body) != frame {
				t.Fatalf("terminal API error: %#v", stream.Err())
			}
			if message == nil || message.Errmsg != apiErr.Message || message.Errtype == nil || string(*message.Errtype) != apiErr.Code {
				t.Fatalf("typed error and terminal error disagree: %+v / %+v", message, apiErr)
			}
			// APIError diagnostics and the retained event own independent bytes.
			apiErr.Body[0] = '['
			if string(raw) != frame {
				t.Fatal("APIError body aliases event Raw")
			}
		} else {
			if stream.Err() != nil || !stream.Next() {
				t.Fatalf("valid stream terminated early: %v", stream.Err())
			}
			wantRaw = last
		}
	}
	if stream.Next() {
		t.Fatal("unexpected frame after terminal state")
	}
	if !decodeError && wantKind != StreamError && stream.Err() != nil {
		t.Fatalf("unclean EOF: %v", stream.Err())
	}
	_, raw, _ := inspect(stream.Value())
	if string(raw) != wantRaw {
		t.Fatalf("last value lost: %s, want %s", raw, wantRaw)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicMarketNumericSizeIsNotStatus(t *testing.T) {
	for _, kind := range []string{"t", "a", "b"} {
		t.Run(kind, func(t *testing.T) {
			frame := `{"f":"` + kind + `","id":"EURUSD","p":0,"s":0,"t":0}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, frame+"\n")
			}))
			defer server.Close()
			client, err := NewClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := client.OpenStreaming(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if !stream.Next() || stream.Value().Kind == StreamError || string(stream.Value().Raw) != frame || stream.Next() || stream.Err() != nil {
				t.Fatalf("numeric size treated as error: %+v / %v", stream.Value(), stream.Err())
			}
		})
	}
}
