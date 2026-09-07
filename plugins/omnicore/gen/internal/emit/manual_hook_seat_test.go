package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A hook file is written ONCE and then belongs to the author, so what it shows
// is what the next hand-written rule will look like. Two things about it are
// therefore not cosmetic: the receiver it declares, and the emission its
// guidance spells.
const manualHookSpec = `
specVersion: 1
entity: Pedido
plural: Pedidos
language: pt-BR
storage:
  kind: flat
  table: pedidos
  description: Pedidos.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: archived_at}
fields:
  - {name: Cliente, type: string, column: cliente, length: 120, livesOn: root, example: Ana, description: O cliente.}
  - {name: Total, type: float64, column: total, livesOn: root, example: 10.5, description: O total.}
modes: [display, insert, update, archive]
update: {shape: both}
removal: {root: archive}
notifications:
  - name: TotalForaDaFaixaNotification
    semantic: validation
    text: {ptbr: Total fora da faixa., eng: Total out of range., esp: x, fra: x, deu: x, ita: x, nld: x}
  - name: PedidoIncompletoNotification
    semantic: validation
    text: {ptbr: Pedido incompleto., eng: Order incomplete., esp: x, fra: x, deu: x, ita: x, nld: x}
  - name: ItemInvalidoNotification
    semantic: validation
    text: {ptbr: Item inválido., eng: Invalid item., esp: x, fra: x, deu: x, ita: x, nld: x}
rules:
  manual:
    - id: total-da-faixa
      description: O total tem de caber na faixa negociada com o cliente.
      scope: [insertOrUpdate]
      notification: TotalForaDaFaixaNotification
      attachTo: Total
    - id: pedido-completo
      description: Um pedido sem itens não pode ser confirmado.
      scope: [insertOrUpdate]
      notification: PedidoIncompletoNotification
      attachTo: Itens
children:
  - name: Item
    plural: Itens
    table: pedido_itens
    parentColumn: pedido_id
    description: Itens.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [Descricao]
    fields:
      - {name: Descricao, type: string, column: descricao, length: 80, example: Caneta, description: A descrição.}
    rules:
      manual:
        - id: item-valido
          description: A descrição tem de casar com o catálogo vigente.
          scope: [insertOrUpdate]
          notification: ItemInvalidoNotification
          attachTo: Descricao
read:
  backing: relational
  view: {name: pedidos}
  byId: true
surfaces: {rest: true}
authz:
  resource: pedido
  dataAccess: anyone-with-permission
  permissions: {insert: "pedido:escrever", update: "pedido:escrever", patch: "pedido:escrever", archive: "pedido:arquivar", read: "pedido:ler"}
`

func manualHookModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(manualHookSpec), "pedido.omnicore.yaml")
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

// TestChildHookTakesThePointerReceiver is a runtime failure the compiler cannot
// see, which is the whole reason it is pinned here.
//
// The generated BuildRules is declared on *Item and calls c.customRules(...); a
// value receiver satisfies that call by taking a COPY. The framework binds the
// Rules to the addressable copy IT materialized, so every
// r.AddNotification(&c.Field, …) the author writes in the hook then references
// a field of the wrong allocation and panics at the first validation — with a
// message about a reference taken from a copy, in a file that has no copy in
// sight.
func TestChildHookTakesThePointerReceiver(t *testing.T) {
	hook := uniqueAnswerSource(t, manualHookModel(t),
		"internal/domain/aggregatevos/item_rules_manual.go")
	if !strings.Contains(hook, "func (c *Item) customRules(") {
		t.Errorf("the child hook does not take the pointer receiver its BuildRules has:\n%s", hook)
	}
}

// TestManualHookShowsTheSeatTheAttachmentHas pins the guidance itself.
//
// A manual rule's attachTo is not checked against the field list — it is free
// text, because the residue is exactly where the shape is not the generator's
// to know. So the seat has to be decided per rule: a name that IS a field
// resolves by reference, and anything else is what the named seat exists for.
// Showing one spelling for both would teach the exception as the default, or
// hand the author a &e.Itens that does not compile.
func TestManualHookShowsTheSeatTheAttachmentHas(t *testing.T) {
	root := uniqueAnswerSource(t, manualHookModel(t), "internal/domain/pedido_rules_manual.go")

	if !strings.Contains(root, "r.AddNotification(&e.Total, TotalForaDaFaixaNotification{}, false)") {
		t.Errorf("an attachment that names a field is not shown on the field reference:\n%s", root)
	}
	if !strings.Contains(root, `r.AddNotificationNamed("Itens", PedidoIncompletoNotification{})`) {
		t.Errorf("an attachment that names no field is not shown on the named seat:\n%s", root)
	}
	if strings.Contains(root, "&e.Itens") {
		t.Errorf("the guidance offers a reference to something that is not a field:\n%s", root)
	}

	child := uniqueAnswerSource(t, manualHookModel(t),
		"internal/domain/aggregatevos/item_rules_manual.go")
	if !strings.Contains(child, "r.AddNotification(&c.Descricao, ItemInvalidoNotification{}, false)") {
		t.Errorf("the child hook's guidance does not address the entry it validates:\n%s", child)
	}
}
