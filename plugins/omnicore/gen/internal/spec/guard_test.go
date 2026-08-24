package spec

import (
	"strings"
	"testing"
)

// A composite value object checks itself inside IsValid, which the framework
// hands a NotificationContext and nothing else. There is no Rules there, so a
// barrier has no pass to end — and a key accepted that emits nothing is the one
// outcome a rule language must never have.
func TestGuardIsRefusedOnACompositeValueObjectRule(t *testing.T) {
	const src = `
specVersion: 1
entity: Contrato
plural: Contratos
language: pt-BR
storage:
  kind: flat
  table: contratos
  description: Contratos.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - name: Vigencia
    livesOn: root
    vo: {kind: composite, ref: Periodo}
    description: A vigência do contrato.
    parts:
      - {part: From, column: vigencia_de,  as: VigenciaDe,  example: "2026-01-01T00:00:00Z"}
      - {part: To,   column: vigencia_ate, as: VigenciaAte, example: "2026-12-31T00:00:00Z"}
valueObjects:
  - name: Periodo
    kind: composite
    description: Um intervalo de tempo.
    parts:
      - {name: From, type: time, description: O início., labelKey: PeriodoFromField}
      - {name: To, type: time, nullable: true, description: O fim., labelKey: PeriodoToField}
    rules:
      list:
        - id: fim-depois-do-inicio
          kind: comparison
          fields: [To]
          other: From
          operator: gte
          notification: PeriodoInvalidoNotification
          guard: true
modes: [display, insert, update]
update: {shape: put}
read:
  backing: relational
  view: {name: contratos}
  byId: true
surfaces: {rest: true}
authz:
  resource: contrato
  dataAccess: anyone-with-permission
  permissions: {insert: "contrato:escrever", update: "contrato:escrever", read: "contrato:ler"}
notifications:
  - name: PeriodoInvalidoNotification
    package: vos
    semantic: validation
    text: {ptbr: Período inválido., eng: Invalid period., esp: x, fra: x, deu: x, ita: x, nld: x}
`
	s, err := Parse([]byte(src), "contrato.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	ps := Validate(s, Options{})
	if !ps.HasBlockers() {
		t.Fatal("a barrier on a composite value object's rule was accepted")
	}
	if !strings.Contains(ps.Error().Error(), "valueObjects[0] (Periodo).rules.list[0] (fim-depois-do-inicio).guard") {
		t.Errorf("the refusal does not point at the key:\n%v", ps.Error())
	}
}

// The one seat that refuses the key is listed in RefusedKeys, which is what makes
// `explain keys` mark them and what exempts them from the examples test. A
// refusal that only lives in the validator sends an author to write the key,
// run, and get blocked — the exact round trip the marking exists to save.
func TestTheGuardRefusalIsDocumented(t *testing.T) {
	if _, refused := RefusalFor("valueObjects[].rules.list[].guard"); !refused {
		t.Error("the composite seat is refused by the validator and not listed in RefusedKeys")
	}
	for _, path := range []string{"rules.list[].guard", "children[].rules.list[].guard"} {
		if _, refused := RefusalFor(path); refused {
			t.Errorf("%s is generated and must not be marked refused", path)
		}
	}
}
