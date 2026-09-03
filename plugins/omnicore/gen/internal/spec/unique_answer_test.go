package spec

import (
	"fmt"
	"strings"
	"testing"
)

// The two keys that shape a unique field's CONFLICT ANSWER — attachTo (which
// field it names) and echoValue (whether it carries the value that collided) —
// are refused in exactly the cases where the generated code could not keep the
// promise the key makes. Everything else is the author's judgement: which values
// are too sensitive to travel back in a 422 is not something this language marks.
const uniqueAnswerSpecTemplate = `
specVersion: 1
entity: Permission
plural: Permissions
language: en-US
storage:
  kind: flat
  table: permissions
  description: The catalog of enforceable permissions.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - name: Key
    livesOn: root
    vo: {kind: composite, ref: PermissionKey}
    description: The permission itself.
    unique:
      enforce: service-precheck+constraint
      notification: PermissionAlreadyExistsNotification
      scope: active-only
%s
    parts:
      - {part: Resource, column: resource_name, length: 64, example: tenant}
      - {part: Action, column: action_name, length: 64, example: read}
valueObjects:
  - name: PermissionKey
    kind: composite
    written: %s
    description: A resource together with what may be done to it.
    parts:
      - {name: Resource, type: string, description: The thing being protected.}
      - {name: Action, type: string, description: What may be done to it.}
notifications:
  - name: PermissionAlreadyExistsNotification
    semantic: conflict
    description: The pair is taken.
    text: {eng: This permission already exists., ptbr: x, esp: x, fra: x, deu: x, ita: x, nld: x}
modes: [display, insert, update, archive]
update: {shape: patch}
delete: {root: soft}
service:
  required: true
  facts:
    - name: PermissionKeyTaken
      kind: exists
      filters: [Resource, Action]
      excludeSelf: true
      activeOnly: true
      description: Whether another active permission holds this pair.
read:
  backing: relational
  view: {name: permissions}
  byId: true
surfaces: {rest: true}
authz:
  resource: permission
  dataAccess: anyone-with-permission
  permissions: {insert: "permission:insert", patch: "permission:update", archive: "permission:archive", read: "permission:read"}
`

