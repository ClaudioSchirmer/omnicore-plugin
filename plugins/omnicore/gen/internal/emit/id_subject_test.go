package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A row whose owner is an `id`, filled from a claim and checked by an
// ownerCheck. Both halves used to be refused for the same stated reason — "an
// identity is text" — and together they forced a choice the author had no way
// to see coming:
//
//   - keep the id, and the column is the engine's own type and can carry a
//     foreign key, but assignedFrom and ownerCheck are both unavailable, so the
//     rule is hand-written;
//   - take the string, and the declarative rules work while the column becomes a
//     VARCHAR that cannot reference a UUID column on postgres. Permanently:
//     changing a column's type later is a migration over live data.
const idSubjectSpec = `
specVersion: 1
entity: Nota
plural: Notas
language: pt-BR
storage:
  kind: flat
  table: notas
  description: Notas.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - name: DonoID
    type: id
    column: dono_id
    livesOn: root
    assignedFrom: identity-claim
    claim: user_id
    example: 9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3
    description: Quem criou a nota.
  - {name: Texto, type: string, column: texto, length: 200, livesOn: root, example: oi, description: O texto.}
  - {name: SolicitanteID, type: string, runtime: true, livesOn: root, claim: user_id, example: 9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3, description: Quem pediu.}
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
rules:
  list:
    - id: so-o-dono-arquiva
      kind: ownerCheck
      scope: [archive]
      fields: [DonoID]
      ownerField: SolicitanteID
      notification: NaoEhDonoNotification
notifications:
  - name: NaoEhDonoNotification
    semantic: forbidden
    text: {ptbr: Nao e sua., eng: Not yours., esp: No es suya., fra: Pas la votre., deu: Nicht Ihre., ita: Non e tua., nld: Niet van jou.}
read:
  backing: relational
  view: {name: notas}
  byId: true
surfaces: {rest: true}
authz:
  resource: nota
  dataAccess: anyone-with-permission
  permissions: {insert: "nota:escrever", update: "nota:escrever", patch: "nota:escrever", archive: "nota:arquivar", read: "nota:ler"}
`

func idSubjectModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(idSubjectSpec), "nota.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("an id owner filled from a claim is still refused:\n%v", ps.Error())
	}
	m, err := ir.Resolve(s, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	return m
}

// The claim arrives as text and the column is an id, so the mapper parses.
func TestIdentityClaimParsesIntoAnID(t *testing.T) {
	m := idSubjectModel(t)
	got := fileNamed(t, m, "internal/application/commands/insert_nota_command.go")
	if !strings.Contains(got, "e.DonoID = domain.NewID(raw)") {
		t.Errorf("the claim is not parsed into the id column:\n%s", got)
	}
}

// And the check unwraps it, because the caller's side is text either way.
// Comparing them whole does not compile — the good failure, but only reached
// three steps after the spec said yes.
func TestOwnerCheckUnwrapsAnIDOwner(t *testing.T) {
	m := idSubjectModel(t)
	got := fileNamed(t, m, "internal/domain/nota.go")
	if !strings.Contains(got, "e.DonoID.Value() != e.SolicitanteID") {
		t.Errorf("the owner check does not compare the id as text:\n%s", got)
	}
}

// The column stays the engine's own id type, which is the whole reason to keep
// the field an id: a VARCHAR holding a UUID cannot carry a foreign key to a
// UUID column on postgres.
func TestIDOwnerKeepsItsNativeColumn(t *testing.T) {
	m := idSubjectModel(t)
	for _, f := range m.Fields {
		if f.Name == "DonoID" {
			if f.GoType != "domain.ID" {
				t.Errorf("DonoID resolved as %s, not an id", f.GoType)
			}
			return
		}
	}
	t.Fatal("DonoID is not among the resolved fields")
}
