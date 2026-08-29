package spec

import "strings"

import "testing"

// The cases here are the ones a person does not reach by hand.
//
// A domain service is declared once per entity and then rarely revisited, so a
// fact whose kind and field disagree — an average over a date, a sum over a
// name — is written once, passes review because the YAML reads sensibly, and is
// found by the compiler minutes later or by nobody. Both shapes below shipped
// exactly that way: `check` was green and the tree did not build.
//
// Everything is asserted against the SAME baseline spec, changing one thing at
// a time, so a case that starts passing for an unrelated reason shows up as the
// baseline test failing rather than as silence here.

// factSpec is the minimal spec plus a service, ready to have one fact bent.
func factSpec(f Fact) *Spec {
	s := minimalSpec()
	s.Fields = append(s.Fields,
		Field{Name: "Credits", Type: "int64", Column: "credits", LivesOn: "root",
			Example: "120", Description: "Créditos."},
		Field{Name: "Grade", Type: "float64", Column: "grade", LivesOn: "root",
			Example: "8.5", Description: "Nota."},
		Field{Name: "Status", Type: "string", Column: "status", Length: 20, LivesOn: "root",
			Example: "ativo", Description: "Situação."},
		Field{Name: "EnrolledAt", Type: "time", Column: "enrolled_at", LivesOn: "root",
			Example: "2026-02-01T09:00:00Z", Description: "Data."},
		Field{Name: "Nickname", Type: "string", Column: "nickname", Length: 20, Nullable: true,
			LivesOn: "root", Example: "Aninha", Description: "Apelido."},
		Field{Name: "CallerEmail", Type: "string", Runtime: true, LivesOn: "root",
			Claim: "email", Example: "a@b.c", Description: "Quem chamou."},
	)
	s.Service = &Service{Required: true, Facts: []Fact{f}}
	return s
}

// blockerAbout reports whether some blocker mentions the given text, so a case
// asserts the REASON it was refused rather than merely that something failed.
func blockerAbout(ps *Problems, text string) bool {
	for _, p := range ps.Blockers() {
		if strings.Contains(p.Where+" "+p.Message+" "+p.Fix, text) {
			return true
		}
	}
	return false
}

func TestFactBaselineIsClean(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "SomaCreditos", Kind: "sum", Field: "Credits",
		Description: "Soma dos créditos.",
	}), Options{})
	if ps.HasBlockers() {
		t.Fatalf("the baseline fact should validate cleanly, got:\n%v", ps.Error())
	}
}

// TestAggregatingANonNumericFieldIsRefused pins the shape that shipped green:
// the framework carries an aggregate as a count, an exact integer or a float,
// and has no carrier for text, a timestamp or a boolean — so `max` over a name
// used to emit a float64 read out of a string column.
func TestAggregatingANonNumericFieldIsRefused(t *testing.T) {
	for _, tc := range []struct{ kind, field string }{
		{"max", "Status"},
		{"min", "Status"},
		{"max", "EnrolledAt"},
		{"avg", "EnrolledAt"},
		{"sum", "Status"},
	} {
		ps := Validate(factSpec(Fact{
			Name: "F", Kind: tc.kind, Field: tc.field, Description: "d",
		}), Options{})
		if !ps.HasBlockers() {
			t.Errorf("%s over %s must be refused: the framework has no carrier for it",
				tc.kind, tc.field)
		}
	}
}

func TestAggregatingANumericFieldIsAccepted(t *testing.T) {
	for _, tc := range []struct{ kind, field string }{
		{"sum", "Credits"}, {"avg", "Credits"}, {"min", "Credits"}, {"max", "Credits"},
		{"sum", "Grade"}, {"avg", "Grade"}, {"min", "Grade"}, {"max", "Grade"},
	} {
		ps := Validate(factSpec(Fact{
			Name: "F", Kind: tc.kind, Field: tc.field, Description: "d",
		}), Options{})
		if ps.HasBlockers() {
			t.Errorf("%s over %s should be accepted, got:\n%v", tc.kind, tc.field, ps.Error())
		}
	}
}

func TestGroupedFactAcceptsEveryComputedKind(t *testing.T) {
	for _, f := range []Fact{
		{Name: "F", Kind: "count", GroupBy: []string{"Status"}, Description: "d"},
		{Name: "F", Kind: "sum", Field: "Credits", GroupBy: []string{"Status"}, Description: "d"},
		{Name: "F", Kind: "avg", Field: "Grade", GroupBy: []string{"Status"}, Description: "d"},
		{Name: "F", Kind: "min", Field: "Grade", GroupBy: []string{"Status"}, Description: "d"},
		{Name: "F", Kind: "max", Field: "Credits", GroupBy: []string{"Status", "Name"}, Description: "d"},
	} {
		ps := Validate(factSpec(f), Options{})
		if ps.HasBlockers() {
			t.Errorf("a grouped %s should be accepted, got:\n%v", f.Kind, ps.Error())
		}
	}
}

