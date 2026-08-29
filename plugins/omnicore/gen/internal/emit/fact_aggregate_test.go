package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The shapes a fact's ANSWER can take, and the query behind each of them. The
// runtime lanes of the matrix prove these run; these prove exactly what is
// written, so a drift names the line rather than a wrong number.
const factAggregateSpec = `
specVersion: 1
entity: Atendimento
plural: Atendimentos
language: pt-BR
storage:
  kind: flat
  table: atendimentos
  description: Atendimentos.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Codigo, type: string, column: codigo, length: 30, livesOn: root, example: "AT-1", description: O codigo.}
  - {name: Setor, type: string, column: setor, length: 20, livesOn: root, example: suporte, description: O setor.}
  - {name: Duracao, type: int, column: duracao, livesOn: root, example: "30", description: A duracao.}
  - {name: Nota, type: float64, column: nota, livesOn: root, nullable: true, example: "8.5", description: "A nota, quando houver."}
modes: [display, insert, update, archive, unarchive]
update: {shape: patch}
delete: {root: soft}
read:
  backing: relational
  view: {name: atendimentos}
  byId: true
surfaces: {rest: true}
authz:
  resource: atendimento
  dataAccess: anyone-with-permission
  permissions: {insert: "atendimento:escrever", patch: "atendimento:escrever", archive: "atendimento:arquivar", unarchive: "atendimento:arquivar", read: "atendimento:ler"}
service:
  required: true
  facts:
    - name: Carga
      aggregates:
        - {kind: count, as: Quantos}
        - {kind: sum, field: Duracao, as: Minutos}
        - {kind: avg, field: Nota, as: NotaMedia}
      filters:
        - Setor
      description: Varios numeros, sem grupo.
      activeOnly: true
    - name: CargaPorSetor
      aggregates:
        - {kind: count, as: Quantos}
        - {kind: min, field: Duracao, as: MaisCurto}
        - {kind: avg, field: Nota, as: NotaMedia}
      groupBy: [Setor]
      description: Os mesmos numeros, por setor.
      activeOnly: true
    - name: TudoQueExiste
      kind: count
      description: Sem filtro nenhum.
    - name: Desde
      kind: count
      filters:
        - {field: CreatedAt, op: gte, as: desde}
        - {field: DeletedAt, op: notnull}
      description: Arquivados a partir de um instante.
`

func factAggregateModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(factAggregateSpec), "atendimento.omnicore.yaml")
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

// TestSeveralAggregatesAreOneQuery is the whole point of the plural form: one
// call carrying every spec, not one call per number.
func TestSeveralAggregatesAreOneQuery(t *testing.T) {
	got := fileNamed(t, factAggregateModel(t), "internal/infra/atendimento_service.go")
	for _, want := range []string{
		"aggQuantos := read.Count()",
		`aggMinutos := read.SumInt("Duracao")`,
		`aggNotaMedia := read.Avg("Nota")`,
		"Aggregate(s.queryContext(), q, aggQuantos, aggMinutos, aggNotaMedia)",
		"AggregateBy(s.queryContext(), q, by, aggQuantos, aggMaisCurto, aggNotaMedia)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the body does not carry %s:\n%s", want, got)
		}
	}
	// Counted INSIDE the one body, because the file holds other facts that
	// also aggregate: what is being asserted is that three numbers cost one
	// query, not that the service issues one query in total.
	body := got[strings.Index(got, "func (s *AtendimentoServiceImpl) Carga("):]
	body = body[:strings.Index(body, "\n}")]
	if n := strings.Count(body, "Loader.Aggregate(s.queryContext()"); n != 1 {
		t.Errorf("three numbers must cost ONE query, got %d:\n%s", n, body)
	}
}

// TestFoundRidesOnlyWhereZeroCouldLie is the correctness of the answer's shape,
// and it differs between the two forms for a reason worth pinning.
func TestFoundRidesOnlyWhereZeroCouldLie(t *testing.T) {
	got := fileNamed(t, factAggregateModel(t), "internal/domain/atendimento_service.go")

	// Ungrouped: the matching set can be empty, so avg carries Found. count and
	// sum never do — zero IS the answer for both.
	for _, want := range []string{"NotaMediaFound bool"} {
		if !strings.Contains(got, want) {
			t.Errorf("the ungrouped result is missing %s:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"QuantosFound", "MinutosFound", "MaisCurtoFound"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%s should not exist — that number cannot come back null:\n%s", unwanted, got)
		}
	}
	// Grouped: a group EXISTS because a row matched, so only a NULLABLE column
	// can still leave it with nothing to measure. Duracao is not nullable.
	if !strings.Contains(got, "type AtendimentoCargaPorSetorGroup struct") {
		t.Fatalf("no group type was emitted:\n%s", got)
	}
	grp := got[strings.Index(got, "type AtendimentoCargaPorSetorGroup struct"):]
	grp = grp[:strings.Index(grp, "\n}")]
	if !strings.Contains(grp, "NotaMediaFound bool") {
		t.Errorf("the grouped avg over a NULLABLE column must carry Found:\n%s", grp)
	}
	if strings.Contains(grp, "MaisCurtoFound") {
		t.Errorf("a min over a NON-nullable column cannot be null in a group that exists:\n%s", grp)
	}
}

// TestAFactWithNoFiltersCarriesNoPredicate is a defect this release fixes, and
// it is invisible to every lane that stops at compiling: criteria.And() with no
// operands is refused by the framework at run time, so the fact panicked the
// first time a rule asked it.
func TestAFactWithNoFiltersCarriesNoPredicate(t *testing.T) {
	got := fileNamed(t, factAggregateModel(t), "internal/infra/atendimento_service.go")
	body := got[strings.Index(got, "func (s *AtendimentoServiceImpl) TudoQueExiste("):]
	body = body[:strings.Index(body, "\n}")]
	if !strings.Contains(body, "criteria.Where(nil)") {
		t.Errorf("a fact with no filters must carry no predicate:\n%s", body)
	}
	if strings.Contains(body, "criteria.And(conds...)") {
		t.Errorf("an empty conjunction is not a predicate — the framework refuses it:\n%s", body)
	}
}

// TestStampedColumnsReachTheQuery keeps the three framework-owned columns
// addressable, and the port importable: their parameter is a time.Time, and the
// emitter never used to add the import that names it.
func TestStampedColumnsReachTheQuery(t *testing.T) {
	m := factAggregateModel(t)
	impl := fileNamed(t, m, "internal/infra/atendimento_service.go")
	for _, want := range []string{
		`criteria.Gte("CreatedAt", desde)`,
		`criteria.NotNull("DeletedAt")`,
	} {
		if !strings.Contains(impl, want) {
			t.Errorf("the query does not carry %s:\n%s", want, impl)
		}
	}
	port := fileNamed(t, m, "internal/domain/atendimento_service.go")
	if !strings.Contains(port, "Desde(desde time.Time) int64") {
		t.Errorf("the stamped column did not reach the signature as an instant:\n%s", port)
	}
	if !strings.Contains(port, `"time"`) {
		t.Errorf("the port names time.Time and imports nothing for it:\n%s", port)
	}
}
