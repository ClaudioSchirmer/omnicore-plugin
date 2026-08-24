package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A `guard: true` rule ends the validation pass where it is declared. The
// framework's StopIfInvalid is itself the condition — it returns without doing
// anything when nothing has been rejected — so the emitted barrier is a bare
// call, with no `if` around it and nothing to return from.
//
// Where it lands is the design: OUTSIDE the rule's own block, at the clause's
// indentation. Pushed inside, it would fire on the first arm that rejected and
// hide the rest of what the same rule found; out here, every rule declared
// above it has already had its say — which is what lets four preconditions all
// be reported with the key on the LAST of them.

const guardSpec = `
specVersion: 1
entity: Pedido
plural: Pedidos
language: pt-BR
storage:
  kind: flat
  table: pedidos
  description: Pedidos.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at}
fields:
  - {name: Cliente, type: string, column: cliente, length: 80, livesOn: root, example: acme, description: O cliente.}
  - {name: Total, type: float64, column: total, livesOn: root, example: "10.5", description: O total.}
  - {name: Apelido, type: string, column: apelido, length: 40, livesOn: root, nullable: true, example: x, description: Um apelido.}
modes: [display, insert, update]
update: {shape: put}
rules:
  list:
    - {id: cliente-required, kind: required, scope: [insertOrUpdate], fields: [Cliente], notification: RequiredFieldNotification}
    - {id: total-range, kind: range, scope: [insertOrUpdate], fields: [Total], min: 0, max: 1000, notification: TotalForaDoIntervaloNotification, guard: true}
    - {id: apelido-length, kind: length, scope: [insertOrUpdate], fields: [Apelido], min: 3, skipWhen: empty, notification: ApelidoCurtoNotification}
read:
  backing: relational
  view: {name: pedidos}
  byId: true
surfaces: {rest: true}
authz:
  resource: pedido
  dataAccess: anyone-with-permission
  permissions: {insert: "pedido:escrever", update: "pedido:escrever", read: "pedido:ler"}
notifications:
  - name: TotalForaDoIntervaloNotification
    semantic: validation
    text: {ptbr: Total fora do intervalo., eng: Total out of range., esp: x, fra: x, deu: x, ita: x, nld: x}
  - name: ApelidoCurtoNotification
    semantic: validation
    text: {ptbr: Apelido curto., eng: Nickname too short., esp: x, fra: x, deu: x, ita: x, nld: x}
`

func guardModel(t *testing.T, src string) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(src), "pedido.omnicore.yaml")
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

func buildRulesOf(t *testing.T, m *ir.Model, path string) string {
	t.Helper()
	src := fileNamed(t, m, path)
	i := strings.Index(src, "func (e *"+m.Entity.Pascal+") BuildRules")
	if i < 0 {
		t.Fatalf("no BuildRules in %s", path)
	}
	return src[i:]
}

