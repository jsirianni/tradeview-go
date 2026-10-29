package tradeview

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func FuzzMarketHeader(f *testing.F) {
	for _, seed := range []string{
		`{"f":"q","id":"X","t":0,"ap":0,"as":0,"bp":0,"bs":0}`,
		`{"s":"error","S":"ok","errmsg":"expired"}`,
		`{"S":"error","f":"q"}`, `{"ſ":"error","f":"q"}`,
		`{"s":"error","s":null,"f":"q"}`, `{"s":null,"s":"error"}`,
		`{"f":"q","F":"t","extra":{"heartbeat":1}}`,
		`{"\u0066":"q","\u0046":"t","\u0073":"error"}`,
		`{"heartbeat":1,"HeartBeat":null}`, `{"heartbeat":null,"heartbeat":0}`,
		`{"f":"t","s":0,"id":"€\\USD","extra":[{"S":"error"}]}`,
		`{"f":"q","f":"t"}`, `{"f":{"nested":[1,2]}}`,
		`null`, `[]`, `123`, `"f"`, `{`, `{"f":"q"}{}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 4096 {
			t.Skip()
		}
		original := bytes.Clone(input)
		var exact map[string]json.RawMessage
		wantErr := json.Unmarshal(input, &exact)
		got, gotErr := decodeMarketHeader(input)
		wantFailure := wantErr != nil || exact == nil
		if (gotErr != nil) != wantFailure {
			t.Fatalf("header acceptance differs from exact-key map: got %v, map %v", gotErr, wantErr)
		}
		if !wantFailure {
			want := marketHeader{Status: exact["s"], Discriminator: exact["f"], Heartbeat: exact["heartbeat"]}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("header differs from exact-key map: got %+v want %+v", got, want)
			}
		}
		if !bytes.Equal(original, input) {
			t.Fatal("header decoder mutated the frame")
		}
	})
}

func FuzzStreamFrames(f *testing.F) {
	for _, seed := range []struct {
		wire  string
		limit uint16
		flags byte
	}{
		{"{\"heartbeat\":1}\n{\"heartbeat\":2}\r\n", 128, 0},
		{" \r\n{\"s\":\"error\",\"S\":\"ok\",\"errmsg\":\"expired\"}\n{\"heartbeat\":1}\n", 128, 1},
		{"{\"heartbeat\":1}\n{\"S\":\"error\",\"heartbeat\":2}", 128, 2},
		{"{\"heartbeat\":1}\r\n{\"s\":\"error\",\"s\":null}\n", 128, 0},
		{"{\"heartbeat\":1}\n{\"s\":null,\"\\u0073\":\"error\",\"errmsg\":\"e\"}\n", 128, 1},
		{"{\"f\":\"t\",\"id\":\"X\",\"p\":0,\"s\":0,\"t\":0}\r\n", 128, 0},
		{"{\"f\":\"q\"}\n", 128, 0},
		{"[]\n{\"s\":\"ok\"}\n{\"s\":\"error\",\"errmsg\":\"e\"}\n[]", 128, 1},
		{"{\"heartbeat\":0}", 14, 0}, // Exactly 15 bytes, with no newline.
		{"{\"heartbeat\":0}\r\n", 14, 1},
		{"{\"heartbeat\":0}\n", 13, 0}, // One byte over the frame limit.
		{"\r\n\n \t\r\n", 1, 0}, {"null\n", 128, 0},
		{"{\"heartbeat\":1}\n{malformed", 128, 0},
		{"", 128, 0},
	} {
		f.Add([]byte(seed.wire), []byte{1, 2, 3, 255}, seed.limit, seed.flags)
	}
	f.Fuzz(func(t *testing.T, wire, chunks []byte, limitValue uint16, flags byte) {
		if len(wire) > 4096 || len(chunks) > 64 {
			t.Skip()
		}
		limit := 1 + int(limitValue)%4096
		fuzzStreamContract(t, wire, chunks, limit, flags, decodeMarketEvent,
			func(event MarketEvent) (StreamMessageKind, json.RawMessage, *ErrorMessage) {
				return event.Kind, event.Raw, event.Error
			})
		fuzzStreamContract(t, wire, chunks, limit, flags, decodeAccountEvent[json.RawMessage],
			func(event AccountEvent[json.RawMessage]) (StreamMessageKind, json.RawMessage, *ErrorMessage) {
				return event.Kind, event.Raw, event.Error
			})
	})
}

// Split independently of Scanner to establish framing, limit and terminal
// expectations. The real event decoder supplies typed values; classification
// is checked against exact JSON keys rather than terminalAPIError itself.
func fuzzStreamContract[T streamEvent](t *testing.T, wire, chunks []byte, limit int, flags byte, decode func([]byte) (T, error), inspect func(T) (StreamMessageKind, json.RawMessage, *ErrorMessage)) {
	t.Helper()
	var want []T
	wantTerminal := ""
	for line := range bytes.SplitSeq(wire, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) > limit {
			wantTerminal = "size"
			break
		}
		frame := bytes.TrimSpace(line)
		if len(frame) == 0 {
			continue
		}
		event, err := decode(frame)
		if err != nil {
			wantTerminal = "decode"
			break
		}
		kind, raw, _ := inspect(event)
		var exact map[string]json.RawMessage
		var status string
		if json.Unmarshal(frame, &exact) == nil {
			if err := json.Unmarshal(exact["s"], &status); err != nil {
				status = "" // Nonstring/absent statuses do not select a status event.
			}
		}
		if (kind == StreamError) != (status == "error") || !bytes.Equal(raw, frame) {
			t.Fatalf("decoded event disagrees with exact status/raw: %s, %s", kind, raw)
		}
		want = append(want, event)
		if kind == StreamError {
			wantTerminal = "api"
			break
		}
	}
	reader := &fuzzChunkReader{data: bytes.Clone(wire), sizes: chunks, eofWithData: flags&1 != 0}
	body := &streamObservedBody{Reader: reader}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/x-ndjson"}}, Body: body}
	stream, err := openHTTPStream(t.Context(), response, nil, streamConfig{maxFrameBytes: limit}, decode)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestBody(t, stream)
	var retained []T
	var last T
	for i, expected := range want {
		if !stream.Next() {
			t.Fatalf("missing event %d: %v", i, stream.Err())
		}
		last = stream.Value()
		if !reflect.DeepEqual(last, expected) {
			t.Fatalf("event %d changed across reader chunks", i)
		}
		retained = append(retained, last)
		kind, _, _ := inspect(last)
		if flags&2 != 0 && i == 0 && kind != StreamError {
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			wantTerminal = ""
			break
		}
	}
	if stream.Next() {
		t.Fatal("unexpected event beyond oracle terminal point")
	}
	var apiErr *APIError
	switch wantTerminal {
	case "api":
		_, raw, message := inspect(last)
		if !errors.As(stream.Err(), &apiErr) || message == nil || apiErr.StatusCode != http.StatusOK || apiErr.Message != message.Errmsg || !bytes.Equal(apiErr.Body, raw) {
			t.Fatalf("error event and terminal APIError disagree: %v", stream.Err())
		}
		if message.Errtype != nil && apiErr.Code != string(*message.Errtype) {
			t.Fatal("terminal API error lost its code")
		}
		apiErr.Body[0] ^= 1
		if !reflect.DeepEqual(stream.Value(), last) {
			t.Fatal("API diagnostics alias the retained event")
		}
	case "size":
		if stream.Err() == nil || !strings.Contains(stream.Err().Error(), "stream frame exceeds") {
			t.Fatalf("missing frame limit error: %v", stream.Err())
		}
	case "decode":
		if stream.Err() == nil || !strings.Contains(stream.Err().Error(), "decode stream frame") {
			t.Fatalf("missing frame decoding error: %v", stream.Err())
		}
	default:
		if stream.Err() != nil {
			t.Fatalf("expected clean EOF/Close: %v", stream.Err())
		}
	}
	if err := stream.Close(); err != nil || body.closed.Load() != 1 || stream.Next() || !reflect.DeepEqual(stream.Value(), last) {
		t.Fatalf("closure/last-value invariant failed: close=%v count=%d", err, body.closed.Load())
	}
	for i, event := range retained {
		if !reflect.DeepEqual(event, want[i]) {
			t.Fatal("later reads changed an earlier event's owned bytes")
		}
	}
}

type fuzzChunkReader struct {
	data        []byte
	sizes       []byte
	index       int
	eofWithData bool
}

func (reader *fuzzChunkReader) Read(dst []byte) (int, error) {
	if len(reader.data) == 0 {
		return 0, io.EOF
	}
	size := 64
	if len(reader.sizes) != 0 {
		size = 1 + int(reader.sizes[reader.index%len(reader.sizes)])
		reader.index++
	}
	n := copy(dst, reader.data[:min(size, len(reader.data))])
	reader.data = reader.data[n:]
	if reader.eofWithData && len(reader.data) == 0 {
		return n, io.EOF
	}
	return n, nil
}
