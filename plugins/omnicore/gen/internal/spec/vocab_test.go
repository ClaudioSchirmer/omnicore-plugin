package spec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestEveryVocabularyIsDocumented is the mechanical form of a promise the
// generator makes to whoever writes a spec: if a key accepts only certain
// values, the tool will tell you which.
//
// It exists because the promise was quietly broken. The list `explain
// vocabulary` printed was written by hand beside the sets, and it fell EIGHT
// sets behind — among them fields[].unique.scope. An author looking for
// "unique among the rows that are not archived" found nothing, edited the
// generated SQL by hand to get it, and only later stumbled on the key that had
// been there all along. The cost of an undocumented vocabulary is not a missing
// paragraph; it is someone working around a feature they own.
//
// So the source of truth is this file, and this test reads it: a set declared
// here and left out of the registry fails the build the day it is written.
func TestEveryVocabularyIsDocumented(t *testing.T) {
	declared := setsDeclaredIn(t, "vocab.go")

	registered := map[string]bool{}
	for _, v := range Vocabularies() {
		registered[v.Set.String()] = true
	}

	var undocumented []string
	for name, values := range declared {
		if !registered[values] {
			undocumented = append(undocumented, name)
		}
	}

	if len(undocumented) > 0 {
		t.Errorf("these closed vocabularies exist and are documented nowhere the author "+
			"looks — a key whose values are unknowable is a key nobody uses:\n  %s\n\n"+
			"Add each to Vocabularies() with the yaml path it governs and one line on what "+
			"the choice decides.", strings.Join(undocumented, "\n  "))
	}
}

// TestEveryVocabularyEntryIsUsable pins the two things that make an entry worth
// printing: it says WHERE the key is, and WHAT the choice decides. A row that
// is only a list of legal strings tells nobody which one they want.
func TestEveryVocabularyEntryIsUsable(t *testing.T) {
	for _, v := range Vocabularies() {
		if v.Path == "" {
			t.Errorf("a vocabulary (%s) is registered with no yaml path", v.Set.String())
		}
		if strings.TrimSpace(v.Why) == "" {
			t.Errorf("%s is registered with no explanation of what the choice decides", v.Path)
		}
		if len(v.Set.List()) == 0 {
			t.Errorf("%s is registered with an empty set", v.Path)
		}
	}
}

// setsDeclaredIn reads `X = set("a", "b")` out of the source, so a vocabulary
// added tomorrow is covered by this test without anyone remembering to add it.
func setsDeclaredIn(t *testing.T, path string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if i >= len(vs.Values) {
				continue
			}
			call, ok := vs.Values[i].(*ast.CallExpr)
			if !ok {
				continue
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || fn.Name != "set" {
				continue
			}
			var values []string
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok {
					continue
				}
				values = append(values, strings.Trim(lit.Value, `"`))
			}
			out[name.Name] = strings.Join(values, " | ")
		}
		return true
	})
	return out
}
