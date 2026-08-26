package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A flat entity carrying the shape this feature exists for: a value the CALLER
// sends, that reaches the entity for a rule to read, and that no column holds.
//
// Two of them, deliberately. One carries a value object and one does not,
// because the automatic value-object pass is the only thing that treats them
// differently — and the field without one must produce no exclusion at all,
// which is a property no assertion about the first would catch.
//
// `modes: [insert]` on the confirmation and the default (every write verb) on
// the passphrase, so both branches of the exclusion emitter are in one fixture.
const bodyRuntimeSpec = `
specVersion: 1
entity: Cadastro
plural: Cadastros
language: pt-BR
storage:
  kind: flat
  table: cadastros
  description: Cadastros.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Ana, description: O nome.}
  - {name: Email, type: string, column: email, length: 160, livesOn: root, vo: {kind: raw, ref: Email}, example: ana@x.com, description: O e-mail.}
  - name: ConfirmacaoEmail
    type: string
    runtime: true
    source: body
    modes: [insert]
    livesOn: root
    vo: {kind: raw, ref: Email}
    example: ana@x.com
    description: O e-mail digitado outra vez.
  - name: OrigemDoFormulario
    type: string
    runtime: true
    source: body
    livesOn: root
    example: landing
    description: De onde veio o cadastro.
valueObjects:
  - name: Email
    kind: raw
    backing: string
    description: Um e-mail valido.
    regex: '^[^@]+@[^@]+$'
    maxLength: 160
    notification: EmailInvalidoNotification
notifications:
  - name: EmailInvalidoNotification
    package: vos
    semantic: validation
    text: {ptbr: E-mail invalido., eng: Invalid e-mail., esp: Correo invalido., fra: E-mail invalide., deu: Ungueltige E-Mail., ita: E-mail non valida., nld: Ongeldig e-mailadres.}
modes: [display, insert, update, archive, unarchive]
update: {shape: both}
delete: {root: soft}
read:
  backing: relational
  view: {name: cadastros}
  byId: true
surfaces: {rest: true}
authz:
  resource: cadastro
  dataAccess: anyone-with-permission
  permissions:
    insert: "cadastro:escrever"
    update: "cadastro:escrever"
    patch: "cadastro:escrever"
    archive: "cadastro:arquivar"
    unarchive: "cadastro:arquivar"
    read: "cadastro:ler"
`

func bodyRuntimeModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(bodyRuntimeSpec), "body-runtime.omnicore.yaml")
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

// TestBodyRuntimeFieldReachesNoTable is the first of the two properties this
// spelling exists for, and the one that cannot be recovered from later.
//
// A column is where every copy of the row comes from: the outbox payload, and
// so the topic, the consuming services, both failure ledgers and the projected
// document — and the audit event. There is no redaction to fall back on for a
// value that should never have been stored, because redaction masks copies of a
// COLUMN. So the assertion is not "the schema does not redact it": it is that
// the schema, the migration and the DDL have never heard of it.
func TestBodyRuntimeFieldReachesNoTable(t *testing.T) {
	m := bodyRuntimeModel(t)
	files := emitAll(t, m)

	for _, f := range files {
		if !strings.Contains(f.Path, "schemas/") && !strings.Contains(f.Path, "migrations/") {
			continue
		}
		for _, name := range []string{"ConfirmacaoEmail", "OrigemDoFormulario", "confirmacao", "origem"} {
			if strings.Contains(string(f.Content), name) {
				t.Errorf("%s names %q — a field with no column reached a table", f.Path, name)
			}
		}
	}

	// The same statement said against the model, so a future emitter that
	// starts reading a different set is caught before it writes anything.
	for _, f := range m.Fields {
		if f.Source == "body" {
			t.Errorf("%s is in m.Fields, which is what the table is built from", f.Name)
		}
	}
}

