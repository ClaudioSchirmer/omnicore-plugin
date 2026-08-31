package spec

import (
	"fmt"
	"strings"
	"testing"
)

// A framework-owned column is the one field shape where every mistake COMPILES.
// A time declared non-nullable emits StampedTimeField over a time.Time and the
// framework panics at boot, which is not visible to a reviewer reading the
// spec — so it is a blocker here. A counter is the one shape where nullability
// is a CHOICE and not a mistake: int64 counts, *int64 counts and can also hold
// the absence StampNull writes.

// The base is a flat entity that already carries a rules.manual entry, because a
// stamped column with nothing to ask for it is a WARNING of its own and would
// otherwise fire in every case below and hide what is being tested.
const stampedTemplate = `
specVersion: 1
entity: Assinatura
plural: Assinaturas
language: pt-BR
storage:
  kind: flat
  table: assinaturas
  description: Assinaturas.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - {name: Titular, type: string, column: titular, length: 120, livesOn: root, example: Ana, description: O titular.}
%s
rules:
  manual:
    - id: carimbo
      description: Pedir o carimbo quando a assinatura for cancelada.
      scope: [update]
modes: [display, insert, update]
update: {shape: patch}
read:
  backing: relational
  view: {name: assinaturas}
  byId: true
surfaces: {rest: true}
authz:
  resource: assinatura
  dataAccess: anyone-with-permission
  permissions: {insert: "assinatura:escrever", patch: "assinatura:escrever", read: "assinatura:ler"}
`

func stampedProblems(t *testing.T, field string) *Problems {
	t.Helper()
	raw := fmt.Sprintf(stampedTemplate, field)
	s, err := Parse([]byte(raw), "stamped.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	return Validate(s, Options{})
}

const (
	stampedTime    = `  - {name: CanceladaEm, type: time, column: cancelada_em, livesOn: root, nullable: true, stamped: time, example: "2029-03-02T14:31:07Z", description: Quando foi cancelada.}`
	stampedCounter = `  - {name: TotalDeCobrancas, type: int64, column: total_de_cobrancas, livesOn: root, stamped: counter, example: "3", description: Quantas cobranças sofreu.}`
)

// TestBothStampedKindsAreAccepted is the case the key exists for.
func TestBothStampedKindsAreAccepted(t *testing.T) {
	ps := stampedProblems(t, stampedTime+"\n"+stampedCounter)
	if ps.HasBlockers() {
		t.Fatalf("a well-formed stamped pair was refused:\n%v", ps.Error())
	}
}

// TestAnUnknownStampKindIsRefusedWithTheSet. "stamped: true" is the shape a
// reader guesses from a boolean-looking key, and it has to meet the two answers
// rather than a yes/no.
func TestAnUnknownStampKindIsRefusedWithTheSet(t *testing.T) {
	ps := stampedProblems(t, `  - {name: CanceladaEm, type: time, column: cancelada_em, livesOn: root, nullable: true, stamped: "true", example: x, description: Quando foi cancelada.}`)
	mustBlock(t, ps, "is not a kind of stamp")
	mustBlock(t, ps, "counter")
}

// TestTheShapeOfTheValueIsHeldToTheKind. Each of these four generates code that
// builds and a framework that panics at construction, which is the whole reason
// they are checked here.
func TestTheShapeOfTheValueIsHeldToTheKind(t *testing.T) {
	for _, c := range []struct{ name, field, want string }{
		{
			"a stamped time that is not a timestamp",
			`  - {name: CanceladaEm, type: string, column: cancelada_em, length: 40, livesOn: root, nullable: true, stamped: time, example: x, description: Quando foi cancelada.}`,
			"a stamped time is a timestamp",
		},
		{
			"a stamped time that cannot be absent",
			`  - {name: CanceladaEm, type: time, column: cancelada_em, livesOn: root, stamped: time, example: x, description: Quando foi cancelada.}`,
			"the fact has not happened",
		},
		{
			"a counter that is not an int64",
			`  - {name: TotalDeCobrancas, type: int, column: total_de_cobrancas, livesOn: root, stamped: counter, example: "3", description: Quantas.}`,
			"a stamped counter is an int64",
		},
	} {
		t.Run(c.name, func(t *testing.T) { mustBlock(t, stampedProblems(t, c.field), c.want) })
	}
}

// TestANullableCounterIsAccepted. The pointer form is what StampNull needs — a
// plain int64 has no absence to write, and the framework's write refuses the
// request naming StampEmpty instead. So a nullable counter is a deliberate
// declaration ("this row may have NO count", which is not "it counted zero"),
// and the generator lowers it to *int64 rather than refusing it.
func TestANullableCounterIsAccepted(t *testing.T) {
	ps := stampedProblems(t, `  - {name: TotalDeCobrancas, type: int64, column: total_de_cobrancas, livesOn: root, nullable: true, stamped: counter, example: "3", description: Quantas.}`)
	if ps.HasBlockers() {
		t.Fatalf("a clearable counter was refused:\n%v", ps.Error())
	}
}

// TestAStampedFieldRefusesTheKeysThatContradictIt. Each of these says something
// about the value that a value the FRAMEWORK mints cannot be: read from
// somewhere, validated as a concept, stated by a caller, or worth masking.
func TestAStampedFieldRefusesTheKeysThatContradictIt(t *testing.T) {
	for _, c := range []struct{ name, extra, want string }{
		{"assignedFrom", `assignedFrom: derived`, "no source to read"},
		{"unique", `unique: {enforce: index, notification: CanceladaEmJaExisteNotification}`, "a business key is a value someone states"},
		{"runtime", `runtime: true`, "has no column"},
	} {
		t.Run(c.name, func(t *testing.T) {
			field := `  - name: CanceladaEm
    type: time
    column: cancelada_em
    livesOn: root
    nullable: true
    stamped: time
    ` + c.extra + `
    example: "2029-03-02T14:31:07Z"
    description: Quando foi cancelada.`
			mustBlock(t, stampedProblems(t, field), c.want)
		})
	}
}

// TestAStampNobodyAsksForIsAWarning is the quiet failure this build refuses to
// ship silently: the rule DSL validates and does not mutate, so a spec with no
// rules.manual entry has declared a column that is never written and no error
// anywhere says so. It is a WARNING and not a blocker because the ask can also
// live in hand-written code beyond this spec.
func TestAStampNobodyAsksForIsAWarning(t *testing.T) {
	raw := strings.Replace(fmt.Sprintf(stampedTemplate, stampedTime), `rules:
  manual:
    - id: carimbo
      description: Pedir o carimbo quando a assinatura for cancelada.
      scope: [update]
`, "", 1)
	s, err := Parse([]byte(raw), "stamped-no-rule.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	ps := Validate(s, Options{})
	if ps.HasBlockers() {
		t.Fatalf("a stamped column with no rule is not a blocker:\n%v", ps.Error())
	}
	if !says(ps, Warning, "nothing in this spec asks for this stamp") {
		t.Errorf("a column nothing ever fills passed without a word:\n%v", ps.items)
	}
}

// TestAStampedFieldIsOutOfEveryWriteSurface is the reason patchExcludes must not
// be used on one: it is already out, so the exclusion changes nothing and reads
// as a decision.
func TestExcludingAStampedFieldFromThePatchSaysNothing(t *testing.T) {
	raw := strings.Replace(fmt.Sprintf(stampedTemplate, stampedTime),
		"update: {shape: patch}", "update: {shape: patch, patchExcludes: [CanceladaEm]}", 1)
	s, err := Parse([]byte(raw), "stamped-patch-exclude.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v", err)
	}
	ps := Validate(s, Options{})
	if !says(ps, Warning, "which no write request carries to begin with") {
		t.Errorf("a no-op exclusion passed without a word:\n%v", ps.items)
	}
}

// TestAFacetRefusesAStamp mirrors the framework's own panic: a sibling row is a
// 1:1 slice of the OWNER's row and carries no framework-owned columns of its own.
func TestAFacetRefusesAStamp(t *testing.T) {
	raw := strings.Replace(fmt.Sprintf(stampedTemplate, stampedTime), `rules:`, `siblings:
  - name: Cobranca
    table: assinaturas_cobranca
    attachTo: root
    description: A face de cobrança.
    fields:
      - {name: UltimaCobrancaEm, type: time, column: ultima_cobranca_em, livesOn: "sibling:Cobranca", nullable: true, stamped: time, example: x, description: Última cobrança.}
rules:`, 1)
	s, err := Parse([]byte(raw), "stamped-facet.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v", err)
	}
	mustBlock(t, Validate(s, Options{}), "carries no framework-owned")
}

