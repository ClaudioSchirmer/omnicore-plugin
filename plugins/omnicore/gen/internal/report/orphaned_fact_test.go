package report

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// The hook file is written ONCE and never again, so it drifts from the spec in
// both directions — and only one of them was ever reported.
//
// A fact ADDED after the file existed declares a method on the port with
// nothing behind it, which is a compile error the report already names. A fact
// REMOVED left its body behind, and nothing anywhere said so: dead code that
// still compiled, in a file the generator is not allowed to open.
//
// It stopped being merely dead when a fact could take a generated entry
// carrier. The carrier is declared beside the port and leaves with the fact, so
// the stranded body fails to build — the compiler names `undefined:
// <Entity><Fact>Entry`, a symbol, and nothing says which decision produced it.

func serviceModel() *ir.Model {
	return &ir.Model{
		Entity: ir.Names{Pascal: "Papel", Snake: "papel"},
		Table:  "papeis",
		Service: &ir.ServiceModel{
			Impl: "PapelServiceImpl",
			Facts: []ir.Fact{{
				Name: "PermissaoIndisponivel", Kind: "manual", Manual: true,
				ReturnType: "bool", Description: "Quais permissões não existem mais.",
			}},
		},
	}
}

// TestAnOrphanedBodyIsNamed. The report is the hand-off, so it is where "this
// file no longer matches the spec" has to be said — by name, and with what to
// do about it.
func TestAnOrphanedBodyIsNamed(t *testing.T) {
	got := Render(Input{
		Model: serviceModel(), SpecPath: "omnicore-gen/papel.omnicore.yaml",
		OrphanedFacts: []string{"RotuloNaoConfere", "PermissaoVetada"},
	})
	for _, want := range []string{
		"bodies the spec no longer asks for",
		"func (s *PapelServiceImpl) RotuloNaoConfere(...)",
		"func (s *PapelServiceImpl) PermissaoVetada(...)",
		"Delete these",
		"undefined: <Entity><Fact>Entry",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not carry %q:\n%s", want, got)
		}
	}
}

// TestNoOrphansMeansNoSection. A report that prints an empty heading trains its
// reader to skip headings, which is the opposite of what the hand-off is for.
func TestNoOrphansMeansNoSection(t *testing.T) {
	got := Render(Input{
		Model: serviceModel(), SpecPath: "omnicore-gen/papel.omnicore.yaml",
	})
	if strings.Contains(got, "bodies the spec no longer asks for") {
		t.Errorf("the section appeared with nothing to report:\n%s", got)
	}
}

// TestTheOrphanSectionStandsAlone is the case the placement is FOR: when the
// last manual fact is the one the spec dropped, there is no manual-facts
// section for this to hang under — and that is the run where the hook file is
// most out of step.
func TestTheOrphanSectionStandsAlone(t *testing.T) {
	m := serviceModel()
	m.Service.Facts = nil
	got := Render(Input{
		Model: m, SpecPath: "omnicore-gen/papel.omnicore.yaml",
		OrphanedFacts: []string{"PermissaoIndisponivel"},
	})
	if !strings.Contains(got, "func (s *PapelServiceImpl) PermissaoIndisponivel(...)") {
		t.Errorf("with no manual facts left, the orphan is not reported at all:\n%s", got)
	}
}

// TestTheManualFactSignatureIsTheONEThePortDeclares. The report used to build
// the line itself from the parameter list and the return TYPE, which stopped
// being the whole answer the moment a fact could answer a map or take a
// generated carrier — it would have printed a signature the author cannot
// paste.
func TestTheManualFactSignatureIsTheONEThePortDeclares(t *testing.T) {
	m := serviceModel()
	m.Service.Facts = []ir.Fact{{
		Name: "RotuloNaoConfere", Kind: "manual", Manual: true, ReturnType: "bool",
		Description: "Quais entradas trazem um rótulo errado.",
		Params: []ir.FactParam{{
			Name: "entries", GoType: "[]PapelRotuloNaoConfereEntry",
			Field: "PermissaoID", PerEntry: "Permissoes", Role: "per-entry",
		}},
		PerEntry: &ir.FactPerEntry{
			Collection: "Permissoes", EntryType: "PapelRotuloNaoConfereEntry",
			Param: "entries",
			Key:   ir.FactParam{Name: "permissaoID", GoType: "domain.ID", Field: "PermissaoID"},
			Fields: []ir.FactParam{
				{Name: "permissaoID", GoType: "domain.ID", Field: "PermissaoID"},
				{Name: "rotulo", GoType: "string", Field: "Rotulo"},
			},
		},
	}}
	got := Render(Input{Model: m, SpecPath: "omnicore-gen/papel.omnicore.yaml"})
	want := "RotuloNaoConfere(entries []PapelRotuloNaoConfereEntry) map[domain.ID]bool"
	if !strings.Contains(got, want) {
		t.Errorf("the report does not print the signature the port declares (%s):\n%s", want, got)
	}
	// The old shape: parameters, then the bare return TYPE. For a batched fact
	// that is `... ) bool`, which is not what the method answers.
	if strings.Contains(got, "RotuloNaoConfere(entries []PapelRotuloNaoConfereEntry) bool") {
		t.Errorf("the report printed the map's VALUE type as the return type:\n%s", got)
	}
}
