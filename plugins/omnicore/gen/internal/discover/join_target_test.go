package discover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/layout"
)

// TestSpecClaimExposesEveryColumnAJoinCanTraverse pins what a READ JOIN needs to
// see in the TARGET's spec, and the case that was invisible until it bit: a
// COMPOSITE value object.
//
// A composite owns no column of its own — its value spans several, one per
// part — and those part columns are ordinary columns of the table: the schema
// enters them in the same bijection a plain field's column goes into, so a join
// may traverse onto one. Reading only fields[].column made them unfindable, and
// the generator then refused a perfectly legal traversal with the WRONG reason
// ("not a column of X" — it is one).
//
// The type is the second half. A part states its own only when the value object
// is reused from another file; when the declaration is in the same spec, the
// type lives THERE and nowhere else, so the claim has to resolve it.
func TestSpecClaimExposesEveryColumnAJoinCanTraverse(t *testing.T) {
	dir := t.TempDir()
	specs := layout.DirIn(dir)
	if err := os.MkdirAll(specs, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := `specVersion: 1
entity: Campus
plural: Campi
storage:
  kind: flat
  table: campi
valueObjects:
  - name: Coordinates
    kind: composite
    parts:
      - {name: Latitude, type: float64}
      - {name: Longitude, type: float64, nullable: true}
fields:
  - {name: Label, type: string, column: label, livesOn: root}
  - {name: AuditorID, type: id, column: auditor_id, nullable: true, livesOn: root}
  - name: Location
    livesOn: root
    vo: {kind: composite, ref: Coordinates}
    parts:
      - {part: Latitude,  column: latitude,  as: Lat}
      - {part: Longitude, column: longitude}
  - name: Optional
    livesOn: root
    nullable: true
    vo: {kind: composite, ref: Coordinates}
    parts:
      - {part: Latitude, column: opt_latitude, as: OptLat}
  - {name: FromToken, type: string, runtime: true, livesOn: root}
`
	if err := os.WriteFile(filepath.Join(specs, "campus"+layout.SpecSuffix), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	claims := discoverSpecs(dir)
	if len(claims) != 1 {
		t.Fatalf("expected one claim, got %d", len(claims))
	}
	byColumn := map[string]FieldClaim{}
	for _, f := range claims[0].Fields {
		byColumn[f.Column] = f
	}

	for _, tc := range []struct {
		column   string
		name     string
		typ      string
		nullable bool
		why      string
	}{
		{"label", "Label", "string", false, "a plain column"},
		{"auditor_id", "AuditorID", "id", true, "a nullable identity"},
		{"latitude", "Lat", "float64", false,
			"a composite part, typed from the value object and named by its `as`"},
		{"longitude", "Longitude", "float64", true,
			"a part the value object declares nullable, named by the part itself"},
		{"opt_latitude", "OptLat", "float64", true,
			"a part of an OPTIONAL composite: every part column is NULL-able, because " +
				"\"every part NULL\" is how the absence of the whole value is written"},
	} {
		got, ok := byColumn[tc.column]
		if !ok {
			t.Errorf("%s (%s) is not visible to a join", tc.column, tc.why)
			continue
		}
		if got.Name != tc.name || got.Type != tc.typ || got.Nullable != tc.nullable {
			t.Errorf("%s (%s): got {%s %s nullable=%v}, want {%s %s nullable=%v}",
				tc.column, tc.why, got.Name, got.Type, got.Nullable, tc.name, tc.typ, tc.nullable)
		}
	}

	// A runtime-only field has no column, so there is nothing to traverse onto —
	// and a claim with an empty column would match a join that named none.
	if _, leaked := byColumn[""]; leaked {
		t.Error("a column-less field must not enter the claim: it would match an empty column")
	}
}
