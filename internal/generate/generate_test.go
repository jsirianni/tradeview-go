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

	yaml "gopkg.in/yaml.v3"
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
	if strings.Contains(string(output), "io.ReadAll(rsp.Body)") || strings.Count(string(output), "readFiniteResponse(rsp)") != 1 || strings.Count(string(output), "finiteResponseError(bodyBytes, rsp)") != 1 {
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

func TestGenerateRejectsInvalidReferences(t *testing.T) {
	cases := []struct {
		name    string
		schema  string
		overlay string
		want    string
	}{
		{name: "missing parameter", overlay: "nonexistentParameter", want: "move-parameter-items-to-schema parameter nonexistentParameter"},
		{name: "missing schema", overlay: "nonexistentSchema", want: "schema-go-names component nonexistentSchema"},
		{name: "info type", schema: "info", want: "schema info"},
		{name: "components type", schema: "components", want: "schema components"},
		{name: "parameter collection type", schema: "parameters", want: "components.parameters"},
		{name: "parameter type", schema: "parameter", want: "move-parameter-items-to-schema parameter orderIds"},
		{name: "parameter schema type", schema: "parameter schema", want: "parameter orderIds schema"},
		{name: "schema collection type", schema: "schemas", want: "components.schemas"},
		{name: "component type", schema: "component", want: "schema-go-names component OrderStatus"},
		{name: "paths type", schema: "paths", want: "schema paths"},
		{name: "path type", schema: "path", want: "schema path /invalid"},
		{name: "operation type", schema: "operation", want: "schema operation get /invalid"},
		{name: "missing operation ID", schema: "missing ID", want: "nonempty operationId"},
		{name: "empty operation ID", schema: "empty ID", want: "nonempty operationId"},
		{name: "operation ID type", schema: "ID type", want: "nonempty operationId"},
		{name: "CSV parameters type", schema: "CSV parameters", want: "csv-query-parameters operation getInstruments parameters"},
		{name: "CSV parameter type", schema: "CSV parameter", want: "csv-query-parameters operation getInstruments parameter symbols"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			source, err := os.ReadFile(filepath.Join("..", "..", "api", "broker-rest-api.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			overlayData, err := os.ReadFile(filepath.Join("..", "..", "api", "overlay.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			var schema map[string]any
			if err := yaml.Unmarshal(source, &schema); err != nil {
				t.Fatal(err)
			}
			components := testObject(t, schema["components"])
			switch tc.schema {
			case "info":
				schema["info"] = false
			case "components":
				schema["components"] = false
			case "parameters":
				components["parameters"] = false
			case "parameter":
				testObject(t, components["parameters"])["orderIds"] = false
			case "parameter schema":
				testObject(t, testObject(t, components["parameters"])["orderIds"])["schema"] = false
			case "schemas":
				components["schemas"] = false
			case "component":
				testObject(t, components["schemas"])["OrderStatus"] = false
			case "paths":
				schema["paths"] = false
			case "path":
				schema["paths"] = map[string]any{"/invalid": false}
			case "operation":
				schema["paths"] = map[string]any{"/invalid": map[string]any{"get": false}}
			case "missing ID", "empty ID", "ID type", "CSV parameters", "CSV parameter":
				op := map[string]any{}
				switch tc.schema {
				case "empty ID":
					op["operationId"] = ""
				case "ID type":
					op["operationId"] = false
				case "CSV parameters":
					op["operationId"] = "getInstruments"
					op["parameters"] = false
				case "CSV parameter":
					op["operationId"] = "getInstruments"
					op["parameters"] = []any{false}
				}
				schema["paths"] = map[string]any{"/invalid": map[string]any{"get": op}}
			}
			switch tc.overlay {
			case "nonexistentParameter":
				overlayData = []byte(strings.ReplaceAll(string(overlayData), "- orderIds", "- nonexistentParameter"))
			case "nonexistentSchema":
				overlayData = []byte(strings.ReplaceAll(string(overlayData), "  OrderStatus:", "  nonexistentSchema:"))
			}
			source, err = yaml.Marshal(schema)
			if err != nil {
				t.Fatal(err)
			}
			writeGenerationInputs(t, root, source, overlayData)
			if err := Generate(root); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Generate error = %v, want %q", err, tc.want)
			}
			if _, err := os.Stat(filepath.Join(root, "pkg")); !os.IsNotExist(err) {
				t.Fatalf("invalid inputs wrote output: %v", err)
			}
		})
	}
}

func writeGenerationInputs(t *testing.T, root string, source, overlayData []byte) {
	t.Helper()
	api := filepath.Join(root, "api")
	if err := os.MkdirAll(api, 0755); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	metadata, err := json.Marshal(map[string]string{"version": "1.21.0", "sha256": hex.EncodeToString(digest[:])})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"broker-rest-api.yaml": source, "upstream.json": metadata, "overlay.yaml": overlayData} {
		if err := os.WriteFile(filepath.Join(api, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func testObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("fixture value has type %T, want object", value)
	}
	return object
}
