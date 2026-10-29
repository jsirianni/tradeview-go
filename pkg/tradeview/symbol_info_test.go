package tradeview

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func symbolInfoModelFactories() map[string]func() any {
	return map[string]func() any{
		"arrays":   func() any { return new(SymbolInfoArrays) },
		"response": func() any { return new(SymbolInfoResponse) },
	}
}

func TestSymbolInfoReportedCompactResponse(t *testing.T) {
	const payload = `{
        "s":"ok", "symbol":["AAPL","MSFT"],
        "description":["Apple","Microsoft"], "currency":["USD","USD"],
        "exchange-listed":["NASDAQ","NASDAQ"], "exchange-traded":["NASDAQ","NASDAQ"],
        "minmovement":[1,1], "pricescale":100,
        "session-regular":["0930-1600","0930-1600"],
        "timezone":["America/New_York","America/New_York"], "type":["stock","stock"],
        "broker-extension":{"venue":"primary"}
    }`
	body := &finiteObservedBody{Reader: strings.NewReader(payload)}
	response, err := ParseGetSymbolInfoResponse(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !body.closed || response.JSON200 == nil {
		t.Fatal("missing closed body or typed response")
	}
	model, err := response.JSON200.AsSymbolInfoResponse()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(model.Pricescale, []float64{100, 100}) || !reflect.DeepEqual(model.Symbol, []string{"AAPL", "MSFT"}) {
		t.Fatalf("compact metadata was not applied to every symbol: %+v", model)
	}
	if string(response.Body) != payload {
		t.Fatal("original HTTP payload changed")
	}
	unionJSON, err := json.Marshal(response.JSON200)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(unionJSON, []byte(`"pricescale":100`)) || !bytes.Contains(unionJSON, []byte(`"broker-extension"`)) {
		t.Fatalf("union lost the original scalar or extension: %s", unionJSON)
	}
	normalized, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(normalized, []byte(`"pricescale":[100,100]`)) {
		t.Fatalf("model did not marshal expanded arrays: %s", normalized)
	}
}

// Each metadata field is exercised against both public models. The values cover
// concrete enum/object types as well as zeros that must not become omissions.
func TestSymbolInfoCompactMetadataFields(t *testing.T) {
	fields := map[string]string{
		"bar-fillgaps": "false", "bar-source": `"trade"`, "bar-transform": `"none"`,
		"base-currency": `"EUR"`, "blockchain-addresses": `{"base-currency":"base","currency":"quote","pair":"pool"}`,
		"currency": `"USD"`, "description": `""`, "dex_currency": `"USDT"`,
		"exchange-listed": `"NASDAQ"`, "exchange-traded": `"NASDAQ"`,
		"expiration": "20260415", "fractional": "false", "has-daily": "true",
		"has-intraday": "true", "has-no-volume": "false", "has-weekly-and-monthly": "false",
		"is-cfd": "false", "isin": `"US0378331005"`, "lotsize": "100",
		"minmovement": "1", "minmovement2": "0", "option-product": `{"code":"VIX","description":"Volatility"}`,
		"option-style": `"european"`, "option-type": `"call"`, "pointvalue": "0.00001", "pricescale": "100",
		"root": `"VIX"`, "root-description": `"Volatility"`, "session-extended": `"0400-2000"`,
		"session-postmarket": `"1600-2000"`, "session-premarket": `"0400-0930"`, "session-regular": `"0930-1600"`,
		"strike": "23.5", "ticker": `"AAPL"`, "timezone": `"America/New_York"`, "type": `"stock"`,
		"typespecs": `"crypto"`, "underlying-symbol": `"PREFIX:VIX"`, "volume-type": `"base"`, "wkn": `"A12ABB"`,
	}
	if len(fields) != 40 {
		t.Fatalf("metadata fixture count=%d, want 40 plus symbol", len(fields))
	}
	for name, factory := range symbolInfoModelFactories() {
		t.Run(name, func(t *testing.T) {
			for field, value := range fields {
				t.Run(field, func(t *testing.T) {
					model := factory()
					input := `{"s":"ok","symbol":["AAPL","MSFT"],"` + field + `":` + value + `}`
					if err := json.Unmarshal([]byte(input), model); err != nil {
						t.Fatal(err)
					}
					encoded, err := json.Marshal(model)
					if err != nil {
						t.Fatal(err)
					}
					var result map[string]json.RawMessage
					if err := json.Unmarshal(encoded, &result); err != nil {
						t.Fatal(err)
					}
					want := `[` + value + `,` + value + `]`
					var actualValue, expectedValue any
					if err := json.Unmarshal(result[field], &actualValue); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(want), &expectedValue); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(actualValue, expectedValue) {
						t.Fatalf("%s=%s, want %s", field, result[field], want)
					}
				})
			}
		})
	}
}

