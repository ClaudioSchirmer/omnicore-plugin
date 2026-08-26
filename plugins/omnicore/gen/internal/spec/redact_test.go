package spec

import (
	"fmt"
	"strings"
	"testing"
)

// A flat entity with one string field, one number, one id and one composite, so
// a redaction can be moved around and every refusal exercised on the same base.
// %s is where the field under test's redact block goes; %s the composite part's.
const redactTemplate = `
specVersion: 1
entity: Cliente
plural: Clientes
language: pt-BR
storage:
  kind: flat
  table: clientes
  description: Clientes.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - name: Documento
    type: string
    column: documento
    length: 14
    livesOn: root
    hidden: %t
    example: "529.982.247-25"
    description: O documento.
%s
  - name: Limite
    type: int64
    column: limite
    livesOn: root
    example: "100000"
    description: O limite de credito.
%s
  - name: TenantID
    type: id
    column: tenant_id
    livesOn: root
    example: 9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3
    description: O tenant.
%s
  - name: Preco
    livesOn: root
    vo: {kind: composite, ref: Dinheiro}
    description: O preco combinado.
%s
    parts:
      - part: Valor
        column: preco_valor
        as: PrecoValor
        example: "185000"
%s
      - {part: Moeda, column: preco_moeda, as: PrecoMoeda, length: 3, example: BRL}
valueObjects:
  - name: Dinheiro
    kind: composite
    description: Um valor com a sua moeda.
    parts:
      - {name: Valor, type: int64, description: O valor.}
      - {name: Moeda, type: string, description: A moeda.}
modes: [display, insert, update]
update: {shape: patch}
read:
  backing: %s
  view: {name: clientes%s}
  byId: true
surfaces: {rest: true}
authz:
  resource: cliente
  dataAccess: anyone-with-permission
  permissions: {insert: "cliente:escrever", patch: "cliente:escrever", read: "cliente:ler"}
`

type redactCase struct {
	onDocumento string // the redact block for the string field, already indented
	onLimite    string // ... for the int64 field
	onTenantID  string // ... for the id field
	onComposite string // ... on the composite FIELD (always a refusal)
	onPart      string // ... on the composite's Valor part
	hidden      bool
	backing     string
}

// block indents a redact declaration to the depth a field's keys sit at.
func block(indent string, lines ...string) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(indent + l)
	}
	return b.String()
}

