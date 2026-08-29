package emit

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
)

// A child is an aggregate value object: a row that belongs to the root and has
// no life of its own.
//
// Two things about it are easy to get wrong and are handled here rather than
// left to whoever reads the code. The root declares NO slice for its children —
// the framework keeps them in its own collection, and a slice field would stay
// empty on read and be ignored on write. And a child carries no id field of its
// own: the id comes from the embedded carrier, so a hand-declared one compiles,
// is never persisted, and never round-trips.
func emitChildren(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File
	for _, c := range m.Children {
		// A MOUNTED collection is declared by the role that owns the shared
		// identity: the table, the schema, the entry type, its input DTO and its
		// rules all exist and belong to that spec. What this role is missing is
		// the SURFACE — which is the whole of what it could not have before.
		if !c.Mounted {
			avo, err := emitAVO(m, c)
			if err != nil {
				return nil, err
			}
			schema, err := emitChildSchema(m, c)
			if err != nil {
				return nil, err
			}
			input, err := emitChildInput(m, c)
			if err != nil {
				return nil, err
			}
			out = append(out, avo, schema, input)
		}

		if c.HasHookFile && !c.Mounted {
			hook, err := emitChildRulesHook(m, c)
			if err != nil {
				return nil, err
			}
			out = append(out, hook)
		}

		// Per-entry editing is a different SURFACE, not a different storage: the
		// same table, the same type, plus three verbs that address one entry.
		if !c.PerChild {
			continue
		}
		for _, fn := range []func(*ir.Model, ir.Child) (fsplan.File, error){
			emitPerChildCommands, emitPerChildRequests,
		} {
			f, err := fn(m, c)
			if err != nil {
				return nil, err
			}
			out = append(out, f)
		}
	}
	return out, nil
}

func emitAVO(m *ir.Model, c ir.Child) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package aggregatevos")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/vos")))
	s.L(")")
	s.Blank()

	doc := []string{fmt.Sprintf("%s is one entry of %s's %s collection.",
		c.Name, m.Entity.Pascal, c.Segment)}
	if c.Description != "" {
		doc = append(doc, "", c.Description)
	}
	doc = append(doc, "",
		"It embeds the framework's managed carrier and declares NO id field of its "+
			"own: an exported ID here would compile, never be persisted, and never come "+
			"back on a read.")
	s.Doc(doc...)
	s.L("type %s struct {", c.Name)
	s.L("\tdomain.Managed")
	emitStructFields(s, c.Fields)
	emitJoinStructFields(s, childJoins(m, c), "this entry")
	s.L("}")
	s.Blank()

	emitCollectionName(s, c)
	emitBusinessIdentity(s, c)
	emitChildRules(s, m, c)

	return goFile("internal/domain/aggregatevos/"+naming.Snake(c.Name)+".go", fsplan.Owned,
		fmt.Sprintf("the %s child value object", c.Name), s)
}

// emitCollectionName declares the ONE name of this collection.
//
// The framework requires it and derives nothing: it is the document segment the
// projection nests the collection under, the Go field the read DTO declares for
// it, and — lower-camelled — the path a notification reaches the wire under. A
// rule could not spell it in the domain's own language, so the domain says it.
func emitCollectionName(s *src, c ir.Child) {
	s.Doc(
		fmt.Sprintf("CollectionName is the name of the %s collection.", c.Segment),
		"",
		"It is a constant of the TYPE, resolved once from a zero value, and it is "+
			"the single source for the document segment, the read DTO's field and the "+
			"notification path — so the three can never drift apart.",
	)
	s.L("func (%s) CollectionName() string { return %s }", c.Name, quote(c.Plural))
	s.Blank()
}

// emitBusinessIdentity answers "is this the SAME entry?" from the business
// point of view — never "did anything change?".
//
// The distinction decides what a re-sent collection does: an entry that matches
// keeps its id and is updated in place; one that does not is archived and a new
// row is inserted. Comparing every field would make a cosmetic edit look like a
// different entry and silently churn history.
func emitBusinessIdentity(s *src, c ir.Child) {
	s.Doc(
		"IsSameBusinessIdentity decides whether two entries are the same one.",
		"",
		"The framework matches children through this, never by comparing every field. "+
			"It answers from the BUSINESS view, so editing a detail still leaves the "+
			"entry recognisable and it keeps its id instead of being replaced.",
	)
	s.L("func (c %s) IsSameBusinessIdentity(other domain.AggregateValueObject) bool {", c.Name)

	if len(c.Identity) == 0 {
		s.L("\t// Identity genuinely is the whole record here.")
		s.L("\treturn domain.IsSameByBusinessFields(c, other)")
		s.L("}")
		s.Blank()
		return
	}

	s.L("\to, ok := other.(%s)", c.Name)
	s.L("\tif !ok {")
	s.L("\t\treturn false")
	s.L("\t}")
	var conds []string
	for _, f := range c.Identity {
		if f.Nullable {
			// Pointer identity is never the question here: the incoming entry
			// and the stored one are distinct allocations, so `==` on the
			// pointers made every re-sent entry read as a different one.
			conds = append(conds, pointerEq("c."+f.Name, "o."+f.Name))
		} else {
			conds = append(conds, fmt.Sprintf("c.%s == o.%s", f.Name, f.Name))
		}
	}
	s.L("\treturn %s", strings.Join(conds, " &&\n\t\t"))
	s.L("}")
	s.Blank()
}

