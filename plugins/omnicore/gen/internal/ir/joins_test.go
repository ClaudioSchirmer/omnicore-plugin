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
			got := resolveJoinField("Student", j, f, target)
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