// TestBodyRuntimeFieldIsInNoResponse is the second property. The field is an
// INPUT: echoing it back would hand a caller their own credential from a
// surface nobody expected to carry one, and would do it on the read path too,
// where no write ever put it.
func TestBodyRuntimeFieldIsInNoResponse(t *testing.T) {
	m := bodyRuntimeModel(t)

	for _, f := range m.ResponseFields() {
		if f.Source == "body" {
			t.Errorf("%s is among the response fields", f.Name)
		}
	}

	src := goSources(emitAll(t, m))
	for path, body := range src {
		if !strings.HasPrefix(path, "internal/web/requests/") {
			continue
		}
		for _, decl := range responseStructsIn(body) {
			for _, name := range []string{"ConfirmacaoEmail", "OrigemDoFormulario"} {
				if strings.Contains(decl, name) {
					t.Errorf("%s: a response type carries %q", path, name)
				}
			}
		}
	}
}

// responseStructsIn returns the body of every `type …Response struct { … }` in a
// file. Scanning the whole file would be answered by the REQUEST type sitting
// beside it, which is exactly where the field is supposed to be.
func responseStructsIn(src string) []string {
	var out []string
	for rest := src; ; {
		i := strings.Index(rest, "Response struct {")
		if i < 0 {
			return out
		}
		rest = rest[i:]
		end := strings.Index(rest, "\n}")
		if end < 0 {
			return append(out, rest)
		}
		out = append(out, rest[:end])
		rest = rest[end:]
	}
}

// TestBodyRuntimeFieldCrossesTheWriteSurface is the other half: keeping it out
// of the table is only correct if it still reaches the entity, or the feature
// is an elaborate way of dropping a field.
func TestBodyRuntimeFieldCrossesTheWriteSurface(t *testing.T) {
	src := goSources(emitAll(t, bodyRuntimeModel(t)))

	// The aggregate declares it AS the value object, and labels it like any
	// other field a caller can get wrong — that label is what a 422 payload
	// puts in `fieldLabel`. Asserted per line rather than as one substring:
	// the struct is gofmt-aligned, and how many spaces sit between the name and
	// the type is not what this is about.
	if !declaresField(src["internal/domain/cadastro.go"],
		"ConfirmacaoEmail", "vos.Email", `labelKey:"CadastroConfirmacaoEmailField"`) {
		t.Error("the aggregate does not declare ConfirmacaoEmail as a labelled value object")
	}

	for _, c := range []struct{ file, want string }{
		{"internal/web/requests/insert_cadastro.go", `json:"confirmacaoEmail"`},
		{"internal/application/commands/insert_cadastro_command.go", "e.ConfirmacaoEmail = vos.Email(c.ConfirmacaoEmail)"},
		{"internal/application/commands/insert_cadastro_command.go", "e.OrigemDoFormulario = c.OrigemDoFormulario"},
	} {
		if !strings.Contains(src[c.file], c.want) {
			t.Errorf("%s does not carry %q", c.file, c.want)
		}
	}

	// modes: [insert] — and so absent from the two verbs it did not name, in
	// the DTO and in the command alike. A field accepted on a verb its author
	// did not declare is a value silently collected and never checked.
	for _, file := range []string{
		"internal/web/requests/update_cadastro.go",
		"internal/web/requests/patch_cadastro.go",
		"internal/application/commands/update_cadastro_command.go",
		"internal/application/commands/patch_cadastro_command.go",
	} {
		if strings.Contains(src[file], "ConfirmacaoEmail") {
			t.Errorf("%s carries ConfirmacaoEmail, which is declared on the insert only", file)
		}
	}
}

