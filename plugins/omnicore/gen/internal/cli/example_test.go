package cli

import (
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
	s, err := spec.Parse([]byte(exampleSpec), "example.omnicore.yaml")
	if err != nil {
		t.Fatalf("the embedded example does not even parse: %v", err)
	}

	if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
		t.Errorf("the embedded example does not validate:\n%v", ps.Error())
	}
	if cov := spec.CheckCoverage(s); cov.HasBlockers() {
		t.Errorf("the embedded example uses something this build refuses:\n%v", cov.Error())
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
