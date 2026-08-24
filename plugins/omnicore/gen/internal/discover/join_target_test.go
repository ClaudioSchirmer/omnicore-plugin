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

// TestSpecClaimExposesTheFrameworkStampedColumns pins the second class of column
// a join can traverse onto and nothing under fields[] ever mentions.
//
// The createdAt, updatedAt and archivedAt slots are declared BY PRESENCE under
// storage.managed, and the framework resolves whatever column sits in each one on
// the READ path, under a fixed logical name — which is exactly what
// read.WithJoins checks a mapped column against. Reading only fields[] made them
// invisible, so the generator refused a legal traversal with the one message an
// author cannot act on: "not a column of X", about a column that is one.
//
// The column NAMES are the author's, not the framework's, which is the whole
// reason they have to be read out of the file: this fixture spells them the
// conventional way, and a spec that spells the archive column dt_exclusao is
// exactly as valid.
//
// The archive column's NULLABILITY is the half only this side can supply. The
// framework does not police the nullability of a managed slot — domain.Managed
// keeps those fields unexported, so its reflective check has nothing to point at
// and answers "not nullable" rather than guessing — and a non-pointer Go field
// therefore survives construction and fails on the first ACTIVE row scanned,
// "never archived" being the normal state.
//
// The revision is deliberately NOT a field of the claim: the read path does not
// resolve it, so a join onto it has to be refused. It is carried apart, by name,
// so the refusal can say which column it is instead of denying it exists.
func TestSpecClaimExposesTheFrameworkStampedColumns(t *testing.T) {
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
  managed:
    revision: revision
    createdAt: created_at
    updatedAt: updated_at
    archivedAt: deleted_at
fields:
  - {name: Label, type: string, column: label, livesOn: root}
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
		nullable bool
		why      string
	}{
		{"created_at", "CreatedAt", false, "stamped on insert, never NULL"},
		{"updated_at", "UpdatedAt", false, "stamped on every write, never NULL"},
		{"deleted_at", "DeletedAt", true,
			"NULL on every row that is not archived, which is the normal state"},
	} {
		got, ok := byColumn[tc.column]
		if !ok {
			t.Errorf("%s (%s) is not visible to a join, but the framework resolves it", tc.column, tc.why)
			continue
		}
		if got.Name != tc.name || got.Type != "time" || got.Nullable != tc.nullable {
			t.Errorf("%s (%s): got {%s %s nullable=%v}, want {%s time nullable=%v}",
				tc.column, tc.why, got.Name, got.Type, got.Nullable, tc.name, tc.nullable)
		}
		if got.LivesOn == "base" || got.LivesOn == "sibling" {
			t.Errorf("%s must read as a column of the target's OWN table, got livesOn=%q",
				tc.column, got.LivesOn)
		}
	}

	if _, reachable := byColumn["revision"]; reachable {
		t.Error("the revision is not resolvable on the read path — claiming it as a field " +
			"would let a join onto it through to a panic at repository construction")
	}
	if claims[0].Revision != "revision" {
		t.Errorf("the revision column must be carried by name so the refusal can be specific, got %q",
			claims[0].Revision)
	}
}

// TestAnUndeclaredStampedColumnStaysInvisible: the managed columns are declared
// by PRESENCE, so a spec that names none has none — and an empty column must
// never enter the claim, where it would match a join that named none.
func TestAnUndeclaredStampedColumnStaysInvisible(t *testing.T) {
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
  managed: {revision: revision}
fields:
  - {name: Label, type: string, column: label, livesOn: root}
`
	if err := os.WriteFile(filepath.Join(specs, "campus"+layout.SpecSuffix), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	claims := discoverSpecs(dir)
	if len(claims) != 1 {
		t.Fatalf("expected one claim, got %d", len(claims))
	}
	for _, f := range claims[0].Fields {
		if f.Column == "" {
			t.Errorf("a managed column that is not declared must not enter the claim: %+v", f)
		}
		if f.Name != "Label" {
			t.Errorf("this spec stamps nothing but the revision; %s must not be reachable", f.Name)
		}
	}
}