func emitChildRules(s *src, m *ir.Model, c ir.Child) {
	s.Doc(
		fmt.Sprintf("BuildRules validates one %s.", c.Name),
		"",
		"It runs scoped to this entry, so a notification it raises reaches the caller "+
			"addressed to the exact position in the collection rather than to the root.",
	)
	s.L("func (c %s) BuildRules(actionName string, service domain.Service, r *domain.Rules) {", c.Name)
	if len(c.Clauses) == 0 && !c.HasHookFile {
		s.L("\t// No rule beyond what the value objects validate on their own.")
		s.L("}")
		return
	}
	for _, clause := range c.Clauses {
		s.L("\tr.%s(func() {", clause.Gate)
		for i, rule := range clause.Rules {
			// No model, on purpose: it is what tells the shared emitters they are
			// writing INSIDE aggregatevos, where a notification of this package
			// is spelled bare. The aggregate-wide kinds — the only ones that
			// would need the model for anything else — are refused in a
			// collection's scope, so nothing here is left without it.
			emitRuleOn(s, clause.Gate, rule, "c")
			// A child carries its own Rules, so the barrier here ends the child's
			// pass: the rest of ITS BuildRules, its own value objects, and every
			// sibling still queued behind it.
			emitGuardBarrier(s, rule, "\t\t", i < len(clause.Rules)-1)
		}
		s.L("\t})")
		s.Blank()
	}
	if c.HasHookFile {
		s.L("\t// Invariants this collection declared that the spec could not express.")
		s.L("\t// They live in %s_rules_manual.go, written once and never touched again.",
			naming.Snake(c.Name))
		s.L("\tc.customRules(actionName, service, r)")
	}
	s.L("}")
}

// emitChildRulesHook is the collection's own escape hatch.
//
// A manual rule declared under children[] used to be parsed and then dropped:
// no hook, no report line, no compile error — the invariant simply did not
// exist, and the only way to notice was to go looking for it. A collection
// states invariants about one entry, so it gets a hook that runs where those
// invariants belong, rather than being told to reach for the root's.
func emitChildRulesHook(m *ir.Model, c ir.Child) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package aggregatevos")
	s.Blank()
	s.L("import %s", quote(fwImport("domain")))
	s.Blank()

	s.Doc(
		fmt.Sprintf("customRules is called at the end of %s's generated BuildRules, with "+
			"the same arguments and scoped to ONE entry, and reports a violation the same "+
			"way: r.AddNotification.", c.Name),
		"",
		manualGateDoc,
		"",
		"A notification raised here is addressed to this entry's position in the "+
			"collection, which is what makes the caller able to tell which one failed.",
	)
	s.L("func (c %s) customRules(actionName string, service domain.Service, r *domain.Rules) {", c.Name)
	writeManualRuleGates(s, c.ManualRules)
	s.L("}")

	f, err := goFile("internal/domain/aggregatevos/"+naming.Snake(c.Name)+"_rules_manual.go",
		fsplan.Hook,
		fmt.Sprintf("the hand-written rules for one %s (%d to implement)",
			c.Name, len(c.ManualRules)), s)
	if err != nil {
		return f, err
	}
	f.Consequence = "Until these are written the collection accepts every entry the " +
		"declared rules allow — the invariants described in the spec are simply not " +
		"enforced, quietly."
	return f, nil
}

func emitChildSchema(m *ir.Model, c ir.Child) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package schemas")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote(fwImport("infra/db/core")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
	if childSchemaNeedsVOs(m, c) {
		// An entry may carry a composite value object too, and a composite is
		// decomposed BY TYPE — so the child's schema names the value object, which
		// lives in vos even though the entry itself lives in aggregatevos.
		s.L("\t%s", quote(m.ImportPath("internal/domain/vos")))
	}
	if schemaNeedsTime(ownColumnsOf(c), facetFields(m, c.Name)) {
		s.L("\t%s", quote("time"))
	}
	s.L(")")
	s.Blank()

	s.Doc(
		fmt.Sprintf("%sSchema maps the %s child to %s.", c.Name, c.Name, c.Table),
		"",
		"ParentID declares the link back to the owner. It is projected automatically "+
			"as the read-only twin of the id, so the column is never mapped as an "+
			"ordinary field — doing that would have the framework overwrite its own value.",
		"",
		"There is no revision column here: the framework forbids one on a child, "+
			"because the aggregate's version belongs to the root.",
	)
	s.L("func %sSchema() *core.TableSchema {", c.Name)
	s.L("\treturn core.NewTableSchema[aggregatevos.%s](%s).", c.Name, quote(c.Table))
	s.L("\t\tID(%s).", quote("id"))
	s.L("\t\tParentID(%s).", quote(parentColumn(c)))
	for _, call := range schemaFieldCalls(ownColumnsOf(c), "\t\t") {
		s.L("\t\t%s.", call)
	}
	// A facet declared on this child is built over the CHILD's type: the
	// framework refuses a sibling whose type is not its owner's, because a facet
	// is a slice of one row of that owner and nothing else.
	for _, sib := range m.SiblingsOn(c.Name) {
		s.L("\t\t// The %s facet of one %s: its own table, sharing that row's key.", sib.Name, c.Name)
		s.L("\t\tSibling(core.NewSiblingSchema[aggregatevos.%s](%s).", c.Name, quote(sib.Table))
		calls := schemaFieldCalls(sib.Fields, "\t\t\t")
		for i, call := range calls {
			sep := "."
			if i == len(calls)-1 {
				sep = ")."
			}
			s.L("\t\t\t%s%s", call, sep)
		}
	}
	tail := childTail(m, c)
	for i, call := range tail {
		if i == len(tail)-1 {
			s.L("\t\t%s", call)
		} else {
			s.L("\t\t%s.", call)
		}
	}
	s.L("}")

	return goFile("internal/infra/schemas/"+naming.Snake(c.Name)+"_schema.go", fsplan.Owned,
		fmt.Sprintf("the %s child schema", c.Table), s)
}