func redactProblems(t *testing.T, c redactCase) *Problems {
	t.Helper()
	backing := c.backing
	if backing == "" {
		backing = "relational"
	}
	version := ""
	if backing == "mongo" {
		version = ", version: 1"
	}
	raw := fmt.Sprintf(redactTemplate, c.hidden,
		c.onDocumento, c.onLimite, c.onTenantID, c.onComposite, c.onPart, backing, version)
	s, err := Parse([]byte(raw), "redact.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	return Validate(s, Options{})
}

// says reports whether any problem of the given severity mentions the fragment.
func says(ps *Problems, sev Severity, fragment string) bool {
	for _, p := range ps.items {
		if p.Severity == sev && strings.Contains(p.String(), fragment) {
			return true
		}
	}
	return false
}

func mustBlock(t *testing.T, ps *Problems, fragment string) {
	t.Helper()
	if !says(ps, Blocker, fragment) {
		t.Errorf("expected a blocker mentioning %q; got:\n%v", fragment, ps.items)
	}
}

// TestBothAxesAreMandatory is the rule the framework panics on at boot, moved to
// where the author is. A defaulted axis would have to default to leaking or to
// guessing, and neither is a decision the generator gets to make.
func TestBothAxesAreMandatory(t *testing.T) {
	for _, missing := range []string{"inSync", "inAudit"} {
		present := "inAudit"
		if missing == "inAudit" {
			present = "inSync"
		}
		ps := redactProblems(t, redactCase{
			hidden:      true,
			onDocumento: block("    ", "redact:", "  "+present+": {kind: plain}"),
		})
		mustBlock(t, ps, missing+" is not declared")
		if !says(ps, Blocker, "kind: plain") {
			t.Errorf("the refusal for a missing %s does not teach the way to say "+
				"\"the real value belongs here\" — an author who wanted that is left "+
				"believing the feature refuses them", missing)
		}
	}
}

// TestEveryRedactorParameterIsHeldToItsKind covers the four ways a declaration
// can be internally inconsistent. The last two matter most: a `keep` or a
// `value` on the wrong kind is silently IGNORED by the framework, so the author
// believes a mask is in force that is not.
func TestEveryRedactorParameterIsHeldToItsKind(t *testing.T) {
	for _, c := range []struct{ name, decl, want string }{
		{"unknown kind",
			block("    ", "redact:", "  inSync: {kind: mascarar}", "  inAudit: {kind: plain}"),
			`"mascarar" is not a redactor`},
		{"fixed with no value",
			block("    ", "redact:", "  inSync: {kind: fixed}", "  inAudit: {kind: plain}"),
			"a fixed redactor needs the value it writes"},
		{"a value on a kind that ignores it",
			block("    ", "redact:", `  inSync: {kind: keep-last, keep: 4, value: "***"}`, "  inAudit: {kind: plain}"),
			"value is only read by kind: fixed"},
		{"keep-last with no keep",
			block("    ", "redact:", "  inSync: {kind: keep-last}", "  inAudit: {kind: plain}"),
			"keep-last needs how many trailing runes stay visible"},
		{"a keep on a kind that ignores it",
			block("    ", "redact:", `  inSync: {kind: fixed, value: "***", keep: 4}`, "  inAudit: {kind: plain}"),
			"keep is only read by kind: keep-last"},
	} {
		t.Run(c.name, func(t *testing.T) {
			mustBlock(t, redactProblems(t, redactCase{hidden: true, onDocumento: c.decl}), c.want)
		})
	}
}

// TestTextOnlyRedactorsAreRefusedOnOtherTypes is the type rule the framework
// enforces against the Go scalar. A mask that changes the column's type breaks
// the map the read side decodes through and the view's $jsonSchema with it.
func TestTextOnlyRedactorsAreRefusedOnOtherTypes(t *testing.T) {
	for _, kind := range []string{"{kind: keep-last, keep: 4}", "{kind: hook}"} {
		ps := redactProblems(t, redactCase{
			onLimite: block("    ", "redact:", "  inSync: "+kind, "  inAudit: {kind: plain}"),
		})
		mustBlock(t, ps, "string-only, and this field is persisted as int64")
	}
}

// TestAFixedValueMustCarryTheColumnsType is the same rule from the other side:
// the replacement is written into the payload under the column's own type, and
// a mismatch is a boot panic naming a builder call rather than a spec line.
func TestAFixedValueMustCarryTheColumnsType(t *testing.T) {
	ps := redactProblems(t, redactCase{
		onLimite: block("    ", "redact:", `  inSync: {kind: fixed, value: "oculto"}`, "  inAudit: {kind: plain}"),
	})
	mustBlock(t, ps, `"oculto" is not a int64`)

	// And the legal one is accepted.
	ok := redactProblems(t, redactCase{
		onLimite: block("    ", "redact:", `  inSync: {kind: fixed, value: "0"}`, "  inAudit: {kind: plain}"),
	})
	if says(ok, Blocker, "redact") {
		t.Errorf("a fixed 0 on an int64 column was refused:\n%v", ok.Blockers())
	}
}

// TestAnIdentifierCannotBeRedacted. A masked identifier points at nothing, and
// the row it addresses is disclosed by whatever it is joined to anyway.
func TestAnIdentifierCannotBeRedacted(t *testing.T) {
	ps := redactProblems(t, redactCase{
		onTenantID: block("    ", "redact:", `  inSync: {kind: fixed, value: "***"}`, "  inAudit: {kind: plain}"),
	})
	mustBlock(t, ps, "an `id` field cannot be redacted")
}

// TestACompositeIsRedactedPerPart. Redacting the value object as a whole would
// decide that a price's CURRENCY is as sensitive as its amount — a value object
// is one concept, not one secret.
func TestACompositeIsRedactedPerPart(t *testing.T) {
	ps := redactProblems(t, redactCase{
		onComposite: block("    ", "redact:", "  inSync: {kind: plain}", "  inAudit: {kind: plain}"),
	})
	mustBlock(t, ps, "redacted per PART")

	// The part itself is fine, and independently of its sibling.
	ok := redactProblems(t, redactCase{
		onPart: block("        ", "redact:", `  inSync: {kind: fixed, value: "0"}`, "  inAudit: {kind: plain}"),
	})
	if says(ok, Blocker, "redact") {
		t.Errorf("a redacted composite PART was refused:\n%v", ok.Blockers())
	}
}

// TestARuntimeFieldHasNoCopyToRedact. It is fed from the caller's token and is
// on no row: there is no payload and no audit entry for a mask to apply to.
func TestARuntimeFieldHasNoCopyToRedact(t *testing.T) {
	raw := strings.Replace(fmt.Sprintf(redactTemplate, false, "", "", "", "", "", "relational", ""),
		"  - name: Limite",
		`  - name: Solicitante
    type: string
    runtime: true
    claim: email
    livesOn: root
    example: a@b.com
    description: Quem chamou.
    redact:
      inSync: {kind: plain}
      inAudit: {kind: plain}
  - name: Limite`, 1)
	s, err := Parse([]byte(raw), "runtime.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	mustBlock(t, Validate(s, Options{}), "never persisted, so no copy of it exists to redact")
}

// TestBothAxesPlainIsAWarningAndNotARefusal. It is a legal thing to say — "we
// looked, and the real value belongs in both" — and a legal thing to have meant
// by accident, which is why it is said out loud rather than blocked.
func TestBothAxesPlainIsAWarningAndNotARefusal(t *testing.T) {
	ps := redactProblems(t, redactCase{
		hidden:      true,
		onDocumento: block("    ", "redact:", "  inSync: {kind: plain}", "  inAudit: {kind: plain}"),
	})
	if says(ps, Blocker, "redact") {
		t.Errorf("plain/plain was refused; it is a declaration, not an error:\n%v", ps.Blockers())
	}
	if !says(ps, Warning, "both axes are plain") {
		t.Errorf("plain/plain passed in silence; got:\n%v", ps.items)
	}
}

// TestARelationalReadModelServesTheRealValue is the asymmetry that surprises
// people, and the reason the warning exists at all: redaction governs the copies
// the FRAMEWORK makes, and a relational read model makes none — it selects the
// column. The same declaration that masks the topic and the audit trail serves
// the value verbatim on this service's own API.
func TestARelationalReadModelServesTheRealValue(t *testing.T) {
	masked := block("    ", "redact:", "  inSync: {kind: keep-last, keep: 4}", "  inAudit: {kind: plain}")

	warns := redactProblems(t, redactCase{onDocumento: masked})
	if !says(warns, Warning, "served IN THE CLEAR") {
		t.Errorf("a masked field on a relational read model passed in silence:\n%v", warns.items)
	}

	// Three ways the warning must NOT fire, because in each the author has
	// already answered it.
	for _, c := range []struct {
		name string
		c    redactCase
	}{
		{"hidden takes it from everybody", redactCase{hidden: true, onDocumento: masked}},
		{"a mongo document IS the redacted payload", redactCase{backing: "mongo", onDocumento: masked}},
		{"an inSync of plain never promised otherwise", redactCase{onDocumento: block("    ",
			"redact:", "  inSync: {kind: plain}", `  inAudit: {kind: fixed, value: "***"}`)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if ps := redactProblems(t, c.c); says(ps, Warning, "served IN THE CLEAR") {
				t.Errorf("the read-side warning cried wolf; got:\n%v", ps.Warnings())
			}
		})
	}
}

// A shared-base role, where two refusals live that a flat entity cannot reach:
// the identity's natural key, and a role that REUSES an identity it does not
// declare. %s is the natural-key field's redact block, %s the other base
// field's, and %t whether this role writes the base or points at one.
const redactSharedBaseTemplate = `
specVersion: 1
entity: Professor
plural: Professores
language: pt-BR
storage:
  kind: sharedbase-role
  table: professores
  description: Professores.
  base:
    table: pessoas
    schemaFunc: PessoaBase
    linkColumn: pessoa_id
    description: A pessoa.
    reuse: %t
    naturalKey: Documento
    link: separate-fk
    rowUniqueness: unique-fk
    orphanPolicy: keep
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - name: Documento
    type: string
    column: documento
    length: 20
    livesOn: base
    example: "PA-4471902"
    description: O documento da pessoa.
%s
  - name: Nome
    type: string
    column: nome
    length: 160
    livesOn: base
    example: Helena
    description: O nome da pessoa.
%s
  - {name: Matricula, type: string, column: matricula, length: 20, livesOn: role, example: DOC-1, description: A matricula.}
modes: [display, insert, update]
update: {shape: patch}
read:
  backing: mongo
  view: {name: professores, version: 1}
  byId: true
surfaces: {rest: true}
authz:
  resource: professor
  dataAccess: anyone-with-permission
  permissions: {insert: "professor:escrever", patch: "professor:escrever", read: "professor:ler"}
`

func sharedBaseProblems(t *testing.T, reuse bool, onNaturalKey, onName string) *Problems {
	t.Helper()
	raw := fmt.Sprintf(redactSharedBaseTemplate, reuse, onNaturalKey, onName)
	s, err := Parse([]byte(raw), "professor.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	return Validate(s, Options{})
}

// TestTheNaturalKeyCannotBeRedacted is the one refusal that is about the value
// of the promise rather than about the mechanism.
//
// The identity's id is UUIDv5 over a FIXED, PUBLIC namespace and the natural key
// in the clear — an unsalted hash — and that id travels in every payload and is
// the projected document's _id. For a low-entropy key (an 11-digit taxpayer id
// is about 2^37) recovering the value from the id is an offline brute force. So
// masking the column would hide nothing while looking like it did, which is the
// one outcome worse than not offering the feature.
func TestTheNaturalKeyCannotBeRedacted(t *testing.T) {
	ps := sharedBaseProblems(t, false,
		block("    ", "redact:", "  inSync: {kind: keep-last, keep: 4}", `  inAudit: {kind: fixed, value: "***"}`), "")
	mustBlock(t, ps, "natural key and cannot be redacted")
	if !says(ps, Blocker, "it is not the identity") {
		t.Errorf("the refusal does not say what to do instead — an author is left with a "+
			"rule and no way out; got:\n%v", ps.Blockers())
	}

	// Any OTHER base column is fine.
	ok := sharedBaseProblems(t, false, "",
		block("    ", "redact:", "  inSync: {kind: hook}", "  inAudit: {kind: plain}"))
	if says(ok, Blocker, "redact") {
		t.Errorf("an ordinary base column was refused:\n%v", ok.Blockers())
	}
}

// TestAReusedBaseCannotDeclareARedaction. A role that points at an identity
// another spec owns does not write that identity's schema, so the declaration
// would be generated NOWHERE — accepted, reported as generated, and absent from
// every payload it claimed to mask.
func TestAReusedBaseCannotDeclareARedaction(t *testing.T) {
	ps := sharedBaseProblems(t, true, "",
		block("    ", "redact:", "  inSync: {kind: keep-last, keep: 4}", "  inAudit: {kind: plain}"))
	mustBlock(t, ps, "would be generated nowhere")
}
