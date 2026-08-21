package emit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The validator accepting `ID` under filters and sort is half an answer: the
// lowering resolves the read's leaves through m.Fields, where the identity has
// never been and never will be. A name blessed there and dropped here is the
// exact failure mode this package's other tests exist for — the spec declares
// something, nothing errors, and the endpoint comes out without it.
//
// So this checks the emitted leaf, not the model: the wire is where the promise
// is either kept or not.
func TestIdentityReachesTheListingRequest(t *testing.T) {
	s, err := spec.Load(filepath.Join("..", "..", "testdata", "specs", "student.omnicore.yaml"))
	if err != nil {
		t.Fatalf("the fixture is not where this test expects it: %v", err)
	}
	m, err := ir.Resolve(s, &discover.Project{
		ModulePath: "example.test/svc",
		Dialects:   []string{"sqlite"},
		Root:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	var leaf string
	for path, src := range goSources(emitAll(t, m)) {
		if !strings.Contains(path, "web") {
			continue
		}
		for _, line := range strings.Split(src, "\n") {
			if strings.Contains(line, `query:"id"`) {
				leaf = strings.TrimSpace(line)
			}
		}
	}
	if leaf == "" {
		t.Fatal("the listing request declares no id leaf — the spec filters and orders by ID " +
			"and the generated endpoint would accept neither")
	}
	// The wire type is the scalar. domain.ID is a struct with no exported
	// field: bound into a query leaf it is not a value the framework can read,
	// it is a nested group with nothing in it.
	if !strings.Contains(leaf, "*string") {
		t.Errorf("the id leaf must carry the wire scalar, got: %s", leaf)
	}
	if !strings.Contains(leaf, `filter:"eq,in"`) {
		t.Errorf("the declared operators must reach the tag, got: %s", leaf)
	}
	if !strings.Contains(leaf, `sort:"asc,desc"`) {
		t.Errorf("the ordering vocabulary must reach the tag, got: %s", leaf)
	}
}
