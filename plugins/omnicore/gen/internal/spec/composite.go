package spec

import (
	"fmt"
	"strings"
)

// Composite value objects, spec-side.
//
// A composite is a value object whose value spans several persisted columns —
// Money{Amount, Currency}, Period{From, To}, Address{Street, City, ZipCode}. It
// is declared in two halves, on purpose, because the halves belong to different
// owners:
//
//	valueObjects[] with kind: composite   → what the value object IS (its parts,
//	                                        their types, its own rules). Declared
//	                                        once, for every entity that uses it.
//	fields[].parts[]                      → where THIS entity stores them (one
//	                                        column per part, and the logical name
//	                                        each is exposed under).
//
// That split mirrors the framework exactly: the domain declares a plain struct
// that owns its rule and nothing else, and the TableSchema is the only place
// that knows it is stored across N columns. Downstream — criteria, audit, the
// projection, the read DTO, filters, orderBy, ?fields=, OpenAPI, GraphQL, gRPC
// and the exports — every part is an ordinary logical field under its exposed
// name, and nothing learns a composite exists.

// ExposedName is the logical name a part is known by everywhere outside the
// domain: the alias when one is declared, the part's own name otherwise.
func ExposedName(p FieldPart) string {
	if p.As != "" {
		return p.As
	}
	return p.Part
}

// IsComposite reports whether a field holds a composite value object.
func IsComposite(f Field) bool { return f.VO != nil && f.VO.Kind == "composite" }

// FindVOPart looks up one part of a composite declaration by its name inside
// the value object.
func FindVOPart(vo *ValueObject, name string) *VOPart {
	if vo == nil {
		return nil
	}
	for i := range vo.Parts {
		if vo.Parts[i].Name == name {
			return &vo.Parts[i]
		}
	}
	return nil
}

// PartLabelKey is the translation-catalog key a part resolves through. It is
// derived from the VALUE OBJECT, never from the entity: the value object owns
// its vocabulary for everyone that uses it, which is the same rule the framework
// applies when it reads the labelKey tag from inside the struct.
func PartLabelKey(voName string, p VOPart) string {
	if p.LabelKey != "" {
		return p.LabelKey
	}
	return voName + p.Name + "Field"
}

// PartAsField renders one part of a composite as the ordinary logical field
// every consumer downstream sees. It is what makes "nothing learns a composite
// exists" true in the validator too: a filter, an index or a ?fields= entry
// naming a part resolves through the same code path as any other field.
//
// The type/nullability resolution is the one place the two halves meet: the
// declaration answers when it is in this spec, the field's own restatement
// answers when the value object is reused from elsewhere.
func PartAsField(s *Spec, owner Field, fp FieldPart) Field {
	out := Field{
		Name:     ExposedName(fp),
		Type:     fp.Type,
		Column:   fp.Column,
		Length:   fp.Length,
		Nullable: fp.Nullable || owner.Nullable,
		LivesOn:  owner.LivesOn,
		Example:  fp.Example,
	}
	vo := findVO(s.ValueObjects, ownerRef(owner))
	if p := FindVOPart(vo, fp.Part); p != nil {
		if out.Type == "" {
			out.Type = p.Type
		}
		// The value object's own shape decides optionality inside the struct; the
		// field's decides it for the whole value object. Either makes the column
		// NULL-able, and an optional composite makes every one of them so —
		// "every part column NULL" is how absence is written and read back.
		out.Nullable = p.Nullable || fp.Nullable || owner.Nullable
		out.VO = p.VO
		out.LabelKey = PartLabelKey(vo.Name, *p)
		out.Description = p.Description
	}
	return out
}

