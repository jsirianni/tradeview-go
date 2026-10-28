package tradeview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// StreamMessageKind distinguishes the variants in a stream without discarding
// their original wire representation.
type StreamMessageKind string

const (
	StreamData      StreamMessageKind = "data"
	StreamPing      StreamMessageKind = "ping"
	StreamError     StreamMessageKind = "error"
	StreamQuote     StreamMessageKind = "quote"
	StreamTrade     StreamMessageKind = "trade"
	StreamDailyBar  StreamMessageKind = "daily_bar"
	StreamHeartbeat StreamMessageKind = "heartbeat"
	StreamAsk       StreamMessageKind = "ask"
	StreamBid       StreamMessageKind = "bid"
)

// AccountEvent contains exactly one of Data, Ping, or Error. Data messages are
// delivered unchanged: the caller decides whether a message is a snapshot or an
// update. Raw retains extension fields and the complete original JSON frame.
type AccountEvent[T any] struct {
	Kind  StreamMessageKind
	Data  *T
	Ping  *PingMessage
	Error *ErrorMessage
	Raw   json.RawMessage
}

type (
	StateEvent               = AccountEvent[AccountStateMessage]
	OrdersEvent              = AccountEvent[OrdersMessage]
	PositionsEvent           = AccountEvent[StreamPositionsMessage]
	NetPositionsEvent        = AccountEvent[StreamNetPositionsMessage]
	IndividualPositionsEvent = AccountEvent[StreamIndividualPositionsMessage]
	NotificationsEvent       = AccountEvent[StreamNotificationsMessage]
	QuotesEvent              = AccountEvent[QuotesMessage]
	DepthEvent               = AccountEvent[Depth]
)

func decodeAccountEvent[T any](frame []byte) (AccountEvent[T], error) {
	event := AccountEvent[T]{Raw: append(json.RawMessage(nil), frame...)}
	if bytes.Equal(frame, []byte("null")) {
		return event, errors.New("null is not a stream message")
	}
	if len(frame) == 0 {
		return event, errors.New("empty stream message")
	}
	if frame[0] == '{' {
		var header map[string]json.RawMessage
		if err := json.Unmarshal(frame, &header); err != nil {
			return event, err
		}
		var status string
		_ = json.Unmarshal(header["s"], &status)
		switch status {
		case "error":
			var message ErrorMessage
			if err := json.Unmarshal(frame, &message); err != nil {
				return event, err
			}
			event.Kind, event.Error = StreamError, &message
			return event, nil
		case "ok":
			var message PingMessage
			if err := json.Unmarshal(frame, &message); err != nil {
				return event, err
			}
			event.Kind, event.Ping = StreamPing, &message
			return event, nil
		}
	}
	var data T
	if err := json.Unmarshal(frame, &data); err != nil {
		return event, err
	}
	event.Kind, event.Data = StreamData, &data
	return event, nil
}

// MarketEvent is a tagged union of every /streaming price-feed frame, including
// the deprecated separate ask/bid variants. Raw retains the original JSON.
type MarketEvent struct {
	Kind      StreamMessageKind
	Quote     *StreamingQuoteResponse
	Trade     *StreamingTradeResponse
	DailyBar  *StreamingDailyBarResponse
	Heartbeat *StreamingHeartbeatResponse
	Ask       *StreamingAskResponseDeprecated
	Bid       *StreamingBidResponseDeprecated
	Error     *ErrorMessage
	Raw       json.RawMessage
}

func decodeMarketEvent(frame []byte) (MarketEvent, error) {
	event := MarketEvent{Raw: append(json.RawMessage(nil), frame...)}
	header, err := decodeMarketHeader(frame)
	if err != nil {
		return event, err
	}
	var status, discriminator string
	_ = json.Unmarshal(header.Status, &status)
	if status == "error" {
		var message ErrorMessage
		if err := json.Unmarshal(frame, &message); err != nil {
			return event, err
		}
		event.Kind, event.Error = StreamError, &message
		return event, nil
	}
	if raw := header.Heartbeat; raw != nil {
		if bytes.Equal(raw, []byte("null")) {
			return event, errors.New("heartbeat must be an integer")
		}
		var message StreamingHeartbeatResponse
		if err := json.Unmarshal(frame, &message); err != nil {
			return event, err
		}
		event.Kind, event.Heartbeat = StreamHeartbeat, &message
		return event, nil
	}
	if err := json.Unmarshal(header.Discriminator, &discriminator); err != nil {
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

// marketHeader retains null separately from an absent field. Using RawMessage
// also leaves type validation to the existing event-specific decoders.
type marketHeader struct {
	Status        json.RawMessage `json:"s"`
	Discriminator json.RawMessage `json:"f"`
	Heartbeat     json.RawMessage `json:"heartbeat"`
}

func decodeMarketHeader(frame []byte) (marketHeader, error) {
	var header marketHeader
	if bytes.Equal(bytes.TrimSpace(frame), []byte("null")) {
		return header, errors.New("null is not a stream message")
	}
	if marketHeaderNeedsMap(frame) {
		// A struct accepts case-folded keys, while the original map dispatcher
		// uses exact keys. Keep that behavior for potentially ambiguous frames.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(frame, &fields); err != nil {
			return header, err
		}
		header.Status = fields["s"]
		header.Discriminator = fields["f"]
		header.Heartbeat = fields["heartbeat"]
		return header, nil
	}
	if err := json.Unmarshal(frame, &header); err != nil {
		return header, err
	}
	return header, nil
}

// marketHeaderNeedsMap conservatively finds anything that could make struct
// field matching differ from exact map lookups. It deliberately also matches
// values and nested keys: false positives only select the compatible slow path.
// No JSON parsing is performed here; json.Unmarshal still validates the frame.
func marketHeaderNeedsMap(frame []byte) bool {
	const heartbeat = "heartbeat"
	for i, value := range frame {
		if value == '\\' || value >= 0x80 {
			return true
		}
		if value != '"' {
			continue
		}
		if i+2 < len(frame) && frame[i+2] == '"' && (frame[i+1] == 'S' || frame[i+1] == 'F') {
			return true
		}
		if i+len(heartbeat)+1 >= len(frame) || frame[i+len(heartbeat)+1] != '"' {
			continue
		}
		folded, uppercase := true, false
		for j := range heartbeat {
			letter := frame[i+j+1]
			if letter|0x20 != heartbeat[j] {
				folded = false
				break
			}
			uppercase = uppercase || letter != heartbeat[j]
		}
		if folded && uppercase {
			return true
		}
	}
	return false
}
