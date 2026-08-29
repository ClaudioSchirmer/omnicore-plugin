package spec

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A fact's filters became a criteria TREE, and every case here is a query that
// would otherwise have been emitted and meant something other than what the
// spec said.
//
// The shape this file exists for is the one the whole key was added to kill: a
// question about a SET, asked as one fact per member and folded back together
// in a rule, with the definition of the set living outside the fact named for
// it. Everything below is what had to be refused for that to be safe to write.

// enumFactSpec is factSpec plus an enum-backed field, because the pinned-value
// half of the vocabulary is only meaningful over a closed set.
func enumFactSpec(f Fact) *Spec {
	s := factSpec(f)
	s.Fields = append(s.Fields, Field{
		Name: "Situacao", Type: "string", Column: "situacao", Length: 20, LivesOn: "root",
		VO:      &FieldVO{Kind: "enum", Ref: "SituacaoAluno"},
		Example: "ativa", Description: "Situação da matrícula.",
	})
	s.ValueObjects = append(s.ValueObjects, ValueObject{
		Name: "SituacaoAluno", Kind: "enum", Backing: "string",
		Description:         "Situação da matrícula.",
		UnknownNotification: "SituacaoDesconhecidaNotification",
		Members: []EnumMember{
			{Name: "Ativa", Value: "ativa", Text: sevenTexts("Ativa")},
			{Name: "Trancada", Value: "trancada", Text: sevenTexts("Trancada")},
		},
	})
	s.Notifications = append(s.Notifications, Notification{
		Name: "SituacaoDesconhecidaNotification", Package: "vos", Semantic: "validation",
		Text: sevenTexts("Situação desconhecida."),
	})
	return s
}

// idFactSpec adds a foreign key, because two of the refusals below are about
// what an IDENTITY can be asked — an order between two of them, and one typed
// into the spec by hand.
func idFactSpec(f Fact) *Spec {
	s := factSpec(f)
	s.Fields = append(s.Fields, Field{
		Name: "CampusID", Type: "id", Column: "campus_id", LivesOn: "root",
		Example: "1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4", Description: "Campus.",
	})
	return s
}

func sevenTexts(v string) Texts {
	return Texts{PTBR: v, ENG: v, ESP: v, FRA: v, DEU: v, ITA: v, NLD: v}
}

// TestBareFilterStillMeansEquality is the retro-compatibility case, and it is
// first on purpose: the key grew a shape, and a language that renames what it
// already had makes every existing spec wrong on the day the feature ships.
func TestBareFilterStillMeansEquality(t *testing.T) {
	s := factSpec(Fact{
		Name: "NotasNaSituacao", Kind: "count", Filters: eqFilters("Status"),
		Description: "Quantos nesta situação.",
	})
	if ps := Validate(s, Options{}); ps.HasBlockers() {
		t.Fatalf("a bare filter name must still validate:\n%v", ps.Error())
	}
	if op := s.Service.Facts[0].Filters[0].Operator(); op != "eq" {
		t.Fatalf("a bare filter name is an equality, got %q", op)
	}
}

// TestFilterBlockParsesBothSpellings proves the two spellings decode into the
// same node type — the reason the whole tree could be added without a second
// key beside the first.
func TestFilterBlockParsesBothSpellings(t *testing.T) {
	var f Fact
	if err := yaml.Unmarshal([]byte("filters: [Status, {field: Grade, op: gte, as: notaMinima}]"), &f); err != nil {
		t.Fatalf("the two spellings must decode together: %v", err)
	}
	if len(f.Filters) != 2 {
		t.Fatalf("expected two nodes, got %d", len(f.Filters))
	}
	if f.Filters[0].Field != "Status" || f.Filters[0].Operator() != "eq" {
		t.Errorf("the bare name did not decode as an eq leaf: %+v", f.Filters[0])
	}
	if f.Filters[1].ParamName() != "notaMinima" {
		t.Errorf("`as` did not reach the parameter name: %+v", f.Filters[1])
	}
}

