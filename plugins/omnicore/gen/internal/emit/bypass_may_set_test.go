package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A tenant-scoped entity whose tenant is server-assigned AND stateable by the
// caller who crosses the scope — the shape two consumers abandoned identity
// assignment for, because there was no way to write it.
//
// PUT and PATCH are both mounted, so the "insert only" half of the promise is
// asserted against verbs that exist rather than against verbs that do not.
const bypassSpec = `
specVersion: 1
entity: Perfil
plural: Perfis
language: pt-BR
storage:
  kind: flat
  table: perfis
  description: Perfis.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - name: TenantID
    type: string
    column: tenant_id
    length: 60
    livesOn: root
    assignedFrom: identity-claim
    claim: tenant_id
    bypassMaySet: true
    example: escola-alfa
    description: O tenant dono da linha.
  - {name: Chave, type: string, column: chave, length: 64, livesOn: root, example: administrador, description: A chave.}
modes: [display, insert, update]
update: {shape: both}
read:
  backing: relational
  view: {name: perfis}
  byId: true
surfaces: {rest: true}
authz:
  resource: perfil
  dataAccess: tenant
  tenantField: TenantID
  bypass: platform:cross-tenant
  permissions:
    insert: "perfil:escrever"
    update: "perfil:escrever"
    patch: "perfil:escrever"
    read: "perfil:ler"
`

func bypassModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(bypassSpec), "bypass.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("the fixture does not validate:\n%v", ps.Error())
	}
	if cov := spec.CheckCoverage(s); cov.HasBlockers() {
		t.Fatalf("the fixture is refused by this build:\n%v", cov.Error())
	}
	m, err := ir.Resolve(s, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	return m
}

// TestBypassSettableScopeIsOnTheInsertAlone pins both halves of where the field
// appears.
//
// It has to be IN the insert body or the operator cannot create anything for
// the customer they support — which is the whole defect. And it has to be OUT
// of the update and the patch, because a record does not change scope by being
// edited: an update that could rewrite the tenant is a row moving between
// scopes with no migration, no audit intent and no way to undo it.
func TestBypassSettableScopeIsOnTheInsertAlone(t *testing.T) {
	m := bypassModel(t)

	if f := m.BypassSettableField(); f == nil {
		t.Fatal("the scope subject was not recognised as stateable by the bypass")
	}
	if got := names(m.CommandFields("insert")); !contains(got, "TenantID") {
		t.Errorf("the insert does not carry TenantID: %v", got)
	}
	for _, verb := range []string{"update", "patch"} {
		if got := names(m.CommandFields(verb)); contains(got, "TenantID") {
			t.Errorf("the %s carries TenantID — a record must not change scope by being edited: %v", verb, got)
		}
	}

	src := goSources(emitAll(t, m))
	// Optional on the wire, whatever the column says: "absent" is what every
	// ordinary caller sends, and it has to be distinguishable from "empty".
	if !declaresField(src["internal/web/requests/insert_perfil.go"],
		"TenantID", "*string", `json:"tenantID,omitempty"`) {
		t.Error("the insert request does not declare TenantID as an OPTIONAL pointer")
	}
	if !declaresField(src["internal/application/commands/insert_perfil_command.go"], "TenantID", "*string") {
		t.Error("the insert command does not declare TenantID as a pointer")
	}
	for _, file := range []string{
		"internal/web/requests/update_perfil.go",
		"internal/web/requests/patch_perfil.go",
		"internal/application/commands/update_perfil_command.go",
		"internal/application/commands/patch_perfil_command.go",
	} {
		if requestOrCommandStruct(src[file]) == "" {
			t.Fatalf("%s has no input type to inspect", file)
		}
		if strings.Contains(requestOrCommandStruct(src[file]), "TenantID") {
			t.Errorf("%s accepts TenantID in its body", file)
		}
	}
}