func TestSymbolInfoScalarSymbol(t *testing.T) {
	for name, factory := range symbolInfoModelFactories() {
		t.Run(name, func(t *testing.T) {
			model := factory()
			if err := json.Unmarshal([]byte(`{"s":"ok","symbol":"AAPL","pricescale":100,"description":"Apple"}`), model); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(model)
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range []string{`"symbol":["AAPL"]`, `"pricescale":[100]`, `"description":["Apple"]`} {
				if !bytes.Contains(encoded, []byte(fragment)) {
					t.Fatalf("missing %s in %s", fragment, encoded)
				}
			}
		})
	}
}

func TestSymbolInfoArrayNullAndOmissionSemantics(t *testing.T) {
	const input = `{"s":"ok","symbol":["AAPL","MSFT"],"pricescale":[100],"currency":null,"root":null,"expiration":[null,20260415],"typespecs":[null,"crypto"],"session-premarket":[null,"0400-0930"],"has-daily":[]}`
	for name, factory := range symbolInfoModelFactories() {
		t.Run(name, func(t *testing.T) {
			model := factory()
			if err := json.Unmarshal([]byte(input), model); err != nil {
				t.Fatal(err)
			}
			value := reflect.ValueOf(model).Elem()
			if !value.FieldByName("Currency").IsNil() || !value.FieldByName("Root").IsNil() || !value.FieldByName("Fractional").IsNil() {
				t.Fatal("null or omitted pointer field changed")
			}
			daily := value.FieldByName("HasDaily")
			if daily.IsNil() || daily.Elem().Len() != 0 {
				t.Fatal("explicit empty array became omitted")
			}
			encoded, err := json.Marshal(model)
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range []string{`"pricescale":[100]`, `"currency":null`, `"expiration":[null,20260415]`, `"typespecs":[null,"crypto"]`, `"session-premarket":[null,"0400-0930"]`, `"has-daily":[]`} {
				if !bytes.Contains(encoded, []byte(fragment)) {
					t.Fatalf("lost %s in %s", fragment, encoded)
				}
			}
			if bytes.Contains(encoded, []byte(`"fractional"`)) || bytes.Contains(encoded, []byte(`"root"`)) {
				t.Fatalf("optional omission changed: %s", encoded)
			}
		})
	}
}

func TestSymbolInfoInvalidCompactMetadata(t *testing.T) {
	cases := map[string]string{
		"missing symbols": `{"pricescale":100}`, "null symbols": `{"symbol":null,"pricescale":100}`,
		"empty symbols": `{"symbol":[],"pricescale":100}`, "numeric symbol": `{"symbol":42,"pricescale":100}`,
		"wrong numeric value": `{"symbol":["AAPL"],"pricescale":"100"}`,
		"wrong boolean value": `{"symbol":["AAPL"],"fractional":"false"}`,
		"wrong object value":  `{"symbol":["AAPL"],"option-product":"VIX"}`,
		"malformed JSON":      `{"symbol":["AAPL"],`, "wrong root shape": `[]`,
	}
	for modelName, factory := range symbolInfoModelFactories() {
		t.Run(modelName, func(t *testing.T) {
			for name, input := range cases {
				t.Run(name, func(t *testing.T) {
					if err := json.Unmarshal([]byte(input), factory()); err == nil {
						t.Fatalf("accepted invalid metadata: %s", input)
					}
				})
			}
			for _, input := range []string{`null`, `{}`, `{"symbol":[],"pricescale":[]}`, `{"symbol":null,"currency":null}`} {
				if err := json.Unmarshal([]byte(input), factory()); err != nil {
					t.Fatalf("added unrelated required/array validation for %s: %v", input, err)
				}
			}
		})
	}
}

