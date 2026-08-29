package spec

import "testing"

// Three questions a fact could not ask, and one it could ask two ways.
//
//   - the ARCHIVED rows alone. `activeOnly` narrows to the living ones and its
//     absence admits both; there was no third answer, so "was this taken and
//     then withdrawn" was written by hand or not asked.
//   - the same question named for the PROBLEM. Spelled as `exists`, a fact has
//     to be named for the healthy state — and the generated test stub answers
//     "nothing found", which under that naming reads as the row being gone and
//     turns a correct spec red on the day it is written.
//   - a column another aggregate owns. The framework has always compiled a
//     declared root join into the existence probe and the aggregate calls, the
//     same way it does into FindAll; the generator was the half that could not
//     name the column, so the query was hand-written.
//
// And the one asked two ways: `activeOnly: true` and `scope: active` govern one
// gate, so declaring both is refused rather than reconciled.

// archivableFactSpec is factSpec plus the archive column and the modes that
// make it coherent — a soft delete on an entity that does not archive is
// refused, and that refusal would otherwise stand in for the one under test.
func archivableFactSpec(f Fact) *Spec {
	s := factSpec(f)
	s.Storage.Managed.ArchivedAt = "deleted_at"
	s.Delete.Root = "soft"
	s.Modes = append(s.Modes, "archive", "unarchive")
	s.Authz.Permissions["archive"] = "student:archive"
	s.Authz.Permissions["unarchive"] = "student:archive"
	return s
}

// TestScopeAcceptsEveryValueItDocuments. A vocabulary is only as good as the
// values it lets through, and a scope quietly refused sends the author to write
// the query by hand — which is the whole failure this key exists to end.
func TestScopeAcceptsEveryValueItDocuments(t *testing.T) {
	for _, sc := range FactScopes.List() {
		s := archivableFactSpec(Fact{Name: "F", Kind: "count", Scope: sc, Description: "d"})
		// The WHOLE spec must be clean, not merely free of a `.scope` blocker: an
		// assertion that only looks for one path passes just as happily when the
		// fixture is broken for an unrelated reason.
		if ps := Validate(s, Options{}); ps.HasBlockers() {
			t.Errorf("scope: %s is documented and refused:\n%v", sc, ps.Error())
		}
	}
}

func TestAnUnknownScopeIsRefusedWithTheWholeSet(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "F", Kind: "count", Scope: "archived", Description: "d",
	}), Options{})
	if !blockerAbout(ps, "is not a scope a fact may ask under") {
		t.Fatalf("an unknown scope must be refused:\n%v", ps.Error())
	}
	if !blockerAbout(ps, "archivedOnly") {
		t.Errorf("the refusal must print the whole set, so the author does not guess again:\n%v",
			ps.Error())
	}
}

// TestScopeAndActiveOnlyTogetherAreRefused. Reconciling them silently would run
// a query the author did not write, and the pair is easy to produce: activeOnly
// is the older spelling of exactly one of the three values.
func TestScopeAndActiveOnlyTogetherAreRefused(t *testing.T) {
	s := archivableFactSpec(Fact{
		Name: "F", Kind: "count", Scope: "all", ActiveOnly: true, Description: "d",
	})
	if ps := Validate(s, Options{}); !blockerAbout(ps, "both govern the archived gate") {
		t.Fatalf("declaring both spellings must be refused:\n%v", ps.Error())
	}
}

// TestArchivedScopeNeedsAnArchiveColumn is the refusal that keeps the key
// honest. With no marker column the framework applies NO gate under any scope,
// so `archivedOnly` would answer about EVERY row rather than about none — a
// query that runs, returns, and means the opposite of what it says.
func TestArchivedScopeNeedsAnArchiveColumn(t *testing.T) {
	ps := Validate(factSpec(Fact{
		Name: "F", Kind: "count", Scope: "archivedOnly", Description: "d",
	}), Options{})
	if !blockerAbout(ps, "no archive column") {
		t.Fatalf("archivedOnly over an entity with no archive column must be refused:\n%v",
			ps.Error())
	}
}

