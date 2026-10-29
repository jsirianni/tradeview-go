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

func TestSymbolInfoAliasesUseFinalSymbolCount(t *testing.T) {
	cases := []struct {
		name, properties string
		want             []string
	}{
		{"folded last", `"symbol":["AAPL"],"Symbol":["MSFT","GOOG"]`, []string{"MSFT", "GOOG"}},
		{"canonical last", `"Symbol":["MSFT","GOOG"],"symbol":["AAPL"]`, []string{"AAPL"}},
		{"folded only", `"SYMBOL":["MSFT","GOOG"]`, []string{"MSFT", "GOOG"}},
		{"unicode folded", `"symbol":["AAPL"],"ſymbol":["MSFT","GOOG"]`, []string{"MSFT", "GOOG"}},
		{"escaped folded", `"symbol":["AAPL"],"\u0053ymbol":["MSFT","GOOG"]`, []string{"MSFT", "GOOG"}},
		{"duplicate canonical", `"symbol":["AAPL"],"symbol":["MSFT","GOOG"]`, []string{"MSFT", "GOOG"}},
		{"canonical scalar last", `"Symbol":["MSFT","GOOG"],"symbol":"AAPL"`, []string{"AAPL"}},
		{"folded scalar last", `"symbol":["MSFT","GOOG"],"Symbol":"AAPL"`, []string{"AAPL"}},
		{"unicode scalar only", `"ſymbol":"AAPL"`, []string{"AAPL"}},
		{"folded array after scalar", `"symbol":"AAPL","Symbol":["MSFT","GOOG"]`, []string{"MSFT", "GOOG"}},
		{"null overwritten", `"symbol":null,"Symbol":["MSFT","GOOG"]`, []string{"MSFT", "GOOG"}},
		{"empty overwritten", `"Symbol":[],"symbol":["MSFT","GOOG"]`, []string{"MSFT", "GOOG"}},
	}
	// Exercise numeric, string, boolean, and object compact fields on either
	// side of the symbol properties. Every expansion must use the final count.
	const metadata = `"pricescale":100,"description":"shared","fractional":false,"option-product":{"code":"VIX","description":"Volatility"}`
	for modelName, factory := range symbolInfoModelFactories() {
		for _, tc := range cases {
			for _, before := range []bool{false, true} {
				position := "/metadata after"
				if before {
					position = "/metadata before"
				}
				t.Run(modelName+"/"+tc.name+position, func(t *testing.T) {
					properties := tc.properties + "," + metadata
					if before {
						properties = metadata + "," + tc.properties
					}
					model := factory()
					if err := json.Unmarshal([]byte(`{"s":"ok",`+properties+`}`), model); err != nil {
						t.Fatal(err)
					}
					if got := reflect.ValueOf(model).Elem().FieldByName("Symbol").Interface(); !reflect.DeepEqual(got, tc.want) {
						t.Fatalf("symbols = %v, want %v", got, tc.want)
					}
					encoded, err := json.Marshal(model)
					if err != nil {
						t.Fatal(err)
					}
					var object map[string]json.RawMessage
					if err := json.Unmarshal(encoded, &object); err != nil {
						t.Fatal(err)
					}
					for field, want := range map[string]string{
						"pricescale": `100`, "description": `"shared"`, "fractional": `false`,
						"option-product": `{"code":"VIX","description":"Volatility"}`,
					} {
						var values []json.RawMessage
						if err := json.Unmarshal(object[field], &values); err != nil {
							t.Fatal(err)
						}
						if len(values) != len(tc.want) {
							t.Fatalf("%s has %d values for %d symbols", field, len(values), len(tc.want))
						}
						for _, value := range values {
							if string(value) != want {
								t.Fatalf("%s = %s, want %s", field, value, want)
							}
						}
					}
				})
			}
		}
	}
}

