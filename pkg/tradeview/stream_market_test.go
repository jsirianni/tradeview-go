package tradeview

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
)

func TestPublicMarketRequiredFields(t *testing.T) {
	contract := loadFixtureContract(t)
	schemas := fixtureValue[map[string]any](t, fixtureValue[map[string]any](t, contract.source["components"])["schemas"])
	for _, fixture := range []struct {
		schema, frame string
		kind          StreamMessageKind
		want          any
	}{
		{"StreamingQuoteResponse", `{"f":"q","id":"","t":0,"ap":0,"as":0,"bp":0,"bs":0}`, StreamQuote, &StreamingQuoteResponse{F: "q"}},
		{"StreamingTradeResponse", `{"f":"t","id":"","p":0,"s":0,"t":0}`, StreamTrade, &StreamingTradeResponse{F: "t"}},
		{"StreamingDailyBarResponse", `{"f":"d","id":"","o":0,"h":0,"l":0,"c":0,"v":0,"t":0}`, StreamDailyBar, &StreamingDailyBarResponse{F: "d"}},
		{"StreamingAskResponse(deprecated)", `{"f":"a","id":"","p":0,"s":0,"t":0}`, StreamAsk, &StreamingAskResponseDeprecated{F: new("a")}},
		{"StreamingBidResponse(deprecated)", `{"f":"b","id":"","p":0,"s":0,"t":0}`, StreamBid, &StreamingBidResponseDeprecated{F: new("b")}},
		{"StreamingHeartbeatResponse", `{"heartbeat":0}`, StreamHeartbeat, &StreamingHeartbeatResponse{Heartbeat: new(0)}},
	} {
		t.Run(fixture.schema, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(fixture.frame), &fields); err != nil {
				t.Fatal(err)
			}
			fields["extension"] = json.RawMessage(`{"unknown":null}`)
			valid, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			event, err := decodeMarketEvent(valid)
			if err != nil || event.Kind != fixture.kind || string(event.Raw) != string(valid) {
				t.Fatalf("explicit zero fields rejected: %+v / %v", event, err)
			}
			var payload any
			switch event.Kind {
			case StreamQuote:
				payload = event.Quote
			case StreamTrade:
				payload = event.Trade
			case StreamDailyBar:
				payload = event.DailyBar
			case StreamAsk:
				payload = event.Ask
			case StreamBid:
				payload = event.Bid
			case StreamHeartbeat:
				payload = event.Heartbeat
			}
			if !reflect.DeepEqual(payload, fixture.want) {
				t.Fatalf("decoded zeros = %#v, want %#v", payload, fixture.want)
			}
			assertMarketFieldStream(t, string(valid), "", false)
			// Derive mandatory fields from the pinned schema, including allOf
			// references, so this matrix detects a newly required model field.
			required := requiredMarketSchemaFields(t, contract, fixtureValue[map[string]any](t, schemas[fixture.schema]))
			// Legacy f and heartbeat are optional in the schema, but are needed
			// to select their wire variant. Keep that existing dispatch contract.
			selector := "f"
			if fixture.kind == StreamHeartbeat {
				selector = "heartbeat"
			}
			if !slices.Contains(required, selector) {
				required = append(required, selector)
			}
			for _, field := range required {
				if _, exists := fields[field]; !exists {
					t.Fatalf("fixture missing schema-required %q", field)
				}
				for _, mutation := range []string{"omitted", "null", "wrong type"} {
					t.Run(field+"/"+mutation, func(t *testing.T) {
						invalid := maps.Clone(fields)
						switch mutation {
						case "omitted":
							delete(invalid, field)
						case "null":
							invalid[field] = json.RawMessage("null")
						case "wrong type":
							invalid[field] = json.RawMessage("true")
						}
						frame, err := json.Marshal(invalid)
						if err != nil {
							t.Fatal(err)
						}
						assertMarketFieldStream(t, string(valid), string(frame), mutation == "wrong type")
					})
				}
			}
		})
	}
}

