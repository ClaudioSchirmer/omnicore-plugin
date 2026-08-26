package spec

import (
	"strings"
	"testing"
)

// "No caller may set this" and "this may have no value" are two statements, and
// the build used to accept only the first of them.
//
// The refusal read "a server-assigned field is always written, so it is never
// null" — true of the identity sources and false of `derived`, whose value is
// written by a rule the author writes and which may legitimately leave it
// unset. What a consumer shipped instead was the non-nullable column, and every
// response for a row that had no value carried the zero time:
//
//	"emailVerifiedAt": "0000-12-31T18:42:28-05:17"
//
// The other two ways out were worse — drop assignedFrom and anyone holding the
// insert permission can claim an address is verified, or drop the field.
const assignedNullableSpec = `
specVersion: 1
entity: Conta
plural: Contas
language: pt-BR
storage:
  kind: flat
  table: contas
  description: Contas.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - {name: Email, type: string, column: email, length: 160, livesOn: root,
     example: ana@escola.br, description: E-mail da conta.}
  - name: VerificadoEm
    type: time
    column: verificado_em
    livesOn: root
    NULLABLE_HERE
    assignedFrom: ASSIGNED_HERE
    CLAIM_HERE
    example: "2026-03-01T13:00:00Z"
    description: Quando o e-mail foi verificado.
modes: [display, insert, update]
update: {shape: patch}
rules:
  manual:
    - {id: verificar-email, description: Gravar VerificadoEm ao verificar o e-mail., scope: [update]}
read:
  backing: relational
  view: {name: contas}
  byId: true
surfaces: {rest: true}
authz:
  resource: conta
  dataAccess: anyone-with-permission
  permissions: {insert: "conta:escrever", patch: "conta:escrever", read: "conta:ler"}
`

// assignedNullable builds the fixture with one assignedFrom source, nullable or
// not. The identity ones need a string field, so the type moves with them.
func assignedNullable(t *testing.T, source string, nullable bool) *Problems {
	t.Helper()
	raw := strings.Replace(assignedNullableSpec, "ASSIGNED_HERE", source, 1)
	raw = strings.Replace(raw, "NULLABLE_HERE", map[bool]string{
		true: "nullable: true", false: "nullable: false",
	}[nullable], 1)
	raw = strings.Replace(raw, "CLAIM_HERE", map[bool]string{
		true: "claim: tenant_id", false: "",
	}[source == "identity-claim"], 1)
	if source != "derived" {
		raw = strings.Replace(raw, "type: time", "type: string\n    length: 160", 1)
	}
	s, err := Parse([]byte(raw), "conta.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing the %s fixture: %v", source, err)
	}
	return Validate(s, Options{})
}

// TestDerivedFieldMayBeNullable is the fix itself.
func TestDerivedFieldMayBeNullable(t *testing.T) {
	ps := assignedNullable(t, "derived", true)
	if ps.HasBlockers() {
		t.Errorf("a nullable derived field was refused — the field is written by a rule "+
			"the author writes, and that rule may leave it unset:\n%v", ps.Error())
	}
}

// TestIdentityAssignedFieldStaysNonNullable keeps the half of the refusal that
// holds: the server always has a subject, and a claim it did not have would
// have failed the request before any column was written. A nullable column
// there describes a row that cannot exist.
func TestIdentityAssignedFieldStaysNonNullable(t *testing.T) {
	for _, source := range []string{"identity-subject", "identity-claim"} {
		ps := assignedNullable(t, source, true)
		if !ps.HasBlockers() {
			t.Errorf("%s accepted nullable: a field read off the caller's token is "+
				"written on every insert", source)
			continue
		}
		if !strings.Contains(ps.Error().Error(), "never null") {
			t.Errorf("%s was refused for the wrong reason:\n%v", source, ps.Error())
		}
	}
	// And the non-nullable spelling of the same field still passes, so the test
	// above is not reading a blocker some other key raised.
	for _, source := range []string{"identity-subject", "identity-claim"} {
		if ps := assignedNullable(t, source, false); ps.HasBlockers() {
			t.Errorf("%s with nullable: false does not validate, so the refusal above "+
				"proves nothing:\n%v", source, ps.Error())
		}
	}
}

// TestNullableDerivedFieldWithNoRuleIsWarnedInItsOwnTerms guards the sentence an
// author reads when nothing computes the field.
//
// For a NON-nullable derived field the warning is a real defect report: the
// column keeps the zero value on every insert and nothing says so. For a
// nullable one, null is a legitimate resting state, and pointing at "a
// rules.manual entry scoped to insert" sends the author to write code that
// should not exist — a verification timestamp is written by whatever verifies.
func TestNullableDerivedFieldWithNoRuleIsWarnedInItsOwnTerms(t *testing.T) {
	noRules := strings.Replace(assignedNullableSpec, `rules:
  manual:
    - {id: verificar-email, description: Gravar VerificadoEm ao verificar o e-mail., scope: [update]}`, "", 1)
	raw := strings.Replace(noRules, "ASSIGNED_HERE", "derived", 1)
	raw = strings.Replace(raw, "NULLABLE_HERE", "nullable: true", 1)
	raw = strings.Replace(raw, "CLAIM_HERE", "", 1)
	s, err := Parse([]byte(raw), "conta.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	ps := Validate(s, Options{})
	if ps.HasBlockers() {
		t.Fatalf("the fixture does not validate:\n%v", ps.Error())
	}
	var found bool
	for _, p := range ps.Items() {
		if strings.Contains(p.Message, "nothing in this spec computes this field") {
			found = true
			if !strings.Contains(p.Message, "nullable") {
				t.Errorf("the warning over a NULLABLE derived field reads as the "+
					"zero-value one: %q", p.Message)
			}
		}
	}
	if !found {
		t.Error("a derived field nothing computes raised no warning at all — the " +
			"column is then a promise with no writer and nobody is told")
	}
	// The rule IS declared in the spec above, so the warning must go away when
	// it is there: a warning that fires either way teaches nothing.
	if ps := assignedNullable(t, "derived", true); len(warningsAbout(ps, "computes this field")) > 0 {
		t.Error("the warning fires even though a rules.manual entry claims the field")
	}
}

func warningsAbout(ps *Problems, needle string) []string {
	var out []string
	for _, p := range ps.Items() {
		if strings.Contains(p.Message, needle) {
			out = append(out, p.Message)
		}
	}
	return out
}