// PartOptional reports whether a part is a POINTER INSIDE the value object —
// optional even when the value object itself is present, like Period{To}.
//
// It is a different question from the field's own nullability and must never be
// derived from it. A part of an optional composite is nullable on the wire and
// in the column because the whole value object may be absent, while inside the
// struct it keeps its own shape; deriving one from the other produces a pointer
// where a value belongs, in both directions, and every one of those is a
// compile error in the generated tree rather than anything the generator sees.
func PartOptional(s *Spec, owner Field, fp FieldPart) bool {
	if p := FindVOPart(findVO(s.ValueObjects, ownerRef(owner)), fp.Part); p != nil {
		return p.Nullable
	}
	// A REUSED composite is not declared here, so the field's restatement is the
	// only answer there is.
	return fp.Nullable
}

func ownerRef(f Field) string {
	if f.VO == nil {
		return ""
	}
	return f.VO.Ref
}

// CompositeParts expands one composite field into the logical fields it
// contributes. A field that is not a composite contributes itself, so a caller
// can walk a field list uniformly.
func CompositeParts(s *Spec, f Field) []Field {
	if !IsComposite(f) {
		return []Field{f}
	}
	out := make([]Field, 0, len(f.Parts))
	for _, p := range f.Parts {
		out = append(out, PartAsField(s, f, p))
	}
	return out
}

// LogicalFields is every field the read side and the wire can name on this
// entity's own struct: the plain fields, each composite's PARTS in place of the
// composite itself, and the facets' fields. It is the set a filter, an index, a
// ?fields= entry or a patchExcludes entry may draw from.
func LogicalFields(s *Spec) []Field {
	var out []Field
	for _, f := range s.Fields {
		if f.Runtime {
			continue
		}
		out = append(out, CompositeParts(s, f)...)
	}
	for _, sib := range s.Siblings {
		for _, f := range sib.Fields {
			out = append(out, CompositeParts(s, f)...)
		}
	}
	return out
}

// findLogicalField resolves a name against the expanded set — a composite's
// exposed part names included, the composite's OWN name deliberately not. A
// composite field has no single value, so nothing that reads or filters one
// value can address it.
func findLogicalField(fields []Field, s *Spec, name string) *Field {
	for _, f := range fields {
		if f.Runtime {
			continue
		}
		if !IsComposite(f) {
			if f.Name == name {
				g := f
				return &g
			}
			continue
		}
		for _, p := range f.Parts {
			if ExposedName(p) == name {
				g := PartAsField(s, f, p)
				return &g
			}
		}
	}
	return nil
}

// compositeOwnerNamed reports the composite field a name addresses AS A WHOLE —
// used only to answer "you named the value object, you meant one of its parts".
func compositeOwnerNamed(s *Spec, name string) *Field {
	for i := range s.Fields {
		if IsComposite(s.Fields[i]) && s.Fields[i].Name == name {
			return &s.Fields[i]
		}
	}
	for i := range s.Siblings {
		for j := range s.Siblings[i].Fields {
			f := &s.Siblings[i].Fields[j]
			if IsComposite(*f) && f.Name == name {
				return f
			}
		}
	}
	return nil
}

// exposedNamesOf lists a composite field's exposed part names, for an error
// message that offers the alternatives instead of only refusing.
func exposedNamesOf(f Field) string {
	names := make([]string, 0, len(f.Parts))
	for _, p := range f.Parts {
		names = append(names, ExposedName(p))
	}
	if len(names) == 0 {
		return "its parts"
	}
	return strings.Join(names, ", ")
}

// reportUnreadable says a name is not a readable field, and says WHY when the
// name is a composite value object. Refusing "Salary" with "does not name a
// readable field" is true and useless: the field is right there in the spec, and
// what the author has to learn is that a composite has no single value to read —
// its parts do, under the names it exposes them by.
func reportUnreadable(s *Spec, name, where string, ps *Problems) {
	if owner := compositeOwnerNamed(s, name); owner != nil {
		ps.BlockerFix(where,
			fmt.Sprintf("%q is a composite value object, so it has no single value to "+
				"read, filter or sort", name),
			fmt.Sprintf("name one of the parts it exposes: %s", exposedNamesOf(*owner)))
		return
	}
	ps.Blockerf(where, "%q does not name a readable field", name)
}

// ---------------------------------------------------------------- validation

