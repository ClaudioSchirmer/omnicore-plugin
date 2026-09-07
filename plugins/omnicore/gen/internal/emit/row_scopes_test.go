package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The three shapes the row scope could not express until authz.scopes replaced
// the fixed owner/tenant pair, proven in the EMITTED code rather than in the
// spec — the half where a green build says nothing.

// A registry whose rows ARE what the caller is scoped to: the tenant registry.
// There is no tenant_id column here because the tenant is the row.
const identityScopeSpec = `
specVersion: 1
entity: Inquilino
plural: Inquilinos
language: pt-BR
storage:
  kind: flat
  table: inquilinos
  description: Inquilinos.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: archived_at}
fields:
  - {name: Nome, type: string, column: nome, length: 160, livesOn: root, example: Escola Alfa, description: O nome.}
modes: [display, insert, update, archive]
update: {shape: both}
removal: {root: archive}
read:
  backing: relational
  view: {name: inquilinos}
  byId: true
  byParams: {filters: [{field: Nome, ops: [icontains]}], controls: {pagination: true}}
surfaces: {rest: true}
authz:
  resource: inquilino
  dataAccess: scoped
  scopes:
    - {field: ID, from: tenant}
  bypass: "*:*"
  permissions: {insert: "inquilino:escrever", update: "inquilino:escrever", patch: "inquilino:escrever", archive: "inquilino:arquivar", read: "inquilino:ler"}
`

// Three scopes at once, one of them fed by a claim the framework has never
// heard of, and one of them enforced on the writes alone.
const manyScopesSpec = `
specVersion: 1
entity: Pedido
plural: Pedidos
language: pt-BR
storage:
  kind: flat
  table: pedidos
  description: Pedidos.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: archived_at}
fields:
  - {name: TenantID, type: string, column: tenant_id, length: 60, livesOn: root, assignedFrom: identity-claim, claim: tenant_id, example: escola-alfa, description: O inquilino.}
  - {name: FilialID, type: string, column: filial_id, length: 60, livesOn: root, assignedFrom: identity-claim, claim: filial_id, example: filial-centro, description: A filial.}
  - {name: CriadoPor, type: string, column: criado_por, length: 160, livesOn: root, assignedFrom: identity-subject, example: ana@x.br, description: Quem abriu.}
  - {name: Descricao, type: string, column: descricao, length: 200, livesOn: root, example: Compra, description: O que foi pedido.}
modes: [display, insert, update, archive]
update: {shape: both}
removal: {root: archive}
read:
  backing: relational
  view: {name: pedidos}
  byId: true
  byParams: {filters: [{field: Descricao, ops: [icontains]}], controls: {pagination: true}}
surfaces: {rest: true}
authz:
  resource: pedido
  dataAccess: scoped
  scopes:
    - {field: TenantID, from: tenant}
    - {field: FilialID, from: claim, claim: filial_id}
    - {field: CriadoPor, from: subject, applies: [insert, update, archive]}
  permissions: {insert: "pedido:escrever", update: "pedido:escrever", patch: "pedido:escrever", archive: "pedido:arquivar", read: "pedido:ler"}
`

func scopeModel(t *testing.T, src string) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(src), "x.omnicore.yaml")
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

// TestTheIdentityScopeFiltersTheReadByTheRowsOwnID. The filter key is the
// framework's own logical name for the aggregate id — the same one every `?id=`
// already binds to — so nothing new has to be declared for it to resolve.
func TestTheIdentityScopeFiltersTheReadByTheRowsOwnID(t *testing.T) {
	src := goSources(emitAll(t, scopeModel(t, identityScopeSpec)))
	got := src["internal/application/queries/find_inquilinos_by_params_query.go"]
	if got == "" {
		t.Fatal("the listing query was not emitted")
	}
	if !strings.Contains(got, `q.Criteria.Filter["ID"] = id.TenantID()`) {
		t.Errorf("the listing is not scoped to the caller's own tenant:\n%s", got)
	}
	if !strings.Contains(got, "if !id.IsSuperAdmin() {") {
		t.Error("the *:* bypass does not cross the read scope, so nobody can administer the registry")
	}
}

// TestTheIdentityScopeGuardsTheWritesAndSkipsTheInsert is the asymmetry that
// makes the registry usable at all.
//
// On an insert the framework has just minted the id and it belongs to nobody:
// comparing it would refuse every creation. On an update or an archive the row
// was LOADED, which is the path the read filter never touches — the exact hole
// the write half exists to close.
func TestTheIdentityScopeGuardsTheWritesAndSkipsTheInsert(t *testing.T) {
	got := goSources(emitAll(t, scopeModel(t, identityScopeSpec)))["internal/domain/inquilino.go"]
	if got == "" {
		t.Fatal("the aggregate was not emitted")
	}
	for _, want := range []string{
		"r.IfUpdate(func() { e.refuseForeignID(r) })",
		"r.IfArchive(func() { e.refuseForeignID(r) })",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no write guard: %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "IfInsert(func() { e.refuseForeignID(r) })") ||
		strings.Contains(got, "IfInsertOrUpdate(func() { e.refuseForeignID(r) })") {
		t.Error("the insert is scoped by the row's own id — every creation would be refused, " +
			"since the framework mints that id on exactly that verb")
	}
	// No struct field holds the id, and Value() on the nil a never-persisted
	// aggregate returns would panic.
	if !strings.Contains(got, "e.GetID() != nil && e.GetID().Value() != e.RequestingID") {
		t.Errorf("the guard does not read the id through the managed carrier, nil-safely:\n%s", got)
	}
	// There is no &e.ID to point a notification at.
	if !strings.Contains(got, `r.AddNotificationNamed("ID", notifications.TenantMismatchNotification{})`) {
		t.Errorf("the refusal is not reported against the id:\n%s", got)
	}
}

