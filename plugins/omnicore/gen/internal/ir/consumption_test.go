package ir

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryModelFieldReachesAnEmitter closes the hole the spec-level manifest
// test cannot see.
//
// That test asks "does anybody mention this yaml key?", and an assignment into
// the model counts — so a key could be validated, resolved, carried all the way
// into the IR, and read by NO emitter. The generated code then ignores the
// declaration silently, which is exactly the failure INV-1 exists to prevent.
// It happened twice: children[].ownedBy and siblings[].attachTo both rode into
// the model and stopped there, so a base-owned collection came out attached to
// the role and a facet declared inside a child came out on the root.
//
// So this test asks the stricter question: does anything DOWNSTREAM of the
// model read this field? Downstream means an emitter, or a method on the model
// (which exists only to be called by one) — never the resolver's own
// constructors, where the assignments live.
func TestEveryModelFieldReachesAnEmitter(t *testing.T) {
	read := identsInEmitters(t)
	for k := range identsIn(t, "../report") {
		read[k] = true
	}
	for k := range identsInModelMethods(t) {
		read[k] = true
	}

	var orphans []string
	for _, f := range modelFields(t) {
		if read[f.Name] {
			continue
		}
		if _, ok := unreadByEmitters[f.Qualified]; ok {
			continue
		}
		orphans = append(orphans, f.Qualified)
	}

	if len(orphans) > 0 {
		t.Errorf("these model fields are filled by the resolver and read by no emitter — "+
			"whatever the author declared to produce them changes nothing in the generated "+
			"code:\n  %s\n\nEither read them in an emitter, refuse the spec key in coverage.go, "+
			"or add them to unreadByEmitters with a reason.",
			strings.Join(orphans, "\n  "))
	}
}

// unreadByEmitters lists model fields no emitter reads ON PURPOSE.
var unreadByEmitters = map[string]string{
	"Model.Module": "read through ImportPath, a method whose body mentions it; " +
		"listed because the name itself never appears in an emitter",
	"Sibling.AttachTo": "resolved into OwnerChild, which is what the emitters ask; " +
		"kept because the report and the messages quote what the author wrote",
	"Field.SpecType": "the resolver turns it into GoType/EntityType; the emitters " +
		"write Go, never the spec's own type names",
	"Field.BaseGoType": "used by the resolver to build GoType for a nullable field",
	"Rule.ID":          "names the rule in the report and in the refusal messages",
	"Rule.OnlyFieldName": "the resolver binds it into OnlyField against the collection " +
		"it counts, and that is what the emitter reads; the name is kept because a " +
		"collection whose field vanished is worth reporting as the author wrote it",
	"Rule.FactName": "the resolver binds it into Fact once the port exists, and that " +
		"is what the emitter reads; the name is kept because a rule naming a fact " +
		"that vanished is worth reporting as the author wrote it",
	"ManualRule.ID": "names the item in the hook file and the report",
	"Child.OwnedBy": "read through BaseChildren/RoleChildren and childOwnerTable, " +
		"which is where the ownership decision actually lands",
	"Child.DocSegment": "read by readColumn when an index or filter addresses a " +
		"child field by its document key",
	"Unique.Scope": "read by resolveConstraints, which turns it into the " +
		"constraint's own Scope — that is what the DDL asks",
}

type modelField struct{ Qualified, Name string }

// modelFields reads ir.go itself, so a field added to the model is covered by
// this test the moment it exists.
func modelFields(t *testing.T) []modelField {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "ir.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing ir.go: %v", err)
	}
	var out []modelField
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range st.Fields.List {
			for _, name := range field.Names {
				if !name.IsExported() {
					continue
				}
				out = append(out, modelField{ts.Name.Name + "." + name.Name, name.Name})
			}
		}
		return true
	})
	return out
}

func identsInEmitters(t *testing.T) map[string]bool { return identsIn(t, "../emit") }

func identsIn(t *testing.T, dir string) map[string]bool {
	t.Helper()
	used := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, e.Name(), b, 0)
		if err != nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.Ident:
				used[v.Name] = true
			case *ast.SelectorExpr:
				used[v.Sel.Name] = true
			}
			return true
		})
	}
	return used
}

// identsInModelMethods collects what the model's own METHODS read.
//
// Methods are downstream: they exist to answer an emitter's question, so a
// field only they touch is genuinely consumed. The resolver's free functions
// are deliberately excluded — that is where `Foo: spec.Foo` is written, and
// counting it is precisely the hole this test closes.
func identsInModelMethods(t *testing.T) map[string]bool {
	t.Helper()
	used := map[string]bool{}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "ir.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing ir.go: %v", err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				used[sel.Sel.Name] = true
			}
			return true
		})
	}
	return used
}
