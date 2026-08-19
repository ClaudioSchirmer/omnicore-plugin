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
		for _, rule := range clause.Rules {
			// No model, on purpose: it is what tells the shared emitters they are
			// writing INSIDE aggregatevos, where a notification of this package
			// is spelled bare. The aggregate-wide kinds — the only ones that
			// would need the model for anything else — are refused in a
			// collection's scope, so nothing here is left without it.
			emitRuleOn(s, clause.Gate, rule, "c")
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
	s.L("\t%s", quote(m.ImportPath("internal/application/dtos")))
	s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
	s.L(")")
	s.Blank()

	// ── add
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
	s.L("func (cmd *Add%sCommand) ApplyTo(_ *configuration.AppContext, e *appdomain.%s) error {", op, entity)
	s.L("\te.%s(%s)", c.AddMethod, childInputFold(c, "cmd", "\t"))
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

	// ── change
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
	s.L("func (cmd *Change%sCommand) ApplyTo(_ *configuration.AppContext, e *appdomain.%s) error {", op, entity)
	s.L("\te.%s(cmd.%sID, %s)", c.ChangeMethod, c.Name, childInputFold(c, "cmd", "\t"))
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

	// ── remove
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
	s.L("func (cmd *Remove%sCommand) ApplyTo(_ *configuration.AppContext, e *appdomain.%s) error {", op, entity)
	s.L("\te.%s(cmd.%sID)", c.RemoveMethod, c.Name)
	s.L("\treturn nil")
	s.L("}")
	s.Blank()
	s.L("// Remove%sResult carries only the owner: the entry it names is gone.", op)
	s.L("type Remove%sResult struct {", op)
	s.L("\t%sID domain.ID", entity)
	s.L("}")
	s.Blank()
	s.L("func (cmd *Remove%sCommand) FromEntity(_ *configuration.AppContext, e *appdomain.%s) (Remove%sResult, error) {", op, entity, op)
	s.L("\treturn Remove%sResult{%sID: *e.GetID()}, nil", op, entity)
	s.L("}")
	s.Blank()

	if c.Mounted {
		// The projector belongs to the spec that declares the entry type, and it
		// is the same function for every role over the identity.
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
	s.L("\tfwrequests %s", quote(fwImport("web/requests")))
	s.L("\tfwresponses %s", quote(fwImport("web/responses")))
	s.L("\t%s", quote(m.ImportPath("internal/application/commands")))
	s.L(")")
	s.Blank()

	// ── add
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

	// ── change
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

	// ── remove
	s.Doc(
		fmt.Sprintf("Remove%sRequest names the entry to take out.", op),
		"",
		"There is no body: everything the verb needs is in the path.")
	s.L("type Remove%sRequest struct {", op)
	s.L("\t%s", autoRequestEmbed)
	s.Blank()
	s.L("\t%sID string `path:%s`", c.Name, quote(idParam))
	s.L("}")
	s.Blank()
	emitAutoToCommand(s, "Remove"+op+"Request", "Remove"+op+"Command")
	s.Blank()
	s.Doc(fmt.Sprintf("Remove%sResponse answers with the owner alone.", op))
	s.L("type Remove%sResponse struct {", op)
	s.L("\t%s", autoResponseEmbed)
	s.Blank()
	s.L("\t%sID domain.ID `json:%s`", entity, quote(m.Entity.Camel+"Id"))
	s.L("}")
	s.Blank()
	emitAutoFromResult(s, "Remove"+op+"Response", "commands.Remove"+op+"Result")

	return goFile("internal/web/requests/"+naming.Snake(op)+"_requests.go",
		fsplan.Owned, fmt.Sprintf("the per-entry wire types for %s", c.Table), s)
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
