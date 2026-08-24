package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The vos package comment is the one file this generator writes ONCE PER
// PROJECT: every spec that declares a value object emits the same bytes into
// internal/domain/vos/doc.go.
//
// It used to carry the running entity's name in its header, which made a
// project with several entities permanently dirty — generate tenant, then
// permission, then role, and each run reported the previous one's doc.go as
// updated, forever. Nothing broke, and that is what made it expensive: it ruled
// out the one cheap CI check this generator's whole model rests on, "regenerate
// and prove the tree did not move".

// entityRenamed rebuilds the hand-written fixture under a different name, which
// is the smallest way to get two specs that both produce the shared file.
func entityRenamed(t *testing.T, to, table string) *ir.Model {
	t.Helper()
	src := strings.ReplaceAll(handwrittenSpec, "entity: Anotacao", "entity: "+to)
	src = strings.ReplaceAll(src, "plural: Anotacoes", "plural: "+to+"s")
	src = strings.ReplaceAll(src, "table: anotacoes", "table: "+table)
	src = strings.ReplaceAll(src, "view: {name: anotacoes}", "view: {name: "+table+"}")
	src = strings.ReplaceAll(src, "anotacao:", table+":")
	src = strings.ReplaceAll(src, "resource: anotacao", "resource: "+table)

	s, err := spec.Parse([]byte(src), strings.ToLower(to)+".omnicore.yaml")
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

func emittedFile(t *testing.T, m *ir.Model, specPath, path string) fsplan.File {
	t.Helper()
	res, err := All(m, t.TempDir(), FileMeta{
		Spec: specPath, Entity: m.Entity.Pascal, Date: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("%s: emitting: %v", m.Entity.Pascal, err)
	}
	for _, f := range res.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("%s emitted no %s", m.Entity.Pascal, path)
	return fsplan.File{}
}

func TestTheVOPackageDocIsTheSameWhicheverEntityWroteIt(t *testing.T) {
	const path = "internal/domain/vos/doc.go"

	byTenant := emittedFile(t, entityRenamed(t, "Tenant", "tenants"),
		"specs/omnicore-gen/tenant.omnicore.yaml", path)
	byRole := emittedFile(t, entityRenamed(t, "Role", "roles"),
		"specs/omnicore-gen/role.omnicore.yaml", path)

	if !byTenant.Shared || !byRole.Shared {
		t.Fatalf("%s is not marked shared, so its header still names one entity", path)
	}
	if string(byTenant.Content) != string(byRole.Content) {
		t.Errorf("two entities produced different bytes for %s:\n--- Tenant\n%s\n--- Role\n%s",
			path, byTenant.Content, byRole.Content)
	}
	for _, claim := range []string{"Tenant", "Role", "tenant.omnicore.yaml", "role.omnicore.yaml"} {
		if strings.Contains(string(byTenant.Content), claim) {
			t.Errorf("%s attributes itself to one spec: found %q", path, claim)
		}
	}
}

// Everything else the run emits IS the entity's, and must keep saying so — the
// fix is a narrow exception, not a general de-attribution.
func TestOrdinaryFilesStillNameTheirEntity(t *testing.T) {
	m := entityRenamed(t, "Tenant", "tenants")
	f := emittedFile(t, m, "specs/omnicore-gen/tenant.omnicore.yaml", "internal/domain/tenant.go")
	if f.Shared {
		t.Fatal("an aggregate's own file must not be shared")
	}
	if !strings.Contains(string(f.Content), "entity:     Tenant") ||
		!strings.Contains(string(f.Content), "spec:       specs/omnicore-gen/tenant.omnicore.yaml") {
		t.Errorf("the header stopped naming the entity and its spec:\n%s", f.Content)
	}
}
