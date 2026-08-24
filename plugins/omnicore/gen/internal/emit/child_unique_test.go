package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// Uniqueness on a COLLECTION ENTRY, which used to be refused outright.
//
// The refusal pointed at businessIdentity, and businessIdentity cannot do this
// job: it is an in-process check over what ONE write carries, so two concurrent
// requests each adding the same entry both pass it and both rows land. The only
// backstop is an index — and with the key refused the author wrote that index by
// hand, into a migration the generator would then describe incorrectly, with no
// way to register a binding for it. So the duplicate surfaced as a raw 500 where
// the root's equivalent is a clean 409.
const childUniqueSpec = `
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
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Admin, description: O nome.}
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
children:
  - name: PapelPermissao
    plural: Permissoes
    table: papel_permissoes
    parentColumn: papel_id
    description: As permissões do papel.
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
        description: A permissão.
        unique:
          enforce: constraint-only
          notification: PermissaoJaConcedidaNotification
          scope: active-only
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

func childUniqueModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(childUniqueSpec), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("uniqueness on a collection entry is still refused:\n%v", ps.Error())
	}
	if cov := spec.CheckCoverage(s); cov.HasBlockers() {
		t.Fatalf("the build refuses to cover it:\n%v", cov.Error())
	}
	m, err := ir.Resolve(s, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"postgres"}, Root: t.TempDir(),
		NextOrdinal: map[string]int{"postgres": 1},
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	return m
}

// The index leads with the parent column, and that is not a preference: an entry
// has no identity outside its owner, and the same permission under a DIFFERENT
// role is a legitimate row.
func TestChildUniqueIndexIsScopedByTheOwner(t *testing.T) {
	m := childUniqueModel(t)
	got := fileNamed(t, m, "migrations/postgres/0001_papel_manual.up.sql")
	want := `CREATE UNIQUE INDEX "papel_permissoes_papel_id_permissao_id_key" ` +
		`ON "papel_permissoes" ("papel_id", "permissao_id") WHERE "deleted_at" IS NULL`
	if !strings.Contains(got, want) {
		t.Errorf("the entry's index is not the per-owner, active-only one:\n%s", got)
	}
}

// The half that was impossible before: without a binding the violation is a raw
// 500, which is the whole reason a hand-written index was not good enough.
func TestChildUniqueIsBoundToItsNotification(t *testing.T) {
	m := childUniqueModel(t)
	got := fileNamed(t, m, "internal/infra/papel_repository.go")
	if !strings.Contains(got, `"papel_permissoes_papel_id_permissao_id_key"`) {
		t.Errorf("the entry's constraint is not bound, so a duplicate is a raw 500:\n%s", got)
	}
	if !strings.Contains(got, "PermissaoJaConcedidaNotification{}") {
		t.Errorf("the binding does not carry the declared conflict:\n%s", got)
	}
	// The field path is the WIRE name of the collection: this string goes
	// straight into the error envelope, and the caller posted a list under it.
	if !strings.Contains(got, `Field: "permissoes"`) {
		t.Errorf("the violation is not reported under the collection the caller sent:\n%s", got)
	}
}

// An entry's active-only scope is defined by the ENTRY's archive column, not the
// root's: a soft-removed entry is what frees the value.
func TestChildActiveOnlyUsesTheEntrysOwnArchiveColumn(t *testing.T) {
	m := childUniqueModel(t)
	for _, c := range m.Constraints {
		if c.Table == "papel_permissoes" {
			if c.Archived != "deleted_at" {
				t.Errorf("the entry's constraint archives by %q", c.Archived)
			}
			return
		}
	}
	t.Fatal("no constraint was resolved for the collection")
}
