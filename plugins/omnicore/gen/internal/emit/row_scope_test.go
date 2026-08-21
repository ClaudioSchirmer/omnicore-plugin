package emit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A tenant-scoped entity. For a while only the READ half of the scope was
// generated, and the output looked complete: a reviewer read tenant isolation
// on the listings and reasonably concluded the posture was in place. A caller
// holding nothing but role:insert / role:update / role:archive could create a
// row inside another tenant, edit one and archive one — and could not read back
// the row they had just archived, so the damage was invisible from their side.
//
// Everything below is about the WRITE half, verb by verb.
const rowScopeSpec = `
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
  - {name: TenantID, type: id, column: tenant_id, livesOn: root, example: 9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3, description: O tenant.}
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Admin, description: O nome.}
modes: [display, insert, update, archive, unarchive]
update: {shape: both}
delete: {root: soft}
read:
  backing: relational
  view: {name: papeis, version: 1}
  byId: true
  byParams:
    filters: [{field: Nome, ops: [eq]}]
    sort: [Nome]
    controls: {pagination: true, orderBy: true}
surfaces: {rest: true}
authz:
  resource: papel
  dataAccess: tenant
  tenantField: TenantID
%s
  permissions: {insert: "papel:escrever", update: "papel:escrever", patch: "papel:escrever", archive: "papel:arquivar", unarchive: "papel:arquivar", read: "papel:ler"}
`

