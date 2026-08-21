package spec

import (
	"strings"
	"testing"
)

// A fact whose question is about one ENTRY of a collection. Four spellings were
// tried in the field before the author gave up and declared the method by hand,
// and NONE of them said anything: `check` was green, `generate` succeeded, the
// tree compiled, and the port carried a method with no parameter — uncallable,
// discovered three steps later while writing the rule that needed it.
//
// So each test here is about a spelling, and what it must now say.
const perEntryFactSpec = `
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
modes: [display, insert, update, archive]
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
service:
  required: true
  facts:
    - name: PermissaoIndisponivel
      kind: %s
      %s
      filters: [%s]
      description: Se a permissão não existe ou foi arquivada. Vive em outra tabela.
read:
  backing: relational
  view: {name: papeis, version: 1}
  byId: true
surfaces: {rest: true}
authz:
  resource: papel
  dataAccess: anyone-with-permission
  permissions: {insert: "papel:escrever", update: "papel:escrever", patch: "papel:escrever", archive: "papel:arquivar", read: "papel:ler"}
`

// perEntryProblems validates one spelling of one fact and returns what was said.
func perEntryProblems(t *testing.T, kind, returns, filters string) *Problems {
	t.Helper()
	src := perEntryFactSpec
	src = strings.Replace(src, "kind: %s", "kind: "+kind, 1)
	src = strings.Replace(src, "      %s\n", returns, 1)
	src = strings.Replace(src, "filters: [%s]", "filters: ["+filters+"]", 1)
	s, err := Parse([]byte(src), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	return Validate(s, Options{})
}

// blockerSaying returns the first blocker whose text mentions want, so a test
// asserts on the SENTENCE the author reads rather than on a position in a list.
// (blockerAbout, in facts_test.go, answers the yes/no half of the same question.)
func blockerSaying(ps *Problems, want string) string {
	for _, p := range ps.Blockers() {
		if strings.Contains(p.Where+p.Message+p.Fix, want) {
			return p.String()
		}
	}
	return ""
}

func TestPerEntryFilterIsAccepted(t *testing.T) {
	ps := perEntryProblems(t, "manual", "      returns: bool\n", "Permissoes.PermissaoID")
	if ps.HasBlockers() {
		t.Fatalf("the per-entry form is refused:\n%v", ps.Error())
	}
}

// The spelling that cost the most: a bare collection field. It resolved against
// nothing and was DROPPED, so the method arrived with no parameter at all.
func TestBareCollectionFieldNamesTheCollection(t *testing.T) {
	ps := perEntryProblems(t, "manual", "      returns: bool\n", "PermissaoID")
	got := blockerSaying(ps, "PermissaoID")
	if got == "" {
		t.Fatalf("a bare collection field is still accepted in silence:\n%v", ps.Error())
	}
	if !strings.Contains(got, "Permissoes.PermissaoID") {
		t.Errorf("the refusal does not offer the per-entry spelling: %s", got)
	}
}

// The second spelling tried: the entry's Go type instead of the collection.
func TestEntryTypeNameIsCorrectedToThePlural(t *testing.T) {
	ps := perEntryProblems(t, "manual", "      returns: bool\n", "PapelPermissao.PermissaoID")
	got := blockerSaying(ps, "PapelPermissao")
	if got == "" {
		t.Fatalf("the entry's type name is accepted as a collection:\n%v", ps.Error())
	}
	if !strings.Contains(got, "Permissoes.PermissaoID") {
		t.Errorf("the refusal does not name the collection: %s", got)
	}
}

func TestUnknownCollectionFieldListsTheOnesThereAre(t *testing.T) {
	ps := perEntryProblems(t, "manual", "      returns: bool\n", "Permissoes.NaoExiste")
	got := blockerSaying(ps, "NaoExiste")
	if got == "" {
		t.Fatalf("an unknown field of a real collection is accepted:\n%v", ps.Error())
	}
	if !strings.Contains(got, "PermissaoID") {
		t.Errorf("the refusal does not list the fields there are: %s", got)
	}
}

// A COMPUTED fact is a query this generator writes, against this entity's own
// table. The collection's column is on another one, so there is nothing to emit
// — and a join here would be a query shape nothing else in the language can
// express or index.
func TestPerEntryFilterIsRefusedOnAComputedFact(t *testing.T) {
	ps := perEntryProblems(t, "exists", "", "Permissoes.PermissaoID")
	got := blockerSaying(ps, "Permissoes.PermissaoID")
	if got == "" {
		t.Fatalf("a computed fact accepted a filter it cannot query:\n%v", ps.Error())
	}
	if !strings.Contains(got, "manual") {
		t.Errorf("the refusal does not point at the kind that CAN ask it: %s", got)
	}
}

// Two filters that camel-case to one parameter emit a signature that does not
// compile — generated code the author did not write and cannot fix.
func TestTwoFiltersCannotShareOneParameterName(t *testing.T) {
	ps := perEntryProblems(t, "manual", "      returns: bool\n", "DonoID, DonoID")
	if blockerSaying(ps, "parameter") == "" {
		t.Errorf("a duplicated filter is accepted:\n%v", ps.Error())
	}
}
