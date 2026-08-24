package spec

import (
	"fmt"
	"sort"
	"strings"
)

// validateJoins refuses every read-join declaration the framework would panic
// on at repository construction.
//
// That is INV-3 applied to a new surface: `read.WithJoins` validates itself the
// moment the repository is built, which means a bad declaration costs a boot to
// find. Everything the framework checks there is knowable from the specs on
// disk, so it is answered here instead — while the author is still in the file.
//
// The one thing this cannot see is a target the project does not declare with a
// spec. That is refused rather than trusted: a join whose target is invisible
// cannot have its column checked, its type derived, or its schema function
// named, and a generator that guesses at all three writes code that compiles
// and reads the wrong column.
func validateJoins(s *Spec, opt Options, ps *Problems) {
	if len(s.Joins) == 0 {
		return
	}

	// One join per foreign key PER JOINING TABLE. The framework derives the SQL
	// alias from the foreign key, so two traversals sharing one key would
	// collide on it — and a column pointing at two tables is a modelling
	// mistake worth naming rather than aliasing around. Two joins to the SAME
	// table are fine as long as they cross different keys (bill_to_id,
	// ship_to_id), which is exactly what tells their aliases apart.
	fkSeen := map[string]string{}
	// A joined Go field must not shadow anything the entity already answers,
	// and that includes another join's field.
	joinNames := map[string]string{}

	for i := range s.Joins {
		validateOneJoin(s, opt, s.Joins[i], fmt.Sprintf("joins[%d]", i), fkSeen, joinNames, ps)
	}

	// A join is not a Mongo concept. The TableSchema is what the projection is
	// composed from and a join deliberately never touches it, so on a Mongo
	// backing the joined fields are real on the loaded entity — the rules read
	// them, a service reads them, FindByID fills them — and absent from every
	// projected document. That is a legitimate shape, and a silent one, so it
	// is said out loud rather than refused.
	//
	// Only when a field is declared VISIBLE, though. A traversal whose every
	// field is hidden was declared for the rules in the first place, and telling
	// its author about a view they never asked to reach is noise.
	if s.Read.Backing == "mongo" && (s.Read.ByID || s.Read.ByParams != nil) && anyVisibleJoinField(s) {
		ps.WarnFix("joins",
			"the read side is Mongo-backed, so the joined fields reach the write path "+
				"and the rules but NOT the view",
			"a projection is composed from the TableSchema, which a join never enters. "+
				"Where the READS need another aggregate's data on this backing, that is "+
				"Embed/Link on the view or a ComposedView; where only the RULES need it, "+
				"this declaration is exactly right — mark the fields hidden: true and the "+
				"wire never sees them")
	}
}

