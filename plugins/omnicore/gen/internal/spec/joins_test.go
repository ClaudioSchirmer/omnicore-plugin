package spec

import (
	"strings"
	"testing"
)

// campusNeighbour is the target a joining spec traverses INTO — the shape the
// generator reads out of another spec of the same project.
func campusNeighbour() Neighbour {
	return Neighbour{
		Path: "specs/omnicore-gen/campus.omnicore.yaml", Entity: "Campus",
		ViewName: "campi", Route: "Campi", Table: "campi",
		Fields: []NeighbourField{
			{Name: "CampusLabel", Column: "label", Type: "string", LivesOn: "root"},
			{Name: "BudgetCode", Column: "budget_code", Type: "string", LivesOn: "root"},
		},
	}
}

// joiningSpec is the minimal spec that reaches into Campus: a non-nullable
// foreign key on the root, and one field mapped across it.
func joiningSpec() *Spec {
	s := minimalSpec()
	s.Fields = append(s.Fields, Field{
		Name: "CampusID", Type: "id", Column: "campus_id", LivesOn: "root",
		Example: "1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4", Description: "O campus.",
	})
	s.Joins = []Join{{
		Kind: "inner", To: "Campus", On: "campus_id",
		Fields: []JoinField{{Name: "CampusName", Column: "label", Description: "Nome do campus."}},
	}}
	return s
}

func joinOpts() Options { return Options{Neighbours: []Neighbour{campusNeighbour()}} }

// TestJoiningSpecIsClean guards the fixture: every "X is refused" assertion
// below is vacuous if the baseline already refuses.
func TestJoiningSpecIsClean(t *testing.T) {
	ps := Validate(joiningSpec(), joinOpts())
	if ps.HasBlockers() {
		t.Fatalf("the baseline join should validate cleanly, got:\n%v", ps.Error())
	}
	if cov := CheckCoverage(joiningSpec()); cov.HasBlockers() {
		t.Fatalf("read joins should be a covered capability, got:\n%v", cov.Error())
	}
}

// TestInnerJoinOverANullableKeyIsRefused is the refusal that matters most.
//
// The declaration lives on the repository, so it applies to FindByID too — the
// load the write-side handlers go through. Over a nullable key an inner join
// drops aggregates in silence and a legitimate write comes back 404 on its own
// record. The framework panics at construction; this catches it in the file.
func TestInnerJoinOverANullableKeyIsRefused(t *testing.T) {
	s := joiningSpec()
	s.Fields[len(s.Fields)-1].Nullable = true
	ps := Validate(s, joinOpts())
	if !ps.HasBlockers() {
		t.Fatal("an inner join over a nullable foreign key must be refused")
	}
	if !strings.Contains(ps.Error().Error(), "left") {
		t.Errorf("the refusal must name the fix; got:\n%v", ps.Error())
	}
}

func TestLeftJoinOverANullableKeyIsAccepted(t *testing.T) {
	s := joiningSpec()
	s.Fields[len(s.Fields)-1].Nullable = true
	s.Joins[0].Kind = "left"
	if ps := Validate(s, joinOpts()); ps.HasBlockers() {
		t.Fatalf("a left join is legal over any foreign key, got:\n%v", ps.Error())
	}
}

func TestJoinRefusesWhatTheFrameworkWouldPanicOn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mut   func(*Spec)
		names string
	}{
		{"a foreign key the joining table does not have",
			func(s *Spec) { s.Joins[0].On = "nope_id" }, "nope_id"},
		{"a foreign key that is not an id",
			func(s *Spec) { s.Joins[0].On = "name" }, "id"},
		{"a column the target does not have",
			func(s *Spec) { s.Joins[0].Fields[0].Column = "nope" }, "nope"},
		{"a Go name the entity already answers",
			func(s *Spec) { s.Joins[0].Fields[0].Name = "Name" }, "Name"},
		{"a join that maps no column",
			func(s *Spec) { s.Joins[0].Fields = nil }, "reaches nothing"},
		{"a kind outside the closed set",
			func(s *Spec) { s.Joins[0].Kind = "outer" }, "outer"},
		{"a collection this spec does not declare",
			func(s *Spec) { s.Joins[0].InChild = "Guardian" }, "Guardian"},
		{"a type that contradicts the target's own declaration",
			func(s *Spec) { s.Joins[0].Fields[0].Type = "int" }, "string"},
		{"a traversal onto itself",
			func(s *Spec) { s.Joins[0].To = "Student" }, "itself"},
		{"a domain type on a join field",
			func(s *Spec) { s.Joins[0].Fields[0].Type = "id" }, "no domain type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := joiningSpec()
			tc.mut(s)
			ps := Validate(s, joinOpts())
			if !ps.HasBlockers() {
				t.Fatal("this must be refused before the boot pays for it")
			}
			if !strings.Contains(ps.Error().Error(), tc.names) {
				t.Errorf("the refusal must name %q; got:\n%v", tc.names, ps.Error())
			}
		})
	}
}

