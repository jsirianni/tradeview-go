package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestRewriteClientPreservesRawStreamsAndRemovesFiniteReaders(t *testing.T) {
	input := []byte(`package tradeview
 type Client struct{}
 type ClientWithResponses struct{}
 type StreamOrdersResponse struct{}
 type StreamOrders200JSONResponseBody struct{}
 type GetAccountsResponse struct{}
 type CancelAllOrdersFormdataBody struct{}
 type CancelAllOrdersFormdataRequestBody CancelAllOrdersFormdataBody
 type ClientWithResponsesInterface interface {
  StreamOrdersWithResponse() (*StreamOrdersResponse,error)
  GetAccountsWithResponse() (*GetAccountsResponse,error)
 }
 func (c *Client) StreamOrders() {}
 func (c *ClientWithResponses) StreamOrdersWithResponse() (*StreamOrdersResponse,error) {return nil,nil}
 func (r StreamOrdersResponse) Status() string {return ""}
 func ParseStreamOrdersResponse(rsp *http.Response) (*StreamOrdersResponse,error) {return nil,nil}
 func ParseGetAccountsResponse(rsp *http.Response) (*GetAccountsResponse,error) {
  bodyBytes,err := io.ReadAll(rsp.Body)
  if err != nil {return nil,err}
  response := &GetAccountsResponse{}
  return response,nil
 }
 `)
	output, err := rewriteClient(input, map[string]bool{"StreamOrders": true})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), "api.gen.go", output, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundRaw, foundUnion, foundFinite := false, false, false
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncDecl:
			if n.Name.Name == "StreamOrders" {
				foundRaw = true
			}
			if n.Name.Name == "ParseGetAccountsResponse" {
				foundFinite = true
			}
			if strings.Contains(n.Name.Name, "StreamOrders") && n.Name.Name != "StreamOrders" {
				t.Errorf("finite stream function survived: %s", n.Name.Name)
			}
		case *ast.TypeSpec:
			if n.Name.Name == "CancelAllOrdersFormdataRequestBody" && !n.Assign.IsValid() {
				t.Error("request body wrapper lost union JSON methods")
			}
			if n.Name.Name == "StreamOrders200JSONResponseBody" {
				foundUnion = true
			}
			if n.Name.Name == "StreamOrdersResponse" {
				t.Error("finite stream response survived")
			}
		}
		return true
	})
	if !foundRaw || !foundUnion || !foundFinite {
		t.Fatalf("lost raw stream=%v, stream union=%v, finite parser=%v", foundRaw, foundUnion, foundFinite)
	}
	if strings.Count(string(output), "responseError(bodyBytes, rsp)") != 1 {
		t.Fatalf("missing finite error-envelope check:\n%s", output)
	}
	if strings.Contains(string(output), "StreamOrdersWithResponse") {
		t.Error("finite stream interface method survived")
	}
}

func TestRewriteClientProtectsDotPathIdentifiers(t *testing.T) {
	input := []byte(`package tradeview
 func NewGetOrdersRequest(pathParam0, pathParam1 string) string {
  return fmt.Sprintf("/accounts/%s/orders/%s", pathParam0, pathParam1)
 }
 `)
	output, err := rewriteClient(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := `fmt.Sprintf("/accounts/%s/orders/%s", escapeDotPathSegment(pathParam0), escapeDotPathSegment(pathParam1))`
	if !strings.Contains(string(output), expected) {
		t.Fatalf("path parameters lack dot-segment protection:\n%s", output)
	}
}
