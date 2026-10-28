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
	changed := false
	if raw, ok := object["symbol"]; ok && compactMetadataValue(raw) {
		object["symbol"] = append(append([]byte{'['}, raw...), ']')
		changed = true
	}
	var symbols []json.RawMessage
	if raw, ok := object["symbol"]; ok {
		if err := json.Unmarshal(raw, &symbols); err != nil {
			return nil, fmt.Errorf("tradeview: decode symbol metadata symbol: %w", err)
		}
	}
	for _, field := range fields {
		if field == "symbol" {
			continue
		}
		raw, ok := object[field]
		if !ok || !compactMetadataValue(raw) {
			continue
		}
		if len(symbols) == 0 {
			return nil, fmt.Errorf("tradeview: scalar symbol metadata %q requires at least one symbol", field)
		}
		var array bytes.Buffer
		array.WriteByte('[')
		for index := range symbols {
			if index > 0 {
				array.WriteByte(',')
			}
			array.Write(raw)
		}
		array.WriteByte(']')
		object[field] = array.Bytes()
		changed = true
	}
	if !changed {
		return data, nil
	}
	return json.Marshal(object)
}

func compactMetadataValue(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) != 0 && raw[0] != '[' && !bytes.Equal(raw, []byte("null"))
}