// validateComposites checks everything about composite value objects that the
// per-field and per-value-object passes cannot see on their own: the two halves
// agreeing, the exposed names not colliding with anything else on the entity,
// and the framework's ONCE RULE, which is a property of the whole schema graph.
func validateComposites(s *Spec, ps *Problems, opt Options) {
	existing := map[string]bool{}
	for _, name := range opt.ExistingVOs {
		existing[name] = true
	}

	// The once rule, framework side: each composite type is decomposed by
	// EXACTLY ONE schema of an entity — the root, a sibling or the shared base.
	// The framework rejects a split at the boot checkpoint because the read side
	// cannot honour it: a sibling is loaded by its own statement, so a split
	// composite reconstructs half-built, and an optional one's "every part NULL
	// ⇒ absent" verdict cannot be reached by either half alone.
	usedBy := map[string]string{}
	claim := func(ref, where string) {
		if prev, dup := usedBy[ref]; dup {
			ps.BlockerFix(where,
				fmt.Sprintf("the composite value object %q is already carried by %s", ref, prev),
				"a composite is persisted by exactly ONE schema of an entity (the framework "+
					"refuses the boot otherwise, because a split value object reconstructs "+
					"half-built). Model the second occurrence as its own value-object type")
			return
		}
		usedBy[ref] = where
	}

	// A part is exposed under a logical name like any other field, so it shares
	// ONE namespace with them — and the same for its column. The plain-field
	// halves of both namespaces are already policed per field list; what needs
	// a pass of its own is every collision a part is on either side of, which no
	// per-field loop can see.
	seenLogical := map[string]string{}
	seenColumn := map[string]string{}
	note := func(kind, name, where string, viaPart bool, seen map[string]string) {
		if name == "" {
			return
		}
		prev, dup := seen[name]
		if !dup {
			seen[name] = where
			return
		}
		if !viaPart && !strings.Contains(prev, ".parts[") {
			return // two plain fields — already reported where they are declared
		}
		ps.BlockerFix(where,
			fmt.Sprintf("the %s %q is already used by %s", kind, name, prev),
			"a composite's part is an ordinary logical field by the time anything "+
				"downstream sees it, so it shares one namespace with the entity's own — "+
				"rename it with as: <Name>")
	}

	walk := func(fields []Field, prefix string, isChild bool) {
		for i, f := range fields {
			where := fmt.Sprintf("%s[%d] (%s)", prefix, i, orUnnamed(f.Name))
			if !IsComposite(f) {
				if len(f.Parts) > 0 {
					ps.BlockerFix(where+".parts",
						"parts belong to a composite value object, and this field is not one",
						"set vo: {kind: composite, ref: <Type>} — or drop parts and use "+
							"column: for this field's single column")
				}
				if !f.Runtime {
					note("field name", f.Name, where, false, seenLogical)
					note("column", f.LivesOn+"."+f.Column, where, false, seenColumn)
				}
				continue
			}
			validateCompositeField(s, f, where, ps, isChild, existing)
			claim(f.VO.Ref, where)
			// The composite's OWN name is in the logical namespace too: it is what
			// the entity's struct field is called.
			note("field name", f.Name, where, true, seenLogical)
			for j, p := range f.Parts {
				pw := fmt.Sprintf("%s.parts[%d] (%s)", where, j, orUnnamed(p.Part))
				note("logical name", ExposedName(p), pw, true, seenLogical)
				note("column", f.LivesOn+"."+p.Column, pw, true, seenColumn)
			}
		}
	}

	walk(s.Fields, "fields", false)
	for i := range s.Siblings {
		where := fmt.Sprintf("siblings[%d].fields", i)
		walk(s.Siblings[i].Fields, where, false)
		// A facet exists to be absent in bulk, and the verb that empties it
		// assigns nothing — it nils every field of the facet. A composite held by
		// VALUE has no nil, so it would keep the row alive after a clear that
		// reported success.
		for j, f := range s.Siblings[i].Fields {
			if IsComposite(f) && !f.Nullable {
				ps.BlockerFix(fmt.Sprintf("%s[%d] (%s).nullable", where, j, orUnnamed(f.Name)),
					"a facet's composite value object must be optional — the facet is emptied "+
						"by nilling its fields, and a composite held by value has no nil",
					"set nullable: true, or move the value object to the entity's own table")
			}
		}
	}
	// A collection entry is its own schema and its own struct, so both the once
	// rule and the namespace start over inside it.
	for i := range s.Children {
		usedBy, seenLogical, seenColumn = map[string]string{}, map[string]string{}, map[string]string{}
		walk(s.Children[i].Fields, fmt.Sprintf("children[%d].fields", i), true)

		// Business identity is compared field by field on the ENTRY's struct, and
		// a composite is neither: the value object as a whole has no comparison
		// the generator can write (a part may be a pointer, where == compares
		// addresses), and a part is not a field of the entry at all.
		for _, id := range s.Children[i].BusinessIdentity {
			f := findLogicalField(s.Children[i].Fields, s, id)
			owner := compositePartOwner(s.Children[i].Fields, id)
			if f == nil && owner == nil {
				continue // an unknown name is the children validator's message
			}
			if owner == nil {
				continue // an ordinary field
			}
			ps.BlockerFix(fmt.Sprintf("children[%d].businessIdentity", i),
				fmt.Sprintf("%q is a composite value object (or one of its parts), and two "+
					"entries are compared field by field on the entry's own struct", id),
				"name a scalar field — a composite has no equality the generator can write, "+
					"and a part is not a field of the entry")
		}
	}
}

