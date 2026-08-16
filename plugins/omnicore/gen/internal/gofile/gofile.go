// Package gofile is the single choke point every generated .go file passes
// through before it is written.
//
// It exists because of one recurring failure: an emitter declares a fixed
// import list, a spec subset stops using one of them, and the generated tree no
// longer compiles. Pruning here — once, for every file — kills that class
// structurally instead of forcing each emitter to reason about its own imports.
// Formatting rides along, so every emitted file is gofmt-clean by construction.
package gofile

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// Finalize prunes unused imports and formats. The input may be untidy; the
// output is what gets written to disk.
func Finalize(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "generated.go", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("the emitted file does not parse: %w\n%s", err, numbered(src))
	}

	used := usedPackages(f)
	pruneImports(f, used)

	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, f); err != nil {
		return nil, fmt.Errorf("printing: %w", err)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("gofmt: %w\n%s", err, numbered(buf.Bytes()))
	}
	return out, nil
}

// usedPackages collects the qualifiers actually referenced as `pkg.Something`.
// Selector expressions are the only way a generated file uses an import, so a
// qualifier absent from this set means the import is dead.
func usedPackages(f *ast.File) map[string]bool {
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Obj == nil {
			used[ident.Name] = true
		}
		return true
	})
	return used
}

func pruneImports(f *ast.File, used map[string]bool) {
	var keptSpecs []ast.Spec
	var keptImports []*ast.ImportSpec

	for _, imp := range f.Imports {
		name := importLocalName(imp)
		// A blank or dot import is there for its side effect, never for a
		// qualifier — dropping it would change behaviour.
		if name == "_" || name == "." || used[name] {
			keptSpecs = append(keptSpecs, imp)
			keptImports = append(keptImports, imp)
		}
	}
	f.Imports = keptImports

	var decls []ast.Decl
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			decls = append(decls, d)
			continue
		}
		if len(keptSpecs) == 0 {
			continue // drop the whole import block
		}
		gd.Specs = keptSpecs
		// A single surviving import reads better unparenthesised, and gofmt
		// agrees, so collapse it rather than leaving an empty-looking block.
		if len(keptSpecs) == 1 {
			gd.Lparen = token.NoPos
			gd.Rparen = token.NoPos
		}
		decls = append(decls, gd)
		keptSpecs = nil
	}
	f.Decls = decls
}

// versionSuffixRe matches the /vN element Go module paths use, which is never
// the package name.
var versionSuffixRe = regexp.MustCompile(`^v[0-9]+$`)

func importLocalName(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	path, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return ""
	}
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	if versionSuffixRe.MatchString(last) && len(parts) > 1 {
		last = parts[len(parts)-2]
	}
	// A dash is illegal in an identifier, so such packages are always aliased
	// by the emitters; falling back to the trimmed form keeps this total.
	return strings.ReplaceAll(last, "-", "")
}

// numbered renders source with line numbers so a parse failure in generated
// code can be read without guessing where it is.
func numbered(src []byte) string {
	var b strings.Builder
	for i, line := range strings.Split(string(src), "\n") {
		fmt.Fprintf(&b, "%4d | %s\n", i+1, line)
	}
	return b.String()
}
