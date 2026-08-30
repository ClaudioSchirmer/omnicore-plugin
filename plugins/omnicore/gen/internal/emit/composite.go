package emit

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
)

// Composite value objects, emitted.
//
// There are exactly two shapes to write and four places to write them. The
// shapes: the value object itself, here, and the schema's decomposition of it,
// in the infra emitter. The places that must know: those two, plus the aggregate
// struct (one field, not N) and the command mappers (fold the flat wire fields
// back into the value object). Everything else — the migration, the view, the
// request and response DTOs, the listing, the exports, the generated tests —
// reads the expanded parts as ordinary fields and never asks.

// schemaFieldCalls renders one schema builder call per LOGICAL declaration: a
// Field(go, column) for an ordinary field, and one Composite(...) for a value
// object that spans several columns.
//
// It is the second and last place the expansion is undone, and it is where the
// framework's own division lands: the parts stay parts everywhere except in the
// domain (which sees the concept) and in this chain (which is the only thing
// that knows both).
//
// indent is the leading tabs of a CONTINUATION line — a composite spans several,
// and a sibling's chain is nested one level deeper than the root's.
func schemaFieldCalls(fields []ir.Field, indent string) []string {
	var out []string
	for _, f := range fields {
		if f.Composite == nil {
			if f.Redaction != nil {
				out = append(out, redactedFieldCall(f, indent))
				continue
			}
			// A framework-owned column is declared by a different builder, and
			// that declaration is the WHOLE difference: the schema is what
			// decides the column is never written from the struct, and what
			// filling it means. Everything above this line — the migration, the
			// read model, the filters, the exports — treats it as an ordinary
			// field, because it is one everywhere except here.
			if verb := stampedVerb(f); verb != "" {
				out = append(out, fmt.Sprintf("%s(%s, %s)", verb, quote(f.Name), quote(f.Column)))
				continue
			}
			out = append(out, fmt.Sprintf("Field(%s, %s)", quote(f.Name), quote(f.Column)))
			continue
		}
		if !f.Composite.First {
			continue
		}
		out = append(out, compositeCall(compositeRun(fields, f.Composite.Owner), indent))
	}
	return out
}

// stampedVerb names the TableSchema builder that declares a framework-owned
// column, and returns "" for an ordinary field.
//
// The two verbs take the same two arguments as Field and differ only in what
// the framework then does with the column, which is why the mapping is a lookup
// and not a shape: `time` binds the write operation's single instant, `counter`
// binds 1 on the insert and `col = col + 1` after it.
func stampedVerb(f ir.Field) string {
	switch f.Stamped {
	case "time":
		return "StampedTimeField"
	case "counter":
		return "StampedCounterField"
	}
	return ""
}

// compositeRun collects the parts belonging to one composite, in declaration
// order.
func compositeRun(fields []ir.Field, owner string) []ir.Field {
	var out []ir.Field
	for _, f := range fields {
		if f.Composite != nil && f.Composite.Owner == owner {
			out = append(out, f)
		}
	}
	return out
}

// compositeCall renders the decomposition.
//
// The entity field is located BY TYPE, which is why the value object is named
// and the field is not: an entity carries at most one field of a given composite
// type — the framework's once rule — so the type IS the address.
func compositeCall(parts []ir.Field, indent string) string {
	if len(parts) == 0 {
		return ""
	}
	c := parts[0].Composite
	var b strings.Builder
	fmt.Fprintf(&b, "Composite(core.NewCompositeValueObject[%s]().", c.VOType)
	for i, p := range parts {
		if p.Redaction != nil {
			// A part is redacted INDEPENDENTLY of its siblings inside the value
			// object, which is why this is per part and not on the Composite call:
			// the currency of a salary is not sensitive, the amount is.
			fmt.Fprintf(&b, "\n%s\tRedactedField(%s, %s,", indent,
				quote(p.Composite.PartName), quote(p.Column))
			fmt.Fprintf(&b, "\n%s\t\tcore.InSync(%s),", indent, redactorExpr(p.Redaction.InSync, p))
			fmt.Fprintf(&b, "\n%s\t\tcore.InAudit(%s))", indent, redactorExpr(p.Redaction.InAudit, p))
		} else {
			fmt.Fprintf(&b, "\n%s\tField(%s, %s)", indent, quote(p.Composite.PartName), quote(p.Column))
		}
		// The alias is written only when it says something: the default exposed
		// name is the part's own, and repeating it would read as a decision.
		if p.Composite.Exposed != p.Composite.PartName {
			fmt.Fprintf(&b, ".As(%s)", quote(p.Composite.Exposed))
		}
		if i < len(parts)-1 {
			b.WriteString(".")
		}
	}
	b.WriteString(")")
	return b.String()
}

