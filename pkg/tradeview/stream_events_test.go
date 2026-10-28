package tradeview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestMarketHeaderFastPathMatchesMap(t *testing.T) {
	fixtures := []struct {
		name  string
		frame string
	}{
		{"quote", `{"f":"q","id":"EURUSD","t":1547942400,"ap":1.47245,"as":100,"bp":1.47245,"bs":100}`},
		{"trade", `{"f":"t","id":"EURUSD","p":1.2,"s":10,"t":1}`},
		{"daily bar", `{"f":"d","id":"EURUSD","o":1,"h":2,"l":0.5,"c":1.5,"v":5,"t":1}`},
		{"heartbeat", `{"heartbeat":1609255644}`},
		{"heartbeat zero", `{"heartbeat":0}`},
		{"ask", `{"f":"a","id":"EURUSD","p":1.2,"s":10,"t":1}`},
		{"bid", `{"f":"b","id":"EURUSD","p":1.1,"s":10,"t":1}`},
		{"API error", `{"s":"error","errmsg":"failed","errtype":"custom"}`},
		{"error before heartbeat", `{"s":"error","errmsg":"failed","heartbeat":null,"f":"q"}`},
		{"heartbeat before discriminator", `{"heartbeat":1,"f":"unknown"}`},
		{"numeric status", `{"s":10,"f":"t"}`},
		{"null status", `{"s":null,"f":"q"}`},
		{"heartbeat null", `{"heartbeat":null,"f":"q"}`},
		{"heartbeat string", `{"heartbeat":"bad"}`},
		{"heartbeat fraction", `{"heartbeat":1.5}`},
		{"discriminator missing", `{}`},
		{"discriminator null", `{"f":null}`},
		{"discriminator number", `{"f":1}`},
		{"discriminator object", `{"f":{}}`},
		{"discriminator unknown", `{"f":"unknown"}`},
		{"uppercase discriminator only", `{"F":"q"}`},
		{"discriminator case collision", `{"f":"q","F":"t"}`},
		{"discriminator reverse case collision", `{"F":"t","f":"q"}`},
		{"uppercase status only", `{"S":"error","f":"q"}`},
		{"status case collision", `{"s":"error","S":"ignored","errmsg":"failed"}`},
		{"status reverse case collision", `{"S":"ignored","s":"error","errmsg":"failed"}`},
		{"uppercase heartbeat only", `{"HEARTBEAT":1,"f":"q"}`},
		{"mixed heartbeat only", `{"HeartBeat":1,"f":"q"}`},
		{"heartbeat case collision", `{"heartbeat":1,"HeartBeat":2,"f":"q"}`},
		{"heartbeat reverse case collision", `{"HeartBeat":2,"heartbeat":1,"f":"q"}`},
		{"duplicate discriminator", `{"f":"t","f":"q"}`},
		{"duplicate discriminator null last", `{"f":"q","f":null}`},
		{"duplicate discriminator null first", `{"f":null,"f":"q"}`},
		{"duplicate status null last", `{"s":"error","s":null,"f":"q"}`},
		{"duplicate status null first", `{"s":null,"s":"error","errmsg":"failed"}`},
		{"duplicate heartbeat null last", `{"heartbeat":1,"heartbeat":null,"f":"q"}`},
		{"duplicate heartbeat null first", `{"heartbeat":null,"heartbeat":1,"f":"q"}`},
		{"escaped canonical key", `{"\u0066":"q"}`},
		{"escaped uppercase collision", `{"f":"q","\u0046":"t"}`},
		{"escaped uppercase reverse collision", `{"\u0046":"t","f":"q"}`},
		{"escaped heartbeat", `{"\u0068eartbeat":1}`},
		{"unicode folded status", `{"ſ":"error","f":"q"}`},
		{"unicode symbol", `{"f":"q","id":"€USD"}`},
		{"escaped value", `{"f":"q","id":"EUR\\USD"}`},
		{"quoted candidate value", `{"f":"q","id":"F"}`},
		{"nested extensions", `{"f":"q","extra":{"F":"t","heartbeat":null},"array":[{"S":"error"}]}`},
		{"ordinary extensions", `{"f":"q","extra":{"value":1},"array":[1,2,3]}`},
		{"empty", ``},
		{"whitespace", " \n\t"},
		{"null", `null`},
		{"padded null", " \nnull\t"},
		{"array", `[]`},
		{"string", `"q"`},
		{"number", `42`},
		{"boolean", `true`},
		{"malformed", `{broken}`},
		{"trailing JSON", `{"f":"q"}{}`},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			frame := []byte(fixture.frame)
			want, wantErr := decodeMarketEventMapBaseline(frame)
			got, gotErr := decodeMarketEvent(frame)
			if (gotErr != nil) != (wantErr != nil) {
				t.Fatalf("error presence changed: optimized=%v map=%v", gotErr, wantErr)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("event changed: optimized=%+v map=%+v", got, want)
			}
		})
	}
}

