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
	return out, nil
}

func emitAVO(m *ir.Model, c ir.Child) (fsplan.File, error) {
	s := &src{}
	s.header(m, fmt.Sprintf("The %s child of %s.", c.Name, m.Entity.Pascal))
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
	for _, f := range c.Fields {
		s.L("\t%s %s `labelKey:%s`%s", f.Name, f.EntityType, quote(f.LabelKey), fieldComment(f))
	}
	s.L("}")
	s.Blank()

	emitCollectionName(s, c)
	emitBusinessIdentity(s, c)
	emitChildRules(s, c)

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
		conds = append(conds, fmt.Sprintf("c.%s == o.%s", f.Name, f.Name))
	}
	s.L("\treturn %s", strings.Join(conds, " &&\n\t\t"))
	s.L("}")
	s.Blank()
}

func emitChildRules(s *src, c ir.Child) {
	s.Doc(
		fmt.Sprintf("BuildRules validates one %s.", c.Name),
		"",
		"It runs scoped to this entry, so a notification it raises reaches the caller "+
			"addressed to the exact position in the collection rather than to the root.",
	)
	s.L("func (c %s) BuildRules(actionName string, service domain.Service, r *domain.Rules) {", c.Name)
	if len(c.Clauses) == 0 {
		s.L("\t// No rule beyond what the value objects validate on their own.")
		s.L("\t_ = actionName")
		s.L("\t_ = service")
		s.L("\t_ = r")
		s.L("}")
		return
	}
	for _, clause := range c.Clauses {
		s.L("\tr.%s(func() {", clause.Gate)
		for _, rule := range clause.Rules {
			emitRuleOn(s, clause.Gate, rule, "c")
		}
		s.L("\t})")
		s.Blank()
	}
	s.L("\t_ = actionName")
	s.L("\t_ = service")
	s.L("}")
}

func emitChildSchema(m *ir.Model, c ir.Child) (fsplan.File, error) {
	s := &src{}
	s.header(m, fmt.Sprintf("The %s child table schema.", c.Name))
	s.Blank()
	s.L("package schemas")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote(fwImport("infra/db/core")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
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
	for _, f := range c.Fields {
		if f.Facet != "" {
			continue // declared inside its own Sibling block below
		}
		s.L("\t\tField(%s, %s).", quote(f.Name), quote(f.Column))
	}
	// A facet declared on this child is built over the CHILD's type: the
	// framework refuses a sibling whose type is not its owner's, because a facet
	// is a slice of one row of that owner and nothing else.
	for _, sib := range m.SiblingsOn(c.Name) {
		s.L("\t\t// The %s facet of one %s: its own table, sharing that row's key.", sib.Name, c.Name)
		s.L("\t\tSibling(core.NewSiblingSchema[aggregatevos.%s](%s).", c.Name, quote(sib.Table))
		for i, f := range sib.Fields {
			sep := "."
			if i == len(sib.Fields)-1 {
				sep = ")."
			}
			s.L("\t\t\tField(%s, %s)%s", quote(f.Name), quote(f.Column), sep)
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
	s.header(m, fmt.Sprintf("The application input for a %s.", c.Name))
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
	s.L("\treturn aggregatevos.%s{", c.Name)
	for _, f := range c.Fields {
		s.L("\t\t%s: %s,", f.Name, entityValue(f, "i."+f.Name))
	}
	s.L("\t}")
	s.L("}")
	s.Blank()
	s.L("var _ = time.Time{}")

	return goFile("internal/application/dtos/"+naming.Snake(c.Name)+"_input.go", fsplan.Owned,
		fmt.Sprintf("the %s input DTO", c.Name), s)
}

// parentColumn is the foreign key a child points back through, as the spec
// declares it. Nothing here derives it: a column name outlives the decision
// that produced it, and renaming one later is a migration.
func parentColumn(c ir.Child) string { return c.ParentColumn }
