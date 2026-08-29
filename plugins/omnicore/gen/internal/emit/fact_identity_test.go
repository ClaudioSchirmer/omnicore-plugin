package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The aggregate id reaching a fact, in both halves it has to reach: the SIGNATURE
// a hand-written body receives it through, and the CRITERIA a generated one
// binds it into.
//
// The signature is the half that motivated it. `kind: manual` hands the author
// the body and not the parameter list — that comes from `filters` — so a manual
// fact needing the id had nothing to receive it by, and the id was re-derived
// inside the body from a natural key: a join whose only job was translating a
// value the caller had already.
const factIdentitySpec = `
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
    - name: EstaEmUso
      kind: manual
      returns: bool
      description: Se alguma outra tabela ainda aponta para esta linha.
      filters: [ID]
    - name: AindaVivo
      kind: exists
      description: Se esta linha ainda não foi arquivada.
      filters: [ID]
      scope: active
    - name: OutroVivoEntreEstes
      kind: exists
      description: Se algum destes ids, que não este, ainda está vivo.
      filters:
        - {field: ID, op: in}
      excludeSelf: true
      scope: active
`

func factIdentityModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(factIdentitySpec), "chamado.omnicore.yaml")
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

// TestAManualFactReceivesTheAggregateID is the ticket, in one assertion. The
// stub is what the author writes into, so the id has to be a parameter of it —
// and typed, because domain.ID is what every caller already holds and what
// every excludeSelf signature this generator writes has always taken.
func TestAManualFactReceivesTheAggregateID(t *testing.T) {
	got := fileNamed(t, factIdentityModel(t), "internal/infra/chamado_service_manual.go")
	if !strings.Contains(got, "EstaEmUso(id domain.ID) bool") {
		t.Fatalf("the manual fact's body has no id to work with:\n%s", got)
	}
}

// TestTheAggregateIDBindsAsCriteria is the generated half. The field name is
// the framework's fixed logical one, unqualified and uncased — the same slot
// criteria.ByID and the exclude-self gate write against — so the loader types
// it as an identity and the probe binds in the dialect's native id form.
func TestTheAggregateIDBindsAsCriteria(t *testing.T) {
	got := fileNamed(t, factIdentityModel(t), "internal/infra/chamado_service.go")
	if !strings.Contains(got, `criteria.Eq("ID", id)`) {
		t.Errorf("the id did not reach the query as the framework's own field name:\n%s", got)
	}
	if !strings.Contains(got, "AindaVivo(id domain.ID) bool") {
		t.Errorf("the generated fact does not take the id it filters by:\n%s", got)
	}
}

// TestTheIDAndExcludeSelfTravelSeparately holds the pair apart. They are two
// questions — WHICH rows, and which row to leave out — and a signature carrying
// both must name them for what they are; folding one into the other is exactly
// the confusion that made excludeSelf get abused as a way to smuggle the id in.
func TestTheIDAndExcludeSelfTravelSeparately(t *testing.T) {
	got := fileNamed(t, factIdentityModel(t), "internal/infra/chamado_service.go")
	if !strings.Contains(got, "OutroVivoEntreEstes(idSet []domain.ID, selfID domain.ID) bool") {
		t.Fatalf("the two ids did not reach the signature under their own names:\n%s", got)
	}
	for _, want := range []string{
		`criteria.In("ID", chamadoCriteriaSet(idSet)...)`,
		`criteria.Ne("ID", selfID)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the query does not carry %s:\n%s", want, got)
		}
	}
}
