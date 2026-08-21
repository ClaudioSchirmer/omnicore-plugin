package spec

import (
	"strings"
	"testing"
)

// The reported shape: a collection whose single field IS its business identity.
//
// Before children[].operations, `check` warned that the change verb could only
// turn one entry into another while keeping the first one's row id, and offered
// two ways out that were both worse than the model: atomic-replace, which makes
// a partial client a silent mass-revoker, and adding a field the domain does
// not have so the verb has something to change.
const childOpsSpec = `
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
    editStrategy: %s
    businessIdentity: [PermissaoID]
    softRemove: true
    archivedAt: deleted_at
%s
    fields:
      - {name: PermissaoID, type: id, column: permissao_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: A permissão.}
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

func childOpsProblems(t *testing.T, strategy, operations string) *Problems {
	t.Helper()
	src := strings.Replace(childOpsSpec, "editStrategy: %s", "editStrategy: "+strategy, 1)
	src = strings.Replace(src, "%s\n", operations, 1)
	s, err := Parse([]byte(src), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	ps := Validate(s, Options{})
	// The two halves an author meets as one answer: Validate refuses an
	// incoherent spec, CheckCoverage refuses what this build will not act on.
	for _, p := range CheckCoverage(s).Blockers() {
		ps.BlockerFix(p.Where, p.Message, p.Fix)
	}
	return ps
}

func warningSaying(ps *Problems, want string) string {
	for _, p := range ps.Warnings() {
		if strings.Contains(p.Where+p.Message+p.Fix, want) {
			return p.String()
		}
	}
	return ""
}

// The warning is the whole reason the key exists, so it has to be there when
// the key is not — and gone the moment the author answers it.
func TestIdentityOnlyCollectionIsWarnedAboutItsChangeVerb(t *testing.T) {
	ps := childOpsProblems(t, "per-child", "")
	got := warningSaying(ps, "no field outside its business identity")
	if got == "" {
		t.Fatalf("nothing warns that the change verb is a swap:\n%v", ps.Error())
	}
	if !strings.Contains(got, "operations: [add, remove]") {
		t.Errorf("the warning does not offer the key that answers it: %s", got)
	}
}

func TestDroppingTheChangeVerbSilencesTheWarning(t *testing.T) {
	ps := childOpsProblems(t, "per-child", "    operations: [add, remove]\n")
	if ps.HasBlockers() {
		t.Fatalf("a collection that mounts add and remove is refused:\n%v", ps.Error())
	}
	if got := warningSaying(ps, "no field outside its business identity"); got != "" {
		t.Errorf("the author answered the question and is warned anyway: %s", got)
	}
}

// Every mistake this key can carry silently REMOVES a route, and a missing
// endpoint is not something an author finds out about from the code.
func TestChildOperationsRefusals(t *testing.T) {
	for _, tc := range []struct {
		name       string
		strategy   string
		operations string
		want       string
	}{
		{"a verb that does not exist", "per-child", "    operations: [add, upsert]\n", "not a per-entry verb"},
		{"the same verb twice", "per-child", "    operations: [add, add]\n", "named twice"},
		{"an empty list", "per-child", "    operations: []\n", "the list is empty"},
		{"a collection with no per-entry verbs at all", "atomic-replace", "    operations: [add]\n", "picks among the PER-ENTRY verbs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := childOpsProblems(t, tc.strategy, tc.operations)
			if got := blockerSaying(ps, tc.want); got == "" {
				t.Fatalf("accepted without saying %q:\n%v", tc.want, ps.Error())
			}
		})
	}
}

// A duplicate answer belongs to the ADD path. Left declared on a collection
// that mounts no add, it is a notification nothing can raise — and it looks
// like a working guard.
func TestDuplicateNotificationNeedsTheAddVerb(t *testing.T) {
	src := strings.Replace(childOpsSpec, "editStrategy: %s", "editStrategy: per-child", 1)
	src = strings.Replace(src, "%s\n",
		"    operations: [remove]\n    duplicateNotification: JaConcedidaNotification\n", 1)
	src = strings.Replace(src, "read:\n", `notifications:
  - name: JaConcedidaNotification
    semantic: conflict
    text: {ptbr: Ja concedida., eng: Already granted., esp: Ya concedido., fra: Deja accorde., deu: Bereits gewaehrt., ita: Gia concesso., nld: Al verleend.}
read:
`, 1)
	s, err := Parse([]byte(src), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if got := blockerSaying(CheckCoverage(s), "operations do not mount one"); got == "" {
		t.Fatalf("a duplicate answer no verb can raise was accepted:\n%v", CheckCoverage(s).Error())
	}
}