func childTail(m *ir.Model, c ir.Child) []string {
	var tail []string
	if c.ArchivedAt != "" {
		tail = append(tail, fmt.Sprintf("DeletedAt(%s)", quote(c.ArchivedAt)))
	}
	if m.Managed.CreatedAt != "" {
		tail = append(tail, fmt.Sprintf("CreatedAt(%s)", quote(m.Managed.CreatedAt)))
	}
	if m.Managed.UpdatedAt != "" {
		tail = append(tail, fmt.Sprintf("UpdatedAt(%s)", quote(m.Managed.UpdatedAt)))
	}
	if len(tail) == 0 {
		tail = append(tail, "Name()")
	}
	return tail
}

// emitChildInput writes the application-layer input DTO.
//
// It sits between the wire request and the domain type on purpose: it carries
// no wire tags (those belong to the web layer) and no context, so the command
// stays free of both.
func emitChildInput(m *ir.Model, c ir.Child) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package dtos")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("time"))
	// An entry field of type: id is `domain.ID`, so this block needs the
	// framework's domain package as much as the root's DTOs do. It was absent,
	// and the emitted file did not compile — three steps after a green check.
	// Declared unconditionally on purpose: gofile prunes what a spec does not
	// use, which is the only version of this that cannot go stale again.
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/vos")))
	s.L(")")
	s.Blank()

	s.Doc(
		fmt.Sprintf("%s carries one %s from the application layer into the domain.", c.InputType, c.Name),
		"",
		"It has no wire tags and takes no context: the wire names live on the web "+
			"layer's request types, and the identity never reaches this far down.",
	)
	s.L("type %s struct {", c.InputType)
	for _, f := range c.Fields {
		s.L("\t%s %s", f.Name, f.GoType)
	}
	s.L("}")
	s.Blank()

	s.L("func (i %s) To%s() aggregatevos.%s {", c.InputType, c.Name, c.Name)
	plain, groups := ir.PlainAndComposites(c.Fields)
	head, tail := "\treturn aggregatevos."+c.Name+"{", "\t}"
	if len(groups) > 0 {
		head = "\tout := aggregatevos." + c.Name + "{"
	}
	s.L("%s", head)
	for _, f := range plain {
		s.L("\t\t%s: %s,", f.Name, entityValue(f, "i."+f.Name))
	}
	s.L("%s", tail)
	if len(groups) > 0 {
		for _, g := range groups {
			emitCompositeFold(s, g, "\t", "out."+g.Owner(), "i")
		}
		s.L("\treturn out")
	}
	s.L("}")

	return goFile("internal/application/dtos/"+naming.Snake(c.Name)+"_input.go", fsplan.Owned,
		fmt.Sprintf("the %s input DTO", c.Name), s)
}

// parentColumn is the foreign key a child points back through, as the spec
// declares it. Nothing here derives it: a column name outlives the decision
// that produced it, and renaming one later is a migration.
func parentColumn(c ir.Child) string { return c.ParentColumn }

// emitPerChildCommands writes the three commands a per-entry collection needs.
//
// They exist because the root's update replaces the WHOLE collection: a caller
// who wants to add one entry would have to resend every other one, and two
// callers doing that concurrently now collide on the root's revision guard —
// the second one is refused with a 409 and has to reload and reapply. Addressing
// one entry is a different operation: it touches its own row, and it needs its
// own not-found answer.
func emitPerChildCommands(m *ir.Model, c ir.Child) (fsplan.File, error) {
	entity := m.Entity.Pascal
	// op is what THIS spec's command types are called. It differs from the
	// collection's own name only when the collection is mounted from a shared
	// identity: the other role generated AddPhotoCommand into the same package
	// already, and it is bound to ITS aggregate.
	op := c.OpBase
	s := &src{}
	s.Blank()
	s.L("package commands")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote(fwImport("application/configuration")))
	s.L("\t%s", quote(fwImport("application/pipeline")))
	s.L("\t%s", quote(fwImport("domain")))
	// The removal verb projects fwresults.None. Pruned when the collection does
	// not mount it, like every other import this file offers.
	s.L("\tfwresults %s", quote(fwImport("application/results")))
	// An entry field of type: time lands on these commands as a time.Time, so
	// the package is as much a dependency here as it is in the entry's own DTO.
	// Declared unconditionally, like every other import this file offers: gofile
	// prunes what a spec does not use, which is the only version of this that
	// cannot go stale.
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote(m.ImportPath("internal/application/dtos")))
	s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
	s.L(")")
	s.Blank()

	if c.MountsAdd {
		emitAddChildCommand(s, m, c, entity, op)
	}
	if c.ChangesByPut() {
		emitChangeChildCommand(s, m, c, entity, op)
	}
	if c.ChangesByPatch() {
		emitPatchChildCommand(s, m, c, entity, op)
	}
	if c.MountsRemove {
		emitRemoveChildCommand(s, m, c, entity, op)
	}

	if c.Mounted {
		// The projector belongs to the spec that declares the entry type, and it
		// is the same function for every role over the identity.
		return goFile("internal/application/commands/"+naming.Snake(op)+"_commands.go",
			fsplan.Owned, fmt.Sprintf("the per-entry commands for %s", c.Table), s)
	}

	// Only the verbs that ANSWER with the entry call it. A collection that
	// mounts removal alone projects nothing back — the entry it named is gone —
	// so the function would sit there unused, and the reader would have to work
	// out that nothing was missing.
	if !c.MountsAdd && !c.MountsChange {
		return goFile("internal/application/commands/"+naming.Snake(op)+"_commands.go",
			fsplan.Owned, fmt.Sprintf("the per-entry commands for %s", c.Table), s)
	}

	s.Doc(
		fmt.Sprintf("projectOne%s renders one stored entry.", c.Name),
		"",
		"Its plural twin projects the whole collection for the root's own verbs; "+
			"this one exists because a per-entry verb answers with the entry it "+
			"touched, not with everything around it.")
	s.L("func projectOne%s(item aggregatevos.%s) %sResult {", c.Name, c.Name, c.Name)
	plain, groups := ir.PlainAndComposites(c.Fields)
	head, tail := "\treturn "+c.Name+"Result{", "\t}"
	if len(groups) > 0 {
		head = "\tout := " + c.Name + "Result{"
	}
	s.L("%s", head)
	s.L("\t\tID: item.GetID(),")
	for _, f := range plain {
		s.L("\t\t%s: %s,", f.Name, wireValue(f, "item"))
	}
	s.L("%s", tail)
	if len(groups) > 0 {
		for _, g := range groups {
			emitCompositeUnfold(s, g, "\t", "out", "item")
		}
		s.L("\treturn out")
	}
	s.L("}")

	return goFile("internal/application/commands/"+naming.Snake(op)+"_commands.go",
		fsplan.Owned, fmt.Sprintf("the per-entry commands for %s", c.Table), s)
}