func TestSymbolInfoAliasesClearSymbols(t *testing.T) {
	for modelName, factory := range symbolInfoModelFactories() {
		for _, properties := range []string{
			`"symbol":["AAPL"],"Symbol":null`, `"symbol":["AAPL"],"ſymbol":[]`,
			`"Symbol":["AAPL"],"symbol":null`, `"Symbol":["AAPL"],"symbol":[]`,
			`"Symbol":null`, `"SYMBOL":[]`,
		} {
			t.Run(modelName+"/"+properties, func(t *testing.T) {
				model := factory()
				if err := json.Unmarshal([]byte(`{`+properties+`}`), model); err != nil {
					t.Fatal(err)
				}
				if got := reflect.ValueOf(model).Elem().FieldByName("Symbol").Len(); got != 0 {
					t.Fatalf("symbol count = %d, want 0", got)
				}
				if err := json.Unmarshal([]byte(`{"pricescale":100,`+properties+`}`), factory()); err == nil {
					t.Fatal("accepted compact metadata without final symbols")
				}
			})
		}
		for _, properties := range []string{
			`"Symbol":42`, `"symbol":["AAPL"],"Symbol":{}`,
			`"Symbol":[42],"symbol":["AAPL"]`,
		} {
			if err := json.Unmarshal([]byte(`{`+properties+`,"pricescale":100}`), factory()); err == nil {
				t.Fatalf("%s accepted wrong symbol type: %s", modelName, properties)
			}
		}
	}
}

func TestSymbolInfoAliasesPreserveRawResponse(t *testing.T) {
	const payload = " {\n \"s\":\"ok\", \"symbol\":\"AAPL\", \"pricescale\":100, \"\\u0053ymbol\":[\"MSFT\",\"GOOG\"], \"broker-extension\":{\"x\":1,\"x\":2}\n} "
	const normalized = " {\n \"s\":\"ok\", \"symbol\":[\"AAPL\"], \"pricescale\":[100,100], \"\\u0053ymbol\":[\"MSFT\",\"GOOG\"], \"broker-extension\":{\"x\":1,\"x\":2}\n} "
	got, err := normalizeSymbolInfoJSON([]byte(payload), []string{"symbol", "pricescale"})
	if err != nil || string(got) != normalized {
		t.Fatalf("normalization = %s, error = %v", got, err)
	}
	response, err := ParseGetSymbolInfoResponse(&http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := response.JSON200.AsSymbolInfoResponse()
	if err != nil || !reflect.DeepEqual(model.Pricescale, []float64{100, 100}) || !reflect.DeepEqual(model.Symbol, []string{"MSFT", "GOOG"}) {
		t.Fatalf("model = %+v, error = %v", model, err)
	}
	union, err := response.JSON200.MarshalJSON()
	if err != nil || string(union) != strings.TrimSpace(payload) || string(response.Body) != payload {
		t.Fatalf("response or union changed raw bytes: %s, %v", union, err)
	}
}

func TestSymbolInfoAliasExpansionBudget(t *testing.T) {
	symbols := `[` + strings.Repeat(`"X",`, 2047) + `"X"]`
	scalar := `"` + strings.Repeat("A", 32768) + `"`
	// The final alias's larger count must be used in the expansion preflight,
	// even when a smaller canonical count exists or the metadata comes first.
	for _, properties := range []string{
		`"symbol":["X"],"Symbol":` + symbols + `,"description":` + scalar,
		`"description":` + scalar + `,"ſymbol":` + symbols,
	} {
		payload := []byte(`{` + properties + `}`)
		for name, factory := range symbolInfoModelFactories() {
			t.Run(name, func(t *testing.T) {
				var sizeErr *SymbolInfoSizeError
				if err := json.Unmarshal(payload, factory()); !errors.As(err, &sizeErr) {
					t.Fatalf("alias expansion escaped budget: %v", err)
				}
			})
		}
	}
	// The final symbols do not remove the existing cost of overwritten scalar
	// metadata: both expansions share one budget, and raw input stays untouched.
	payload := []byte(`{"Symbol":[` + strings.Repeat(`"X",`, 1023) + `"X"],"description":"` + strings.Repeat("A", 9000) + `","description":"` + strings.Repeat("A", 9000) + `"}`)
	original := bytes.Clone(payload)
	var sizeErr *SymbolInfoSizeError
	if _, err := normalizeSymbolInfoJSON(payload, []string{"symbol", "description"}); !errors.As(err, &sizeErr) || !bytes.Equal(payload, original) {
		t.Fatalf("duplicate alias expansion lost budget or input: %v", err)
	}
}