// TestUnknownKeyInsideAFilterIsRefused closes the hole a custom unmarshaler
// opens: yaml.v3 stops applying KnownFields the moment a type decodes itself,
// so `feild:` would have landed as an empty node and emitted a method with one
// parameter missing.
func TestUnknownKeyInsideAFilterIsRefused(t *testing.T) {
	var f Fact
	err := yaml.Unmarshal([]byte("filters: [{feild: Status}]"), &f)
	if err == nil {
		t.Fatal("an unknown key inside a filter must be an error")
	}
	if !strings.Contains(err.Error(), "feild") {
		t.Errorf("the error must name the offending key, got: %v", err)
	}
}

// TestUnknownOperatorIsRefusedWithTheSet keeps the vocabulary closed: the
// framework rejects an operator it has no builder for, so this build does too,
// and prints the whole set rather than the one word that failed.
func TestUnknownOperatorIsRefusedWithTheSet(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "Coisas", Kind: "count", Description: "d",
		Filters: []FactFilter{{Field: "Status", Op: "matches"}},
	}), Options{})
	if !blockerAbout(ps, "startswith") {
		t.Fatalf("an unknown operator must print the set, got:\n%v", ps.Error())
	}
}

// TestOperatorAndColumnMustAgree is the filter-side twin of the kind/field
// check: each of these compiles, runs, and answers something nobody asked.
func TestOperatorAndColumnMustAgree(t *testing.T) {
	for _, tc := range []struct{ name, field, op, want string }{
		{"isnull over a NOT NULL column", "Status", "isnull", "same answer for every row"},
		{"contains over a number", "Credits", "contains", "matches TEXT"},
		{"a range over an identity", "CampusID", "gte", "not greater than"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := Validate(idFactSpec(Fact{
				Name: "Coisas", Kind: "count", Description: "d",
				Filters: []FactFilter{{Field: tc.field, Op: tc.op}},
			}), Options{})
			if !blockerAbout(ps, tc.want) {
				t.Fatalf("expected a refusal about %q, got:\n%v", tc.want, ps.Error())
			}
		})
	}
}

// TestOneValueAndASetAreNotInterchangeable pins the two keys apart. A single
// key covering both would have to guess whether a one-item list is a set of one
// or a scalar somebody over-punctuated, and the generated signature differs.
func TestOneValueAndASetAreNotInterchangeable(t *testing.T) {
	for _, tc := range []struct {
		name string
		leaf FactFilter
		want string
	}{
		{"in with a single value", FactFilter{Field: "Status", Op: "in", Value: "ativa"},
			"compares against a SET"},
		{"eq with a set", FactFilter{Field: "Status", Op: "eq", Values: []any{"ativa"}},
			"compares against ONE value"},
		{"both at once", FactFilter{Field: "Status", Op: "in", Value: "ativa", Values: []any{"ativa"}},
			"pins both a value and a set"},
		{"an empty set", FactFilter{Field: "Status", Op: "in", Values: []any{}},
			"the set is empty"},
		{"a value where none is compared", FactFilter{Field: "Nickname", Op: "isnull", Value: "x"},
			"compares against nothing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := Validate(factSpec(Fact{
				Name: "Coisas", Kind: "count", Description: "d",
				Filters: []FactFilter{tc.leaf},
			}), Options{})
			if !blockerAbout(ps, tc.want) {
				t.Fatalf("expected a refusal about %q, got:\n%v", tc.want, ps.Error())
			}
		})
	}
}

// TestPinnedEnumLiteralIsTheMemberName pins the spelling. The stored value is
// what the query carries, but the SPEC names the member — otherwise one project
// spells the same member two ways, and the day the stored value changes the
// fact quietly stops matching anything.
func TestPinnedEnumLiteralIsTheMemberName(t *testing.T) {
	ok := enumFactSpec(Fact{
		Name: "Trancados", Kind: "count", Description: "d",
		Filters: []FactFilter{{Field: "Situacao", Op: "in", Values: []any{"Trancada"}}},
	})
	if ps := Validate(ok, Options{}); ps.HasBlockers() {
		t.Fatalf("a member NAME must be accepted:\n%v", ps.Error())
	}
	bad := enumFactSpec(Fact{
		Name: "Trancados", Kind: "count", Description: "d",
		Filters: []FactFilter{{Field: "Situacao", Op: "in", Values: []any{"trancada"}}},
	})
	ps := Validate(bad, Options{})
	if !blockerAbout(ps, "is not a member of SituacaoAluno") {
		t.Fatalf("the stored value is not the spelling; expected the member list, got:\n%v", ps.Error())
	}
}

