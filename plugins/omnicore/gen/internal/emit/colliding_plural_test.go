package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The shape three separate emitter bugs were found in at once, and the reason
// they are asserted from ONE fixture: they were reported together, from one
// User entity in an RBAC service, and each of them is invisible from the spec.
//
//   - a password confirmation compared against the password — the confirmation
//     is `source: body` and typed as a plain string, the password is a value
//     object;
//   - a collection called Papeis, whose plural a second entity of the same
//     service legitimately reuses.
//
// The spec below is green on check, and was green before any of the three was
// fixed. That is the point: nothing in the language said them, so nothing in
// the language could have caught them.
const collidingPluralSpec = `
specVersion: 1
entity: Usuario
plural: Usuarios
language: pt-BR
storage:
  kind: flat
  table: usuarios
  description: Usuarios.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Ana, description: O nome.}
  - {name: Senha, type: string, column: senha, length: 200, livesOn: root, vo: {kind: raw, ref: Senha}, example: s3nh4forte, description: A senha.}
  - name: ConfirmacaoSenha
    type: string
    runtime: true
    source: body
    modes: [insert]
    livesOn: root
    example: s3nh4forte
    description: A senha outra vez.
valueObjects:
  - name: Senha
    kind: raw
    backing: string
    description: Uma senha forte.
    minLength: 8
    maxLength: 200
    notification: SenhaInvalidaNotification
children:
  - name: UsuarioPapel
    plural: Papeis
    table: usuario_papeis
    parentColumn: usuario_id
    description: Os papeis.
    ownedBy: root
    editStrategy: per-child
    operations: [add, remove]
    businessIdentity: [PapelID]
    duplicateNotification: PapelJaNoUsuarioNotification
    fields:
      - {name: PapelID, type: id, column: papel_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: O papel.}
notifications:
  - name: SenhaInvalidaNotification
    package: vos
    semantic: validation
    text: {ptbr: Senha invalida., eng: Invalid password., esp: x, fra: x, deu: x, ita: x, nld: x}
  - name: ConfirmacaoSenhaDivergenteNotification
    semantic: validation
    text: {ptbr: A confirmacao nao confere., eng: Confirmation mismatch., esp: x, fra: x, deu: x, ita: x, nld: x}
  - name: PapelJaNoUsuarioNotification
    semantic: conflict
    text: {ptbr: Papel repetido., eng: Duplicate role., esp: x, fra: x, deu: x, ita: x, nld: x}
modes: [display, insert, update]
update: {shape: patch}
rules:
  list:
    - id: senha-confirmada
      kind: comparison
      scope: [insert]
      fields: [ConfirmacaoSenha]
      other: Senha
      operator: eq
      notification: ConfirmacaoSenhaDivergenteNotification
read:
  backing: relational
  view: {name: usuarios}
  byId: true
surfaces: {rest: true}
authz:
  resource: usuario
  dataAccess: anyone-with-permission
  permissions: {insert: "usuario:escrever", patch: "usuario:escrever", read: "usuario:ler"}
`

func collidingPluralModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(collidingPluralSpec), "usuario.omnicore.yaml")
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

// TestComparisonUnwrapsAValueObject is the one that does not compile.
//
// A comparison is the only rule kind whose both operands are entity fields, and
// so the only one a value object breaks: `range` and `length` compare against a
// literal, and an untyped constant converts to whatever named type the field
// has. Two typed operands do not — `string` against `vos.Senha` is a build
// failure in a tree the spec had already said yes to.
func TestComparisonUnwrapsAValueObject(t *testing.T) {
	got := fileNamed(t, collidingPluralModel(t), "internal/domain/usuario.go")
	if !strings.Contains(got, "if e.ConfirmacaoSenha != e.Senha.Value() {") {
		t.Errorf("the comparison does not unwrap the value object it compares against:\n%s",
			ruleLines(got, "ConfirmacaoSenha"))
	}
}

// TestABodySourcedFieldIsNeverEchoed is the one that compiles, passes vet,
// passes the generated suite, and leaks a credential.
//
// echoValue defaults to true and no spec in the reporting project declared it,
// so the plaintext confirmation came back in the 422 — into the response body,
// and from there into every log that renders a notification. The field is the
// one kind the generator can recognise without being told: `source: body` means
// it reaches no copy of anything, and the refusal was the last seat where that
// was still untrue.
func TestABodySourcedFieldIsNeverEchoed(t *testing.T) {
	got := fileNamed(t, collidingPluralModel(t), "internal/domain/usuario.go")
	if strings.Contains(got, "ConfirmacaoSenhaDivergenteNotification{}, e.ConfirmacaoSenha") {
		t.Errorf("the refusal echoes the plaintext the caller sent:\n%s",
			ruleLines(got, "ConfirmacaoSenha"))
	}
	// And the rule still reports — dropping the value must not drop the answer.
	if !strings.Contains(got,
		`r.AddNotification("ConfirmacaoSenha", ConfirmacaoSenhaDivergenteNotification{})`) {
		t.Errorf("the rule lost its notification along with the echo:\n%s",
			ruleLines(got, "ConfirmacaoSenha"))
	}
}

// TestTheCollectionProjectorIsQualifiedByItsEntity is the collision.
//
// Every entity's commands land in one Go package, so a projector named from the
// plural alone is a name two entities can both claim — and in an RBAC service
// they do: Group has Roles and User has Roles. The per-entry projector beside it
// was already qualified by the entry type, which is what made this a one-line
// class of fix rather than a design question.
func TestTheCollectionProjectorIsQualifiedByItsEntity(t *testing.T) {
	m := collidingPluralModel(t)
	if len(m.Children) != 1 {
		t.Fatalf("the fixture lost its collection: %d children", len(m.Children))
	}
	if got := m.Children[0].Projector; got != "ProjectUsuarioPapeis" {
		t.Errorf("the collection projector is %q — a second entity with a Papeis "+
			"collection redeclares it in the same package", got)
	}

	src := fileNamed(t, m, "internal/application/commands/utils/usuario_usuario_papel_projection.go")
	if !strings.Contains(src, "func ProjectUsuarioPapeis(") {
		t.Errorf("the emitted projector does not carry the entity's name:\n%s", src)
	}

	// The per-entry one has always been qualified — it sits in the per-entry
	// command file rather than beside its plural twin. Asserting it keeps the
	// two names in step, which is the whole reason the collision was one-sided.
	var declared bool
	for _, f := range emitAll(t, m) {
		if strings.Contains(string(f.Content), "func ProjectOneUsuarioPapel(") {
			declared = true
		}
		// And no emitted file may still declare the unqualified name.
		if strings.Contains(string(f.Content), "func ProjectPapeis(") {
			t.Errorf("%s still declares the unqualified projector", f.Path)
		}
	}
	if !declared {
		t.Error("the per-entry projector is no longer qualified by the entry type")
	}
}

// ruleLines quotes just the lines that mention a field, so a failure reads as
// the emitted rule rather than as a whole aggregate.
func ruleLines(src, field string) string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, field) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
