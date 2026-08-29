package spec

import "testing"

// A fact may now answer SEVERAL numbers, and be narrowed by columns the
// framework stamps. Both widen what a query can say, and both open shapes that
// compile and mean something other than what was written — which is what every
// case here refuses.

// aggFactSpec is factSpec with a NULLABLE numeric column, because half of what
// a multi-answer fact gets wrong is only visible over one.
func aggFactSpec(f Fact) *Spec {
	s := factSpec(f)
	s.Fields = append(s.Fields, Field{
		Name: "Bonus", Type: "float64", Column: "bonus", LivesOn: "root", Nullable: true,
		Example: "1.5", Description: "Bônus, quando houver.",
	})
	return s
}

func TestSeveralAggregatesValidateTogether(t *testing.T) {
	s := aggFactSpec(Fact{
		Name: "Carga", Description: "Quantos e quanto.",
		Aggregates: []FactAggregate{
			{Kind: "count", As: "Quantos"},
			{Kind: "sum", Field: "Credits", As: "Creditos"},
			{Kind: "avg", Field: "Bonus", As: "BonusMedio"},
		},
		GroupBy: []string{"Status"},
	})
	if ps := Validate(s, Options{}); ps.HasBlockers() {
		t.Fatalf("a multi-aggregate fact should validate cleanly:\n%v", ps.Error())
	}
}

// TestOneShapeOrTheOther refuses a fact asking for two answers from one method.
func TestOneShapeOrTheOther(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "Carga", Kind: "count", Description: "d",
		Aggregates: []FactAggregate{
			{Kind: "count", As: "Quantos"},
			{Kind: "sum", Field: "Credits", As: "Creditos"},
		},
	}), Options{})
	if !blockerAbout(ps, "two different answers") {
		t.Fatalf("kind and aggregates together must be refused, got:\n%v", ps.Error())
	}
}

// TestAggregateEntriesAreHeldToTheSameBar walks the refusals that are about ONE
// entry: what it computes, what it computes over, and what it is called.
func TestAggregateEntriesAreHeldToTheSameBar(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []FactAggregate
		want    string
	}{
		{"a single entry is kind:", []FactAggregate{{Kind: "count", As: "Quantos"}},
			"which is what kind: says"},
		{"exists is not an aggregate",
			[]FactAggregate{{Kind: "exists", As: "Existe"}, {Kind: "count", As: "Quantos"}},
			"is not something this list can compute"},
		{"an entry with no name",
			[]FactAggregate{{Kind: "count"}, {Kind: "sum", Field: "Credits", As: "Creditos"}},
			"becomes a field of the fact's answer"},
		{"a lower-case name",
			[]FactAggregate{{Kind: "count", As: "quantos"}, {Kind: "sum", Field: "Credits", As: "Creditos"}},
			"it is a Go struct field"},
		{"two entries under one name",
			[]FactAggregate{{Kind: "min", Field: "Grade", As: "Nota"}, {Kind: "max", Field: "Grade", As: "Nota"}},
			"already answers under the name"},
		{"count over a column",
			[]FactAggregate{{Kind: "count", Field: "Credits", As: "Quantos"}, {Kind: "sum", Field: "Credits", As: "Creditos"}},
			"count counts ROWS"},
		{"an aggregate with no column",
			[]FactAggregate{{Kind: "sum", As: "Creditos"}, {Kind: "count", As: "Quantos"}},
			"needs the field it aggregates"},
		{"a column with no carrier",
			[]FactAggregate{{Kind: "max", Field: "Status", As: "Ultimo"}, {Kind: "count", As: "Quantos"}},
			"cannot aggregate Status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := Validate(aggFactSpec(Fact{
				Name: "Carga", Description: "d", Aggregates: tc.entries,
			}), Options{})
			if !blockerAbout(ps, tc.want) {
				t.Fatalf("expected a refusal about %q, got:\n%v", tc.want, ps.Error())
			}
		})
	}
}

// TestAnAggregateNameCannotCollideWithAGroupingKey pins the one collision that
// is invisible in the spec: the keys and the numbers share the answer's struct.
func TestAnAggregateNameCannotCollideWithAGroupingKey(t *testing.T) {
	ps := Validate(aggFactSpec(Fact{
		Name: "Carga", Description: "d", GroupBy: []string{"Status"},
		Aggregates: []FactAggregate{
			{Kind: "count", As: "Status"},
			{Kind: "sum", Field: "Credits", As: "Creditos"},
		},
	}), Options{})
	if !blockerAbout(ps, "the grouping key Status already answers") {
		t.Fatalf("a name shared with a grouping key must be refused, got:\n%v", ps.Error())
	}
}

