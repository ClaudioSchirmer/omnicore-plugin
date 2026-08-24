package ir

import (
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// resolveJoins turns each declared traversal into the shape the emitters need,
// and hangs its fields where the framework actually lands them: a root join's on
// the entity, a child join's on that collection's entry.
//
// The TYPE is derived from the TARGET's own spec rather than restated here.
// That is the whole reason the target must be a spec of this project: the column
// exists there or it does not, and it has exactly one type — a second statement
// of it is a second thing to keep in step.
//
// Nullability has TWO independent sources and either one alone decides it: a
// left join with no counterpart produces NULL whatever the target declares, and
// a column the TARGET declares nullable is reachable as NULL even under an inner
// join — which proves the joined ROW exists, never that every column of it is
// filled. The framework refuses a non-pointer Go field in both cases.
func resolveJoins(s *spec.Spec, p *discover.Project, m *Model) {
	for _, j := range s.Joins {
		// A hand-written target is invisible to the generator: validation has
		// already demanded an explicit type per field for exactly that case, so
		// the resolution below falls back to it rather than skipping the join.
		target := claimNamed(p.SiblingSpecs, j.To)
		// inChild takes either of the collection's two names; below this line
		// there is one, and it is the entry type's — which is also what the
		// generated schema function is called.
		inChild := canonicalCollection(s.Children, j.InChild)
		rj := Join{
			Kind:              j.Kind,
			Target:            j.To,
			TargetSchemaFunc:  j.To + "Schema",
			FKColumn:          j.On,
			Child:             inChild,
			TargetHandWritten: target == nil,
		}
		if inChild != "" {
			rj.ChildSchemaFunc = inChild + "Schema"
		}
		for _, f := range j.Fields {
			rj.Fields = append(rj.Fields, resolveJoinField(m.Entity.Pascal, j, f, target))
		}
		m.Joins = append(m.Joins, rj)

		if inChild == "" {
			continue
		}
		for i := range m.Children {
			if m.Children[i].Name == inChild {
				m.Children[i].JoinFields = append(m.Children[i].JoinFields, rj.Fields...)
			}
		}
	}
}

func resolveJoinField(entity string, j spec.Join, f spec.JoinField, target *discover.SpecClaim) Field {
	// The target's own declaration wins whenever there is one; the field's own
	// keys stand in ONLY for a hand-written aggregate this project has no spec
	// for, which is the single case validation demands them in.
	specType, targetNullable := f.Type, f.Nullable
	if target != nil {
		if tf := claimFieldByColumn(target.Fields, f.Column); tf != nil {
			specType, targetNullable = tf.Type, tf.Nullable
		}
	}

	// A join field carries NO DOMAIN TYPE — not an identity, not a value object
	// of any kind. The framework refuses one at construction, and the reason is
	// not stylistic: the value belongs to ANOTHER aggregate and arrives
	// read-only, so it is never written through this entity and never validated
	// by this domain. A domain type here would be an instance no rule ever
	// approved.
	//
	// An IDENTITY column therefore lands as its canonical TEXT rather than as
	// domain.ID. That is also the only shape that is correct on every engine:
	// three of the four store an id as BINARY(16)/RAW(16), and the framework
	// decodes it on the way out.
	base := goTypeOf(specType)
	if specType == "id" {
		base = "string"
	}

	// The pointer is decided by what can be ABSENT, and there are two
	// independent sources of absence: a left join with no counterpart, and a
	// column the TARGET declares nullable. Either one makes NULL reachable, and
	// a non-pointer field cannot hold it — the scan fails on the first row that
	// has one. The framework enforces exactly this pair for an identity column
	// and leaves the rest to the declaration, so the generator applies it to
	// every type rather than waiting to be told per column.
	nullable := j.Kind == "left" || targetNullable
	goType := base
	if nullable {
		goType = "*" + base
	}
	label := f.LabelKey
	if label == "" {
		label = entity + f.Name + "Field"
	}
	return Field{
		Name: f.Name, Column: f.Column, SpecType: specType,
		GoType: goType, BaseGoType: base,
		EntityType: goType, BaseEntityType: base,
		Nullable: nullable,
		JSONName: naming.Camel(f.Name), LabelKey: label, Text: f.Text.Map(),
		Example: f.Example, Description: f.Description,
		Hidden: f.Hidden,
	}
}

// RootJoins are the traversals that hang off the root — the only ones whose
// fields a criteria may address, and the only ones the root SELECT carries.
func (m *Model) RootJoins() []Join {
	var out []Join
	for _, j := range m.Joins {
		if j.Child == "" {
			out = append(out, j)
		}
	}
	return out
}

// RootJoinFields are those traversals' fields, flattened.
func (m *Model) RootJoinFields() []Field {
	var out []Field
	for _, j := range m.RootJoins() {
		out = append(out, j.Fields...)
	}
	return out
}

// JoinField finds a joined field by its Go name, on the root or in a collection.
// It is how the read side answers "is this filterable name a joined one".
func (m *Model) JoinField(name string) (Field, bool) {
	for _, j := range m.Joins {
		for _, f := range j.Fields {
			if f.Name == name {
				return f, true
			}
		}
	}
	return Field{}, false
}

func claimNamed(cs []discover.SpecClaim, entity string) *discover.SpecClaim {
	for i := range cs {
		if cs[i].Entity == entity {
			return &cs[i]
		}
	}
	return nil
}

func claimFieldByColumn(fs []discover.FieldClaim, column string) *discover.FieldClaim {
	for i := range fs {
		if fs[i].Column == column {
			return &fs[i]
		}
	}
	return nil
}

// joinNamed finds a joined field among the ones the read model serves. It is
// consulted from the ReadModel being built rather than through lookupField,
// for the same reason the managed set is: m.Read still holds the previous
// (empty) value while resolveRead runs.
func joinNamed(fs []Field, name string) *Field {
	for i := range fs {
		if fs[i].Name == name {
			return &fs[i]
		}
	}
	return nil
}

// ResponseJoinFields are the ROOT joins' fields a CALLER receives.
//
// It is narrower than ReadModel.JoinFields on purpose, and the narrowing is the
// whole point of joins[].fields[].hidden: a traversal declared so a rule can
// decide against another aggregate's value belongs on the entity and nowhere
// near the wire. What it does NOT narrow is everything else — the field is
// still loaded, still readable by the rules, still filterable and sortable —
// because "you do not receive this" and "this does not exist" are different
// statements.
func (m *Model) ResponseJoinFields() []Field {
	return visibleFields(m.Read.JoinFields)
}

// ResponseChildJoinFields is the same narrowing, inside a collection's entry.
func (m *Model) ResponseChildJoinFields(c Child) []Field {
	return visibleFields(m.ServedJoinFields(c))
}

func visibleFields(fs []Field) []Field {
	out := make([]Field, 0, len(fs))
	for _, f := range fs {
		if f.Hidden {
			continue
		}
		out = append(out, f)
	}
	return out
}

// ServedJoinFields are a collection's joined fields AS THE READ SERVES THEM.
//
// The gate is the backing, for the same reason ReadModel.JoinFields is gated: a
// join never enters the TableSchema, so a Mongo-projected document does not
// carry these columns and a DTO field for one would be a zero value forever.
func (m *Model) ServedJoinFields(c Child) []Field {
	if m.Read.Backing != "relational" {
		return nil
	}
	return c.JoinFields
}
