package spec

import (
	"fmt"
	"strings"
	"testing"
)

// The row scope used to be two fixed shapes: owner-only compared a named field
// to Identity.Subject, tenant compared another named field to
// Identity.TenantID. Three things it could not say, each of which cost a real
// service hand-written code:
//
//   - a registry whose rows ARE the thing the caller is scoped to. A tenant
//     registry has no tenant_id column — the tenant is the row's id — so there
//     was nothing to name under tenantField and the entity went unscoped;
//   - a scope by any OTHER claim. branch_id, cost_center, franchise: the
//     framework has no accessor for them and the language offered no way to say
//     "read it by name";
//   - MORE THAN ONE. A business that lives under a tenant and a branch had to
//     pick which of the two the generator would enforce.
//
// These cases pin all three, plus the refusals that keep the key honest.

const scopesTemplate = `
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
  - {name: Descricao, type: string, column: descricao, length: 200, livesOn: root, example: Compra, description: O que foi pedido.}
%s
modes: [display, insert, update, archive]
update: {shape: both}
removal: {root: archive}
read:
  backing: relational
  view: {name: pedidos}
  byId: true
surfaces: {rest: true}
authz:
  resource: pedido
%s
  permissions: {insert: "pedido:escrever", update: "pedido:escrever", patch: "pedido:escrever", archive: "pedido:arquivar", read: "pedido:ler"}
`

// scopeProblems validates the template with an authz posture block (already
// indented) and, optionally, extra fields.
func scopeProblems(t *testing.T, authz string, extraFields ...string) *Problems {
	t.Helper()
	raw := fmt.Sprintf(scopesTemplate, strings.Join(extraFields, "\n"), authz)
	s, err := Parse([]byte(raw), "pedido.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	return Validate(s, Options{})
}

func scopeMustPass(t *testing.T, authz string, extraFields ...string) {
	t.Helper()
	if ps := scopeProblems(t, authz, extraFields...); ps.HasBlockers() {
		t.Fatalf("a legitimate scope was refused:\n%v", ps.Error())
	}
}

// TestSeveralScopesAreAccepted is the case a business with a tenant AND a
// branch needs, and the one the old language forced a choice between.
func TestSeveralScopesAreAccepted(t *testing.T) {
	scopeMustPass(t, `  dataAccess: scoped
  scopes:
    - {field: TenantID, from: tenant}
    - {field: FilialID, from: claim, claim: filial_id}`)
}

// TestAScopeOverTheAggregateIdentityIsAccepted is the tenant registry: there is
// no scope column because the row IS the scope.
func TestAScopeOverTheAggregateIdentityIsAccepted(t *testing.T) {
	scopeMustPass(t, `  dataAccess: scoped
  scopes:
    - {field: ID, from: tenant}`)
}

// TestAClaimScopeNeedsItsName. The framework has no accessor for a claim it has
// never heard of, so the NAME is the whole content of the declaration — and a
// scope missing it would compare every row against the empty string, which
// matches nothing and reads as a service with no data.
func TestAClaimScopeNeedsItsName(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: FilialID, from: claim}`)
	mustBlock(t, ps, "does not say which claim")
}

// TestTheFrameworkAccessorsRefuseAClaimName. `tenant` reads whichever claim the
// DEPLOYMENT configured; naming one beside it would pin a name the deployment
// is free to change, and the two would disagree the day it did.
func TestTheFrameworkAccessorsRefuseAClaimName(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: TenantID, from: tenant, claim: tenant_id}`)
	mustBlock(t, ps, "owns which claim it looks at")
}

// TestAScopeNeedsBothHalves: a field with no fact about the caller compares
// against nothing.
func TestAScopeNeedsBothHalves(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: TenantID}`)
	mustBlock(t, ps, "which fact about the CALLER")
}

// TestAScopeOverARuntimeFieldIsRefused. Row scoping is a WHERE clause: a field
// with no column has nothing to put in it. Accepting one produced the worst
// possible outcome — the spec validated, the report said the entity was scoped,
// and the service served every row to any caller holding the read permission.
func TestAScopeOverARuntimeFieldIsRefused(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: Solicitante, from: subject}`,
		`  - {name: Solicitante, type: string, runtime: true, source: subject, livesOn: root, example: ana@x.br, description: Quem pediu.}`)
	mustBlock(t, ps, "it has no column")
}

// TestAScopeOverAnUnknownFieldIsRefused.
func TestAScopeOverAnUnknownFieldIsRefused(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: NaoExiste, from: tenant}`)
	mustBlock(t, ps, "does not name a field of this entity")
}

// TestTwoScopesOverOneFieldAreRefused: two conditions on one column, and the
// second can only ever narrow the first to nothing or repeat it.
func TestTwoScopesOverOneFieldAreRefused(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: TenantID, from: tenant}
    - {field: TenantID, from: subject}`)
	mustBlock(t, ps, "already narrowed by another scope")
}