// TestGroupingKeyMustHaveABucket is the same reasoning as the rule-level cap: a
// row whose key is absent belongs to no group, and counting the nulls together,
// apart, or not at all are three different rules the spec has not chosen
// between.
func TestGroupingKeyMustHaveABucket(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "F", Kind: "count", GroupBy: []string{"Nickname"}, Description: "d",
	}), Options{})
	if !blockerAbout(ps, "nullable") {
		t.Fatalf("a nullable grouping key must be refused, got:\n%v", ps.Error())
	}
}

func TestGroupingKeyMustBePersisted(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "F", Kind: "count", GroupBy: []string{"CallerEmail"}, Description: "d",
	}), Options{})
	if !blockerAbout(ps, "runtime-only") {
		t.Fatalf("a runtime-only grouping key has no column to group by, got:\n%v", ps.Error())
	}
}

func TestGroupingKeyMustExist(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "F", Kind: "count", GroupBy: []string{"Departamento"}, Description: "d",
	}), Options{})
	if !ps.HasBlockers() {
		t.Fatal("grouping by a field the entity does not declare must be refused")
	}
}

func TestGroupingKeyIsNotRepeated(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "F", Kind: "count", GroupBy: []string{"Status", "Status"}, Description: "d",
	}), Options{})
	if !blockerAbout(ps, "twice") {
		t.Fatalf("the same key twice must be refused, got:\n%v", ps.Error())
	}
}

// TestExistsCannotBeGrouped keeps the two halves of the DSL from blurring: a
// yes/no answer per group is a count, and accepting groupBy on exists would
// have meant emitting one and calling it something else.
func TestExistsCannotBeGrouped(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "F", Kind: "exists", Filters: eqFilters("Status"),
		GroupBy: []string{"Status"}, Description: "d",
	}), Options{})
	if !blockerAbout(ps, "count") {
		t.Fatalf("exists cannot be grouped, and the fix names count, got:\n%v", ps.Error())
	}
}

// TestManualFactCannotBeGrouped is the ELSE staying honest: nobody generates
// that body, so there is no query for groupBy to shape.
func TestManualFactCannotBeGrouped(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "F", Kind: "manual", Returns: "int64",
		GroupBy: []string{"Status"}, Description: "answered elsewhere",
	}), Options{})
	if !ps.HasBlockers() {
		t.Fatal("a manual fact has no generated query, so groupBy must be refused")
	}
}

// TestGroupedFactCountsAsACapability guards the report: a spec using the
// database's GROUP BY must SAY so, or a reviewer reading the coverage list sees
// a service and no hint that any of it is per-group.
func TestGroupedFactCountsAsACapability(t *testing.T) {
	cov := CheckCoverage(factSpec(Fact{
		Name: "F", Kind: "count", GroupBy: []string{"Status"}, Description: "d",
	}))
	if cov.HasBlockers() {
		t.Fatalf("a grouped fact is implemented and must not be refused, got:\n%v", cov.Error())
	}
	found := false
	for _, c := range AllCapabilities() {
		if c == CapGroupedFact {
			found = true
		}
	}
	if !found {
		t.Error("CapGroupedFact must be in the closed list, or `explain coverage` never mentions it")
	}
}

// ── factRange: the rule half of a fact ───────────────────────────────────────
//
// These pin the shapes that would otherwise be discovered by the compiler or,
// worse, not at all: a rule limiting a fact nobody declared, a limit with no
// bound, a notification with nowhere to land.

// factRangeSpec is the baseline: a service with one countable fact, plus a rule
// limiting it.
func factRangeSpec(r Rule) *Spec {
	s := factSpec(Fact{
		Name: "TotalDeTurmas", Kind: "count", Description: "Quantas turmas existem.",
	})
	s.Service.Facts = append(s.Service.Facts,
		Fact{Name: "PorSituacao", Kind: "count", GroupBy: []string{"Status"},
			Description: "Quantas por situação."},
		Fact{Name: "TemAlguma", Kind: "exists", Filters: eqFilters("Status"),
			Description: "Existe alguma."},
		Fact{Name: "AberturaDoCurso", Kind: "manual", Returns: "bool",
			Description: "Respondida por outro serviço."},
	)
	s.Notifications = append(s.Notifications, Notification{
		Name: "LimiteNotification", Semantic: "validation",
		Text: Texts{
			PTBR: "Limite atingido.", ENG: "Limit reached.", ESP: "Limite alcanzado.",
			FRA: "Limite atteinte.", DEU: "Grenze erreicht.", ITA: "Limite raggiunto.",
			NLD: "Limiet bereikt.",
		},
	})
	s.Rules.List = append(s.Rules.List, r)
	return s
}