func validateOneJoin(s *Spec, opt Options, j Join, where string, fkSeen, joinNames map[string]string, ps *Problems) {
	if !JoinKinds.Has(j.Kind) {
		ps.BlockerFix(where+".kind",
			fmt.Sprintf("%q is not a join kind", j.Kind),
			"one of: "+JoinKinds.String())
	}
	if j.To == "" {
		ps.BlockerFix(where+".to", "a join needs a target",
			"name the entity on the other side of the foreign key")
		return
	}
	if j.To == s.Entity {
		ps.BlockerFix(where+".to",
			"an entity cannot declare a read join onto itself",
			"the traversal's predicate is fk = target.id on a second copy of the same "+
				"table, which is a hierarchy walk rather than a reach into another "+
				"aggregate — model the parent as its own entity, or read it with "+
				"deps.DB.Querier()")
		return
	}

	// A target this generator can SEE is checked in full: the column exists on
	// it or it does not, and its type is derived rather than restated. A
	// hand-written aggregate is invisible here — nothing about it is in a spec —
	// so it is accepted on the author's word, with the two consequences said out
	// loud: the field types have to be declared, and the schema function is
	// assumed to follow the project's own convention.
	//
	// Refusing it outright would be the wrong trade. A service that adopted this
	// generator midway has hand-written aggregates by definition, and "you may
	// not reach that one" would be a limit of the tool presented as a limit of
	// the framework.
	target := neighbourNamed(opt.Neighbours, j.To)
	if target == nil {
		warnUnseenTarget(j, where, opt, ps)
	}

	// The joining table: the root's, or the collection's when inChild is set.
	// Everything below — the foreign key, the Go fields, the shadowing check —
	// is asked of THAT table, never of the root by default.
	ownerFields := s.Fields
	ownerLabel := "the root"
	if j.InChild != "" {
		c := findChild(s.Children, j.InChild)
		if c == nil {
			ps.BlockerFix(where+".inChild",
				fmt.Sprintf("%q is not a collection of %s", j.InChild, s.Entity),
				"only a collection this spec declares under children[] can carry a join. "+
					"A collection owned by a SHARED BASE belongs to the base: declare the "+
					"join on the base's own spec instead")
			return
		}
		if c.OwnedBy == "base" {
			ps.BlockerFix(where+".inChild",
				fmt.Sprintf("%q is owned by the shared base, not by %s", j.InChild, s.Entity),
				"the framework accepts a join only from a collection the ROOT's schema "+
					"declares; a base-owned one is declared by the identity's schema, so "+
					"the join belongs on that spec")
			return
		}
		ownerFields = c.Fields
		ownerLabel = "the collection " + j.InChild
	}

	// The foreign key, on the joining table.
	fk := fieldByColumn(ownerFields, j.On)
	switch {
	case j.On == "":
		ps.BlockerFix(where+".on", "a join needs a foreign key column",
			"name the column on "+ownerLabel+" that points at "+j.To)
		return
	case fk == nil:
		ps.BlockerFix(where+".on",
			fmt.Sprintf("%q is not a column of %s", j.On, ownerLabel),
			"the foreign key is a column of the JOINING table — this entity's, not "+
				j.To+"'s. Declare it under fields[] first if it is genuinely missing")
		return
	case fk.Type != "id":
		ps.BlockerFix(where+".on",
			fmt.Sprintf("%s is %s, not an id", fk.Name, fk.Type),
			"the predicate is fk = "+j.To+".id, so the key has to be an id — a match "+
				"against a code or a natural key is deliberately not expressible")
	}

	fkKey := ownerLabel + "." + j.On
	if prev, dup := fkSeen[fkKey]; dup {
		ps.BlockerFix(where+".on",
			fmt.Sprintf("%q already carries the join to %s", j.On, prev),
			"one foreign key reaches ONE table — it is also what tells the two SQL "+
				"aliases apart. Two traversals to the same table need two foreign keys "+
				"(bill_to_id, ship_to_id)")
	}
	fkSeen[fkKey] = j.To

	// An inner join drops the joining row when there is no counterpart. Over a
	// non-nullable key referential integrity means there always is one, so the
	// choice is intent and query plan; over a nullable one it silently drops
	// aggregates — from FindByID too, which the write-side handlers load
	// through, so a legitimate write becomes a 404.
	if j.Kind == "inner" && fk != nil && fk.Nullable {
		ps.BlockerFix(where+".kind",
			fmt.Sprintf("%s is nullable, so an inner join would silently drop every %s with no %s",
				fk.Name, s.Entity, j.To),
			"use kind: left — the declaration reaches every read through this "+
				"repository, FindByID included, so the rows it drops are dropped from "+
				"writes as well")
	}

	if len(j.Fields) == 0 {
		ps.BlockerFix(where+".fields",
			"a join that maps no column reaches nothing",
			"map at least one column of "+j.To+" onto a Go field: "+
				"{name: <GoField>, column: <target column>}")
		return
	}

	for k, f := range j.Fields {
		validateJoinField(s, j, target, f, ownerFields, ownerLabel,
			fmt.Sprintf("%s.fields[%d]", where, k), joinNames, ps)
	}
}

