package spec

import (
	"strings"
	"testing"
)

// A spec written against 0.64 or below meets the row scope's rename, and the
// two halves of that rename fail in two different places: the KEYS are unknown
// to the decoder, the VALUES are unknown to the validator. Both used to answer
// with a shrug — "unknown key", "not a data-access model" — which is true and
// useless when the thing did not go away but moved.
//
// These pin that both answers carry the destination.

const retiredScopeSpec = `
specVersion: 1
entity: Pedido
plural: Pedidos
language: pt-BR
storage:
  kind: flat
  table: pedidos
  description: Pedidos.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - {name: TenantID, type: string, column: tenant_id, length: 60, livesOn: root, assignedFrom: identity-claim, claim: tenant_id, example: escola-alfa, description: O inquilino.}
modes: [display, insert]
read:
  backing: relational
  view: {name: pedidos}
  byId: true
surfaces: {rest: true}
authz:
  resource: pedido
  dataAccess: %s
  permissions: {insert: "pedido:escrever", read: "pedido:ler"}
`

// TestTheRetiredScopeKeysSayWhereTheyWent — the decoder's half.
func TestTheRetiredScopeKeysSayWhereTheyWent(t *testing.T) {
	for _, key := range []string{"tenantField", "ownerField"} {
		t.Run(key, func(t *testing.T) {
			raw := strings.Replace(retiredScopeSpec, "  dataAccess: %s",
				"  dataAccess: scoped\n  "+key+": TenantID", 1)
			_, err := Parse([]byte(raw), "pedido.omnicore.yaml")
			if err == nil {
				t.Fatalf("%s is no longer a key and was accepted", key)
			}
			if !strings.Contains(err.Error(), `renamed to "scopes"`) {
				t.Errorf("the refusal does not say where %s went: %v", key, err)
			}
		})
	}
}

// TestTheRetiredScopePosturesCarryTheirMigration — the validator's half. An
// edit-distance guess could never reach the answer here: the value did not
// change spelling, it split into a posture and a list.
func TestTheRetiredScopePosturesCarryTheirMigration(t *testing.T) {
	for value, want := range map[string]string{
		"tenant":     "from: tenant",
		"owner-only": "from: subject",
	} {
		t.Run(value, func(t *testing.T) {
			raw := strings.Replace(retiredScopeSpec, "%s", value, 1)
			s, err := Parse([]byte(raw), "pedido.omnicore.yaml")
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}
			ps := Validate(s, Options{})
			mustBlock(t, ps, "retired in 0.65.0")
			mustBlock(t, ps, want)
			mustBlock(t, ps, "dataAccess: scoped")
		})
	}
}