// TestATimestampOrAnIdentityCannotBePinned refuses the two literals that read
// as data entry: a date typed into a spec is a query that ages, and an id is a
// row someone pasted.
func TestATimestampOrAnIdentityCannotBePinned(t *testing.T) {
	for field, want := range map[string]string{
		"EnrolledAt": "query that ages",
		"CampusID":   "row someone pasted",
	} {
		ps := Validate(idFactSpec(Fact{
			Name: "Coisas", Kind: "count", Description: "d",
			Filters: []FactFilter{{Field: field, Op: "eq", Value: "x"}},
		}), Options{})
		if !blockerAbout(ps, want) {
			t.Fatalf("pinning %s should be refused (%q), got:\n%v", field, want, ps.Error())
		}
	}
}

// TestTwoComparisonsOfOneFieldNeedNames is why `as` exists: the default
// parameter name is the field's own, and a floor and a ceiling over one column
// are two parameters. Two of one name do not compile.
func TestTwoComparisonsOfOneFieldNeedNames(t *testing.T) {
	clash := factSpec(Fact{
		Name: "NaFaixa", Kind: "count", Description: "d",
		Filters: []FactFilter{{Field: "Grade", Op: "gte"}, {Field: "Grade", Op: "lte"}},
	})
	ps := Validate(clash, Options{})
	if !blockerAbout(ps, "do not compile") {
		t.Fatalf("two parameters of one name must be refused, got:\n%v", ps.Error())
	}
	if !blockerAbout(ps, "`as:`") {
		t.Fatalf("the fix must name `as:`, got:\n%v", ps.Error())
	}
	named := factSpec(Fact{
		Name: "NaFaixa", Kind: "count", Description: "d",
		Filters: []FactFilter{
			{Field: "Grade", Op: "gte", As: "notaMinima"},
			{Field: "Grade", Op: "lte", As: "notaMaxima"},
		},
	})
	if ps := Validate(named, Options{}); ps.HasBlockers() {
		t.Fatalf("named parameters resolve the clash:\n%v", ps.Error())
	}
}

// TestAGroupIsAGroupAndNothingElse refuses the shapes that would emit silently:
// a comparison beside a connective is a narrowing the emitter never reaches, an
// empty group matches nothing, and a group of one combines nothing.
func TestAGroupIsAGroupAndNothingElse(t *testing.T) {
	for _, tc := range []struct {
		name string
		node FactFilter
		want string
	}{
		{"a leaf key on a group", FactFilter{Field: "Status",
			Any: []FactFilter{{Field: "Grade", Op: "gte"}, {Field: "Credits", Op: "gte"}}},
			"says nothing here"},
		{"two connectives", FactFilter{
			All: []FactFilter{{Field: "Grade", Op: "gte"}, {Field: "Credits", Op: "gte"}},
			Any: []FactFilter{{Field: "Status"}, {Field: "Nickname"}}},
			"at once"},
		{"an empty group", FactFilter{Any: []FactFilter{}}, "holds no condition"},
		{"a group of one", FactFilter{Any: []FactFilter{{Field: "Status"}}}, "combines nothing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := Validate(factSpec(Fact{
				Name: "Coisas", Kind: "count", Description: "d",
				Filters: []FactFilter{tc.node},
			}), Options{})
			if !blockerAbout(ps, tc.want) {
				t.Fatalf("expected a refusal about %q, got:\n%v", tc.want, ps.Error())
			}
		})
	}
}

