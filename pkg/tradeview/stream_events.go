package tradeview

import (
	"bytes"
	"encoding/json"
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

type StateEvent = AccountEvent[AccountStateMessage]
type OrdersEvent = AccountEvent[OrdersMessage]
type PositionsEvent = AccountEvent[StreamPositionsMessage]
type NetPositionsEvent = AccountEvent[StreamNetPositionsMessage]
type IndividualPositionsEvent = AccountEvent[StreamIndividualPositionsMessage]
type NotificationsEvent = AccountEvent[StreamNotificationsMessage]
type QuotesEvent = AccountEvent[QuotesMessage]
type DepthEvent = AccountEvent[Depth]

func decodeAccountEvent[T any](frame []byte) (AccountEvent[T], error) {
	event := AccountEvent[T]{Raw: append(json.RawMessage(nil), frame...)}
	if bytes.Equal(frame, []byte("null")) {
		return event, fmt.Errorf("null is not a stream message")
	}
	if len(frame) == 0 {
		return event, fmt.Errorf("empty stream message")
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
