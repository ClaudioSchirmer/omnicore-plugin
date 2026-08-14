package spec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEverySpecFieldIsConsumedOrRefused is the mechanical form of INV-1.
//
// The failure it guards is quiet and expensive: a key the loader accepts, the
// validator approves, and NO emitter reads. The author writes it, the spec goes
// green, and the intent is silently dropped — the generated service simply does
// not do the thing they asked for, and nothing anywhere says so.
//
// Reviewing for that by eye does not work; it is the kind of gap that opens
// while adding something else. So every field of the spec language must be
// either READ by the resolver or an emitter, or listed below as deliberately
// unread — and a deliberate entry must say why.
func TestEverySpecFieldIsConsumedOrRefused(t *testing.T) {
	fields := specFieldNames(t)
	// A field REFUSED in coverage.go is accounted for too: the author is told
	// the key does nothing in this build, which is the opposite of silence.
	consumed := identifiersUsedIn(t, "../ir", "../emit")
	for k := range identifiersUsedInFile(t, "coverage.go") {
		consumed[k] = true
	}

	var orphans []string
	for _, f := range fields {
		if consumed[f.Name] {
			continue
		}
		if _, ok := deliberatelyUnread[f.Qualified]; ok {
			continue
		}
		orphans = append(orphans, f.Qualified+" (yaml: "+f.Tag+")")
	}

	if len(orphans) > 0 {
		t.Errorf("these spec fields are accepted by the loader and read by nobody — "+
			"an author can set them and the generator will silently ignore the "+
			"instruction:\n  %s\n\nEither consume them in an emitter, refuse them in "+
			"coverage.go, or add them to deliberatelyUnread with a reason.",
			strings.Join(orphans, "\n  "))
	}
}

// deliberatelyUnread lists fields no emitter reads ON PURPOSE, each with the
// reason. An entry here is a claim that setting the key changes nothing AND
// that this is correct — not a place to silence a finding.
var deliberatelyUnread = map[string]string{
	"Spec.SourcePath": "bookkeeping, not part of the language",
	"Child.SoftRemove": "the validator's gate for archivedAt: it forces the pair to " +
		"agree, and archivedAt is what the schema and the DDL actually read",
	"Authz.Resource": "the validator reads it to check every permission is namespaced " +
		"by it; the emitters write the permission STRINGS, which are declared",
	"Spec.SpecVersion": "read by the validator to refuse a version this build " +
		"does not speak; there is nothing for an emitter to do with it",
	"Field.LabelKey": "consumed through the resolved model's own LabelKey, which " +
		"defaults when the spec leaves it empty",
	"Rule.ID":       "identifies the rule in messages and in the report, not in emitted code",
	"ManualRule.ID": "same: it names the item in the hook file and the report",
	"Notification.Description": "documentation for the spec reader; the emitted " +
		"notification documents itself from its semantic and tvars",
	"Fact.Description":        "documentation for the spec reader, rendered into the port's comment",
	"Child.Description":       "rendered into the generated comment and the table comment",
	"Sibling.Description":     "rendered into the generated comment and the table comment",
	"ValueObject.Description": "rendered into the generated type's comment",
	"Storage.Description":     "rendered into the table comment",
	"Base.Description":        "rendered into the base table's comment",
	"Rule.Description":        "rendered as the comment above the generated rule",
	"Spec.Delete": "the emitters read the MODES; the validator forces this block " +
		"to agree with them, and declaring both is what makes a disagreement " +
		"detectable instead of ambiguous",
	"Delete.Root": "same: it is cross-checked against the modes rather than read " +
		"on its own",
}

type specField struct {
	Qualified string
	Name      string
	Tag       string
}

// specFieldNames reads the language definition itself, so a field added to
// spec.go is covered by this test the moment it exists.
func specFieldNames(t *testing.T) []specField {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "spec.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing spec.go: %v", err)
	}

	var out []specField
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
			if field.Tag == nil || len(field.Names) == 0 {
				continue
			}
			tag := field.Tag.Value
			yamlTag := extractYAMLTag(tag)
			if yamlTag == "" || yamlTag == "-" {
				// A field with no yaml tag is not part of the language.
				if yamlTag != "-" {
					continue
				}
			}
			for _, name := range field.Names {
				out = append(out, specField{
					Qualified: ts.Name.Name + "." + name.Name,
					Name:      name.Name,
					Tag:       yamlTag,
				})
			}
		}
		return true
	})
	return out
}

func extractYAMLTag(raw string) string {
	raw = strings.Trim(raw, "`")
	idx := strings.Index(raw, `yaml:"`)
	if idx < 0 {
		return ""
	}
	rest := raw[idx+len(`yaml:"`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return strings.Split(rest[:end], ",")[0]
}

// identifiersUsedIn collects every identifier the resolver and the emitters
// mention. Coarse on purpose: a false "consumed" is possible, a false "orphan"
// is not, and this test exists to catch what nobody reads at all.
func identifiersUsedIn(t *testing.T, dirs ...string) map[string]bool {
	t.Helper()
	used := map[string]bool{}
	for _, dir := range dirs {
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
	}
	return used
}

// TestCoverageRefusesWhatNoEmitterImplements is the other half.
//
// A vocabulary VALUE can be dead the same way a field can: accepted by the
// closed set, approved by the validator, and handled by no emitter. The edit
// strategy is the case that motivated this — "per-child" was a legal value that
// changed nothing, so a spec asking for per-entry endpoints quietly received a
// replace-everything collection instead.
func TestCoverageRefusesWhatNoEmitterImplements(t *testing.T) {
	s := minimalSpec()
	s.Children = []Child{{
		Name: "Grade", Table: "student_grades", OwnedBy: "root",
		EditStrategy: "per-child", BusinessIdentity: []string{"Subject"},
		Fields: []Field{{Name: "Subject", Type: "string", Column: "subject", Length: 40}},
	}}
	ps := CheckCoverage(s)
	if !ps.HasBlockers() {
		t.Error("per-child edit is not generated by any emitter, so a spec asking " +
			"for it must be refused rather than silently given atomic-replace")
	}
}

func identifiersUsedInFile(t *testing.T, path string) map[string]bool {
	t.Helper()
	used := map[string]bool{}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, b, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
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
	return used
}
