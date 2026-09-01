package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// hardRemoveModel is childOpsSpec with the archive column taken off the
// collection, which is what turns the per-entry removal into a real DELETE.
//
// It is here to reach `removeOp`'s OTHER branch. Since framework v0.61.1 that
// branch is honest for ANY child: `removeChild` routes on the child's own
// DeletedAt column and nothing else, so a child that declares none — root-owned
// or base — has its row deleted. Against v0.60.0 this same fixture generated a
// `DELETE` route that answered 500 on every call, because a root-owned child
// reached the archive path with no column to stamp; that is the gap v0.61.1
// closed, and it is why this generator requires it.
func hardRemoveModel(t *testing.T) *ir.Model {
	t.Helper()
	src := strings.Replace(childOpsSpec, "%s\n", "", 1)
	src = strings.Replace(src, "    softRemove: true\n    archivedAt: deleted_at\n", "", 1)
	if strings.Contains(src, "softRemove") {
		t.Fatal("the fixture still declares softRemove")
	}
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

// removeMount is the mounted block of the per-entry removal, which is the only
// place the status, the projection and the documented text can be read together.
//
// verb is Archive or Delete: the generated names say which of the two removals
// the collection declared, because that is what the route does.
func removeMount(t *testing.T, m *ir.Model, verb string) string {
	t.Helper()
	routes := fileNamed(t, m, "internal/web/papel_routes.go")
	head := "h" + verb + "PapelPermissao, s" + verb + "PapelPermissao := "
	i := strings.Index(routes, head)
	if i < 0 {
		t.Fatalf("the removal verb is not mounted:\n%s", routes)
	}
	rest := routes[i:]
	if j := strings.Index(rest, "RequirePermission("); j >= 0 {
		return rest[:j]
	}
	return rest
}

// The per-entry removal answers 204 with no body, exactly like the root's own
// archive and delete.
//
// It used to answer 200 carrying the OWNER's id — the `:id` segment the caller
// had just put in the path, so a body that could tell them nothing they did not
// already know. Worse, the same generator emitted a 204 for `DELETE /papeis/:id`
// and a 200 for `DELETE /papeis/:id/permissoes/:pid`, so one service contradicted
// itself on one verb. This test is what keeps the two ends of that contract
// together; nothing anchored the old 200, which is how it survived.
func TestPerEntryRemoveAnswersNoContent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model func(*testing.T) *ir.Model
		verb  string
		route string
	}{
		{"archive", func(t *testing.T) *ir.Model { return childOpsModel(t, "") }, "Archive",
			`fiber.MethodPatch, "/:id/permissoes/:papelPermissaoId/archive"`},
		{"delete", hardRemoveModel, "Delete",
			`fiber.MethodDelete, "/:id/permissoes/:papelPermissaoId"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.model(t)
			mount := removeMount(t, m, tc.verb)
			for _, want := range []string{
				"fwresponses.NoBody,",
				"commands." + tc.verb + "PapelPermissaoCommand, fwresults.None]",
				"fiber.StatusNoContent)",
				tc.route,
			} {
				if !strings.Contains(mount, want) {
					t.Errorf("the removal mount is missing %q:\n%s", want, mount)
				}
			}
			if strings.Contains(mount, "fiber.StatusOK") {
				t.Errorf("the removal still answers 200:\n%s", mount)
			}
			if strings.Contains(mount, tc.verb+"PapelPermissaoResponse") {
				t.Errorf("the removal still projects a response:\n%s", mount)
			}
		})
	}
}

// A 204 has no body, so the types that described one must not exist at all.
//
// Leaving them behind would be worse than harmless: `RemovePapelPermissaoResult`
// is what a hand-written route would reach for, and it would mount the 200 the
// spec no longer describes. The verbs that DO answer keep theirs — add and
// change return the entry as stored, which is how the caller learns the id the
// server minted.
func TestPerEntryRemoveEmitsNoResultOrResponseType(t *testing.T) {
	m := childOpsModel(t, "")
	for _, f := range emitAll(t, m) {
		body := string(f.Content)
		for _, gone := range []string{
			"type ArchivePapelPermissaoResult struct",
			"type ArchivePapelPermissaoResponse struct",
		} {
			if strings.Contains(body, gone) {
				t.Errorf("%s still declares %q:\n%s", f.Path, gone, body)
			}
		}
		if strings.Contains(body, "ArchivePapelPermissaoResponse{}") {
			t.Errorf("%s still names the response the verb does not answer with:\n%s", f.Path, body)
		}
	}

	cmds := fileNamed(t, m, "internal/application/commands/archive_papel_permissao_command.go")
	if !strings.Contains(cmds, "FromEntity(_ *configuration.AppContext, _ *appdomain.Papel) (fwresults.None, error)") {
		t.Errorf("the removal command does not project None:\n%s", cmds)
	}
	for file, keep := range map[string]string{
		"add_papel_permissao_command.go":    "type AddPapelPermissaoResult struct",
		"change_papel_permissao_command.go": "type ChangePapelPermissaoResult struct",
	} {
		body := fileNamed(t, m, "internal/application/commands/"+file)
		if !strings.Contains(body, keep) {
			t.Errorf("the verbs that DO answer lost %q:\n%s", keep, body)
		}
	}
}

// The generated OpenAPI may not promise an undo that is not mounted.
//
// The root already refuses to: it appends "(reversible)" to the archive summary
// only when the unarchive endpoint exists. The per-entry archive said the entry
// was "reversible" unconditionally, and there is no per-entry unarchive to
// reverse it with — children[].operations is closed at add|change|remove,
// unarchive is a root mode, and an archived entry is not loaded into the
// aggregate, so no command can even address it. The only way "back" is a fresh
// add, which mints a NEW id.
func TestPerEntryArchiveDocPromisesNoUnarchive(t *testing.T) {
	mount := removeMount(t, childOpsModel(t, ""), "Archive")
	if strings.Contains(mount, "reversible") {
		t.Errorf("the per-entry archive still promises a reversal nothing mounts:\n%s", mount)
	}
	if !strings.Contains(mount, "no per-entry unarchive") {
		t.Errorf("the doc does not say the entry cannot be brought back:\n%s", mount)
	}
	if !strings.Contains(mount, "204") {
		t.Errorf("the doc does not state the status it answers:\n%s", mount)
	}
}
