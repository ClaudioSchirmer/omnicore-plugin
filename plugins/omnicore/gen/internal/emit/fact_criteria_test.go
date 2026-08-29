package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A fact's filters are a criteria TREE, and this is the translation held in
// place. Every operator of the spec has exactly one builder in the framework's
// criteria package; a mapping that drifts produces a query that compiles and
// asks something else, which is the one failure a generated probe cannot
// survive — the rule it feeds is an invariant, and a wrong answer lets a write
// through or refuses a legitimate one.
const factCriteriaSpec = `
specVersion: 1
entity: Chamado
plural: Chamados
language: pt-BR
storage:
  kind: flat
  table: chamados
  description: Chamados.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Codigo, type: string, column: codigo, length: 30, livesOn: root, example: "CH-1", description: O código.}
  - {name: Titulo, type: string, column: titulo, length: 120, livesOn: root, example: Impressora, description: O título.}
  - name: Situacao
    type: string
    column: situacao
    length: 20
    livesOn: root
    vo: {kind: enum, ref: SituacaoChamado}
    example: aberto
    description: A situação.
  - {name: Prioridade, type: int, column: prioridade, livesOn: root, example: "2", description: A prioridade.}
  - {name: FilaID, type: id, column: fila_id, livesOn: root, example: 6f0f6f2a-6f9f-4a6a-9f9f-2b7a9c5f21d4, description: A fila.}
  - {name: FechadoEm, type: time, column: fechado_em, livesOn: root, nullable: true, example: "2026-03-01T10:00:00Z", description: Quando fechou.}
valueObjects:
  - name: SituacaoChamado
    kind: enum
    backing: string
    description: A situação do chamado.
    unknownNotification: SituacaoDesconhecidaNotification
    members:
      - {name: Aberto, value: aberto, text: {ptbr: Aberto, eng: Open, esp: Abierto, fra: Ouvert, deu: Offen, ita: Aperto, nld: Open}}
      - {name: Cancelado, value: cancelado, text: {ptbr: Cancelado, eng: Cancelled, esp: Cancelado, fra: Annule, deu: Storniert, ita: Annullato, nld: Geannuleerd}}
notifications:
  - name: SituacaoDesconhecidaNotification
    package: vos
    semantic: validation
    text: {ptbr: Desconhecida., eng: Unknown., esp: Desconocida., fra: Inconnue., deu: Unbekannt., ita: Sconosciuta., nld: Onbekend.}
modes: [display, insert, update, archive, unarchive]
update: {shape: patch}
delete: {root: soft}
read:
  backing: relational
  view: {name: chamados}
  byId: true
surfaces: {rest: true}
authz:
  resource: chamado
  dataAccess: anyone-with-permission
  permissions: {insert: "chamado:escrever", patch: "chamado:escrever", archive: "chamado:arquivar", unarchive: "chamado:arquivar", read: "chamado:ler"}
service:
  required: true
  facts:
    - name: TodoOVocabulario
      kind: count
      description: Uma folha por operador.
      filters:
        - FilaID
        - {field: Codigo, op: ne, as: codigoIgnorado}
        - {field: Prioridade, op: gt, as: acimaDe}
        - {field: Prioridade, op: lt, as: abaixoDe}
        - {field: Titulo, op: contains, as: trecho}
        - {field: Codigo, op: startswith, as: prefixo}
        - {field: Codigo, op: endswith, as: sufixo}
        - {field: FechadoEm, op: isnull}
        - {field: Situacao, op: nin, values: [Cancelado]}
    - name: ConjuntoPeloChamador
      kind: count
      description: O conjunto chega como parâmetro.
      filters:
        - {field: Situacao, op: in}
    - name: Conectivos
      kind: count
      description: Os dois conectivos e a negação.
      filters:
        - any:
            - {field: Codigo, op: eq, as: codigoExato}
            - all:
                - {field: Titulo, op: contains, as: trechoDoTitulo}
                - {field: Prioridade, op: gte, as: prioridadeMinima}
        - not:
            - any:
                - {field: Situacao, op: eq, value: Cancelado}
                - {field: FechadoEm, op: notnull}
`

func factCriteriaModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(factCriteriaSpec), "chamado.omnicore.yaml")
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