// TestAManualFactTakesOnlyParameters keeps the ELSE honest. A manual fact emits
// no query, so its filters exist only to shape the method someone is being
// asked to write: a connective, a pinned constant and an operator that puts
// nothing in the signature are all declarations with no effect — the shape this
// language refuses hardest, because it looks like it did something.
func TestAManualFactTakesOnlyParameters(t *testing.T) {
	for _, tc := range []struct {
		name string
		node FactFilter
		want string
	}{
		{"a connective", FactFilter{Any: []FactFilter{{Field: "Status"}, {Field: "Grade", Op: "gte"}}},
			"nothing to combine"},
		{"a pinned constant", FactFilter{Field: "Status", Op: "eq", Value: "ativa"},
			"a manual fact has none"},
		{"an operator with no value", FactFilter{Field: "Nickname", Op: "isnull"},
			"writes no query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := Validate(factSpec(Fact{
				Name: "Coisas", Kind: "manual", Returns: "bool",
				Description: "Respondida por outro serviço.",
				Filters:     []FactFilter{tc.node},
			}), Options{})
			if !blockerAbout(ps, tc.want) {
				t.Fatalf("expected a refusal about %q, got:\n%v", tc.want, ps.Error())
			}
		})
	}
}

// TestADeclarativeRuleCannotFillASet is the boundary between the two ways a set
// reaches the query. factRange fills a fact's arguments from the ENTITY, and the
// entity carries one value per field: passing the single value would turn the IN
// into an equality and answer a question nobody asked.
func TestADeclarativeRuleCannotFillASet(t *testing.T) {
	withRule := func(filters []FactFilter) *Spec {
		s := factSpec(Fact{
			Name: "NasSituacoes", Kind: "count", Description: "Quantos nestas situações.",
			Filters: filters,
		})
		s.Notifications = append(s.Notifications, Notification{
			Name: "DemaisNotification", Semantic: "validation",
			Text: sevenTexts("Demais."),
		})
		s.Rules.List = append(s.Rules.List, Rule{
			ID: "teto", Kind: "factRange", Scope: []string{"insert"},
			Fact: "NasSituacoes", Max: ptrFloat(50), AttachTo: "Status",
			Notification: "DemaisNotification",
		})
		return s
	}

	ps := Validate(withRule([]FactFilter{{Field: "Status", Op: "in"}}), Options{})
	if !blockerAbout(ps, "one value per field") {
		t.Fatalf("a set PARAMETER cannot be filled from the entity, got:\n%v", ps.Error())
	}
	if !blockerAbout(ps, "values: [...]") {
		t.Fatalf("the fix must point at pinning the set, got:\n%v", ps.Error())
	}

	// And the other half: pinned in the spec, the fact has no parameter for the
	// set at all, so the rule reads it exactly as it always could. This is the
	// case the whole key was added for.
	pinned := withRule([]FactFilter{{Field: "Status", Op: "in", Values: []any{"ativa", "trancada"}}})
	if ps := Validate(pinned, Options{}); ps.HasBlockers() {
		t.Fatalf("a pinned set leaves nothing for the rule to fill:\n%v", ps.Error())
	}
}

// TestAUniquePrecheckStaysPlainEquality is the one place this vocabulary is
// closed. The pre-check is the domain's half of a uniqueness whose other half
// is a database index: the index answers "is this exact tuple present", and a
// fact that ranged, ORed or pinned would ask something else and report its
// answer under the index's notification.
func TestAUniquePrecheckStaysPlainEquality(t *testing.T) {
	s := factSpec(Fact{
		Name: "StatusTomado", Kind: "exists", Description: "Já existe.",
		Filters: []FactFilter{
			{Field: "Status"},
			{Field: "Nickname", Op: "isnull"},
		},
		ExcludeSelf: true,
	})
	for i := range s.Fields {
		if s.Fields[i].Name == "Status" {
			s.Fields[i].Unique = &Unique{
				Enforce: "service-precheck+constraint", Notification: "StatusTomadoNotification",
			}
		}
	}
	s.Notifications = append(s.Notifications, Notification{
		Name: "StatusTomadoNotification", Semantic: "conflict", Text: sevenTexts("Já existe."),
	})
	ps := Validate(s, Options{})
	if !blockerAbout(ps, "more than equality") {
		t.Fatalf("a pre-check must compare the index's columns for equality, got:\n%v", ps.Error())
	}
}

func ptrFloat(v float64) *float64 { return &v }