// TestStatedScopeIsAppliedAfterTheIdentityAndOutsideIt is the ordering the whole
// feature rests on.
//
// AFTER, because the caller's word is the exception and the identity is the
// rule — the other way round and a bypassing caller's tenant is overwritten by
// their own on every insert. OUTSIDE the nil check, because the value came from
// the REQUEST: an absent identity is no reason to drop it, and on a bench with
// authentication off it is the only value there is.
func TestStatedScopeIsAppliedAfterTheIdentityAndOutsideIt(t *testing.T) {
	mapper := goSources(emitAll(t, bypassModel(t)))["internal/application/commands/insert_perfil_command.go"]
	if mapper == "" {
		t.Fatal("the insert command was not emitted")
	}
	body := mapper[strings.Index(mapper, ") ToEntity("):]

	claim := strings.Index(body, `id.Claims["tenant_id"]`)
	stated := strings.Index(body, "if c.TenantID != nil {")
	if claim < 0 {
		t.Fatal("the mapper never reads the tenant claim")
	}
	if stated < 0 {
		t.Fatal("the mapper never applies a tenant the caller stated")
	}
	if stated < claim {
		t.Error("the caller's stated tenant is applied BEFORE the claim, so the identity overwrites it")
	}

	// The override must not be nested inside `if id := ctx.Identity(); id != nil`.
	// Its own line sits at one tab, the identity block's contents at two.
	if !strings.Contains(body, "\n\tif c.TenantID != nil {") {
		t.Error("the stated tenant is applied inside the identity check, so a request without one loses it")
	}
}

// TestTheStatedScopeIsAnsweredByTheRowScopeGuard is why the mapper is allowed
// to be unconditional.
//
// Nothing in the mapper asks whether the caller MAY state a tenant. What
// answers that is the guard the row scope already generates — over this exact
// field, standing down for the bypass — so a caller who may not gets the same
// refusal a write into a foreign record gets, rather than a silent 201 filed
// under the wrong tenant. If that guard ever stopped being emitted, the
// mapper's unconditional assignment would become the hole.
func TestTheStatedScopeIsAnsweredByTheRowScopeGuard(t *testing.T) {
	entity := goSources(emitAll(t, bypassModel(t)))["internal/domain/perfil.go"]
	if entity == "" {
		t.Fatal("the aggregate was not emitted")
	}
	guard := entity[strings.Index(entity, "func (e *Perfil) refuseForeignTenant("):]
	guard = guard[:strings.Index(guard, "\n}")]

	for _, want := range []string{
		"e.TenantID != e.RequestingTenant", // the stated value, compared
		"!e.RequestingMayCrossScope",       // and the bypass standing down
		"notifications.TenantMismatchNotification{}",
	} {
		if !strings.Contains(guard, want) {
			t.Errorf("the row-scope guard does not carry %q:\n%s", want, guard)
		}
	}
	if !strings.Contains(entity, "r.IfInsertOrUpdate(func() { e.refuseForeignTenant(r) })") {
		t.Error("the guard is not run on the insert, which is the verb the stated tenant rides on")
	}
}

// TestTheStatedScopeIsStillStoredAndStillReturned guards the boring half: this
// key changes WHO may supply the value, and nothing else. The column, the
// projection and every response are what they were.
func TestTheStatedScopeIsStillStoredAndStillReturned(t *testing.T) {
	m := bypassModel(t)
	src := goSources(emitAll(t, m))

	if !strings.Contains(src["internal/infra/schemas/perfil_schema.go"], `Field("TenantID", "tenant_id")`) {
		t.Error("the tenant lost its column")
	}
	if got := names(m.ResponseFields()); !contains(got, "TenantID") {
		t.Errorf("the tenant left the responses: %v", got)
	}
}

// names is the field list as an emitter would read it.
func names(fs []ir.Field) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return out
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// requestOrCommandStruct returns the INPUT type of a write file — the Request or
// the Command — and nothing else. The Response and the Result sit in the same
// files and carry the tenant on purpose, so scanning the whole file would be
// answered by the wrong type.
func requestOrCommandStruct(src string) string {
	for _, marker := range []string{"Request struct {", "Command struct {"} {
		i := strings.Index(src, marker)
		if i < 0 {
			continue
		}
		rest := src[i:]
		if end := strings.Index(rest, "\n}"); end >= 0 {
			return rest[:end]
		}
		return rest
	}
	return ""
}
