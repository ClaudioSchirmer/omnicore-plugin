package spec

import (
	"strings"
	"testing"
)

// The reported shape: a spec could ask for REST, for GraphQL and for exports,
// and only the first two were independent — the exports were mounted inside the
// REST branch, and a collection's per-entry verbs were mounted there and NOWHERE
// else. An entity with children on a GraphQL-only service published a schema
// that could create a role and never grant it a permission.
//
// These tests hold the three switches apart, and hold the collection's own seat
// to the one rule that makes it safe: it may take a verb OFF a surface, never
// put one somewhere the entity does not serve.
const surfacesSpec = `
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
    editStrategy: %STRATEGY%
    businessIdentity: [PermissaoID]
    softRemove: true
    archivedAt: deleted_at
%CHILD%
    fields:
      - {name: PermissaoID, type: id, column: permissao_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: A permissão.}
      - {name: Nivel, type: string, column: nivel, length: 20, example: total, description: O nível.}
read:
  backing: relational
  view: {name: papeis}
  byId: true
  byParams:
    filters: [{field: Nome, ops: [eq]}]
%SURFACES%
authz:
  resource: papel
  dataAccess: anyone-with-permission
  permissions: {insert: "papel:escrever", update: "papel:escrever", patch: "papel:escrever", archive: "papel:arquivar", read: "papel:ler"}
`

func surfaceProblems(t *testing.T, strategy, child, surfaces string) *Problems {
	t.Helper()
	src := strings.NewReplacer(
		"%STRATEGY%", strategy,
		"%CHILD%\n", child,
		"%SURFACES%", surfaces,
	).Replace(surfacesSpec)
	s, err := Parse([]byte(src), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v\n%s", err, src)
	}
	ps := Validate(s, Options{})
	for _, p := range CheckCoverage(s).Blockers() {
		ps.BlockerFix(p.Where, p.Message, p.Fix)
	}
	return ps
}

// Each switch on its own is a complete answer. The exports one is the addition:
// a project that wants the spreadsheet and no CRUD API used to have to declare
// surfaces.rest: true and then publish endpoints it never asked for.
func TestEachSurfaceStandsAlone(t *testing.T) {
	for name, tc := range map[string]struct{ strategy, block string }{
		"rest only":    {"per-child", "surfaces: {rest: true}\n"},
		"graphql only": {"per-child", "surfaces:\n  graphql: {enabled: true}\n"},
		// atomic-replace, and the reason is the next test: an exports-only
		// entity serves no verb at all, so a collection with per-entry verbs on
		// it would have nowhere to answer — which is refused, on purpose.
		"exports only": {"atomic-replace", "surfaces:\n  exports: {csv: {delimiter: \",\"}}\n"},
		"all three": {"per-child", "surfaces:\n  rest: true\n  graphql: {enabled: true}\n" +
			"  exports: {csv: {delimiter: \",\"}, xlsx: {sheet: Papeis}}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			ps := surfaceProblems(t, tc.strategy, "", tc.block)
			if ps.HasBlockers() {
				t.Fatalf("%s is refused:\n%v", name, ps.Error())
			}
		})
	}
}

// The collection's verbs need a verb-carrying surface, and the exports are not
// one: they render the listing. An entity that serves nothing else cannot hold a
// per-child collection, and saying so here is better than generating three
// commands and no way to reach them.
func TestPerChildOnAnExportsOnlyEntityIsRefused(t *testing.T) {
	ps := surfaceProblems(t, "per-child", "",
		"surfaces:\n  exports: {csv: {delimiter: \",\"}}\n")
	if got := blockerSaying(ps, "reaches no surface"); got == "" {
		t.Fatalf("a collection with nowhere to answer is accepted:\n%v", ps.Error())
	}
}

// The one combination that is not a surface at all.
func TestNoSurfaceAtAllIsRefused(t *testing.T) {
	ps := surfaceProblems(t, "per-child", "", "surfaces: {rest: false}\n")
	if got := blockerSaying(ps, "exposes no surface"); got == "" {
		t.Fatalf("a spec exposing nothing is accepted:\n%v", ps.Error())
	}
}