// TestEveryScopeIsForcedIntoTheReadFilter. Several scopes are ANDed, which a
// filter map does for free — but only for the entries that are actually put in
// it. One missing entry is a whole dimension of isolation gone, and the listing
// still answers with rows.
func TestEveryScopeIsForcedIntoTheReadFilter(t *testing.T) {
	got := goSources(emitAll(t, scopeModel(t, manyScopesSpec)))["internal/application/queries/find_pedidos_by_params_query.go"]
	if got == "" {
		t.Fatal("the listing query was not emitted")
	}
	if !strings.Contains(got, `q.Criteria.Filter["TenantID"] = id.TenantID()`) {
		t.Errorf("the tenant scope is not forced:\n%s", got)
	}
	// A claim the framework has no accessor for: read by name, and its ZERO
	// VALUE is what a token without it scopes to — "" matches no row, whereas a
	// missing key would answer with every row in the table.
	if !strings.Contains(got, `filialIDScope, _ := id.Claims["filial_id"].(string)`) ||
		!strings.Contains(got, `q.Criteria.Filter["FilialID"] = filialIDScope`) {
		t.Errorf("the filial_id claim scope is not forced:\n%s", got)
	}
	// Declared applies: [insert, update, archive] — the reads are deliberately
	// outside it, so the listing shows the whole branch.
	if strings.Contains(got, `Filter["CriadoPor"]`) {
		t.Errorf("a scope declared for the writes alone narrowed the read too:\n%s", got)
	}
}

// TestEveryScopeGetsItsOwnGuard. One body per scope, not one with an && in it:
// a refusal has to name the field the write fell outside of, or the caller is
// told "forbidden" about a row and cannot tell which rule they broke.
func TestEveryScopeGetsItsOwnGuard(t *testing.T) {
	got := goSources(emitAll(t, scopeModel(t, manyScopesSpec)))["internal/domain/pedido.go"]
	if got == "" {
		t.Fatal("the aggregate was not emitted")
	}
	for field, carrier := range map[string]string{
		"TenantID":  "RequestingTenantID",
		"FilialID":  "RequestingFilialID",
		"CriadoPor": "RequestingCriadoPor",
	} {
		if !strings.Contains(got, "func (e *Pedido) refuseForeign"+field+"(") {
			t.Errorf("no guard for the %s scope:\n%s", field, got)
		}
		if !strings.Contains(got, "e."+field+" != e."+carrier) {
			t.Errorf("the %s guard does not compare against %s", field, carrier)
		}
		if !strings.Contains(got, "r.AddNotification(&e."+field+", notifications.TenantMismatchNotification{}, false)") {
			t.Errorf("the %s refusal does not name the field it fell outside of", field)
		}
	}
}

// TestTheClaimScopeCarrierIsFedFromTheNamedClaim. The guard reads a field the
// command mapper has to fill; a carrier nobody feeds compares against "" and
// refuses every write, or — under stand-down — none of them.
func TestTheClaimScopeCarrierIsFedFromTheNamedClaim(t *testing.T) {
	src := goSources(emitAll(t, scopeModel(t, manyScopesSpec)))
	for _, file := range []string{
		"internal/application/commands/insert_pedido_command.go",
		"internal/application/commands/archive_pedido_command.go",
	} {
		got := src[file]
		if got == "" {
			t.Fatalf("%s was not emitted", file)
		}
		if !strings.Contains(got, `if raw, ok := id.Claims["filial_id"].(string); ok {`) ||
			!strings.Contains(got, "e.RequestingFilialID = raw") {
			t.Errorf("%s does not feed the filial_id claim onto the entity:\n%s", file, got)
		}
		if !strings.Contains(got, "e.RequestingTenantID = id.TenantID()") {
			t.Errorf("%s does not feed the tenant onto the entity", file)
		}
		if !strings.Contains(got, "e.RequestingCriadoPor = id.Subject") {
			t.Errorf("%s does not feed the subject onto the entity", file)
		}
	}
}

// TestTheGeneratedSuiteStatesTheIdOfAScopedRegistry. A never-persisted
// aggregate has no id — GetID returns nil — and the guard stands down on one.
// Without the fixture stating it, every case in the generated file would pass
// with the guard asleep, which is the one failure a generated suite must not be
// able to have.
func TestTheGeneratedSuiteStatesTheIdOfAScopedRegistry(t *testing.T) {
	got := goSources(emitAll(t, scopeModel(t, identityScopeSpec)))["internal/domain/inquilino_test.go"]
	if got == "" {
		t.Fatal("the domain tests were not emitted")
	}
	if !strings.Contains(got, "e.SetID(domain.NewID(") {
		t.Errorf("the fixture never states an id, so the id scope guard stands down in every case:\n%s", got)
	}
	if !strings.Contains(got, "func TestInquilino_UpdateOutsideID_IsRefused(") {
		t.Error("the write half of the id scope is not proven")
	}
	if !strings.Contains(got, "func TestInquilino_InsertIsNotNarrowedByID(") {
		t.Error("nothing proves the insert is deliberately outside the scope — a guard " +
			"that was silently never registered would look exactly like the policy")
	}
}