// compositePartOwner reports the composite field a name belongs to, whether the
// name is the composite's own or one of its exposed parts'.
func compositePartOwner(fields []Field, name string) *Field {
	for i := range fields {
		if !IsComposite(fields[i]) {
			continue
		}
		if fields[i].Name == name {
			return &fields[i]
		}
		for _, p := range fields[i].Parts {
			if ExposedName(p) == name {
				return &fields[i]
			}
		}
	}
	return nil
}

// validateCompositeRuleTargets keeps the entity's declarative rules off the
// composites.
//
// The entity's rules are emitted against ENTITY fields, and a composite is not
// one value: `Salary` is a struct the DSL has no comparison for, and
// `SalaryAmount` is a name that exists on the wire and in the table but never on
// the aggregate. Both would generate code that does not compile — and the fix is
// not "drop the rule", it is "declare it where the value object can answer it",
// which is what the message says.
func validateCompositeRuleTargets(s *Spec, ps *Problems) {
	check := func(rs Rules, where string, owned func(string) *Field) {
		for i, r := range rs.List {
			w := fmt.Sprintf("%s.list[%d] (%s)", where, i, orUnnamed(r.ID))
			named := append(append([]string{}, r.Fields...), r.Other, r.AttachTo)
			for _, n := range named {
				if n == "" {
					continue
				}
				f := owned(n)
				if f == nil {
					continue
				}
				ps.BlockerFix(w+".fields",
					fmt.Sprintf("%q is a composite value object (or one of its parts), and "+
						"the entity's rules are checked against the entity's own fields", n),
					fmt.Sprintf("declare the invariant under valueObjects (%s).rules — a "+
						"cross-field rule between parts is exactly what a composite exists "+
						"for. Anything needing the old state, another field of the entity or "+
						"the domain service belongs to rules.manual, where the composite is "+
						"in hand as a whole", f.VO.Ref))
			}
		}
	}

	rootOwned := func(n string) *Field {
		if f := compositeOwnerNamed(s, n); f != nil {
			return f
		}
		for i := range s.Fields {
			if !IsComposite(s.Fields[i]) {
				continue
			}
			for _, p := range s.Fields[i].Parts {
				if ExposedName(p) == n {
					return &s.Fields[i]
				}
			}
		}
		return nil
	}
	check(s.Rules, "rules", rootOwned)

	for i := range s.Children {
		c := s.Children[i]
		owned := func(n string) *Field {
			for j := range c.Fields {
				f := &c.Fields[j]
				if !IsComposite(*f) {
					continue
				}
				if f.Name == n {
					return f
				}
				for _, p := range f.Parts {
					if ExposedName(p) == n {
						return f
					}
				}
			}
			return nil
		}
		check(c.Rules, fmt.Sprintf("children[%d].rules", i), owned)
	}
}

