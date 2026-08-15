package emit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The tests in this file all check for the same failure: something the spec
// declared that produced NOTHING, with no error anywhere.
//
// Every bug this build has shipped was of that shape. A transition rule inside
// a collection validated, generated its notification and all seven
// translations, and emitted a clause with no edges. A labelKey was written onto
// every field of a collection and registered in no catalog. Two roles over one
// identity each emitted a type with the same name into the same package. None
// of it failed: the build was green, the tests were green, the report said
// nothing, and the only way to notice was to read the output and compare it
// against the spec by eye.
//
// So these assertions are deliberately generic. A test that checks "the
// transition rule works" passes again the day a different rule silently stops
// working; these check the PROPERTY — declared implies emitted — over whatever
// the matrix happens to contain, which is why adding a case to the matrix
// widens them for free.

// matrixModels resolves every spec in the coverage matrix. They are the corpus:
// each one exists because it covers an axis nothing else does.
func matrixModels(t *testing.T) map[string]*ir.Model {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "specs", "matrix", "*.yaml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("the coverage matrix is not where this test expects it: %v", err)
	}
	out := map[string]*ir.Model{}
	for _, p := range paths {
		s, err := spec.Load(p)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(p), err)
		}
		m, err := ir.Resolve(s, &discover.Project{
			ModulePath: "example.test/svc",
			Dialects:   []string{"sqlite"},
			Root:       t.TempDir(),
		})
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(p), err)
		}
		out[filepath.Base(p)] = m
	}
	return out
}

func emitAll(t *testing.T, m *ir.Model) []fsplan.File {
	t.Helper()
	root := t.TempDir()
	res, err := All(m, root, FileMeta{
		Spec: "specs/x.omnicore.yaml", Entity: m.Entity.Pascal,
		Date: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("%s: emitting: %v", m.Entity.Pascal, err)
	}
	return res.Files
}

// goSources returns the emitted Go, keyed by path.
func goSources(files []fsplan.File) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".go") {
			out[f.Path] = string(f.Content)
		}
	}
	return out
}

// ownedSources drops the registration files, which two entities are SUPPOSED to
// declare into: notifications.go and the seven catalogs are merged key-wise, so
// each entity emits the whole file and the writer keeps what is already there.
// Comparing those as competing declarations reports the mechanism itself.
func ownedSources(files []fsplan.File) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".go") && f.Class == fsplan.Owned {
			out[f.Path] = string(f.Content)
		}
	}
	return out
}

// TestEveryDeclaredRuleReachesTheCode is the direct regression for the class of
// bug that cost the most: a rule that validates and emits nothing.
//
// The check is that the notification a rule raises is REFERENCED somewhere in
// the emitted Go. It cannot be: the type is generated either way, so its
// presence in the catalogs and in notifications.go proves nothing — only a
// reference from the code that enforces the rule does. A clause emitted with
// no edges, a kind the emitter quietly skipped, a rule dropped by a resolver
// that had fallen behind: all three leave the notification unreferenced.
func TestEveryDeclaredRuleReachesTheCode(t *testing.T) {
	for name, m := range matrixModels(t) {
		t.Run(name, func(t *testing.T) {
			body := strings.Join(valuesOf(goSources(emitAll(t, m))), "\n")

			type want struct{ rule, notification string }
			var wants []want
			for _, cl := range m.Clauses {
				for _, r := range cl.Rules {
					if r.Notification != "" {
						wants = append(wants, want{r.ID, r.Notification})
					}
				}
			}
			for _, c := range m.Children {
				for _, cl := range c.Clauses {
					for _, r := range cl.Rules {
						if r.Notification != "" {
							wants = append(wants, want{r.ID, r.Notification})
						}
					}
				}
			}
			for _, w := range wants {
				// The declaration site does not count as enforcement: what is
				// being asserted is that something RAISES it.
				if !regexp.MustCompile(`AddNotification\([^)]*` + regexp.QuoteMeta(w.notification)).MatchString(body) {
					t.Errorf("rule %q declares %s and no emitted code raises it — "+
						"the rule validated, its type and translations were generated, "+
						"and the check itself was never written",
						w.rule, w.notification)
				}
			}
		})
	}
}