// emitAddChildCommand writes the verb that appends ONE entry.
func emitAddChildCommand(s *src, m *ir.Model, c ir.Child, entity, op string) {
	s.Doc(
		fmt.Sprintf("Add%sCommand adds ONE entry to %s's %s.", op, entity, c.Segment),
		"",
		"The path carries the OWNER's id; the body is the entry. The aggregate is "+
			"loaded, the entry appended, and the whole thing saved in one transaction — "+
			"so the collection's invariants are checked against what will actually be "+
			"stored.")
	s.L("type Add%sCommand struct {", op)
	s.L("\tpipeline.CommandWithBodyIDBase")
	emitChildFieldsFlat(s, c)
	s.L("}")
	s.Blank()
	s.L("func (cmd *Add%sCommand) ApplyTo(%s *configuration.AppContext, e *appdomain.%s) error {",
		op, identityParam(m), entity)
	s.L("\te.%s(%s)", c.AddMethod, childInputFold(c, "cmd", "\t"))
	emitIdentityFeed(s, m)
	s.L("\treturn nil")
	s.L("}")
	s.Blank()
	emitPerChildResult(s, m, c, "Add")
	s.L("func (cmd *Add%sCommand) FromEntity(_ *configuration.AppContext, e *appdomain.%s) (Add%sResult, error) {", op, entity, op)
	s.L("\titems := domain.GetCurrentItemsOf[aggregatevos.%s](e.GetAggregateRoot())", c.Name)
	s.L("\tout := Add%sResult{%sID: *e.GetID()}", op, entity)
	s.L("\t// The entry as STORED, which is the last one the aggregate holds — the")
	s.L("\t// domain may have normalised a value the caller sent.")
	s.L("\tif len(items) > 0 {")
	s.L("\t\tout.%s = projectOne%s(items[len(items)-1])", c.Name, c.Name)
	s.L("\t}")
	s.L("\treturn out, nil")
	s.L("}")
	s.Blank()
}

// emitChangeChildCommand writes the verb that replaces ONE entry in place.
//
// It is the verb children[].operations most often leaves out: a collection
// whose every field is its business identity has nothing a change could change
// and still be the same entry.
func emitChangeChildCommand(s *src, m *ir.Model, c ir.Child, entity, op string) {
	s.Doc(
		fmt.Sprintf("Change%sCommand replaces ONE entry, keeping its id.", op),
		"",
		"Full replacement of that entry: every writable field must arrive, because "+
			"an absent one here cannot be told from an explicit null. An id the "+
			"collection does not hold answers not-found rather than doing nothing.")
	s.L("type Change%sCommand struct {", op)
	s.L("\tpipeline.CommandWithBodyIDBase")
	s.L("\t%sID string", c.Name)
	emitChildFieldsFlat(s, c)
	s.L("}")
	s.Blank()
	s.L("func (cmd *Change%sCommand) ApplyTo(%s *configuration.AppContext, e *appdomain.%s) error {",
		op, identityParam(m), entity)
	s.L("\te.%s(cmd.%sID, %s)", c.ChangeMethod, c.Name, childInputFold(c, "cmd", "\t"))
	emitIdentityFeed(s, m)
	s.L("\treturn nil")
	s.L("}")
	s.Blank()
	emitPerChildResult(s, m, c, "Change")
	s.L("func (cmd *Change%sCommand) FromEntity(_ *configuration.AppContext, e *appdomain.%s) (Change%sResult, error) {", op, entity, op)
	s.L("\tout := Change%sResult{%sID: *e.GetID()}", op, entity)
	s.L("\tfor _, item := range domain.GetCurrentItemsOf[aggregatevos.%s](e.GetAggregateRoot()) {", c.Name)
	s.L("\t\tif item.GetID().Value() == cmd.%sID {", c.Name)
	s.L("\t\t\tout.%s = projectOne%s(item)", c.Name, c.Name)
	s.L("\t\t\tbreak")
	s.L("\t\t}")
	s.L("\t}")
	s.L("\treturn out, nil")
	s.L("}")
	s.Blank()
}

