package ir

import (
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// Composite value objects, resolved.
//
// The resolution is the whole design, and it is one sentence: a composite field
// EXPANDS into one ordinary Field per part, under the logical name the schema
// exposes it by, and each of those carries a Composite back-reference saying
// where it came from.
//
// That is what makes the feature nearly free downstream. The framework's own
// claim is that nothing below the schema learns a composite exists — criteria,
// audit, the projection, the read DTO, filters, orderBy, ?fields=, OpenAPI,
// GraphQL, gRPC and the exports all see flat fields — and the generator holds
// itself to the same claim: the migration, the read view, the request and
// response DTOs, the listing and every generated test read m.Fields and never
// ask. Exactly four places do ask, and they are the four that speak the DOMAIN's
// language rather than the wire's:
//
//	the aggregate struct   → one field of the value object's type, not N
//	the TableSchema        → Composite(NewCompositeValueObject[…]…), not N Fields
//	the command mappers    → fold the flat wire fields back into the value object
//	the valid-entity test  → build the value object to build the entity
type CompositePart struct {
	// VOName is the value object's own type name (Money); VOType is how it is
	// written from OUTSIDE the vos package (vos.Money).
	VOName string
	VOType string
	// Owner is the entity field holding the value object (Salary), and
	// OwnerNullable says the composite is OPTIONAL — held as a pointer, absent as
	// a whole when every part column is NULL.
	Owner         string
	OwnerNullable bool
	// OwnerLabelKey is the catalog key for the composite AS A WHOLE — what a rule
	// about the value object attaches to. The parts carry their own, declared
	// inside the value object.
	OwnerLabelKey string
	// OwnerText is the LABEL of the composite as a whole, per catalog code, as
	// the aggregate's own field declared it.
	OwnerText map[string]string
	// OwnerDescription is the one line about the concept, used as the aggregate
	// field's comment.
	OwnerDescription string
	// PartName is the field's name INSIDE the value object (Amount); Exposed is
	// the logical name everything downstream sees (SalaryAmount).
	PartName string
	Exposed  string
	// PartType is the part's Go type as the VALUE OBJECT declares it — which is
	// not the wire type: an optional composite makes every wire field a pointer
	// while the parts inside it keep their own shape.
	PartType     string
	PartBaseType string
	PartNullable bool
	// First and Last mark the ends of one composite's run of parts, so an emitter
	// that must write one call per composite can drive off the field list it
	// already walks.
	First bool
	Last  bool
}

// VOPart is one part of a composite value object, as the vos package declares
// it. The types here are written from INSIDE that package: a part wrapped in
// another value object of the same package is `Currency`, never `vos.Currency`.
type VOPart struct {
	Name        string
	SpecType    string
	GoType      string // includes the pointer when the part is optional
	BaseGoType  string
	Nullable    bool
	VOKind      string // "" | raw | enum | reuse | manual
	LabelKey    string
	Text        map[string]string
	Description string
}

// resolveCompositeDeclaration fills the composite half of a ValueObject: its
// parts and the rules it owns.
func resolveCompositeDeclaration(s *spec.Spec, sv spec.ValueObject, v *ValueObject) {
	for _, p := range sv.Parts {
		v.Parts = append(v.Parts, resolveVOPart(sv.Name, p))
	}
	byName := map[string]VOPart{}
	for _, p := range v.Parts {
		byName[p.Name] = p
	}
	lookup := func(name string) *Field {
		p, ok := byName[name]
		if !ok {
			return nil
		}
		f := Field{
			Name: p.Name, SpecType: p.SpecType,
			GoType: p.GoType, BaseGoType: p.BaseGoType,
			EntityType: p.GoType, BaseEntityType: p.BaseGoType,
			VOKind: p.VOKind, Nullable: p.Nullable, LabelKey: p.LabelKey, Text: p.Text,
			JSONName: naming.Camel(p.Name), Description: p.Description,
		}
		return &f
	}
	v.Rules = resolveCompositeRules(sv.Rules, lookup)
}

func resolveVOPart(voName string, p spec.VOPart) VOPart {
	base := goTypeOf(p.Type)
	// A part that is itself a value object is declared as that type: the vos
	// package is the value object's own home, so the reference is unqualified.
	if p.VO != nil && p.VO.Kind != "" && p.VO.Kind != "none" {
		base = p.VO.Ref
	}
	goType := base
	if p.Nullable {
		goType = "*" + base
	}
	kind := ""
	if p.VO != nil {
		kind = p.VO.Kind
	}
	return VOPart{
		Name: p.Name, SpecType: p.Type, GoType: goType, BaseGoType: base,
		Nullable: p.Nullable, VOKind: kind,
		LabelKey:    spec.PartLabelKey(voName, p),
		Text:        p.Text.Map(),
		Description: p.Description,
	}
}

// resolveCompositeRules lowers a composite's own invariants. There is no verb
// gate: the framework validates every value-object field on every write, so a
// composite's rules are a flat list checked whenever its value is.
func resolveCompositeRules(rs spec.Rules, lookup func(string) *Field) []Rule {
	var out []Rule
	for _, r := range rs.List {
		rule := Rule{
			ID: r.ID, Kind: r.Kind, Operator: r.Operator,
			Min: r.Min, Max: r.Max, Notification: r.Notification,
			AttachTo: r.AttachTo, EchoValue: r.Echoes(), Description: r.Description,
			SkipWhen: r.SkipWhen,
		}
		for _, fn := range r.Fields {
			if f := lookup(fn); f != nil {
				rule.Fields = append(rule.Fields, *f)
			}
		}
		if r.Other != "" {
			rule.Other = lookup(r.Other)
		}
		if rule.AttachTo == "" && len(rule.Fields) > 0 {
			rule.AttachTo = rule.Fields[0].Name
		}
		out = append(out, rule)
	}
	return out
}

// expandComposite turns one composite field into the logical fields it
// contributes. The value object's declaration answers what each part IS; the
// field answers where it is stored and what it is called.
func expandComposite(s *spec.Spec, entity string, f spec.Field) []Field {
	voName := f.VO.Ref
	label := f.LabelKey
	if label == "" {
		label = entity + f.Name + "Field"
	}

	out := make([]Field, 0, len(f.Parts))
	for i, fp := range f.Parts {
		lf := spec.PartAsField(s, f, fp)
		base := goTypeOf(lf.Type)
		// The WIRE shape. An OPTIONAL composite is absent as a whole, so every one
		// of its parts is a pointer out here even when the part is mandatory
		// inside the value object — there is no value to send when the value
		// object is not there.
		goType := base
		if lf.Nullable {
			goType = "*" + base
		}

		partBase := base
		voKind := ""
		if lf.VO != nil && lf.VO.Kind != "" && lf.VO.Kind != "none" {
			voKind = lf.VO.Kind
			partBase = "vos." + lf.VO.Ref
		}
		// The part's own optionality, read from the DECLARATION — never derived
		// from lf.Nullable, which answers the other question (may the COLUMN be
		// NULL) and is true for every part of an optional composite.
		partNullable := spec.PartOptional(s, f, fp)
		partType := partBase
		if partNullable {
			partType = "*" + partBase
		}

		out = append(out, Field{
			Name: lf.Name, Column: lf.Column, SpecType: lf.Type,
			GoType: goType, BaseGoType: base,
			// A part is never assigned through EntityType: the command mapper
			// builds the value object as a whole. They are set to the part's own
			// shape so anything reading them (the VO-example lookup in the test
			// emitter, for one) sees the truth.
			EntityType: partType, BaseEntityType: partBase, VOKind: voKind,
			Nullable: lf.Nullable, Length: lf.Length,
			JSONName: naming.Camel(lf.Name), LabelKey: lf.LabelKey, Text: lf.Text.Map(),
			Example: lf.Example, Description: lf.Description,
			LivesOn: f.LivesOn,
			// Hidden is the OWNER's answer: a value object is one concept, so it
			// leaves the responses whole or not at all. Declaring it per part
			// would expose half a value and call it the value.
			Hidden: f.Hidden,
			// Uniqueness is the owner's too, and it lands on the FIRST part alone
			// so the constraint is built once, over the whole run, rather than
			// once per column. A value object is unique as a tuple: a constraint
			// on one part would refuse rows the domain accepts.
			Unique: uniqueOfOwner(f, i == 0),
			Composite: &CompositePart{
				VOName: voName, VOType: "vos." + voName,
				Owner: f.Name, OwnerNullable: f.Nullable,
				OwnerLabelKey: label, OwnerText: f.Text.Map(), OwnerDescription: f.Description,
				PartName: fp.Part, Exposed: lf.Name,
				PartType: partType, PartBaseType: partBase, PartNullable: partNullable,
				First: i == 0, Last: i == len(f.Parts)-1,
			},
		})
	}
	return out
}

// Composites lists the composite value objects a field slice carries, once
// each, in declaration order — the form the four domain-facing emitters need,
// where a composite is ONE thing rather than N.
func Composites(fields []Field) []CompositeGroup {
	var out []CompositeGroup
	for _, f := range fields {
		if f.Composite == nil {
			continue
		}
		if f.Composite.First {
			out = append(out, CompositeGroup{Head: *f.Composite})
		}
		if len(out) > 0 {
			out[len(out)-1].Parts = append(out[len(out)-1].Parts, f)
		}
	}
	return out
}

// CompositeGroup is one composite value object as an entity carries it: the
// provenance shared by every part, plus the parts themselves in declaration
// order.
type CompositeGroup struct {
	Head  CompositePart
	Parts []Field
}

// Owner is the entity field the value object lives in.
func (g CompositeGroup) Owner() string { return g.Head.Owner }

// Optional reports whether the composite is held as a pointer — absent as a
// whole when every part column is NULL.
func (g CompositeGroup) Optional() bool { return g.Head.OwnerNullable }

// GoType is how the field is declared on the aggregate.
func (g CompositeGroup) GoType() string {
	if g.Head.OwnerNullable {
		return "*" + g.Head.VOType
	}
	return g.Head.VOType
}

// HasComposites reports whether any of these fields is a composite's part —
// the question every emitter that must import the vos package asks.
func HasComposites(fields []Field) bool {
	for _, f := range fields {
		if f.Composite != nil {
			return true
		}
	}
	return false
}

// PlainAndComposites splits a field list into the fields that stand alone and
// the composites, preserving the order a reader of the spec expects.
func PlainAndComposites(fields []Field) (plain []Field, groups []CompositeGroup) {
	for _, f := range fields {
		if f.Composite == nil {
			plain = append(plain, f)
		}
	}
	return plain, Composites(fields)
}

// UsesComposites reports whether this entity carries a composite value object
// anywhere its own struct reaches.
func (m *Model) UsesComposites() bool {
	if HasComposites(m.Fields) {
		return true
	}
	for _, sib := range m.Siblings {
		if HasComposites(sib.Fields) {
			return true
		}
	}
	for _, c := range m.Children {
		if HasComposites(c.Fields) {
			return true
		}
	}
	return false
}

// uniqueOfOwner carries a composite field's uniqueness onto the head of its run
// of parts, and onto nothing else. The constraint spans every part column, so
// exactly one part must claim it — attaching it to each would emit one
// single-column constraint per part, which is the opposite of what the key says.
func uniqueOfOwner(f spec.Field, head bool) *Unique {
	if !head || f.Unique == nil {
		return nil
	}
	scope := f.Unique.Scope
	if scope == "" {
		scope = "all"
	}
	return &Unique{Enforce: f.Unique.Enforce, Notification: f.Unique.Notification, Scope: scope}
}
