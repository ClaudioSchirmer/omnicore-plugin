package ir

import (
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// TestAJoinedArchiveColumnLandsInAPointer is the one thing about a traversal
// onto a framework-stamped column that only the GENERATOR can get right.
//
// The framework checks a join field's nullability against the target's Go type,
// and a managed slot has none to check: domain.Managed keeps CreatedAt,
// UpdatedAt and DeletedAt unexported, so FieldByName finds nothing and the check
// answers "not nullable" rather than guessing. A non-pointer time.Time therefore
// passes repository construction and fails on the first ACTIVE row scanned —
// deleted_at IS NULL being the normal state of a row, not the exception.
//
// The generator reads the column out of the target's own storage.managed, so it
// is the side that knows. The two timestamps stay values; the archive column
// becomes a pointer under EITHER join kind, because an inner join proves the
// joined ROW exists and never that a column of it is filled.
func TestAJoinedArchiveColumnLandsInAPointer(t *testing.T) {
	target := &discover.SpecClaim{
		Entity: "Campus", Table: "campi", Revision: "revision",
		Fields: []discover.FieldClaim{
			{Name: "Label", Column: "label", Type: "string", LivesOn: "root"},
			{Name: "CreatedAt", Column: "created_at", Type: "time", LivesOn: "root"},
			{Name: "UpdatedAt", Column: "updated_at", Type: "time", LivesOn: "root"},
			{Name: "DeletedAt", Column: "deleted_at", Type: "time", LivesOn: "root", Nullable: true},
		},
	}

	for _, tc := range []struct {
		kind, column, want string
		why                string
	}{
		{"inner", "created_at", "time.Time", "stamped on insert: never NULL"},
		{"inner", "updated_at", "time.Time", "stamped on every write: never NULL"},
		{"inner", "deleted_at", "*time.Time",
			"NULL on every active row, and the framework cannot say so about a managed slot"},
		{"left", "created_at", "*time.Time",
			"a left join with no counterpart produces NULL whatever the column declares"},
	} {
		t.Run(tc.kind+" "+tc.column, func(t *testing.T) {
			j := spec.Join{Kind: tc.kind, To: "Campus", On: "campus_id"}
			f := spec.JoinField{Name: "CampusStamp", Column: tc.column}
			got := resolveJoinField("Student", tc.kind, j, f, target)
			if got.GoType != tc.want {
				t.Errorf("%s across a %s join: got %s, want %s — %s",
					tc.column, tc.kind, got.GoType, tc.want, tc.why)
			}
			if got.SpecType != "time" {
				t.Errorf("a stamped column crosses as a timestamp, got %q", got.SpecType)
			}
		})
	}
}

// TestAbsenceFollowsThePathNotTheHop is the rule a chain adds, and the one that
// is wrong by default: a hop's own kind decides the framework VERB, but what can
// be ABSENT is decided by the whole block it hangs in. One left anywhere above
// makes every field below it a pointer — an inner hop three levels down a left
// chain still lands NULL on the root that never matched.
func TestAbsenceFollowsThePathNotTheHop(t *testing.T) {
	city := discover.SpecClaim{
		Entity: "City", Table: "cities",
		Fields: []discover.FieldClaim{
			{Name: "CityName", Column: "name", Type: "string", LivesOn: "root"},
		},
	}
	campus := discover.SpecClaim{
		Entity: "Campus", Table: "campi",
		Fields: []discover.FieldClaim{
			{Name: "CampusLabel", Column: "label", Type: "string", LivesOn: "root"},
		},
	}
	p := &discover.Project{SiblingSpecs: []discover.SpecClaim{campus, city}}
	m := &Model{Entity: Names{Pascal: "Student"}}

	head := spec.Join{
		Kind: "left", To: "Campus", On: "campus_id",
		Fields: []spec.JoinField{{Name: "CampusName", Column: "label"}},
		Then: []spec.Join{{
			// Declared INNER, and every column it maps is NOT NULL over there.
			Kind: "inner", To: "City", On: "city_id",
			Fields: []spec.JoinField{{Name: "CampusCityName", Column: "name"}},
		}},
	}

	got := resolveJoinHop(&spec.Spec{}, p, m, head, "", false, "")
	if len(got.Through) != 1 {
		t.Fatalf("the hop did not resolve: %+v", got)
	}
	hop := got.Through[0]
	if hop.Kind != "inner" {
		t.Errorf("the hop's own kind decides the VERB and must survive: got %q", hop.Kind)
	}
	if hop.PathKind != "left" {
		t.Errorf("under a left head the PATH is left, got %q", hop.PathKind)
	}
	if hop.Fields[0].GoType != "*string" {
		t.Errorf("a non-nullable column under a left path still lands NULL, so the field is a "+
			"pointer: got %s", hop.Fields[0].GoType)
	}
	if hop.Via != "Campus" {
		t.Errorf("the hop should record what it continues from, got %q", hop.Via)
	}
	if hop.Child != "" {
		t.Errorf("a hop hangs off nothing of its own, got child %q", hop.Child)
	}
}
