package spec

import (
	"strings"
	"testing"
)

// campusReachingCity is the same target as campusNeighbour, with the two keys a
// CHAIN departs from: one mandatory, one not. A hop's foreign key is a column of
// the PREVIOUS TARGET, so these are what the tests below traverse — and their
// absence from the joining spec is the point.
func campusReachingCity() Neighbour {
	n := campusNeighbour()
	n.Fields = append(n.Fields,
		NeighbourField{Name: "CityID", Column: "city_id", Type: "id", LivesOn: "root"},
		NeighbourField{Name: "MayorID", Column: "mayor_id", Type: "id", LivesOn: "root", Nullable: true},
		NeighbourField{Name: "CountryName", Column: "country_name", Type: "string", LivesOn: "base"},
	)
	return n
}

func cityNeighbour() Neighbour {
	return Neighbour{
		Path: "specs/omnicore-gen/city.omnicore.yaml", Entity: "City",
		ViewName: "cities", Route: "Cities", Table: "cities",
		Fields: []NeighbourField{
			{Name: "CityName", Column: "name", Type: "string", LivesOn: "root"},
		},
	}
}

func chainOpts() Options {
	return Options{Neighbours: []Neighbour{campusReachingCity(), cityNeighbour()}}
}

// chainingSpec is joiningSpec with the traversal continued one hop further out.
func chainingSpec() *Spec {
	s := joiningSpec()
	s.Joins[0].Then = []Join{{
		Kind: "inner", To: "City", On: "city_id",
		Fields: []JoinField{{Name: "CampusCityName", Column: "name", Description: "A cidade do campus."}},
	}}
	return s
}

// TestChainingSpecIsClean guards the fixture, for the same reason its one-hop
// twin does: every refusal asserted below is vacuous if the baseline is dirty.
func TestChainingSpecIsClean(t *testing.T) {
	if ps := Validate(chainingSpec(), chainOpts()); ps.HasBlockers() {
		t.Fatalf("a chain over declared keys should validate cleanly, got:\n%v", ps.Error())
	}
	if cov := CheckCoverage(chainingSpec()); cov.HasBlockers() {
		t.Fatalf("chained joins should be a covered capability, got:\n%v", cov.Error())
	}
}

// TestAHopsKeyIsAColumnOfThePreviousTarget is the rule that tells a chain apart
// from a second join, and the one an author gets wrong first: campus_id is a
// column of THIS entity, and a hop departs from the campus.
func TestAHopsKeyIsAColumnOfThePreviousTarget(t *testing.T) {
	s := chainingSpec()
	s.Joins[0].Then[0].On = "campus_id"
	ps := Validate(s, chainOpts())
	if !ps.HasBlockers() {
		t.Fatal("a hop crossing a key of the DECLARING entity must be refused")
	}
	if !strings.Contains(ps.Error().Error(), "Campus") {
		t.Errorf("the refusal must name the table the hop departs from; got:\n%v", ps.Error())
	}
}

// TestAnInnerHopIsJudgedByThePathNotByItself is the narrowing the chain brings.
//
// Inner all the way, a nullable key drops roots from every read — the same
// refusal a one-hop join carries. Under a LEFT above it, the very same hop drops
// nothing: it binds its own block, and a miss reports the chain absent with the
// root intact. Refusing both would forbid exactly what left(x).then(inner(y)) is
// for.
func TestAnInnerHopIsJudgedByThePathNotByItself(t *testing.T) {
	inner := chainingSpec()
	inner.Joins[0].Then[0].On = "mayor_id"
	if ps := Validate(inner, chainOpts()); !ps.HasBlockers() {
		t.Error("an inner hop over a nullable key, inner all the way, must be refused")
	}

	under := chainingSpec()
	under.Joins[0].Kind = "left"
	under.Joins[0].Then[0].On = "mayor_id"
	if ps := Validate(under, chainOpts()); ps.HasBlockers() {
		t.Errorf("under a left above it, an inner hop over a nullable key drops nothing:\n%v",
			ps.Error())
	}
}

