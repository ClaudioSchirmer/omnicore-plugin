package emit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// permissionForRoute reads the guard off ONE mounted route.
//
// Asserting on the file as a whole cannot answer the question this key raises,
// which is not "does the permission appear" but "which route did it land on":
// a value that gates the collection AND the root would look identical in a
// substring check and would be the exact bug worth catching.
func permissionForRoute(t *testing.T, routes, method, path string) string {
	t.Helper()
	mount := fmt.Sprintf("%s, %q,", method, path)
	i := strings.Index(routes, mount)
	if i < 0 {
		t.Fatalf("no route is mounted at %s %s:\n%s", method, path, routes)
	}
	rest := routes[i:]
	const marker = `RequirePermission("`
	j := strings.Index(rest, marker)
	if j < 0 {
		t.Fatalf("the route at %s %s has no permission at all:\n%s", method, path, routes)
	}
	rest = rest[j+len(marker):]
	k := strings.Index(rest, `"`)
	if k < 0 {
		t.Fatalf("the permission on %s %s is unterminated:\n%s", method, path, routes)
	}
	return rest[:k]
}

func childPermRoutes(t *testing.T, m *ir.Model) string {
	t.Helper()
	return fileNamed(t, m, "internal/web/papel_routes.go")
}

// The default is inheritance and has to stay inheritance. A spec that declares
// nothing must generate what it generated before the key existed — re-gating a
// mounted route behind something new would refuse callers holding exactly what
// they were granted, on a regeneration that changed no key.
func TestPerChildRoutesInheritTheRootsUpdatePermission(t *testing.T) {
	routes := childPermRoutes(t, childOpsModel(t, ""))
	for _, r := range []struct{ method, path string }{
		{"fiber.MethodPost", "/:id/permissoes"},
		{"fiber.MethodPut", "/:id/permissoes/:papelPermissaoId"},
		{"fiber.MethodPatch", "/:id/permissoes/:papelPermissaoId/archive"},
	} {
		if got := permissionForRoute(t, routes, r.method, r.path); got != "papel:escrever" {
			t.Errorf("%s %s requires %q, not the root's update permission",
				r.method, r.path, got)
		}
	}
}

// The point of the key: the collection edge is gated on its own, and NOTHING
// else moves. The root's own verbs are the control — an implementation that
// widened the new permission to the whole entity would pass a test that only
// looked at the collection.
func TestDeclaredChildPermissionGatesOnlyTheCollection(t *testing.T) {
	m := childOpsModel(t, "    operations: [add, remove]\n"+
		"    permissions: {add: \"papel:conceder\", remove: \"papel:conceder\"}\n")
	routes := childPermRoutes(t, m)

	for _, r := range []struct{ method, path string }{
		{"fiber.MethodPost", "/:id/permissoes"},
		{"fiber.MethodPatch", "/:id/permissoes/:papelPermissaoId/archive"},
	} {
		if got := permissionForRoute(t, routes, r.method, r.path); got != "papel:conceder" {
			t.Errorf("%s %s requires %q, not the permission the collection declared",
				r.method, r.path, got)
		}
	}

	for _, r := range []struct{ method, path, want string }{
		{"fiber.MethodPost", "/", "papel:escrever"},
		{"fiber.MethodPut", "/:id", "papel:escrever"},
		{"fiber.MethodPatch", "/:id", "papel:escrever"},
		{"fiber.MethodPatch", "/:id/archive", "papel:arquivar"},
		{"fiber.MethodGet", "/:id", "papel:ler"},
	} {
		if got := permissionForRoute(t, routes, r.method, r.path); got != r.want {
			t.Errorf("the root's %s %s moved to %q — the collection's permission leaked "+
				"onto the entity", r.method, r.path, got)
		}
	}
}

// A partial map gates the verb it names and leaves the others inheriting. The
// add is the one that widens privilege on an RBAC collection; the removal often
// belongs to whoever may edit the record.
func TestPartialChildPermissionsGateOnlyTheDeclaredVerb(t *testing.T) {
	m := childOpsModel(t, "    permissions: {add: \"papel:conceder\"}\n")
	routes := childPermRoutes(t, m)

	if got := permissionForRoute(t, routes, "fiber.MethodPost", "/:id/permissoes"); got != "papel:conceder" {
		t.Errorf("the declared verb requires %q", got)
	}
	for _, r := range []struct{ method, path string }{
		{"fiber.MethodPut", "/:id/permissoes/:papelPermissaoId"},
		{"fiber.MethodPatch", "/:id/permissoes/:papelPermissaoId/archive"},
	} {
		if got := permissionForRoute(t, routes, r.method, r.path); got != "papel:escrever" {
			t.Errorf("%s %s requires %q — an undeclared verb stopped inheriting",
				r.method, r.path, got)
		}
	}
}
