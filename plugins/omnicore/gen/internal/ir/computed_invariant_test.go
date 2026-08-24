package ir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The invariant the whole defect reduces to: a spec that VALIDATES never
// produces a derivation with fewer sources resolved than it declared.
//
// The old shape had no such rule. The validator admitted six categories of
// source, the emitter resolved two, and the gap was dropped with a bare
// `continue` — so `check` was green, `generate` succeeded, the tree compiled
// (a function with fewer parameters is valid Go) and the field was empty on
// every surface forever. Nothing anywhere disagreed, because the inputs were
// simply absent.
//
// It is asserted over every fixture in the repository rather than over one
// hand-made model: the point is that no INPUT can reach that state, and a
// single fixture proves only itself.
func TestNoValidSpecLosesADerivationSource(t *testing.T) {
	var specs []string
	for _, dir := range []string{"../../testdata/specs", "../../testdata/specs/matrix"} {
		found, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
		if err != nil {
			t.Fatalf("listing %s: %v", dir, err)
		}
		specs = append(specs, found...)
	}
	if len(specs) == 0 {
		t.Fatal("no fixture was found, so this test would prove nothing")
	}

	// How many derivations were actually inspected. A run that resolves nothing
	// passes every assertion below without reaching the question — the failure
	// mode this test exists to catch, wearing the test's own clothes.
	seen := 0
	for _, path := range specs {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		s, err := spec.Parse(raw, path)
		if err != nil {
			continue // a fixture that does not parse is another test's subject
		}
		if ps := spec.Validate(s, spec.Options{LangFallback: true}); ps.HasBlockers() {
			// Several fixtures are only valid alongside their neighbours (a join
			// target, a shared base). Their blockers are not this test's subject;
			// what it asserts is about specs that DO validate.
			continue
		}
		m, err := Resolve(s, &discover.Project{
			ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
		})
		if err != nil {
			t.Errorf("%s: validates but does not resolve: %v", filepath.Base(path), err)
			continue
		}
		check := func(scope string, cs []ComputedField) {
			for _, c := range cs {
				seen++
				if len(c.SourceFields) != len(c.Sources) {
					t.Errorf("%s: %s %s declared %d sources and resolved %d — the derivation "+
						"would take fewer parameters than the spec named, which compiles and "+
						"renders the field empty forever",
						filepath.Base(path), scope, c.Name, len(c.Sources), len(c.SourceFields))
				}
			}
		}
		check("read.computed", m.Read.Computed)
		for _, ch := range m.Children {
			check("children("+ch.Plural+").computed", ch.Computed)
		}
	}
	if seen == 0 {
		t.Fatal("no fixture declares a derivation, so the invariant was never exercised — " +
			"add one rather than letting this pass")
	}
	t.Logf("%d derivation(s) inspected across %d fixture(s)", seen, len(specs))
}