// TestBodyRuntimeValueObjectIsExcludedFromTheVerbsThatDoNotCarryIt is the rule
// that makes the whole thing usable.
//
// The framework's automatic pass walks the STRUCT — that is what validates a
// columnless field for free — and it does not know which verbs carry the field.
// Without the exclusions, an archive, a delete and a patch of any other field
// are each answered with "the confirmation is required", for a value the
// request had no business sending.
func TestBodyRuntimeValueObjectIsExcludedFromTheVerbsThatDoNotCarryIt(t *testing.T) {
	entity := goSources(emitAll(t, bodyRuntimeModel(t)))["internal/domain/cadastro.go"]
	if entity == "" {
		t.Fatal("the aggregate was not emitted")
	}
	rules := entity[strings.Index(entity, "func (e *Cadastro) BuildRules("):]

	for _, gate := range []string{"IfUpdate", "IfArchive", "IfUnarchive"} {
		clause := clauseBody(rules, gate)
		if clause == "" {
			t.Errorf("no %s clause was emitted", gate)
			continue
		}
		if !strings.Contains(clause, `r.IgnoreValueObject("ConfirmacaoEmail")`) {
			t.Errorf("%s does not exclude ConfirmacaoEmail, which it never carries", gate)
		}
	}

	// The verb that DOES carry it must not exclude it: the value object is the
	// only thing checking the shape of what the caller sent.
	if insert := clauseBody(rules, "IfInsert"); strings.Contains(insert, `IgnoreValueObject("ConfirmacaoEmail")`) {
		t.Error("the insert excludes ConfirmacaoEmail, so nothing validates the value it carries")
	}

	// And a body field with no value object needs no exclusion at all — there
	// is nothing for the automatic pass to reach.
	if strings.Contains(rules, `IgnoreValueObject("OrigemDoFormulario")`) {
		t.Error("a field with no value object was excluded from a pass that never saw it")
	}
}

// TestBodyRuntimeFieldConditionallyExcludedWhenTheVerbMayCarryIt covers the
// default `modes`, where the update gate is reached by writes that carry the
// field and by writes that cannot: a PATCH that never mentions it, a per-entry
// child verb, a facet being cleared. "Judge it when a value arrived" is the
// only reading of that clause true for all of them.
func TestBodyRuntimeFieldConditionallyExcludedWhenTheVerbMayCarryIt(t *testing.T) {
	m := bodyRuntimeModel(t)
	// Give the value-object field the default modes, which is what the
	// passphrase field has here — but that one has no value object, so the
	// assertion needs the confirmation widened instead.
	for i := range m.Runtime {
		if m.Runtime[i].Name == "ConfirmacaoEmail" {
			m.Runtime[i].Modes = []string{"insert", "update"}
		}
	}

	entity := goSources(emitAll(t, m))["internal/domain/cadastro.go"]
	clause := clauseBody(entity[strings.Index(entity, "func (e *Cadastro) BuildRules("):], "IfUpdate")
	if !strings.Contains(clause, `if e.ConfirmacaoEmail == "" {`) {
		t.Errorf("the update clause excludes unconditionally; a write that DID carry the field goes unchecked:\n%s", clause)
	}
	if !strings.Contains(clause, `r.IgnoreValueObject("ConfirmacaoEmail")`) {
		t.Errorf("the update clause never excludes the field, so a patch of any other field is refused:\n%s", clause)
	}
}

// declaresField reports whether some ONE line of src carries every fragment.
// Struct members are gofmt-aligned, so the run of spaces between a name and its
// type is a formatting decision this file has no business pinning.
func declaresField(src string, fragments ...string) bool {
	for _, line := range strings.Split(src, "\n") {
		all := true
		for _, want := range fragments {
			if !strings.Contains(line, want) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// clauseBody returns the source between `r.<gate>(func() {` and its closing
// `})`, or "" when the gate was not emitted.
func clauseBody(src, gate string) string {
	open := "r." + gate + "(func() {"
	i := strings.Index(src, open)
	if i < 0 {
		return ""
	}
	rest := src[i+len(open):]
	end := strings.Index(rest, "\n\t})")
	if end < 0 {
		return rest
	}
	return rest[:end]
}
