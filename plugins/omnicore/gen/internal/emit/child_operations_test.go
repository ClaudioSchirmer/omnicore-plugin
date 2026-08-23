package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A collection whose ONLY field is its business identity — a grant that holds a
// catalog id and nothing else.
//
// There is nothing about such an entry that can change and still leave it the
// same entry, so the change verb can only turn entry A into entry B while
// keeping A's row id. children[].operations is what lets the spec say so; the
// two ways out before it were atomic-replace, which is a different contract (an
// omitted entry is revoked), and inventing a mutable field the model does not
// have.
const childOpsSpec = `
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
    editStrategy: per-child
    businessIdentity: [PermissaoID]
    softRemove: true
    archivedAt: deleted_at
%s
    fields:
      - {name: PermissaoID, type: id, column: permissao_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: A permissão.}
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

func childOpsModel(t *testing.T, operations string) *ir.Model {
	t.Helper()
	src := strings.Replace(childOpsSpec, "%s\n", operations, 1)
	s, err := spec.Parse([]byte(src), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("the fixture does not validate:\n%v", ps.Error())
	}
	m, err := ir.Resolve(s, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	return m
}

// A verb the spec left out leaves NO trace: not a route, not a command, not a
// wire type, not a domain method, not a generated test.
//
// Each of those is a separate way for the verb to come back. A leftover
// ChangeXByID on the aggregate is the worst of them — it compiles, it is
// callable, and the next hand-written route mounts exactly the verb the spec
// decided against.
func TestDroppedChildVerbLeavesNoTrace(t *testing.T) {
	m := childOpsModel(t, "    operations: [add, remove]\n")
	for _, f := range emitAll(t, m) {
		if strings.Contains(string(f.Content), "ChangePapelPermissao") {
			t.Errorf("%s still carries the change verb the spec did not mount:\n%s",
				f.Path, string(f.Content))
		}
	}

	routes := fileNamed(t, m, "internal/web/papel_routes.go")
	if !strings.Contains(routes, `fiber.MethodPost, "/:id/permissoes"`) {
		t.Errorf("the add verb is not mounted:\n%s", routes)
	}
	if !strings.Contains(routes, `fiber.MethodPatch, "/:id/permissoes/:papelPermissaoId/archive"`) {
		t.Errorf("the remove verb is not mounted as an archive:\n%s", routes)
	}
	if strings.Contains(routes, `fiber.MethodPut, "/:id/permissoes`) {
		t.Errorf("the change verb is mounted anyway:\n%s", routes)
	}

	dom := fileNamed(t, m, "internal/domain/papel.go")
	if !strings.Contains(dom, "func (e *Papel) RemovePapelPermissaoByID(") {
		t.Errorf("the remove verb lost its domain method:\n%s", dom)
	}
	if !strings.Contains(dom, "func (e *Papel) AddPapelPermissao(") {
		t.Errorf("the add verb lost its domain method:\n%s", dom)
	}
}

// The default is all three, and it has to stay that way: `operations` is a
// subtraction, and a spec written before the key existed must keep generating
// the surface it already generated. Silently mounting fewer verbs on a
// regeneration takes routes away from a running service.
func TestPerChildDefaultsToTheWholeTrio(t *testing.T) {
	m := childOpsModel(t, "")
	cmds := fileNamed(t, m, "internal/application/commands/papel_permissao_commands.go")
	for _, verb := range []string{"AddPapelPermissaoCommand", "ChangePapelPermissaoCommand", "RemovePapelPermissaoCommand"} {
		if !strings.Contains(cmds, "type "+verb+" struct") {
			t.Errorf("the default surface is missing %s:\n%s", verb, cmds)
		}
	}
}

// The projector a per-entry verb answers with is written only when a verb
// answers with the entry. Removal answers with the owner alone, so a
// remove-only collection would otherwise carry a function nothing calls.
func TestRemoveOnlyCollectionWritesNoEntryProjector(t *testing.T) {
	m := childOpsModel(t, "    operations: [remove]\n")
	cmds := fileNamed(t, m, "internal/application/commands/papel_permissao_commands.go")
	if strings.Contains(cmds, "func projectOnePapelPermissao(") {
		t.Errorf("nothing projects an entry here, yet the projector was written:\n%s", cmds)
	}
	if !strings.Contains(cmds, "type RemovePapelPermissaoCommand struct") {
		t.Errorf("the one mounted verb is missing:\n%s", cmds)
	}
}