// TestOneForeignKeyReachesOneTable pins the alias rule: the framework derives a
// traversal's SQL alias from its foreign key, so two joins sharing one key
// collide on it.
func TestOneForeignKeyReachesOneTable(t *testing.T) {
	s := joiningSpec()
	s.Joins = append(s.Joins, Join{
		Kind: "inner", To: "Campus", On: "campus_id",
		Fields: []JoinField{{Name: "CampusCode", Column: "budget_code"}},
	})
	if ps := Validate(s, joinOpts()); !ps.HasBlockers() {
		t.Fatal("two traversals over one foreign key must be refused")
	}
}

func TestTwoJoinsCannotLandOnOneGoField(t *testing.T) {
	s := joiningSpec()
	s.Fields = append(s.Fields, Field{
		Name: "OtherCampusID", Type: "id", Column: "other_campus_id", LivesOn: "root",
		Example: "1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4", Description: "Outro campus.",
	})
	s.Joins = append(s.Joins, Join{
		Kind: "inner", To: "Campus", On: "other_campus_id",
		Fields: []JoinField{{Name: "CampusName", Column: "label"}},
	})
	if ps := Validate(s, joinOpts()); !ps.HasBlockers() {
		t.Fatal("two traversals cannot fill one Go field")
	}
}

// TestAHandWrittenTargetIsAcceptedOnTheAuthorsWord covers the escape hatch: a
// project that adopted this generator midway has aggregates it does not own, and
// "you may not reach that one" would be a limit of the tool sold as a limit of
// the framework. The price is that each field states its own type.
func TestAHandWrittenTargetIsAcceptedOnTheAuthorsWord(t *testing.T) {
	s := joiningSpec()
	s.Joins[0].Fields[0].Type = "string"

	ps := Validate(s, Options{}) // no neighbours: the target is invisible
	if ps.HasBlockers() {
		t.Fatalf("an unseen target with an explicit type must be accepted, got:\n%v", ps.Error())
	}
	if len(ps.Warnings()) == 0 {
		t.Error("it must not be accepted SILENTLY — the unchecked half has to be said out loud")
	}
}

func TestAnUnseenTargetDemandsAnExplicitType(t *testing.T) {
	ps := Validate(joiningSpec(), Options{}) // the field states no type
	if !ps.HasBlockers() {
		t.Fatal("with nothing to derive a type from, the type must be demanded")
	}
}

// TestAJoinedIdentityCrossesAsText pins the shape the framework demands: a join
// field carries no domain type, so an identity column lands as plain text — and
// the pointer follows from what can be ABSENT, which is why a NULLABLE identity
// is nullable text even under an INNER join.
func TestAJoinedIdentityCrossesAsText(t *testing.T) {
	target := campusNeighbour()
	target.Fields = append(target.Fields,
		NeighbourField{Name: "OwnerID", Column: "owner_id", Type: "id", LivesOn: "root"},
		NeighbourField{Name: "AuditorID", Column: "auditor_id", Type: "id", LivesOn: "root", Nullable: true})

	s := joiningSpec()
	s.Joins[0].Fields = append(s.Joins[0].Fields,
		JoinField{Name: "CampusOwnerID", Column: "owner_id"},
		JoinField{Name: "CampusAuditorID", Column: "auditor_id"})

	opts := Options{Neighbours: []Neighbour{target}}
	if ps := Validate(s, opts); ps.HasBlockers() {
		t.Fatalf("joining onto an identity column is legal, got:\n%v", ps.Error())
	}

	jr := joinReachOf(s, opts)
	for _, tc := range []struct {
		field    string
		nullable bool
	}{{"CampusOwnerID", false}, {"CampusAuditorID", true}} {
		f := fieldNamedIn(jr.root, tc.field)
		if f == nil {
			t.Fatalf("%s did not resolve on the read side", tc.field)
		}
		if f.Type != "string" {
			t.Errorf("%s must cross as text, got %q", tc.field, f.Type)
		}
		if f.Nullable != tc.nullable {
			t.Errorf("%s nullable = %v, want %v", tc.field, f.Nullable, tc.nullable)
		}
	}
}

