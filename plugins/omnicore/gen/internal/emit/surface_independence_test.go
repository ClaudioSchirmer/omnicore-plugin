package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The reported shape, from a real RBAC service: every collection verb was an
// HTTP endpoint and none of them was in the schema. A GraphQL-only consumer
// could create a role and never grant it a permission, create a user and never
// place them in a group — the write side of the aggregate, silently halved.
//
// The three surfaces are independent now, and whatever the entity mounts reaches
// every one of them that is on. These tests hold both halves.
const surfaceSpec = `
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
    permissions: {add: "papel:conceder"}
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

func surfaceModel(t *testing.T, child, surfaces string) *ir.Model {
	t.Helper()
	src := strings.NewReplacer(
		"%CHILD%\n", child,
		"%SURFACES%", surfaces,
	).Replace(surfaceSpec)
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

const bothSurfaces = "surfaces:\n  rest: true\n  graphql: {enabled: true}\n"

// The fix itself: the per-entry verbs are mutations, with the same command, the
// same handler and the same per-verb permission the REST route uses.
func TestPerEntryVerbsReachGraphQL(t *testing.T) {
	routes := fileNamed(t, surfaceModel(t, "", bothSurfaces), "internal/web/papel_routes.go")
	gql := routes[strings.Index(routes, "func MountPapeisGraphQL("):]

	for _, want := range []string{
		`fwgraphql.MutationWithBodyID[requests.AddPapelPermissaoRequest](
		"addPapelPermissao", requests.AddPapelPermissaoResponse{}.FromResult,`,
		`fwgraphql.MutationWithBodyID[requests.ChangePapelPermissaoGraphQLRequest](
		"changePapelPermissao", requests.ChangePapelPermissaoResponse{}.FromResult,`,
		`fwgraphql.MutationWithBodyID[requests.ArchivePapelPermissaoGraphQLRequest](
		"archivePapelPermissao", requests.ArchivePapelPermissaoGraphQLResponse{}.FromResult,`,
	} {
		if !strings.Contains(gql, want) {
			t.Errorf("the collection verb is missing from the schema:\n%s\nwanted:\n%s", gql, want)
		}
	}

	// The per-verb permission travels, exactly as it does on REST: `add` was
	// gated on its own here, the other two inherit the root's update.
	if !strings.Contains(gql, `"addPapelPermissao"`) ||
		!strings.Contains(gql[strings.Index(gql, `"addPapelPermissao"`):], `RequirePermission("papel:conceder")`) {
		t.Errorf("the add mutation lost the collection's own permission:\n%s", gql)
	}
	if !strings.Contains(gql[strings.Index(gql, `"archivePapelPermissao"`):], `RequirePermission("papel:escrever")`) {
		t.Errorf("the remove mutation did not inherit the root's update permission:\n%s", gql)
	}
}

// The one real difference between the surfaces, and the reason the GraphQL
// requests exist at all: the framework's input decoder skips a path-tagged
// field, so a Request that names the entry through the path would reach the
// command with an empty id and replace nothing.
func TestGraphQLPerEntryRequestsCarryTheEntryIDInTheInput(t *testing.T) {
	m := surfaceModel(t, "", bothSurfaces)
	change := fileNamed(t, m, "internal/web/requests/change_papel_permissao.go")
	archive := fileNamed(t, m, "internal/web/requests/archive_papel_permissao.go")
	got := change + archive

	// REST keeps the path segment.
	if !strings.Contains(got, "PapelPermissaoID string `path:\"papelPermissaoId\"`") {
		t.Errorf("the REST request lost its path id:\n%s", got)
	}
	// GraphQL carries the same name as a body field.
	for _, want := range []string{
		"type ChangePapelPermissaoGraphQLRequest struct",
		"type ArchivePapelPermissaoGraphQLRequest struct",
		"PapelPermissaoID string `json:\"papelPermissaoId\"`",
		"type ArchivePapelPermissaoGraphQLResponse struct",
		"return ArchivePapelPermissaoGraphQLResponse{Success: true}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the GraphQL wire pair is incomplete, missing %q:\n%s", want, got)
		}
	}
	// The add verb needs no second shape: its request carries no path segment.
	if strings.Contains(got, "type AddPapelPermissaoGraphQLRequest struct") {
		t.Errorf("a GraphQL request was written for a verb that did not need one:\n%s", got)
	}
}

// A GraphQL-only service: no HTTP route at all, and the collection's verbs
// answer anyway. This is the case that used to lose them entirely — mounted
// inside the REST branch, generated, and unreachable.
func TestGraphQLOnlyKeepsTheCollectionVerbs(t *testing.T) {
	routes := fileNamed(t, surfaceModel(t, "", "surfaces:\n  graphql: {enabled: true}\n"),
		"internal/web/papel_routes.go")

	if strings.Contains(routes, "app.Group(") || strings.Contains(routes, "fwopenapi.Mount(") {
		t.Errorf("surfaces.rest is false and HTTP routes were mounted anyway:\n%s", routes)
	}
	for _, want := range []string{"addPapelPermissao", "changePapelPermissao", "archivePapelPermissao"} {
		if !strings.Contains(routes, `"`+want+`"`) {
			t.Errorf("%s did not survive a GraphQL-only surface:\n%s", want, routes)
		}
	}
}

