package ir

import (
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// Every key that addresses a collection takes either of its two names, and the
// IR is where that stops being true — one spelling below this line, chosen here.
//
// readColumn is the one that nearly got away. read.indexes and read.fieldRestrict
// address a collection's field as <collection>.<field>, validation accepts both
// spellings, and a head matched against the entry type ALONE fell through to
// `return name` — declaring an index over the literal string
// "PermissoesCS.PermissaoID" instead of the document path. The spec validated,
// the view was built, and nothing indexed anything: the same silent shape as the
// derivation that lost a parameter.
func TestACollectionsFieldResolvesUnderEitherSpelling(t *testing.T) {
	sp := &spec.Spec{Children: []spec.Child{{Name: "PapelPermissaoCS", Plural: "PermissoesCS"}}}
	m := &Model{Children: []Child{{
		Name: "PapelPermissaoCS", Plural: "PermissoesCS", DocSegment: "PermissoesCS",
		Fields: []Field{{Name: "PermissaoID", Column: "permissao_id"}},
	}}}

	const want = "PermissoesCS.permissao_id"
	for _, spelling := range []string{"PapelPermissaoCS", "PermissoesCS"} {
		if got := readColumn(sp, m, spelling+".PermissaoID"); got != want {
			t.Errorf("%s.PermissaoID resolved to %q, want %q — an unresolved head becomes "+
				"an index over a string no document has", spelling, got, want)
		}
	}
}

// The canonical form itself: whatever the author wrote, the IR carries the entry
// type's name, because that is what every emitter below matches on — the schema
// function a join calls is <Name>Schema, and a facet's owner is compared to
// Child.Name.
func TestTheIRCarriesOneSpelling(t *testing.T) {
	children := []spec.Child{{Name: "PapelPermissaoCS", Plural: "PermissoesCS"}}
	for _, written := range []string{"PapelPermissaoCS", "PermissoesCS"} {
		if got := canonicalCollection(children, written); got != "PapelPermissaoCS" {
			t.Errorf("%q canonicalised to %q, want PapelPermissaoCS", written, got)
		}
	}
	// A name nothing answers to travels unchanged: validation has already
	// refused it, and blanking it here would turn a reported blocker into a rule
	// that silently applies to nothing.
	if got := canonicalCollection(children, "NaoExiste"); got != "NaoExiste" {
		t.Errorf("an unresolvable name became %q — validation's blocker would lose its subject", got)
	}
}