// schemaNeedsVOs reports whether the root's schema file has to import the vos
// package. Only a composite makes it: a scalar value object is mapped by its
// FIELD name, so the schema never mentions the type.
func schemaNeedsVOs(m *ir.Model) bool {
	if ir.HasComposites(roleColumns(m)) {
		return true
	}
	for _, sib := range m.SiblingsOn("") {
		if ir.HasComposites(sib.Fields) {
			return true
		}
	}
	return false
}

// childSchemaNeedsVOs is the same question for a collection entry's schema.
//
// It is a separate file with its own imports, and it crosses two packages that
// look alike and are not: the ENTRY is a type of aggregatevos, while the value
// object it carries is a type of vos. Getting this wrong produces an undefined
// symbol at build time rather than anything the generator would notice.
func childSchemaNeedsVOs(m *ir.Model, c ir.Child) bool {
	if ir.HasComposites(c.Fields) {
		return true
	}
	for _, sib := range m.SiblingsOn(c.Name) {
		if ir.HasComposites(sib.Fields) {
			return true
		}
	}
	return false
}

// ownColumnsOf are the entry's fields stored on the child's OWN table — a
// facet's fields are declared inside their own Sibling block instead.
func ownColumnsOf(c ir.Child) []ir.Field {
	var out []ir.Field
	for _, f := range c.Fields {
		if f.Facet == "" {
			out = append(out, f)
		}
	}
	return out
}

// emitStructFields writes a run of resolved fields as Go struct members —
// the ONE place in the domain layer where the expansion is undone.
//
// A composite's parts arrive here as several fields, because that is what they
// are to the table, the wire and everything that reads them. The aggregate is
// the other side of that: it declares the CONCEPT, one field of the value
// object's type, and never sees the columns. The parts' own labelKeys are not
// repeated either — they live inside the value object, which owns its
// vocabulary for every entity that uses it.
func emitStructFields(s *src, fields []ir.Field) {
	for _, f := range fields {
		if f.Composite == nil {
			s.L("\t%s %s `labelKey:%s`%s", f.Name, f.EntityType, quote(f.LabelKey), fieldComment(f))
			continue
		}
		if !f.Composite.First {
			continue
		}
		c := f.Composite
		typ := c.VOType
		if c.OwnerNullable {
			typ = "*" + c.VOType
		}
		comment := ""
		if c.OwnerDescription != "" {
			comment = " // " + firstLine(c.OwnerDescription)
		}
		s.L("\t%s %s `labelKey:%s`%s", c.Owner, typ, quote(c.OwnerLabelKey), comment)
	}
}

// ---------------------------------------------------------------- fold / unfold
//
// The wire carries the parts flat and the domain carries the value object whole,
// so exactly one seam has to convert between them — and it is the command
// mapper, the same place a scalar value object is cast. Neither side knows about
// the other: the framework decomposes for storage, the application composes from
// the request.

// partValue renders the expression assigned to ONE part when building the value
// object, reading the flat field off `from`.
//
// Two nullabilities meet here and they are not the same question. The WIRE field
// is a pointer when the part is optional OR when the whole value object is; the
// PART is a pointer only when the value object says so. The case where they
// differ — a mandatory part of an optional composite — is the one that needs a
// dereference, and it is handled by the caller's nil guard rather than here.
func partValue(f ir.Field, from string) string {
	ref := from + "." + f.Name
	base := f.Composite.PartBaseType
	switch {
	case f.Nullable && f.Composite.PartNullable:
		if f.VOKind == "" {
			return ref
		}
		return fmt.Sprintf("(*%s)(%s)", base, ref)
	case f.Nullable:
		// The value object is optional and this part is not: the caller has
		// already proved the pointer is non-nil.
		if f.VOKind == "" {
			return "*" + ref
		}
		return fmt.Sprintf("%s(*%s)", base, ref)
	default:
		if f.VOKind == "" {
			return ref
		}
		return fmt.Sprintf("%s(%s)", base, ref)
	}
}

