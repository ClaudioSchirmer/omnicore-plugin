package spec

import (
	"strings"
	"testing"
)

// A field carries four names — the Go identifier, the column, the wire key and
// the notification token — and until these two keys existed the last two were
// derived and unarguable. Making them declarable is what lets a service answer
// in the domain's own word ("cpf", not "nationalId"); it is also what makes
// every way of getting a wire name wrong reachable from one line of yaml, which
// is what the refusals below are for.
const wireNameSpec = `
specVersion: 1
entity: Pessoa
plural: Pessoas
language: pt-BR
storage:
  kind: flat
  table: pessoas
  description: Pessoas.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Ana, description: O nome.}
  - name: DocumentoNacional
    type: string
    column: documento_nacional
    length: 14
    livesOn: root
    example: "529.982.247-25"
    description: O documento.
%s
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
read:
  backing: relational
  view: {name: pessoas}
  byId: true
surfaces: {rest: true}
authz:
  resource: pessoa
  dataAccess: anyone-with-permission
  permissions: {insert: "pessoa:escrever", update: "pessoa:escrever", patch: "pessoa:escrever", archive: "pessoa:arquivar", read: "pessoa:ler"}
`

func wireNameProblems(t *testing.T, keys string) *Problems {
	t.Helper()
	s, err := Parse([]byte(strings.Replace(wireNameSpec, "%s\n", keys, 1)), "pessoa.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	return Validate(s, Options{})
}

// TestTheWireNamePairIsAccepted is the case the keys exist for, and it comes
// first so every refusal below is proven to have refused something specific
// rather than the whole shape.
func TestTheWireNamePairIsAccepted(t *testing.T) {
	ps := wireNameProblems(t, "    jsonName: cpf\n    notifyAs: cpf\n")
	if ps.HasBlockers() {
		t.Fatalf("the canonical declaration is refused:\n%v", ps.Error())
	}
	for _, p := range ps.Warnings() {
		if strings.Contains(p.Where, "jsonName") || strings.Contains(p.Where, "notifyAs") {
			t.Errorf("the pair declared together still warns: %s — %s", p.Where, p.Message)
		}
	}
}

// TestHalfAWireNameWarns: the two are one vocabulary. A caller who posted `cpf`
// and is refused about `documentoNacional` cannot map the answer back to
// anything they wrote — and neither half is wrong on its own, so this is a
// warning the author can overrule, not a blocker.
func TestHalfAWireNameWarns(t *testing.T) {
	for _, c := range []struct{ keys, wants string }{
		{"    jsonName: cpf\n", "notifyAs"},
		{"    notifyAs: cpf\n", "jsonName"},
	} {
		ps := wireNameProblems(t, c.keys)
		if ps.HasBlockers() {
			t.Fatalf("declaring one half is refused outright:\n%v", ps.Error())
		}
		var found bool
		for _, p := range ps.Warnings() {
			if strings.Contains(p.Where, c.wants) {
				found = true
			}
		}
		if !found {
			t.Errorf("declaring only %q says nothing about the other half", c.keys)
		}
	}
}

// TestAWireNameMustLookLikeTheOthersInThePayload. The restriction is not style
// policing: every other name on this service's wire is a lower-camel rendering,
// so an override in another shape leaves ONE field spelled unlike its
// neighbours in the same body — and a dot or a bracket in a notification token
// forges a path segment, which reads to the caller as nesting that is not there.
func TestAWireNameMustLookLikeTheOthersInThePayload(t *testing.T) {
	for _, bad := range []string{"documento_nacional", "DocumentoNacional", "endereco.cep", "itens[0]", "9cpf"} {
		ps := wireNameProblems(t, "    jsonName: "+bad+"\n    notifyAs: "+bad+"\n")
		if !ps.HasBlockers() {
			t.Errorf("%q was accepted as a wire name", bad)
		}
	}
}

// TestAWireNameCannotTakeAManagedOne. The framework already answers under these
// on every aggregate; two fields under one key hand the caller whichever the
// encoder wrote last, with nothing marking the loser.
func TestAWireNameCannotTakeAManagedOne(t *testing.T) {
	for _, taken := range []string{"id", "createdAt", "updatedAt", "deletedAt", "revision", "parentId"} {
		ps := wireNameProblems(t, "    jsonName: "+taken+"\n    notifyAs: "+taken+"\n")
		if blockerSaying(ps, taken) == "" {
			t.Errorf("%q was accepted, and it is the framework's own", taken)
		}
	}
}

// TestAWireNameThatChangesNothingIsRefused. A key that restates the default
// reads as a decision somebody made, and the next reader spends time looking
// for what it decided.
func TestAWireNameThatChangesNothingIsRefused(t *testing.T) {
	ps := wireNameProblems(t, "    jsonName: documentoNacional\n    notifyAs: documentoNacional\n")
	if blockerSaying(ps, "already what") == "" {
		t.Errorf("a no-op override was accepted:\n%v", ps.Error())
	}
}

// TestTwoFieldsMayNotShareAWireName is the collision the override made possible.
// Without it two Go names cannot be equal and the rendering is a function of the
// name, so the payload could not have two of anything.
func TestTwoFieldsMayNotShareAWireName(t *testing.T) {
	ps := wireNameProblems(t, "    jsonName: nome\n    notifyAs: nome\n")
	if blockerSaying(ps, "already names another field") == "" {
		t.Errorf("two fields were allowed under one key:\n%v", ps.Error())
	}
}
