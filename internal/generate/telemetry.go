package generate

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
)

// addTelemetry emits shared observation hooks with static operation metadata so
// logging and telemetry never infer routes from URLs containing identifiers.
func addTelemetry(path string, operations []operation) error {
	// #nosec G304 -- Fixed generated output beneath the developer-selected repository root.
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		return err
	}
	byName := make(map[string]operation)
	for _, op := range operations {
		byName[strings.ToUpper(op.ID[:1])+op.ID[1:]] = op
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	seen := make(map[string]bool)
	parsers, clients := 0, 0
	insert := func(pos token.Pos, text string) {
		offset := fset.Position(pos).Offset
		edits = append(edits, edit{offset, offset, text})
	}
	namedResults := func(d *ast.FuncDecl) error {
		if d.Type.Results == nil || len(d.Type.Results.List) != 2 {
			return fmt.Errorf("unexpected telemetry result shape: %s", d.Name.Name)
		}
		insert(d.Type.Results.List[0].Type.Pos(), "result ")
		insert(d.Type.Results.List[1].Type.Pos(), "resultErr ")
		return nil
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				if !d.Lparen.IsValid() {
					return errors.New("generated imports must use a block")
				}
				insert(d.Lparen+1, "\n\"log/slog\"")
			}
			for _, spec := range d.Specs {
				if typ, ok := spec.(*ast.TypeSpec); ok && typ.Name.Name == "Client" {
					st, ok := typ.Type.(*ast.StructType)
					if !ok {
						return errors.New("client must be a struct")
					}
					insert(st.Fields.Closing, "\ntelemetry *telemetry\nlogger *slog.Logger\n")
					clients++
				}
			}
		case *ast.FuncDecl:
			if d.Body == nil {
				continue
			}
			receiver := ""
			if d.Recv != nil {
				if ptr, ok := d.Recv.List[0].Type.(*ast.StarExpr); ok {
					if ident, ok := ptr.X.(*ast.Ident); ok {
						receiver = ident.Name
					}
				}
			}
			if receiver == "Client" && d.Name.Name != "applyEditors" {
				name := strings.TrimSuffix(strings.TrimSuffix(d.Name.Name, "WithFormdataBody"), "WithBody")
				op, ok := byName[name]
				if !ok {
					return fmt.Errorf("missing telemetry operation for %s", d.Name.Name)
				}
				if err := namedResults(d); err != nil {
					return err
				}
				insert(d.Body.Lbrace+1, fmt.Sprintf("\nctx, observation := c.startObservation(ctx, %q, %q, %q, %t)\ndefer func() { observation.response(result, resultErr) }()\n", op.ID, op.Method, op.Path, op.Streaming))
				seen[op.ID] = true
			}
			if receiver == "ClientWithResponses" && strings.HasSuffix(d.Name.Name, "WithResponse") {
				insert(d.Body.Lbrace+1, "\nctx = typedOperationContext(ctx)\n")
			}
			if receiver == "" && strings.HasPrefix(d.Name.Name, "Parse") && strings.HasSuffix(d.Name.Name, "Response") {
				if err := namedResults(d); err != nil {
					return err
				}
				insert(d.Body.Lbrace+1, "\ndefer func() { finishResponseObservation(rsp, resultErr) }()\n")
				parsers++
			}
		}
	}
	if len(seen) != len(operations) || parsers != 38 || clients != 1 {
		return fmt.Errorf("incomplete telemetry inventory: operations=%d parsers=%d clients=%d", len(seen), parsers, clients)
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		source = append(append(append([]byte{}, source[:e.start]...), []byte(e.text)...), source[e.end:]...)
	}
	source = bytes.ReplaceAll(source, []byte("doRequest(c.Client, req)"), []byte("doObservedRequest(c.Client, req, observation)"))
	output, err := format.Source(source)
	if err != nil {
		return err
	}
	// #nosec G703 G306 -- Fixed generated output under the trusted repository root; public source is intentionally readable.
	return os.WriteFile(path, output, 0644)
}
