package emit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A unique field's CONFLICT ANSWER has two halves an author could not reach:
// which field it names, and whether it carries the value that collided. Both
// were decided inside the generator — the seat hardcoded to the field, the echo
// hardcoded to on for a scalar and to OFF for a composite — so a spec could say
// nothing about either.
//
// The composite half was the defect. "This permission already exists" without
// the pair says only that something collided; the caller sent `tenant:read` and
// the answer cannot name it. The old comment justified the silence by saying an
// echo would hand back a formatted struct — which the IMMUTABLE rule on the same
// field disproved, since it echoed the same value object and the framework
// rendered it through String().
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
  - name: Slug
    type: string
    column: slug
    length: 64
    livesOn: root
    example: tenant-read
    description: A short handle.
    unique:
      enforce: service-precheck+constraint
      notification: SlugAlreadyExistsNotification
%s
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
  - name: SlugAlreadyExistsNotification
    semantic: conflict
    description: The slug is taken.
    text: {eng: This slug already exists., ptbr: x, esp: x, fra: x, deu: x, ita: x, nld: x}
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
    - name: SlugTaken
      kind: exists
      filters: [Slug]
      excludeSelf: true
      activeOnly: true
      description: Whether another active permission holds this slug.
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

// uniqueAnswerSpec builds the fixture with whatever the composite's unique block
// and the scalar's unique block should say, and whoever writes the value object.
func uniqueAnswerSpec(compositeKeys, scalarKeys, written string) string {
	return fmt.Sprintf(uniqueAnswerSpecTemplate, compositeKeys, scalarKeys, written)
}

func uniqueAnswerModel(t *testing.T, compositeKeys, scalarKeys, written string) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(uniqueAnswerSpec(compositeKeys, scalarKeys, written)), "u.omnicore.yaml")
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

func uniqueAnswerSource(t *testing.T, m *ir.Model, suffix string) string {
	t.Helper()
	for path, body := range goSources(emitAll(t, m)) {
		if strings.HasSuffix(path, suffix) {
			return body
		}
	}
	t.Fatalf("no emitted file ends in %q", suffix)
	return ""
}

// TestCompositeUniqueEchoesTheWholeValueWhenAsked is the defect this key exists
// to fix. The pre-check must hand back the value object, which the framework
// renders through String() — the same expression the immutable rule on the same
// field has always emitted.
func TestCompositeUniqueEchoesTheWholeValueWhenAsked(t *testing.T) {
	m := uniqueAnswerModel(t, "      echoValue: true", "", "manual")
	entity := uniqueAnswerSource(t, m, "internal/domain/permission.go")

	want := `r.AddNotification("Key", PermissionAlreadyExistsNotification{}, e.Key)`
	if !strings.Contains(entity, want) {
		t.Errorf("the conflict does not carry the refused value.\nwant a call containing:\n  %s\ngot:\n%s",
			want, uniquePrecheckBlock(entity))
	}
}

// TestCompositeUniqueStaysSilentByDefault is the other half, and it is what
// keeps every composite unique written before this key existed meaning exactly
// what it meant. The echo of a composite is OPT-IN: a value object that renders
// itself is the author's doing, not something the generator can assume.
func TestCompositeUniqueStaysSilentByDefault(t *testing.T) {
	m := uniqueAnswerModel(t, "", "", "manual")
	entity := uniqueAnswerSource(t, m, "internal/domain/permission.go")

	if strings.Contains(entity, "PermissionAlreadyExistsNotification{}, e.Key") {
		t.Errorf("a composite echoed its value with no echoValue asked for:\n%s",
			uniquePrecheckBlock(entity))
	}
	if !strings.Contains(entity, `r.AddNotification("Key", PermissionAlreadyExistsNotification{})`) {
		t.Errorf("the conflict is not raised at all:\n%s", uniquePrecheckBlock(entity))
	}
}

// TestScalarUniqueCanTurnTheEchoOff is the opt-out that did not exist. A unique
// e-mail or document number echoed its value into the 422 body and into every
// log that renders a notification, with no way to say otherwise — while
// rules.list[].echoValue documented that exact judgement as the author's.
func TestScalarUniqueCanTurnTheEchoOff(t *testing.T) {
	// The default first, so the opt-out below is proven to have TURNED SOMETHING
	// OFF rather than to have matched a call that was never emitted.
	on := uniqueAnswerSource(t,
		uniqueAnswerModel(t, "", "", "manual"), "internal/domain/permission.go")
	if !strings.Contains(on, `r.AddNotification("Slug", SlugAlreadyExistsNotification{}, e.Slug)`) {
		t.Fatalf("a scalar unique does not echo by default:\n%s", uniquePrecheckBlock(on))
	}

	off := uniqueAnswerSource(t,
		uniqueAnswerModel(t, "", "      echoValue: false", "manual"), "internal/domain/permission.go")
	if strings.Contains(off, "SlugAlreadyExistsNotification{}, e.Slug") {
		t.Errorf("echoValue: false was ignored — the value still travels back:\n%s",
			uniquePrecheckBlock(off))
	}
	if !strings.Contains(off, `r.AddNotification("Slug", SlugAlreadyExistsNotification{})`) {
		t.Errorf("turning the echo off dropped the conflict itself:\n%s", uniquePrecheckBlock(off))
	}
}

// TestUniqueAttachToGovernsBothHalves pins that the seat reaches the pre-check
// AND the constraint binding.
//
// They are two roads to one conflict — the domain asks first, the database is
// the backstop for the race — and a caller who saw one field named by the
// pre-check and another by the constraint would be looking at two places for one
// problem. The SPELLING differs on purpose: the binding reports the wire name,
// the pre-check the entity's.
func TestUniqueAttachToGovernsBothHalves(t *testing.T) {
	m := uniqueAnswerModel(t, "      attachTo: Slug", "", "manual")

	entity := uniqueAnswerSource(t, m, "internal/domain/permission.go")
	if !strings.Contains(entity, `r.AddNotification("Slug", PermissionAlreadyExistsNotification{})`) {
		t.Errorf("attachTo did not reach the pre-check:\n%s", uniquePrecheckBlock(entity))
	}
	if strings.Contains(entity, `r.AddNotification("Key", PermissionAlreadyExistsNotification{})`) {
		t.Errorf("the pre-check still reports against the default seat:\n%s", uniquePrecheckBlock(entity))
	}

	repo := uniqueAnswerSource(t, m, "internal/infra/permission_repository.go")
	if !strings.Contains(repo, `"slug"`) {
		t.Errorf("attachTo did not reach the constraint binding:\n%s", repo)
	}
}

// uniquePrecheckBlock cuts the pre-check out of the entity file so a failure
// prints the rule rather than the whole aggregate.
func uniquePrecheckBlock(entity string) string {
	const marker = "// The database unique index is the backstop"
	i := strings.Index(entity, marker)
	if i < 0 {
		return entity
	}
	rest := entity[i:]
	if j := strings.Index(rest, "\n\t})"); j > 0 {
		return rest[:j]
	}
	return rest
}