// TestAManualFactHasNoScopeToDeclare. It writes no query, so the archived gate
// describes something the generator is not emitting — the same reason
// activeOnly is already refused there.
func TestAManualFactHasNoScopeToDeclare(t *testing.T) {
	s := archivableFactSpec(Fact{
		Name: "F", Kind: "manual", Returns: "bool", Scope: "active", Description: "d",
	})
	if ps := Validate(s, Options{}); !blockerAbout(ps, "not writing") {
		t.Fatalf("a manual fact must not declare a scope:\n%v", ps.Error())
	}
}

// TestNotExistsIsAcceptedAndAnswersNoNumber. It is `exists` with the reading
// inverted, so everything true of one because it answers a bool is true of the
// other: no aggregated field, nothing to group, nothing for a range to compare.
func TestNotExistsIsAcceptedAndAnswersNoNumber(t *testing.T) {
	if ps := Validate(factSpec(Fact{
		Name: "SemDuplicata", Kind: "notExists",
		Filters: []FactFilter{{Field: "Status"}}, Description: "d",
	}), Options{}); ps.HasBlockers() {
		t.Fatalf("notExists must be accepted:\n%v", ps.Error())
	}
}

func TestNotExistsRefusesWhatExistsRefuses(t *testing.T) {
	for _, tc := range []struct {
		name string
		bend func(*Fact)
		want string
	}{
		{"a declared return type", func(f *Fact) { f.Returns = "bool" }, "already determines what it returns"},
		{"a grouping", func(f *Fact) { f.GroupBy = []string{"Status"} }, "nothing to report per group"},
	} {
		f := Fact{Name: "F", Kind: "notExists", Description: "d"}
		tc.bend(&f)
		if ps := Validate(factSpec(f), Options{}); !blockerAbout(ps, tc.want) {
			t.Errorf("notExists with %s must be refused like exists is:\n%v", tc.name, ps.Error())
		}
	}
}

// TestARangeOverNotExistsIsRefused. A range compares a number, and this answers
// yes or no — and the refusal must NAME the kind rather than say "exists",
// which would send the author looking for a fact they did not write.
func TestARangeOverNotExistsIsRefused(t *testing.T) {
	s := factSpec(Fact{Name: "SemDuplicata", Kind: "notExists", Description: "d"})
	s.Rules = Rules{List: []Rule{{
		ID: "teto", Kind: "factRange", Fact: "SemDuplicata", Max: f64(5),
		AttachTo: "Name", Notification: "TetoNotification", Scope: []string{"insert"},
	}}}
	s.Notifications = []Notification{{
		Name: "TetoNotification", Semantic: "validation", Package: "domain",
		Text: Texts{PTBR: "a", ENG: "a", ESP: "a", FRA: "a", DEU: "a", ITA: "a", NLD: "a"},
	}}
	ps := Validate(s, Options{})
	if !blockerAbout(ps, "notExists answers yes or no") {
		t.Fatalf("a range over notExists must be refused, naming the kind:\n%v", ps.Error())
	}
}

func f64(v float64) *float64 { return &v }

// TestAFactMayFilterByAJoinedField is the capability itself. The framework
// compiles a ROOT join into the probe exactly as it does into FindAll — it even
// types an identity column across the join leg so the bind is the dialect's
// native id form rather than text that matches nothing on three engines — and
// the generator used to answer "unknown field".
func TestAFactMayFilterByAJoinedField(t *testing.T) {
	s := joiningSpec()
	s.Service = &Service{Required: true, Facts: []Fact{{
		Name: "HomonimoNoMesmoCampus", Kind: "exists",
		Filters:     []FactFilter{{Field: "Name"}, {Field: "CampusName"}},
		Description: "Se outro aluno de mesmo nome está no campus com este rótulo.",
	}}}
	if ps := Validate(s, joinOpts()); ps.HasBlockers() {
		t.Fatalf("a fact narrowed by a root join's field must be accepted:\n%v", ps.Error())
	}
	if cov := CheckCoverage(s); cov.HasBlockers() {
		t.Fatalf("a fact over a joined field must be a covered capability:\n%v", cov.Error())
	}
}

