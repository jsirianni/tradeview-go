package generate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

type overlay struct {
	NumberFormat string `yaml:"number-format"`
	CSV          []struct {
		Operation string `yaml:"operation"`
		Name      string `yaml:"name"`
	} `yaml:"csv-query-parameters"`
	MoveItems              []string          `yaml:"move-parameter-items-to-schema"`
	SchemaNames            map[string]string `yaml:"schema-go-names"`
	SymbolInfoScalarModels []string          `yaml:"symbol-info-scalar-models"`
}
type operation struct {
	ID          string `json:"operationId"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	RawMethod   string `json:"rawMethod"`
	TypedMethod string `json:"typedMethod"`
	Streaming   bool   `json:"streaming"`
	Test        string `json:"test"`
}

// Generate applies the pinned schema rules and regenerates the broker package.
// Root is the repository directory; generation does not change the process's
// working directory or fetch a new API schema.
func Generate(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	schemaPath := filepath.Join(root, "api", "broker-rest-api.yaml")
	// #nosec G304 -- The developer selects the trusted repository root; this input name is fixed.
	source, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}
	var metadata struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	}
	metadataPath := filepath.Join(root, "api", "upstream.json")
	// #nosec G304 -- The developer selects the trusted repository root; this metadata name is fixed.
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return fmt.Errorf("decode upstream metadata %q: %w", metadataPath, err)
	}
	digest := sha256.Sum256(source)
	if hex.EncodeToString(digest[:]) != metadata.SHA256 {
		return errors.New("upstream schema checksum mismatch")
	}
	var schema map[string]any
	if err := yaml.Unmarshal(source, &schema); err != nil {
		return fmt.Errorf("decode schema %q: %w", schemaPath, err)
	}
	info, err := object(schema["info"], "schema info")
	if err != nil {
		return err
	}
	if info["version"] != metadata.Version {
		return errors.New("upstream schema version mismatch")
	}
	var rules overlay
	overlayPath := filepath.Join(root, "api", "overlay.yaml")
	// #nosec G304 -- The developer selects the trusted repository root; this overlay name is fixed.
	data, err := os.ReadFile(overlayPath)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return fmt.Errorf("decode overlay %q: %w", overlayPath, err)
	}
	applyNumbers(schema, rules.NumberFormat)
	components, err := object(schema["components"], "schema components")
	if err != nil {
		return err
	}
	parameters, err := object(components["parameters"], "schema components.parameters")
	if err != nil {
		return err
	}
	for _, name := range rules.MoveItems {
		parameter, err := object(parameters[name], "overlay move-parameter-items-to-schema parameter "+name)
		if err != nil {
			return err
		}
		if items, ok := parameter["items"]; ok {
			parameterSchema, err := object(parameter["schema"], "overlay move-parameter-items-to-schema parameter "+name+" schema")
			if err != nil {
				return err
			}
			parameterSchema["items"] = items
			delete(parameter, "items")
		} else {
			return fmt.Errorf("overlay parameter %s no longer needs items repair", name)
		}
	}
	schemas, err := object(components["schemas"], "schema components.schemas")
	if err != nil {
		return err
	}
	for component, name := range rules.SchemaNames {
		componentSchema, err := object(schemas[component], "overlay schema-go-names component "+component)
		if err != nil {
			return err
		}
		componentSchema["x-go-name"] = name
	}
	var operations []operation
	streamNames := make(map[string]bool)
	paths, err := object(schema["paths"], "schema paths")
	if err != nil {
		return err
	}
	for path, pathValue := range paths {
		pathItem, err := object(pathValue, "schema path "+path)
		if err != nil {
			return err
		}
		for method, value := range pathItem {
			if !strings.Contains(" get post put patch delete head options trace ", " "+method+" ") {
				continue
			}
			op, err := object(value, "schema operation "+method+" "+path)
			if err != nil {
				return err
			}
			id, ok := op["operationId"].(string)
			if !ok || strings.TrimSpace(id) == "" {
				return fmt.Errorf("schema operation %s %s requires a nonempty operationId", method, path)
			}
			for _, rule := range rules.CSV {
				if rule.Operation == id {
					found := false
					params, ok := op["parameters"].([]any)
					if !ok {
						return fmt.Errorf("overlay csv-query-parameters operation %s parameters must be an array", id)
					}
					for _, paramValue := range params {
						param, err := object(paramValue, "overlay csv-query-parameters operation "+id+" parameter "+rule.Name)
						if err != nil {
							return err
						}
						if param["name"] == rule.Name {
							param["style"] = "form"
							param["explode"] = false
							found = true
						}
					}
					if !found {
						return fmt.Errorf("overlay CSV parameter %s/%s not found", id, rule.Name)
					}
				}
			}
			name := strings.ToUpper(id[:1]) + id[1:]
			streaming := id == "streaming" || strings.HasPrefix(id, "stream")
			rawName := name
			if requestBody, ok := op["requestBody"].(map[string]any); ok {
				if content, ok := requestBody["content"].(map[string]any); ok {
					if _, ok := content["application/x-www-form-urlencoded"]; ok {
						rawName += "WithFormdataBody"
					}
				}
			}
			typed := rawName + "WithResponse"
			if streaming {
				streamNames[name] = true
				typed = "Open" + name
			}
			operations = append(operations, operation{ID: id, Method: strings.ToUpper(method), Path: path, RawMethod: rawName, TypedMethod: typed, Streaming: streaming, Test: "TestOperationTransport/" + id})
		}
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].ID < operations[j].ID })
	if len(operations) != 47 || len(streamNames) != 9 {
		return fmt.Errorf("unexpected API inventory: %d operations, %d streams", len(operations), len(streamNames))
	}
	effective, err := yaml.Marshal(schema)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp("", "tradeview-openapi-*.yaml")
	if err != nil {
		return err
	}
	tempName := temporary.Name()
	defer os.Remove(tempName)
	if _, err := temporary.Write(effective); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	outputPath := filepath.Join(root, "pkg", "tradeview", "api.gen.go")
	// #nosec G301 -- Generated public source directories are intentionally readable by other local users.
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	// #nosec G204 -- Fixed executable and pinned Go tool; only the CreateTemp path varies, with no shell.
	command := exec.Command("go", "tool", "oapi-codegen", "--config", "api/oapi-codegen.yaml", tempName)
	command.Dir = root
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return err
	}
	if err := postprocess(outputPath, streamNames); err != nil {
		return err
	}
	if err := addTelemetry(outputPath, operations); err != nil {
		return err
	}
	if err := addSymbolInfoDecoders(outputPath, rules.SymbolInfoScalarModels); err != nil {
		return err
	}
	inventory, err := json.MarshalIndent(operations, "", "  ")
	if err != nil {
		return err
	}
	inventory = append(inventory, '\n')
	// #nosec G306 -- The committed operation inventory is public source metadata, intentionally mode 0644.
	if err := os.WriteFile(filepath.Join(root, "api", "operations.json"), inventory, 0644); err != nil {
		return err
	}
	fmt.Printf("Generated %d operations (%d finite, %d streaming).\n", len(operations), len(operations)-len(streamNames), len(streamNames))
	return nil
}

// object checks repository-controlled schema and overlay references before use.
func object(value any, context string) (map[string]any, error) {
	result, ok := value.(map[string]any)
	if !ok || result == nil {
		return nil, fmt.Errorf("%s must reference an object", context)
	}
	return result, nil
}

func applyNumbers(value any, format string) {
	switch v := value.(type) {
	case map[string]any:
		if v["type"] == "number" {
			v["format"] = format
		}
		for _, child := range v {
			applyNumbers(child, format)
		}
	case []any:
		for _, child := range v {
			applyNumbers(child, format)
		}
	}
}
func postprocess(path string, streamNames map[string]bool) error {
	// #nosec G304 -- Called only with the fixed generated output path beneath the trusted repository root.
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	output, err := rewriteClient(source, streamNames)
	if err != nil {
		return err
	}
	// #nosec G703 G306 -- Fixed generated output under the trusted repository root; public source is intentionally mode 0644.
	return os.WriteFile(path, output, 0644)
}
func rewriteClient(source []byte, streamNames map[string]bool) ([]byte, error) {
	source = bytes.ReplaceAll(source, []byte("runtime.MarshalForm(body, nil)"), []byte("encodeForm(body)"))
	source = bytes.ReplaceAll(source, []byte("strings.NewReader(bodyStr.Encode())"), []byte("strings.NewReader(bodyStr)"))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "api.gen.go", source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	removeName := func(name string) bool {
		for stream := range streamNames {
			if name == stream+"Response" || name == stream+"WithResponse" || name == "Parse"+stream+"Response" {
				return true
			}
		}
		return false
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.Name == "NewClient" {
				defaults := 0
				ast.Inspect(d.Body, func(node ast.Node) bool {
					value, ok := node.(*ast.UnaryExpr)
					if !ok || value.Op != token.AND {
						return true
					}
					literal, ok := value.X.(*ast.CompositeLit)
					if !ok {
						return true
					}
					selector, ok := literal.Type.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "Client" {
						return true
					}
					pkg, ok := selector.X.(*ast.Ident)
					if !ok || pkg.Name != "http" {
						return true
					}
					edits = append(edits, edit{start: fset.Position(value.Pos()).Offset, end: fset.Position(value.End()).Offset, text: "defaultHTTPClient()"})
					defaults++
					return false
				})
				if defaults != 1 {
					return nil, fmt.Errorf("expected one default HTTP client initializer, got %d", defaults)
				}
			}
			ast.Inspect(d.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Do" {
					return true
				}
				receiver, ok := selector.X.(*ast.SelectorExpr)
				if !ok || receiver.Sel.Name != "Client" {
					return true
				}
				client, ok := receiver.X.(*ast.Ident)
				if !ok || client.Name != "c" {
					return true
				}
				edits = append(edits, edit{start: fset.Position(call.Pos()).Offset, end: fset.Position(call.End()).Offset, text: "doRequest(c.Client, req)"})
				return false
			})
			removeReceiver := false
			if d.Recv != nil {
				for _, field := range d.Recv.List {
					receiver := field.Type
					if pointer, ok := receiver.(*ast.StarExpr); ok {
						receiver = pointer.X
					}
					if identifier, ok := receiver.(*ast.Ident); ok && removeName(identifier.Name) {
						removeReceiver = true
					}
				}
			}
			if removeReceiver || removeName(d.Name.Name) {
				start := fset.Position(d.Pos()).Offset
				if d.Doc != nil {
					start = fset.Position(d.Doc.Pos()).Offset
				}
				edits = append(edits, edit{start: start, end: fset.Position(d.End()).Offset, text: ""})
				continue
			}
			if strings.HasPrefix(d.Name.Name, "Parse") && strings.HasSuffix(d.Name.Name, "Response") {
				guards, cases, decoders, reads := 0, 0, 0, 0
				// Each finite parser reads bodyBytes before constructing its response wrapper.
				for _, stmt := range d.Body.List {
					if assign, ok := stmt.(*ast.AssignStmt); ok {
						for _, lhs := range assign.Lhs {
							if identifier, ok := lhs.(*ast.Ident); ok && identifier.Name == "response" {
								offset := fset.Position(stmt.Pos()).Offset
								edits = append(edits, edit{offset, offset, "if err := finiteResponseError(bodyBytes, rsp); err != nil { return nil, err }\n"})
								guards++
							}
						}
					}
				}
				ast.Inspect(d.Body, func(node ast.Node) bool {
					switch n := node.(type) {
					case *ast.CallExpr:
						if selector, ok := n.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "ReadAll" {
							if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "io" {
								edits = append(edits, edit{start: fset.Position(n.Pos()).Offset, end: fset.Position(n.End()).Offset, text: "readFiniteResponse(rsp)"})
								reads++
							}
						}
					case *ast.CaseClause:
						if len(n.List) != 1 {
							return true
						}
						var condition bytes.Buffer
						if err := format.Node(&condition, fset, n.List[0]); err != nil {
							return true
						}
						if condition.String() == `strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200` {
							// The guard uses parsed media types, including JSON suffixes and parameters.
							edits = append(edits, edit{start: fset.Position(n.List[0].Pos()).Offset, end: fset.Position(n.List[0].End()).Offset, text: "rsp.StatusCode == http.StatusOK"})
							cases++
						}
					case *ast.IfStmt:
						assignment, ok := n.Init.(*ast.AssignStmt)
						if !ok || len(assignment.Rhs) != 1 || len(n.Body.List) != 1 {
							return true
						}
						call, ok := assignment.Rhs[0].(*ast.CallExpr)
						if !ok {
							return true
						}
						selector, ok := call.Fun.(*ast.SelectorExpr)
						if !ok || selector.Sel.Name != "Unmarshal" {
							return true
						}
						pkg, ok := selector.X.(*ast.Ident)
						if !ok || pkg.Name != "json" {
							return true
						}
						result, ok := n.Body.List[0].(*ast.ReturnStmt)
						if !ok || len(result.Results) != 2 {
							return true
						}
						cause, ok := result.Results[1].(*ast.Ident)
						if !ok || cause.Name != "err" {
							return true
						}
						edits = append(edits, edit{start: fset.Position(cause.Pos()).Offset, end: fset.Position(cause.End()).Offset, text: "newProtocolError(bodyBytes, rsp, err)"})
						decoders++
					}
					return true
				})
				if guards != 1 || cases != 1 || decoders != 1 || reads != 1 {
					return nil, fmt.Errorf("unsupported finite parser shape in %s: guards=%d cases=%d decoders=%d reads=%d", d.Name.Name, guards, cases, decoders, reads)
				}
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if typ, ok := spec.(*ast.TypeSpec); ok {
					// A new defined type loses the JSON methods on generated union bodies.
					// Keep wrapper bodies as aliases so form serialization preserves variants.
					if strings.HasSuffix(typ.Name.Name, "RequestBody") && !typ.Assign.IsValid() {
						if _, ok := typ.Type.(*ast.Ident); ok {
							offset := fset.Position(typ.Type.Pos()).Offset
							edits = append(edits, edit{start: offset, end: offset, text: "= "})
						}
					}
					if removeName(typ.Name.Name) {
						start := fset.Position(d.Pos()).Offset
						if d.Doc != nil {
							start = fset.Position(d.Doc.Pos()).Offset
						}
						edits = append(edits, edit{start: start, end: fset.Position(d.End()).Offset, text: ""})
						continue
					}
					if iface, ok := typ.Type.(*ast.InterfaceType); ok {
						for _, field := range iface.Methods.List {
							for _, name := range field.Names {
								if removeName(name.Name) {
									start := fset.Position(field.Pos()).Offset
									if field.Doc != nil {
										start = fset.Position(field.Doc.Pos()).Offset
									}
									edits = append(edits, edit{start: start, end: fset.Position(field.End()).Offset, text: ""})
								}
							}
						}
					}
				}
			}
		}
	}
	// URL resolution must not interpret a caller's literal dot identifier as
	// relative-path navigation. Other path parameters are already escaped.
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Sprintf" {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "fmt" {
			return true
		}
		for _, argument := range call.Args {
			name, ok := argument.(*ast.Ident)
			if ok && strings.HasPrefix(name.Name, "pathParam") {
				edits = append(edits, edit{start: fset.Position(name.Pos()).Offset, end: fset.Position(name.End()).Offset, text: "escapeDotPathSegment(" + name.Name + ")"})
			}
		}
		return true
	})
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		source = append(append(append([]byte{}, source[:e.start]...), []byte(e.text)...), source[e.end:]...)
	}
	output, err := format.Source(source)
	if err != nil {
		return nil, fmt.Errorf("format postprocessed client: %w", err)
	}
	// Code generation must not embed the temporary schema file path.
	output = bytes.ReplaceAll(output, []byte("// Package tradeview provides primitives to interact with the openapi HTTP API."), []byte("// Package tradeview provides transport bindings for TradingView's broker REST API."))
	return output, nil
}