func validateJoinField(s *Spec, j Join, target *Neighbour, f JoinField,
	ownerFields []Field, ownerLabel, where string, joinNames map[string]string, ps *Problems) {

	if f.Name == "" {
		ps.Blockerf(where+".name", "a joined field needs a Go name")
		return
	}
	if !goIdentRe.MatchString(f.Name) {
		ps.BlockerFix(where+".name",
			fmt.Sprintf("%q is not a Go field name", f.Name),
			"exported PascalCase, like the entity's own fields")
	}
	if f.Column == "" {
		ps.BlockerFix(where+".column",
			fmt.Sprintf("%s maps no column of %s", f.Name, j.To),
			"name the column on the target this field is filled from")
		return
	}
	// A join field carries NO domain type, and `id` is the one spec type that
	// would ask for one. The value belongs to another aggregate and arrives
	// read-only — never written through this entity, never validated by this
	// domain — so an identity lands as its canonical TEXT instead, which is also
	// the only shape correct on all four engines. Nothing is lost: the column is
	// still the target's identity, and the generator derives the text form from
	// the target's own declaration when the type is left out.
	if f.Type == "id" {
		ps.BlockerFix(where+".type",
			"a join field carries no domain type, and that includes an identity",
			"drop the key — an identity column arrives as text, and the generator "+
				"reads which columns are identities from "+j.To+"'s own spec. For a "+
				"hand-written target, declare type: string")
		return
	}

	// The Go name must not shadow anything the JOINING TABLE already answers to,
	// and that namespace is the owner's — not the aggregate's. The framework
	// checks `owner.Resolve`, which reaches the owner's own columns and the
	// facets attached to it (plus, on the root, the shared base) and stops
	// there: a collection's schema does not resolve the root's fields.
	//
	// So an entry may carry a joined name the root also uses. Refusing that
	// would be this generator inventing a rule and presenting it as the
	// framework's, and the two structs are genuinely separate namespaces in the
	// emitted Go.
	owner := s.Entity
	if j.InChild != "" {
		owner = "the collection " + j.InChild
	}
	if what := shadowedOnOwner(s, j.InChild, ownerFields, f.Name); what != "" {
		ps.BlockerFix(where+".name",
			fmt.Sprintf("%s already resolves on %s — %s", f.Name, owner, what),
			"the joined side's spelling never surfaces above infra, so rename this "+
				"field freely: the column it reads is unchanged")
		return
	}
	if prev, dup := joinNames[ownerLabel+"/"+f.Name]; dup {
		ps.BlockerFix(where+".name",
			fmt.Sprintf("%s is already filled by the join to %s", f.Name, prev),
			"two traversals cannot land on one Go field — give this one its own name")
		return
	}
	joinNames[ownerLabel+"/"+f.Name] = j.To

	// A target this generator cannot see has nothing to check the column
	// against, so the field's own type is what stands in for the derivation —
	// demanded rather than guessed. The same goes for its NULLABILITY, which is
	// not derivable from the join kind: an inner join proves the joined ROW
	// exists, never that every column of it is filled, so `nullable: true` is
	// the only way to say "this column can be NULL on the other side" about an
	// aggregate the generator cannot read.
	if target == nil {
		switch {
		case f.Type == "":
			ps.BlockerFix(where+".type",
				fmt.Sprintf("%s has no type, and %s is not a spec of this project to derive one from",
					f.Name, j.To),
				"state the type here — it is what the Go field on this entity is declared "+
					"as, and it must match the column on "+j.To)
		case !JoinFieldTypes.Has(f.Type):
			ps.BlockerFix(where+".type",
				fmt.Sprintf("%q is not a type a join field can carry", f.Type),
				"one of: "+JoinFieldTypes.String())
		}
		return
	}

	// The column, on the target's OWN table. A traversal is one predicate onto
	// one table: the target's shared base and its siblings live in other tables
	// the join never reaches.
	tf := neighbourFieldByColumn(target.Fields, f.Column)
	if tf == nil {
		ps.BlockerFix(where+".column",
			fmt.Sprintf("%q is not a column of %s", f.Column, j.To),
			fmt.Sprintf("%s declares: %s", j.To, columnList(target.Fields)))
		return
	}
	if tf.LivesOn == "base" || strings.HasPrefix(tf.LivesOn, "sibling:") {
		ps.BlockerFix(where+".column",
			fmt.Sprintf("%q lives on %s's %s, not on its own table", f.Column, j.To, tf.LivesOn),
			"a traversal is ONE predicate onto ONE table, so it reaches the target's "+
				"own columns only — the base's and the facets' live in tables this join "+
				"never enters")
		return
	}
	// Both restatements are refused the same way and for the same reason: the
	// target's own declaration is the single place the two sides cannot
	// disagree, and a second copy here is a copy that goes stale the first time
	// the target's spec changes and this one is not reopened.
	if f.Nullable && !tf.Nullable {
		ps.BlockerFix(where+".nullable",
			fmt.Sprintf("%s declares %q as NOT NULL", j.To, f.Column),
			"leave nullable out — it is derived from the target's own declaration. "+
				"The key exists only for a target this project has no spec for, where "+
				"there is nothing to derive it from")
	}
	if f.Type != "" && f.Type != tf.Type {
		ps.BlockerFix(where+".type",
			fmt.Sprintf("%s declares %q as %s, not %s", j.To, f.Column, tf.Type, f.Type),
			"leave type out — it is derived from the target's own declaration, which "+
				"is the one place the two cannot disagree")
	}
}