// TestAFactMayNotFilterByAChildJoinsField. A child join rides the collection's
// own batched SELECT and never reaches a predicate — the framework says so in
// as many words, and the same boundary applies to every other child field.
func TestAFactMayNotFilterByAChildJoinsField(t *testing.T) {
	s := joiningSpec()
	s.Children = []Child{{
		Name: "Guardian", Plural: "Guardians", Table: "student_guardians",
		ParentColumn: "student_id", Description: "Responsáveis.", OwnedBy: "root",
		EditStrategy: "atomic-replace", BusinessIdentity: []string{"Document"},
		Fields: []Field{{
			Name: "Document", Type: "string", Column: "document", Length: 20,
			Example: "PA-1", Description: "O documento.",
		}, {
			Name: "CampusID", Type: "id", Column: "campus_id",
			Example: "1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4", Description: "O campus.",
		}},
	}}
	s.Joins = append(s.Joins, Join{
		Kind: "inner", To: "Campus", On: "campus_id", InChild: "Guardians",
		Fields: []JoinField{{
			Name: "GuardianCampusName", Column: "label", Description: "Nome do campus.",
		}},
	})
	s.Service = &Service{Required: true, Facts: []Fact{{
		Name: "F", Kind: "exists",
		Filters:     []FactFilter{{Field: "GuardianCampusName"}},
		Description: "d",
	}}}
	ps := Validate(s, joinOpts())
	if !blockerAbout(ps, "a child join is load-only") {
		t.Fatalf("a fact must not filter by a child join's field:\n%v", ps.Error())
	}
}

// TestFactRangeMayNotFillAJoinedArgument. The field is ON the entity, so
// `e.<Field>` compiles — and on an INSERT the traversal has not run, so what it
// passes is the zero value. Same silence the stamped columns are refused for,
// arriving through a different door.
func TestFactRangeMayNotFillAJoinedArgument(t *testing.T) {
	s := joiningSpec()
	s.Service = &Service{Required: true, Facts: []Fact{{
		Name: "NoMesmoCampus", Kind: "count",
		Filters: []FactFilter{{Field: "CampusName"}}, Description: "d",
	}}}
	s.Rules = Rules{List: []Rule{{
		ID: "teto", Kind: "factRange", Fact: "NoMesmoCampus", Max: f64(5),
		AttachTo: "Name", Notification: "TetoNotification", Scope: []string{"insert"},
	}}}
	s.Notifications = []Notification{{
		Name: "TetoNotification", Semantic: "validation", Package: "domain",
		Text: Texts{PTBR: "a", ENG: "a", ESP: "a", FRA: "a", DEU: "a", ITA: "a", NLD: "a"},
	}}
	ps := Validate(s, joinOpts())
	if !blockerAbout(ps, "until it has been LOADED") {
		t.Fatalf("a declarative range must not fill an argument from a joined field:\n%v",
			ps.Error())
	}
}

// TestASetParameterIsNamedForASet keeps the signature honest. `in` takes a
// SLICE, and the parameter was named after the field in the singular — so the
// one place a reader looks to find out whether they are asking about one thing
// or many said "one" while the type said the opposite.
func TestASetParameterIsNamedForASet(t *testing.T) {
	if got := (FactFilter{Field: "Status", Op: "in"}).ParamName(); got != "statusSet" {
		t.Errorf("a set leaf must name a set, got %q", got)
	}
	if got := (FactFilter{Field: "Status"}).ParamName(); got != "status" {
		t.Errorf("a single-value leaf must keep the field's own name, got %q", got)
	}
	// The escape hatch still wins: `as` is the author's word.
	if got := (FactFilter{Field: "Status", Op: "in", As: "situacoes"}).ParamName(); got != "situacoes" {
		t.Errorf("`as` must override the suffix, got %q", got)
	}
	// And a per-entry leaf still sheds the collection, set or not.
	if got := (FactFilter{Field: "Permissoes.PermissaoID", Op: "in"}).ParamName(); got != "permissaoIDSet" {
		t.Errorf("a per-entry set leaf must shed the collection and name a set, got %q", got)
	}
	// Nothing in, nothing out: a suffix on an empty name would produce a
	// parameter called "Set", which names nothing.
	if got := SetParamName(""); got != "" {
		t.Errorf("an empty name must stay empty, got %q", got)
	}
}