// TestACollectionEntryRefusesAStampAndSaysWhy. The framework allows it; this
// build does not lower it, and the difference has to reach the author — a
// refusal that reads like the framework's would send them to the wrong docs.
func TestACollectionEntryRefusesAStampAndSaysWhy(t *testing.T) {
	raw := strings.Replace(fmt.Sprintf(stampedTemplate, stampedTime), `rules:`, `children:
  - name: Parcela
    plural: Parcelas
    table: assinatura_parcelas
    parentColumn: assinatura_id
    description: As parcelas.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [Numero]
    fields:
      - {name: Numero, type: int, column: numero, example: "1", description: O número.}
      - {name: QuitadaEm, type: time, column: quitada_em, nullable: true, stamped: time, example: x, description: Quando foi quitada.}
rules:`, 1)
	s, err := Parse([]byte(raw), "stamped-child.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v", err)
	}
	mustBlock(t, Validate(s, Options{}), "does not lower a stamped column on a collection entry")
}

// TestTheRefusedSeatsAreListedForExplainKeys keeps the two halves in step: a seat
// the validator blocks and `explain keys` prints unmarked sends an author to
// write it, run, and get blocked — the exact round trip the marking exists to
// save.
func TestTheStampedRefusalsAreListed(t *testing.T) {
	for _, path := range []string{"children[].fields[].stamped", "siblings[].fields[].stamped"} {
		if _, refused := RefusalFor(path); !refused {
			t.Errorf("%s is refused by the validator and not listed in RefusedKeys", path)
		}
	}
	if _, refused := RefusalFor("fields[].stamped"); refused {
		t.Error("the root seat is the one this key exists for and must not be marked refused")
	}
}
