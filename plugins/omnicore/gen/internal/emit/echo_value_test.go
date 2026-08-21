package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// `echoValue` was opt-in, and almost nothing opted in — so a 422 stated the
// rule and never what was refused. "At most 4 guardians" without "you sent 6",
// "the key is taken" without which key. The framework has carried the value as
// NotificationMessage.FieldValue since the beginning; leaving it out was the
// generator's omission.
//
// It is the default now, and the fixture below is the whole surface of that
// decision: a bound the caller crossed, a bound they crossed on a collection, a
// value that came twice, a rule that opts out, and the two refusals that echo
// nothing because there is nothing useful to echo.
const echoValueSpec = `
specVersion: 1
entity: Cardapio
plural: Cardapios
language: pt-BR
storage:
  kind: flat
  table: cardapios
  description: Cardápios.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Executivo, description: O nome.}
  - {name: Porcoes, type: int, column: porcoes, livesOn: root, example: "4", description: Quantas porções rende.}
  - {name: Senha, type: string, column: senha, length: 60, livesOn: root, example: s3gr3d0, description: A senha de edição.}
children:
  - name: CardapioItem
    plural: Itens
    table: cardapio_itens
    parentColumn: cardapio_id
    description: Os pratos do cardápio.
    ownedBy: root
    editStrategy: per-child
    operations: [add, remove]
    businessIdentity: [PratoID]
    duplicateNotification: PratoJaNoCardapioNotification
    softRemove: true
    archivedAt: deleted_at
    fields:
      - {name: PratoID, type: id, column: prato_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: O prato.}
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
rules:
  list:
    - {id: nome-obrigatorio, kind: required, scope: [insertOrUpdate], fields: [Nome], notification: RequiredFieldNotification}
    - id: porcoes-validas
      kind: range
      scope: [insertOrUpdate]
      fields: [Porcoes]
      min: 1
      max: 12
      notification: PorcoesInvalidasNotification
    - id: senha-longa-o-bastante
      kind: length
      scope: [insertOrUpdate]
      fields: [Senha]
      min: 8
      echoValue: false
      notification: SenhaCurtaNotification
    - id: teto-de-itens
      kind: groupCap
      scope: [insertOrUpdate]
      fields: [CardapioItem]
      cap: 30
      description: Um teto sobre o TAMANHO da coleção.
      notification: ItensDemaisNotification
    - id: prato-repetido
      kind: childDuplicate
      scope: [insertOrUpdate]
      fields: [CardapioItem]
      notification: PratoJaNoCardapioNotification
notifications:
  - name: PorcoesInvalidasNotification
    semantic: validation
    tvars: [min, max]
    description: Número de porções fora da faixa.
    text:
      ptbr: As porções devem ficar entre {min} e {max}.
      eng: Servings must be between {min} and {max}.
      esp: Las porciones deben estar entre {min} y {max}.
      fra: Les portions doivent être entre {min} et {max}.
      deu: Die Portionen müssen zwischen {min} und {max} liegen.
      ita: Le porzioni devono essere tra {min} e {max}.
      nld: De porties moeten tussen {min} en {max} liggen.
  - name: SenhaCurtaNotification
    semantic: validation
    tvars: [min]
    description: Senha abaixo do comprimento mínimo.
    text:
      ptbr: A senha precisa de ao menos {min} caracteres.
      eng: The password needs at least {min} characters.
      esp: La contraseña necesita al menos {min} caracteres.
      fra: Le mot de passe exige au moins {min} caractères.
      deu: Das Passwort braucht mindestens {min} Zeichen.
      ita: La password richiede almeno {min} caratteri.
      nld: Het wachtwoord vereist minstens {min} tekens.
  - name: ItensDemaisNotification
    semantic: validation
    tvars: [max]
    description: Cardápio com pratos demais.
    text:
      ptbr: Um cardápio comporta no máximo {max} pratos.
      eng: A menu holds at most {max} dishes.
      esp: Un menú admite como máximo {max} platos.
      fra: Un menu contient au maximum {max} plats.
      deu: Eine Karte fasst höchstens {max} Gerichte.
      ita: Un menu contiene al massimo {max} piatti.
      nld: Een menu bevat maximaal {max} gerechten.
  - name: PratoJaNoCardapioNotification
    package: aggregatevos
    semantic: conflict
    description: O prato já consta no cardápio.
    text:
      ptbr: O prato já consta no cardápio.
      eng: The dish is already on the menu.
      esp: El plato ya consta en el menú.
      fra: Le plat figure déjà au menu.
      deu: Das Gericht steht bereits auf der Karte.
      ita: Il piatto è già nel menu.
      nld: Het gerecht staat al op het menu.
read:
  backing: relational
  view: {name: cardapios, version: 1}
  byId: true
surfaces: {rest: true}
authz:
  resource: cardapio
  dataAccess: anyone-with-permission
  permissions: {insert: "cardapio:escrever", update: "cardapio:escrever", patch: "cardapio:escrever", archive: "cardapio:arquivar", read: "cardapio:ler"}
`

func echoValueModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(echoValueSpec), "cardapio.omnicore.yaml")
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

// A rule that declares no opinion echoes. That is the change: the key used to
// have to be written, and specs did not write it.
func TestRulesEchoTheRejectedValueByDefault(t *testing.T) {
	m := echoValueModel(t)
	got := fileNamed(t, m, "internal/domain/cardapio.go")
	if !strings.Contains(got, `PorcoesInvalidasNotification{Min: "1", Max: "12"}, e.Porcoes`) {
		t.Errorf("a range refusal states the bound and not the value that crossed it:\n%s", got)
	}
}

// And a rule that says otherwise is obeyed — the reason the key became a
// pointer rather than a flipped bool.
func TestEchoValueFalseIsHonoured(t *testing.T) {
	m := echoValueModel(t)
	got := fileNamed(t, m, "internal/domain/cardapio.go")
	if strings.Contains(got, "SenhaCurtaNotification{Min: \"8\"}, e.Senha") {
		t.Errorf("echoValue: false still sent the value back — and this one is a secret:\n%s", got)
	}
	if !strings.Contains(got, `SenhaCurtaNotification{Min: "8"})`) {
		t.Errorf("the opted-out rule lost its notification entirely:\n%s", got)
	}
}

// The refusals whose useful value is not a field of the entity: the count that
// broke the cap, and the entry that came twice.
func TestCollectionRefusalsEchoWhatBrokeThem(t *testing.T) {
	m := echoValueModel(t)
	got := fileNamed(t, m, "internal/domain/cardapio.go")
	if !strings.Contains(got, `ItensDemaisNotification{Max: "30"}, len(items)`) {
		t.Errorf("the cap says the limit and not how far past it the write is:\n%s", got)
	}
	if !strings.Contains(got, `PratoJaNoCardapioNotification{}, items[i].PratoID`) {
		t.Errorf("the duplicate refusal does not name which entry came twice:\n%s", got)
	}
	// The per-entry add door is a different raise site with the same question.
	if !strings.Contains(got, `PratoJaNoCardapioNotification{}, item.PratoID`) {
		t.Errorf("adding a colliding entry does not name the value that collided:\n%s", got)
	}
}

// The two that stay silent, on purpose. A `required` refusal has nothing to
// echo — the value is absent, which is the whole complaint — and echoing an
// empty string reads as though something was sent.
func TestRequiredEchoesNothing(t *testing.T) {
	m := echoValueModel(t)
	got := fileNamed(t, m, "internal/domain/cardapio.go")
	if !strings.Contains(got, `r.AddNotification("Nome", domain.RequiredFieldNotification{})`) {
		t.Errorf("a required refusal echoes a value it does not have:\n%s", got)
	}
}