// TestAJoinFieldIsFilterableAndSortable proves the read side can name it: a root
// join rides the root SELECT, so the store compares and orders by it like a
// column of the table.
func TestAJoinFieldIsFilterableAndSortable(t *testing.T) {
	s := joiningSpec()
	s.Read.ByParams = &ByParams{
		Filters:  []Filter{{Field: "CampusName", Ops: []string{"eq", "contains"}}},
		Sort:     []string{"CampusName"},
		Controls: Controls{OrderBy: true},
	}
	if ps := Validate(s, joinOpts()); ps.HasBlockers() {
		t.Fatalf("a root join's field is addressable in a criteria, got:\n%v", ps.Error())
	}
}

// TestAJoinFieldIsNotFilterableOnAMongoBacking is the other half of the same
// rule: a projection is composed from the TableSchema, which a join never
// enters, so there is no column there to filter on.
func TestAJoinFieldIsNotFilterableOnAMongoBacking(t *testing.T) {
	s := joiningSpec()
	s.Read.Backing = "mongo"
	s.Read.View.Version = 1
	s.Read.ByParams = &ByParams{
		Filters: []Filter{{Field: "CampusName", Ops: []string{"eq"}}},
	}
	if ps := Validate(s, joinOpts()); !ps.HasBlockers() {
		t.Fatal("a joined field cannot be filtered on a backing that does not carry it")
	}
}

// TestAMongoBackedJoinIsWarnedAboutOnlyWhenItLooksLikeAReadRequest: a traversal
// whose every field is hidden was declared for the rules, and telling its author
// about a view they never asked to reach is noise.
func TestAMongoBackedJoinIsWarnedAboutOnlyWhenItLooksLikeAReadRequest(t *testing.T) {
	visible := joiningSpec()
	visible.Read.Backing = "mongo"
	visible.Read.View.Version = 1
	if len(warningsAt(Validate(visible, joinOpts()), "joins")) == 0 {
		t.Error("a SERVED join field on a Mongo backing reaches the rules and not the view — say so")
	}

	hidden := joiningSpec()
	hidden.Read.Backing = "mongo"
	hidden.Read.View.Version = 1
	hidden.Joins[0].Fields[0].Hidden = true
	if w := warningsAt(Validate(hidden, joinOpts()), "joins"); len(w) > 0 {
		t.Errorf("a rules-only traversal needs no warning about the view: %v", w)
	}
}

// TestAnUnseenTargetsNullableColumnIsDeclarable is the gap `nullable` closes.
//
// The pointer does not follow from the join kind: an INNER join proves the
// joined ROW exists, never that every column of it is filled. When the target is
// a spec of this project that is read off its declaration — but a hand-written
// aggregate is invisible here, and without a key to say it the generator emitted
// a non-pointer field that the framework refuses at repository construction. A
// boot to find what the file could have said.
func TestAnUnseenTargetsNullableColumnIsDeclarable(t *testing.T) {
	s := joiningSpec()
	s.Joins[0].Fields[0].Type = "string"
	s.Joins[0].Fields[0].Nullable = true
	// No neighbours: Campus is hand-written as far as this run can tell.
	if ps := Validate(s, Options{}); ps.HasBlockers() {
		t.Fatalf("nullable must be declarable for an unseen target, got:\n%v", ps.Error())
	}
}

