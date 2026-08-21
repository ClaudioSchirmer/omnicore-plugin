package emit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// The shape that hid the hole: a TENANT-SCOPED root that also has per-entry
// child verbs and a 1:1 facet.
//
// rowScopeSpec, the fixture the write guard was built against, is flat — no
// children, no siblings. So every test of the guard read the root's own three
// mappers and found them fed, while the four OTHER write mappers the generator
// emits for a richer entity went unread. All four mount UpdateCommandHandler,
// so the guard runs for them; none of them named the AppContext, so the guard
// ran on zeroed fields and stood down. A caller holding nothing but
// perfil:escrever could add an entry to, change an entry of, revoke an entry
// from, and clear the facet of an aggregate owned by ANOTHER TENANT.
//
// Nothing about it is visible from a build or from the generated suite, which
// is why the fixture had to grow rather than the assertions.
const identityFeedSpec = `
specVersion: 1
entity: Perfil
plural: Perfis
language: pt-BR
storage:
  kind: flat
  table: perfis
  description: Perfis de acesso.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: TenantID, type: id, column: tenant_id, livesOn: root, example: 9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3, description: O tenant dono.}
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Admin, description: O nome.}
children:
  - name: PerfilPermissao
    plural: Permissoes
    table: perfil_permissoes
    parentColumn: perfil_id
    description: As permissões concedidas pelo perfil.
    ownedBy: root
    editStrategy: per-child
    operations: [add, change, remove]
    businessIdentity: [PermissaoID]
    softRemove: true
    archivedAt: deleted_at
    fields:
      - {name: PermissaoID, type: id, column: permissao_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: A permissão concedida.}
      - {name: Escopo, type: string, column: escopo, length: 20, example: leitura, description: O escopo da concessão.}
siblings:
  - name: Contato
    table: perfil_contatos
    description: Dados de contato do responsável, quando houver.
    attachTo: root
    fields:
      - {name: ResponsavelEmail, type: string, column: responsavel_email, length: 160, nullable: true, example: ana@empresa.br, description: E-mail do responsável.}
modes: [display, insert, update, archive, unarchive]
update: {shape: both}
delete: {root: soft}
rules:
  list:
    - id: teto-de-permissoes
      kind: groupCap
      scope: [insertOrUpdate]
      fields: [PerfilPermissao]
      cap: 200
      description: Um limite sobre o TAMANHO da coleção — o orçamento de um token.
      notification: PermissoesDemaisNotification
    - id: teto-por-escopo
      kind: groupCap
      scope: [insertOrUpdate]
      fields: [PerfilPermissao]
      groupBy: [Escopo]
      cap: 3
      notification: PermissoesDemaisPorEscopoNotification
    - id: teto-de-leitura
      kind: groupCap
      scope: [insertOrUpdate]
      fields: [PerfilPermissao]
      cap: 7
      only: {field: Escopo, equals: leitura}
      notification: LeiturasDemaisNotification
notifications:
  - name: PermissoesDemaisNotification
    semantic: validation
    tvars: [max]
    description: O perfil concede mais permissões do que o permitido.
    text:
      ptbr: Um perfil pode conceder no máximo {max} permissões.
      eng: A profile may grant at most {max} permissions.
      esp: Un perfil puede conceder como máximo {max} permisos.
      fra: Un profil peut accorder au maximum {max} permissions.
      deu: Ein Profil darf höchstens {max} Berechtigungen gewähren.
      ita: Un profilo può concedere al massimo {max} permessi.
      nld: Een profiel mag maximaal {max} permissies verlenen.
  - name: PermissoesDemaisPorEscopoNotification
    semantic: validation
    tvars: [cap]
    description: O perfil concede mais permissões de um mesmo escopo do que o permitido.
    text:
      ptbr: No máximo {cap} permissões por escopo.
      eng: At most {cap} permissions per scope.
      esp: Como máximo {cap} permisos por ámbito.
      fra: Au maximum {cap} permissions par portée.
      deu: Höchstens {cap} Berechtigungen pro Bereich.
      ita: Al massimo {cap} permessi per ambito.
      nld: Maximaal {cap} permissies per bereik.
  - name: LeiturasDemaisNotification
    semantic: validation
    tvars: [max]
    description: O perfil concede mais permissões de leitura do que o permitido.
    text:
      ptbr: No máximo {max} permissões de leitura.
      eng: At most {max} read permissions.
      esp: Como máximo {max} permisos de lectura.
      fra: Au maximum {max} permissions de lecture.
      deu: Höchstens {max} Leseberechtigungen.
      ita: Al massimo {max} permessi di lettura.
      nld: Maximaal {max} leesrechten.
read:
  backing: relational
  view: {name: perfis, version: 1}
  byId: true
surfaces:
  rest: true
  graphql: {enabled: true, mutations: [insert, update, archive, unarchive]}
authz:
  resource: perfil
  dataAccess: %s
  tenantField: TenantID
  permissions: {insert: "perfil:escrever", update: "perfil:escrever", patch: "perfil:escrever", archive: "perfil:arquivar", unarchive: "perfil:arquivar", read: "perfil:ler"}
`