func TestMarketHeaderEligibility(t *testing.T) {
	fixtures := []struct {
		frame   string
		mapPath bool
	}{
		{`{"s":10,"f":"t","id":"EURUSD"}`, false},
		{`{"heartbeat":0}`, false},
		{`{"f":"q","S":"ignored"}`, true},
		{`{"F":"q","f":"t"}`, true},
		{`{"f":"q","extension":"F"}`, true},
		{`{"f":"q","extension":{"S":0}}`, true},
		{`{"\u0066":"q"}`, true},
		{`{"f":"q","id":"é"}`, true},
		{`{"f":"q","id":"\\"}`, true},
	}
	for _, fixture := range fixtures {
		if got := marketHeaderNeedsMap([]byte(fixture.frame)); got != fixture.mapPath {
			t.Errorf("map eligibility for %s = %v, want %v", fixture.frame, got, fixture.mapPath)
		}
	}
	// Every mixed-case heartbeat spelling must use exact-key map lookups.
	const heartbeat = "heartbeat"
	for mask := 0; mask < 1<<len(heartbeat); mask++ {
		spelling := []byte(heartbeat)
		for i := range spelling {
			if mask&(1<<i) != 0 {
				spelling[i] -= 'a' - 'A'
			}
		}
		frame := []byte(fmt.Sprintf(`{"%s":1,"f":"q"}`, spelling))
		if got := marketHeaderNeedsMap(frame); got != (mask != 0) {
			t.Fatalf("map eligibility for %s = %v", frame, got)
		}
		want, wantErr := decodeMarketEventMapBaseline(frame)
		got, gotErr := decodeMarketEvent(frame)
		if (gotErr != nil) != (wantErr != nil) || !reflect.DeepEqual(got, want) {
			t.Fatalf("heartbeat spelling changed dispatch: %s, optimized=%+v/%v map=%+v/%v", frame, got, gotErr, want, wantErr)
		}
	}
	// Short and unterminated candidates must remain safe for the JSON decoder.
	for _, frame := range []string{`"`, `"S`, `"F`, `"heart`, `"heartbeat`, `"Heartbeat`} {
		_ = marketHeaderNeedsMap([]byte(frame))
	}
}

func TestMarketEventOwnsRawFrame(t *testing.T) {
	for _, frame := range []string{`{"f":"q","id":"EURUSD"}`, `{"f":"q","id":"F"}`} {
		input := []byte(frame)
		event, err := decodeMarketEvent(input)
		if err != nil {
			t.Fatal(err)
		}
		copy(input, strings.Repeat("x", len(input)))
		if string(event.Raw) != frame {
			t.Fatalf("Raw aliases the caller's frame: %s", event.Raw)
		}
	}
}

var marketBenchmarkSink MarketEvent

