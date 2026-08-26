package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// The identity sources exist so a fact about the caller reaches a rule without a
// ctx the domain does not have. Everything below that promise is one line in the
// command mapper, so that line is what gets asserted — not the model, which can
// carry the source and still emit nothing.

// theCallerModel is the matrix case that declares all five sources.
const theCallerCase = "36-fontes-de-identidade.yaml"

// TestEveryIdentitySourceReachesTheMapper. A source the resolver carries and the
// emitter drops leaves the field on the aggregate at its zero value: false for a
// permission, which is the safe-looking answer and the wrong one, and "" for a
// subject, which makes an ownerCheck compare the row against nobody.
func TestEveryIdentitySourceReachesTheMapper(t *testing.T) {
	m := matrixModels(t)[theCallerCase]
	if m == nil {
		t.Fatalf("%s is missing from the coverage matrix", theCallerCase)
	}
	src := mapperSource(t, m)
	for _, want := range []string{
		"e.SolicitanteId = id.Subject",
		"e.SolicitanteSetor = id.TenantID()",
		`e.SolicitantePodeAdministrar = id.HasPermission("chamado:administrar")`,
		"e.SolicitanteSuperUsuario = id.IsSuperAdmin()",
		"e.SolicitanteAutenticado = true",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the insert mapper does not feed the identity: %q is missing", want)
		}
	}
}

// TestASuperAdminFieldIsNotAskedThroughHasPermission is the one that would fail
// in production rather than in CI. HasPermission PANICS on a wildcard, so the
// `*:*` question has a method of its own — and a feed that reached for the
// obvious call would generate, compile, and take the service down on the first
// request that carried an identity.
func TestASuperAdminFieldIsNotAskedThroughHasPermission(t *testing.T) {
	m := matrixModels(t)[theCallerCase]
	if m == nil {
		t.Fatalf("%s is missing from the coverage matrix", theCallerCase)
	}
	if strings.Contains(mapperSource(t, m), `HasPermission("*:*")`) {
		t.Error("the feed asked HasPermission about the wildcard; the framework panics on that")
	}
}

// TestTheGeneratedTestGrantsWhatTheEntityAsksAbout. The generated command test
// asserts each identity field ARRIVED, and a permission field only arrives true
// if the fixture Identity actually holds the grant. Without this the suite would
// be green on the deny branch of every question the entity asks.
func TestTheGeneratedTestGrantsWhatTheEntityAsksAbout(t *testing.T) {
	m := matrixModels(t)[theCallerCase]
	if m == nil {
		t.Fatalf("%s is missing from the coverage matrix", theCallerCase)
	}
	var tests string
	for path, src := range goSources(emitAll(t, m)) {
		if strings.Contains(path, "commands") && strings.HasSuffix(path, "_test.go") {
			tests = src
		}
	}
	if tests == "" {
		t.Fatal("no generated command test was emitted")
	}
	if !strings.Contains(tests, "chamado:administrar") {
		t.Error("the fixture Identity does not hold the permission the entity asks about, " +
			"so the assertion that it arrived can only pass by accident")
	}
	if !strings.Contains(tests, `"permissions":`) {
		t.Error("the fixture Identity carries no permissions claim at all")
	}
}

// TestTheRowScopesBypassIsNotGrantedToTheOrdinaryCaller. The mapper test's caller
// is the ordinary one. Handing it the operator's bypass would make every
// generated scope assertion pass for a reason the deployment does not reproduce.
func TestTheRowScopesBypassIsNotGrantedToTheOrdinaryCaller(t *testing.T) {
	for name, m := range matrixModels(t) {
		if m.Authz.BypassField == nil {
			continue
		}
		var tests string
		for path, src := range goSources(emitAll(t, m)) {
			if strings.Contains(path, "commands") && strings.HasSuffix(path, "_test.go") {
				tests = src
			}
		}
		if strings.Contains(tests, `"permissions":`) {
			t.Errorf("%s: the generated command test grants the row scope's bypass to the "+
				"ordinary caller", name)
		}
	}
}

// mapperSource is the insert command's own file, which is where the identity
// feed is written.
func mapperSource(t *testing.T, m *ir.Model) string {
	t.Helper()
	for path, src := range goSources(emitAll(t, m)) {
		if strings.Contains(path, "commands") && strings.Contains(path, "insert_") &&
			!strings.HasSuffix(path, "_test.go") {
			return src
		}
	}
	t.Fatal("no insert command was emitted")
	return ""
}
