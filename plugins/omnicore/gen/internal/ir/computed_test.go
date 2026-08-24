package ir

import (
	"strings"
	"testing"
)

// A derivation's sources are resolved ONCE, here, and the emitters read the
// result. They used to be resolved twice against two different sets — the
// validator blessed six categories, the emitter looked two of them up — and the
// gap was swallowed by a bare `continue`: the signature came out one parameter
// short, it compiled, and the field was empty on every surface forever.
func TestARootJoinsFieldIsADerivationSource(t *testing.T) {
	r := &ReadModel{
		Backing:  "relational",
		Computed: []ComputedField{{Name: "Rotulo", Sources: []string{"Nome", "CidadeNome", "CreatedAt"}}},
		Managed:  []Field{{Name: "CreatedAt", GoType: "time.Time"}},
		// A join's fields reach the read model and nothing else — they are
		// nowhere in the entity's own list, which is exactly why the emitter
		// used to lose them.
		JoinFields: []Field{{Name: "CidadeNome", GoType: "string"}},
	}
	owner := []Field{{Name: "Nome", GoType: "string"}}

	if err := bindComputedSources(r, owner); err != nil {
		t.Fatalf("a root join's field is readable and must be derivable: %v", err)
	}
	got := r.Computed[0].SourceFields
	if len(got) != 3 {
		t.Fatalf("the derivation lost a source: %d of 3 resolved (%v)", len(got), got)
	}
	for i, want := range []string{"Nome", "CidadeNome", "CreatedAt"} {
		if got[i].Name != want {
			t.Errorf("source %d is %q, want %q — the order IS the signature", i, got[i].Name, want)
		}
	}
}

// The refusal that replaces the silence. A name nothing answers to means the
// validator and this resolver have drifted apart, and the only honest outcome is
// a generator that stops: the alternative compiles and ships an empty field.
func TestAnUnresolvableSourceStopsTheGenerator(t *testing.T) {
	r := &ReadModel{Computed: []ComputedField{{Name: "Rotulo", Sources: []string{"NaoExiste"}}}}
	err := bindComputedSources(r, []Field{{Name: "Nome", GoType: "string"}})
	if err == nil {
		t.Fatal("an unresolvable source was accepted — the signature would be one parameter short")
	}
	if !strings.Contains(err.Error(), "NaoExiste") || !strings.Contains(err.Error(), "Rotulo") {
		t.Errorf("the error names neither the source nor the field: %v", err)
	}
}

// The per-entry scope: the entry's own fields and what a join declared inChild
// brought onto it — the root's are deliberately out of reach, because the
// framework pushes a nested field's sources down under its OWN segment.
func TestAnEntryDerivationReadsTheEntry(t *testing.T) {
	m := &Model{Children: []Child{{
		Name: "LinhaCF", Plural: "LinhasCF",
		Fields:     []Field{{Name: "Sku", GoType: "string"}},
		JoinFields: []Field{{Name: "ProdutoNome", GoType: "string"}},
		Computed:   []ComputedField{{Name: "Rotulo", Sources: []string{"Sku", "ProdutoNome"}}},
	}}}
	if err := bindChildComputedSources(m); err != nil {
		t.Fatalf("the entry's own fields must be derivable: %v", err)
	}
	if n := len(m.Children[0].Computed[0].SourceFields); n != 2 {
		t.Fatalf("the per-entry derivation lost a source: %d of 2 resolved", n)
	}
}

func TestAnEntryDerivationCannotReachTheRoot(t *testing.T) {
	m := &Model{
		Fields: []Field{{Name: "Codigo", GoType: "string"}},
		Children: []Child{{
			Name: "LinhaCF", Plural: "LinhasCF",
			Fields:   []Field{{Name: "Sku", GoType: "string"}},
			Computed: []ComputedField{{Name: "Rotulo", Sources: []string{"Codigo"}}},
		}},
	}
	err := bindChildComputedSources(m)
	if err == nil {
		t.Fatal("a root field was accepted as a per-entry source — the store would be " +
			"asked for LinhasCF.Codigo, which is not a path any document has")
	}
	if !strings.Contains(err.Error(), "Codigo") {
		t.Errorf("the error does not name the source: %v", err)
	}
}