// TestEveryOperatorReachesItsBuilder is the mapping, spelled out. It is written
// as one assertion per operator rather than as a golden file so a failure names
// the operator that drifted.
func TestEveryOperatorReachesItsBuilder(t *testing.T) {
	got := fileNamed(t, factCriteriaModel(t), "internal/infra/chamado_service.go")
	for _, want := range []string{
		`criteria.Eq("FilaID", filaID)`,
		`criteria.Ne("Codigo", codigoIgnorado)`,
		`criteria.Gt("Prioridade", acimaDe)`,
		`criteria.Lt("Prioridade", abaixoDe)`,
		`criteria.Contains("Titulo", trecho)`,
		`criteria.StartsWith("Codigo", prefixo)`,
		`criteria.EndsWith("Codigo", sufixo)`,
		`criteria.IsNull("FechadoEm")`,
		`criteria.Nin("Situacao", "cancelado")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the query does not carry %s:\n%s", want, got)
		}
	}
}

// TestAPinnedEnumIsWrittenAsItsSTOREDValue is the half a signature cannot show.
// The spec names the member; the column holds the value; anything else compares
// a name against a column that has never held one.
func TestAPinnedEnumIsWrittenAsItsSTOREDValue(t *testing.T) {
	got := fileNamed(t, factCriteriaModel(t), "internal/infra/chamado_service.go")
	if strings.Contains(got, `"Cancelado"`) {
		t.Errorf("the member's NAME reached the query; the column holds its value:\n%s", got)
	}
	if !strings.Contains(got, `criteria.Eq("Situacao", "cancelado")`) {
		t.Errorf("the pinned member did not resolve to its stored value:\n%s", got)
	}
}

// TestASetParameterIsWidenedOnce keeps the port typed and the DSL fed. The port
// takes a typed slice because that is the question the domain asks; criteria
// takes ...any because a comparison is over values. The widener is per-entity
// because every generated implementation lands in ONE infra package.
func TestASetParameterIsWidenedOnce(t *testing.T) {
	got := fileNamed(t, factCriteriaModel(t), "internal/infra/chamado_service.go")
	// The name says MANY. Left as the field's own — `situacao []string` — the
	// one place a reader looks to find out whether they are asking about one
	// thing or many said "one" while the type said the opposite.
	if !strings.Contains(got, "ConjuntoPeloChamador(situacaoSet []string) int64") {
		t.Errorf("the set did not reach the signature as a slice named for a set:\n%s", got)
	}
	if !strings.Contains(got, `criteria.In("Situacao", chamadoCriteriaSet(situacaoSet)...)`) {
		t.Errorf("the set is not spread into the comparison:\n%s", got)
	}
	if n := strings.Count(got, "func chamadoCriteriaSet["); n != 1 {
		t.Errorf("the widener must be declared exactly once, got %d", n)
	}
}

// TestConnectivesNest is the tree surviving the walk: an OR holding an AND, and
// a NOT over a whole OR. criteria.Not takes ONE expression, so several nodes
// under a `not` are ANDed first — here there is one, and it must not be wrapped
// in a pointless And.
func TestConnectivesNest(t *testing.T) {
	got := fileNamed(t, factCriteriaModel(t), "internal/infra/chamado_service.go")
	for _, want := range []string{
		"criteria.Or(",
		`criteria.Eq("Codigo", codigoExato)`,
		"criteria.And(",
		`criteria.Contains("Titulo", trechoDoTitulo)`,
		`criteria.Gte("Prioridade", prioridadeMinima)`,
		"criteria.Not(",
		`criteria.NotNull("FechadoEm")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the tree lost %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "criteria.Not(\n\t\t\tcriteria.And(") {
		t.Errorf("a single node under `not` was wrapped in an And it does not need:\n%s", got)
	}
}

// TestAFactWithNoSetDoesNotCarryTheWidener keeps the emitted file honest: a
// helper nothing calls is dead code in a file the author may not edit, and go
// vet does not flag an unused package-level function.
func TestAFactWithNoSetDoesNotCarryTheWidener(t *testing.T) {
	src := strings.Replace(factCriteriaSpec,
		"    - name: ConjuntoPeloChamador\n      kind: count\n      description: O conjunto chega como parâmetro.\n      filters:\n        - {field: Situacao, op: in}\n",
		"", 1)
	s, err := spec.Parse([]byte(src), "chamado.omnicore.yaml")
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
	if got := fileNamed(t, m, "internal/infra/chamado_service.go"); strings.Contains(got, "CriteriaSet[") {
		t.Errorf("the widener was emitted for a service that asks no set question:\n%s", got)
	}
}