// TestNullableIsRefusedWhenTheTargetAlreadyAnswersIt keeps ONE source of truth.
//
// The target's own spec says whether the column is nullable. A second copy on
// this side is a copy that goes stale the first time the target changes and this
// file is not reopened — the same reason `type` is refused when it can be
// derived.
func TestNullableIsRefusedWhenTheTargetAlreadyAnswersIt(t *testing.T) {
	s := joiningSpec()
	s.Joins[0].Fields[0].Nullable = true // but Campus declares `label` NOT NULL
	ps := Validate(s, joinOpts())
	if !ps.HasBlockers() {
		t.Fatal("restating nullability a visible target already declares must be refused")
	}
	if !strings.Contains(ps.Error().Error(), "leave nullable out") {
		t.Errorf("the refusal must name the fix; got:\n%v", ps.Error())
	}
}

// TestAChildJoinMayReuseARootFieldName holds the generator to the framework's
// OWN namespace rule rather than a stricter invented one.
//
// read.WithJoins checks `owner.Resolve`, and the owner of a child join is the
// COLLECTION's schema — which reaches the collection's columns and its facets
// and stops there. A collection does not resolve the root's fields, and the two
// entries are separate structs in the emitted Go, so a name used on both is
// legal. Refusing it would be this generator presenting its own rule as the
// framework's.
func TestAChildJoinMayReuseARootFieldName(t *testing.T) {
	s := joiningSpec()
	s.Fields = append(s.Fields, Field{
		Name: "CityName", Type: "string", Column: "city_name", LivesOn: "root",
		Length: 80, Example: "Porto Alegre", Description: "A cidade do aluno.",
	})
	s.Children = []Child{{
		Name: "Guardiao", Plural: "Guardioes", Table: "guardioes", OwnedBy: "root",
		Description:      "Os responsáveis pelo aluno.",
		ParentColumn:     "matricula_id",
		EditStrategy:     "atomic-replace",
		BusinessIdentity: []string{"CampusID"},
		Fields: []Field{{
			Name: "CampusID", Type: "id", Column: "campus_id",
			Example: "1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4", Description: "O campus.",
		}},
	}}
	// The entry reaches Campus and lands the value on a field the ROOT also
	// declares — a different struct, a different namespace.
	s.Joins = append(s.Joins, Join{
		Kind: "left", To: "Campus", On: "campus_id", InChild: "Guardiao",
		Fields: []JoinField{{Name: "CityName", Column: "label", Description: "A cidade."}},
	})
	if ps := Validate(s, joinOpts()); ps.HasBlockers() {
		t.Fatalf("a child join may carry a name the root also uses, got:\n%v", ps.Error())
	}
}

// TestAJoinFieldStillCannotShadowItsOwnTable is the other half of the rule
// above: relaxing the ROOT's namespace must not relax the OWNER's.
func TestAJoinFieldStillCannotShadowItsOwnTable(t *testing.T) {
	s := joiningSpec()
	s.Joins[0].Fields[0].Name = "CampusID" // already a field of the root, which owns this join
	ps := Validate(s, joinOpts())
	if !ps.HasBlockers() {
		t.Fatal("a join field must not shadow one the joining table declares")
	}
}

// TestReservedViewSuffixMatchesTheFrameworksRule keeps the generator's copy
// exact at the edge.
//
// The framework's query.ReservedNameSuffixProblem is a HasSuffix test. A length
// guard here would let the degenerate name `__0` through the generator and into
// a boot the framework aborts — and a copy that answers differently at the edge
// is worse than no copy, because the author trusts it.
func TestReservedViewSuffixMatchesTheFrameworksRule(t *testing.T) {
	for _, name := range []string{"users__0", "users__1", "__0", "__1"} {
		if ReservedViewSuffix(name) == "" {
			t.Errorf("%q ends in a blue-green slot suffix and must be refused", name)
		}
	}
	for _, name := range []string{"users", "users__2", "u__0s", ""} {
		if why := ReservedViewSuffix(name); why != "" {
			t.Errorf("%q is a legal read-model name, got refused: %s", name, why)
		}
	}
}

// campusThatStamps is the same target with its framework-managed columns
// visible — the shape discoverSpecs reads out of `storage.managed`, where those
// columns are declared BY PRESENCE and never appear under fields[].
func campusThatStamps() Neighbour {
	n := campusNeighbour()
	n.Revision = "revision"
	n.Fields = append(n.Fields,
		NeighbourField{Name: "CreatedAt", Column: "created_at", Type: "time", LivesOn: "root"},
		NeighbourField{Name: "UpdatedAt", Column: "updated_at", Type: "time", LivesOn: "root"},
		NeighbourField{Name: "DeletedAt", Column: "deleted_at", Type: "time", LivesOn: "root", Nullable: true})
	return n
}

