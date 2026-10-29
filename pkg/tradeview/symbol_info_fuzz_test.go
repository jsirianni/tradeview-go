package tradeview

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"
)

// This oracle delegates key folding, duplicates and property order to
// encoding/json, independently of the normalizer's token/span traversal.
type fuzzScalarOrSlice[T any] struct {
	values []T
	scalar bool
}

func (value *fuzzScalarOrSlice[T]) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	value.scalar = len(raw) > 0 && raw[0] != '[' && !bytes.Equal(raw, []byte("null"))
	if !value.scalar {
		return json.Unmarshal(raw, &value.values)
	}
	var item T
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	value.values = []T{item}
	return nil
}

func (value fuzzScalarOrSlice[T]) expanded(count int) []T {
	if !value.scalar {
		return value.values
	}
	result := make([]T, count)
	for i := range result {
		result[i] = value.values[0]
	}
	return result
}

func FuzzSymbolInfoJSON(f *testing.F) {
	for _, seed := range []string{
		`{"symbol":["AAPL"],"Symbol":["MSFT","GOOG"],"pricescale":100}`,
		`{"Symbol":["MSFT","GOOG"],"symbol":["AAPL"],"description":"shared"}`,
		`{"ſymbol":"MSFT","pricescale":0,"description":""}`,
		`{"\u0073ymbol":["X","Y"],"pricescale":0,"description":"a\\b"}`,
		`{"symbol":"X","symbol":["Y","Z"],"pricescale":1,"pricescale":2}`,
		`{"symbol":["X"],"pricescale":1,"Pricescale":[2],"extension":{"s":1,"s":2}}`,
		`{"symbol":["X"],"Symbol":null,"pricescale":1}`,
		`{"symbol":[],"pricescale":1}`, `{"symbol":null,"pricescale":null}`,
		`{"symbol":[null,"X"],"description":[null,"x"],"pricescale":[null,0]}`,
		`{"symbol":["X"],"description":{"nested":[1,2]}}`,
		`null`, `[]`, `123`, `{"symbol":`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		// Bounds both raw parsing and the largest possible compact expansion.
		if len(input) > 2048 {
			t.Skip()
		}
		original := bytes.Clone(input)
		fields := []string{"symbol", "pricescale", "description"}
		normalized, err := normalizeSymbolInfoJSON(input, fields)
		if !bytes.Equal(input, original) {
			t.Fatal("normalization mutated the input")
		}
		if err == nil {
			if !json.Valid(normalized) || int64(len(normalized)) > MaxSymbolInfoExpandedBytes {
				t.Fatal("normalization produced invalid or excessive JSON")
			}
			again, err := normalizeSymbolInfoJSON(normalized, fields)
			if err != nil || !bytes.Equal(again, normalized) {
				t.Fatalf("normalization is not idempotent: %v", err)
			}
		}
		var oracle struct {
			Symbol      fuzzScalarOrSlice[string]  `json:"symbol"`
			Pricescale  fuzzScalarOrSlice[float64] `json:"pricescale"`
			Description fuzzScalarOrSlice[string]  `json:"description"`
		}
		oracleErr := json.Unmarshal(input, &oracle)
		check := func(symbol []string, pricescale []float64, description []string) {
			t.Helper()
			if oracleErr != nil {
				t.Fatalf("typed model accepted an invalid selected field: %v", oracleErr)
			}
			count := len(oracle.Symbol.values)
			if count > MaxSymbolInfoSymbols || !reflect.DeepEqual(symbol, oracle.Symbol.values) ||
				!reflect.DeepEqual(pricescale, oracle.Pricescale.expanded(count)) ||
				!reflect.DeepEqual(description, oracle.Description.expanded(count)) {
				t.Fatalf("typed fields disagree with scalar/array oracle: symbols=%q pricescale=%v description=%q", symbol, pricescale, description)
			}
		}
		var arrays SymbolInfoArrays
		if err := json.Unmarshal(input, &arrays); err == nil {
			check(arrays.Symbol, arrays.Pricescale, arrays.Description)
		}
		var response SymbolInfoResponse
		if err := json.Unmarshal(input, &response); err == nil {
			check(response.Symbol, response.Pricescale, response.Description)
		}
		if !bytes.Equal(input, original) {
			t.Fatal("typed decoding mutated raw input")
		}
		if json.Valid(input) {
			var union GetSymbolInfo200JSONResponseBody
			owned := bytes.Clone(input)
			if err := union.UnmarshalJSON(owned); err != nil {
				t.Fatal(err)
			}
			owned[0] ^= 1
			_, _ = union.AsSymbolInfoResponse()
			raw, err := union.MarshalJSON()
			if err != nil || !bytes.Equal(raw, original) {
				t.Fatalf("union accessor changed its owned raw response: %v", err)
			}
		}
	})
}