// validateCompositeField checks one field that carries a composite: that the
// value object exists and is a composite, that the keys a single column needs
// are absent, and that every declared part is placed exactly once.
func validateCompositeField(s *Spec, f Field, where string, ps *Problems, isChild bool, existing map[string]bool) {
	if f.VO.Ref == "" {
		ps.BlockerFix(where+".vo.ref",
			"a composite field does not say which value object it holds",
			"set vo.ref to the type, and declare it under valueObjects with kind: composite")
		return
	}

	local := findVO(s.ValueObjects, f.VO.Ref)
	if local == nil && !existing[f.VO.Ref] {
		ps.BlockerFix(where+".vo.ref",
			fmt.Sprintf("this project declares no value object named %q", f.VO.Ref),
			"declare it under valueObjects with kind: composite (parts + rules), or "+
				"correct the name")
		return
	}
	if local != nil && local.Kind != "composite" {
		ps.Blockerf(where+".vo.kind",
			"the field says composite but the declaration under valueObjects says %q — "+
				"one of the two is wrong", local.Kind)
		return
	}

	// The single-column keys have nothing to hold on a value that spans several.
	if f.Column != "" {
		ps.BlockerFix(where+".column",
			"a composite value object's value spans SEVERAL columns, so it has no single one",
			"drop column and give each part its own under parts[].column")
	}
	if f.Type != "" {
		ps.BlockerFix(where+".type",
			"a composite value object has no scalar type — its parts do",
			"drop type; each part declares its own under valueObjects[].parts[].type")
	}
	if f.Length != 0 {
		ps.BlockerFix(where+".length",
			"a length sizes ONE column, and a composite occupies several",
			"declare it per part, under parts[].length")
	}
	if f.Unique != nil {
		ps.BlockerFix(where+".unique",
			"uniqueness over a composite value object is not generated by this build — "+
				"it would need a multi-column constraint, and none is emitted",
			"declare the uniqueness on a single-column field, or model the identifying "+
				"part as its own field")
	}
	if f.Runtime {
		ps.BlockerFix(where+".runtime",
			"a runtime-only field is fed from one token claim, and a composite has several parts",
			"declare the parts as separate runtime fields")
	}
	if f.AssignedFrom != "" {
		ps.BlockerFix(where+".assignedFrom",
			"the server assigns ONE value from the caller's identity, and a composite has several parts",
			"declare the field that records who acted as a plain string field")
	}

	if len(f.Parts) == 0 {
		ps.BlockerFix(where+".parts",
			fmt.Sprintf("the composite value object %s is not placed — it declares no column to live in", f.VO.Ref),
			"list one entry per part: {part: <Name>, column: <column>, as: <ExposedName>}")
		return
	}

	seenPart := map[string]bool{}
	for i, p := range f.Parts {
		pw := fmt.Sprintf("%s.parts[%d] (%s)", where, i, orUnnamed(p.Part))
		if p.Part == "" {
			ps.BlockerFix(pw, "the part does not say which field of the value object it places",
				"set part: <Name> — a name from the declaration's parts list")
			continue
		}
		if seenPart[p.Part] {
			ps.Blockerf(pw, "the part %q is placed twice", p.Part)
		}
		seenPart[p.Part] = true

		if p.As != "" && !goIdentRe.MatchString(p.As) {
			ps.BlockerFix(pw+".as",
				fmt.Sprintf("%q is not a usable Go field name", p.As), "use exported PascalCase")
		}
		if p.Column == "" {
			ps.Blockerf(pw+".column", "the column name is required")
		} else {
			if !columnRe.MatchString(p.Column) {
				ps.BlockerFix(pw+".column",
					fmt.Sprintf("%q is not a usable column name", p.Column),
					"lowercase, digits and underscores, starting with a letter")
			}
			if engine := ReservedWord(p.Column); engine != "" {
				ps.BlockerFix(pw+".column",
					fmt.Sprintf("the column name %q is a reserved word (%s)", p.Column, engine),
					"identifiers are emitted unquoted in places the generator does not control; "+
						"rename the column")
			}
			if strings.HasPrefix(p.Column, "_") {
				ps.BlockerFix(pw+".column",
					fmt.Sprintf("the column %q starts with an underscore", p.Column),
					"the underscore prefix is reserved by the framework")
			}
		}

		decl := FindVOPart(local, p.Part)
		if local != nil && decl == nil {
			ps.BlockerFix(pw+".part",
				fmt.Sprintf("%s declares no part named %q", f.VO.Ref, p.Part),
				"name one of its parts, or add it under valueObjects[].parts")
			continue
		}
		// A REUSED composite has no declaration in this file, so the field has to
		// restate the part's shape — exactly as a field reusing a scalar value
		// object restates its backing under type:.
		if local == nil {
			if !FieldTypes.Has(p.Type) {
				ps.BlockerFix(pw+".type",
					fmt.Sprintf("%q is not a persistable type", p.Type),
					"the value object is reused from elsewhere in the project, so its parts "+
						"are not declared here — restate this one's type; one of: "+FieldTypes.String())
			}
		} else {
			if p.Type != "" && p.Type != decl.Type {
				ps.Blockerf(pw+".type",
					"the field says %q and the declaration under valueObjects says %q — "+
						"one of the two is wrong", p.Type, decl.Type)
			}
			if p.Nullable && !decl.Nullable {
				ps.BlockerFix(pw+".nullable",
					"the column is declared NULL-able while the part is not a pointer inside "+
						"the value object, so a NULL row would be refused on read",
					fmt.Sprintf("set valueObjects[].parts (%s) nullable: true, or drop it here", p.Part))
			}
		}

		typ := p.Type
		if decl != nil {
			typ = decl.Type
		}
		if typ == "string" && p.Length <= 0 {
			ps.BlockerFix(pw+".length",
				"a string part needs a column length",
				"set length: N — a zero-length VARCHAR is rejected by postgres, sqlserver and oracle")
		}
		if typ != "string" && p.Length != 0 {
			ps.BlockerFix(pw+".length",
				"a length sizes text, and this part is not text", "drop length")
		}
		if p.Example == "" {
			ps.WarnFix(pw, "no example value",
				"it is what Swagger's \"try it out\" shows for this part's flat wire field")
		}
	}

	// Every declared part must be placed. The framework maps partially by design
	// — an undeclared part is simply not persisted and reconstructs as its zero —
	// but here the generator wrote the value object itself, so a part it declared
	// and did not store is a mistake, not a choice, and the only symptom would be
	// a field that silently reads back empty.
	if local != nil {
		for _, decl := range local.Parts {
			if !seenPart[decl.Name] {
				ps.BlockerFix(where+".parts",
					fmt.Sprintf("the part %s.%s is declared and never placed, so it would be "+
						"neither persisted nor scanned and would read back as its zero value",
						f.VO.Ref, decl.Name),
					fmt.Sprintf("add {part: %s, column: <column>} — or drop the part from the "+
						"declaration if the domain does not need it", decl.Name))
			}
		}
	}
}