// TestAJoinReachesTheTargetsStampedColumns is the gap this file was refusing to
// let anyone cross.
//
// The framework accepts it: whatever columns the target registers in its managed
// slots are resolved on the read path under fixed logical names, and
// read.WithJoins checks a mapped column against exactly that. The generator saw
// only fields[], so it answered "not a column of X" about a column that is one —
// a refusal an author cannot act on, because the target's declaration is already
// right. The columns below are this FIXTURE's; the framework fixes the slot, and
// each spec names its own column.
//
// The nullability is the half only this side can get right. An INNER join proves
// the joined ROW exists, never that every column of it is filled, and "not
// archived" is the NORMAL state of a row — so the archive column has to land in
// a pointer or the scan fails on the first active row.
func TestAJoinReachesTheTargetsStampedColumns(t *testing.T) {
	s := joiningSpec()
	s.Joins[0].Fields = append(s.Joins[0].Fields,
		JoinField{Name: "CampusCreatedAt", Column: "created_at", Description: "Quando o campus entrou."},
		JoinField{Name: "CampusUpdatedAt", Column: "updated_at", Description: "Quando mudou."},
		JoinField{Name: "CampusArchivedAt", Column: "deleted_at", Description: "Quando foi arquivado."})

	opts := Options{Neighbours: []Neighbour{campusThatStamps()}}
	if ps := Validate(s, opts); ps.HasBlockers() {
		t.Fatalf("the framework resolves these columns on the read path, got:\n%v", ps.Error())
	}

	jr := joinReachOf(s, opts)
	for _, tc := range []struct {
		field    string
		nullable bool
	}{
		{"CampusCreatedAt", false},
		{"CampusUpdatedAt", false},
		{"CampusArchivedAt", true},
	} {
		f := fieldNamedIn(jr.root, tc.field)
		if f == nil {
			t.Fatalf("%s did not resolve on the read side", tc.field)
		}
		if f.Type != "time" {
			t.Errorf("%s must cross as a timestamp, got %q", tc.field, f.Type)
		}
		if f.Nullable != tc.nullable {
			t.Errorf("%s nullable = %v, want %v — an archive column is NULL on every "+
				"active row, and a non-pointer field cannot hold that",
				tc.field, f.Nullable, tc.nullable)
		}
	}
}

// TestAChildJoinReachesTheStampedColumnsToo: the traversal reads the same target
// declaration whichever table it hangs off, so nothing about the collection case
// may weaken it.
func TestAChildJoinReachesTheStampedColumnsToo(t *testing.T) {
	s := joiningSpec()
	s.Children = []Child{{
		Name: "Guardiao", Plural: "Guardioes", Table: "guardioes", OwnedBy: "root",
		Description:      "Os responsáveis pelo aluno.",
		ParentColumn:     "matricula_id",
		EditStrategy:     "atomic-replace",
		BusinessIdentity: []string{"CampusID"},
		Fields: []Field{{
			Name: "CampusID", Type: "id", Column: "campus_id",
			Example: "1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4", Description: "O campus.",
		}},
	}}
	s.Joins = append(s.Joins, Join{
		Kind: "left", To: "Campus", On: "campus_id", InChild: "Guardiao",
		Fields: []JoinField{{
			Name: "CampusArchivedAt", Column: "deleted_at", Description: "Quando foi arquivado.",
		}},
	})
	opts := Options{Neighbours: []Neighbour{campusThatStamps()}}
	if ps := Validate(s, opts); ps.HasBlockers() {
		t.Fatalf("a collection reaches the target's stamped columns as well, got:\n%v", ps.Error())
	}
}

// TestAStampedColumnTheTargetDoesNotDeclareIsStillRefused keeps the reach honest:
// the columns are declared BY PRESENCE, so a target with no archive column has
// no deleted_at to traverse onto and the ordinary refusal is the right one.
func TestAStampedColumnTheTargetDoesNotDeclareIsStillRefused(t *testing.T) {
	s := joiningSpec()
	s.Joins[0].Fields[0].Column = "deleted_at"
	// campusNeighbour declares no managed columns at all.
	ps := Validate(s, joinOpts())
	if !ps.HasBlockers() {
		t.Fatal("a column the target does not declare must be refused, stamped or not")
	}
	if !strings.Contains(ps.Error().Error(), "not a column of Campus") {
		t.Errorf("the refusal must say the column is not there; got:\n%v", ps.Error())
	}
}

