package spec

import (
	"strings"
	"testing"
)

// withComposite hangs a two-part composite value object off the minimal spec, in
// the shape both halves of the language require: the declaration says what the
// value object IS, the field says where this entity stores it.
func withComposite(written string) *Spec {
	s := minimalSpec()
	s.ValueObjects = []ValueObject{{
		Name: "PermissionKey", Kind: "composite", Written: written,
		Description: "Um par recurso:acao, com a regra do curinga.",
		Parts: []VOPart{
			{Name: "Resource", Type: "string", Description: "O recurso."},
			{Name: "Action", Type: "string", Description: "A acao."},
		},
	}}
	s.Fields = append(s.Fields, Field{
		Name: "Key", LivesOn: "root", Description: "A permissao.",
		VO: &FieldVO{Kind: "composite", Ref: "PermissionKey"},
		Parts: []FieldPart{
			{Part: "Resource", Column: "key_resource", As: "KeyResource", Length: 60, Example: "aluno"},
			{Part: "Action", Column: "key_action", As: "KeyAction", Length: 30, Example: "ler"},
		},
	})
	return s
}

// TestHandWrittenCompositeIsAccepted is the gap this kind was added for: a
// composite whose invariant none of the five rule kinds can state, and which
// `kind: manual` could not express either — that one is a scalar with a backing,
// and a composite's parts are what the schema decomposes into columns.
func TestHandWrittenCompositeIsAccepted(t *testing.T) {
	if ps := Validate(withComposite("manual"), Options{}); ps.HasBlockers() {
		t.Fatalf("a hand-written composite value object is refused:\n%v", ps.Error())
	}
}

// TestHandWrittenCompositeStaysLegalOnceWritten is the half that makes the
// feature work more than once. The run AFTER the author writes the type finds it
// in the project, and the guard against a second copy of one rule would
// otherwise refuse the declaration that asked for it.
func TestHandWrittenCompositeStaysLegalOnceWritten(t *testing.T) {
	opt := Options{
		ExistingVOs: []string{"PermissionKey"},
		VOOwner:     map[string]string{"PermissionKey": "SomeoneElse"},
	}
	if ps := Validate(withComposite("manual"), opt); ps.HasBlockers() {
		t.Fatalf("the declaration is refused once it has been honoured:\n%v", ps.Error())
	}
	// The generated twin is refused there, which is the rule this exempts from.
	if ps := Validate(withComposite(""), opt); !ps.HasBlockers() {
		t.Error("a GENERATED composite is allowed to redeclare a type the project " +
			"already has — that is two copies of one rule, free to drift")
	}
}

// TestHandWrittenCompositeRefusesRules pins the reason, not just the refusal:
// the author took the file, so there is nowhere left to emit a declared rule.
func TestHandWrittenCompositeRefusesRules(t *testing.T) {
	s := withComposite("manual")
	s.ValueObjects[0].Rules = Rules{List: []Rule{{
		ID: "resource-required", Kind: "required", Fields: []string{"Resource"},
		Notification: "RequiredFieldNotification",
	}}}

	ps := Validate(s, Options{})
	if !ps.HasBlockers() {
		t.Fatal("a rule on a hand-written composite is accepted, and nothing emits it")
	}
	if !strings.Contains(ps.Error().Error(), "no file for the generator to put a rule in") {
		t.Errorf("the blocker does not say why the rule has nowhere to go:\n%v", ps.Error())
	}
}

// TestWrittenManualIsRefusedOnAScalar keeps the language from having two
// spellings for one thing. A raw or an enum you write yourself has no shape left
// to declare once its rule is yours — that is `kind: manual`, backing included.
func TestWrittenManualIsRefusedOnAScalar(t *testing.T) {
	s := minimalSpec()
	s.ValueObjects = []ValueObject{{
		Name: "UF", Kind: "raw", Backing: "string", Written: "manual",
		MinLength: 2, MaxLength: 2, Notification: "SchemaViolationNotification",
		Description: "Sigla da unidade federativa.",
	}}
	s.Fields[0].VO = &FieldVO{Kind: "raw", Ref: "UF"}

	ps := Validate(s, Options{})
	if !ps.HasBlockers() {
		t.Fatal("written: manual on a scalar is accepted, so the same thing has two spellings")
	}
	if !strings.Contains(ps.Error().Error(), "write kind: manual instead") {
		t.Errorf("the blocker does not point at the one key that says this:\n%v", ps.Error())
	}
}