// shadowedOnOwner names what a joined Go field would shadow ON THE JOINING
// TABLE, or "" when the name is free there.
//
// It reproduces the reach of the framework's `owner.Resolve`, which is what
// read.WithJoins consults: the owner's own columns, plus the 1:1 facets attached
// to that same node — the root's for a root join, the collection's for a child
// one. A root's fields include the shared base's (they are declared here with
// livesOn: base), which is the third thing Resolve reaches.
//
// What it deliberately does NOT reach is the other direction: a collection does
// not resolve the root's fields, and a facet of one collection is nothing to
// another. Checking wider would refuse declarations the framework accepts.
func shadowedOnOwner(s *Spec, inChild string, ownerFields []Field, name string) string {
	if findField(ownerFields, name) != nil {
		return "a field it already declares"
	}
	attachTo := "child:" + inChild
	for i := range s.Siblings {
		sib := s.Siblings[i]
		isChildFacet := strings.HasPrefix(sib.AttachTo, "child:")
		if inChild == "" {
			// The root's own facets: root, and role under sharedbase. A facet of
			// a collection hangs off that collection, not off the root.
			if isChildFacet {
				continue
			}
		} else if sib.AttachTo != attachTo {
			continue
		}
		if findField(sib.Fields, name) != nil {
			return "a field of the facet " + sib.Name
		}
	}
	return ""
}

func neighbourNamed(ns []Neighbour, entity string) *Neighbour {
	for i := range ns {
		if ns[i].Entity == entity {
			return &ns[i]
		}
	}
	return nil
}

func neighbourFieldByColumn(fs []NeighbourField, column string) *NeighbourField {
	for i := range fs {
		if fs[i].Column == column {
			return &fs[i]
		}
	}
	return nil
}

func fieldByColumn(fs []Field, column string) *Field {
	for i := range fs {
		if fs[i].Column == column {
			return &fs[i]
		}
	}
	return nil
}