// validateCompositeDeclaration checks one valueObjects[] entry of kind
// composite: its parts, and the rules it owns.
func validateCompositeDeclaration(s *Spec, vo ValueObject, where string, ps *Problems, opt Options) {
	if vo.Backing != "" {
		ps.BlockerFix(where+".backing",
			"a composite value object has no single underlying value — its parts are its value",
			"drop backing; each part declares its own type under parts[].type")
	}
	if vo.Notification != "" || vo.UnknownNotification != "" {
		ps.BlockerFix(where,
			"notification/unknownNotification answer for a value with ONE shape or ONE set, "+
				"and a composite has neither",
			"raise the answers from the composite's own rules, under rules.list[].notification")
	}
	if vo.Regex != "" || vo.MinLength != 0 || vo.MaxLength != 0 || vo.Min != nil || vo.Max != nil {
		ps.BlockerFix(where,
			"format and range keys bound ONE value, and a composite has several",
			"declare them per part under rules.list (kind: length or range, fields: [<Part>])")
	}
	if len(vo.Members) > 0 {
		ps.Blockerf(where+".members", "members belong to an enum value object, not a composite")
	}

	if len(vo.Parts) == 0 {
		ps.BlockerFix(where+".parts",
			"a composite value object is defined by its parts, and this one declares none",
			"list them: {name: Amount, type: int64}, {name: Currency, type: string, "+
				"vo: {kind: enum, ref: Currency}} — a value object with one part is a raw one")
		return
	}
	if len(vo.Parts) == 1 {
		ps.BlockerFix(where+".parts",
			"a composite value object with ONE part occupies one column, which is what a "+
				"raw or enum value object already is",
			"add the part the concept is missing, or declare it with kind: raw / kind: enum")
	}

	seen := map[string]bool{}
	for i, p := range vo.Parts {
		pw := fmt.Sprintf("%s.parts[%d] (%s)", where, i, orUnnamed(p.Name))
		if p.Name == "" || !goIdentRe.MatchString(p.Name) {
			ps.BlockerFix(pw, "the part needs an exported Go field name", "use PascalCase, e.g. Amount")
		} else if seen[p.Name] {
			ps.Blockerf(pw, "the part %q is declared twice", p.Name)
		} else {
			seen[p.Name] = true
		}
		if why, reserved := reservedFieldNames[p.Name]; reserved {
			ps.BlockerFix(pw, fmt.Sprintf("%q is a reserved field name", p.Name), why)
		}
		if !FieldTypes.Has(p.Type) {
			ps.BlockerFix(pw+".type",
				fmt.Sprintf("%q is not a persistable type", p.Type),
				"one of: "+FieldTypes.String())
		}
		if p.VO != nil {
			validatePartVO(s, p, pw, ps, opt)
		}
		if p.Description == "" {
			ps.WarnFix(pw, "no description", "it becomes the column comment in the migration")
		}
	}

	validateCompositeRules(s, vo, where, ps)
}

