package emit

import (
	"strings"
	"testing"
)

// A `valueObject` rule adds no check of its own: it pulls forward one the
// framework runs anyway, so that a value object can be the PREMISE of the rules
// below it. The automatic pass happens after BuildRules, which is too late for
// anything to depend on it.
//
// Three properties are the whole feature, and each is a way the hand-written
// version of this gets it wrong:
//
//   - the call is bare (IsValid reports AND emits — a notification beside it is
//     the same complaint twice);
//   - the field is excluded from the automatic pass (or it is asked again at
//     the end, which is that duplicate arriving by the other door);
//   - an optional value object stays optional (the automatic pass skips a nil
//     field, and moving WHEN a value is checked must not change WHETHER it may
//     be absent).

const voRuleSpec = `
specVersion: 1
entity: Matricula
plural: Matriculas
language: pt-BR
storage:
  kind: flat
  table: matriculas
  description: Matriculas.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - {name: TenantID, type: id, column: tenant_id, livesOn: root, example: 0e37df36-f698-11e6-8dd4-cb9ced3df976, description: O tenant.}
  - {name: Contato, type: string, column: contato, length: 160, livesOn: root, example: a@b.co, description: O contato., vo: {kind: raw, ref: ContatoEmail}}
  - {name: Apelido, type: string, column: apelido, length: 160, livesOn: root, nullable: true, example: a@b.co, description: Um contato alternativo., vo: {kind: raw, ref: ContatoEmail}}
  - {name: Situacao, type: string, column: situacao, length: 20, livesOn: root, example: ativa, description: A situacao., vo: {kind: enum, ref: SituacaoMatricula}}
valueObjects:
  - name: ContatoEmail
    kind: raw
    backing: string
    description: Um e-mail valido.
    regex: '^[^@\s]+@[^@\s]+\.[a-z]{2,}$'
    minLength: 6
    maxLength: 160
    notification: InvalidContatoEmailNotification
  - name: SituacaoMatricula
    kind: enum
    backing: string
    description: A situacao da matricula.
    unknownNotification: UnknownSituacaoMatriculaNotification
    members:
      - {name: Ativa, value: ativa}
      - {name: Trancada, value: trancada}
modes: [display, insert, update]
update: {shape: put}
rules:
  list:
    - id: tenant-required
      kind: valueObject
      scope: [insertOrUpdate]
      fields: [TenantID]
      guard: true
      description: everything below reads this tenant.
    - id: contatos-validos
      kind: valueObject
      scope: [insertOrUpdate]
      fields: [Contato, Apelido, Situacao]
read:
  backing: relational
  view: {name: matriculas}
  byId: true
surfaces: {rest: true}
authz:
  resource: matricula
  dataAccess: anyone-with-permission
  permissions: {insert: "matricula:escrever", update: "matricula:escrever", read: "matricula:ler"}
notifications:
  - name: InvalidContatoEmailNotification
    semantic: validation
    package: vos
    text: {ptbr: Contato invalido., eng: Invalid contact., esp: x, fra: x, deu: x, ita: x, nld: x}
  - name: UnknownSituacaoMatriculaNotification
    semantic: validation
    package: vos
    text: {ptbr: Situacao desconhecida., eng: Unknown status., esp: x, fra: x, deu: x, ita: x, nld: x}
`

func voRulesBody(t *testing.T) string {
	t.Helper()
	return buildRulesOf(t, guardModel(t, voRuleSpec), "internal/domain/matricula.go")
}

