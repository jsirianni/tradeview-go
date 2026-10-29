package tradeview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// MaxSymbolInfoExpandedBytes bounds compact metadata normalization, including
// estimated destination element storage. Raw response unions remain unchanged.
const MaxSymbolInfoExpandedBytes int64 = 16 << 20

// MaxSymbolInfoSymbols bounds the number of symbols decoded by typed models.
const MaxSymbolInfoSymbols = 65536

// SymbolInfoSizeError reports symbol metadata exceeding a typed model budget.
// It retains no broker data, including field names or repeated scalar values.
type SymbolInfoSizeError struct{ Limit int64 }

func (e *SymbolInfoSizeError) Error() string {
	return fmt.Sprintf("tradeview: symbol metadata exceeds %d byte expansion budget or %d symbols", e.Limit, MaxSymbolInfoSymbols)
}

// symbolInfoExpansionCost accounts for serialized array values plus a conservative
// per-element allowance, without multiplying untrusted sizes before checking.
func symbolInfoExpansionCost(remaining int64, count int, rawBytes int) (int64, bool) {
	if count <= 0 || remaining < 2 {
		return 0, false
	}
	perElement := int64(rawBytes)
	if perElement > remaining-2 || perElement > MaxSymbolInfoExpandedBytes {
		return 0, false
	}
	perElement += 65 // comma plus destination element storage (including object pointers)
	if int64(count) > (remaining-2)/perElement {
		return 0, false
	}
	return int64(count)*perElement + 2, true
}

// normalizeSymbolInfoJSON expands only compact metadata. Existing arrays and
// nulls retain their original decoding behavior, including nullable elements.
func normalizeSymbolInfoJSON(data []byte, fields []string) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("tradeview: decode symbol metadata: %w", err)
	}
	if object == nil {
		return data, nil
	}
	symbolCount := 0
	metadataFields := make(map[string]bool, len(fields))
	for _, field := range fields {
		metadataFields[field] = true
	}

	// Retain the original property order and every duplicate key. Re-encoding
	// the map would change which case-folded field encoding/json assigns last.
	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	type expansion struct {
		start, end, count int
		field             string
		symbol            bool
		raw               json.RawMessage
	}
	var expansions []expansion
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		field, ok := token.(string)
		if !ok {
			return nil, errors.New("tradeview: invalid symbol metadata property")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		// encoding/json assigns Unicode case-folded aliases too. Check every
		// occurrence before final typed decoding, including overwritten arrays.
		// The last matching occurrence also determines the final slice length,
		// including a null or empty array that clears an earlier symbol value.
		isSymbol := strings.EqualFold(field, "symbol")
		if isSymbol {
			symbolCount, err = boundedSymbolInfoCount(raw)
			if err != nil {
				return nil, fmt.Errorf("tradeview: decode symbol metadata symbol: %w", err)
			}
		}
		if !(metadataFields[field] || isSymbol && metadataFields["symbol"]) || !compactMetadataValue(raw) {
			continue
		}
		end := int(decoder.InputOffset())
		start := end - len(raw)
		expansions = append(expansions, expansion{start: start, end: end, field: field, symbol: isSymbol, raw: raw})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if len(expansions) == 0 {
		return data, nil
	}
	// Preflight every expansion before allocating any repeated representation.
	// This also counts duplicate properties that the final decode overwrites.
	// Resolve the symbol count after reading every key so metadata before a
	// later symbol alias uses the same final count as metadata after that alias.
	remaining := MaxSymbolInfoExpandedBytes - int64(len(data))
	for index := range expansions {
		item := &expansions[index]
		item.count = symbolCount
		if item.symbol {
			item.count = 1
		} else if item.count == 0 {
			return nil, fmt.Errorf("tradeview: scalar symbol metadata %q requires at least one symbol", item.field)
		}
		cost, ok := symbolInfoExpansionCost(remaining, item.count, len(item.raw))
		if !ok {
			return nil, &SymbolInfoSizeError{Limit: MaxSymbolInfoExpandedBytes}
		}
		remaining -= cost
	}
	var output bytes.Buffer
	copied := 0
	for _, item := range expansions {
		output.Write(data[copied:item.start])
		output.WriteByte('[')
		for index := range item.count {
			if index > 0 {
				output.WriteByte(',')
			}
			output.Write(item.raw)
		}
		output.WriteByte(']')
		copied = item.end
	}
	output.Write(data[copied:])
	return output.Bytes(), nil
}

func compactMetadataValue(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) != 0 && raw[0] != '[' && !bytes.Equal(raw, []byte("null"))
}

// boundedSymbolInfoCount avoids allocating a RawMessage slice proportional to
// an untrusted symbol count. Individual elements are discarded as they are read.
func boundedSymbolInfoCount(raw json.RawMessage) (int, error) {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		return 0, nil
	}
	if compactMetadataValue(raw) {
		return 1, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Token(); err != nil {
		return 0, err
	}
	count := 0
	for decoder.More() {
		if count == MaxSymbolInfoSymbols {
			return 0, &SymbolInfoSizeError{Limit: MaxSymbolInfoExpandedBytes}
		}
		var item json.RawMessage
		if err := decoder.Decode(&item); err != nil {
			return 0, err
		}
		count++
	}
	if _, err := decoder.Token(); err != nil {
		return 0, err
	}
	return count, nil
}