// An enabled surface that carries nothing is the hole the new default closes on
// one side and this blocker closes on the other: `mutations: []` narrows every
// write verb off, and on an entity with no read there is nothing left to publish.
func TestAGraphQLSurfaceThatExposesNothingIsRefused(t *testing.T) {
	src := strings.NewReplacer(
		"%STRATEGY%", "atomic-replace",
		"%CHILD%\n", "",
		"%SURFACES%", "surfaces:\n  graphql: {enabled: true, mutations: []}\n",
		"  byId: true\n", "  byId: false\n",
		"modes: [display, insert, update, archive]", "modes: [insert, update]",
	).Replace(surfacesSpec)
	src = strings.Replace(src, "  byParams:\n    filters: [{field: Nome, ops: [eq]}]\n", "", 1)
	s, err := Parse([]byte(src), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	ps := Validate(s, Options{})
	if got := blockerSaying(ps, "exposes nothing"); got == "" {
		t.Fatalf("a hollow GraphQL surface is accepted:\n%v", ps.Error())
	}
}

// The collection's seat: it narrows, and every absent key follows the entity.
func TestChildSurfacesNarrow(t *testing.T) {
	for name, child := range map[string]string{
		"one verb off the schema": "    operations: [add, change, remove]\n" +
			"    surfaces: {graphql: {mutations: [add, remove]}}\n",
		"the whole collection off the schema": "    operations: [add, remove]\n" +
			"    surfaces: {graphql: {enabled: false}}\n",
		"the whole collection off REST": "    operations: [add, remove]\n" +
			"    surfaces: {rest: false}\n",
	} {
		t.Run(name, func(t *testing.T) {
			ps := surfaceProblems(t, "per-child", child,
				"surfaces:\n  rest: true\n  graphql: {enabled: true}\n")
			if ps.HasBlockers() {
				t.Fatalf("a legitimate narrowing is refused:\n%v", ps.Error())
			}
		})
	}
}

// A verb that reaches no surface at all is generated code nobody can call —
// which is exactly what every collection verb was on a GraphQL-only service,
// silently, before this key existed.
func TestAVerbOnNoSurfaceIsRefused(t *testing.T) {
	ps := surfaceProblems(t, "per-child",
		"    operations: [add, remove]\n    surfaces: {graphql: {enabled: false}}\n",
		"surfaces:\n  graphql: {enabled: true}\n")
	if got := blockerSaying(ps, "reaches no surface"); got == "" {
		t.Fatalf("a verb with nowhere to answer is accepted:\n%v", ps.Error())
	}
}

// The seat narrows and never widens: a collection cannot reach a surface its
// entity does not serve, because the mount that would carry it is not written.
func TestChildSurfacesCannotWiden(t *testing.T) {
	for name, tc := range map[string]struct{ child, surfaces, want string }{
		"REST past a GraphQL-only entity": {
			"    operations: [add]\n    surfaces: {rest: true}\n",
			"surfaces:\n  graphql: {enabled: true}\n",
			"serves no REST surface",
		},
		"GraphQL past a REST-only entity": {
			"    operations: [add]\n    surfaces: {graphql: {enabled: true}}\n",
			"surfaces: {rest: true}\n",
			"serves no GraphQL surface",
		},
	} {
		t.Run(name, func(t *testing.T) {
			ps := surfaceProblems(t, "per-child", tc.child, tc.surfaces)
			if got := blockerSaying(ps, tc.want); got == "" {
				t.Fatalf("a widening is accepted:\n%v", ps.Error())
			}
		})
	}
}

// The same refusals children[].operations and children[].permissions already
// make, for the same reason: a key that reads as a decision must not be silently
// inert.
func TestChildSurfaceRefusals(t *testing.T) {
	for _, tc := range []struct{ name, strategy, child, want string }{
		{
			"a collection with no per-entry verbs at all",
			"atomic-replace",
			"    surfaces: {rest: true}\n",
			"only a per-child collection",
		},
		{
			"a verb that does not exist",
			"per-child",
			"    surfaces: {graphql: {mutations: [upsert]}}\n",
			"not a per-entry verb",
		},
		{
			"a verb the collection does not mount",
			"per-child",
			"    operations: [remove]\n    surfaces: {graphql: {mutations: [add]}}\n",
			"does not mount it",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := surfaceProblems(t, tc.strategy, tc.child,
				"surfaces:\n  rest: true\n  graphql: {enabled: true}\n")
			if got := blockerSaying(ps, tc.want); got == "" {
				t.Fatalf("the mistake is accepted:\n%v", ps.Error())
			}
		})
	}
}
