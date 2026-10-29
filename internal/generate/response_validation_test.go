package generate

import (
	"fmt"
	"strings"
	"testing"
)

func TestResponseValidationGeneration(t *testing.T) {
	var source strings.Builder
	source.WriteString("package tradeview\nfunc (u Union) AsSuccessResponse() (SuccessResponse,error) { return SuccessResponse{},nil }\nfunc (u Union) AsErrorResponse() (ErrorResponse,error) {return ErrorResponse{},nil}\n")
	for index := range 38 {
		fmt.Fprintf(&source, "func ParseOperation%dResponse() { var dest Union; _ = dest }\n", index)
	}
	output, err := rewriteResponseValidation([]byte(source.String()))
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if strings.Count(text, "validateFiniteResponseJSON[Union](bodyBytes)") != 38 || !strings.Contains(text, "responseJSONTypeOf[Union](): {responseJSONTypeOf[SuccessResponse]()}") || strings.Contains(text, "responseJSONTypeOf[ErrorResponse]()") {
		t.Fatalf("incomplete validation generation: %s", text)
	}
	if _, err := rewriteResponseValidation([]byte("package tradeview\nfunc ParseBrokenResponse() {}")); err == nil {
		t.Fatal("accepted missing destination")
	}
	if _, err := rewriteResponseValidation([]byte("package tradeview")); err == nil {
		t.Fatal("accepted missing finite parsers")
	}
}
