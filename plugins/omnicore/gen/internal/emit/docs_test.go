package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The docs block is the only place in this language whose audience is the API
// CALLER rather than a developer reading the generated tree, and the only prose
// that reaches the OpenAPI document at all.
//
// The case that motivated it: an entity whose composite value object is exposed
// as two separate wire fields — a resource and an action that are two halves of
// ONE value, rendered as `resource:action`. Every description the spec already
// had reached a migration comment, a Go doc comment or a hook file; none reached
// Swagger, so the two fields read as unrelated strings and the composed form was
// discoverable only by reading the domain code.
//
// The fixture is that shape deliberately, and the prose is MULTI-PARAGRAPH with
// markdown in it, because that is the half a single-line description cannot do
// and the half that breaks first if the escaping is wrong.
const docsSpec = `
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
    parts:
      - {part: Resource, column: resource_name, length: 64, example: tenant}
      - {part: Action, column: action_name, length: 64, example: read}
valueObjects:
  - name: PermissionKey
    kind: composite
    written: manual
    description: A resource together with what may be done to it.
    parts:
      - {name: Resource, type: string, description: The thing being protected.}
      - {name: Action, type: string, description: What may be done to it.}
modes: [display, insert, update, archive]
update: {shape: patch}
delete: {root: soft}
read:
  backing: relational
  view: {name: permissions}
  byId: true
  byParams:
    filters:
      - {field: Resource, ops: [eq]}
    controls: {pagination: true}
surfaces: {rest: true}
authz:
  resource: permission
  dataAccess: anyone-with-permission
  permissions: {insert: "permission:insert", patch: "permission:update", archive: "permission:archive", read: "permission:read"}
docs:
  description: |
    ` + "`resource`" + ` and ` + "`action`" + ` are the two halves of one value: the permission is
    rendered as ` + "`resource:action`" + `.

    ` + "`*`" + ` is accepted as an **entire** half only.
  operations:
    archive: |
      There is no unarchive: a retired permission comes back as a NEW row.
`

func docsModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(docsSpec), "docs.omnicore.yaml")
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

func docsRoutes(t *testing.T) string {
	t.Helper()
	for path, body := range goSources(emitAll(t, docsModel(t))) {
		if strings.HasSuffix(path, "internal/web/permission_routes.go") {
			return body
		}
	}
	t.Fatal("the routes file was not emitted")
	return ""
}

// TestDocsProseReachesEveryOperation is the promise the key makes: prose written
// once is on every endpoint, without being restated per verb.
func TestDocsProseReachesEveryOperation(t *testing.T) {
	routes := docsRoutes(t)
	for _, summary := range []string{
		"Create a permission",
		"Update a permission (partial)",
		"Archive a permission",
		"Get a permission by id",
		"List permissions",
	} {
		block := docBlockFor(t, routes, summary)
		if !strings.Contains(block, "two halves of one value") {
			t.Errorf("the entity-wide prose is missing from %q:\n%s", summary, block)
		}
	}
}

// TestDocsPerOperationProseLandsOnThatOperationOnly is the other half. A key
// that leaked one verb's paragraph onto every endpoint would be worse than no
// key at all: the archive caveat would read as a statement about creating.
func TestDocsPerOperationProseLandsOnThatOperationOnly(t *testing.T) {
	routes := docsRoutes(t)
	const own = "There is no unarchive"

	if block := docBlockFor(t, routes, "Archive a permission"); !strings.Contains(block, own) {
		t.Errorf("the archive prose did not reach the archive operation:\n%s", block)
	}
	for _, summary := range []string{"Create a permission", "List permissions"} {
		if block := docBlockFor(t, routes, summary); strings.Contains(block, own) {
			t.Errorf("the archive prose leaked onto %q:\n%s", summary, block)
		}
	}
}

// TestDocsProseFollowsTheVerbSentence pins the ORDER and the fact that the
// generator's own sentence survives.
//
// The sentence states framework behaviour the author does not own — that PATCH
// cannot set a value back to null — so prose that replaced it would be prose
// that silently stops telling a caller something true. Order matters for the
// same reason it does in the file: the verb's behaviour is the premise, and the
// entity's prose usually refers to it.
func TestDocsProseFollowsTheVerbSentence(t *testing.T) {
	block := docBlockFor(t, docsRoutes(t), "Update a permission (partial)")
	verb := strings.Index(block, "Partial update")
	prose := strings.Index(block, "two halves of one value")
	switch {
	case verb < 0:
		t.Fatalf("the generator's own sentence was replaced by the author's prose:\n%s", block)
	case prose < 0:
		t.Fatalf("the author's prose is missing:\n%s", block)
	case prose < verb:
		t.Errorf("the author's prose came BEFORE the verb sentence:\n%s", block)
	}
}

// TestDocsProseKeepsItsParagraphsAndMarkdown is the escaping half, and the
// reason the prose can be a Go string literal at all.
//
// A struct tag could not carry this: the tag is delimited by backticks, so the
// `code` spans the author writes would not compile, and a double quote would cut
// the value in half. A Go string literal emitted through %q carries both, plus
// the blank line between paragraphs that makes markdown render as two
// paragraphs instead of one run-on.
func TestDocsProseKeepsItsParagraphsAndMarkdown(t *testing.T) {
	block := docBlockFor(t, docsRoutes(t), "Create a permission")
	if !strings.Contains(block, `\n\n`) {
		t.Errorf("the paragraph break was flattened — markdown will render one paragraph:\n%s", block)
	}
	if !strings.Contains(block, "`resource:action`") {
		t.Errorf("the backticked code span did not survive:\n%s", block)
	}
	if !strings.Contains(block, "**entire**") {
		t.Errorf("the bold markdown did not survive:\n%s", block)
	}
}

// TestRoutesWithoutDocsAreUnchanged guards the entity that declares nothing: the
// key is optional, and an absent block must leave the description byte-identical
// to what it was — no trailing blank line, no empty paragraph.
func TestRoutesWithoutDocsAreUnchanged(t *testing.T) {
	m := docsModel(t)
	m.Docs = ir.Docs{}
	var routes string
	for path, body := range goSources(emitAll(t, m)) {
		if strings.HasSuffix(path, "internal/web/permission_routes.go") {
			routes = body
		}
	}
	block := docBlockFor(t, routes, "Create a permission")
	if strings.Contains(block, "+") {
		t.Errorf("an entity with no docs block emitted a multi-part description:\n%s", block)
	}
	if strings.Contains(block, `\n`) {
		t.Errorf("an entity with no docs block emitted a paragraph break:\n%s", block)
	}
}

// docBlockFor returns the Description lines of the fwopenapi.Doc whose Summary
// is the one named — the operation's own block and nothing from its neighbours.
func docBlockFor(t *testing.T, routes, summary string) string {
	t.Helper()
	lines := strings.Split(routes, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "Summary:") || !strings.Contains(line, `"`+summary+`"`) {
			continue
		}
		var out []string
		for _, l := range lines[i+1:] {
			if strings.Contains(l, "Tags:") {
				return strings.Join(out, "\n")
			}
			out = append(out, l)
		}
	}
	t.Fatalf("no operation summarised %q was emitted", summary)
	return ""
}