func identityFeedModel(t *testing.T, dataAccess string) *ir.Model {
	t.Helper()
	src := strings.Replace(identityFeedSpec, "%s", dataAccess, 1)
	if dataAccess != "tenant" {
		src = strings.Replace(src, "  tenantField: TenantID\n", "", 1)
	}
	s, err := spec.Parse([]byte(src), "perfil.omnicore.yaml")
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

// The guard is only as good as its feed, and the feed has to reach EVERY mapper
// that writes the root — not just the ones the root's own verbs use.
//
// A per-entry verb writes the aggregate under ModeUpdate, so IfInsertOrUpdate
// fires and refuseForeignTenant runs. With the context discarded the three
// fields it reads stayed zero, RequestingIdentityPresent was false, and the
// stand-down policy — the DEFAULT — turned the guard into a no-op on exactly
// the routes that grant and revoke.
func TestPerEntryChildVerbsCarryTheIdentity(t *testing.T) {
	m := identityFeedModel(t, "tenant")
	got := fileNamed(t, m, "internal/application/commands/perfil_permissao_commands.go")
	for _, verb := range []string{"AddPerfilPermissao", "ChangePerfilPermissao", "RemovePerfilPermissao"} {
		sig := fmt.Sprintf("func (cmd *%sCommand) ApplyTo(ctx *configuration.AppContext", verb)
		if !strings.Contains(got, sig) {
			t.Errorf("%s discards the AppContext, so the write guard reads zeroed fields:\n%s", verb, got)
		}
	}
	if n := strings.Count(got, "e.RequestingTenant = id.TenantID()"); n != 3 {
		t.Errorf("the caller reaches %d of the 3 per-entry mappers:\n%s", n, got)
	}
	if n := strings.Count(got, "e.RequestingIdentityPresent = true"); n != 3 {
		t.Errorf("%d of the 3 per-entry mappers record that an identity was present:\n%s", n, got)
	}
}

// The GraphQL-only facet verb, which is the same defect wearing a different
// name: no body, no assignment the reader would miss, and a write to the root
// all the same.
func TestFacetClearVerbCarriesTheIdentity(t *testing.T) {
	m := identityFeedModel(t, "tenant")
	got := fileNamed(t, m, "internal/application/commands/clear_contato_command.go")
	if !strings.Contains(got, "func (cmd *ClearContatoCommand) ApplyTo(ctx *configuration.AppContext") {
		t.Errorf("the facet-clearing mutation discards the AppContext:\n%s", got)
	}
	if !strings.Contains(got, "e.RequestingTenant = id.TenantID()") {
		t.Errorf("clearing a facet of another tenant's row goes unguarded:\n%s", got)
	}
}

// The other half of the contract: an entity with no runtime fields has nothing
// to feed, and its mappers say so at the signature rather than taking a
// parameter they never read.
func TestUnscopedEntityLeavesTheContextUnnamed(t *testing.T) {
	m := identityFeedModel(t, "anyone-with-permission")
	got := fileNamed(t, m, "internal/application/commands/perfil_permissao_commands.go")
	if strings.Contains(got, "ApplyTo(ctx *configuration.AppContext") {
		t.Errorf("a mapper with nothing to carry names the context anyway:\n%s", got)
	}
	if strings.Contains(got, "RequestingTenant") {
		t.Errorf("an unscoped entity got a feed it has no fields for:\n%s", got)
	}
}