// emitPatchChildCommand writes the verb that changes SOME fields of ONE entry.
//
// It is the entry-level twin of the root's patch, and it exists for the reason
// the root's does: a caller who wants to move one value should not have to
// resend the rest of the record to do it. On a collection the cost was higher
// than at the root, because the rest of the record INCLUDES the business
// identity — so a full replacement asked the caller to echo the very fields
// that say which entry this is, and accepted them as new ones if they differed.
//
// The merge is the answer to both halves. What the caller did not send comes
// from the entry AS STORED, read back through the collection's own projector,
// so the fields this verb excludes are not "whatever arrived" but what the
// server already holds — and the identity among them cannot move at all,
// because no wire field reaches it.
func emitPatchChildCommand(s *src, m *ir.Model, c ir.Child, entity, op string) {
	patchable := c.PatchableFields()
	s.Doc(
		fmt.Sprintf("Patch%sCommand changes SOME fields of ONE entry, keeping its id.", op),
		"",
		"Every field is a pointer because a partial change is tri-state: a nil field "+
			"means the caller did not send it, which is different from sending an empty "+
			"value. The consequence is the root patch's own — this verb cannot set a "+
			"value back to null.",
		"",
		"The fields it does not carry are not defaults and not blanks: they are read "+
			"off the stored entry, which is what makes this a change to the entry rather "+
			"than a replacement of it. An id the collection does not hold answers "+
			"not-found, exactly as the full replacement does.")
	s.L("type Patch%sCommand struct {", op)
	s.L("\tpipeline.CommandWithBodyIDBase")
	s.L("\t%sID string", c.Name)
	for _, f := range patchable {
		s.L("\t%s %s", f.Name, commandFieldType(f, true))
	}
	s.L("}")
	s.Blank()
	s.L("func (cmd *Patch%sCommand) ApplyPartiallyTo(%s *configuration.AppContext, e *appdomain.%s) error {",
		op, identityParam(m), entity)
	s.L("\t// The entry AS STORED. A partial change is a change to what the server")
	s.L("\t// already holds, so everything this verb does not carry — the business")
	s.L("\t// identity first of all — comes from here and never from the body.")
	s.L("\tvar entry dtos.%s", c.InputType)
	s.L("\tfor _, current := range domain.GetCurrentItemsOf[aggregatevos.%s](e.GetAggregateRoot()) {", c.Name)
	s.L("\t\tif current.GetID().Value() != cmd.%sID {", c.Name)
	s.L("\t\t\tcontinue")
	s.L("\t\t}")
	s.L("\t\tstored := projectOne%s(current)", c.Name)
	s.L("\t\tentry = dtos.%s{", c.InputType)
	for _, f := range c.Fields {
		s.L("\t\t\t%s: stored.%s,", f.Name, f.Name)
	}
	s.L("\t\t}")
	s.L("\t\tbreak")
	s.L("\t}")
	if len(patchable) > 0 {
		s.Blank()
		s.L("\t// Then what the caller sent, and only that.")
	}
	for _, f := range patchable {
		s.L("\tif cmd.%s != nil {", f.Name)
		if f.Nullable {
			s.L("\t\tentry.%s = cmd.%s", f.Name, f.Name)
		} else {
			s.L("\t\tentry.%s = *cmd.%s", f.Name, f.Name)
		}
		s.L("\t}")
	}
	s.Blank()
	s.L("\t// Addressed by id, always — including when the loop above matched nothing.")
	s.L("\t// The aggregate answers not-found for an entry it does not hold, and it is")
	s.L("\t// the ONE place that answer is decided: the empty replacement never lands,")
	s.L("\t// because there is no entry for it to land on.")
	s.L("\te.%s(cmd.%sID, entry.To%s())", c.ChangeMethod, c.Name, c.Name)
	emitIdentityFeed(s, m)
	s.L("\treturn nil")
	s.L("}")
	s.Blank()
	emitPerChildResult(s, m, c, "Patch")
	s.L("func (cmd *Patch%sCommand) FromEntity(_ *configuration.AppContext, e *appdomain.%s) (Patch%sResult, error) {", op, entity, op)
	s.L("\tout := Patch%sResult{%sID: *e.GetID()}", op, entity)
	s.L("\tfor _, item := range domain.GetCurrentItemsOf[aggregatevos.%s](e.GetAggregateRoot()) {", c.Name)
	s.L("\t\tif item.GetID().Value() == cmd.%sID {", c.Name)
	s.L("\t\t\tout.%s = projectOne%s(item)", c.Name, c.Name)
	s.L("\t\t\tbreak")
	s.L("\t\t}")
	s.L("\t}")
	s.L("\treturn out, nil")
	s.L("}")
	s.Blank()
}

