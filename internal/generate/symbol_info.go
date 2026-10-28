package generate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// addSymbolInfoDecoders implements the compatibility rule without changing the
// upstream schema or the generated public slice fields.
func addSymbolInfoDecoders(path string, models []string) error {
	// #nosec G304 -- Called only with the fixed generated output path beneath the developer-selected repository root.
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	output, err := rewriteSymbolInfoDecoders(source, models)
	if err != nil {
		return err
	}
	// #nosec G703 G306 -- Fixed generated output under the trusted repository root; public source is intentionally mode 0644.
	return os.WriteFile(path, output, 0644)
}

func rewriteSymbolInfoDecoders(source []byte, models []string) ([]byte, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "api.gen.go", source, 0)
	if err != nil {
		return nil, err
	}
	structs := make(map[string]*ast.StructType)
	for _, declaration := range file.Decls {
		declaration, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range declaration.Specs {
			spec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if value, ok := spec.Type.(*ast.StructType); ok {
				structs[spec.Name.Name] = value
			}
		}
	}
	output := bytes.NewBuffer(bytes.Clone(source))
	for _, model := range models {
		value, ok := structs[model]
		if !ok {
			return nil, fmt.Errorf("symbol-info scalar model %s is not a generated struct", model)
		}
		var fields []string
		hasSymbol := false
		for _, field := range value.Fields.List {
			fieldType := field.Type
			if pointer, ok := fieldType.(*ast.StarExpr); ok {
				fieldType = pointer.X
			}
			if array, ok := fieldType.(*ast.ArrayType); !ok || array.Len != nil {
				continue
			}
			if field.Tag == nil {
				return nil, fmt.Errorf("symbol-info model %s has an untagged array", model)
			}
			tag, err := strconv.Unquote(field.Tag.Value)
			if err != nil {
				return nil, err
			}
			name := strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
			if name == "" || name == "-" {
				return nil, fmt.Errorf("symbol-info model %s has an array without a JSON name", model)
			}
			fields = append(fields, name)
			hasSymbol = hasSymbol || name == "symbol"
		}
		if !hasSymbol {
			return nil, fmt.Errorf("symbol-info model %s has no symbol array", model)
		}
		sort.Strings(fields)
		fmt.Fprintf(output, "\n// UnmarshalJSON expands compact metadata shared by every symbol.\nfunc (value *%s) UnmarshalJSON(data []byte) error {\n", model)
		fmt.Fprintf(output, "type plain %s\nnormalized, err := normalizeSymbolInfoJSON(data, []string{\n", model)
		for _, field := range fields {
			fmt.Fprintf(output, "%q,\n", field)
		}
		output.WriteString("})\nif err != nil { return err }\nreturn json.Unmarshal(normalized, (*plain)(value))\n}\n")
	}
	return format.Source(output.Bytes())
}