func rowScopeModel(t *testing.T, policy string) *ir.Model {
	t.Helper()
	src := strings.Replace(rowScopeSpec, "%s\n", policy, 1)
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

// The guard has to run on EVERY write verb. Archive is the one that matters
// most and is easiest to miss: it does not dispatch under IfUpdate — it is its
// own EntityMode with its own entry point — and it loads through the
// repository, which the read side's filter never touches.
func TestWriteGuardCoversEveryWriteVerb(t *testing.T) {
	m := rowScopeModel(t, "")
	got := fileNamed(t, m, "internal/domain/papel.go")
	for _, gate := range []string{"IfInsertOrUpdate", "IfArchive", "IfUnarchive"} {
		want := fmt.Sprintf("r.%s(func() { e.refuseForeignTenant(r) })", gate)
		if !strings.Contains(got, want) {
			t.Errorf("no write guard under %s — that verb writes another tenant's row unchecked", gate)
		}
	}
	if strings.Contains(got, "IfDisplay(func() { e.refuseForeignTenant") {
		t.Error("the guard runs on a READ: the contract there is an empty page, not a 403")
	}
	if !strings.Contains(got, "notifications.TenantMismatchNotification{}") {
		t.Errorf("the guard does not answer with the framework's tenant mismatch:\n%s", got)
	}
}

// An id-typed tenant column is compared as TEXT against the claim, which is
// what a claim is. Comparing the two directly does not compile — the good case
// — but only if something renders the unwrap.
func TestWriteGuardComparesAnIDAgainstTheClaimAsText(t *testing.T) {
	m := rowScopeModel(t, "")
	got := fileNamed(t, m, "internal/domain/papel.go")
	if !strings.Contains(got, "e.TenantID.Value() != e.RequestingTenant") {
		t.Errorf("the guard does not compare the row's tenant against the caller's:\n%s", got)
	}
}

// The bodyless verbs had a flat no-op for a mapper, so nothing about the caller
// ever reached the entity and the guard would have had nothing to read.
func TestBodylessVerbCarriesTheIdentity(t *testing.T) {
	m := rowScopeModel(t, "")
	got := fileNamed(t, m, "internal/application/commands/archive_papel_command.go")
	if !strings.Contains(got, "e.RequestingTenant = id.TenantID()") {
		t.Errorf("archive does not carry the caller onto the entity:\n%s", got)
	}
}

// authz.noIdentity — the policy that used to be an `else` branch.
//
// The default is stand-down, matching what every other identity-derived rule
// this generator writes already does: an ownerCheck tolerates an absent
// principal, and has to, since with auth.mode disabled no request carries one.
// A row scope that alone failed closed would be the odd one out, and the
// surprise would land on the bench where the entity is first run.
func TestNoIdentityStandsDownByDefault(t *testing.T) {
	m := rowScopeModel(t, "")
	domainSrc := fileNamed(t, m, "internal/domain/papel.go")
	if !strings.Contains(domainSrc, "e.RequestingIdentityPresent &&") {
		t.Errorf("the default refuses a write with no identity, so a dev bench writes nothing:\n%s", domainSrc)
	}
	q := fileNamed(t, m, "internal/application/queries/find_papeis_by_params_query.go")
	if strings.Contains(q, `Filter["TenantID"] = ""`) {
		t.Errorf("the default scopes an absent identity to no rows, so a dev bench lists nothing:\n%s", q)
	}
	cmd := fileNamed(t, m, "internal/application/commands/insert_papel_command.go")
	if !strings.Contains(cmd, "e.RequestingIdentityPresent = true") {
		t.Errorf("nothing records that the request carried an identity:\n%s", cmd)
	}
}

// And `refuse` is still reachable, for a service that wants the scope enforced
// even with authentication off.
func TestNoIdentityRefuseIsAvailable(t *testing.T) {
	m := rowScopeModel(t, "  noIdentity: refuse\n")
	domainSrc := fileNamed(t, m, "internal/domain/papel.go")
	if strings.Contains(domainSrc, "RequestingIdentityPresent") {
		t.Errorf("refuse still stands down for an absent identity:\n%s", domainSrc)
	}
	q := fileNamed(t, m, "internal/application/queries/find_papeis_by_params_query.go")
	if !strings.Contains(q, `Filter["TenantID"] = ""`) {
		t.Errorf("refuse does not scope an absent identity to nothing:\n%s", q)
	}
}

// The hole this shape exists to close: an empty scope means two different
// things, and only ONE of them is confined to a dev bench.
//
//   - no identity at all — reachable only with auth.mode disabled, which the
//     framework refuses outside APP_PROFILE=dev;
//   - a real, signed, valid token that simply carries no such claim — an
//     ordinary production request.
//
// Both arrive at the domain as "", because the entity is all the domain sees.
// Standing down on the VALUE would therefore hand the entire write guard to
// anyone holding a token without the claim, in production. So the guard asks
// about PRESENCE, which the mapper records inside the nil check.
func TestStandDownAsksAboutPresenceNotAboutAnEmptyScope(t *testing.T) {
	for _, policy := range []string{"", "  noIdentity: stand-down\n"} {
		m := rowScopeModel(t, policy)
		got := fileNamed(t, m, "internal/domain/papel.go")
		if strings.Contains(got, `e.RequestingTenant != ""`) {
			t.Error("the guard stands down on an EMPTY SCOPE — a token carrying no tenant " +
				"claim would bypass every write check in production")
		}
	}
}

// The SAME question, on the rule that has been tolerating an absent principal
// since long before the row scope existed. It tolerated an empty VALUE, so a
// real token carrying no `email` claim walked through an ownerCheck in
// production — the identical hole, in the older half of the language.
func TestOwnerCheckAsksAboutPresenceNotAboutAnEmptyPrincipal(t *testing.T) {
	m := idSubjectModel(t)
	got := fileNamed(t, m, "internal/domain/nota.go")
	if strings.Contains(got, `e.SolicitanteID != ""`) {
		t.Errorf("the owner check stands down on an EMPTY PRINCIPAL — a token without "+
			"the claim edits any row:\n%s", got)
	}
	if !strings.Contains(got, "e.RequestingIdentityPresent &&") {
		t.Errorf("the owner check does not ask whether an identity was present:\n%s", got)
	}
	cmd := fileNamed(t, m, "internal/application/commands/insert_nota_command.go")
	if !strings.Contains(cmd, "e.RequestingIdentityPresent = true") {
		t.Errorf("nothing records that the request carried an identity:\n%s", cmd)
	}
}

// authz.bypass — the platform operator. Before it there was no way to ask at
// all: a `*:*` holder was filtered like anybody else, and HasPermission panics
// on the wildcard such a claim carries.
func TestBypassCrossesTheScopeOnBothSides(t *testing.T) {
	m := rowScopeModel(t, "  bypass: platform:cross-tenant\n")
	q := fileNamed(t, m, "internal/application/queries/find_papeis_by_params_query.go")
	if !strings.Contains(q, `if !id.HasPermission("platform:cross-tenant")`) {
		t.Errorf("the read is scoped even for the bypass holder:\n%s", q)
	}
	domainSrc := fileNamed(t, m, "internal/domain/papel.go")
	if !strings.Contains(domainSrc, "!e.RequestingMayCrossScope") {
		t.Errorf("the write guard ignores the bypass:\n%s", domainSrc)
	}
	cmd := fileNamed(t, m, "internal/application/commands/insert_papel_command.go")
	if !strings.Contains(cmd, `e.RequestingMayCrossScope = id.HasPermission("platform:cross-tenant")`) {
		t.Errorf("nothing asks whether the caller holds the bypass:\n%s", cmd)
	}
}
