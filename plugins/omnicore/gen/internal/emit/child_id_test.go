package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A collection entry whose field is an id, and a fact whose parameter is one.
// Both used to emit a file that named domain.ID with no import block behind it:
// the spec was green, the generation succeeded, and `go build` failed three
// steps later on `undefined: domain`. The tree that ships is the contract, so
// the assertion is about the import, not about the field.
const childIDSpec = `
specVersion: 1
entity: Papel
plural: Papeis
language: pt-BR
storage:
  kind: flat
  table: papeis
  description: Papéis.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Admin, description: O nome.}
  - {name: DonoID, type: id, column: dono_id, livesOn: root, example: 9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3, description: O dono.}
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
children:
  - name: PapelPermissao
    plural: Permissoes
    table: papel_permissoes
    parentColumn: papel_id
    description: As permissões do papel.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [PermissaoID]
    fields:
      - {name: PermissaoID, type: id, column: permissao_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: A permissão.}
service:
  required: true
  facts:
    - name: DonoIndisponivel
      kind: manual
      returns: bool
      filters: [DonoID]
      description: Se o dono não existe ou foi arquivado. Vive em outra tabela.
read:
  backing: relational
  view: {name: papeis, version: 1}
  byId: true
surfaces: {rest: true}
authz:
  resource: papel
  dataAccess: anyone-with-permission
  permissions: {insert: "papel:escrever", update: "papel:escrever", patch: "papel:escrever", archive: "papel:arquivar", read: "papel:ler"}
`

func childIDModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(childIDSpec), "papel.omnicore.yaml")
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

// fileNamed finds one emitted file by the tail of its path, so a test can assert
// about the tree the way a reader finds it.
func fileNamed(t *testing.T, m *ir.Model, suffix string) string {
	t.Helper()
	for _, f := range emitAll(t, m) {
		if strings.HasSuffix(f.Path, suffix) {
			return string(f.Content)
		}
	}
	t.Fatalf("no emitted file ends in %q", suffix)
	return ""
}

func TestChildInputDTOImportsTheDomainItNames(t *testing.T) {
	m := childIDModel(t)
	got := fileNamed(t, m, "internal/application/dtos/papel_permissao_input.go")
	if !strings.Contains(got, "PermissaoID domain.ID") {
		t.Fatalf("the entry field is not typed as an id:\n%s", got)
	}
	if !strings.Contains(got, `"github.com/ClaudioSchirmer/omnicore/domain"`) {
		t.Errorf("the DTO names domain.ID and does not import domain:\n%s", got)
	}
}

func TestChildInputTestImportsTheDomainItNames(t *testing.T) {
	m := childIDModel(t)
	got := fileNamed(t, m, "internal/application/dtos/papel_dtos_test.go")
	if !strings.Contains(got, "domain.NewID(") {
		t.Fatalf("the sample does not build an id:\n%s", got)
	}
	if !strings.Contains(got, `"github.com/ClaudioSchirmer/omnicore/domain"`) {
		t.Errorf("the test names domain.NewID and does not import domain:\n%s", got)
	}
}

// The service hook is the author's file, but what is handed over still has to
// compile: a first run that does not reads as a broken generation rather than
// as a TODO.
func TestServiceHookImportsTheDomainItNames(t *testing.T) {
	m := childIDModel(t)
	got := fileNamed(t, m, "internal/infra/papel_service_manual.go")
	if !strings.Contains(got, "domain.ID") {
		t.Fatalf("the stub takes no id, so this fixture proves nothing:\n%s", got)
	}
	if !strings.Contains(got, `import "github.com/ClaudioSchirmer/omnicore/domain"`) {
		t.Errorf("the stub names domain.ID and has no import block:\n%s", got)
	}
}
