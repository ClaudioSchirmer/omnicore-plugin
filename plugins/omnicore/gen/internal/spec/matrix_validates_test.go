package spec

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEveryMatrixSpecValidates is the check that was missing, and its absence
// cost a whole round.
//
// The coverage matrix is the corpus every other test reaches for, and each of
// those tests takes what it needs and SKIPS what it cannot use: the emitters'
// matrixModels parses and resolves without validating, the IR's invariant sweep
// `continue`s past a fixture with blockers, and the report's matrix does the
// same. Every one of those skips is correct on its own — a fixture that does not
// validate is another test's subject — and together they meant a fixture ADDED
// to the matrix could be refused by `check` while the entire Go suite stayed
// green. The only thing that noticed was the golden gate, which needs Docker and
// five database engines and therefore runs late, if at all.
//
// So this test asserts the property nothing else does: a spec in the matrix is
// a spec the generator ACCEPTS. It is the cheapest possible version of the gate
// — no containers, no DDL — and it runs on every `go test ./...`.
//
// Coverage is asserted beside validation because `generate` runs both, and a
// fixture that validates and is then refused for using a capability this build
// does not implement fails in exactly the same place, with the same lateness.
func TestEveryMatrixSpecValidates(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "specs", "matrix", "*.yaml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("the coverage matrix is not where this test expects it: %v", err)
	}
	for _, path := range paths {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			s, err := Parse(raw, path)
			if err != nil {
				t.Fatalf("this fixture does not parse:\n%v", err)
			}
			ps := Validate(s, Options{LangFallback: true})
			if ps.HasBlockers() {
				if why, exempt := validatesOnlyWithNeighbours[name]; exempt {
					t.Skipf("exempt: %s\n\n%v", why, ps.Error())
				}
				t.Fatalf("this fixture is in the matrix and `check` refuses it — the "+
					"golden gate would fail on it, and nothing else in this suite looks:\n%v",
					ps.Error())
			}
			if cov := CheckCoverage(s); cov.HasBlockers() {
				t.Fatalf("this fixture uses a capability this build does not implement, "+
					"so `generate` refuses it:\n%v", cov.Error())
			}
		})
	}
}

// validatesOnlyWithNeighbours names the fixtures whose blockers are a property
// of being validated ALONE, keyed to the reason.
//
// It is a map rather than a list so an entry cannot be added without stating
// why, and it is deliberately tiny: an entry here is a fixture this test cannot
// speak for, so every addition narrows what the check is worth.
var validatesOnlyWithNeighbours = map[string]string{
	"20-filho-de-base-montado.yaml": "it mounts a collection its SHARED BASE owns, and the " +
		"spec that declares that base is a different file — validated alone, the collection " +
		"resolves to nothing. The gate validates the directory, where the neighbour is present.",
}