func uniqueAnswerProblems(t *testing.T, uniqueKeys, written string) *Problems {
	t.Helper()
	raw := fmt.Sprintf(uniqueAnswerSpecTemplate, uniqueKeys, written)
	s, err := Parse([]byte(raw), "unique-answer.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	return Validate(s, Options{})
}

// TestCompositeEchoIsRefusedWhenTheGeneratorWritesTheValueObject is the one that
// has to be a BLOCKER rather than a warning.
//
// A composite echoes the value object as a whole — no single part stands for the
// tuple — and a composite THIS generator writes declares no String(): it emits
// the struct and its IsValid and nothing else, deliberately, because a composite
// declaring no Value() is what tells the framework to decompose it into columns.
// So the echo would print a Go struct into an API response, and it would do it
// silently: it compiles, it runs, and a caller discovers `{tenant read}` in a
// 422 body.
func TestCompositeEchoIsRefusedWhenTheGeneratorWritesTheValueObject(t *testing.T) {
	ps := uniqueAnswerProblems(t, "      echoValue: true", "generated")
	if !ps.HasBlockers() {
		t.Fatal("echoValue: true on a generated composite was accepted — the conflict " +
			"would answer with a formatted struct")
	}
	if msg := ps.Error().Error(); !strings.Contains(msg, "String()") {
		t.Errorf("the refusal does not say what is missing, so the author cannot act on it:\n%s", msg)
	}
}

// TestCompositeEchoIsAllowedOnAValueObjectTheAuthorWrites is the other side.
// `written: manual` is the kind whose FILE is the author's, so it is the only
// one that can carry a String() — and the generator emits no file where the
// absence of one could be caught, which is why this warns rather than passing in
// silence.
func TestCompositeEchoIsAllowedOnAValueObjectTheAuthorWrites(t *testing.T) {
	ps := uniqueAnswerProblems(t, "      echoValue: true", "manual")
	if ps.HasBlockers() {
		t.Fatalf("echoValue: true on a hand-written composite was refused:\n%v", ps.Error())
	}
	if !strings.Contains(warningText(ps), "String()") {
		t.Errorf("nothing told the author that String() is now a contract:\n%s", warningText(ps))
	}
}

// TestCompositeUniqueWithoutEchoValueIsUnchanged guards every spec written
// before the key existed. The echo of a composite is opt-in, so an absent key
// must not start meaning anything — least of all a refusal.
func TestCompositeUniqueWithoutEchoValueIsUnchanged(t *testing.T) {
	ps := uniqueAnswerProblems(t, "", "generated")
	if ps.HasBlockers() {
		t.Fatalf("a composite unique that says nothing about the echo was refused:\n%v", ps.Error())
	}
	if strings.Contains(warningText(ps), "String()") {
		t.Errorf("an absent echoValue produced a report about String():\n%s", warningText(ps))
	}
}

// TestUniqueAttachToMustNameAField: a conflict points the caller at something
// they can change. A seat naming nothing points at nothing, and the generated
// notification would carry a field name no response, no schema and no form has.
func TestUniqueAttachToMustNameAField(t *testing.T) {
	ps := uniqueAnswerProblems(t, "      attachTo: Permission", "manual")
	if !ps.HasBlockers() {
		t.Fatal("attachTo naming no field was accepted")
	}
	msg := ps.Error().Error()
	if !strings.Contains(msg, "attachTo") || !strings.Contains(msg, "Permission") {
		t.Errorf("the refusal does not name the key or the value:\n%s", msg)
	}
}

// TestUniqueAttachToAcceptsTheCompositeFieldItself pins the case the key exists
// for: at spec level a composite IS one field, so naming it is legal — including
// naming it explicitly, which is how an author restates the default while
// renaming nothing.
func TestUniqueAttachToAcceptsTheCompositeFieldItself(t *testing.T) {
	if ps := uniqueAnswerProblems(t, "      attachTo: Key", "manual"); ps.HasBlockers() {
		t.Fatalf("attachTo naming the composite field itself was refused:\n%v", ps.Error())
	}
}

// warningText joins the warnings so a test can assert on what the author is
// TOLD, which is the whole point of a warning that refuses nothing.
func warningText(ps *Problems) string {
	var b strings.Builder
	for _, w := range ps.Warnings() {
		fmt.Fprintf(&b, "%s: %s %s\n", w.Where, w.Message, w.Fix)
	}
	return b.String()
}

// A collection entry's uniqueness reaches neither key, and both were listed as
// if it did. The entry is `constraint-only` — a pre-check would query the
// collection's own table and this build writes none — so there is no call site
// that ever held the value, and the binding reports against the COLLECTION's
// segment rather than a field, because what a caller has to know is which entry
// of the array collided.
const childUniqueAnswerTemplate = `
specVersion: 1
entity: Papel
plural: Papeis
language: pt-BR
storage:
  kind: flat
  table: papeis
  description: Papeis.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Admin, description: O nome.}
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
children:
  - name: PapelPermissao
    plural: Permissoes
    table: papel_permissoes
    parentColumn: papel_id
    description: As permissoes do papel.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [PermissaoID]
    softRemove: true
    archivedAt: deleted_at
    fields:
      - name: PermissaoID
        type: id
        column: permissao_id
        example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31
        description: A permissao.
        unique:
          enforce: constraint-only
          notification: PermissaoJaConcedidaNotification
          scope: active-only
%s
notifications:
  - name: PermissaoJaConcedidaNotification
    semantic: conflict
    text: {ptbr: Ja concedida., eng: Already granted., esp: Ya concedida., fra: Deja accordee., deu: Bereits gewaehrt., ita: Gia concessa., nld: Al verleend.}
read:
  backing: relational
  view: {name: papeis}
  byId: true
surfaces: {rest: true}
authz:
  resource: papel
  dataAccess: anyone-with-permission
  permissions: {insert: "papel:escrever", update: "papel:escrever", patch: "papel:escrever", archive: "papel:arquivar", read: "papel:ler"}
`

func childUniqueAnswerProblems(t *testing.T, uniqueKeys string) *Problems {
	t.Helper()
	raw := fmt.Sprintf(childUniqueAnswerTemplate, uniqueKeys)
	s, err := Parse([]byte(raw), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing:\n%v\n\n%s", err, raw)
	}
	return Validate(s, Options{})
}

func TestChildUniqueRefusesTheConflictAnswerKeys(t *testing.T) {
	if ps := childUniqueAnswerProblems(t, ""); ps.HasBlockers() {
		t.Fatalf("the fixture itself does not validate:\n%v", ps.Error())
	}
	for _, tc := range []struct{ name, keys, want string }{
		{"echoValue", "          echoValue: true", "never saw the value"},
		{"attachTo", "          attachTo: PermissaoID", "reported against the collection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := childUniqueAnswerProblems(t, tc.keys)
			if !ps.HasBlockers() {
				t.Fatalf("%s was accepted on a collection entry, where nothing reads it", tc.name)
			}
			if msg := ps.Error().Error(); !strings.Contains(msg, tc.want) {
				t.Errorf("the refusal does not say why nothing reads it:\n%s", msg)
			}
		})
	}
}