// TestFactNamesACompositePart is the pre-check the language could not ask for:
// uniqueness over a PAIR. Filters, indexes and ?fields= had resolved against the
// expanded set for as long as composites existed; facts had not.
func TestFactNamesACompositePart(t *testing.T) {
	s := withComposite("manual")
	s.Service = &Service{Required: true, Facts: []Fact{{
		Name: "KeyTaken", Kind: "exists", Filters: eqFilters("KeyResource", "KeyAction"),
		ExcludeSelf: true, Description: "Se outra linha ja tem este par.",
	}}}
	if ps := Validate(s, Options{}); ps.HasBlockers() {
		t.Fatalf("a fact over a composite's parts is refused:\n%v", ps.Error())
	}
}

// TestFactNamingTheCompositeItselfSaysWhichPartsExist pins the message. The
// composite IS a field of the entity, so naming it is the easy mistake, and
// "does not name a field" would be both true and useless.
func TestFactNamingTheCompositeItselfSaysWhichPartsExist(t *testing.T) {
	s := withComposite("manual")
	s.Service = &Service{Required: true, Facts: []Fact{{
		Name: "KeyTaken", Kind: "exists", Filters: eqFilters("Key"),
		Description: "Se outra linha ja tem esta chave.",
	}}}

	ps := Validate(s, Options{})
	if !ps.HasBlockers() {
		t.Fatal("a fact filtering on the composite as a whole is accepted, and it has no column")
	}
	if !strings.Contains(ps.Error().Error(), "KeyResource, KeyAction") {
		t.Errorf("the blocker does not offer the parts it could have named:\n%v", ps.Error())
	}
}

// TestHiddenFieldIsAcceptedAndFilterable is the shape the key exists for: the
// caller searches BY the field and receives something else.
func TestHiddenFieldIsAcceptedAndFilterable(t *testing.T) {
	s := minimalSpec()
	s.Fields = append(s.Fields, Field{
		Name: "DocumentNumber", Type: "string", Column: "document_number", Length: 20,
		LivesOn: "root", Hidden: true, Example: "529.982.247-25",
		Description: "Documento de identidade.",
	})
	s.Read.ByParams = &ByParams{Filters: []Filter{{Field: "DocumentNumber", Ops: []string{"eq"}}}}

	if ps := Validate(s, Options{}); ps.HasBlockers() {
		t.Fatalf("a hidden field that is still filtered on is refused:\n%v", ps.Error())
	}
}

// TestHiddenAndFieldRestrictContradict: one says nobody receives the field, the
// other says the callers holding a permission do. Emitting both would generate a
// permission that unlocks nothing, which reads in a review as an exposure that
// is not there.
func TestHiddenAndFieldRestrictContradict(t *testing.T) {
	s := minimalSpec()
	s.Fields = append(s.Fields, Field{
		Name: "DocumentNumber", Type: "string", Column: "document_number", Length: 20,
		LivesOn: "root", Hidden: true, Example: "529.982.247-25",
		Description: "Documento de identidade.",
	})
	s.Read.FieldRestrict = []FieldRestrict{{Field: "DocumentNumber", Permission: "student:read-document"}}

	ps := Validate(s, Options{})
	if !ps.HasBlockers() {
		t.Fatal("a hidden field is also restricted by permission, and the two disagree")
	}
	if !strings.Contains(ps.Error().Error(), "nothing for a permission to unlock") {
		t.Errorf("the blocker does not name the contradiction:\n%v", ps.Error())
	}
}