// validatePartVO checks a part that is itself a value object. Money's Currency
// is the case this exists for: a value object nests inside a composite and still
// persists as its underlying scalar.
func validatePartVO(s *Spec, p VOPart, where string, ps *Problems, opt Options) {
	if !VOKinds.Has(p.VO.Kind) {
		ps.BlockerFix(where+".vo.kind",
			fmt.Sprintf("%q is not a value-object kind", p.VO.Kind), "one of: "+VOKinds.String())
		return
	}
	if p.VO.Kind == "none" || p.VO.Kind == "" {
		return
	}
	if p.VO.Kind == "composite" {
		ps.BlockerFix(where+".vo.kind",
			"a composite value object may not hold another one",
			"a value object nested two deep is an entity in disguise, and the framework "+
				"decomposes exactly one level — flatten the concept, or model the inner "+
				"one as its own entity")
		return
	}
	if p.VO.Ref == "" {
		ps.BlockerFix(where+".vo.ref", "the part's value object needs a name", "set vo.ref")
		return
	}
	if p.VO.Kind == "reuse" {
		for _, name := range opt.ExistingVOs {
			if name == p.VO.Ref {
				return
			}
		}
		known := "none — this project declares no value objects yet"
		if len(opt.ExistingVOs) > 0 {
			known = strings.Join(opt.ExistingVOs, ", ")
		}
		ps.BlockerFix(where+".vo.ref",
			fmt.Sprintf("the project declares no value object named %q", p.VO.Ref),
			"declare it under valueObjects (kind: raw or enum), or correct the name — "+
				"known here: "+known)
		return
	}
	inner := findVO(s.ValueObjects, p.VO.Ref)
	if inner == nil {
		ps.BlockerFix(where+".vo.ref",
			fmt.Sprintf("this spec declares no value object named %q", p.VO.Ref),
			"declare it under valueObjects, or use vo.kind: reuse for one already generated")
		return
	}
	if inner.Kind != p.VO.Kind {
		ps.Blockerf(where+".vo.kind",
			"the part says %q but the declaration under valueObjects says %q — "+
				"one of the two is wrong", p.VO.Kind, inner.Kind)
	}
}

