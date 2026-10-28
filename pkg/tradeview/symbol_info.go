package tradeview

import (
	"bytes"
	"encoding/json"
	"fmt"
)

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
	if raw, ok := object["symbol"]; ok {
		if compactMetadataValue(raw) {
			symbolCount = 1
		} else {
			var symbols []json.RawMessage
			if err := json.Unmarshal(raw, &symbols); err != nil {
				return nil, fmt.Errorf("tradeview: decode symbol metadata symbol: %w", err)
			}
			symbolCount = len(symbols)
		}
	}
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
	var output bytes.Buffer
	copied := 0
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		field := token.(string)
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		if !metadataFields[field] || !compactMetadataValue(raw) {
			continue
		}
		count := symbolCount
		if field == "symbol" {
			count = 1
		} else if count == 0 {
			return nil, fmt.Errorf("tradeview: scalar symbol metadata %q requires at least one symbol", field)
		}
		end := int(decoder.InputOffset())
		start := end - len(raw)
		output.Write(data[copied:start])
		output.WriteByte('[')
		for index := range count {
			if index > 0 {
				output.WriteByte(',')
			}
			output.Write(raw)
		}
		output.WriteByte(']')
		copied = end
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if copied == 0 {
		return data, nil
	}
	output.Write(data[copied:])
	return output.Bytes(), nil
}

func compactMetadataValue(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) != 0 && raw[0] != '[' && !bytes.Equal(raw, []byte("null"))
}
