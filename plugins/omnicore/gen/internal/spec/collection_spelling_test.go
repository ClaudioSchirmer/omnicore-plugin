package spec

import (
	"strings"
	"testing"
)

// One collection, two names, and every key that addresses one takes both.
//
// The defect this pins was not that a spelling was wrong — it was that the
// language had no single answer. joins[].inChild, rules.list[].fields and
// read.computed.from resolved the entry type's `name`; service.facts[].filters
// resolved the collection's `plural`, and its refusal argued that plural was the
// name everything already used. Nothing in `explain` said so, so the only way to
// learn which key wanted which word was to be refused by it.
const twoSpellingsSpec = `
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
notifications:
  - name: PermissaoDuplicadaNotification
    semantic: conflict
    text: {ptbr: Duplicada., eng: Duplicate., esp: Duplicado., fra: Duplique., deu: Duplikat., ita: Duplicato., nld: Duplicaat.}
rules:
  list:
    - id: permissao-duplicada
      kind: childDuplicate
      scope: [insertOrUpdate]
      fields: [COLLECTION]
      notification: PermissaoDuplicadaNotification
      description: Uma permissão não se repete.
read:
  backing: relational
  view: {name: papeis}
  byId: true
surfaces: {rest: true}
authz:
  resource: papel
  dataAccess: anyone-with-permission
  permissions: {insert: "papel:escrever", update: "papel:escrever", patch: "papel:escrever", archive: "papel:arquivar", read: "papel:ler"}
`

func spellingProblems(t *testing.T, collection string) *Problems {
	t.Helper()
	src := strings.Replace(twoSpellingsSpec, "COLLECTION", collection, 1)
	s, err := Parse([]byte(src), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	return Validate(s, Options{})
}

// A derived field may not take the collection's own name: the read shape already
// declares a Go field under it. The ENTRY TYPE is a different word and collides
// with nothing, so it must still be accepted — a refusal there would have no
// defect behind it.
func TestAComputedFieldMayNotTakeTheCollectionName(t *testing.T) {
	base := strings.Replace(twoSpellingsSpec, "COLLECTION", "Permissoes", 1)
	base = strings.Replace(base, "read:\n  backing: relational", `read:
  computed:
    - {name: COMPUTED, type: string, from: [Nome], example: x, description: Um rótulo.}
  backing: relational`, 1)

	for _, tc := range []struct {
		name    string
		refused bool
		why     string
	}{
		{"Permissoes", true, "it is the collection's field on the read shape"},
		{"PapelPermissao", false, "the entry type is <Name>RowResult and collides with nothing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Parse([]byte(strings.Replace(base, "COMPUTED", tc.name, 1)),
				"papel.omnicore.yaml")
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}
			ps := Validate(s, Options{})
			got := blockerSaying(ps, "read shape already declares a field") != ""
			if got != tc.refused {
				t.Errorf("refused=%v, want %v — %s\n%v", got, tc.refused, tc.why, ps.Error())
			}
		})
	}
}

func TestCollectionRuleTakesEitherSpelling(t *testing.T) {
	for _, spelling := range []string{"PapelPermissao", "Permissoes"} {
		if ps := spellingProblems(t, spelling); ps.HasBlockers() {
			t.Errorf("rules.list[].fields refuses %q:\n%v", spelling, ps.Error())
		}
	}
}

// The overlap that would make "both spellings" ambiguous: one word naming two
// different collections. It is refused outright, which is what lets every other
// key take either name without asking which the author meant.
func TestOneWordMayNotNameTwoCollections(t *testing.T) {
	src := strings.Replace(twoSpellingsSpec, "COLLECTION", "Permissoes", 1)
	src = strings.Replace(src, "  - name: PapelPermissao", `  - name: Permissoes
    plural: PermissoesDoPapel
    table: papel_permissoes_2
    parentColumn: papel_id
    description: Outra coleção.
    ownedBy: root
    editStrategy: atomic-replace
    fields:
      - {name: Outro, type: string, column: outro, length: 10, example: x, description: Outro.}
  - name: PapelPermissao`, 1)
	s, err := Parse([]byte(src), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	ps := Validate(s, Options{})
	if blockerSaying(ps, "names two different collections") == "" {
		t.Fatalf("a word naming two collections is accepted:\n%v", ps.Error())
	}
}
