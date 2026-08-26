package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A spec that puts a redaction at every seat one can occupy: the root, a 1:1
// facet, a collection entry, a facet OF that entry, and one part of a composite
// value object — with all four redactors between them, including the hook and a
// fixed value on each non-string type.
const redactSpec = `
specVersion: 1
entity: Paciente
plural: Pacientes
language: pt-BR
storage:
  kind: flat
  table: pacientes
  description: Pacientes.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Ana, description: O nome.}
  - name: Documento
    type: string
    column: documento
    length: 14
    livesOn: root
    hidden: true
    example: "529.982.247-25"
    description: O documento.
    redact:
      inSync: {kind: keep-last, keep: 4}
      inAudit: {kind: fixed, value: "***"}
  - name: Email
    type: string
    column: email
    length: 160
    livesOn: root
    hidden: true
    example: ana@x.com
    description: O e-mail.
    redact:
      inSync: {kind: hook}
      inAudit: {kind: plain}
  - name: Salario
    livesOn: root
    vo: {kind: composite, ref: Dinheiro}
    description: O salario declarado.
    parts:
      - part: Valor
        column: salario_valor
        as: SalarioValor
        example: "185000"
        redact:
          inSync: {kind: fixed, value: "0"}
          inAudit: {kind: fixed, value: "0"}
      - {part: Moeda, column: salario_moeda, as: SalarioMoeda, length: 3, example: BRL}
valueObjects:
  - name: Dinheiro
    kind: composite
    description: Um valor com a sua moeda.
    parts:
      - {name: Valor, type: int64, description: O valor.}
      - {name: Moeda, type: string, description: A moeda.}
siblings:
  - name: Contato
    table: paciente_contatos
    attachTo: root
    description: O contato.
    fields:
      - name: Telefone
        type: string
        column: telefone
        length: 20
        nullable: true
        example: "11999998888"
        description: O telefone.
        redact:
          inSync: {kind: keep-last, keep: 2}
          inAudit: {kind: fixed, value: "***"}
children:
  - name: Exame
    plural: Exames
    table: paciente_exames
    parentColumn: paciente_id
    ownedBy: root
    editStrategy: atomic-replace
    description: Os exames.
    businessIdentity: [Codigo]
    fields:
      - {name: Codigo, type: string, column: codigo, length: 20, example: HEM, description: O codigo.}
      - name: Resultado
        type: string
        column: resultado
        length: 200
        example: normal
        description: O resultado.
        redact:
          inSync: {kind: hook}
          inAudit: {kind: fixed, value: "***"}
      - name: ColhidoEm
        type: time
        column: colhido_em
        example: "2026-02-01T09:00:00Z"
        description: Quando foi colhido.
        redact:
          inSync: {kind: fixed, value: "1970-01-01T00:00:00Z"}
          inAudit: {kind: plain}
      - name: Reservado
        type: bool
        column: reservado
        example: "false"
        description: Se e reservado.
        redact:
          inSync: {kind: fixed, value: "false"}
          inAudit: {kind: plain}
modes: [display, insert, update]
update: {shape: put}
read:
  backing: mongo
  view: {name: pacientes, version: 1}
  byId: true
surfaces: {rest: true}
authz:
  resource: paciente
  dataAccess: anyone-with-permission
  permissions: {insert: "paciente:escrever", update: "paciente:escrever", read: "paciente:ler"}
`

func redactModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(redactSpec), "redact.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("the fixture does not validate:\n%v", ps.Error())
	}
	if cov := spec.CheckCoverage(s); cov.HasBlockers() {
		t.Fatalf("the fixture is refused by this build:\n%v", cov.Error())
	}
	m, err := ir.Resolve(s, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	return m
}

// TestRedactionReachesTheSchemaAtEverySeat is the direct regression for the way
// this feature fails silently: a redaction that validates, resolves, and is then
// emitted as a plain Field.
//
// The generated tree still compiles, still boots, still passes every test — and
// the value the author asked to be masked travels to the topic, to every
// consuming service and into the projected document, in full. Nothing anywhere
// says so. Asserting on the CALL is the only check that catches it.
func TestRedactionReachesTheSchemaAtEverySeat(t *testing.T) {
	src := goSources(emitAll(t, redactModel(t)))

	root := src["internal/infra/schemas/paciente_schema.go"]
	if root == "" {
		t.Fatal("the root schema was not emitted")
	}
	child := src["internal/infra/schemas/exame_schema.go"]
	if child == "" {
		t.Fatal("the child schema was not emitted")
	}

	for _, c := range []struct{ where, file, want string }{
		{"the root", root, `RedactedField("Documento", "documento"`},
		{"the root", root, `RedactedField("Email", "email"`},
		{"the facet", root, `RedactedField("Telefone", "telefone"`},
		{"the composite part", root, `RedactedField("Valor", "salario_valor"`},
		{"the collection entry", child, `RedactedField("Resultado", "resultado"`},
		{"the collection entry", child, `RedactedField("ColhidoEm", "colhido_em"`},
		{"the facet OF a collection entry", child, `RedactedField("Observacao", "observacao"`},
	} {
		if !strings.Contains(c.file, c.want) {
			t.Errorf("%s: %s is missing — a redaction that emits a plain Field masks nothing "+
				"and reports nothing", c.where, c.want)
		}
	}

	// The part's SIBLING inside the value object keeps its plain declaration: a
	// part is redacted independently, and masking the currency of a salary
	// because the amount is sensitive is a decision nobody made.
	if !strings.Contains(root, `Field("Moeda", "salario_moeda")`) {
		t.Error("the unredacted part of the composite lost its plain Field declaration")
	}
	// And nothing that declared no redaction gained one.
	if strings.Contains(root, `RedactedField("Nome"`) {
		t.Error("a field with no redact block was emitted as redacted")
	}
}

