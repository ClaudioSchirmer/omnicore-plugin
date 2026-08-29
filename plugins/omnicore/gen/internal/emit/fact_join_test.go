package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A fact narrowed by a column ANOTHER aggregate owns.
//
// The framework has always served this: a root join is in the FROM of every
// query the loader runs, and `Exists` compiles the same traversal `FindAll`
// does. What the generator could not do was NAME the column — so "does an
// active row exist whose campus is labelled this" was written by hand, per
// service, against a store that was ready for it.
//
// The two halves worth proving separately: the criteria must address the JOIN's
// Go field (which is what the framework resolves against, never a column), and
// the join must be resolved BEFORE the service — it was not, and the fact hit
// the panic that guards against exactly this kind of generator inconsistency.

const factJoinSpec = `
specVersion: 1
entity: Aluno
plural: Alunos
language: pt-BR
storage:
  kind: flat
  table: alunos
  description: Alunos.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Ana, description: O nome.}
  - {name: CampusID, type: id, column: campus_id, livesOn: root, example: 1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4, description: O campus.}
modes: [display, insert, update, archive, unarchive]
update: {shape: both}
delete: {root: soft}
joins:
  - to: Campus
    kind: inner
    on: campus_id
    fields:
      - {name: CampusRotulo, column: rotulo, type: string, example: Norte, description: O rótulo do campus.}
      - {name: CampusDonoID, column: dono_id, type: string, example: 1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4, description: O dono do campus.}
service:
  required: true
  facts:
    - name: HomonimoNoMesmoCampus
      kind: exists
      filters: [Nome, CampusRotulo]
      excludeSelf: true
      activeOnly: true
      description: Se outro aluno ativo de mesmo nome está no campus com este rótulo.
    - name: AlunosDoDono
      kind: count
      filters: [CampusDonoID]
      description: Quantos alunos estão em campi deste dono.
read:
  backing: relational
  view: {name: alunos}
  byId: true
surfaces: {rest: true}
authz:
  resource: aluno
  dataAccess: anyone-with-permission
  permissions: {insert: "aluno:escrever", update: "aluno:escrever", patch: "aluno:escrever", archive: "aluno:arquivar", unarchive: "aluno:arquivar", read: "aluno:ler"}
`

func factJoinModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(factJoinSpec), "aluno.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	// No neighbour: Campus is a hand-written aggregate here, which is why every
	// joined field states its own type. That is the harder case — the generator
	// derives nothing and must still reach the column.
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

// TestAJoinedFieldReachesTheQuery. criteria resolves against the GO FIELD the
// join lands on, never against the column on the other side — the joined
// side's spelling never surfaces above infrastructure.
func TestAJoinedFieldReachesTheQuery(t *testing.T) {
	got := fileNamed(t, factJoinModel(t), "internal/infra/aluno_service.go")
	for _, want := range []string{
		`criteria.Eq("CampusRotulo", campusRotulo)`,
		`criteria.Eq("CampusDonoID", campusDonoID)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the query does not narrow by the joined field (%s):\n%s", want, got)
		}
	}
	// And never by the target's own column name, which resolves to nothing on
	// this side and would be a predicate the loader cannot bind.
	if strings.Contains(got, `criteria.Eq("rotulo"`) {
		t.Errorf("the query addressed the TARGET's column instead of the Go field:\n%s", got)
	}
}

// TestAJoinedFieldReachesThePort keeps the two ends in step: a parameter the
// query reads and the port does not declare is a method nobody can call, which
// is the shape a silent drop used to produce.
func TestAJoinedFieldReachesThePort(t *testing.T) {
	got := fileNamed(t, factJoinModel(t), "internal/domain/aluno_service.go")
	want := "HomonimoNoMesmoCampus(nome string, campusRotulo string, selfID domain.ID) bool"
	if !strings.Contains(got, want) {
		t.Errorf("the port does not declare %s:\n%s", want, got)
	}
}

// TestAJoinedIdentityCrossesAsText. A join field carries no domain type — the
// value belongs to another aggregate and arrives read-only — so an identity
// column lands as its canonical text, and the parameter must be a string
// rather than a domain.ID nothing on this side ever validated.
func TestAJoinedIdentityCrossesAsText(t *testing.T) {
	got := fileNamed(t, factJoinModel(t), "internal/domain/aluno_service.go")
	if !strings.Contains(got, "AlunosDoDono(campusDonoID string) int64") {
		t.Errorf("a joined identity must cross as text:\n%s", got)
	}
}