// emitRemoveChildCommand writes the verb that takes ONE entry out. Nothing is
// projected back AT ALL — the endpoint answers 204, like the root's own archive
// and delete — so the command declares fwresults.None and the wire pair has no
// Response half.
//
// The owner id was the one thing an earlier shape put in that body, and it is
// the :id segment the caller itself sent: a 200 carrying it invited callers to
// look for content that was never news. The model is still built here for the
// identity feed, which a REVOKE needs as much as a grant: taking an entry out of
// another tenant's aggregate is the same write, in the other direction.
func emitRemoveChildCommand(s *src, m *ir.Model, c ir.Child, entity, op string) {
	s.Doc(
		fmt.Sprintf("Remove%sCommand takes ONE entry out of the collection.", op),
		"",
		"There is no body: the entry is named by the path. Whether the row is "+
			"archived or deleted follows the child's own declaration, not this verb.")
	s.L("type Remove%sCommand struct {", op)
	s.L("\tpipeline.CommandWithBodyIDBase")
	s.L("\t%sID string", c.Name)
	s.L("}")
	s.Blank()
	s.L("func (cmd *Remove%sCommand) ApplyTo(%s *configuration.AppContext, e *appdomain.%s) error {",
		op, identityParam(m), entity)
	s.L("\te.%s(cmd.%sID)", c.RemoveMethod, c.Name)
	emitIdentityFeed(s, m)
	s.L("\treturn nil")
	s.L("}")
	s.Blank()
	s.Doc(fmt.Sprintf("FromEntity projects nothing: the entry Remove%sCommand named is", op),
		"gone, so the endpoint answers 204 — and the framework's NoBody projection",
		"is paired with a None on this side.")
	s.L("func (cmd *Remove%sCommand) FromEntity(_ *configuration.AppContext, _ *appdomain.%s) (fwresults.None, error) {", op, entity)
	s.L("\treturn fwresults.None{}, nil")
	s.L("}")
	s.Blank()
}

func emitPerChildResult(s *src, m *ir.Model, c ir.Child, verb string) {
	s.Doc(fmt.Sprintf("%s%sResult is the owner plus the entry as stored.", verb, c.OpBase))
	s.L("type %s%sResult struct {", verb, c.OpBase)
	s.L("\t%sID domain.ID", m.Entity.Pascal)
	s.L("\t%s %sResult", c.Name, c.Name)
	s.L("}")
	s.Blank()
}

// emitPerChildRequests writes the wire types for the per-entry endpoints.
//
// They reuse the collection's own Request/Response types rather than declaring
// a second pair: the entry a caller POSTs to the collection and the entry they
// send inside the root's body are the same thing, and two shapes for one
// concept drift apart the first time a field is added.
func emitPerChildRequests(m *ir.Model, c ir.Child) (fsplan.File, error) {
	entity := m.Entity.Pascal
	op := c.OpBase
	idParam := naming.Camel(c.Name) + "Id"

	s := &src{}
	s.Blank()
	s.L("package requests")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote(fwImport("domain")))
	// The partial change declares the entry's fields itself, one pointer each,
	// instead of embedding the collection's own Request — so a time field lands
	// in THIS file rather than in the shared one that already imports the
	// package. Pruned when no such field exists.
	s.L("\t%s", quote("time"))
	s.L("\tfwrequests %s", quote(fwImport("web/requests")))
	s.L("\tfwresponses %s", quote(fwImport("web/responses")))
	// For the GraphQL removal's payload, which projects from the framework's
	// None. Pruned when the collection does not expose that verb there.
	s.L("\tfwresults %s", quote(fwImport("application/results")))
	s.L("\t%s", quote(m.ImportPath("internal/application/commands")))
	s.L(")")
	s.Blank()

	if c.MountsAdd {
		emitAddChildRequest(s, m, c, entity, op)
	}
	if c.ChangesByPut() {
		emitChangeChildRequest(s, m, c, op, idParam)
		if c.OnGQL("change") {
			emitChangeChildGraphQLRequest(s, c, op, idParam)
		}
	}
	if c.ChangesByPatch() {
		emitPatchChildRequest(s, m, c, op, idParam)
		if c.OnGQL("patch") {
			emitPatchChildGraphQLRequest(s, c, op, idParam)
		}
	}
	if c.MountsRemove {
		emitRemoveChildRequest(s, c, op, idParam)
		if c.OnGQL("remove") {
			emitRemoveChildGraphQLPair(s, c, op, idParam)
		}
	}

	return goFile("internal/web/requests/"+naming.Snake(op)+"_requests.go",
		fsplan.Owned, fmt.Sprintf("the per-entry wire types for %s", c.Table), s)
}

// emitAddChildRequest writes the wire pair of the verb that appends one entry.
func emitAddChildRequest(s *src, m *ir.Model, c ir.Child, entity, op string) {
	s.Doc(
		fmt.Sprintf("Add%sRequest adds one entry to an existing %s.", op, entity),
		"",
		"The owner comes from the path; the body is the entry, in the same shape it "+
			"has inside the root's own body.")
	s.L("type Add%sRequest struct {", op)
	s.L("\t%s", autoRequestEmbed)
	s.L("\t%sRequest", c.Name)
	s.L("}")
	s.Blank()
	emitAutoToCommand(s, "Add"+op+"Request", "Add"+op+"Command")
	s.Blank()
	emitPerChildResponse(s, m, c, "Add")
}

// emitChangeChildRequest writes the wire pair of the verb that replaces one
// entry in place.
func emitChangeChildRequest(s *src, m *ir.Model, c ir.Child, op, idParam string) {
	s.Doc(
		fmt.Sprintf("Change%sRequest replaces one entry, keeping its id.", op),
		"",
		fmt.Sprintf("The entry is named by the %s path segment. The body is a FULL "+
			"replacement — a field left out is not \"unchanged\", it is a field the "+
			"caller did not send.", idParam))
	s.L("type Change%sRequest struct {", op)
	s.L("\t%s", autoRequestEmbed)
	s.L("\t%sID string `path:%s`", c.Name, quote(idParam))
	s.L("\t%sRequest", c.Name)
	s.L("}")
	s.Blank()
	emitAutoToCommand(s, "Change"+op+"Request", "Change"+op+"Command")
	s.Blank()
	emitPerChildResponse(s, m, c, "Change")
}