// TestAHopCannotNameACollection: only the HEAD decides what a chain hangs off.
// The framework panics on this at declaration.
func TestAHopCannotNameACollection(t *testing.T) {
	s := chainingSpec()
	s.Joins[0].Then[0].InChild = "Guardian"
	ps := Validate(s, chainOpts())
	if !ps.HasBlockers() {
		t.Fatal("a hop naming a collection must be refused")
	}
	if !strings.Contains(ps.Error().Error(), "inChild") {
		t.Errorf("the refusal must name the key at fault; got:\n%v", ps.Error())
	}
}

// TestAHopReachesItsTargetsOwnTable: the previous target enters the statement
// reduced to one table, and so does this hop's. A column of the target's shared
// base lives in a table the traversal never opens.
func TestAHopReachesItsTargetsOwnTable(t *testing.T) {
	s := chainingSpec()
	s.Joins[0].Then[0].On = "country_name" // a base column of Campus
	ps := Validate(s, chainOpts())
	if !ps.HasBlockers() {
		t.Fatal("a hop departing from a column of the previous target's BASE must be refused")
	}
	if !strings.Contains(ps.Error().Error(), "not on its own table") {
		t.Errorf("the refusal must say the key is not on the reduced table; got:\n%v", ps.Error())
	}
}

// TestTwoHopsOffOneTargetCannotShareAKey — the SQL alias is derived from the
// path of keys, so two traversals leaving the same point by the same column
// would collide on it.
func TestTwoHopsOffOneTargetCannotShareAKey(t *testing.T) {
	s := chainingSpec()
	s.Joins[0].Then = append(s.Joins[0].Then, Join{
		Kind: "left", To: "City", On: "city_id",
		Fields: []JoinField{{Name: "CampusCityAgain", Column: "name", Description: "A mesma cidade."}},
	})
	ps := Validate(s, chainOpts())
	if !ps.HasBlockers() {
		t.Fatal("two hops off one target sharing a foreign key must be refused")
	}
	if !strings.Contains(ps.Error().Error(), "already carries the join") {
		t.Errorf("the refusal must name the collision; got:\n%v", ps.Error())
	}
}

// TestTwoCHAINSMayCrossTheSameKey is the other half, and the reason the key is
// the PATH rather than the table: two chains reaching campus from different
// points are different traversals under different aliases, and each may cross
// its city_id.
func TestTwoChainsMayCrossTheSameKey(t *testing.T) {
	s := chainingSpec()
	s.Children = append(s.Children, Child{
		Name: "Guardian", Plural: "Guardians", Table: "student_guardians",
		ParentColumn: "student_id", Description: "Responsáveis.", OwnedBy: "root",
		EditStrategy: "atomic-replace", BusinessIdentity: []string{"GuardianCampusID"},
		Fields: []Field{{
			Name: "GuardianCampusID", Type: "id", Column: "campus_id",
			Example: "1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4", Description: "O campus.",
		}},
	})
	s.Joins = append(s.Joins, Join{
		Kind: "left", To: "Campus", On: "campus_id", InChild: "Guardian",
		Fields: []JoinField{{Name: "GuardianCampusName", Column: "label", Description: "O campus."}},
		Then: []Join{{
			Kind: "left", To: "City", On: "city_id",
			Fields: []JoinField{{Name: "GuardianCampusCityName", Column: "name", Description: "A cidade."}},
		}},
	})
	if ps := Validate(s, chainOpts()); ps.HasBlockers() {
		t.Errorf("two different chains may each cross city_id — different aliases:\n%v", ps.Error())
	}
}

// TestAHopFieldSharesTheHeADsNamespace: every hop's fields land on the SAME
// struct, so a hop three tables out collides with the head exactly as two heads
// would.
func TestAHopFieldSharesTheHeadsNamespace(t *testing.T) {
	s := chainingSpec()
	s.Joins[0].Then[0].Fields[0].Name = "CampusName" // the head already fills it
	ps := Validate(s, chainOpts())
	if !ps.HasBlockers() {
		t.Fatal("a hop cannot land on a Go field the head already fills")
	}
	if !strings.Contains(ps.Error().Error(), "CampusName") {
		t.Errorf("the refusal must name the field; got:\n%v", ps.Error())
	}
}
