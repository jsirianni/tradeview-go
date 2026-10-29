package tradeview

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
)

func TestStreamHandshakeContract(t *testing.T) {
	contract := loadFixtureContract(t)
	endpoints := 0
	for _, fixture := range _operationFixtures {
		entry := contract.manifest[fixture.id]
		if !entry.Streaming {
			continue
		}
		endpoints++
		for _, scenario := range []struct {
			name, media string
			status      int
			valid       bool
		}{
			{"JSON", "application/json", 200, true},
			{"JSON parameters", `Application/JSON; charset="utf-8"`, 200, true},
			{"JSON suffix", "application/vnd.broker+json; version=1", 200, true},
			{"NDJSON", "application/x-ndjson", 200, true},
			{"NDJSON parameters", "Application/X-NDJSON; charset=utf-8", 200, true},
			{"NDJSON alias", "application/ndjson", 200, true},
			{"created", "application/json", 201, false},
			{"accepted", "application/json", 202, false},
			{"no content", "application/json", 204, false},
			{"partial content", "application/json", 206, false},
			{"missing media", "", 200, false},
			{"malformed parameters", "application/json; charset=", 200, false},
			{"text", "text/plain", 200, false},
			{"event stream", "text/event-stream", 200, false},
			{"JSON sequence", "application/json-seq", 200, false},
			{"invalid suffix", "application/+json", 200, false},
			{"non-application JSON", "text/vendor+json", 200, false},
		} {
			t.Run(fixture.id+"/"+scenario.name, func(t *testing.T) {
				payload := "{\"s\":\"ok\"}\n"
				if fixture.id == "streaming" {
					payload = "{\"heartbeat\":0}\n"
				}
				body := &streamObservedBody{Reader: strings.NewReader(payload)}
				doer := finiteResponseDoer(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: scenario.status, Header: http.Header{"Content-Type": {scenario.media}}, Body: body}, nil
				})
				telemetry := newTelemetryFixture(t)
				var output bytes.Buffer
				client, err := NewClient("https://example.invalid", WithHTTPClient(doer), telemetry.option, WithLogger(slog.New(slog.NewJSONHandler(&output, nil))))
				if err != nil {
					t.Fatal(err)
				}
				query, _ := contract.queryFixture(fixture.id, contract.operations[fixture.id])
				result := invokeFixture(t, reflect.ValueOf(client).MethodByName(entry.TypedMethod), t.Context(), fixture.path, query, nil)
				category, frames := "protocol", int64(0)
				if scenario.valid {
					if !result[1].IsNil() {
						t.Fatal(result[1].Interface())
					}
					stream := fixtureValue[readStream](t, result[0].Interface())
					defer closeTestBody(t, stream)
					if body.closed.Load() != 0 || len(telemetry.spans.Ended()) != 0 || output.Len() != 0 {
						t.Fatal("valid stream completed during handshake")
					}
					if !stream.Next() || stream.Next() || stream.Err() != nil {
						t.Fatalf("valid stream failed: %v", stream.Err())
					}
					if err := stream.Close(); err != nil {
						t.Fatal(err)
					}
					category, frames = "", 1
				} else {
					if !result[0].IsNil() || result[1].IsNil() {
						t.Fatal("invalid handshake returned a stream or no error")
					}
					var protocolErr *ProtocolError
					err := fixtureValue[error](t, result[1].Interface())
					if !errors.As(err, &protocolErr) || protocolErr.StatusCode != scenario.status || protocolErr.ContentType != scenario.media || string(protocolErr.Body) != payload {
						t.Fatalf("incorrect protocol diagnostics: %#v", err)
					}
				}
				if body.closed.Load() != 1 {
					t.Fatalf("body closed %d times", body.closed.Load())
				}
				records := logRecords(t, output.Bytes())
				if len(records) != 1 || records[0]["operation"] != fixture.id {
					t.Fatalf("completion logs: %s", output.String())
				}
				if category != "" && (records[0]["error_type"] != category || records[0]["level"] != "ERROR") {
					t.Fatalf("invalid handshake not logged as failure: %v", records[0])
				}
				spans := telemetry.spans.Ended()
				if len(spans) != 1 || spanAttribute(spans[0], "error.type") != category {
					t.Fatalf("completion spans: %v", spans)
				}
				if category != "" && spans[0].Status().Code != codes.Error {
					t.Fatal("invalid handshake did not mark span failed")
				}
				if telemetry.sum(t, "tradeview.client.operations") != 1 || telemetry.sum(t, "tradeview.client.active_operations") != 0 || telemetry.sum(t, "tradeview.client.stream.frames") != frames {
					t.Fatal("incorrect operation/frame accounting")
				}
			})
		}
	}
	if endpoints != 9 {
		t.Fatalf("tested %d stream openers, want 9", endpoints)
	}
}

func TestStreamHandshakeDiagnostics(t *testing.T) {
	const limit = 16
	cause := errors.New("diagnostic read interrupted")
	for _, status := range []int{200, 206, 401, 503} {
		for _, readFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("status=%d/readFailure=%t", status, readFailure), func(t *testing.T) {
				payload := []byte(strings.Repeat("x", limit+32))
				var reader io.Reader = bytes.NewReader(payload)
				want := string(payload[:limit])
				if readFailure {
					reader = io.MultiReader(strings.NewReader("partial"), finiteFailingReader{err: cause})
					want = "partial"
				}
				body := &streamObservedBody{Reader: reader}
				client, err := NewClient("https://example.invalid", WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: http.Header{"Content-Type": {"text/plain"}}, Body: body}, nil
				})))
				if err != nil {
					t.Fatal(err)
				}
				stream, err := client.OpenStreaming(t.Context(), WithMaxFrameBytes(limit))
				if stream != nil || err == nil || body.closed.Load() != 1 {
					t.Fatalf("stream=%v err=%v closes=%d", stream, err, body.closed.Load())
				}
				if errors.Is(err, cause) != readFailure {
					t.Fatalf("read cause not preserved: %v", err)
				}
				var diagnostic []byte
				if status < 300 {
					var protocolErr *ProtocolError
					if !errors.As(err, &protocolErr) || protocolErr.ContentType != "text/plain" || protocolErr.StatusCode != status {
						t.Fatalf("missing ProtocolError: %v", err)
					}
					diagnostic = protocolErr.Body
				} else if !readFailure {
					var httpErr *HTTPError
					if !errors.As(err, &httpErr) || httpErr.StatusCode != status || httpErr.Status != http.StatusText(status) {
						t.Fatalf("missing HTTPError: %v", err)
					}
					diagnostic = httpErr.Body
				} else {
					return // Existing non-2xx read failures retain their transport cause.
				}
				if string(diagnostic) != want {
					t.Fatalf("diagnostic=%q, want %q", diagnostic, want)
				}
				payload[0] = '!'
				if string(diagnostic) != want {
					t.Fatal("diagnostic aliases input storage")
				}
			})
		}
	}
}