func BenchmarkDecodeMarketEvent(b *testing.B) {
	fixtures := []struct {
		name  string
		frame string
	}{
		{"quote", `{"f":"q","id":"EURUSD","t":1547942400,"ap":1.47245,"as":100,"bp":1.47245,"bs":100}`},
		{"trade", `{"f":"t","id":"EURUSD","p":1.47245,"s":100,"t":1547942400}`},
		{"daily_bar", `{"f":"d","id":"EURUSD","o":1,"h":2,"l":0.5,"c":1.5,"v":5,"t":1547942400}`},
		{"heartbeat", `{"heartbeat":1609255644}`},
	}
	decoders := []struct {
		name   string
		decode func([]byte) (MarketEvent, error)
	}{
		{"compact_header", decodeMarketEvent},
		{"map_baseline", decodeMarketEventMapBaseline},
	}
	for _, fixture := range fixtures {
		for _, decoder := range decoders {
			b.Run(fixture.name+"/"+decoder.name, func(b *testing.B) {
				frame := []byte(fixture.frame)
				b.ReportAllocs()
				b.SetBytes(int64(len(frame)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					event, err := decoder.decode(frame)
					if err != nil {
						b.Fatal(err)
					}
					// Include the unchanged error-envelope pass performed by Next.
					if err := streamAPIError(frame, 200); err != nil {
						b.Fatal(err)
					}
					marketBenchmarkSink = event
				}
			})
		}
	}
}

// decodeMarketEventMapBaseline preserves the pre-optimization dispatcher for
// semantic comparisons and allocation benchmarks. Keep its header as a map.
func decodeMarketEventMapBaseline(frame []byte) (MarketEvent, error) {
	event := MarketEvent{Raw: append(json.RawMessage(nil), frame...)}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(frame, &header); err != nil {
		return event, err
	}
	if header == nil {
		return event, fmt.Errorf("null is not a stream message")
	}
	var status, discriminator string
	_ = json.Unmarshal(header["s"], &status)
	if status == "error" {
		var message ErrorMessage
		if err := json.Unmarshal(frame, &message); err != nil {
			return event, err
		}
		event.Kind, event.Error = StreamError, &message
		return event, nil
	}
	if raw, exists := header["heartbeat"]; exists {
		if bytes.Equal(raw, []byte("null")) {
			return event, fmt.Errorf("heartbeat must be an integer")
		}
		var message StreamingHeartbeatResponse
		if err := json.Unmarshal(frame, &message); err != nil {
			return event, err
		}
		event.Kind, event.Heartbeat = StreamHeartbeat, &message
		return event, nil
	}
	if err := json.Unmarshal(header["f"], &discriminator); err != nil {
		return event, fmt.Errorf("missing or invalid market message discriminator: %w", err)
	}
	switch discriminator {
	case "q":
		var message StreamingQuoteResponse
		if err := json.Unmarshal(frame, &message); err != nil {
			return event, err
		}
		event.Kind, event.Quote = StreamQuote, &message
	case "t":
		var message StreamingTradeResponse
		if err := json.Unmarshal(frame, &message); err != nil {
			return event, err
		}
		event.Kind, event.Trade = StreamTrade, &message
	case "d":
		var message StreamingDailyBarResponse
		if err := json.Unmarshal(frame, &message); err != nil {
			return event, err
		}
		event.Kind, event.DailyBar = StreamDailyBar, &message
	case "a":
		var message StreamingAskResponseDeprecated
		if err := json.Unmarshal(frame, &message); err != nil {
			return event, err
		}
		event.Kind, event.Ask = StreamAsk, &message
	case "b":
		var message StreamingBidResponseDeprecated
		if err := json.Unmarshal(frame, &message); err != nil {
			return event, err
		}
		event.Kind, event.Bid = StreamBid, &message
	default:
		return event, fmt.Errorf("unknown market message discriminator %q", discriminator)
	}
	return event, nil
}
