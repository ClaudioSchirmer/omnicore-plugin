package spec

import (
	_ "embed"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// specSource is the language definition itself, embedded so the key reference
// can be DERIVED rather than written.
//
// A hand-written list is what this replaces one layer up: the table of closed
// vocabularies was maintained by hand beside the sets and fell eight of them
// behind, and the key an author needed was among the missing. A reference
// generated from the same struct the loader decodes into cannot fall behind at
// all — a key added tomorrow appears the moment it exists.
//
//go:embed spec.go
var specSource string

// Key is one key of the language, at the path an author writes it.
type Key struct {
	Path string
	Type string
	Doc  string
}

// parseSpecFields walks the embedded language definition, following each struct
// into the keys it nests, so a path reads the way an author writes it
// ("children[].rules.list[].kind") rather than as a Go type name.
func Keys() ([]Key, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "spec.go", specSource, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	structs := map[string]*ast.StructType{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, s := range gd.Specs {
			ts, ok := s.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				structs[ts.Name.Name] = st
			}
		}
	}

	var out []Key
	seen := map[string]bool{}
	// A block that nests ITSELF — a fact's filter tree, whose all/any/not hold
	// more filters — is walked once and not followed back into. Its keys are the
	// same keys at every depth, so descending would list them again under a
	// longer path, and the reference would answer "what can I configure?" with
	// several hundred spellings of one block.
	open := map[string]bool{}
	var walk func(typeName, prefix string, depth int)
	walk = func(typeName, prefix string, depth int) {
		st, ok := structs[typeName]
		if !ok || depth > 6 || open[typeName] {
			return
		}
		open[typeName] = true
		defer delete(open, typeName)
		for _, field := range st.Fields.List {
			tag := yamlTagOf(field)
			if tag == "" || tag == "-" {
				continue
			}
			path := tag
			if prefix != "" {
				path = prefix + "." + tag
			}
			elem, rendered, isList := describeType(field.Type)
			if !seen[path] {
				seen[path] = true
				out = append(out, Key{Path: path, Type: rendered, Doc: firstSentence(field.Doc)})
			}
			if _, nested := structs[elem]; nested {
				childPrefix := path
				if isList {
					childPrefix += "[]"
				}
				walk(elem, childPrefix, depth+1)
			}
		}
	}
	walk("Spec", "", 0)

	sort.SliceStable(out, func(i, j int) bool { return false }) // declaration order
	return out, nil
}

func yamlTagOf(field *ast.Field) string {
	if field.Tag == nil || len(field.Names) == 0 {
		return ""
	}
	raw := strings.Trim(field.Tag.Value, "`")
	i := strings.Index(raw, `yaml:"`)
	if i < 0 {
		return ""
	}
	rest := raw[i+len(`yaml:"`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return strings.Split(rest[:end], ",")[0]
}

// describeType returns the element type name (for following into a nested
// block), how the type reads to an author, and whether it is a list.
func describeType(e ast.Expr) (elem, rendered string, isList bool) {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name, scalarName(t.Name), false
	case *ast.StarExpr:
		el, r, _ := describeType(t.X)
		return el, r, false
	case *ast.ArrayType:
		el, r, _ := describeType(t.Elt)
		return el, "[]" + r, true
	case *ast.MapType:
		_, k, _ := describeType(t.Key)
		_, v, _ := describeType(t.Value)
		return "", "map[" + k + "]" + v, false
	case *ast.SelectorExpr:
		return "", t.Sel.Name, false
	}
	return "", "value", false
}

func scalarName(name string) string {
	switch name {
	case "string", "int", "int64", "bool", "float64":
		return name
	case "any":
		// A key typed `any` takes a value in the FIELD's type — a number for a
		// number, text for text, an enum member by name. "any" would read as
		// "anything goes", which is the one thing it does not mean.
		return "literal"
	}
	return name
}

// firstSentence keeps the one line that says what the key decides. A key
// reference that reprints a paragraph per key is a document nobody scans.
func firstSentence(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	var text []string
	for _, c := range doc.List {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), " "))
		if line == "" {
			break
		}
		text = append(text, line)
	}
	joined := strings.Join(text, " ")
	if i := strings.Index(joined, ". "); i > 0 {
		joined = joined[:i+1]
	}
	return strings.TrimSpace(joined)
}
