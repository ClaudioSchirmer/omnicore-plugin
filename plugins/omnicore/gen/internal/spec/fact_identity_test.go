package spec

import "testing"

// The aggregate id as a fact's filter.
//
// It was the one fixed name this key could not say while everything under it
// could: the framework locks the Go side of every primary key to "ID",
// criteria.ByID is Where(Eq("ID", id)), and the exclude-self gate this same
// generator emits is Ne("ID", selfID). A `kind: manual` fact whose body needed
// the id therefore had no way to receive it — the body is the author's, the
// signature is not — so the id had to be re-derived inside the body from a
// natural key, paying a join to translate a value the caller was holding.
//
// What every case below protects is the pair: the name resolves, and the two
// places it CANNOT be used still refuse it for their own reasons.

// TestAFactMayBeNarrowedByTheAggregateID is the whole point. Both kinds, because
// the answer is the same one: manual, where the filter exists only to shape the
// method the author writes, and a computed one, where it becomes criteria.
func TestAFactMayBeNarrowedByTheAggregateID(t *testing.T) {
	for _, f := range []Fact{
		{Name: "EstaEmUso", Kind: "manual", Returns: "bool",
			Description: "Se alguém ainda aponta para esta linha.",
			Filters:     eqFilters(IdentityName)},
		{Name: "AindaVivo", Kind: "exists",
			Description: "Se esta linha ainda existe.",
			Filters:     eqFilters(IdentityName)},
		{Name: "VivosEntreEstes", Kind: "count",
			Description: "Quantos destes ids existem.",
			Filters:     []FactFilter{{Field: IdentityName, Op: "in"}}},
	} {
		if ps := Validate(factSpec(f), Options{}); ps.HasBlockers() {
			t.Errorf("%s: a fact narrowed by %s must validate:\n%v", f.Name, IdentityName, ps.Error())
		}
	}
}

// TestTheIDCoexistsWithExcludeSelf holds the two apart. They are not the same
// question — one asks WHICH row, the other takes the row being written out of
// the answer — and "any of these, other than me" needs both. They reach the
// signature under different names, so nothing collides.
func TestTheIDCoexistsWithExcludeSelf(t *testing.T) {
	s := factSpec(Fact{
		Name: "OutroVivoEntreEstes", Kind: "exists",
		Description: "Se algum destes ids, que não este, existe.",
		Filters:     []FactFilter{{Field: IdentityName, Op: "in"}},
		ExcludeSelf: true,
	})
	if ps := Validate(s, Options{}); ps.HasBlockers() {
		t.Fatalf("a filter on %s beside excludeSelf must validate:\n%v", IdentityName, ps.Error())
	}
}

// TestADeclarativeRuleCannotFillTheAggregateID is the refusal that had to ship
// WITH the acceptance. A rules.list entry fills a fact's arguments from the
// entity being written, and the entity has no id to give: it is not minted
// until after the rules have run. Left unrefused the emitter wrote `e.` and the
// tree did not build.
func TestADeclarativeRuleCannotFillTheAggregateID(t *testing.T) {
	s := factSpec(Fact{
		Name: "Vizinhos", Kind: "count", Description: "Quantos como este.",
		Filters: []FactFilter{{Field: IdentityName, Op: "ne", As: "outro"}},
	})
	s.Notifications = append(s.Notifications, Notification{
		Name: "DemaisNotification", Semantic: "validation", Text: sevenTexts("Demais."),
	})
	s.Rules.List = append(s.Rules.List, Rule{
		ID: "teto", Kind: "factRange", Scope: []string{"insert"}, Fact: "Vizinhos",
		Max: ptrFloat(50), AttachTo: "Status", Notification: "DemaisNotification",
	})
	ps := Validate(s, Options{})
	if !blockerAbout(ps, "not minted until after the rules have run") {
		t.Fatalf("a rule cannot fill the aggregate id, got:\n%v", ps.Error())
	}
	if !blockerAbout(ps, "rules.manual") {
		t.Fatalf("the fix must point at the hand-written call, got:\n%v", ps.Error())
	}
}

// TestTheIDCannotBePinnedOrOrdered is the pair of refusals the id inherits by
// being typed as one. Both existed already and neither was reachable, because
// the name resolved to nothing at all — so this is the proof they bite now that
// it does.
func TestTheIDCannotBePinnedOrOrdered(t *testing.T) {
	pinned := factSpec(Fact{
		Name: "AquelaLinha", Kind: "exists", Description: "Aquela linha.",
		Filters: []FactFilter{{Field: IdentityName, Value: "1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4"}},
	})
	if ps := Validate(pinned, Options{}); !blockerAbout(ps, "a row someone pasted") {
		t.Errorf("an id pinned in the spec must be refused, got:\n%v", ps.Error())
	}
	ordered := factSpec(Fact{
		Name: "DepoisDaquela", Kind: "count", Description: "Depois daquela.",
		Filters: []FactFilter{{Field: IdentityName, Op: "gt", As: "depoisDe"}},
	})
	if ps := Validate(ordered, Options{}); !blockerAbout(ps, "one identity is not greater than another") {
		t.Errorf("an order between two identities must be refused, got:\n%v", ps.Error())
	}
}

// TestANearSpellingOfTheIDIsAnsweredWithTheSpelling is what the author who
// wrote `Id` gets. Now that the exact name works, "does not name a field of
// this entity" would be the wrong half of the truth — it reads as "this entity
// has no id", which is never true.
func TestANearSpellingOfTheIDIsAnsweredWithTheSpelling(t *testing.T) {
	s := factSpec(Fact{
		Name: "EstaEmUso", Kind: "manual", Returns: "bool",
		Description: "Se alguém ainda aponta para esta linha.",
		Filters:     eqFilters("Id"),
	})
	ps := Validate(s, Options{})
	if !blockerAbout(ps, "the aggregate id answers to") {
		t.Fatalf("a near-spelling must be answered with the spelling, got:\n%v", ps.Error())
	}
}