// TestARangeBoundsOneNumber is the other half of a multi-answer fact: a rule
// says which number it limits, because a generator picking one is a generator
// enforcing a rule nobody wrote.
func TestARangeBoundsOneNumber(t *testing.T) {
	withRule := func(fact string) *Spec {
		s := aggFactSpec(Fact{
			Name: "Carga", Description: "Quantos e quanto.",
			Aggregates: []FactAggregate{
				{Kind: "count", As: "Quantos"},
				{Kind: "sum", Field: "Credits", As: "Creditos"},
			},
		})
		s.Notifications = append(s.Notifications, Notification{
			Name: "DemaisNotification", Semantic: "validation", Text: sevenTexts("Demais."),
		})
		s.Rules.List = append(s.Rules.List, Rule{
			ID: "teto", Kind: "factRange", Scope: []string{"insert"}, Fact: fact,
			Max: ptrFloat(50), AttachTo: "Status", Notification: "DemaisNotification",
		})
		return s
	}

	if ps := Validate(withRule("Carga.Creditos"), Options{}); ps.HasBlockers() {
		t.Fatalf("naming the number must validate:\n%v", ps.Error())
	}
	if ps := Validate(withRule("Carga"), Options{}); !blockerAbout(ps, "answers several numbers") {
		t.Fatalf("a bare name over a multi-answer fact must be refused, got:\n%v", ps.Error())
	}
	if ps := Validate(withRule("Carga.Inexistente"), Options{}); !blockerAbout(ps, "answers no number called") {
		t.Fatalf("an unknown slot must be refused with the list, got:\n%v", ps.Error())
	}
	// And the reverse: reaching inside a fact that answers one number.
	single := factSpec(Fact{Name: "Total", Kind: "count", Description: "d"})
	single.Notifications = append(single.Notifications, Notification{
		Name: "DemaisNotification", Semantic: "validation", Text: sevenTexts("Demais."),
	})
	single.Rules.List = append(single.Rules.List, Rule{
		ID: "teto", Kind: "factRange", Scope: []string{"insert"}, Fact: "Total.Value",
		Max: ptrFloat(50), AttachTo: "Status", Notification: "DemaisNotification",
	})
	if ps := Validate(single, Options{}); !blockerAbout(ps, "nothing to reach inside") {
		t.Fatalf("a dotted name over a single-answer fact must be refused, got:\n%v", ps.Error())
	}
}

// TestStampedColumnsAreFilterable is the capability itself: the three columns
// the framework owns are addressable by their fixed logical names, and only
// where the storage declares them.
func TestStampedColumnsAreFilterable(t *testing.T) {
	ok := factSpec(Fact{
		Name: "Desde", Kind: "count", Description: "d",
		Filters: []FactFilter{{Field: "CreatedAt", Op: "gte", As: "desde"}},
	})
	if ps := Validate(ok, Options{}); ps.HasBlockers() {
		t.Fatalf("a stamped column the storage declares must be filterable:\n%v", ps.Error())
	}

	// DeletedAt is not declared by the minimal fixture's storage.
	missing := factSpec(Fact{
		Name: "Arquivados", Kind: "count", Description: "d",
		Filters: []FactFilter{{Field: "DeletedAt", Op: "notnull"}},
	})
	ps := Validate(missing, Options{})
	if !blockerAbout(ps, "storage.managed.archivedAt") {
		t.Fatalf("the refusal must name the key that declares the column, got:\n%v", ps.Error())
	}
}

// TestTheArchivedScopeAndDeletedAtDoNotArgue refuses the two readings that both
// ship a query that runs and answers nothing anybody asked for.
func TestTheArchivedScopeAndDeletedAtDoNotArgue(t *testing.T) {
	archivable := func(f Fact) *Spec {
		s := factSpec(f)
		s.Storage.Managed.ArchivedAt = "deleted_at"
		return s
	}
	contradiction := archivable(Fact{
		Name: "Arquivados", Kind: "count", Description: "d", ActiveOnly: true,
		Filters: []FactFilter{{Field: "DeletedAt", Op: "notnull"}},
	})
	if ps := Validate(contradiction, Options{}); !blockerAbout(ps, "together they match nothing") {
		t.Fatalf("activeOnly beside DeletedAt notnull must be refused, got:\n%v", ps.Error())
	}
	redundant := archivable(Fact{
		Name: "Vivos", Kind: "count", Description: "d", ActiveOnly: true,
		Filters: []FactFilter{{Field: "DeletedAt", Op: "isnull"}},
	})
	if ps := Validate(redundant, Options{}); !blockerAbout(ps, "asks it a second time") {
		t.Fatalf("activeOnly beside DeletedAt isnull must be refused, got:\n%v", ps.Error())
	}
}

// TestADeclarativeRuleCannotFillAStampedColumn is the third thing factRange
// cannot pass, beside a set and an absent composite: a value the entity does
// not carry because the framework owns it.
func TestADeclarativeRuleCannotFillAStampedColumn(t *testing.T) {
	s := factSpec(Fact{
		Name: "Desde", Kind: "count", Description: "d",
		Filters: []FactFilter{{Field: "CreatedAt", Op: "gte", As: "desde"}},
	})
	s.Notifications = append(s.Notifications, Notification{
		Name: "DemaisNotification", Semantic: "validation", Text: sevenTexts("Demais."),
	})
	s.Rules.List = append(s.Rules.List, Rule{
		ID: "teto", Kind: "factRange", Scope: []string{"insert"}, Fact: "Desde",
		Max: ptrFloat(50), AttachTo: "Status", Notification: "DemaisNotification",
	})
	ps := Validate(s, Options{})
	if !blockerAbout(ps, "the entity carries no such field") {
		t.Fatalf("a rule cannot fill a stamped column, got:\n%v", ps.Error())
	}
	if !blockerAbout(ps, "rules.manual") {
		t.Fatalf("the fix must point at the hand-written call, got:\n%v", ps.Error())
	}
}