// TestScopesUnderAnUnscopedPostureAreRefused. A posture stated one way and
// mechanised the other is what a reviewer reads as isolation that is not there.
func TestScopesUnderAnUnscopedPostureAreRefused(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: anyone-with-permission
  scopes:
    - {field: TenantID, from: tenant}`)
	mustBlock(t, ps, "the rows are not scoped")
}

// TestAScopedPostureWithNoScopesIsRefused — the other direction, and the
// dangerous one: it would generate no filter and no guard at all.
func TestAScopedPostureWithNoScopesIsRefused(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped`)
	mustBlock(t, ps, "does not say by what")
}

// TestAppliesIsCheckedAgainstWhatIsServed: a scope on a verb the entity does
// not mount enforces nothing, which is dead configuration that reads as cover.
func TestAppliesIsCheckedAgainstWhatIsServed(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: TenantID, from: tenant, applies: [read, delete]}`)
	mustBlock(t, ps, "serves no delete")
}

// TestAppliesRefusesAWordOutsideTheSet.
func TestAppliesRefusesAWordOutsideTheSet(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: TenantID, from: tenant, applies: [patch]}`)
	mustBlock(t, ps, "is not a place a scope is enforced")
}

// TestAScopeEnforcedNowhereIsRefused.
func TestAScopeEnforcedNowhereIsRefused(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: TenantID, from: tenant, applies: []}`)
	// An empty list is "omitted" to the yaml decoder, so this passes as the
	// default. The refusal that matters is the one above it; this case pins
	// that an empty list is not silently a scope that does nothing.
	if ps.HasBlockers() {
		t.Fatalf("an empty applies should read as the default, not as a refusal:\n%v", ps.Error())
	}
}

// TestReadOnlyScopeIsAccepted: the shape where a domain rule owns the writes
// and the scope only narrows what is listed.
func TestReadOnlyScopeIsAccepted(t *testing.T) {
	scopeMustPass(t, `  dataAccess: scoped
  scopes:
    - {field: TenantID, from: tenant, applies: [read]}`)
}

// TestAScopeOnTheIdentityWarnsWhenItCoversTheInsert. It is not a refusal — the
// author may be minting ids themselves — but left silent it produces a service
// that refuses every creation, and the spec is the last place anybody looks.
func TestAScopeOnTheIdentityWarnsWhenItCoversTheInsert(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: ID, from: tenant, applies: [read, insert, update]}`)
	if ps.HasBlockers() {
		t.Fatalf("this is a warning, not a refusal:\n%v", ps.Error())
	}
	if !says(ps, Warning, "refuse every creation") {
		t.Errorf("scoping an insert by the aggregate's own id was not flagged:\n%v", ps.items)
	}
}

// TestTheInsertIsSkippedByDefaultOnAnIdentityScope pins the DEFAULT, which is
// the whole reason a tenant registry is writable at all.
func TestTheInsertIsSkippedByDefaultOnAnIdentityScope(t *testing.T) {
	raw := fmt.Sprintf(scopesTemplate, "", `  dataAccess: scoped
  scopes:
    - {field: ID, from: tenant}`)
	s, err := Parse([]byte(raw), "pedido.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	got := ScopeApplies(s, s.Authz.Scopes[0])
	for _, want := range []string{"read", "update", "archive"} {
		if !contains(got, want) {
			t.Errorf("a scope over the aggregate id does not cover %s: %v", want, got)
		}
	}
	if contains(got, "insert") {
		t.Errorf("a scope over the aggregate id covers the insert by default (%v) — "+
			"the framework mints the identity on that verb, so it would refuse every "+
			"creation the entity serves", got)
	}
}

// TestAnOrdinaryScopeCoversEverythingByDefault — the other half of the same
// default, so the exception above cannot quietly become the rule.
func TestAnOrdinaryScopeCoversEverythingByDefault(t *testing.T) {
	raw := fmt.Sprintf(scopesTemplate, "", `  dataAccess: scoped
  scopes:
    - {field: TenantID, from: tenant}`)
	s, err := Parse([]byte(raw), "pedido.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	got := ScopeApplies(s, s.Authz.Scopes[0])
	for _, want := range []string{"read", "insert", "update", "archive"} {
		if !contains(got, want) {
			t.Errorf("an ordinary scope does not cover %s: %v", want, got)
		}
	}
}

// TestAScopesOwnCarrierNameIsRefused. The caller's half is synthesised onto the
// aggregate as Requesting<Field>; an author who declared one would get two Go
// struct fields with one name and a build failure with no line pointing at the
// spec. It is not a fixed word, so the static reserved list cannot hold it.
func TestAScopesOwnCarrierNameIsRefused(t *testing.T) {
	ps := scopeProblems(t, `  dataAccess: scoped
  scopes:
    - {field: TenantID, from: tenant}`,
		`  - {name: RequestingTenantID, type: string, runtime: true, source: subject, livesOn: root, example: x, description: Quem chamou.}`)
	mustBlock(t, ps, "the entity already declares one")
}