// TestEveryLabelKeyIsTranslated pins the other half of "declared, then
// forgotten": a label the code asks the framework to resolve, against a catalog
// that has no such key.
//
// The failure is invisible by construction — the export succeeds, the data is
// right, and the heading is a Go identifier — so nothing but a test like this
// ever reports it.
func TestEveryLabelKeyIsTranslated(t *testing.T) {
	tagRe := regexp.MustCompile("labelKey:\"([A-Za-z0-9_]+)\"")

	for name, m := range matrixModels(t) {
		t.Run(name, func(t *testing.T) {
			files := emitAll(t, m)
			src := goSources(files)

			declared := map[string]string{} // key -> the file that asks for it
			for path, body := range src {
				if strings.Contains(path, "/translations/") {
					continue
				}
				for _, mm := range tagRe.FindAllStringSubmatch(body, -1) {
					if _, seen := declared[mm[1]]; !seen {
						declared[mm[1]] = path
					}
				}
			}

			catalog := map[string]bool{}
			for _, f := range files {
				if !strings.Contains(f.Path, "/translations/") {
					continue
				}
				for _, mm := range regexp.MustCompile(`"([A-Za-z0-9_]+)":`).
					FindAllStringSubmatch(string(f.Content), -1) {
					catalog[mm[1]] = true
				}
			}

			var missing []string
			for key, path := range declared {
				if !catalog[key] {
					missing = append(missing, key+" (tagged in "+path+")")
				}
			}
			sort.Strings(missing)
			if len(missing) > 0 {
				t.Errorf("these labelKeys are tagged onto fields and translated in no "+
					"catalog, so the raw Go identifier is what the caller sees:\n  %s",
					strings.Join(missing, "\n  "))
			}
		})
	}
}

// TestNoSymbolIsDeclaredTwice guards the seam that opened the moment two roles
// could share one identity.
//
// Every emitted file lands in a package shared with the OTHER entities of the
// project, and a name chosen from the collection alone — TituloRequest,
// projectTitulos, AddTituloCommand — is unique only until a second role exposes
// the same collection. Each collision was found by compiling a two-spec
// project by hand; this finds them without one, and finds the within-entity
// case the compiler would have caught anyway.
func TestNoSymbolIsDeclaredTwice(t *testing.T) {
	for name, m := range matrixModels(t) {
		t.Run(name, func(t *testing.T) {
			seen := map[string]string{} // "pkg.Symbol" -> first file
			for path, body := range ownedSources(emitAll(t, m)) {
				for _, sym := range topLevelNames(t, path, body) {
					key := filepath.Dir(path) + "." + sym
					if first, dup := seen[key]; dup {
						t.Errorf("%s is declared in BOTH %s and %s — one package, one name",
							sym, first, path)
						continue
					}
					seen[key] = path
				}
			}
		})
	}
}

// TestTwoRolesOverOneIdentityDeclareNoSymbolTwice is the cross-entity half, and
// the one that matters: within a single spec the compiler would have caught
// every collision anyway.
//
// Two roles over one shared identity emit into the SAME Go packages. Everything
// named after the collection alone — TituloRequest, TituloResult,
// projectTitulos, AddTituloCommand — is unique only until the second role
// exposes the same collection, and then the project stops compiling with a
// message that names neither spec. Each of those was found by hand, one
// compile at a time; this finds them from the pair itself.
func TestTwoRolesOverOneIdentityDeclareNoSymbolTwice(t *testing.T) {
	models := matrixModels(t)
	owner, ok := models["12-filho-de-base.yaml"]
	mounted, ok2 := models["20-filho-de-base-montado.yaml"]
	if !ok || !ok2 {
		t.Fatal("the mounted pair is not in the matrix any more — this test is about that pair")
	}

	seen := map[string]string{}
	for _, m := range []*ir.Model{owner, mounted} {
		for path, body := range ownedSources(emitAll(t, m)) {
			// A test file is per entity and never imported; a duplicate there
			// is a redeclaration the compiler reports, and it is checked by the
			// same rule below, so nothing is exempt.
			for _, sym := range topLevelNames(t, path, body) {
				key := filepath.Dir(path) + "." + sym
				if first, dup := seen[key]; dup && first != m.Entity.Pascal+":"+path {
					t.Errorf("both roles declare %s in %s (%s and %s) — one package, one name",
						sym, filepath.Dir(path), first, m.Entity.Pascal+":"+path)
					continue
				}
				seen[key] = m.Entity.Pascal + ":" + path
			}
		}
	}
}

// topLevelNames lists what a file declares at package scope, methods excluded:
// a method is scoped by its receiver, so two types may each have one.
func topLevelNames(t *testing.T, path, body string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Base(path), body, 0)
	if err != nil {
		t.Fatalf("%s does not parse: %v", path, err)
	}
	var out []string
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Recv == nil {
				out = append(out, decl.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					out = append(out, s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name != "_" {
							out = append(out, n.Name)
						}
					}
				}
			}
		}
	}
	return out
}

func valuesOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