// emitPatchChildRequest writes the wire pair of the verb that changes some
// fields of one entry.
//
// It declares the entry's fields itself instead of embedding the collection's
// `<Child>Request`, and that is the point rather than a duplication: the
// embedded type is the FULL entry, every field mandatory and the business
// identity among them. What this verb accepts is a different set — narrower by
// change.patchExcludes — and every field of it is optional. A caller reading
// the type sees exactly what may move; a field that may not is not there to be
// sent.
func emitPatchChildRequest(s *src, m *ir.Model, c ir.Child, op, idParam string) {
	s.Doc(
		fmt.Sprintf("Patch%sRequest changes some fields of one entry, keeping its id.", op),
		"",
		fmt.Sprintf("The entry is named by the %s path segment. Every field of the body is "+
			"optional: one left out keeps the value the entry already has.", idParam),
		"",
		"A field the collection put off-limits to a partial change is ABSENT here, not "+
			"ignored — the caller sees what this verb can move, and the entry's business "+
			"identity is decided by what is stored, never by what arrives.")
	s.L("type Patch%sRequest struct {", op)
	s.L("\t%s", autoRequestEmbed)
	s.L("\t%sID string `path:%s`", c.Name, quote(idParam))
	emitPatchChildFields(s, c)
	s.L("}")
	s.Blank()
	emitAutoToCommand(s, "Patch"+op+"Request", "Patch"+op+"Command")
	s.Blank()
	emitPerChildResponse(s, m, c, "Patch")
}

// emitPatchChildGraphQLRequest is the partial change with the entry's id in the
// INPUT, for the same decoder reason its full-replacement twin has one: the
// framework skips a `path`-tagged field, so the REST type would reach the
// command with an empty entry id and change nothing.
func emitPatchChildGraphQLRequest(s *src, c ir.Child, op, idParam string) {
	s.Doc(
		fmt.Sprintf("Patch%sGraphQLRequest is Patch%sRequest with the entry's id in the input.", op, op),
		"",
		"GraphQL has no path segment, and the framework's decoder does not fill a "+
			"path-tagged field from an input object. The id travels as a field here, "+
			"under the name the REST route spells in its path, and the command on the "+
			"other side is the same one.")
	s.L("type Patch%sGraphQLRequest struct {", op)
	s.L("\t%s", autoRequestEmbed)
	s.L("\t%sID string `json:%s`", c.Name, quote(idParam))
	emitPatchChildFields(s, c)
	s.L("}")
	s.Blank()
	emitAutoToCommand(s, "Patch"+op+"GraphQLRequest", "Patch"+op+"Command")
	s.Blank()
}

// emitPatchChildFields declares what a partial change may carry: the patchable
// fields, each one optional, each one omitempty — the same tri-state shape the
// root's patch request has, over the entry's own fields.
func emitPatchChildFields(s *src, c ir.Child) {
	for _, f := range c.PatchableFields() {
		s.L("\t%s %s `json:%s example:%s`", f.Name,
			commandFieldType(f, true), quote(jsonTag(f, true)), quote(f.Example))
	}
}

// emitRemoveChildRequest writes the REQUEST of the verb that takes one entry
// out — and only the request.
//
// There is no Response type because there is no response: the verb answers 204,
// so the route is mounted with the framework's NoBody projection over
// fwresults.None. The half that existed before carried the owner id, which is
// the :id segment the caller put in the path — a body that could tell a caller
// nothing it did not already know, on a 200 the root's own archive and delete
// never answer.
func emitRemoveChildRequest(s *src, c ir.Child, op, idParam string) {
	s.Doc(
		fmt.Sprintf("Remove%sRequest names the entry to take out.", op),
		"",
		"There is no body: everything the verb needs is in the path. Nothing comes "+
			"back either — the endpoint answers 204.")
	s.L("type Remove%sRequest struct {", op)
	s.L("\t%s", autoRequestEmbed)
	s.Blank()
	s.L("\t%sID string `path:%s`", c.Name, quote(idParam))
	s.L("}")
	s.Blank()
	emitAutoToCommand(s, "Remove"+op+"Request", "Remove"+op+"Command")
}

// emitChangeChildGraphQLRequest writes the same request with the entry's id in
// the BODY.
//
// It exists because of one framework fact, not a taste: the GraphQL input
// decoder skips a `path`-tagged field — deliberately, since there is no path to
// read it from — so the REST Request would reach the command with an empty entry
// id and replace nothing. Everything else is shared: the same embedded entry
// shape, the same command, the same Response.
//
// The id keeps the name the REST path segment gives it, so one entry id is one
// word across both surfaces.
func emitChangeChildGraphQLRequest(s *src, c ir.Child, op, idParam string) {
	s.Doc(
		fmt.Sprintf("Change%sGraphQLRequest is Change%sRequest with the entry's id in the input.", op, op),
		"",
		"GraphQL has no path segment, and the framework's decoder does not fill a "+
			"path-tagged field from an input object — it skips it. So the id travels "+
			"as a field here, under the same name the REST route spells in its path, "+
			"and the command on the other side is the same one.")
	s.L("type Change%sGraphQLRequest struct {", op)
	s.L("\t%s", autoRequestEmbed)
	s.L("\t%sID string `json:%s`", c.Name, quote(idParam))
	s.L("\t%sRequest", c.Name)
	s.L("}")
	s.Blank()
	emitAutoToCommand(s, "Change"+op+"GraphQLRequest", "Change"+op+"Command")
	s.Blank()
}

