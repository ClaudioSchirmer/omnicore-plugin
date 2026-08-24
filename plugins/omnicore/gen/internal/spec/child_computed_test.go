package spec

import (
	"strings"
	"testing"
)

// read.computed used to be the ONLY seat, and it is the wrong one for anything
// about a row of a collection: the derivation runs once per document, and what
// the root holds for a collection is a slice. A `from:` naming a collection's
// field validated green — readableField accepts it, six categories wide — and
// then vanished in emission.
//
// Both directions are pinned here, because the two refusals are each other's
// signpost: a root derivation reaching into a collection is sent to
// children[].computed, and a per-entry one reaching up is sent to read.computed.
const childComputedSpec = `
specVersion: 1
entity: CestaT
plural: CestasT
language: pt-BR
storage:
  kind: flat
  table: cestas_t
  description: Cestas.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Codigo, type: string, column: codigo, length: 20, livesOn: root, example: "CST-1", description: O código.}
children:
  - name: LinhaT
    plural: LinhasT
    table: cesta_t_linhas
    parentColumn: cesta_t_id
    description: Linhas.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [Sku]
    fields:
      - {name: Sku, type: string, column: sku, length: 20, example: "SKU-9", description: O item.}
    computed:
      - name: Rotulo
        type: string
        from: [CHILD_FROM]
        example: "SKU-9"
        description: O rótulo da linha.
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
read:
  backing: relational
  view: {name: cestas_t}
  computed:
    - name: Resumo
      type: string
      from: [ROOT_FROM]
      example: "CST-1"
      description: O resumo.
  byId: true
surfaces: {rest: true}
authz:
  resource: cestat
  dataAccess: anyone-with-permission
  permissions: {insert: "cestat:escrever", update: "cestat:escrever", patch: "cestat:escrever", archive: "cestat:arquivar", read: "cestat:ler"}
`

func childComputedProblems(t *testing.T, rootFrom, childFrom string) *Problems {
	t.Helper()
	src := strings.Replace(childComputedSpec, "ROOT_FROM", rootFrom, 1)
	src = strings.Replace(src, "CHILD_FROM", childFrom, 1)
	s, err := Parse([]byte(src), "cesta.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	return Validate(s, Options{})
}

func TestPerEntryComputedIsAccepted(t *testing.T) {
	if ps := childComputedProblems(t, "Codigo", "Sku"); ps.HasBlockers() {
		t.Fatalf("the per-entry seat is refused:\n%v", ps.Error())
	}
}

// The silent one: green check, successful generate, a field empty forever.
func TestARootDerivationCannotReadACollection(t *testing.T) {
	ps := childComputedProblems(t, "LinhasT.Sku", "Sku")
	got := blockerSaying(ps, "LinhasT.Sku")
	if got == "" {
		t.Fatalf("a collection's field is still accepted as a root source:\n%v", ps.Error())
	}
	if !strings.Contains(got, "children[]") {
		t.Errorf("the refusal does not point at the seat that CAN answer it: %s", got)
	}
	// And it must offer the spelling THAT KEY takes — bare. Echoing back the
	// dotted form the author wrote sends them to a key that refuses exactly
	// that, which is the same defect as an error message arguing against the
	// three keys next to it.
	if !strings.Contains(got, "from: [Sku]") {
		t.Errorf("the fix does not name the source as the per-entry key spells it: %s", got)
	}
	if strings.Contains(got, "from: [LinhasT.Sku]") {
		t.Errorf("the fix echoes the dotted form, which children[].computed refuses: %s", got)
	}
}

// The bare spelling of the same mistake — the author who wrote `from: [Sku]`
// meaning the entry's field. It resolves against the root and finds nothing.
func TestABareCollectionFieldIsRefusedAsARootSource(t *testing.T) {
	ps := childComputedProblems(t, "Sku", "Sku")
	if blockerSaying(ps, "Sku") == "" {
		t.Fatalf("a bare collection field is accepted as a root source:\n%v", ps.Error())
	}
}

func TestAPerEntryDerivationCannotReadTheRoot(t *testing.T) {
	ps := childComputedProblems(t, "Codigo", "Codigo")
	got := blockerSaying(ps, "Codigo")
	if got == "" {
		t.Fatalf("a root field is accepted as a per-entry source:\n%v", ps.Error())
	}
	if !strings.Contains(got, "read.computed") {
		t.Errorf("the refusal does not point at the seat that CAN answer it: %s", got)
	}
}

// A derived name that shadows a stored one is refused at both levels, for the
// same reason: two struct fields with one name is a tree that does not compile,
// and the spec is where that can still be said in words.
// Each source becomes a PARAMETER, so two sources that camel-case to one word
// emit a signature that does not compile — generated code the author did not
// write and cannot fix from the spec that produced it.
//
// The domain-service facts have had this check since a manual fact could take
// two filters; the read side's derivations went without it, at both levels.
func TestADerivationsParametersMustBeDistinct(t *testing.T) {
	// A leading run of capitals lowercases as a unit, so these two field names
	// are ONE parameter: idNumber.
	src := strings.Replace(childComputedSpec, "ROOT_FROM", "IDNumber, IdNumber", 1)
	src = strings.Replace(src, "CHILD_FROM", "Sku", 1)
	src = strings.Replace(src, "children:", `  - {name: IDNumber, type: string, column: id_number, length: 20, livesOn: root, example: "1", description: Um número.}
  - {name: IdNumber, type: string, column: id_number_2, length: 20, livesOn: root, example: "2", description: Outro número.}
children:`, 1)
	s, err := Parse([]byte(src), "cesta.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	ps := Validate(s, Options{})
	if blockerSaying(ps, "both reach the derivation as the parameter") == "" {
		t.Fatalf("two sources that camel to one parameter were accepted:\n%v", ps.Error())
	}
}

// The same source listed twice is the trivial form of it.
func TestASourceListedTwiceIsRefused(t *testing.T) {
	ps := childComputedProblems(t, "Codigo", "Sku, Sku")
	if blockerSaying(ps, "is listed twice") == "" {
		t.Fatalf("a source listed twice was accepted, and it is one parameter twice:\n%v", ps.Error())
	}
}

func TestAPerEntryComputedMayNotShadowAStoredField(t *testing.T) {
	src := strings.Replace(childComputedSpec, "ROOT_FROM", "Codigo", 1)
	src = strings.Replace(src, "CHILD_FROM", "Sku", 1)
	src = strings.Replace(src, "      - name: Rotulo", "      - name: Sku", 1)
	s, err := Parse([]byte(src), "cesta.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	ps := Validate(s, Options{})
	if blockerSaying(ps, "already a field of the collection") == "" {
		t.Fatalf("a derived field shadowing a stored one is accepted:\n%v", ps.Error())
	}
}
