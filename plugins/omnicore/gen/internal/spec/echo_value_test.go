package spec

import (
	"fmt"
	"testing"
)

// The one echo the generator does not leave to the author.
//
// echoValue defaults to true, and that default is right for almost everything:
// "at most 4 guardians" without "you sent 6" is half an answer. A `source: body`
// runtime field is the exception the generator can recognise on its own — it
// reaches no column, so no payload, no audit event and no response, and the
// canonical one is a password confirmation. The emitter drops the echo there
// whatever the rule says; this file covers the half that must not be silent,
// the author who wrote `echoValue: true` and would otherwise believe it.
const echoValueSpecTemplate = `
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
  - {name: Senha, type: string, column: senha, length: 200, livesOn: root, example: s3nh4forte, description: A senha.}
  - name: ConfirmacaoSenha
    type: string
    runtime: true
    source: body
    modes: [insert]
    livesOn: root
    example: s3nh4forte
    description: A senha outra vez.
modes: [display, insert, update]
update: {shape: patch}
rules:
  list:
%s
notifications:
  - name: ConfirmacaoSenhaDivergenteNotification
    semantic: validation
    text: {ptbr: A confirmacao nao confere., eng: Confirmation mismatch., esp: x, fra: x, deu: x, ita: x, nld: x}
  - name: SenhaCurtaNotification
    semantic: validation
    text: {ptbr: Senha curta., eng: Password too short., esp: x, fra: x, deu: x, ita: x, nld: x}
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

func echoValueProblems(t *testing.T, rule string) *Problems {
	t.Helper()
	raw := fmt.Sprintf(echoValueSpecTemplate, rule)
	s, err := Parse([]byte(raw), "echo-value.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	return Validate(s, Options{})
}

const confirmationRule = `    - id: senha-confirmada
      kind: comparison
      scope: [insert]
      fields: [ConfirmacaoSenha]
      other: Senha
      operator: eq
      notification: ConfirmacaoSenhaDivergenteNotification`

// TestEchoValueTrueIsRefusedOnABodySourcedField: a key the build ignores is a
// promise the author believes is in force. The same reasoning that refuses a
// redaction the generator would write nowhere.
func TestEchoValueTrueIsRefusedOnABodySourcedField(t *testing.T) {
	ps := echoValueProblems(t, confirmationRule+"\n      echoValue: true")
	mustBlock(t, ps, "fed from the REQUEST BODY")
}

// TestEchoValueFalseIsAcceptedOnABodySourcedField: saying out loud what the
// generator does anyway is not a mistake, and refusing it would make the safe
// spelling the one that fails.
func TestEchoValueFalseIsAcceptedOnABodySourcedField(t *testing.T) {
	ps := echoValueProblems(t, confirmationRule+"\n      echoValue: false")
	if ps.HasBlockers() {
		t.Fatalf("echoValue: false was refused:\n%v", ps.Error())
	}
}

// TestOmittingEchoValueIsSilent is the common case, and the one the emitter
// answers on its own: nothing was said, nothing is lost, and nothing is echoed.
func TestOmittingEchoValueIsSilent(t *testing.T) {
	ps := echoValueProblems(t, confirmationRule)
	if ps.HasBlockers() {
		t.Fatalf("the rule that says nothing about echoing was refused:\n%v", ps.Error())
	}
}

// TestEchoValueTrueStaysLegalOnAnOrdinaryField keeps the refusal narrow: which
// persisted values are sensitive is still the author's call, and this build has
// no opinion on it.
func TestEchoValueTrueStaysLegalOnAnOrdinaryField(t *testing.T) {
	ps := echoValueProblems(t, `    - id: senha-longa
      kind: length
      scope: [insert]
      fields: [Senha]
      min: 8
      echoValue: true
      notification: SenhaCurtaNotification`)
	if ps.HasBlockers() {
		t.Fatalf("echoValue: true was refused on a persisted field:\n%v", ps.Error())
	}
}