// The exports are their own surface: they need a listing, not a CRUD API beside
// them. A spec that asks for the spreadsheet alone gets the two download paths
// and nothing else — including the view name the export handler reads through,
// which used to be declared only because a REST read route needed it first.
func TestExportsStandWithoutREST(t *testing.T) {
	routes := fileNamed(t, surfaceModel(t, "    surfaces: {graphql: {enabled: true}}\n",
		"surfaces:\n  graphql: {enabled: true}\n  exports: {csv: {delimiter: \",\"}, xlsx: {sheet: Papeis}}\n"),
		"internal/web/papel_routes.go")

	for _, want := range []string{
		"viewName := view.Name()",
		`fiber.MethodGet, "/papeis.csv"`,
		`fiber.MethodGet, "/papeis.xlsx"`,
	} {
		if !strings.Contains(routes, want) {
			t.Errorf("the export surface is incomplete without REST, missing %q:\n%s", want, routes)
		}
	}
	if strings.Contains(routes, "app.Group(") {
		t.Errorf("no REST surface was asked for and the group was mounted:\n%s", routes)
	}
}

// Absent narrows nothing. An entity that enables the surface and says no more
// gets every read it serves and every write verb it mounts — which is what
// makes the hollow surface unreachable by accident rather than by validation.
func TestGraphQLWithNoNarrowingExposesEverything(t *testing.T) {
	routes := fileNamed(t, surfaceModel(t, "", bothSurfaces), "internal/web/papel_routes.go")
	gql := routes[strings.Index(routes, "func MountPapeisGraphQL("):]
	for _, want := range []string{
		`"papeis", "Papel"`,   // the connection
		`"papel", "Papel"`,    // the singular read
		`"createPapel"`,       // insert
		`"updatePapel"`,       // PUT
		`"patchPapel"`,        // PATCH
		`"archivePapel"`,      // archive (unarchive is not among this fixture's modes)
		`"addPapelPermissao"`, // the collection
	} {
		if !strings.Contains(gql, want) {
			t.Errorf("an enabled surface is missing %s:\n%s", want, gql)
		}
	}
}

// The narrowing keys still narrow, on both sides, and they narrow ONE surface:
// a verb off the schema keeps its REST route.
func TestNarrowingTakesOneVerbOffOneSurface(t *testing.T) {
	m := surfaceModel(t,
		"    surfaces: {graphql: {mutations: [add]}}\n",
		"surfaces:\n  rest: true\n  graphql: {enabled: true, mutations: [insert]}\n")
	routes := fileNamed(t, m, "internal/web/papel_routes.go")
	gql := routes[strings.Index(routes, "func MountPapeisGraphQL("):]

	if !strings.Contains(gql, `"createPapel"`) {
		t.Errorf("the listed mutation is missing:\n%s", gql)
	}
	for _, unwanted := range []string{`"patchPapel"`, `"archivePapel"`, `"changePapelPermissao"`, `"archivePapelPermissao"`} {
		if strings.Contains(gql, unwanted) {
			t.Errorf("%s was exposed even though the spec narrowed it away:\n%s", unwanted, gql)
		}
	}
	if !strings.Contains(gql, `"addPapelPermissao"`) {
		t.Errorf("the collection verb the spec DID list is missing:\n%s", gql)
	}
	// The REST side is untouched: narrowing one surface is not dropping a verb.
	if !strings.Contains(routes, `fiber.MethodPut, "/:id/permissoes/:papelPermissaoId"`) {
		t.Errorf("narrowing the schema took the REST route away too:\n%s", routes)
	}
	// And nothing writes a wire pair for a verb that is not on the schema.
	reqs := fileNamed(t, m, "internal/web/requests/change_papel_permissao.go") +
		fileNamed(t, m, "internal/web/requests/archive_papel_permissao.go")
	if strings.Contains(reqs, "GraphQLRequest struct") {
		t.Errorf("a GraphQL-shaped request was written for a collection that has none:\n%s", reqs)
	}
}

// A collection taken off REST keeps everything else it had: the commands, the
// domain methods, the wire types — and its schema seat.
func TestACollectionOffRESTKeepsItsSchemaSeat(t *testing.T) {
	m := surfaceModel(t, "    surfaces: {rest: false}\n", bothSurfaces)
	routes := fileNamed(t, m, "internal/web/papel_routes.go")

	if strings.Contains(routes, `"/:id/permissoes"`) {
		t.Errorf("the collection was taken off REST and its routes were mounted:\n%s", routes)
	}
	if !strings.Contains(routes, `"addPapelPermissao"`) {
		t.Errorf("the collection lost the surface it was NOT taken off:\n%s", routes)
	}
	// The root's own routes are untouched — this key is per collection.
	if !strings.Contains(routes, `fiber.MethodPost, "/"`) {
		t.Errorf("taking one collection off REST took the entity with it:\n%s", routes)
	}
	cmds := fileNamed(t, m, "internal/application/commands/add_papel_permissao_command.go")
	if !strings.Contains(cmds, "type AddPapelPermissaoCommand struct") {
		t.Errorf("a surface decision deleted the command behind it:\n%s", cmds)
	}
}
