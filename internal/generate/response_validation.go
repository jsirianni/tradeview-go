package generate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
)

func addResponseValidation(path string) error {
	// #nosec G304 -- Fixed generated output beneath the developer-selected repository root.
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	output, err := rewriteResponseValidation(source)
	if err != nil {
		return err
	}
	// #nosec G703 G306 -- Fixed generated public source path, intentionally mode 0644.
	return os.WriteFile(path, output, 0644)
}

// Derive model and union metadata from the generated types/accessors instead of
// maintaining a parallel schema or a handwritten list of finite operations.
func rewriteResponseValidation(source []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "api.gen.go", source, 0)
	if err != nil {
		return nil, err
	}
	unions := make(map[string][]string)
	for _, declaration := range file.Decls {
		decl, ok := declaration.(*ast.FuncDecl)
		if !ok || decl.Recv == nil || !strings.HasPrefix(decl.Name.Name, "As") || decl.Type.Results == nil || len(decl.Type.Results.List) != 2 {
			continue
		}
		receiver, ok := decl.Recv.List[0].Type.(*ast.Ident)
		if !ok {
			continue
		}
		result := decl.Type.Results.List[0].Type
		name := string(source[fset.Position(result.Pos()).Offset:fset.Position(result.End()).Offset])
		// Error envelopes are handled before success model validation; their
		// permissive shape must not mask malformed success payloads.
		if name != "ErrorResponse" {
			unions[receiver.Name] = append(unions[receiver.Name], name)
		}
	}
	type insertion struct {
		offset int
		text   string
	}
	var insertions []insertion
	for _, declaration := range file.Decls {
		decl, ok := declaration.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(decl.Name.Name, "Parse") || !strings.HasSuffix(decl.Name.Name, "Response") {
			continue
		}
		count := 0
		ast.Inspect(decl.Body, func(node ast.Node) bool {
			value, ok := node.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "dest" {
				return true
			}
			model := string(source[fset.Position(value.Type.Pos()).Offset:fset.Position(value.Type.End()).Offset])
			// Insert after the declaration, before its unmarshal and assignment.
			insertions = append(insertions, insertion{fset.Position(value.End()).Offset, fmt.Sprintf("\nif err := validateFiniteResponseJSON[%s](bodyBytes); err != nil { return nil, newProtocolError(bodyBytes, rsp, err) }\n", model)})
			count++
			return false
		})
		if count != 1 {
			return nil, fmt.Errorf("finite validation: %s has %d destination models", decl.Name.Name, count)
		}
	}
	if len(insertions) != 38 {
		return nil, fmt.Errorf("finite validation: found %d parsers, expected 38", len(insertions))
	}
	sort.Slice(insertions, func(i, j int) bool { return insertions[i].offset > insertions[j].offset })
	for _, insertion := range insertions {
		source = append(append(bytes.Clone(source[:insertion.offset]), insertion.text...), source[insertion.offset:]...)
	}
	output := bytes.NewBuffer(source)
	output.WriteString("\n// _responseJSONUnions describes the concrete models behind opaque JSON unions.\nvar _responseJSONUnions = map[responseJSONType][]responseJSONType{\n")
	names := make([]string, 0, len(unions))
	for name := range unions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(output, "responseJSONTypeOf[%s](): {", name)
		for _, variant := range unions[name] {
			fmt.Fprintf(output, "responseJSONTypeOf[%s](),", variant)
		}
		output.WriteString("},\n")
	}
	output.WriteString("}\n")
	return format.Source(output.Bytes())
}