// emitRemoveChildGraphQLPair writes the removal's GraphQL wire pair.
//
// Both halves exist for the same reason and neither has a REST twin. The
// REQUEST carries the entry id as an input field, for the decoder reason the
// change verb has. The RESPONSE exists because a GraphQL field must resolve to
// SOMETHING: REST answers 204 with no body at all, and a payload type with no
// field is not a schema a server can publish.
//
// So the payload is the acknowledgement and nothing else. It does not carry the
// owner id — that is the argument the caller itself sent — and it does not carry
// the entry, which is the whole point of the verb. It is written by hand rather
// than through the generic mapper for the plainest possible reason: there is no
// Result field behind it to map from, only the framework's None.
func emitRemoveChildGraphQLPair(s *src, c ir.Child, op, idParam string) {
	s.Doc(
		fmt.Sprintf("Remove%sGraphQLRequest names the entry to take out.", op),
		"",
		"The entry id is an input field rather than a path segment: GraphQL has no "+
			"path, and the framework's decoder skips a path-tagged field instead of "+
			"inventing a value for it.")
	s.L("type Remove%sGraphQLRequest struct {", op)
	s.L("\t%s", autoRequestEmbed)
	s.Blank()
	s.L("\t%sID string `json:%s`", c.Name, quote(idParam))
	s.L("}")
	s.Blank()
	emitAutoToCommand(s, "Remove"+op+"GraphQLRequest", "Remove"+op+"Command")
	s.Blank()

	s.Doc(
		fmt.Sprintf("Remove%sGraphQLResponse acknowledges the removal, and says nothing else.", op),
		"",
		"The REST verb answers 204 with no body. A GraphQL field cannot: it must "+
			"resolve to a type, and a payload with no fields is not publishable. So "+
			"the payload is a single true — the owner id is what the caller passed in, "+
			"and the entry is gone by definition.",
		"",
		"No generic mapper here, and it is not an oversight: the command projects the "+
			"framework's None, so there is no Result field for a mapped one to read.")
	s.L("type Remove%sGraphQLResponse struct {", op)
	s.L("\tSuccess bool `json:\"success\"`")
	s.L("}")
	s.Blank()
	s.Doc(fmt.Sprintf("FromResult answers true: the pipeline only projects a result it succeeded with."))
	s.L("func (Remove%sGraphQLResponse) FromResult(fwresults.None) Remove%sGraphQLResponse {", op, op)
	s.L("\treturn Remove%sGraphQLResponse{Success: true}", op)
	s.L("}")
	s.Blank()
}

func emitPerChildResponse(s *src, m *ir.Model, c ir.Child, verb string) {
	entity := m.Entity.Pascal
	s.Doc(fmt.Sprintf("%s%sResponse is the owner's id plus the entry as stored.", verb, c.OpBase))
	s.L("type %s%sResponse struct {", verb, c.OpBase)
	s.L("\t%s", autoResponseEmbed)
	s.Blank()
	s.L("\t%sID domain.ID `json:%s`", entity, quote(m.Entity.Camel+"Id"))
	s.L("\t%s %sResponse `json:%s`", c.Name, c.Name, quote(naming.Camel(c.Name)))
	s.L("}")
	s.Blank()
	emitAutoFromResult(s, verb+c.OpBase+"Response", "commands."+verb+c.OpBase+"Result")
	s.Blank()
}

// emitChildFieldsFlat declares the entry's writable fields directly on a
// per-entry command.
//
// FLAT, not one nested input field, and the reason is the command's own
// responsibility: this verb handles exactly ONE entry, so the entry IS the
// body. (The root's insert carries `[]dtos.<C>Input` for the opposite reason —
// it handles MANY.) Flat is also what lets the Request build this command
// through the framework's generic mapper: the wire body is flat too, so the
// two shapes align field for field and no hand-written seat is left over.
func emitChildFieldsFlat(s *src, c ir.Child) {
	for _, f := range c.Fields {
		s.L("\t%s %s", f.Name, f.GoType)
	}
}

// childInputLiteral renders the entry on its way into the domain.
//
// It goes through dtos.<C>Input rather than building the aggregate value
// object here, and that is the whole point of the indirection: the input type
// is where a value object is REASSEMBLED — an enum cast, a composite folded
// from its parts — and that reassembly must exist in exactly one place. What
// this literal does is a flat copy of scalars, which is the part that can be
// repeated without anything drifting.
func childInputFold(c ir.Child, recv, indent string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "dtos.%s{\n", c.InputType)
	for _, f := range c.Fields {
		fmt.Fprintf(&b, "%s\t%s: %s.%s,\n", indent, f.Name, recv, f.Name)
	}
	fmt.Fprintf(&b, "%s}.To%s()", indent, c.Name)
	return b.String()
}

// childJoins are the read joins that hang off THIS collection.
//
// A child join rides the collection's own batched SELECT, so its fields are
// filled on every loaded entry — and an InnerJoinInChild eliminates the ENTRY,
// not the root: the aggregate still comes back, with that element missing from
// its collection. That is a silent hole in the array rather than a missing
// aggregate, which is why the left form is the one to reach for whenever the
// relationship is genuinely optional.
func childJoins(m *ir.Model, c ir.Child) []ir.Join {
	var out []ir.Join
	for _, j := range m.Joins {
		if j.Child == c.Name {
			out = append(out, j)
		}
	}
	return out
}
