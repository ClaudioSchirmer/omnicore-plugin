package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// One declaration, every surface. The point of the two keys is that a service
// answers in the domain's own word — so the word has to reach ALL of the places
// that name the field, or the author has renamed it in some payloads and not in
// the one the caller happened to read.
const wireNameEmitSpec = `
specVersion: 1
entity: Pessoa
plural: Pessoas
language: pt-BR
storage:
  kind: flat
  table: pessoas
  description: Pessoas.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
notifications:
  - name: DocumentoJaExisteNotification
    semantic: conflict
    text: {ptbr: Documento já cadastrado., eng: Document already registered., esp: x, fra: x, deu: x, ita: x, nld: x}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Ana, description: O nome.}
  - name: DocumentoNacional
    type: string
    column: documento_nacional
    length: 14
    livesOn: root
    example: "529.982.247-25"
    description: O documento.
    jsonName: cpf
    notifyAs: cpf
    unique:
      enforce: service-precheck+constraint
      notification: DocumentoJaExisteNotification
service:
  required: true
  facts:
    - name: DocumentoTomado
      kind: exists
      filters: [DocumentoNacional]
      excludeSelf: true
      description: Se o documento já pertence a outra pessoa.
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
read:
  backing: relational
  view: {name: pessoas}
  byId: true
surfaces: {rest: true}
authz:
  resource: pessoa
  dataAccess: anyone-with-permission
  permissions: {insert: "pessoa:escrever", update: "pessoa:escrever", patch: "pessoa:escrever", archive: "pessoa:arquivar", read: "pessoa:ler"}
`

func wireNameEmitModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(wireNameEmitSpec), "pessoa.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("the fixture does not validate:\n%v", ps.Error())
	}
	m, err := ir.Resolve(s, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	return m
}

// TestNotifyAsTravelsAsTheFrameworkTag is what makes ONE declaration govern
// every seat that can name the field.
//
// The framework reads `notifyAs:"..."` off the reflect.StructField itself, so
// the tag is honored identically by a field-reference rule, the automatic
// value-object pass, an enum membership refusal and the named seat. Writing the
// token into each emitted call instead would have been the same decision made
// in five places.
func TestNotifyAsTravelsAsTheFrameworkTag(t *testing.T) {
	entity := uniqueAnswerSource(t, wireNameEmitModel(t), "internal/domain/pessoa.go")
	if !strings.Contains(entity, "`labelKey:\"PessoaDocumentoNacionalField\" notifyAs:\"cpf\"`") {
		t.Errorf("the declared token is not on the struct field:\n%s", entity)
	}
	// A field that declared nothing keeps a bare tag. The framework's default is
	// the same lower-camel rendering, so an emitted `notifyAs:"nome"` would say
	// nothing today and freeze a stale token the day the renderer learns a new
	// acronym.
	if strings.Contains(entity, "notifyAs:\"nome\"") {
		t.Errorf("a field that declared no token got one anyway:\n%s", entity)
	}
}

// TestTheUniqueConstraintBindingSpeaksTheDeclaredToken closes the second road to
// the same conflict.
//
// The domain pre-check reaches the field by reference and the framework reads
// the tag; the database constraint is a string this generator writes. Left
// re-rendering the Go name, the two roads answered one duplicate with two
// different field names — and which one a caller saw depended on whether they
// lost the race.
func TestTheUniqueConstraintBindingSpeaksTheDeclaredToken(t *testing.T) {
	repo := uniqueAnswerSource(t, wireNameEmitModel(t), "internal/infra/pessoa_repository.go")
	if !strings.Contains(repo, `"cpf"`) {
		t.Errorf("the constraint binding still reports the derived name:\n%s", repo)
	}
	if strings.Contains(repo, `"documentoNacional"`) {
		t.Errorf("the constraint binding kept the name the override replaced:\n%s", repo)
	}
}

// TestJSONNameReachesTheWireEverywhere: the write payload, the read DTOs and
// the query vocabulary all read Field.JSONName, so declaring it once is meant to
// move all of them. A surface that re-rendered the Go name instead would leave
// the caller sending `cpf` and filtering on `documentoNacional`.
func TestJSONNameReachesTheWireEverywhere(t *testing.T) {
	files := goSources(emitAll(t, wireNameEmitModel(t)))
	var offenders []string
	for path, body := range files {
		if !strings.Contains(path, "internal/web") && !strings.Contains(path, "internal/application") {
			continue
		}
		if strings.Contains(body, `"documentoNacional"`) || strings.Contains(body, "json:\"documentoNacional") {
			offenders = append(offenders, path)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("these surfaces still name the field by its Go rendering: %s", strings.Join(offenders, ", "))
	}
	var sawIt bool
	for path, body := range files {
		if strings.Contains(path, "internal/web") && strings.Contains(body, "json:\"cpf") {
			sawIt = true
		}
	}
	if !sawIt {
		t.Error("no wire type carries the declared name — the assertion above proves nothing")
	}
}