// emitCompositeFold writes the statements that build one composite value object
// from the flat fields of `from` and assign it to `target`.
//
// An OPTIONAL composite is decided as a GROUP, exactly as the read side decides
// it: every part absent means the value object is absent, and any part carrying
// a value makes it present. That symmetry is the point — a value written through
// this mapper and read back through the scan plan is the same value.
func emitCompositeFold(s *src, g ir.CompositeGroup, indent, target, from string) {
	c := g.Head
	if !c.OwnerNullable {
		s.L("%s%s = %s{", indent, target, c.VOType)
		for _, p := range g.Parts {
			s.L("%s\t%s: %s,", indent, p.Composite.PartName, partValue(p, from))
		}
		s.L("%s}", indent)
		return
	}

	local := naming.Camel(c.Owner)
	conds := make([]string, 0, len(g.Parts))
	for _, p := range g.Parts {
		conds = append(conds, from+"."+p.Name+" != nil")
	}
	s.L("%s// %s is optional as a WHOLE: nothing sent, nothing stored — the same", indent, c.Owner)
	s.L("%s// verdict the read side takes when every one of its columns is NULL.", indent)
	s.L("%sif %s {", indent, strings.Join(conds, " || "))
	s.L("%s\t%s := %s{}", indent, local, c.VOType)
	for _, p := range g.Parts {
		if p.Composite.PartNullable {
			s.L("%s\t%s.%s = %s", indent, local, p.Composite.PartName, partValue(p, from))
			continue
		}
		s.L("%s\tif %s.%s != nil {", indent, from, p.Name)
		s.L("%s\t\t%s.%s = %s", indent, local, p.Composite.PartName, partValue(p, from))
		s.L("%s\t}", indent)
	}
	s.L("%s\t%s = &%s", indent, target, local)
	s.L("%s}", indent)
}

// emitFieldAssignments writes the wire→domain assignments of a full write: a
// plain cast per ordinary field, and one folded value object per composite.
func emitFieldAssignments(s *src, fields []ir.Field, indent, target, from string) {
	plain, groups := ir.PlainAndComposites(fields)
	for _, f := range plain {
		s.L("%s%s.%s = %s", indent, target, f.Name, entityValue(f, from+"."+f.Name))
	}
	for _, g := range groups {
		emitCompositeFold(s, g, indent, target+"."+g.Owner(), from)
	}
}

// emitCompositePatch is the fold a PARTIAL update needs: each part is applied
// only when the caller sent it, and the value object is materialised on first
// use so a patch can fill in an absent one.
//
// A patch can never CLEAR it, and that is not a gap here: an absent field and an
// explicit null are indistinguishable in a partial body, which is the same
// reason a nullable scalar cannot be nulled by PATCH either.
func emitCompositePatch(s *src, g ir.CompositeGroup, indent, target, from string) {
	c := g.Head
	if !c.OwnerNullable {
		for _, p := range g.Parts {
			s.L("%sif %s.%s != nil {", indent, from, p.Name)
			s.L("%s\t%s.%s = %s", indent, target, p.Composite.PartName, patchPartValue(p, from))
			s.L("%s}", indent)
		}
		return
	}
	conds := make([]string, 0, len(g.Parts))
	for _, p := range g.Parts {
		conds = append(conds, from+"."+p.Name+" != nil")
	}
	s.L("%sif %s {", indent, strings.Join(conds, " || "))
	s.L("%s\tif %s == nil {", indent, target)
	s.L("%s\t\t%s = &%s{}", indent, target, c.VOType)
	s.L("%s\t}", indent)
	for _, p := range g.Parts {
		s.L("%s\tif %s.%s != nil {", indent, from, p.Name)
		s.L("%s\t\t%s.%s = %s", indent, target, p.Composite.PartName, patchPartValue(p, from))
		s.L("%s\t}", indent)
	}
	s.L("%s}", indent)
}

// patchPartValue is partValue under a patch, where EVERY wire field is a
// pointer and the nil guard has already been written by the caller.
func patchPartValue(f ir.Field, from string) string {
	ref := from + "." + f.Name
	base := f.Composite.PartBaseType
	if f.Composite.PartNullable {
		if f.VOKind == "" {
			return ref
		}
		return fmt.Sprintf("(*%s)(%s)", base, ref)
	}
	if f.VOKind == "" {
		return "*" + ref
	}
	return fmt.Sprintf("%s(*%s)", base, ref)
}