func FuzzSymbolInfoBudget(f *testing.F) {
	for _, seed := range []struct {
		count, size uint64
		remaining   int64
		flags       byte
	}{
		{1, 1, 68, 0}, {1, 1, 67, 1}, {0, 0, 0, 0},
		{65536, 1024, MaxSymbolInfoExpandedBytes, 2},
		{65537, 1, MaxSymbolInfoExpandedBytes, 7},
		{8192, 1024, MaxSymbolInfoExpandedBytes, 24},
		{math.MaxUint64, math.MaxUint64, math.MaxInt64, 255},
	} {
		f.Add(seed.count, seed.size, seed.remaining, seed.flags)
	}
	f.Fuzz(func(t *testing.T, countValue, sizeValue uint64, remaining int64, flags byte) {
		count := int(min(countValue, uint64(math.MaxInt)))
		size := int(min(sizeValue, uint64(math.MaxInt)))
		// Big integer arithmetic supplies an overflow-independent budget oracle.
		wantCost := new(big.Int).SetUint64(uint64(size))
		wantCost.Add(wantCost, big.NewInt(65))
		wantCost.Mul(wantCost, new(big.Int).SetUint64(uint64(count)))
		wantCost.Add(wantCost, big.NewInt(2))
		wantOK := count > 0 && remaining >= 2 && int64(size) <= MaxSymbolInfoExpandedBytes && wantCost.Cmp(big.NewInt(remaining)) <= 0
		cost, ok := symbolInfoExpansionCost(remaining, count, size)
		if ok != wantOK || ok && cost != wantCost.Int64() {
			t.Fatalf("budget arithmetic: cost=%d ok=%v, want %s/%v", cost, ok, wantCost, wantOK)
		}

		// Exercise the real preflight with bounded wire input, including arrays
		// later overwritten by aliases and repeated compact fields. Large accepted
		// outputs are skipped; rejected cases never need to allocate the expansion.
		count = int(countValue % (MaxSymbolInfoSymbols + 2))
		if count == 0 {
			return
		}
		size = int(sizeValue % 1025)
		keys := []string{`symbol`, `Symbol`, `ſymbol`, `\u0073ymbol`}
		key := keys[int(flags)%len(keys)]
		scalar := `"` + strings.Repeat("x", size) + `"`
		payload := `{"` + key + `":[` + strings.Repeat(`"X",`, count-1) + `"X"]`
		finalCount := count
		if flags&4 != 0 {
			payload += `,"symbol":["last"]`
			finalCount = 1
		}
		copies := 1 + int(flags>>3)%4
		for range copies {
			payload += `,"description":` + scalar
		}
		payload += `}`
		expandedCost := int64(len(payload)) + int64(copies)*(int64(finalCount)*(int64(len(scalar))+65)+2)
		mustReject := count > MaxSymbolInfoSymbols || expandedCost > MaxSymbolInfoExpandedBytes
		if !mustReject && expandedCost > 256<<10 {
			return
		}
		output, err := normalizeSymbolInfoJSON([]byte(payload), []string{"symbol", "description"})
		if mustReject {
			if _, ok := errors.AsType[*SymbolInfoSizeError](err); !ok {
				t.Fatalf("alias/duplicate expansion bypassed budget: %v", err)
			}
		} else if err != nil || int64(len(output)) > MaxSymbolInfoExpandedBytes || !json.Valid(output) {
			t.Fatalf("bounded expansion failed: %v", err)
		}
	})
}
