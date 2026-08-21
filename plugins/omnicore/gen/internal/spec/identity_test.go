package spec

import (
	"strings"
	"testing"
)

// The aggregate id used to be nameable NOWHERE on the read side.
//
// It is not a declared field — `fields[].name: ID` is a reserved name and a
// boot panic — and it is not one of the framework-stamped columns `read.managed`
// admits, which are the three timestamps and nothing else. So every read key
// resolved it through the declared set, found nothing, and refused it with "ID
// does not name a readable field": true, useless, and about the one column every
// entity has. `?orderBy=id` — a listing's cheapest total order, and the tie-break
// the cursor already appends to every key — was inexpressible.
//
// These tests pin both halves of the answer: the id filters and orders like the
// column it is, and stays refused where it would name a PROJECTED field, with a
// refusal that says which of the two it is.

func listingSpec() *Spec {
	s := minimalSpec()
	s.Read.ByParams = &ByParams{
		Filters:  []Filter{{Field: "Name", Ops: []string{"eq"}}},
		Sort:     []string{"Name"},
		Controls: Controls{OrderBy: true},
	}
	return s
}

// refusal validates and returns the blockers as text, failing when there are
// none: an assertion over the MESSAGE is vacuous if nothing was refused.
func refusal(t *testing.T, s *Spec) string {
	t.Helper()
	ps := Validate(s, Options{})
	if !ps.HasBlockers() {
		t.Fatal("this must be refused, and nothing was")
	}
	return ps.Error().Error()
}

func TestIdentityIsSortable(t *testing.T) {
	s := listingSpec()
	s.Read.ByParams.Sort = []string{"ID", "Name"}
	if ps := Validate(s, Options{}); ps.HasBlockers() {
		t.Fatalf("ordering by the aggregate id must be accepted, got:\n%v", ps.Error())
	}
}

func TestIdentityIsFilterable(t *testing.T) {
	s := listingSpec()
	s.Read.ByParams.Filters = append(s.Read.ByParams.Filters,
		Filter{Field: "ID", Ops: []string{"eq", "in"}})
	if ps := Validate(s, Options{}); ps.HasBlockers() {
		t.Fatalf("filtering by the aggregate id must be accepted, got:\n%v", ps.Error())
	}
}

// An id is opaque: eq/ne/in/nin answer for it and a range does not. The check is
// the ordinary per-type one, which is the point — the identity resolves to a
// field of type id and is then held to the same rules as any other id column.
func TestIdentityRefusesAnOrderedOperator(t *testing.T) {
	s := listingSpec()
	s.Read.ByParams.Filters = append(s.Read.ByParams.Filters,
		Filter{Field: "ID", Ops: []string{"gte"}})
	if msg := refusal(t, s); !strings.Contains(msg, "ordered type") {
		t.Errorf("the refusal should be the type check, got:\n%v", msg)
	}
}

// The other half. These keys address a column in the PROJECTION, and the id is
// not in it — the framework carries it and the projector writes it as the
// document's _id. Accepting them would emit an index over a field the document
// does not have, a restriction on the handle every response must carry, and a
// derivation from a value the reader never selects.
func TestIdentityIsRefusedWhereItWouldNameAProjection(t *testing.T) {
	cases := map[string]func(*Spec){
		"read.indexes": func(s *Spec) {
			s.Read.Backing = "mongo"
			s.Read.Indexes = []Index{{Name: "by_id", Fields: []string{"ID"}}}
		},
		"read.fieldRestrict": func(s *Spec) {
			s.Read.FieldRestrict = []FieldRestrict{{Field: "ID", Permission: "student:admin"}}
		},
		"read.computed": func(s *Spec) {
			s.Read.Computed = []Computed{{
				Name: "Handle", Type: "string", From: []string{"ID"},
				Example: "abc", Description: "O id em outra forma.",
			}}
		},
		"controls.search": func(s *Spec) {
			s.Read.Backing = "mongo"
			s.Read.Indexes = []Index{{Name: "busca", Fields: []string{"Name"}, Text: true}}
			s.Read.ByParams.Controls.Search = []string{"ID"}
		},
	}
	for key, apply := range cases {
		t.Run(key, func(t *testing.T) {
			s := listingSpec()
			apply(s)
			// The refusal has to teach where the id DOES work, or the author
			// learns only that it is unavailable — which is what sends someone
			// looking for a workaround.
			msg := refusal(t, s)
			if !strings.Contains(msg, "read.byParams.sort") {
				t.Errorf("the refusal should point at the two keys that accept the id, got:\n%v", msg)
			}
		})
	}
}

// A spelling that is not the exact name lands on the same explanation rather
// than on "does not name a readable field", which said nothing about the id at
// all.
func TestIdentityMisspeltIsExplained(t *testing.T) {
	s := listingSpec()
	s.Read.ByParams.Sort = []string{"id"}
	if msg := refusal(t, s); !strings.Contains(msg, `"ID"`) {
		t.Errorf("the refusal should name the spelling that works, got:\n%v", msg)
	}
}

// Nothing above loosened the write side: declaring the id as a field is still
// the boot panic it always was.
func TestIdentityIsStillNotADeclarableField(t *testing.T) {
	s := minimalSpec()
	s.Fields = append(s.Fields, Field{
		Name: "ID", Type: "id", Column: "id_alt", LivesOn: "root",
		Example: "x", Description: "Nao.",
	})
	if ps := Validate(s, Options{}); !ps.HasBlockers() {
		t.Fatal("fields[].name: ID must stay reserved")
	}
}