// emitCompositeUnfold writes the statements that flatten one composite back onto
// the wire: reading the value object off `from` and assigning each part to the
// flat field of `target`.
func emitCompositeUnfold(s *src, g ir.CompositeGroup, indent, target, from string) {
	c := g.Head
	owner := from + "." + c.Owner
	if !c.OwnerNullable {
		for _, p := range g.Parts {
			s.L("%s%s.%s = %s", indent, target, p.Name, unfoldPart(p, owner, false))
		}
		return
	}
	s.L("%sif %s != nil {", indent, owner)
	for _, p := range g.Parts {
		if p.Composite.PartNullable {
			s.L("%s\t%s.%s = %s", indent, target, p.Name, unfoldPart(p, owner, false))
			continue
		}
		// The wire field is a pointer because the value object is optional, and a
		// pointer needs something to point AT — the value has to be copied into a
		// local first, or every row would share the last one's address.
		local := naming.Camel(p.Name)
		s.L("%s\t%s := %s", indent, local, unfoldPart(p, owner, true))
		s.L("%s\t%s.%s = &%s", indent, target, p.Name, local)
	}
	s.L("%s}", indent)
}

// unfoldPart renders one part AS THE WIRE SEES IT — the underlying scalar,
// never the value object. bare asks for the value itself rather than a pointer
// to it, for the copy an optional composite's mandatory part needs.
func unfoldPart(f ir.Field, owner string, bare bool) string {
	ref := owner + "." + f.Composite.PartName
	switch {
	case f.Composite.PartNullable && !bare:
		if f.VOKind == "" {
			return ref
		}
		return fmt.Sprintf("(*%s)(%s)", f.BaseGoType, ref)
	default:
		if f.VOKind == "" {
			return ref
		}
		return ref + ".Value()"
	}
}

