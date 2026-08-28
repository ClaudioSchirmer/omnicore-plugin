package report

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// The report is the hand-off, and a surface decision that is not in it is a
// decision nobody reviews. This is the reporting half of the gap a consumer
// service hit: its collection verbs were REST-only, and the spec, the generated
// code and this file all said nothing — so the behaviour got written down, in
// that project's own spec, as though somebody had chosen it.
func surfaceModel(children []ir.Child, su ir.Surfaces) *ir.Model {
	return &ir.Model{
		Entity: ir.Names{
			Pascal: "Role", PluralPascal: "Roles", Camel: "role", PluralCamel: "roles",
			PluralSnake: "roles", Route: "/roles",
		},
		Table:    "roles",
		Children: children,
		Surfaces: su,
		Ops: []ir.Operation{
			{Verb: "insert", Method: "fiber.MethodPost", Path: "/", Write: true, Summary: "Create a role"},
			{Verb: "patch", Method: "fiber.MethodPatch", Path: "/:id", Write: true, Summary: "Update a role"},
		},
	}
}

func grantsCollection() []ir.Child {
	return []ir.Child{{
		Name: "RolePermission", Plural: "Permissions", Segment: "permissions",
		OpBase:   "RolePermission",
		PerChild: true, MountsAdd: true, MountsChange: true, MountsRemove: true,
		ArchivedAt:  "deleted_at",
		Permissions: map[string]string{"add": "role:grant", "change": "role:update", "remove": "role:grant"},
		// What the resolver fills in from the entity's surfaces and the
		// collection's own block: on REST, and on the schema for all three.
		OnREST:       true,
		GQLMutations: map[string]bool{"add": true, "change": true, "remove": true},
	}}
}

// Every generated endpoint appears with the surfaces it answers on — the
// collection's verbs included, which is the row that used to exist nowhere.
func TestSurfaceMatrixListsEveryEndpoint(t *testing.T) {
	out := Render(Input{
		Model: surfaceModel(grantsCollection(), ir.Surfaces{
			REST: true, GraphQL: true,
			GQLMutations: map[string]bool{"insert": true, "update": true},
		}),
		SpecPath: "omnicore-gen/role.omnicore.yaml",
	})

	for _, want := range []string{
		"### Where each endpoint answers",
		"Surfaces enabled: **REST · GraphQL**",
		"| Create a role | `POST /roles` | `createRole` |",
		"| Update a role | `PATCH /roles/:id` | `patchRole` |",
		"| Add one `RolePermission` | `POST /roles/:id/permissions` | `addRolePermission` |",
		"| Replace one `RolePermission` | `PUT /roles/:id/permissions/:rolePermissionId` | `changeRolePermission` |",
		// The removal's method is the child's own declaration: this one archives,
		// so it is a PATCH and not the DELETE a caller would read as permanent.
		"| Take out one `RolePermission` | `PATCH /roles/:id/permissions/:rolePermissionId/archive` | `removeRolePermission` |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the surface matrix is missing %q:\n%s", want, out)
		}
	}
}

// A dash that is a DECISION reads exactly like a dash that is a surface being
// off, and only the first is something to confirm. So the narrowings are named
// under the table, by the key that made them.
func TestSurfaceMatrixNamesEveryNarrowing(t *testing.T) {
	children := grantsCollection()
	children[0].GQLMutations = map[string]bool{"add": true}

	out := Render(Input{
		Model: surfaceModel(children, ir.Surfaces{
			REST: true, GraphQL: true,
			GQLMutations: map[string]bool{"insert": true},
		}),
		SpecPath: "omnicore-gen/role.omnicore.yaml",
	})

	for _, want := range []string{
		"| Update a role | `PATCH /roles/:id` | — |",
		"| Take out one `RolePermission` | `PATCH /roles/:id/permissions/:rolePermissionId/archive` | — |",
		"Narrowed on purpose",
		"`patch` off GraphQL (surfaces.graphql.mutations)",
		"`change RolePermission` off GraphQL (children[].surfaces.graphql)",
		"`remove RolePermission` off GraphQL (children[].surfaces.graphql)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the matrix does not account for %q:\n%s", want, out)
		}
	}
}

// A surface that is simply off is not a narrowing, and must not be reported as
// one: a GraphQL-only service would otherwise read a paragraph asking it to
// confirm every REST route it deliberately does not have.
func TestASurfaceThatIsOffIsNotReportedAsANarrowing(t *testing.T) {
	children := grantsCollection()
	children[0].OnREST = false

	out := Render(Input{
		Model: surfaceModel(children, ir.Surfaces{
			GraphQL:      true,
			GQLMutations: map[string]bool{"insert": true, "update": true},
		}),
		SpecPath: "omnicore-gen/role.omnicore.yaml",
	})

	if !strings.Contains(out, "Surfaces enabled: **GraphQL**") {
		t.Errorf("the enabled surfaces are wrong:\n%s", out)
	}
	if strings.Contains(out, "Narrowed on purpose") {
		t.Errorf("a surface that is off was reported as a per-verb decision:\n%s", out)
	}
	if !strings.Contains(out, "| Add one `RolePermission` | — | `addRolePermission` |") {
		t.Errorf("the collection's REST column should be a dash and its schema column a field:\n%s", out)
	}
}
