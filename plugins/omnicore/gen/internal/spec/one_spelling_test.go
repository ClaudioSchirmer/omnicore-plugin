package spec

import (
	"strings"
	"testing"
)

// The acceptance the work order names: ONE spec, ONE spelling, across every key
// that addresses a collection — and it passes whichever of the two names the
// author picked.
//
// That is the whole defect stated as a test. Before, `joins[].inChild`,
// `rules.list[].fields` and `read.computed.from` took the entry type and
// `service.facts[].filters` took the collection name, so no consistent spelling
// existed: a spec correct by any one key's rule was refused by another, and the
// facts' refusal argued the opposite of the other three. A per-key test cannot
// catch that — each one passes on its own. Only a spec using all four at once can.
const oneSpellingSpec = `
specVersion: 1
entity: PapelOS
plural: PapeisOS
language: pt-BR
storage:
  kind: flat
  table: papeis_os
  description: Papéis.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Admin, description: O nome.}
children:
  - name: PapelPermissaoOS
    plural: PermissoesOS
    table: papel_os_permissoes
    parentColumn: papel_os_id
    description: As permissões.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [PermissaoID]
    fields:
      - {name: PermissaoID, type: id, column: permissao_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: A permissão.}
joins:
  # KEY 1 — joins[].inChild
  - to: RecursoOS
    kind: inner
    on: permissao_id
    inChild: COLLECTION
    fields:
      - {name: RecursoNome, type: string, column: recurso_nome, example: tenant, description: O recurso.}
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
rules:
  list:
    # KEY 2 — rules.list[].fields on a childDuplicate
    - id: permissao-nao-se-repete
      kind: childDuplicate
      scope: [insertOrUpdate]
      fields: [COLLECTION]
      notification: PermissaoDuplicadaOSNotification
notifications:
  - name: PermissaoDuplicadaOSNotification
    semantic: conflict
    description: Duas entradas para a mesma permissão.
    text: {ptbr: Duplicada., eng: Duplicate., esp: Duplicado., fra: Duplique., deu: Duplikat., ita: Duplicato., nld: Duplicaat.}
service:
  required: true
  facts:
    # KEY 4 — service.facts[].filters, per entry
    - name: PermissaoIndisponivel
      kind: manual
      returns: bool
      filters: [COLLECTION.PermissaoID]
      description: Se a permissão não existe ou foi arquivada.
read:
  backing: relational
  view: {name: papeis_os}
  # KEY 3 — read.computed.from, reaching a collection: refused at the root and
  # sent to children[].computed, which is where the same source is in scope.
  computed:
    - {name: Resumo, type: string, from: [Nome], example: x, description: Um resumo.}
  byId: true
surfaces: {rest: true}
authz:
  resource: papelos
  dataAccess: anyone-with-permission
  permissions: {insert: "papelos:escrever", update: "papelos:escrever", patch: "papelos:escrever", archive: "papelos:arquivar", read: "papelos:ler"}
`

func TestOneSpellingPassesEveryKeyThatNamesACollection(t *testing.T) {
	for _, spelling := range []string{"PapelPermissaoOS", "PermissoesOS"} {
		t.Run(spelling, func(t *testing.T) {
			src := strings.ReplaceAll(oneSpellingSpec, "COLLECTION", spelling)
			s, err := Parse([]byte(src), "papel.omnicore.yaml")
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}
			// Neighbours are absent, so the join's target is unknown and its fields
			// carry their own declared types — a warning, never a blocker.
			if ps := Validate(s, Options{}); ps.HasBlockers() {
				t.Fatalf("one spec spelled %q consistently is still refused:\n%v", spelling, ps.Error())
			}
		})
	}
}
