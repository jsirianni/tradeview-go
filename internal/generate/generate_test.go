package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
  switch {
  case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
   var dest GetAccounts200JSONResponseBody
   if err := json.Unmarshal(bodyBytes, &dest); err != nil {return nil,err}
   response.JSON200 = &dest
  }
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
	if strings.Count(string(output), "finiteResponseError(bodyBytes, rsp)") != 1 {
		t.Fatalf("missing finite response validation:\n%s", output)
	}
	if !strings.Contains(string(output), "newProtocolError(bodyBytes, rsp, err)") || strings.Contains(string(output), `strings.Contains(rsp.Header.Get("Content-Type"), "json")`) {
		t.Fatalf("finite parser retained unsafe media/decode handling:\n%s", output)
	}
	if strings.Contains(string(output), "StreamOrdersWithResponse") {
		t.Error("finite stream interface method survived")
	}
}

func TestRewriteClientRejectsUnknownFiniteParserShape(t *testing.T) {
	input := []byte(`package tradeview
 func ParseGetAccountsResponse(rsp *http.Response) (*GetAccountsResponse,error) {
  bodyBytes,err := io.ReadAll(rsp.Body)
  if err != nil {return nil,err}
  response := &GetAccountsResponse{}
  switch {
  case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 201:
   var dest GetAccounts200JSONResponseBody
   if err := json.Unmarshal(bodyBytes, &dest); err != nil {return nil,err}
   response.JSON200 = &dest
  }
  return response,nil
 }
 `)
	if _, err := rewriteClient(input, nil); err == nil || !strings.Contains(err.Error(), "unsupported finite parser shape") {
		t.Fatalf("accepted a new response contract without updating finite validation: %v", err)
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

var writeBindings = flag.Bool("write", false, "regenerate committed broker bindings")

// TestRegenerateBindings writes generated source only when explicitly requested
// by go generate. The ordinary test suite keeps the checkout read-only.
func TestRegenerateBindings(t *testing.T) {
	if !*writeBindings {
		t.Skip("regeneration requires -write; use go generate ./...")
	}
	if err := Generate(filepath.Join("..", "..")); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateChecksExplicitRootBeforeWriting(t *testing.T) {
	root := t.TempDir()
	api := filepath.Join(root, "api")
	if err := os.MkdirAll(api, 0755); err != nil {
		t.Fatal(err)
	}
	schema := []byte("openapi: 3.0.0\n")
	if err := os.WriteFile(filepath.Join(api, "broker-rest-api.yaml"), schema, 0644); err != nil {
		t.Fatal(err)
	}
	wrongDigest := sha256.Sum256([]byte("different schema"))
	metadata, err := json.Marshal(map[string]string{"version": "1.21.0", "sha256": hex.EncodeToString(wrongDigest[:])})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(api, "upstream.json"), metadata, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	err = Generate(root)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Generate(root) error = %v, want checksum mismatch from the supplied root", err)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("Generate changed working directory: %s -> %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(root, "pkg")); !os.IsNotExist(err) {
		t.Fatalf("Generate wrote package output before source verification: %v", err)
	}
}

func TestRewriteClientProtectsDefaultRedirects(t *testing.T) {
	input := []byte(`package tradeview
    func NewClient() *Client {
        client := &Client{}
        if client.Client == nil { client.Client = &http.Client{} }
        return client
    }
    func callerClient() *http.Client { return &http.Client{} }
    `)
	output, err := rewriteClient(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "client.Client = defaultHTTPClient()") {
		t.Fatalf("missing safe default initializer:\n%s", output)
	}
	if !strings.Contains(string(output), "return &http.Client{}") {
		t.Fatal("changed unrelated caller client")
	}
	_, err = rewriteClient([]byte(`package tradeview; func NewClient() {}`), nil)
	if err == nil {
		t.Fatal("accepted missing generator default initializer")
	}
}
