package spec

import (
	"strings"
	"testing"
)

// A per-tenant natural key: the shape where the pre-check and the index used to
// disagree in silence. The fact filtered by [TenantID, Key] and the index
// covered role_key alone, so tenant B was refused a handle only tenant A held —
// under a notification saying the handle was taken, for a tenant where it was
// free. Every multi-tenant entity with a natural key landed there, on exactly
// the handles two customers both pick.
const uniqueWithinSpec = `
specVersion: 1
entity: Papel
plural: Papeis
language: pt-BR
storage:
  kind: flat
  table: papeis
  description: Papéis.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: TenantID, type: id, column: tenant_id, livesOn: root, example: 9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3, description: O tenant.}
  - {name: Apelido, type: string, column: apelido, length: 60, nullable: true, livesOn: root, example: adm, description: Apelido opcional.}
  - name: Chave
    type: string
    column: chave
    length: 60
    livesOn: root
    example: administrator
    description: O identificador do papel.
    unique:
      enforce: service-precheck+constraint
      notification: ChaveJaExisteNotification
      scope: active-only
%s
notifications:
  - name: ChaveJaExisteNotification
    semantic: conflict
    text: {ptbr: Ja existe., eng: Already exists., esp: Ya existe., fra: Existe deja., deu: Existiert bereits., ita: Esiste gia., nld: Bestaat al.}
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
service:
  required: true
  facts:
    - name: ChaveTomada
      kind: exists
      filters: [%s]
      excludeSelf: true
      activeOnly: true
      description: Se outro papel ativo ja usa esta chave.
read:
  backing: relational
  view: {name: papeis, version: 1}
  byId: true
surfaces: {rest: true}
authz:
  resource: papel
  dataAccess: anyone-with-permission
  permissions: {insert: "papel:escrever", update: "papel:escrever", patch: "papel:escrever", archive: "papel:arquivar", read: "papel:ler"}
`

// uniqueWithinProblems validates one pairing of a `within` clause and a set of
// precheck filters — the two halves this feature holds to each other.
func uniqueWithinProblems(t *testing.T, within, filters string) *Problems {
	t.Helper()
	clause := ""
	if within != "" {
		clause = "      within: [" + within + "]"
	}
	src := strings.Replace(uniqueWithinSpec, "%s\n", clause+"\n", 1)
	src = strings.Replace(src, "filters: [%s]", "filters: ["+filters+"]", 1)
	s, err := Parse([]byte(src), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	return Validate(s, Options{})
}

func TestScopedUniqueMatchingItsPrecheckIsAccepted(t *testing.T) {
	ps := uniqueWithinProblems(t, "TenantID", "TenantID, Chave")
	if ps.HasBlockers() {
		t.Fatalf("a scoped unique whose precheck matches is refused:\n%v", ps.Error())
	}
}

func TestUnscopedUniqueWithAnUnscopedPrecheckIsStillAccepted(t *testing.T) {
	ps := uniqueWithinProblems(t, "", "Chave")
	if ps.HasBlockers() {
		t.Fatalf("the plain, table-wide unique regressed:\n%v", ps.Error())
	}
}

// The reported shape, verbatim: a per-tenant precheck beside a global index. It
// used to pass check, generate and build.
func TestPrecheckNarrowerThanTheIndexIsRefused(t *testing.T) {
	ps := uniqueWithinProblems(t, "", "TenantID, Chave")
	got := blockerSaying(ps, "within")
	if got == "" {
		t.Fatalf("a precheck narrower than its index is still accepted:\n%v", ps.Error())
	}
	if !strings.Contains(got, "within: [TenantID]") {
		t.Errorf("the refusal does not hand over the exact clause to paste: %s", got)
	}
}

// And the mirror: an index scoped by a column the precheck ignores, so the
// domain answers about the whole table while the database answers per tenant.
func TestPrecheckWiderThanTheIndexIsRefused(t *testing.T) {
	ps := uniqueWithinProblems(t, "TenantID", "Chave")
	got := blockerSaying(ps, "ChaveTomada")
	if got == "" {
		t.Fatalf("an index scoped past its precheck is still accepted:\n%v", ps.Error())
	}
	if !strings.Contains(got, "TenantID, Chave") {
		t.Errorf("the refusal does not name the filters the fact needs: %s", got)
	}
}

// NULLs do not collide, so a nullable scope column scopes nothing: every row
// without a value would be unique on its own.
func TestNullableScopeColumnIsRefused(t *testing.T) {
	ps := uniqueWithinProblems(t, "Apelido", "Apelido, Chave")
	if blockerSaying(ps, "NULL") == "" {
		t.Errorf("a nullable scope column is accepted:\n%v", ps.Error())
	}
}

func TestScopeByAnUnknownFieldIsRefused(t *testing.T) {
	ps := uniqueWithinProblems(t, "NaoExiste", "NaoExiste, Chave")
	if blockerSaying(ps, "NaoExiste") == "" {
		t.Errorf("a scope naming no field is accepted:\n%v", ps.Error())
	}
}

func TestFieldCannotBeScopedByItself(t *testing.T) {
	ps := uniqueWithinProblems(t, "Chave", "Chave")
	if blockerSaying(ps, "itself") == "" {
		t.Errorf("a field scoped by itself is accepted:\n%v", ps.Error())
	}
}