func TestSymbolInfoInvalidCompactFiniteResponse(t *testing.T) {
	response, err := ParseGetSymbolInfoResponse(&http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"s":"ok","symbol":[],"pricescale":100}`)),
	})
	var protocolErr *ProtocolError
	if response != nil || !errors.As(err, &protocolErr) || !strings.Contains(err.Error(), "pricescale") {
		t.Fatalf("missing contextual compact metadata error: %v", err)
	}
}

func TestSymbolInfoCompactMetadataPreservesPropertyOrder(t *testing.T) {
	cases := []struct {
		name, input string
		pricescale  []float64
		symbol      []string
	}{
		{"canonical array last", `{"symbol":["AAPL","MSFT"],"Pricescale":[200,200],"pricescale":[100,100],"description":"shared"}`, []float64{100, 100}, []string{"AAPL", "MSFT"}},
		{"folded array last", `{"symbol":["AAPL","MSFT"],"pricescale":[100,100],"Pricescale":[200,200],"description":"shared"}`, []float64{200, 200}, []string{"AAPL", "MSFT"}},
		{"canonical scalar last", `{"symbol":["AAPL","MSFT"],"Pricescale":[200,200],"pricescale":100}`, []float64{100, 100}, []string{"AAPL", "MSFT"}},
		{"folded array after scalar", `{"symbol":["AAPL","MSFT"],"pricescale":100,"Pricescale":[200,200]}`, []float64{200, 200}, []string{"AAPL", "MSFT"}},
		{"duplicate canonical arrays", `{"symbol":["AAPL","MSFT"],"pricescale":[100,100],"pricescale":[200,200],"description":"shared"}`, []float64{200, 200}, []string{"AAPL", "MSFT"}},
		{"duplicate canonical scalar then array", `{"symbol":["AAPL","MSFT"],"pricescale":100,"pricescale":[200,200]}`, []float64{200, 200}, []string{"AAPL", "MSFT"}},
		{"duplicate canonical array then scalar", `{"symbol":["AAPL","MSFT"],"pricescale":[200,200],"pricescale":100}`, []float64{100, 100}, []string{"AAPL", "MSFT"}},
		{"duplicate canonical scalars", `{"symbol":["AAPL","MSFT"],"pricescale":200,"pricescale":100}`, []float64{100, 100}, []string{"AAPL", "MSFT"}},
		{"canonical scalar symbol last", `{"Symbol":["MSFT"],"symbol":"AAPL","pricescale":100}`, []float64{100}, []string{"AAPL"}},
		{"folded symbol array last", `{"symbol":"AAPL","Symbol":["MSFT"],"pricescale":100}`, []float64{100}, []string{"MSFT"}},
		{"duplicate scalar symbols", `{"symbol":"MSFT","symbol":"AAPL","pricescale":100}`, []float64{100}, []string{"AAPL"}},
	}
	for modelName, factory := range symbolInfoModelFactories() {
		t.Run(modelName, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					model := factory()
					if err := json.Unmarshal([]byte(tc.input), model); err != nil {
						t.Fatal(err)
					}
					value := reflect.ValueOf(model).Elem()
					if !reflect.DeepEqual(value.FieldByName("Pricescale").Interface(), tc.pricescale) || !reflect.DeepEqual(value.FieldByName("Symbol").Interface(), tc.symbol) {
						t.Fatalf("property precedence changed: %+v", model)
					}
				})
			}
		})
	}
}

func TestNormalizeSymbolInfoChangesOnlyCompactValueSpans(t *testing.T) {
	const input = " {\n \"symbol\" : \"AAPL\", \"description\" : \"Apple\\tInc\", \"pricescale\" : 100 , \"Pricescale\":[200], \"currency\":null, \"broker-extension\":{\"x\":1,\"x\":2}, \"description\" : [\"last\"]\n} "
	const want = " {\n \"symbol\" : [\"AAPL\"], \"description\" : [\"Apple\\tInc\"], \"pricescale\" : [100] , \"Pricescale\":[200], \"currency\":null, \"broker-extension\":{\"x\":1,\"x\":2}, \"description\" : [\"last\"]\n} "
	output, err := normalizeSymbolInfoJSON([]byte(input), []string{"symbol", "description", "pricescale", "currency"})
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != want {
		t.Fatalf("normalization changed untouched bytes:\ngot  %s\nwant %s", output, want)
	}
}

func TestSymbolInfoRejectsCompactAmplification(t *testing.T) {
	symbols := strings.Repeat(`"X",`, 2047) + `"X"`
	scalar := `"` + strings.Repeat("A", 32768) + `"`
	payload := []byte(`{"s":"ok","symbol":[` + symbols + `],"description":` + scalar + `}`)
	response, err := ParseGetSymbolInfoResponse(&http.Response{
		StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(bytes.NewReader(payload)),
	})
	var protocolErr *ProtocolError
	var parserSizeErr *SymbolInfoSizeError
	if response != nil || !errors.As(err, &protocolErr) || !errors.As(err, &parserSizeErr) {
		t.Fatalf("finite parser bypassed expansion preflight: %v", err)
	}
	for name, factory := range symbolInfoModelFactories() {
		t.Run(name, func(t *testing.T) {
			var sizeErr *SymbolInfoSizeError
			if err := json.Unmarshal(payload, factory()); !errors.As(err, &sizeErr) || sizeErr.Limit != MaxSymbolInfoExpandedBytes {
				t.Fatalf("unbounded compact expansion: %v", err)
			}
		})
	}
	// A collection of individually allowed scalars must share one budget.
	symbols = strings.Repeat(`"X",`, 1023) + `"X"`
	scalar = `"` + strings.Repeat("A", 9000) + `"`
	payload = []byte(`{"symbol":[` + symbols + `],"description":` + scalar + `,"description":` + scalar + `}`)
	var sizeErr *SymbolInfoSizeError
	if _, err := normalizeSymbolInfoJSON(payload, []string{"symbol", "description"}); !errors.As(err, &sizeErr) {
		t.Fatalf("duplicate expansion escaped budget: %v", err)
	}
}

func TestSymbolInfoExpansionArithmetic(t *testing.T) {
	for _, tc := range []struct {
		remaining  int64
		count, raw int
	}{
		{remaining: math.MaxInt64, count: math.MaxInt, raw: math.MaxInt},
		{remaining: MaxSymbolInfoExpandedBytes, count: math.MaxInt, raw: 1},
		{remaining: -1, count: 1, raw: 1}, {remaining: 66, count: 1, raw: 1},
	} {
		if cost, ok := symbolInfoExpansionCost(tc.remaining, tc.count, tc.raw); ok {
			t.Fatalf("accepted oversized arithmetic: %+v cost=%d", tc, cost)
		}
	}
	if cost, ok := symbolInfoExpansionCost(68, 1, 1); !ok || cost != 68 {
		t.Fatalf("boundary cost=%d ok=%v", cost, ok)
	}
}

func TestSymbolInfoRejectsTooManySymbols(t *testing.T) {
	payload := []byte(`{"symbol":[` + strings.Repeat(`"X",`, MaxSymbolInfoSymbols) + `"X"]}`)
	for name, factory := range symbolInfoModelFactories() {
		t.Run(name, func(t *testing.T) {
			var sizeErr *SymbolInfoSizeError
			if err := json.Unmarshal(payload, factory()); !errors.As(err, &sizeErr) {
				t.Fatalf("accepted excessive symbols: %v", err)
			}
		})
	}
}

func TestSymbolInfoCountLimitCoversFoldedAndDuplicateKeys(t *testing.T) {
	oversized := `[` + strings.Repeat(`"X",`, MaxSymbolInfoSymbols) + `"X"]`
	for _, key := range []string{"symbol", "Symbol", "SYMBOL", "ſymbol"} {
		for _, payload := range []string{
			`{"` + key + `":` + oversized + `}`,
			`{"symbol":["X"],"` + key + `":` + oversized + `,"description":"shared"}`,
			`{"` + key + `":` + oversized + `,"symbol":["X"],"description":"shared"}`,
		} {
			for name, factory := range symbolInfoModelFactories() {
				t.Run(name+"/"+key, func(t *testing.T) {
					var sizeErr *SymbolInfoSizeError
					if err := json.Unmarshal([]byte(payload), factory()); !errors.As(err, &sizeErr) {
						t.Fatalf("folded or overwritten array escaped cap: %v", err)
					}
				})
			}
		}
	}
	// Folded nulls and arrays retain encoding/json's final-key precedence;
	// scalar compact expansion still uses the last exact canonical key only.
	for _, payload := range []string{
		`{"Symbol":null}`, `{"ſymbol":["X"]}`,
		`{"symbol":"X","Symbol":["Y"],"description":"shared"}`,
		`{"Symbol":["Y"],"symbol":"X","description":"shared"}`,
		`{"symbol":"X","Symbol":null,"description":"shared"}`,
	} {
		for name, factory := range symbolInfoModelFactories() {
			t.Run(name+"/control", func(t *testing.T) {
				if err := json.Unmarshal([]byte(payload), factory()); err != nil {
					t.Fatalf("changed ordinary folded/null/scalar behavior: %v", err)
				}
			})
		}
	}
}
