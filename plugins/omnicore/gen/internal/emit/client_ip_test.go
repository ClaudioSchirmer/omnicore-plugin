package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

const clientIPSpec = `specVersion: 1
entity: Acesso
plural: Acessos
language: pt-BR
storage:
  kind: flat
  table: acessos
  description: Acessos.
  managed: {revision: revision}
fields:
  - {name: Recurso, type: string, column: recurso, length: 120, livesOn: root, example: /rel, description: O recurso.}
  - name: DonoId
    type: string
    column: dono_id
    length: 64
    livesOn: root
    assignedFrom: identity-subject
    example: usr-1
    description: Quem registrou.
  - name: OrigemRede
    type: string
    column: origem_rede
    length: 45
    livesOn: root
    assignedFrom: client-ip
    example: 203.0.113.7
    description: De onde veio.
  - name: OrigemProxy
    type: string
    column: origem_proxy
    length: 45
    livesOn: root
    nullable: true
    assignedFrom: client-ip
    example: 198.51.100.4
    description: A mesma origem, anulável.
modes: [display, insert, update]
update: {shape: put}
read:
  backing: relational
  view: {name: acessos}
  byId: true
surfaces: {rest: true}
authz:
  resource: acesso
  dataAccess: anyone-with-permission
  permissions: {insert: "acesso:escrever", update: "acesso:escrever", read: "acesso:ler"}
`

func clientIPModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(clientIPSpec), "acesso.omnicore.yaml")
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

// The origin is NOT in the token, so the assignment must not be inside the
// identity check.
//
// This is the whole reason client-ip is a source of its own rather than a third
// identity one. The framework resolves the address in its HTTP middleware and
// hands it over on the AppContext, so it exists for an anonymous route and on a
// bench with authentication switched off — and reading it inside
// `if id := ctx.Identity(); id != nil` would drop it on exactly those requests,
// silently, leaving a column that is populated in production and empty in every
// test.
func TestTheOriginIsReadOutsideTheIdentityCheck(t *testing.T) {
	got := fileNamed(t, clientIPModel(t), "internal/application/commands/insert_acesso_command.go")

	ip := strings.Index(got, "e.OrigemRede = ctx.ClientIP()")
	if ip < 0 {
		t.Fatalf("the origin never reaches the entity:\n%s", got)
	}
	guard := strings.Index(got, "if id := ctx.Identity(); id != nil {")
	if guard < 0 {
		t.Fatalf("the fixture lost its identity block, so this test proves nothing:\n%s", got)
	}
	if ip > guard {
		t.Errorf("the origin is assigned after the identity check opens, so an "+
			"anonymous caller's address is dropped:\n%s", got)
	}
}

// The two shapes of "no inbound request". ClientIP() answers "" off the request
// path, and the column decides how that is recorded: a nullable field stays
// NULL and is left untouched, a plain one takes the empty string.
func TestTheAbsentOriginTakesTheShapeTheColumnDeclares(t *testing.T) {
	got := fileNamed(t, clientIPModel(t), "internal/application/commands/insert_acesso_command.go")

	if !strings.Contains(got, "e.OrigemRede = ctx.ClientIP()") {
		t.Errorf("a non-nullable origin must be assigned unconditionally — "+
			"the empty string IS the record of a write off the request path:\n%s", got)
	}
	for _, want := range []string{
		"if ip := ctx.ClientIP(); ip != \"\" {",
		"e.OrigemProxy = &ip",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("a nullable origin must stay NULL rather than hold an empty "+
				"string, missing %q:\n%s", want, got)
		}
	}
}

// Only the insert writes it: the row records where it CAME FROM, not where the
// last edit came from — the same rule the identity sources follow, and for the
// same reason. An update that re-read the origin would hand the row's history
// to whoever touched it last.
func TestTheOriginIsNotRewrittenByAnUpdate(t *testing.T) {
	got := fileNamed(t, clientIPModel(t), "internal/application/commands/update_acesso_command.go")
	if strings.Contains(got, "ctx.ClientIP()") {
		t.Errorf("an update re-reads the origin, so the row stops recording where "+
			"it was created:\n%s", got)
	}
}

// A server-assigned field is absent from the write surface. That is what the
// key BUYS, and it holds for this source exactly as it does for an identity:
// no request carries it, no command declares it, no OpenAPI schema advertises
// it as writable.
func TestTheOriginIsOnNoWriteSurface(t *testing.T) {
	m := clientIPModel(t)
	for _, path := range []string{
		"internal/application/commands/insert_acesso_command.go",
		"internal/web/requests/insert_acesso.go",
		"internal/web/requests/update_acesso.go",
	} {
		body := fileNamed(t, m, path)
		for _, gone := range []string{"OrigemRede string `json", "OrigemProxy *string `json"} {
			if strings.Contains(body, gone) {
				t.Errorf("%s advertises the origin as something a caller sends:\n%s", path, body)
			}
		}
	}
	// The command type itself: the field may be assigned onto the ENTITY, never
	// declared on the command a request maps into.
	cmd := fileNamed(t, m, "internal/application/commands/insert_acesso_command.go")
	body := between(t, cmd, "type InsertAcessoCommand struct {", "}")
	if strings.Contains(body, "OrigemRede") || strings.Contains(body, "OrigemProxy") {
		t.Errorf("the command carries the origin, so a caller can state it:\n%s", body)
	}
}