func TestGuardEmitsABareBarrierAfterTheRule(t *testing.T) {
	got := buildRulesOf(t, guardModel(t, guardSpec), "internal/domain/pedido.go")

	if !strings.Contains(got, "\t\tr.StopIfInvalid()\n") {
		t.Fatalf("no barrier at the clause's own indentation:\n%s", got)
	}
	// Nothing wraps it: StopIfInvalid is already the condition.
	if strings.Contains(got, "if r.StopIfInvalid") {
		t.Errorf("the barrier was wrapped in an if:\n%s", got)
	}
	// One rule is a guard, so exactly one barrier.
	if n := strings.Count(got, "r.StopIfInvalid()"); n != 1 {
		t.Errorf("emitted %d barriers, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, "// guard (total-range):") {
		t.Errorf("the barrier does not name the rule that asked for it:\n%s", got)
	}
}

// The barrier belongs to the rule that declared it — after that rule's block,
// and before the next rule's. This is what makes it positional.
func TestGuardLandsBetweenTheRightRules(t *testing.T) {
	got := buildRulesOf(t, guardModel(t, guardSpec), "internal/domain/pedido.go")

	cliente := strings.Index(got, `r.AddNotification("Cliente"`)
	total := strings.Index(got, `r.AddNotification("Total"`)
	barrier := strings.Index(got, "r.StopIfInvalid()")
	apelido := strings.Index(got, `r.AddNotification("Apelido"`)

	if cliente < 0 || total < 0 || barrier < 0 || apelido < 0 {
		t.Fatalf("a rule is missing from the emitted body:\n%s", got)
	}
	if !(cliente < total && total < barrier && barrier < apelido) {
		t.Errorf("the barrier is not between total-range and apelido-length:\n%s", got)
	}
}

// A rule that stands down when its field is absent still gets its barrier
// OUTSIDE that gate: the barrier is about the pass, not about whether this
// rule was evaluated.
func TestGuardOnASkippableRuleSitsOutsideTheSkipGate(t *testing.T) {
	src := strings.Replace(guardSpec,
		"fields: [Apelido], min: 3, skipWhen: empty, notification: ApelidoCurtoNotification}",
		"fields: [Apelido], min: 3, skipWhen: empty, notification: ApelidoCurtoNotification, guard: true}", 1)
	got := buildRulesOf(t, guardModel(t, src), "internal/domain/pedido.go")

	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "r.StopIfInvalid()") && !strings.HasPrefix(line, "\t\tr.") {
			t.Errorf("a barrier was emitted inside a block: %q\n%s", line, got)
		}
	}
	if n := strings.Count(got, "r.StopIfInvalid()"); n != 2 {
		t.Errorf("emitted %d barriers, want 2", n)
	}
}

// No guard anywhere means no barrier anywhere — the key is opt-in and every
// existing spec keeps the tree it already had.
func TestNoGuardEmitsNoBarrier(t *testing.T) {
	src := strings.Replace(guardSpec, ", guard: true}", "}", 1)
	got := buildRulesOf(t, guardModel(t, src), "internal/domain/pedido.go")
	if strings.Contains(got, "StopIfInvalid") {
		t.Errorf("a spec with no guard emitted a barrier:\n%s", got)
	}
}

// A collection's rule takes the key too. The entry carries its own Rules, so
// the barrier there ends the entry's own pass: the rest of ITS BuildRules, its
// value objects, and every sibling still queued behind it.
func TestGuardOnAChildEmitsTheBarrierInTheChildsRules(t *testing.T) {
	src := strings.Replace(guardSpec, "\nrules:\n", `
children:
  - name: Item
    plural: Itens
    table: pedido_itens
    description: Itens.
    ownedBy: root
    parentColumn: pedido_id
    editStrategy: atomic-replace
    businessIdentity: [Codigo]
    fields:
      - {name: Codigo, type: string, column: codigo, length: 20, example: a1, description: O código.}
      - {name: Nome, type: string, column: nome, length: 40, example: x, description: O nome.}
    rules:
      list:
        - {id: codigo-required, kind: required, scope: [insertOrUpdate], fields: [Codigo], notification: RequiredFieldNotification, guard: true}
        - {id: nome-required, kind: required, scope: [insertOrUpdate], fields: [Nome], notification: RequiredFieldNotification}
rules:
`, 1)

	got := fileNamed(t, guardModel(t, src), "internal/domain/aggregatevos/item.go")
	if !strings.Contains(got, "\t\tr.StopIfInvalid()\n") {
		t.Fatalf("no barrier in the child's BuildRules:\n%s", got)
	}
	barrier := strings.Index(got, "r.StopIfInvalid()")
	codigo := strings.Index(got, `r.AddNotification("Codigo"`)
	nome := strings.Index(got, `r.AddNotification("Nome"`)
	if codigo < 0 || nome < 0 || barrier < 0 {
		t.Fatalf("a rule is missing from the child's body:\n%s", got)
	}
	if !(codigo < barrier && barrier < nome) {
		t.Errorf("the barrier is not between the child's two rules:\n%s", got)
	}
}
