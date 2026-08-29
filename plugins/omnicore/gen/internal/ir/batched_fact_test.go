package ir

import (
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The resolution the emitters read, and the two ways it used to be wrong before
// anything downstream could notice.
//
// One is arithmetic on the archived gate: the language now says it two ways
// (`activeOnly: true` and `scope:`) and every emitter must read ONE resolved
// answer, or each of them becomes a place the two can disagree.
//
// The other is a CRASH. A per-entry filter addressing its collection by the
// entry type's name — the spelling every other key of this language accepts —
// validated cleanly and then resolved against the collection's `plural`, found
// nothing, and hit the panic that guards against generator inconsistency. It is
// a panic, not a wrong answer, so nothing about it is subtle once reached; what
// was subtle is that `check` said the spec was fine.

func resolveFacts(t *testing.T, yaml string) *Model {
	t.Helper()
	s, err := spec.Parse([]byte(yaml), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("the fixture does not validate:\n%v", ps.Error())
	}
	m, err := Resolve(s, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	return m
}

func perEntryIRSpec(factBody string) string {
	return `
specVersion: 1
entity: Papel
plural: Papeis
language: pt-BR
storage:
  kind: flat
  table: papeis
  description: Papéis.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Admin, description: O nome.}
  - {name: DonoID, type: id, column: dono_id, livesOn: root, example: 9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3, description: O dono.}
modes: [display, insert, update, archive, unarchive]
update: {shape: both}
delete: {root: soft}
children:
  - name: PapelPermissao
    plural: Permissoes
    table: papel_permissoes
    parentColumn: papel_id
    description: As permissões do papel.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [PermissaoID]
    fields:
      - {name: PermissaoID, type: id, column: permissao_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: A permissão.}
      - {name: Rotulo, type: string, column: rotulo, length: 60, example: Ler, description: O rótulo.}
service:
  required: true
  facts:
` + factBody + `
read:
  backing: relational
  view: {name: papeis}
  byId: true
surfaces: {rest: true}
authz:
  resource: papel
  dataAccess: anyone-with-permission
  permissions: {insert: "papel:escrever", update: "papel:escrever", patch: "papel:escrever", archive: "papel:arquivar", unarchive: "papel:arquivar", read: "papel:ler"}
`
}

// TestTheBatchParameterReplacesTheScalarOnes is the substitution the whole
// shape depends on: a per-entry leaf contributed one scalar each, and in the
// batched form those same leaves are FIELDS OF AN ENTRY. Left behind, the
// method would take the collection AND one entry's values beside it.
func TestTheBatchParameterReplacesTheScalarOnes(t *testing.T) {
	m := resolveFacts(t, perEntryIRSpec(`    - name: RotuloNaoConfere
      kind: manual
      returns: bool
      perEntry: Permissoes.PermissaoID
      filters: [DonoID, Permissoes.PermissaoID, Permissoes.Rotulo]
      description: Quais entradas trazem um rótulo errado.`))

	f := m.Service.Facts[0]
	if !f.Batched() {
		t.Fatal("the fact did not resolve as batched")
	}
	if got, want := len(f.Params), 2; got != want {
		t.Fatalf("the method takes %d parameter(s), want %d (the root's, plus ONE batch): %+v",
			got, want, f.Params)
	}
	if f.Params[0].Name != "donoID" {
		t.Errorf("the root's own parameter must survive, got %q", f.Params[0].Name)
	}
	if got, want := f.Params[1].GoType, "[]PapelRotuloNaoConfereEntry"; got != want {
		t.Errorf("the batch parameter is %q, want %q", got, want)
	}
	if got, want := len(f.PerEntry.Fields), 2; got != want {
		t.Fatalf("the entry contributes %d field(s), want %d", got, want)
	}
	if f.PerEntry.Fields[0].Field != "PermissaoID" {
		t.Errorf("the KEY must lead the entry's fields, got %q", f.PerEntry.Fields[0].Field)
	}
}

// TestOneEntryFieldNeedsNoCarrier. With the key alone the carrier would be pure
// ceremony, and the parameter is a plain slice of the key's own type.
func TestOneEntryFieldNeedsNoCarrier(t *testing.T) {
	m := resolveFacts(t, perEntryIRSpec(`    - name: PermissaoIndisponivel
      kind: manual
      returns: bool
      perEntry: Permissoes.PermissaoID
      filters: [DonoID]
      description: Quais permissões desta escrita não existem mais.`))

	f := m.Service.Facts[0]
	if f.PerEntry.Carrier() {
		t.Errorf("a carrier was built for an entry contributing only its key: %+v", f.PerEntry)
	}
	if got, want := f.PerEntry.EntryGoType(), "domain.ID"; got != want {
		t.Errorf("one entry travels as %q, want the key's own type %q", got, want)
	}
	if got, want := f.Params[1].Name, "permissaoIDSet"; got != want {
		t.Errorf("the batch parameter is named %q, want %q — it holds many", got, want)
	}
}

// TestThePerEntryKeyResolvesUnderEitherSpelling is the crash. Every key that
// addresses a collection takes both its `plural` and the entry type's `name`,
// and this one resolved only the first — through a prefix trim rather than a
// cut, so the other spelling kept the whole string, matched no field, and
// reached the panic.
func TestThePerEntryKeyResolvesUnderEitherSpelling(t *testing.T) {
	for _, spelling := range []string{"Permissoes", "PapelPermissao"} {
		m := resolveFacts(t, perEntryIRSpec(`    - name: PermissaoIndisponivel
      kind: manual
      returns: bool
      perEntry: `+spelling+`.PermissaoID
      filters: [`+spelling+`.PermissaoID]
      description: Quais permissões desta escrita não existem mais.`))

		f := m.Service.Facts[0]
		if !f.Batched() {
			t.Fatalf("addressed as %q, the collection did not resolve", spelling)
		}
		// The COLLECTION is carried in its canonical plural whichever spelling
		// the author used, so the port's documentation reads the same either way.
		if got, want := f.PerEntry.Collection, "Permissoes"; got != want {
			t.Errorf("addressed as %q, the collection resolved to %q, want %q",
				spelling, got, want)
		}
	}
}

// TestTheArchivedGateResolvesToOneWord. Two spellings in the language, one
// answer downstream — an emitter that had to know both would be a second place
// for them to disagree.
//
// And the DEFAULT is `all`, not `active`: a fact has always included the
// archived rows unless told otherwise, so narrowing it here would silently
// change what every spec written before the key asks.
func TestTheArchivedGateResolvesToOneWord(t *testing.T) {
	for _, tc := range []struct{ declared, want string }{
		{"", "all"},
		{"      activeOnly: true", "active"},
		{"      scope: active", "active"},
		{"      scope: all", "all"},
		{"      scope: archivedOnly", "archivedOnly"},
	} {
		m := resolveFacts(t, perEntryIRSpec(`    - name: Quantos
      kind: count
      filters: [DonoID]
`+tc.declared+`
      description: Quantos papéis.`))
		if got := m.Service.Facts[0].Scope; got != tc.want {
			t.Errorf("%q resolved to scope %q, want %q", tc.declared, got, tc.want)
		}
	}
}