// validateCompositeRules checks the invariants a composite owns. They are
// checked against its PARTS, and against nothing else: a value object sees its
// own value, so a rule reaching for the entity, the old state or a service is
// not a rule this type can answer.
func validateCompositeRules(s *Spec, vo ValueObject, where string, ps *Problems) {
	rw := where + ".rules"
	if len(vo.Rules.Manual) > 0 {
		ps.BlockerFix(rw+".manual",
			"a composite value object has no hook file of its own in this build",
			"declare the hand-written invariant under the ENTITY's rules.manual — it "+
				"already sees the composite as a field, and it is the file the generator "+
				"never rewrites")
	}

	scope := make([]Field, 0, len(vo.Parts))
	for _, p := range vo.Parts {
		scope = append(scope, Field{
			Name: p.Name, Type: p.Type, Nullable: p.Nullable, VO: p.VO,
			LabelKey: PartLabelKey(vo.Name, p),
		})
	}

	seenID := map[string]bool{}
	for i, r := range vo.Rules.List {
		w := fmt.Sprintf("%s.list[%d] (%s)", rw, i, orUnnamed(r.ID))
		if r.ID == "" {
			ps.BlockerFix(w, "the rule needs an id", "kebab-case, e.g. end-after-start")
		} else if seenID[r.ID] {
			ps.Blockerf(w, "the rule id %q is used twice", r.ID)
		} else {
			seenID[r.ID] = true
		}
		if !CompositeRuleKinds.Has(r.Kind) {
			hint := "one of: " + CompositeRuleKinds.String()
			if RuleKinds.Has(r.Kind) {
				hint = fmt.Sprintf("%q is a rule of the ENTITY, not of a value object: it needs "+
					"the old state, a service or the rest of the aggregate, none of which a "+
					"value object can see. Declare it under the entity's rules. Here: %s",
					r.Kind, CompositeRuleKinds.String())
			}
			ps.BlockerFix(w+".kind",
				fmt.Sprintf("%q is not a rule a composite value object can check", r.Kind), hint)
			continue
		}
		if len(r.Scope) > 0 {
			ps.BlockerFix(w+".scope",
				"a value object's rule has no verb to gate on — the framework validates "+
					"every value-object field on every write",
				"drop scope; a rule that must fire on one verb only belongs to the entity's rules")
		}
		if r.Notification == "" {
			ps.BlockerFix(w+".notification",
				"a rule needs the notification it raises when it refuses",
				"declare one under notifications with package: vos — the value object is "+
					"declared there, so the type has to be reachable from it")
		} else {
			validateNotificationRef(s, r.Notification, w+".notification", ps)
			if n := findNotification(s, r.Notification); n != nil && n.Package != "vos" {
				ps.BlockerFix(w+".notification",
					fmt.Sprintf("%s is declared in package %q and is raised from inside a "+
						"value object, which lives in vos", r.Notification, orUnnamed(n.Package)),
					"set notifications[].package: vos — the vos package is a leaf and cannot "+
						"import the domain package back")
			}
		}
		validateRuleFields(r, scope, w, ps)
		validateRuleShape(s, r, scope, w, ps)
	}
}

// findNotification looks a declared answer up by name.
func findNotification(s *Spec, name string) *Notification {
	for i := range s.Notifications {
		if s.Notifications[i].Name == name {
			return &s.Notifications[i]
		}
	}
	return nil
}
