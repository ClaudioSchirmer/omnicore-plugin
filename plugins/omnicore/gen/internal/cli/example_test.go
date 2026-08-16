package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// TestExampleSpecValidates is what makes `explain example` worth reading.
//
// An example that no longer validates is worse than none: it is a plausible,
// authoritative-looking file that an author copies and then spends an hour
// fighting. Because this one is printed to teach the SHAPE of a spec, it has to
// be a spec the current build actually accepts — so the claim in its own header
// is checked here rather than trusted.
func TestExampleSpecValidates(t *testing.T) {
	for name, raw := range map[string]string{
		"flat":       exampleSpec,
		"sharedbase": exampleSharedBaseSpec,
	} {
		s, err := spec.Parse([]byte(raw), name+".omnicore.yaml")
		if err != nil {
			t.Fatalf("the %s example does not even parse: %v", name, err)
		}
		if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
			t.Errorf("the %s example does not validate:\n%v", name, ps.Error())
		}
		if cov := spec.CheckCoverage(s); cov.HasBlockers() {
			t.Errorf("the %s example uses something this build refuses:\n%v", name, cov.Error())
		}
	}
}

// TestExamplesTogetherCoverTheLanguage is what makes the pair worth having.
//
// One example can never show everything — storage.kind is a single value, so a
// flat spec cannot demonstrate a shared identity, and neither can show every
// mutually exclusive option. But between them the coverage should be near
// total, because the gap is what an author has to invent: the run that started
// this had someone editing generated SQL because `unique.scope` appeared in no
// example and in no table.
func TestExamplesTogetherCoverTheLanguage(t *testing.T) {
	keys, err := spec.Keys()
	if err != nil {
		t.Fatalf("reading the language definition: %v", err)
	}
	both := exampleSpec + "\n" + exampleSharedBaseSpec

	// A key that is only ever a NESTED repeat of one already shown (the same
	// `unique` block under children[]) is covered by its top-level twin.
	shown := func(path string) bool {
		leaf := path
		if i := strings.LastIndex(leaf, "."); i >= 0 {
			leaf = leaf[i+1:]
		}
		return strings.Contains(both, "\n"+strings.Repeat(" ", 0)+leaf+":") ||
			strings.Contains(both, " "+leaf+":") ||
			strings.Contains(both, "{"+leaf+":") ||
			strings.Contains(both, ", "+leaf+":")
	}

	var missing []string
	for _, k := range keys {
		if _, ok := notShownOnPurpose[k.Path]; ok {
			continue
		}
		if _, refused := spec.RefusedKeys()[k.Path]; refused {
			continue // an example must not teach a key that `check` blocks
		}
		if !shown(k.Path) {
			missing = append(missing, k.Path)
		}
	}
	if len(missing) > 0 {
		t.Errorf("these keys appear in NEITHER example, so the only way to learn they "+
			"exist is to guess the name:\n  %s\n\nAdd them to whichever example can carry "+
			"them, or state in that example's header why they cannot be shown.",
			strings.Join(missing, "\n  "))
	}
}

// notShownOnPurpose lists keys the examples deliberately do not demonstrate.
//
// Refused keys come from spec.RefusedKeys(), the same list `explain keys` marks
// from — one source, so a key that stops being refused stops being exempt here
// on the same day. What is written out is only what needs a reason beyond
// "refused".
var notShownOnPurpose = map[string]string{
	"authz.tenantField": "needs dataAccess: tenant, and both examples show " +
		"anyone-with-permission; a third example for one key is not worth its " +
		"maintenance — `explain keys` lists it and `explain vocabulary` says what " +
		"the choice decides",
}

// TestExamplesAreGoldenMatrixCases keeps the printed example and the gated one
// from drifting apart.
//
// Validating is a weaker promise than the header makes: an example can pass
// `check` and still generate a tree that does not build — which is exactly what
// the shared-identity example did on its first day, in three different ways.
// The matrix generates, builds, vets and tests every case, so the example is
// one, and this asserts they are the same bytes.
func TestExamplesAreGoldenMatrixCases(t *testing.T) {
	onDisk, err := os.ReadFile("../../testdata/specs/matrix/18-exemplo-sharedbase.yaml")
	if err != nil {
		t.Fatalf("the matrix case for a printed example is missing: %v", err)
	}
	if string(onDisk) != exampleSharedBaseSpec {
		t.Error("the matrix case has drifted from the example `explain example` prints — " +
			"the gate is then proving something nobody reads. Copy one over the other.")
	}
}

// TestExampleSpecIsPrintedWhole guards the plumbing: an embed that silently
// resolved to an empty string would print a heading and nothing under it, and
// the topic would look implemented while teaching nothing.
func TestExampleSpecIsPrintedWhole(t *testing.T) {
	out := Explain("example")
	for _, want := range []string{"specVersion:", "storage:", "fields:", "rules:", "read:", "authz:"} {
		if !strings.Contains(out, want) {
			t.Errorf("the printed example is missing the %q block", want)
		}
	}
}

// TestExampleIsOfferedAsATopic pins the discovery path: a topic nobody is told
// about is a topic nobody runs, and this one exists precisely for an author who
// does not yet know what to ask.
func TestExampleIsOfferedAsATopic(t *testing.T) {
	if !strings.Contains(Explain(""), "example") {
		t.Error("`explain` does not list the example topic")
	}
}
