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
		Spec: "omnicore-gen/x.omnicore.yaml", Entity: m.Entity.Pascal,
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

// TestEveryEnumMemberTextReachesACatalog is the third instance of the same bug,
// and the one that stayed open longest.
//
// `valueObjects[].members[].text` was a first-class key of the spec language:
// documented in `explain keys`, parsed, validated, and accepted by `check` with
// "✓ this spec can be generated". It reached the IR and stopped there — the IR's
// EnumMember carried ConstName, Literal and Name, so no emitter could ever see
// the seven translations. Nothing failed. The author declared "Aberto"/"Open"/
// "Ouvert", got a green check, and read `SituacaoCurso.aberto` on the screen in
// all seven languages, because that is what Translator.EnumDescription answers
// when the catalog has no entry.
//
// The asymmetry was the tell: the sibling key `descriptionKeys` was REFUSED by
// name, so the one that did nothing announced it and the one that did nothing
// silently was the one carrying the text.
//
// This asserts the property over whatever the matrix contains: every member of
// every enum gets an entry, in every catalog, under the key the framework
// derives — and never the key itself as the value.
func TestEveryEnumMemberTextReachesACatalog(t *testing.T) {
	for name, m := range matrixModels(t) {
		var members []ir.EnumMember
		for _, vo := range m.ValueObjects {
			members = append(members, vo.Members...)
		}
		if len(members) == 0 {
			continue
		}
		t.Run(name, func(t *testing.T) {
			entries := catalogEntries(m)
			for _, lang := range ir.LangOrder {
				byKey := map[string]string{}
				for _, e := range entries[lang] {
					byKey[e.Key] = e.Value
				}
				for _, mem := range members {
					got, ok := byKey[mem.DescriptionKey]
					if !ok {
						t.Errorf("%s is in no %s catalog — the member's text was declared, "+
							"parsed and dropped, and EnumDescription answers the raw key",
							mem.DescriptionKey, lang)
						continue
					}
					if got == "" || got == mem.DescriptionKey {
						t.Errorf("%s in %s resolves to %q — an entry whose value is the key "+
							"is the same nothing as no entry at all",
							mem.DescriptionKey, lang, got)
					}
				}
			}
		})
	}
}

// TestEnumDescriptionKeyMatchesTheFramework pins the key SHAPE, which is the
// half of this that no amount of emitting can fix from the inside.
//
// domain.EnumDescriptionKey reflects over the value: "<TypeName>.<value>". A
// catalog filed under the member's Go NAME — SituacaoCurso.Aberto — is
// well-formed, complete, and never found, because the framework never asks for
// that key. So the entry has to be built from the value, and this is what says
// so.
func TestEnumDescriptionKeyMatchesTheFramework(t *testing.T) {
	models := matrixModels(t)
	for _, tc := range []struct{ spec, vo, key string }{
		// backing: int — the key carries the number.
		{"04-enum-int-comparison.yaml", "NivelContrato", "NivelContrato.1"},
		// backing: string — the key carries the wire value, lowercase and all.
		{"11-mysql-nats-mongo-graphql.yaml", "SituacaoCurso", "SituacaoCurso.aberto"},
	} {
		m, ok := models[tc.spec]
		if !ok {
			t.Fatalf("%s left the matrix — this test is about the enum it declares", tc.spec)
		}
		var found bool
		for _, vo := range m.ValueObjects {
			if vo.Name != tc.vo {
				continue
			}
			for _, mem := range vo.Members {
				if mem.DescriptionKey == tc.key {
					found = true
				}
				if strings.Contains(mem.DescriptionKey, "."+mem.Name) {
					t.Errorf("%s is keyed by the member NAME; the framework reflects over the "+
						"VALUE and will look up something else", mem.DescriptionKey)
				}
			}
		}
		if !found {
			t.Errorf("no member of %s is keyed %q", tc.vo, tc.key)
		}
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

// TestEveryScopedEntityProvesItsScope is the regression for a test that PASSES
// while proving nothing.
//
// A row scope — owner-only or tenant — is the one restriction whose failure is
// silent in both directions: the query still returns rows, they are simply
// somebody else's. The generated criteria test used to assert only that the
// mapper does not error, which an omitted filter also satisfies; a real project
// noticed and wrote the missing coverage by hand.
//
// So this asserts the ASSERTION exists: for every scoped spec of the matrix, the
// emitted test file must attach an identity, name the scoping field, and compare
// something against it. Checking that a file "has a test for the scope" by name
// would pass again the day the body stops asserting.
func TestEveryScopedEntityProvesItsScope(t *testing.T) {
	for name, m := range matrixModels(t) {
		if !m.Read.ByParams {
			continue
		}
		var field string
		switch m.Authz.DataAccess {
		case "owner-only":
			if m.Authz.OwnerField != nil {
				field = m.Authz.OwnerField.Name
			}
		case "tenant":
			if m.Authz.TenantField != nil {
				field = m.Authz.TenantField.Name
			}
		}
		if field == "" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			// The read-wide proofs ride whichever query file the entity has;
			// read them all rather than guessing which one.
			var src string
			for path, body := range goSources(emitAll(t, m)) {
				if strings.HasPrefix(path, "internal/application/queries/find_") &&
					strings.HasSuffix(path, "_query_test.go") {
					src += body
				}
			}
			if src == "" {
				t.Fatal("a scoped entity emitted no query tests at all")
			}
			for _, needed := range []string{
				"SetIdentity",                           // a caller is attached, not assumed
				"out.Filter[" + `"` + field + `"` + "]", // the scoping field is read back
				"somebody-else",                         // the caller's own attempt to choose it
			} {
				if !strings.Contains(src, needed) {
					t.Errorf("the scope test does not %q — a scoped read whose filter is "+
						"dropped answers with everybody's rows, and a test that only checks "+
						"for an error passes anyway", needed)
				}
			}
		})
	}
}
