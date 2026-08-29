package spec

import (
	"strings"
	"testing"
)

// grantingSpec is the smallest entity carrying the collection this key exists
// for: a per-entry collection whose identity is a reference nobody edits, plus
// one field that does change.
func grantingSpec() *Spec {
	s := minimalSpec()
	s.Modes = []string{"display", "insert", "update"}
	s.Update = Update{Shape: "patch"}
	s.Authz.Permissions = map[string]string{
		"insert": "student:write", "patch": "student:write", "read": "student:read",
	}
	s.Children = []Child{{
		Name: "Grant", Plural: "Grants", Table: "student_grants",
		ParentColumn: "student_id", Description: "O que o aluno pode.",
		OwnedBy: "root", EditStrategy: "per-child",
		BusinessIdentity: []string{"ClaimID"},
		Fields: []Field{
			{Name: "ClaimID", Type: "id", Column: "claim_id",
				Example: "3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31", Description: "Permissão do catálogo."},
			{Name: "Value", Type: "string", Column: "value", Length: 40,
				Example: "leitura", Description: "O que a entrada concede."},
		},
	}}
	return s
}

func blockersAt(ps *Problems, prefix string) []string {
	var out []string
	for _, p := range ps.Blockers() {
		if strings.HasPrefix(p.Where, prefix) {
			out = append(out, p.String())
		}
	}
	return out
}

// TestGrantingSpecIsClean guards the fixture: every refusal asserted below is
// vacuous if the baseline already refuses the same place.
func TestGrantingSpecIsClean(t *testing.T) {
	ps := Validate(grantingSpec(), Options{})
	if ps.HasBlockers() {
		t.Fatalf("the baseline should validate cleanly, got:\n%v", ps.Error())
	}
	if got := ChildChangeShape(grantingSpec().Children[0]); got != "put" {
		t.Errorf("a collection that says nothing must keep serving the full replacement, got %q", got)
	}
}

// A partial change that still accepts the entry's identity is the whole defect
// this key was added to close: the caller sends a different reference, the
// aggregate keeps the row id, and the history reads as one grant being edited
// where a grant was really swapped for another. It has to be refused rather
// than fixed silently — the author writes the same `patchExcludes` line the
// root writes for its natural key, so the two levels read alike.
func TestPatchThatCanReKeyTheEntryIsRefused(t *testing.T) {
	s := grantingSpec()
	s.Children[0].Change = &ChildChange{Shape: "patch"}

	got := strings.Join(blockersAt(Validate(s, Options{}), "children[0]"), "\n")
	if got == "" {
		t.Fatal("a partial change that accepts the business identity must be refused")
	}
	for _, want := range []string{"ClaimID", "row id", "patchExcludes"} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal omits %q, so it does not say what to write:\n%s", want, got)
		}
	}
}

// Excluded, the same spec is accepted — and the shape resolves to the two verbs
// the emitters branch on.
func TestPatchWithTheIdentityExcludedIsAccepted(t *testing.T) {
	for _, shape := range []string{"patch", "both"} {
		t.Run(shape, func(t *testing.T) {
			s := grantingSpec()
			s.Children[0].Change = &ChildChange{Shape: shape, PatchExcludes: []string{"ClaimID"}}

			if ps := Validate(s, Options{}); ps.HasBlockers() {
				t.Fatalf("this is the shape the key exists for:\n%v", ps.Error())
			}
			c := s.Children[0]
			if !ChildServesPatch(c) {
				t.Error("the collection serves no patch, which is what it just declared")
			}
			if put := ChildServesPut(c); put != (shape == "both") {
				t.Errorf("shape %q serves put = %v", shape, put)
			}
		})
	}
}

// An entry with nothing outside its identity has no partial change to serve:
// every field is excluded, so the verb would accept a body and do nothing. The
// honest answer is the one `operations` already offers, and the refusal says so
// rather than leaving the author to discover an endpoint that never changes
// anything.
func TestPatchWithNothingLeftToChangeIsRefused(t *testing.T) {
	s := grantingSpec()
	s.Children[0].BusinessIdentity = []string{"ClaimID", "Value"}
	s.Children[0].Change = &ChildChange{Shape: "patch", PatchExcludes: []string{"ClaimID", "Value"}}

	got := strings.Join(blockersAt(Validate(s, Options{}), "children[0]"), "\n")
	if !strings.Contains(got, "never change anything") || !strings.Contains(got, "operations") {
		t.Errorf("the refusal should point at operations: [add, remove]:\n%s", got)
	}
}

// Every way the block itself can be in the wrong place. Each one is silent in
// the generated code — a shape nobody reads, an exclusion on a verb that does
// not exist, a block whose default is the opposite of what it looks like — so
// each is refused by name instead of accepted and dropped.
func TestChangeBlockOutOfPlaceIsRefused(t *testing.T) {
	cases := map[string]func(*Spec){
		"on an atomic-replace collection": func(s *Spec) {
			s.Children[0].EditStrategy = "atomic-replace"
			s.Children[0].Change = &ChildChange{Shape: "patch"}
		},
		"on a collection that mounts no change": func(s *Spec) {
			s.Children[0].Operations = []string{"add", "remove"}
			s.Children[0].Change = &ChildChange{Shape: "patch"}
		},
		"declared without a shape": func(s *Spec) {
			s.Children[0].Change = &ChildChange{PatchExcludes: []string{"ClaimID"}}
		},
		"excluding a field the entry does not have": func(s *Spec) {
			s.Children[0].Change = &ChildChange{Shape: "patch", PatchExcludes: []string{"ClaimID", "Nope"}}
		},
		"with a shape outside the closed set": func(s *Spec) {
			s.Children[0].Change = &ChildChange{Shape: "partial", PatchExcludes: []string{"ClaimID"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := grantingSpec()
			mutate(s)
			if len(blockersAt(Validate(s, Options{}), "children[0]")) == 0 {
				t.Error("this must be refused, not accepted and ignored")
			}
		})
	}
}

// A collection named after its own entity would have the emitters write
// Patch<Name>Command into the same package the entity's own patch types live
// in. It compiles nowhere, and the author cannot fix generated code — so the
// spec is refused with the rename as the answer.
func TestCollectionNamedAfterItsEntityCannotServeAPatch(t *testing.T) {
	s := grantingSpec()
	s.Entity, s.Plural = "Grant", "GrantRecords"
	s.Authz.Resource = "grant"
	s.Children[0].Change = &ChildChange{Shape: "patch", PatchExcludes: []string{"ClaimID"}}

	got := strings.Join(blockersAt(Validate(s, Options{}), "children[0]"), "\n")
	if !strings.Contains(got, "PatchGrantCommand") {
		t.Errorf("the refusal should name the type that would be declared twice:\n%s", got)
	}
}

// The warning about a change that can only swap one entry for another is about
// the PUT. A collection serving patch alone has no such verb, and repeating the
// observation there would send the author to answer something they already did.
func TestTheSwapWarningIsAboutThePutOnly(t *testing.T) {
	s := grantingSpec()
	s.Children[0].BusinessIdentity = []string{"ClaimID", "Value"}

	if w := warningsAt(Validate(s, Options{}), "children[0] (Grant).businessIdentity"); len(w) == 0 {
		t.Fatal("a put-shaped change over an all-identity entry should still warn")
	}

	s.Children[0].Operations = []string{"add", "remove"}
	if w := warningsAt(Validate(s, Options{}), "children[0] (Grant).businessIdentity"); len(w) > 0 {
		t.Errorf("the author answered it with operations, and it still warns: %v", w)
	}
}