func TestValueObjectRuleValidatesInPlaceAndSilencesTheAutomaticPass(t *testing.T) {
	got := voRulesBody(t)

	// An id IS a value object to the framework: domain.ID writes IsValid, which
	// is how the automatic pass discovers it — so the rule reaches it the same
	// way, with no special case in the spec.
	if !strings.Contains(got, `e.TenantID.IsValid("TenantID", r.Context())`) {
		t.Errorf("the id was not validated in place:\n%s", got)
	}
	if !strings.Contains(got, `r.IgnoreValueObject("TenantID")`) {
		t.Errorf("the field was not excluded from the automatic pass:\n%s", got)
	}
	// The call is bare on purpose: IsValid reports AND emits.
	if strings.Contains(got, "if e.TenantID.IsValid") || strings.Contains(got, "if !e.TenantID.IsValid") {
		t.Errorf("the call was wrapped in a condition:\n%s", got)
	}
	// And it raises nothing of its own — that answer belongs to the value object.
	if strings.Contains(got, `r.AddNotification("TenantID"`) {
		t.Errorf("a second notification was raised for one wrong value:\n%s", got)
	}
}

// An enum declares no IsValid at all: it names its members and the answer for a
// value outside them, and the framework checks membership. Emitting the raw
// call for one would not compile.
func TestValueObjectRuleAsksAnEnumForMembership(t *testing.T) {
	got := voRulesBody(t)

	if !strings.Contains(got, `domain.ValidateEnum(e.Situacao, "Situacao", r.Context())`) {
		t.Errorf("the enum was not validated by membership:\n%s", got)
	}
	if strings.Contains(got, "e.Situacao.IsValid(") {
		t.Errorf("an enum was asked for an IsValid it does not declare:\n%s", got)
	}
	if !strings.Contains(got, `r.IgnoreValueObject("Situacao")`) {
		t.Errorf("the enum was left in the automatic pass as well:\n%s", got)
	}
}

// Absence is not a violation, and pulling the check forward must not turn an
// optional value object into a required one.
func TestValueObjectRuleKeepsAnOptionalValueOptional(t *testing.T) {
	got := voRulesBody(t)

	i := strings.Index(got, "if e.Apelido != nil {")
	if i < 0 {
		t.Fatalf("the optional value object was dereferenced unguarded:\n%s", got)
	}
	call := strings.Index(got, `e.Apelido.IsValid("Apelido", r.Context())`)
	if call < i {
		t.Errorf("the call is not inside the nil guard:\n%s", got)
	}
	// The exclusion is unconditional: the field is this rule's business in this
	// mode whether or not it carries a value.
	if !strings.Contains(got, "\t\tr.IgnoreValueObject(\"Apelido\")") {
		t.Errorf("the exclusion was emitted inside the guard, or not at all:\n%s", got)
	}
}

// The barrier is the rule's, not the field's: it lands after the whole rule, so
// every field the rule names has had its say before the pass stops.
func TestValueObjectRuleCarriesTheBarrierAfterTheWholeRule(t *testing.T) {
	got := voRulesBody(t)

	tenant := strings.Index(got, `r.IgnoreValueObject("TenantID")`)
	barrier := strings.Index(got, "r.StopIfInvalid()")
	contato := strings.Index(got, `e.Contato.IsValid("Contato", r.Context())`)

	if tenant < 0 || barrier < 0 || contato < 0 {
		t.Fatalf("a rule is missing from the emitted body:\n%s", got)
	}
	if !(tenant < barrier && barrier < contato) {
		t.Errorf("the barrier is not between the two rules:\n%s", got)
	}
	if n := strings.Count(got, "r.StopIfInvalid()"); n != 1 {
		t.Errorf("emitted %d barriers, want 1:\n%s", n, got)
	}
}

// The doc comment on BuildRules states the rule for value objects. With a
// hoist in the file the old sentence — "nothing here validates a value object"
// — describes a file the reader can see is different, which reads as a bug in
// the generator rather than as the feature it is.
func TestValueObjectRuleCorrectsTheBuildRulesDocumentation(t *testing.T) {
	src := fileNamed(t, guardModel(t, voRuleSpec), "internal/domain/matricula.go")

	if strings.Contains(src, "Nothing here validates a value object") {
		t.Errorf("the doc comment contradicts the body it documents:\n%s", src)
	}
	if !strings.Contains(src, "AFTER these rules") {
		t.Errorf("the doc comment does not say why the check was pulled forward:\n%s", src)
	}
}