// TestEachRedactorRendersItsConstructor pins the mapping from the spec's word to
// the framework's call, including the two that carry a TYPED literal.
//
// The literal's type is not decoration: the framework compares it against the
// column's effective scalar at construction and PANICS on a mismatch, because a
// payload whose column changed type breaks the map the read side decodes
// through. An untyped constant is `int` and `float64` — wrong for exactly the
// int64 money column most likely to be masked with a zero.
func TestEachRedactorRendersItsConstructor(t *testing.T) {
	src := goSources(emitAll(t, redactModel(t)))
	root := src["internal/infra/schemas/paciente_schema.go"]
	child := src["internal/infra/schemas/exame_schema.go"]

	for _, c := range []struct{ file, want, why string }{
		{root, "core.InSync(core.RedactKeepLast(4))", "keep-last must carry its n"},
		{root, `core.InAudit(core.RedactWith("***"))`, "a fixed string mask must be quoted"},
		{root, "core.InAudit(core.Plain())", "plain is a declaration, not an omission"},
		{root, "core.RedactUsing(redactPacienteEmailInSync)", "a hook must call the derived function"},
		{root, "core.RedactWith(int64(0))", "an int64 column needs an int64 literal, not an untyped 0"},
		{child, "core.RedactWith(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC))", "a time mask is written as a literal instant"},
		{child, "core.RedactWith(false)", "a bool mask is written as a bool"},
	} {
		if !strings.Contains(c.file, c.want) {
			t.Errorf("%s is missing: %s", c.want, c.why)
		}
	}

	// The import that only a timestamp mask needs, and only where it is used.
	if !strings.Contains(child, `"time"`) {
		t.Error("the child schema masks a timestamp and does not import time")
	}
	if strings.Contains(root, `"time"`) {
		t.Error("the root schema imports time and has no reason to — an import nobody " +
			"needs is the kind of noise that gets copied into the next emitter")
	}
}

// TestRedactHookIsAHookFileThatPanics pins both halves of the escape hatch: the
// generator declares the function, and it does NOT invent a body.
//
// The quiet alternative — a stub returning "***" — fails in the safe direction
// and is still the wrong answer, and it is the expensive one: the framework
// cannot see that a hook's body changed later (the view hash mixes in only the
// KIND), so documents projected through a placeholder are repaired by a version
// bump and a rebuild, by hand, months later. A panic costs one failed write.
func TestRedactHookIsAHookFileThatPanics(t *testing.T) {
	files := emitAll(t, redactModel(t))

	var hook *fsplan.File
	for i := range files {
		if strings.HasSuffix(files[i].Path, "paciente_redactors_manual.go") {
			hook = &files[i]
		}
	}
	if hook == nil {
		t.Fatal("no redactor hook file was emitted for a spec declaring two of them")
	}
	if hook.Class != fsplan.Hook {
		t.Errorf("the redactor file is %q: hand-written code in an Owned file is "+
			"clobbered by the next run", hook.Class)
	}
	if hook.Consequence == "" {
		t.Error("the hook file states no consequence — a reader cannot tell a quiet TODO " +
			"from one that rolls back writes")
	}

	body := string(hook.Content)
	for _, want := range []string{
		"func redactPacienteEmailInSync(v string) string {",
		"func redactExameResultadoInSync(v string) string {",
		"func redactPacienteObservacaoInSync(v string) string {",
		"panic(",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the hook file is missing %q", want)
		}
	}
	// One function per AXIS, and only for the axes that asked for one.
	if strings.Contains(body, "InAudit") {
		t.Error("a function was written for an audit axis that declared no hook")
	}
}

// TestNoHookFileWithoutAHook keeps the tree free of a file that teaches nothing.
func TestNoHookFileWithoutAHook(t *testing.T) {
	m := redactModel(t)
	// Strip the two hook axes and re-emit.
	strip := func(fs []ir.Field) {
		for i := range fs {
			if fs[i].Redaction != nil && fs[i].Redaction.InSync.IsHook() {
				fs[i].Redaction.InSync = ir.Redactor{Kind: "plain"}
			}
		}
	}
	strip(m.Fields)
	for i := range m.Children {
		strip(m.Children[i].Fields)
	}
	for _, f := range emitAll(t, m) {
		if strings.HasSuffix(f.Path, "_redactors_manual.go") {
			t.Errorf("%s was written for a spec that declares no hook — a file of nothing "+
				"but a header is one an author opens once and stops opening", f.Path)
		}
	}
}