func validFactRange() Rule {
	max := 500.0
	return Rule{
		ID: "teto", Kind: "factRange", Scope: []string{"insert"},
		Fact: "TotalDeTurmas", Max: &max, AttachTo: "Name",
		Notification: "LimiteNotification",
	}
}

func TestFactRangeBaselineIsClean(t *testing.T) {
	ps := Validate(factRangeSpec(validFactRange()), Options{})
	if ps.HasBlockers() {
		t.Fatalf("the baseline factRange should validate cleanly, got:\n%v", ps.Error())
	}
}

func TestFactRangeAcceptsEveryAnswerShape(t *testing.T) {
	// A scalar, a scalar carrying found, and a grouped one each emit a DIFFERENT
	// body; a spec that validates for one and not the others would leave two of
	// the three unreachable.
	for _, fact := range []string{"TotalDeTurmas", "PorSituacao"} {
		r := validFactRange()
		r.Fact = fact
		if ps := Validate(factRangeSpec(r), Options{}); ps.HasBlockers() {
			t.Errorf("limiting %s should be accepted, got:\n%v", fact, ps.Error())
		}
	}
}

func TestFactRangeNeedsABound(t *testing.T) {
	r := validFactRange()
	r.Min, r.Max = nil, nil
	if ps := Validate(factRangeSpec(r), Options{}); !ps.HasBlockers() {
		t.Fatal("a rule that says nothing about the answer must be refused")
	}
}

func TestFactRangeNeedsSomewhereToLand(t *testing.T) {
	r := validFactRange()
	r.AttachTo = ""
	if ps := Validate(factRangeSpec(r), Options{}); !blockerAbout(ps, "attachTo") {
		t.Fatalf("a fact's answer is not a field, so attachTo is required, got:\n%v", ps.Error())
	}
	r.AttachTo = "Departamento"
	if ps := Validate(factRangeSpec(r), Options{}); !ps.HasBlockers() {
		t.Fatal("attachTo must name a field that exists")
	}
}

func TestFactRangeNamesAFactThatExists(t *testing.T) {
	r := validFactRange()
	r.Fact = "NaoDeclarado"
	if ps := Validate(factRangeSpec(r), Options{}); !ps.HasBlockers() {
		t.Fatal("limiting a fact nobody declared must be refused")
	}
	r.Fact = ""
	if ps := Validate(factRangeSpec(r), Options{}); !blockerAbout(ps, "fact") {
		t.Fatal("the rule must name the fact it limits")
	}
}

// TestFactRangeRefusesAnUncomparableFact keeps the pairing honest: a yes/no
// answer has nothing to compare against a bound, and a manual fact returning a
// bool is the same problem wearing another kind.
func TestFactRangeRefusesAnUncomparableFact(t *testing.T) {
	for _, fact := range []string{"TemAlguma", "AberturaDoCurso"} {
		r := validFactRange()
		r.Fact = fact
		if ps := Validate(factRangeSpec(r), Options{}); !ps.HasBlockers() {
			t.Errorf("limiting %s must be refused: there is no number to compare", fact)
		}
	}
}

func TestFactRangeNamesNoFields(t *testing.T) {
	r := validFactRange()
	r.Fields = []string{"Name"}
	if ps := Validate(factRangeSpec(r), Options{}); !blockerAbout(ps, "fields") {
		t.Fatalf("the subject is the fact, not a field — declaring both is refused, got:\n%v", ps.Error())
	}
}

func TestFactRangeWithoutAServiceIsRefused(t *testing.T) {
	s := factRangeSpec(validFactRange())
	s.Service = nil
	if ps := Validate(s, Options{}); !ps.HasBlockers() {
		t.Fatal("a rule reading a fact needs the service that answers it")
	}
}

// eqFilters is the Go spelling of `filters: [A, B]` — the bare-name form, which
// is an equality against a parameter and is what most of these tests are about.
// Written once here so a test that cares about the SHAPE of the tree says so by
// building the tree itself.
func eqFilters(names ...string) []FactFilter {
	out := make([]FactFilter, 0, len(names))
	for _, n := range names {
		out = append(out, FactFilter{Field: n})
	}
	return out
}
