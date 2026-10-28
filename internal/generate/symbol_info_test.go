package generate

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestRewriteSymbolInfoDecodersPreservesFields(t *testing.T) {
	source := []byte("package tradeview\nimport \"encoding/json\"\ntype SymbolInfoArrays struct { Symbol []string `json:\"symbol\"`; Pricescale []float64 `json:\"pricescale\"`; HasDaily *[]bool `json:\"has-daily,omitempty\"` }\ntype SymbolInfoResponse struct { S string `json:\"s\"`; Symbol []string `json:\"symbol\"`; Pricescale []float64 `json:\"pricescale\"` }\n")
	output, err := rewriteSymbolInfoDecoders(source, []string{"SymbolInfoArrays", "SymbolInfoResponse"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "api.gen.go", output, 0); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"Pricescale []float64", "HasDaily   *[]bool",
		"func (value *SymbolInfoArrays) UnmarshalJSON", "func (value *SymbolInfoResponse) UnmarshalJSON",
		`"has-daily",`, `"pricescale",`, `"symbol",`,
		"return json.Unmarshal(normalized, (*plain)(value))",
	} {
		if !strings.Contains(string(output), fragment) {
			t.Fatalf("missing %q in generated decoder:\n%s", fragment, output)
		}
	}
	if strings.Contains(string(output), `"s",`) {
		t.Fatal("status was treated as scalar metadata")
	}
	again, err := rewriteSymbolInfoDecoders(source, []string{"SymbolInfoArrays", "SymbolInfoResponse"})
	if err != nil || string(again) != string(output) {
		t.Fatalf("decoder generation is not reproducible: %v", err)
	}
}

func TestRewriteSymbolInfoDecodersRejectsStaleOverlay(t *testing.T) {
	for _, source := range []string{"package tradeview", "package tradeview; type SymbolInfoArrays string", "package tradeview; type SymbolInfoArrays struct { Pricescale []float64 }"} {
		if _, err := rewriteSymbolInfoDecoders([]byte(source), []string{"SymbolInfoArrays"}); err == nil {
			t.Fatalf("accepted stale overlay against %s", source)
		}
	}
}
