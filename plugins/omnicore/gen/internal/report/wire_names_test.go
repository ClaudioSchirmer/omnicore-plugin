package report

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// TestRenamedFieldsReachTheTableFromEveryScope guards the one thing the
// "Fields the wire calls something else" section exists for: a wire rename is
// the single field-level decision a reviewer cannot recover by reading the
// spec's field list, and its cost is asymmetric — the Go name and the column
// stay renameable, these are what every caller already wrote against.
//
// The section shipped walking the OWNER's fields only, so a rename declared
// inside a collection was accepted by the validator, honored by the emitters
// (the child struct gets its notifyAs tag like any other) and listed nowhere —
// invisible in exactly the review the section is meant to serve, and more so
// than a root field, since it sits one level down in the spec.
func TestRenamedFieldsReachTheTableFromEveryScope(t *testing.T) {
	model := &ir.Model{
		Entity: ir.Names{Pascal: "Assinante", PluralSnake: "assinantes"},
		Table:  "assinantes",
		Fields: []ir.Field{
			{Name: "NationalID", JSONName: "cpf", NotifyAs: "cpf"},
			{Name: "FullName", JSONName: "fullName"}, // derived: not a rename
		},
		Children: []ir.Child{{
			Name: "Telefone", Plural: "Telefones", GoPlural: "Telefones", Segment: "telefones",
			Fields: []ir.Field{
				{Name: "Numero", JSONName: "linha", NotifyAs: "linha"},
				{Name: "Rotulo", JSONName: "rotulo"}, // derived: not a rename
			},
		}},
	}

	out := Render(Input{Model: model, SpecPath: "omnicore-gen/assinante.omnicore.yaml"})

	if !strings.Contains(out, "| `NationalID` | `cpf` | `cpf` |") {
		t.Error("the root's renamed field is missing from the table")
	}
	// Scoped, because two collections may each carry a `Numero` and an
	// unqualified row would not say which one to go and look at.
	if !strings.Contains(out, "| `Telefones[].Numero` | `linha` | `linha` |") {
		t.Errorf("a renamed field inside a collection never reaches the table:\n%s",
			lineWith(out, "wire calls something else"))
	}
	// A field on its derived name is not a decision anybody made, in either
	// scope. Listing it would bury the rows that ARE decisions.
	for _, unwanted := range []string{"`FullName`", "`Telefones[].Rotulo`"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("%s renders to its default and must not be listed as renamed", unwanted)
		}
	}
}

// TestTheTwoHalvesAreFlaggedWhenTheyDisagree: a split vocabulary is legal and
// deliberate, and it is also the shape a reviewer must stop on — the caller
// posts under one name and is refused under the other. The warning has to name
// the field in the same scoped form the table uses, or it points at nothing.
func TestTheTwoHalvesAreFlaggedWhenTheyDisagree(t *testing.T) {
	out := Render(Input{
		Model: &ir.Model{
			Entity: ir.Names{Pascal: "Assinante", PluralSnake: "assinantes"},
			Table:  "assinantes",
			Children: []ir.Child{{
				Name: "Telefone", Plural: "Telefones", GoPlural: "Telefones", Segment: "telefones",
				Fields: []ir.Field{{Name: "Numero", JSONName: "linha", NotifyAs: "numeroDeLinha"}},
			}},
		},
		SpecPath: "omnicore-gen/assinante.omnicore.yaml",
	})

	if !strings.Contains(out, "The two columns disagree for `Telefones[].Numero`") {
		t.Errorf("a split vocabulary inside a collection is not flagged:\n%s",
			lineWith(out, "disagree"))
	}
}
