package spec

import (
	"fmt"
	"testing"
)

// A flat entity with one persisted field and one runtime field whose block is
// substituted per case, so every refusal is exercised against the same base.
// The second %s is the entity's own modes, which one case narrows.
const bodyRuntimeTemplate = `
specVersion: 1
entity: Cadastro
plural: Cadastros
language: pt-BR
storage:
  kind: flat
  table: cadastros
  description: Cadastros.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Ana, description: O nome.}
%s
modes: %s
update: {shape: patch}
read:
  backing: relational
  view: {name: cadastros}
  byId: true
surfaces: {rest: true}
authz:
  resource: cadastro
  dataAccess: anyone-with-permission
  permissions: {insert: "cadastro:escrever", patch: "cadastro:escrever", read: "cadastro:ler"}
`

func bodyRuntimeProblems(t *testing.T, field string, modes ...string) *Problems {
	t.Helper()
	list := "[display, insert, update]"
	if len(modes) == 1 {
		list = modes[0]
	}
	raw := fmt.Sprintf(bodyRuntimeTemplate, field, list)
	s, err := Parse([]byte(raw), "body-runtime.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	return Validate(s, Options{})
}

// TestBodySourcedFieldIsAccepted is the case the whole spelling exists for: a
// value the caller sends, checked by a rule, stored by nobody. It needs neither
// a column nor a claim, and saying so must not be a refusal.
func TestBodySourcedFieldIsAccepted(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: ConfirmacaoSenha
    type: string
    runtime: true
    source: body
    livesOn: root
    example: "s3nh4"
    description: A senha digitada outra vez.`)
	if ps.HasBlockers() {
		t.Fatalf("a body-sourced runtime field was refused:\n%v", ps.Error())
	}
}

// TestClaimIsStillRequiredWhenTheSourceIsTheToken keeps the old contract: a
// runtime field with no source is a claim field, and the framework has no
// convention for which claim.
func TestClaimIsStillRequiredWhenTheSourceIsTheToken(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: Solicitante
    type: string
    runtime: true
    livesOn: root
    example: ana@x.com
    description: Quem pediu.`)
	mustBlock(t, ps, "does not say where its value comes from")
	// And the message has to NAME the other answer. The blocker that sent two
	// consumers away said only "name the claim", so an author whose value comes
	// from the request read it as "runtime fields come from tokens, full stop".
	mustBlock(t, ps, "source: body")
}

// TestABodyFieldCannotAlsoNameAClaim refuses the spec that says two different
// things about where one value comes from.
func TestABodyFieldCannotAlsoNameAClaim(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: ConfirmacaoSenha
    type: string
    runtime: true
    source: body
    claim: email
    livesOn: root
    example: "s3nh4"
    description: A senha digitada outra vez.`)
	mustBlock(t, ps, "naming a claim says two different things")
}

// TestSourceIsRefusedOnAPersistedField keeps the key from reading as "this is
// not stored" on a field whose column says it is.
func TestSourceIsRefusedOnAPersistedField(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - {name: Apelido, type: string, column: apelido, length: 40, livesOn: root, source: body, example: Ana, description: O apelido.}`)
	mustBlock(t, ps, "this field has a column")
}

// TestModesIsRefusedOnAClaimField: a claim reaches every verb, including the
// bodyless ones, so naming write verbs for it promises a narrowing that nothing
// implements.
func TestModesIsRefusedOnAClaimField(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: Solicitante
    type: string
    runtime: true
    claim: email
    modes: [insert]
    livesOn: root
    example: ana@x.com
    description: Quem pediu.`)
	mustBlock(t, ps, "fed from the caller's token")
}

// TestModesMustNameVerbsTheEntityHas catches the field declared on a verb the
// spec never mounts: nothing would ever carry it, and nothing would say so.
func TestModesMustNameVerbsTheEntityHas(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: ConfirmacaoSenha
    type: string
    runtime: true
    source: body
    modes: [update]
    livesOn: root
    example: "s3nh4"
    description: A senha digitada outra vez.`, "[display, insert]")
	mustBlock(t, ps, "this entity has no update verb")
}

// TestPatchIsNotAModeAFieldCanName pins the one value the key deliberately does
// not take. The rule gates cannot tell a PATCH from a PUT — both are dispatched
// into IfUpdate — so offering the distinction here would promise an enforcement
// BuildRules has no way to deliver.
func TestPatchIsNotAModeAFieldCanName(t *testing.T) {
	ps := bodyRuntimeProblems(t, `  - name: ConfirmacaoSenha
    type: string
    runtime: true
    source: body
    modes: [patch]
    livesOn: root
    example: "s3nh4"
    description: A senha digitada outra vez.`)
	mustBlock(t, ps, "is not a write verb whose body can carry a field")
}

// TestABodyFieldCannotBeTheOwnerOfARowScopeCheck is the security one. The point
// of an owner check is that the caller does not get to choose who they are; a
// body-sourced field is precisely what the caller chooses.
func TestABodyFieldCannotBeTheOwnerOfARowScopeCheck(t *testing.T) {
	raw := fmt.Sprintf(bodyRuntimeTemplate, `  - {name: Dono, type: string, column: dono, length: 160, livesOn: root, example: ana@x.com, description: O dono.}
  - name: Chamador
    type: string
    runtime: true
    source: body
    livesOn: root
    example: ana@x.com
    description: Quem chamou.`, "[display, insert, update]")
	raw += `
rules:
  list:
    - {id: dono-confere, kind: ownerCheck, scope: [update], fields: [Dono], ownerField: Chamador, notification: NaoEDonoNotification}
notifications:
  - name: NaoEDonoNotification
    semantic: forbidden
    text: {ptbr: Nao e seu., eng: Not yours., esp: No es tuyo., fra: Pas a vous., deu: Nicht deins., ita: Non tuo., nld: Niet van jou.}
`
	s, err := Parse([]byte(raw), "body-runtime.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	mustBlock(t, Validate(s, Options{}), "fed from the REQUEST BODY")
}
