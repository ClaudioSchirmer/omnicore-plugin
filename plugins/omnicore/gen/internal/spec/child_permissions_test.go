package spec

import (
	"strings"
	"testing"
)

// The reported shape: an RBAC entity whose collection edge is a DIFFERENT job
// from editing the root.
//
// Renaming a role and changing what the role confers are two privileges, and
// giving them one permission is what lets the holder of the smaller one help
// themselves to the larger. Before children[].permissions the spec could not
// say it: the per-entry routes took the root's update permission and there was
// no key to override it, while declaring an unused `update` beside `patch` was
// correctly refused as a permission for an operation nobody serves.

// A collection that gates its own verbs is the point of the key, so it has to
// validate — and the values reach the routes, which is web.go's test.
func TestChildPermissionsAreAcceptedPerVerb(t *testing.T) {
	ps := childOpsProblems(t, "per-child",
		"    operations: [add, remove]\n"+
			"    permissions: {add: \"papel:conceder\", remove: \"papel:conceder\"}\n")
	if ps.HasBlockers() {
		t.Fatalf("a collection gating its own verbs is refused:\n%v", ps.Error())
	}
}

// Partial is legal and is the common case: gate the verb that widens privilege,
// leave the rest inheriting. A map that had to be complete would force an
// author to restate the inherited value, and a restated default is a default
// that silently stops tracking the thing it copied.
func TestChildPermissionsMayGateOneVerbAndInheritTheRest(t *testing.T) {
	ps := childOpsProblems(t, "per-child",
		"    operations: [add, remove]\n"+
			"    permissions: {add: \"papel:conceder\"}\n")
	if ps.HasBlockers() {
		t.Fatalf("a partial map is refused:\n%v", ps.Error())
	}
}

// Every mistake this key can carry is silent in the generated code: a
// permission on a verb nobody mounts reads, in the spec, as a route being
// guarded, and so does a misspelled verb.
func TestChildPermissionRefusals(t *testing.T) {
	for _, tc := range []struct {
		name     string
		strategy string
		block    string
		want     string
	}{
		{
			"a verb that does not exist",
			"per-child",
			"    permissions: {upsert: \"papel:conceder\"}\n",
			"not a per-entry verb",
		},
		{
			"a verb the collection does not mount",
			"per-child",
			"    operations: [remove]\n    permissions: {add: \"papel:conceder\"}\n",
			"that verb is not mounted",
		},
		{
			"a collection with no per-entry verbs at all",
			"atomic-replace",
			"    permissions: {add: \"papel:conceder\"}\n",
			"permissions gates the PER-ENTRY verbs",
		},
		{
			"an empty map",
			"per-child",
			"    permissions: {}\n",
			"the map is empty",
		},
		{
			"an empty permission",
			"per-child",
			"    permissions: {add: \"\"}\n",
			"the permission is empty",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := childOpsProblems(t, tc.strategy, tc.block)
			if got := blockerSaying(ps, tc.want); got == "" {
				t.Fatalf("accepted without saying %q:\n%v", tc.want, ps.Error())
			}
		})
	}
}

// The same warning the root's permissions get, for the same reason: a claim
// outside the resource's namespace is legitimate, and is also what a permission
// borrowed from another entity looks like. On this key the borrowed claim is
// the dangerous direction — it grants the OTHER entity's edge.
func TestChildPermissionOutsideTheResourceNamespaceIsWarned(t *testing.T) {
	ps := childOpsProblems(t, "per-child",
		"    operations: [add, remove]\n"+
			"    permissions: {add: \"grupo:conceder\"}\n")
	if ps.HasBlockers() {
		t.Fatalf("a short claim is refused rather than questioned:\n%v", ps.Error())
	}
	if got := warningSaying(ps, "is not namespaced by the declared resource"); got == "" {
		t.Fatalf("a permission from another resource passes unmentioned:\n%v", ps.Error())
	}
}

// A collection on an entity that serves no write of its own. The per-entry
// routes are mounted from the COLLECTION, not from the root's modes, so they
// exist — and what they inherit does not. Before this check the emitter wrote
// RequirePermission(""), a requirement no claim answers: the routes fail closed
// at runtime and generation said nothing.
const childPermNoRootWriteSpec = `
specVersion: 1
entity: Turma
plural: Turmas
language: pt-BR
storage:
  kind: flat
  table: turmas
  description: Turmas.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Codigo, type: string, column: codigo, length: 20, livesOn: root, example: "3B", description: O código.}
modes: [display, archive, unarchive]
delete: {root: soft}
children:
  - name: Aula
    plural: Aulas
    table: turma_aulas
    parentColumn: turma_id
    description: As aulas.
    ownedBy: root
    editStrategy: per-child
    operations: [add]
    businessIdentity: [DiaSemana]
    softRemove: true
    archivedAt: deleted_at
%s
    fields:
      - {name: DiaSemana, type: string, column: dia_semana, length: 15, example: segunda, description: O dia.}
read:
  backing: relational
  view: {name: turmas}
  byId: true
surfaces: {rest: true}
authz:
  resource: turma
  dataAccess: anyone-with-permission
  permissions: {archive: "turma:arquivar", unarchive: "turma:arquivar", read: "turma:ler"}
`

func childPermNoRootWriteProblems(t *testing.T, block string) *Problems {
	t.Helper()
	src := strings.Replace(childPermNoRootWriteSpec, "%s\n", block, 1)
	s, err := Parse([]byte(src), "turma.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	return Validate(s, Options{})
}

func TestPerEntryVerbWithNothingToInheritIsRefused(t *testing.T) {
	ps := childPermNoRootWriteProblems(t, "")
	got := blockerSaying(ps, "no update, patch or insert to inherit from")
	if got == "" {
		t.Fatalf("a route whose permission is the empty string was accepted:\n%v", ps.Error())
	}
	if !strings.Contains(got, "permissions:") {
		t.Errorf("the refusal does not name the key that answers it: %s", got)
	}
}

// And the key IS the answer, not just the diagnosis: the collection can be
// gated even when there is no root write to borrow from.
func TestDeclaringThePermissionAnswersTheMissingInheritance(t *testing.T) {
	ps := childPermNoRootWriteProblems(t, "    permissions: {add: \"turma:escrever\"}\n")
	if ps.HasBlockers() {
		t.Fatalf("declaring the permission does not clear the refusal:\n%v", ps.Error())
	}
}