// TestAJoinOntoTheTargetsRevisionIsRefusedByName covers the one managed column
// that stays out of reach — and the reason it is worth its own message.
//
// The framework's read path resolves the stamped TIMESTAMPS and stops, so a
// traversal onto the revision fails at repository construction. Falling back to
// "is not a column of Campus" would send the author to fix a declaration that is
// already correct; the column IS there, it is the crossing that is refused.
func TestAJoinOntoTheTargetsRevisionIsRefusedByName(t *testing.T) {
	s := joiningSpec()
	s.Joins[0].Fields[0].Column = "revision"
	ps := Validate(s, Options{Neighbours: []Neighbour{campusThatStamps()}})
	if !ps.HasBlockers() {
		t.Fatal("the revision is not resolvable on the read path — the join must be refused")
	}
	if !strings.Contains(ps.Error().Error(), "optimistic-concurrency") {
		t.Errorf("the refusal must say WHY this column and not the timestamps; got:\n%v", ps.Error())
	}
}

// TestAJoinFieldCannotShadowWhatTheOwnersSchemaStamps is the same blind spot on
// the OTHER side of the traversal.
//
// read.WithJoins refuses a Go name `owner.Resolve` already answers, and Resolve
// reaches the owner's stamped columns and its link column right after its own —
// none of which is under fields[]. So these names were accepted here and paid
// for at repository construction, which is a boot to find.
func TestAJoinFieldCannotShadowWhatTheOwnersSchemaStamps(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		mut   func(*Spec)
	}{
		{"the created_at the entity itself stamps", "CreatedAt", func(*Spec) {}},
		{"the updated_at the entity itself stamps", "UpdatedAt", func(*Spec) {}},
		{"the archive column the entity declares", "DeletedAt",
			func(s *Spec) { s.Storage.Managed.ArchivedAt = "deleted_at" }},
		{"the link column a role resolves as ParentID", "ParentID", func(s *Spec) {
			s.Storage.Kind = "sharedbase-role"
			s.Storage.Base = &Base{
				Table: "people", Description: "A identidade.", NaturalKey: "Name",
				Link: "shared-pk", SchemaFunc: "PersonSchema",
				RowUniqueness: "unique-fk", OrphanPolicy: "keep",
			}
			s.Fields[0].LivesOn = "base"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := joiningSpec()
			tc.mut(s)
			s.Joins[0].Fields[0].Name = tc.field
			ps := Validate(s, joinOpts())
			if !ps.HasBlockers() {
				t.Fatal("the framework refuses this name at construction — refuse it here")
			}
			if !strings.Contains(ps.Error().Error(), tc.field) {
				t.Errorf("the refusal must name the field; got:\n%v", ps.Error())
			}
		})
	}
}

// TestAChildJoinFieldCannotShadowTheCollectionsParentKey: the owner of a child
// join is the COLLECTION's schema, and what it resolves on its own is the
// collection's link back to the root, not the root's.
func TestAChildJoinFieldCannotShadowTheCollectionsParentKey(t *testing.T) {
	s := joiningSpec()
	s.Children = []Child{{
		Name: "Guardiao", Plural: "Guardioes", Table: "guardioes", OwnedBy: "root",
		Description:      "Os responsáveis pelo aluno.",
		ParentColumn:     "matricula_id",
		EditStrategy:     "atomic-replace",
		BusinessIdentity: []string{"CampusID"},
		Fields: []Field{{
			Name: "CampusID", Type: "id", Column: "campus_id",
			Example: "1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4", Description: "O campus.",
		}},
	}}
	s.Joins = append(s.Joins, Join{
		Kind: "left", To: "Campus", On: "campus_id", InChild: "Guardiao",
		Fields: []JoinField{{Name: "ParentID", Column: "label", Description: "A cidade."}},
	})
	if ps := Validate(s, joinOpts()); !ps.HasBlockers() {
		t.Fatal("a collection resolves ParentID itself — a join field may not take that name")
	}
}