// compositeSample renders a composite value object as a Go literal, for the
// fixture every generated rule test starts from.
//
// It cannot go through the ordinary field sampler: that one reads the WIRE
// nullability, which is true for every part of an optional composite even when
// the part itself is mandatory. Inside the value object the part's own shape is
// what counts, and mixing the two produces a pointer where a value belongs.
func compositeSample(g ir.CompositeGroup) string {
	parts := make([]string, 0, len(g.Parts))
	for _, p := range g.Parts {
		lit := literalFor(p)
		if p.VOKind != "" {
			lit = fmt.Sprintf("%s(%s)", p.Composite.PartBaseType, lit)
		}
		if p.Composite.PartNullable {
			lit = fmt.Sprintf("func() *%s { v := %s(%s); return &v }()",
				p.Composite.PartBaseType, p.Composite.PartBaseType, lit)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", p.Composite.PartName, lit))
	}
	lit := fmt.Sprintf("%s{%s}", g.Head.VOType, strings.Join(parts, ", "))
	if g.Optional() {
		return "&" + lit
	}
	return lit
}

// compositeAlternate builds a value object that DIFFERS from compositeSample —
// what an immutability test assigns to prove the change is refused.
//
// Exactly one part moves, and it is the first part that is not itself a value
// object when there is one. Moving every part would work too, but a part typed
// as an enum has no "other value" this emitter can invent: the alternate would
// be outside the declared set, and the enum's own notification would fire beside
// the one under test. One part is enough — a tuple differs when any member does.
func compositeAlternate(g ir.CompositeGroup) string {
	move := 0
	for i, p := range g.Parts {
		if p.VOKind == "" {
			move = i
			break
		}
	}
	parts := make([]string, 0, len(g.Parts))
	for i, p := range g.Parts {
		lit := literalFor(p)
		if i == move {
			lit = alternateValue(ir.Field{SpecType: p.SpecType})
		}
		if p.VOKind != "" {
			lit = fmt.Sprintf("%s(%s)", p.Composite.PartBaseType, lit)
		}
		if p.Composite.PartNullable {
			lit = fmt.Sprintf("func() *%s { v := %s(%s); return &v }()",
				p.Composite.PartBaseType, p.Composite.PartBaseType, lit)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", p.Composite.PartName, lit))
	}
	lit := fmt.Sprintf("%s{%s}", g.Head.VOType, strings.Join(parts, ", "))
	if g.Optional() {
		return "&" + lit
	}
	return lit
}

// compositeGroupNamed finds the composite a rule addressed as a whole, or nil
// when the name is an ordinary field.
func compositeGroupNamed(m *ir.Model, owner string) *ir.CompositeGroup {
	for _, g := range ir.Composites(m.AllOwnerFields()) {
		if g.Owner() == owner {
			out := g
			return &out
		}
	}
	return nil
}

// emitEntityLiteralFields writes the members of an aggregate's struct literal:
// one entry per ordinary field, and one whole value object per composite.
func emitEntityLiteralFields(s *src, fields []ir.Field, indent string) {
	plain, groups := ir.PlainAndComposites(fields)
	for _, f := range plain {
		s.L("%s%s: %s,", indent, f.Name, sampleValue(f))
	}
	for _, g := range groups {
		s.L("%s%s: %s,", indent, g.Owner(), compositeSample(g))
	}
}

// emitCompositeVO writes a value object whose value spans several fields.
//
// What makes it a composite is what it does NOT declare: there is no Value(),
// because there is no single scalar to yield. That absence is the framework's
// discriminator — a value object with Value() occupies one column and is mapped
// with Field(...), one without spans several and is decomposed with
// Composite(...) — so adding a Value() here would silently turn the type back
// into a scalar one and fail the boot at the schema that decomposes it.
func emitCompositeVO(m *ir.Model, vo ir.ValueObject) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package vos")
	s.Blank()

	imports := compositeImports(vo)
	if len(imports) == 1 {
		s.L("import %s", quote(imports[0]))
	} else {
		s.L("import (")
		for _, imp := range imports {
			s.L("\t%s", quote(imp))
		}
		s.L(")")
	}
	s.Blank()

	doc := []string{fmt.Sprintf("%s is a value object whose value spans several fields.", vo.Name)}
	if vo.Description != "" {
		doc = append(doc, "", vo.Description)
	}
	doc = append(doc,
		"",
		"It declares no Value(), and that absence is the whole definition: a value "+
			"object that yields one scalar occupies one column, and one that yields none "+
			"occupies as many as it has parts. Which columns those are is declared once, "+
			"in the TableSchema — this type never learns it is stored at all, and neither "+
			"does anything reading it back.",
		"",
		"The framework finds every field of this type and validates it on each write, "+
			"exactly as it does the single-scalar kinds, so no rule elsewhere repeats "+
			"what IsValid below already checks.")
	s.Doc(doc...)
	s.L("type %s struct {", vo.Name)
	for _, p := range vo.Parts {
		s.L("\t%s %s `labelKey:%s`%s", p.Name, p.GoType, quote(p.LabelKey), partComment(p))
	}
	s.L("}")
	s.Blank()

	emitCompositeIsValid(s, m, vo)

	return goFile("internal/domain/vos/"+naming.Snake(vo.Name)+".go", fsplan.Owned,
		fmt.Sprintf("the %s composite value object (%d parts)", vo.Name, len(vo.Parts)), s)
}

func compositeImports(vo ir.ValueObject) []string {
	need := map[string]bool{fwImport("domain"): true}
	for _, p := range vo.Parts {
		if p.SpecType == "time" && p.VOKind == "" {
			need["time"] = true
		}
	}
	var out []string
	if need["time"] {
		out = append(out, "time")
	}
	out = append(out, fwImport("domain"))
	return out
}

func partComment(p ir.VOPart) string {
	if p.Description == "" {
		return ""
	}
	return " // " + firstLine(p.Description)
}

// emitCompositeIsValid writes the rule the value object owns.
//
// Two things run here and nowhere else. First, the parts that are THEMSELVES
// value objects: the framework's automatic pass validates the composite, never
// its interior, so an enum part's membership and a raw part's format are checked
// from in here or not at all. Second, the cross-field invariants — the ones a
// single-scalar value object cannot express, and the reason this kind exists.
func emitCompositeIsValid(s *src, m *ir.Model, vo ir.ValueObject) {
	s.Doc(
		"IsValid is the framework's entry point.",
		"",
		"It reports every problem it finds through the context rather than returning "+
			"one, so a caller sees all of them at once. The field name it is handed is "+
			"the ENTITY's field — the concept as a whole — while a problem about one "+
			"part is attached to that part's own name, which is what makes a message "+
			"about an amount say so instead of naming the value object.",
		"",
		"A part that is itself a value object is validated HERE: the framework's "+
			"automatic pass validates this composite, never its interior.")
	// The field name is deliberately unused: every problem this body reports is
	// about ONE PART, and a part is named by the value object, not by the entity
	// carrying it. A rule about the composite as a whole is a rule about the
	// entity's field, and it is declared there.
	s.L("func (v %s) IsValid(_ string, ctx *domain.NotificationContext) bool {", vo.Name)
	s.L("\tok := true")

	for _, p := range vo.Parts {
		emitPartVOCheck(s, p)
	}
	for _, rule := range vo.Rules {
		emitCompositeRule(s, m, rule)
	}

	s.L("\treturn ok")
	s.L("}")
}

// emitPartVOCheck validates a part that carries a value object of its own.
func emitPartVOCheck(s *src, p ir.VOPart) {
	if p.VOKind == "" || p.VOKind == "none" {
		return
	}
	ref := "v." + p.Name
	closing := 0
	if p.Nullable {
		s.L("\tif %s != nil {", ref)
		ref = "*" + ref
		closing++
	}
	switch p.VOKind {
	case "enum":
		s.L("\t// Membership is not a rule this type writes: the enum declares its set,")
		s.L("\t// and anything outside it arrives as the unknown sentinel.")
		s.L("\tif !domain.ValidateEnum(%s, %s, ctx) {", ref, quote(p.Name))
	default:
		// raw / reuse: the value object owns an IsValid and reports through the
		// same context, so calling it is the whole check.
		s.L("\tif !%s.IsValid(%s, ctx) {", ref, quote(p.Name))
	}
	s.L("\t\tok = false")
	s.L("\t}")
	for i := 0; i < closing; i++ {
		s.L("\t}")
	}
}

// emitCompositeRule writes one declared invariant of the value object.
//
// The shape differs from the entity's rule DSL in one way that matters: there is
// no verb gate. A value object is validated whenever its value is, so a rule
// that must fire on one verb only is a rule about the ENTITY and is refused at
// validation rather than emitted here as something that fires always.
func emitCompositeRule(s *src, m *ir.Model, rule ir.Rule) {
	if rule.Description != "" {
		s.L("\t// %s", firstLine(rule.Description))
	}
	switch rule.Kind {
	case "required":
		for _, f := range rule.Fields {
			s.L("\tif %s {", voZeroCheck(f))
			emitCompositeRaise(s, m, rule, f)
			s.L("\t}")
		}
	case "range":
		for _, f := range rule.Fields {
			var conds []string
			val := deref(f, "v")
			if rule.Min != nil {
				conds = append(conds, fmt.Sprintf("%s < %s", val, number(*rule.Min, f.SpecType)))
			}
			if rule.Max != nil {
				conds = append(conds, fmt.Sprintf("%s > %s", val, number(*rule.Max, f.SpecType)))
			}
			body := strings.Join(conds, " || ")
			if f.Nullable {
				s.L("\tif v.%s != nil && (%s) {", f.Name, body)
			} else {
				s.L("\tif %s {", body)
			}
			emitCompositeRaise(s, m, rule, f)
			s.L("\t}")
		}
	case "length":
		for _, f := range rule.Fields {
			val := deref(f, "v")
			var conds []string
			if rule.Min != nil {
				conds = append(conds, fmt.Sprintf("len(%s) < %d", val, int(*rule.Min)))
			}
			if rule.Max != nil {
				conds = append(conds, fmt.Sprintf("len(%s) > %d", val, int(*rule.Max)))
			}
			body := strings.Join(conds, " || ")
			if f.Nullable {
				s.L("\tif v.%s != nil && (%s) {", f.Name, body)
			} else {
				s.L("\tif %s {", body)
			}
			emitCompositeRaise(s, m, rule, f)
			s.L("\t}")
		}
	case "comparison":
		if rule.Other == nil || len(rule.Fields) == 0 {
			return
		}
		left, right := rule.Fields[0], *rule.Other
		var conds []string
		for _, g := range []string{comparisonGuard(left, "v"), comparisonGuard(right, "v")} {
			if g != "" && g != "true" {
				conds = append(conds, g)
			}
		}
		conds = append(conds, comparisonExpr(left, right, rule.Operator, "v"))
		s.L("\tif %s {", strings.Join(conds, " && "))
		emitCompositeRaise(s, m, rule, left)
		s.L("\t}")
	case "requiredIf":
		if rule.Other == nil {
			return
		}
		s.L("\tif %s {", presenceCheck(*rule.Other, "v"))
		for _, f := range rule.Fields {
			s.L("\t\tif %s {", voZeroCheck(f))
			s.L("\t\t\tctx.AddNotification(%s, %s%s)",
				quote(attachOf(rule, f)), notifLiteralFor(rule, m), echoArgOn(rule, f, "v"))
			s.L("\t\t\tok = false")
			s.L("\t\t}")
		}
		s.L("\t}")
	default:
		// Unreachable: validation refuses any other kind on a value object.
		s.L("\t// unsupported composite rule kind %q (id %s) — this is a generator bug",
			rule.Kind, rule.ID)
	}
}

func emitCompositeRaise(s *src, m *ir.Model, rule ir.Rule, f ir.Field) {
	s.L("\t\tctx.AddNotification(%s, %s%s)",
		quote(attachOf(rule, f)), notifLiteralFor(rule, m), echoArgOn(rule, f, "v"))
	s.L("\t\tok = false")
}

// attachOf is the part a refusal is reported against: the one the spec named,
// or the rule's subject.
func attachOf(rule ir.Rule, f ir.Field) string {
	if rule.AttachTo != "" {
		return rule.AttachTo
	}
	return f.Name
}

// voZeroCheck is zeroCheck with the value object's receiver — "this part carries
// nothing".
func voZeroCheck(f ir.Field) string { return zeroCheck(f, "v") }

// presenceCheck is the affirmative twin: "this part carries a value", which is
// what makes a conditional requirement fire.
func presenceCheck(f ir.Field, recv string) string {
	if f.Nullable {
		return recv + "." + f.Name + " != nil"
	}
	return "!(" + zeroCheck(f, recv) + ")"
}

// ---------------------------------------------------------------- generated tests

// emitCompositeVOTests covers a composite value object the way the raw and enum
// kinds are covered: one case proving it ACCEPTS a well-formed value, and one
// case per declared rule proving it refuses.
//
// The pair is the point. A value object that rejects everything passes every
// negative test there is, and a composite is where that is easiest to write by
// accident: a cross-field rule reading the two operands in the wrong order
// refuses every value and looks, from one test, like a working invariant.
func emitCompositeVOTests(s *src, m *ir.Model, vo ir.ValueObject) {
	literal := voLiteral(m, vo)

	s.Doc(
		fmt.Sprintf("%s accepts a well-formed value.", vo.Name),
		"",
		"It is the half a negative test cannot cover: a rule written against the "+
			"wrong operand refuses everything and passes every case below.")
	s.L("func Test%sAcceptsAWellFormedValue(t *testing.T) {", vo.Name)
	s.L("\tctx := domain.NewNotificationContext(%s)", quote(vo.Name))
	s.L("\tif !(%s).IsValid(%s, ctx) {", literal, quote(vo.Name))
	s.L("\t\tt.Error(\"a well-formed value was refused\")")
	s.L("\t}")
	s.L("}")
	s.Blank()

	// The seam that is easiest to break and hardest to notice: the framework's
	// automatic pass validates the COMPOSITE and never its interior, so a part
	// that is itself a value object is checked from inside IsValid or not at all.
	// Dropping that call leaves an out-of-set enum stored, with every other test
	// still green — the enum's own membership test passes, because the enum is
	// fine; it is the composite that stopped asking.
	for _, p := range vo.Parts {
		inner := declaredVO(m, p.BaseGoType)
		if p.VOKind == "" || p.VOKind == "reuse" || inner == nil {
			continue
		}
		s.Doc(fmt.Sprintf("%s validates its %s part, which nothing else does.",
			vo.Name, p.Name))
		s.L("func Test%s_%s_IsValidated(t *testing.T) {", vo.Name, p.Name)
		s.L("\tctx := domain.NewNotificationContext(%s)", quote(vo.Name))
		s.L("\tv := %s", literal)
		bad := invalidSample(*inner)
		if p.Nullable {
			s.L("\tbad := %s(%s)", p.BaseGoType, bad)
			s.L("\tv.%s = &bad", p.Name)
		} else {
			s.L("\tv.%s = %s(%s)", p.Name, p.BaseGoType, bad)
		}
		s.L("\tif v.IsValid(%s, ctx) {", quote(vo.Name))
		s.L("\t\tt.Error(%s)", quote("a value the "+p.Name+" value object refuses was accepted"))
		s.L("\t}")
		s.L("}")
		s.Blank()
	}

	for _, rule := range vo.Rules {
		violation, part := compositeViolation(rule)
		if violation == "" {
			continue // no case this emitter can construct; the rule still runs
		}
		s.Doc(fmt.Sprintf("%s enforces %s.", vo.Name, rule.ID))
		s.L("func Test%s_%s(t *testing.T) {", vo.Name, ruleTestName(rule.ID))
		s.L("\tctx := domain.NewNotificationContext(%s)", quote(vo.Name))
		s.L("\tv := %s", literal)
		s.L("\tv.%s = %s", part, violation)
		s.L("\tif v.IsValid(%s, ctx) {", quote(vo.Name))
		s.L("\t\tt.Error(%s)", quote(rule.ID+" was not enforced"))
		s.L("\t}")
		s.L("}")
		s.Blank()
	}
}

// compositeViolation renders a value the rule is GUARANTEED to refuse, and the
// part to plant it in. It answers "" when the kind has no case this emitter can
// construct on its own.
func compositeViolation(rule ir.Rule) (value, part string) {
	if len(rule.Fields) == 0 {
		return "", ""
	}
	f := rule.Fields[0]
	switch rule.Kind {
	case "required", "requiredIf":
		return pointerize(f, zeroValue(f)), f.Name
	case "range":
		if rule.Max != nil {
			return pointerize(f, overMax(f, *rule.Max)), f.Name
		}
		if rule.Min != nil {
			return pointerize(f, underMin(f, *rule.Min)), f.Name
		}
	case "length":
		if rule.Max != nil {
			return pointerize(f, fmt.Sprintf("strings.Repeat(%s, %d)", quote("x"), int(*rule.Max)+1)), f.Name
		}
		if rule.Min != nil {
			return pointerize(f, quote("")), f.Name
		}
	case "comparison":
		if rule.Other != nil {
			return pointerize(f, violatingComparison(f, *rule.Other, rule.Operator)), f.Name
		}
	}
	return "", ""
}

// voLiteral renders a composite as a Go literal FROM INSIDE the vos package,
// where its own types are unqualified.
//
// The sample for each part is the `example:` an entity's field declared for it —
// the one place in the spec that says what a plausible value looks like. Without
// it an enum part would be sampled as its own lower-cased name, which is not a
// member, and the accepts-a-valid-value case would fail against a correct
// generator.
func voLiteral(m *ir.Model, vo ir.ValueObject) string {
	parts := make([]string, 0, len(vo.Parts))
	for _, p := range vo.Parts {
		lit := literalFor(ir.Field{
			SpecType: p.SpecType,
			Name:     p.Name,
			Example:  compositePartExample(m, vo.Name, p.Name),
		})
		if p.VOKind != "" {
			lit = fmt.Sprintf("%s(%s)", p.BaseGoType, lit)
		}
		if p.Nullable {
			lit = fmt.Sprintf("func() *%s { v := %s(%s); return &v }()", p.BaseGoType, p.BaseGoType, lit)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", p.Name, lit))
	}
	return fmt.Sprintf("%s{%s}", vo.Name, strings.Join(parts, ", "))
}

// compositePartExample finds the example an entity's field declared for one part
// of one composite. Any field carrying the value object answers: the example
// belongs to the part, and a value object used twice is the same value object.
func compositePartExample(m *ir.Model, voName, partName string) string {
	sets := [][]ir.Field{m.AllOwnerFields()}
	for _, c := range m.Children {
		sets = append(sets, c.Fields)
	}
	for _, set := range sets {
		for _, f := range set {
			if f.Composite == nil || f.Composite.VOName != voName {
				continue
			}
			if f.Composite.PartName == partName && f.Example != "" {
				return f.Example
			}
		}
	}
	return ""
}

// ruleTestName turns a rule's kebab-case id into a Go identifier. The id is the
// author's own words for the invariant, so it is what makes the failing test
// name say which rule broke.
func ruleTestName(id string) string {
	var b strings.Builder
	upper := true
	for _, r := range id {
		if r == '-' || r == '_' || r == ' ' {
			upper = true
			continue
		}
		if upper {
			b.WriteString(naming.Pascal(string(r)))
			upper = false
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// declaredVO finds a value object THIS spec declares, by the type name a part
// refers to it by. A reused one is not here — its rule lives in the spec that
// declares it, so this generator has no invalid sample to construct.
func declaredVO(m *ir.Model, name string) *ir.ValueObject {
	for i := range m.ValueObjects {
		if m.ValueObjects[i].Name == name {
			return &m.ValueObjects[i]
		}
	}
	return nil
}
