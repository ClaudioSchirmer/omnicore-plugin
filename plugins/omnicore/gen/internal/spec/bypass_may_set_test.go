package spec

import (
	"fmt"
	"strings"
	"testing"
)

// A tenant-scoped entity with a collection, so the key can be moved to every
// seat that has to refuse it. The slots are: the TenantID field's extra keys,
// an extra key on an ordinary field, one on a collection entry's field, the
// dataAccess, and the bypass line.
const bypassTemplate = `
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
    example: escola-alfa
    description: O tenant dono da linha.
%s
  - name: Chave
    type: string
    column: chave
    length: 64
    livesOn: root
    example: administrador
    description: A chave do perfil.
%s
children:
  - name: PerfilItem
    plural: Itens
    table: perfil_itens
    parentColumn: perfil_id
    description: Itens do perfil.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [Rotulo]
    fields:
      - name: Rotulo
        type: string
        column: rotulo
        length: 40
        example: leitura
        description: O rotulo do item.
%s
modes: [display, insert, update]
update: {shape: patch}
read:
  backing: relational
  view: {name: perfis}
  byId: true
surfaces: {rest: true}
authz:
  resource: perfil
  dataAccess: %s
  tenantField: TenantID
%s
  permissions: {insert: "perfil:escrever", patch: "perfil:escrever", read: "perfil:ler"}
`

type bypassCase struct {
	onTenant   string // extra keys under the TenantID field, already indented
	onOrdinary string // ... under Chave
	onChild    string // ... under the entry's Rotulo
	dataAccess string // defaults to tenant
	bypass     string // the authz.bypass line, already indented; empty = nobody crosses
	modes      string // replaces the entity's own modes line when set
}

func bypassProblems(t *testing.T, c bypassCase) *Problems {
	t.Helper()
	access := c.dataAccess
	if access == "" {
		access = "tenant"
	}
	bypass := c.bypass
	if bypass == "" {
		bypass = "  # nobody crosses the scope"
	}
	raw := fmt.Sprintf(bypassTemplate, c.onTenant, c.onOrdinary, c.onChild, access, bypass)
	if c.modes != "" {
		raw = strings.Replace(raw, "modes: [display, insert, update]", c.modes, 1)
	}
	s, err := Parse([]byte(raw), "bypass.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	// dataAccess: tenant with no tenantField is a different refusal; the cases
	// that change the access also drop the field, so the template keeps both and
	// the unused one is simply ignored by the validator.
	return Validate(s, Options{})
}

const assignedTenant = `    assignedFrom: identity-claim
    claim: tenant_id`

// TestBypassMaySetIsAcceptedOnTheScopeSubject is the case the key exists for:
// the server fills the tenant from the claim, and the one caller who crosses
// the row scope may state another.
func TestBypassMaySetIsAcceptedOnTheScopeSubject(t *testing.T) {
	ps := bypassProblems(t, bypassCase{
		onTenant: assignedTenant + "\n    bypassMaySet: true",
		bypass:   `  bypass: platform:cross-tenant`,
	})
	if ps.HasBlockers() {
		t.Fatalf("the shape this key exists for was refused:\n%v", ps.Error())
	}
}

// TestBypassMaySetNeedsSomethingToOverride: the key says who may state a value
// the SERVER would otherwise assign. On a field the client already sends there
// is nothing to except anybody from.
func TestBypassMaySetNeedsSomethingToOverride(t *testing.T) {
	ps := bypassProblems(t, bypassCase{
		onTenant: "    bypassMaySet: true",
		bypass:   `  bypass: platform:cross-tenant`,
	})
	mustBlock(t, ps, "nothing assigns this field")
}

// TestBypassMaySetIsRefusedOnADerivedField: a derived value is computed from
// the entity's own fields, so no caller — privileged or not — has one to state.
func TestBypassMaySetIsRefusedOnADerivedField(t *testing.T) {
	ps := bypassProblems(t, bypassCase{
		onTenant: "    assignedFrom: derived\n    bypassMaySet: true",
		bypass:   `  bypass: platform:cross-tenant`,
	})
	mustBlock(t, ps, "there is no caller — not even a privileged one")
}

// TestBypassMaySetNeedsABypass: with nobody crossing the scope, the exception
// applies to nobody and the field would simply be back in the request.
func TestBypassMaySetNeedsABypass(t *testing.T) {
	ps := bypassProblems(t, bypassCase{
		onTenant: assignedTenant + "\n    bypassMaySet: true",
	})
	mustBlock(t, ps, "no caller crosses the row scope")
}

// TestBypassMaySetNeedsAScopedDataAccess: nothing narrows the rows, so there is
// no scope to cross and no guard to answer a stated value.
func TestBypassMaySetNeedsAScopedDataAccess(t *testing.T) {
	ps := bypassProblems(t, bypassCase{
		onTenant:   assignedTenant + "\n    bypassMaySet: true",
		dataAccess: "anyone-with-permission",
	})
	mustBlock(t, ps, "nothing scopes the rows of this entity")
}

// TestBypassMaySetIsRefusedOffTheScopeSubject is the security one, and the
// reason the key is not simply "this field may be sent by a privileged caller".
//
// What refuses a caller who may NOT state a value is the row-scope guard, and
// that guard compares exactly one field. On any other field the value would be
// written from the request and never compared — accepted from everybody, on a
// field the spec advertises as server-assigned.
func TestBypassMaySetIsRefusedOffTheScopeSubject(t *testing.T) {
	ps := bypassProblems(t, bypassCase{
		onTenant:   assignedTenant,
		onOrdinary: "    assignedFrom: identity-subject\n    bypassMaySet: true",
		bypass:     `  bypass: platform:cross-tenant`,
	})
	mustBlock(t, ps, "is not the field the row scope narrows by")
}

// TestBypassMaySetIsRefusedOnACollectionEntry: an entry is not the subject of
// any scope, and assignedFrom is already refused there for the same reason.
func TestBypassMaySetIsRefusedOnACollectionEntry(t *testing.T) {
	ps := bypassProblems(t, bypassCase{
		onTenant: assignedTenant + "\n    bypassMaySet: true",
		onChild:  "        bypassMaySet: true",
		bypass:   `  bypass: platform:cross-tenant`,
	})
	mustBlock(t, ps, "not the subject of the row scope")
}

// TestBypassMaySetNeedsAnInsert: the exception rides on the insert body alone,
// because a record does not change scope by being updated. Without that verb it
// is offered nowhere.
func TestBypassMaySetNeedsAnInsert(t *testing.T) {
	ps := bypassProblems(t, bypassCase{
		onTenant: assignedTenant + "\n    bypassMaySet: true",
		bypass:   `  bypass: platform:cross-tenant`,
		modes:    "modes: [display, update]",
	})
	mustBlock(t, ps, "this entity has no insert verb")
}