func columnList(fs []NeighbourField) string {
	var out []string
	for _, f := range fs {
		if f.Column != "" && f.LivesOn != "base" && !strings.HasPrefix(f.LivesOn, "sibling:") {
			out = append(out, f.Column)
		}
	}
	if len(out) == 0 {
		return "no columns of its own"
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func knownEntities(ns []Neighbour) string {
	var out []string
	for _, n := range ns {
		if n.Entity != "" {
			out = append(out, n.Entity)
		}
	}
	if len(out) == 0 {
		return " — and this project declares no other spec yet"
	}
	sort.Strings(out)
	return " (this project declares: " + strings.Join(out, ", ") + ")"
}

// joinReach is what the declared traversals add to the names the READ side can
// resolve, split the way the framework splits them.
//
// The root's entry is addressable in a criteria — filter, order, `?fields=` —
// because a root join rides the root SELECT. A collection's entry is served
// inside the entry and nothing more: filtering the root by a field of a 1:N
// collection is a pushdown one root SELECT cannot express, which is the same
// boundary every child field already has.
type joinReach struct {
	root  []Field
	child map[string][]Field
}

// joinReachOf resolves each mapped field to the type the TARGET declares for
// its column, so the read side can check an operator against it exactly as it
// would for a column of this entity's own table.
//
// A relational backing is the condition, and it is not a policy: a join leaves
// the TableSchema untouched, so a Mongo projection over the same entity never
// carries these columns. Naming one in a filter there would emit a query
// parameter the store cannot answer.
func joinReachOf(s *Spec, opt Options) joinReach {
	jr := joinReach{child: map[string][]Field{}}
	if s.Read.Backing != "relational" {
		return jr
	}
	for _, j := range s.Joins {
		target := neighbourNamed(opt.Neighbours, j.To)
		for _, f := range j.Fields {
			specType, targetNullable := f.Type, false
			if target != nil {
				if tf := neighbourFieldByColumn(target.Fields, f.Column); tf != nil {
					specType, targetNullable = tf.Type, tf.Nullable
				}
			}
			if specType == "" {
				continue
			}
			// An identity crosses as TEXT (a join field carries no domain type),
			// so the operators a filter may declare on it are a string's. The
			// framework still binds the predicate in the target's native id form —
			// it takes that typing from the TARGET's schema rather than from this
			// side, precisely because nothing about the field says "identity".
			if specType == "id" {
				specType = "string"
			}
			rf := Field{
				Name: f.Name, Type: specType, Column: f.Column,
				// Two independent sources of absence: no counterpart (left), or a
				// column the target itself declares nullable.
				Nullable: j.Kind == "left" || targetNullable,
				LivesOn:  "root",
			}
			if j.InChild == "" {
				jr.root = append(jr.root, rf)
				continue
			}
			jr.child[j.InChild] = append(jr.child[j.InChild], rf)
		}
	}
	return jr
}

func fieldNamedIn(fs []Field, name string) *Field {
	for i := range fs {
		if fs[i].Name == name {
			return &fs[i]
		}
	}
	return nil
}

// warnUnseenTarget says out loud what cannot be checked about a hand-written
// target, and what the generated code will therefore assume.
func warnUnseenTarget(j Join, where string, opt Options, ps *Problems) {
	ps.WarnFix(where+".to",
		fmt.Sprintf("no spec of this project declares %q, so its columns cannot be checked here", j.To),
		fmt.Sprintf("the generated repository calls schemas.%sSchema() — a hand-written "+
			"aggregate that names its schema differently fails the build at once, which "+
			"is the honest failure. Each mapped field has to state its own type, and "+
			"`nullable: true` on any column that is nullable OVER THERE — an inner join "+
			"proves the joined row exists, not that every column of it is filled, and a "+
			"non-pointer field receiving a NULL is refused at repository construction. "+
			"The framework validates the column and the Go field itself at that same "+
			"point%s", j.To, knownEntities(opt.Neighbours)))
}

func anyVisibleJoinField(s *Spec) bool {
	for _, j := range s.Joins {
		for _, f := range j.Fields {
			if !f.Hidden {
				return true
			}
		}
	}
	return false
}