func requiredMarketSchemaFields(t *testing.T, contract fixtureContract, schema map[string]any) []string {
	t.Helper()
	schema = contract.resolve(schema)
	var result []string
	if required, ok := schema["required"]; ok {
		for _, field := range fixtureValue[[]any](t, required) {
			result = append(result, fixtureValue[string](t, field))
		}
	}
	if allOf, ok := schema["allOf"]; ok {
		for _, part := range fixtureValue[[]any](t, allOf) {
			result = append(result, requiredMarketSchemaFields(t, contract, fixtureValue[map[string]any](t, part))...)
		}
	}
	return result
}

func assertMarketFieldStream(t *testing.T, valid, invalid string, typeError bool) {
	t.Helper()
	payload := valid + "\n"
	if invalid != "" {
		payload += invalid + "\n" + valid + "\n"
	}
	body := &streamObservedBody{Reader: strings.NewReader(payload)}
	telemetry := newTelemetryFixture(t)
	var output bytes.Buffer
	client, err := NewClient("https://example.invalid", telemetry.option, WithLogger(slog.New(slog.NewJSONHandler(&output, nil))), WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.OpenStreaming(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if !stream.Next() || stream.Err() != nil || string(stream.Value().Raw) != valid {
		t.Fatalf("valid zero frame: %+v / %v", stream.Value(), stream.Err())
	}
	previous := stream.Value()
	if stream.Next() {
		t.Fatalf("invalid frame yielded: %s", invalid)
	}
	category := ""
	if invalid != "" {
		category = "client"
		if stream.Err() == nil || !strings.Contains(stream.Err().Error(), "decode stream frame") {
			t.Fatalf("missing decoding error for %s: %v", invalid, stream.Err())
		}
		var decodeErr *json.UnmarshalTypeError
		if typeError && !errors.As(stream.Err(), &decodeErr) {
			t.Fatalf("decoding cause not preserved: %v", stream.Err())
		}
	} else if stream.Err() != nil {
		t.Fatalf("valid zero frame failed: %v", stream.Err())
	}
	if !reflect.DeepEqual(stream.Value(), previous) || stream.Next() {
		t.Fatal("last valid frame changed or iteration continued after termination")
	}
	if err := stream.Close(); err != nil || body.closed.Load() != 1 {
		t.Fatalf("close: %v / count %d", err, body.closed.Load())
	}
	records := logRecords(t, output.Bytes())
	if len(records) != 1 || category != "" && records[0]["error_type"] != category {
		t.Fatalf("completion logs: %s", output.String())
	}
	spans := telemetry.spans.Ended()
	if len(spans) != 1 || spanAttribute(spans[0], "error.type") != category || category != "" && spans[0].Status().Code != codes.Error {
		t.Fatalf("completion spans: %v", spans)
	}
	if telemetry.sum(t, "tradeview.client.operations") != 1 || telemetry.sum(t, "tradeview.client.active_operations") != 0 || telemetry.sum(t, "tradeview.client.stream.frames") != 1 {
		t.Fatal("incorrect operation/frame accounting")
	}
}

func TestMarketRequiredFieldMatching(t *testing.T) {
	for _, scenario := range []struct {
		fields string
		valid  bool
	}{
		{`"id":"EURUSD"`, true},
		{`"ID":"EURUSD"`, true},
		{`"\u0069d":"EURUSD"`, true},
		{`"id":"EURUSD","id":null`, false},
		{`"id":null,"id":"EURUSD"`, true},
		{`"id":"EURUSD","ID":null`, false},
		{`"ID":null,"id":"EURUSD"`, true},
		{`"id":true,"id":"EURUSD"`, false},
		{`"extension":{"id":"EURUSD"}`, false},
	} {
		frame := `{"f":"q","t":0,"ap":0,"as":0,"bp":0,"bs":0,` + scenario.fields + `}`
		event, err := decodeMarketEvent([]byte(frame))
		if (err == nil) != scenario.valid || scenario.valid && (event.Quote.Id != "EURUSD" || string(event.Raw) != frame) {
			t.Fatalf("field matching for %s: %+v / %v", frame, event, err)
		}
	}
	for _, frame := range []string{`{"heartbeat":0,"HEARTBEAT":null}`, `{"heartbeat":1.5}`, `{"heartbeat":[]}`} {
		if _, err := decodeMarketEvent([]byte(frame)); err == nil {
			t.Fatalf("invalid heartbeat accepted: %s", frame)
		}
	}
}
