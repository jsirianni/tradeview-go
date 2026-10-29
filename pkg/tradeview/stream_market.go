package tradeview

import (
	"encoding/json"
	"fmt"
)

// Presence-aware wire fields keep absent/null values distinct from legitimate
// zeros. Decode once, then construct the public generated model. Pointer fields
// retain encoding/json's duplicate-key and case-folded matching behavior.
func decodeMarketQuote(frame []byte) (StreamingQuoteResponse, error) {
	var fields struct {
		F  *string  `json:"f"`
		Id *string  `json:"id"`
		T  *float64 `json:"t"`
		Ap *float64 `json:"ap"`
		As *float64 `json:"as"`
		Bp *float64 `json:"bp"`
		Bs *float64 `json:"bs"`
	}
	if err := json.Unmarshal(frame, &fields); err != nil {
		return StreamingQuoteResponse{}, err
	}
	if err := requireMarketFields(
		marketField{"f", fields.F != nil}, marketField{"id", fields.Id != nil},
		marketField{"t", fields.T != nil}, marketField{"ap", fields.Ap != nil},
		marketField{"as", fields.As != nil}, marketField{"bp", fields.Bp != nil},
		marketField{"bs", fields.Bs != nil},
	); err != nil {
		return StreamingQuoteResponse{}, err
	}
	return StreamingQuoteResponse{F: *fields.F, Id: *fields.Id, T: *fields.T, Ap: *fields.Ap, As: *fields.As, Bp: *fields.Bp, Bs: *fields.Bs}, nil
}

// The pinned schema shares id/p/s/t across trades and deprecated ask/bid
// messages. Their f discriminator must be present to select these variants.
type marketPriceFields struct {
	F  *string  `json:"f"`
	Id *string  `json:"id"`
	P  *float64 `json:"p"`
	S  *float64 `json:"s"`
	T  *float64 `json:"t"`
}

func decodeMarketPrice(frame []byte) (marketPriceFields, error) {
	var fields marketPriceFields
	if err := json.Unmarshal(frame, &fields); err != nil {
		return fields, err
	}
	err := requireMarketFields(
		marketField{"f", fields.F != nil}, marketField{"id", fields.Id != nil},
		marketField{"p", fields.P != nil}, marketField{"s", fields.S != nil},
		marketField{"t", fields.T != nil},
	)
	return fields, err
}

func decodeMarketDailyBar(frame []byte) (StreamingDailyBarResponse, error) {
	var fields struct {
		F  *string  `json:"f"`
		Id *string  `json:"id"`
		O  *float64 `json:"o"`
		H  *float64 `json:"h"`
		L  *float64 `json:"l"`
		C  *float64 `json:"c"`
		V  *float64 `json:"v"`
		T  *float64 `json:"t"`
	}
	if err := json.Unmarshal(frame, &fields); err != nil {
		return StreamingDailyBarResponse{}, err
	}
	if err := requireMarketFields(
		marketField{"f", fields.F != nil}, marketField{"id", fields.Id != nil},
		marketField{"o", fields.O != nil}, marketField{"h", fields.H != nil},
		marketField{"l", fields.L != nil}, marketField{"c", fields.C != nil},
		marketField{"v", fields.V != nil}, marketField{"t", fields.T != nil},
	); err != nil {
		return StreamingDailyBarResponse{}, err
	}
	return StreamingDailyBarResponse{F: *fields.F, Id: *fields.Id, O: *fields.O, H: *fields.H, L: *fields.L, C: *fields.C, V: *fields.V, T: *fields.T}, nil
}

type marketField struct {
	name    string
	present bool
}

func requireMarketFields(fields ...marketField) error {
	for _, field := range fields {
		if !field.present {
			return fmt.Errorf("missing or null required market field %q", field.name)
		}
	}
	return nil
}
