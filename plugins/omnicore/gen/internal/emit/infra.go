package emit

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

func emitInfra(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File
	if m.IsRole() && !m.Base.Reuse {
		f, err := emitBaseSchema(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	for _, fn := range []func(*ir.Model) (fsplan.File, error){
		emitSchema, emitRepository,
	} {
		f, err := fn(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if m.Read.Enabled {
		f, err := emitView(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	// The hand-written redactors, beside the schemas that call them. Written
	// only when a `hook` axis exists: a file of nothing but a header is a file
	// an author opens once, learns nothing from, and stops opening.
	if len(ir.RedactionHooks(m)) > 0 {
		f, err := emitRedactHook(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// emitSchema writes the Go↔column map. One schema per file: a schema is
// reusable across aggregates, and bundling several in one file makes that
// impossible to see.
func emitSchema(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package schemas")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote(fwImport("infra/db/core")))
	s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
	if schemaNeedsVOs(m) {
		// A composite is decomposed by TYPE, so the schema names the value object
		// itself — the one place outside the domain that mentions one.
		s.L("\t%s", quote(m.ImportPath("internal/domain/vos")))
	}
	if schemaNeedsTime(roleColumns(m), facetFields(m, "")) {
		s.L("\t%s", quote("time"))
	}
	s.L(")")
	s.Blank()

	s.Doc(
		fmt.Sprintf("%sSchema maps the aggregate's Go fields to columns of %s.",
			m.Entity.Pascal, m.Table),
		"",
		"In every Field pair the LEFT side is the Go field and the RIGHT side is the "+
			"column. Swapping them still compiles and is silently wrong, so the order is "+
			"never improvised.",
		"",
		"The managed columns are declared by presence: a column that is not named here "+
			"is never mentioned in any SQL the framework writes.")
	s.L("func %sSchema() *core.TableSchema {", m.Entity.Pascal)
	s.L("\treturn core.NewTableSchema[*appdomain.%s](%s).", m.Entity.Pascal, quote(m.Table))
	s.L("\t\tID(%s).", quote("id"))

	// EVERY call after Revision goes through this list, so the LAST one — and
	// only the last — drops the trailing dot. Splitting the chain across ad-hoc
	// s.L calls left a dangling dot (a parse error) the moment the tail of the
	// chain happened to be empty: a role keeping every column on the base, with
	// no timestamps of its own.
	type call struct {
		pre  []string // comment lines above the call
		text string   // the call itself; may span lines
	}
	var chain []call
	chain = append(chain, call{text: fmt.Sprintf("Revision(%s)", quote(m.Managed.Revision))})
	if m.IsRole() {
		chain = append(chain, call{
			pre: []string{
				"// The shared identity. The registry keys it by TABLE, so every role",
				"// declaring the same base behaves as one — but two declarations that",
				"// DISAGREE about it abort the boot.",
			},
			text: fmt.Sprintf("SharedBase(%s(), %s)", m.Base.FuncName, quote(baseLinkColumn(m))),
		})
	}
	for _, sib := range m.SiblingsOn("") {
		var b strings.Builder
		fmt.Fprintf(&b, "Sibling(core.NewSiblingSchema[*appdomain.%s](%s).",
			m.Entity.Pascal, quote(sib.Table))
		calls := schemaFieldCalls(sib.Fields, "\t\t\t")
		for i, c := range calls {
			sep := "."
			if i == len(calls)-1 {
				sep = ")"
			}
			fmt.Fprintf(&b, "\n\t\t\t%s%s", c, sep)
		}
		chain = append(chain, call{
			pre: []string{
				fmt.Sprintf("// The %s facet: its own table, sharing this row's key. Every column is", sib.Name),
				"// nullable and the row only exists when at least one has a value.",
			},
			text: b.String(),
		})
	}
	// Only the collections this role owns. A base-owned one is declared by the
	// shared identity's schema instead — that declaration is what makes it
	// visible to every other role over the same identity.
	for _, c := range m.RoleChildren() {
		chain = append(chain, call{text: fmt.Sprintf("Child(%sSchema())", c.Name)})
	}
	for _, c := range schemaFieldCalls(roleColumns(m), "\t\t") {
		chain = append(chain, call{text: c})
	}
	if m.Managed.ArchivedAt != "" {
		chain = append(chain, call{text: fmt.Sprintf("DeletedAt(%s)", quote(m.Managed.ArchivedAt))})
	}
	if m.Managed.CreatedAt != "" {
		chain = append(chain, call{text: fmt.Sprintf("CreatedAt(%s)", quote(m.Managed.CreatedAt))})
	}
	if m.Managed.UpdatedAt != "" {
		chain = append(chain, call{text: fmt.Sprintf("UpdatedAt(%s)", quote(m.Managed.UpdatedAt))})
	}
	for i, c := range chain {
		for _, p := range c.pre {
			s.L("\t\t%s", p)
		}
		if i == len(chain)-1 {
			s.L("\t\t%s", c.text)
		} else {
			s.L("\t\t%s.", c.text)
		}
	}
	s.L("}")

	return goFile("internal/infra/schemas/"+m.Entity.Snake+"_schema.go", fsplan.Owned,
		fmt.Sprintf("the %s schema (%d columns)", m.Table, len(m.Fields)), s)
}

func emitRepository(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package infra")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote(fwImport("application/persistence")))
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\t%s", quote(fwImport("infra/db/command/read")))
	s.L("\t%s", quote(fwImport("infra/db/command/write")))
	s.L("\t%s", quote(fwImport("infra/db/core")))
	s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
	s.L("\t%s", quote(m.ImportPath("internal/infra/schemas")))
	s.L(")")
	s.Blank()

	entity := "appdomain." + m.Entity.Pascal
	s.Doc(
		fmt.Sprintf("%sRepository persists the aggregate.", m.Entity.Pascal),
		"",
		"The engine parameter is the neutral relational engine, so changing dialect "+
			"never edits this file — it is a configuration change plus the engine's build tag.")
	s.L("type %sRepository struct {", m.Entity.Pascal)
	s.L("\tread.%s[*%s]", repoBase(m), entity)
	s.L("}")
	s.Blank()

	s.L("func New%sRepository(engine core.RelationalEngine) *%sRepository {",
		m.Entity.Pascal, m.Entity.Pascal)
	s.L("\tr := &%sRepository{", m.Entity.Pascal)
	s.L("\t\t%s: read.New%s[*%s](", repoBase(m), repoBase(m), entity)
	s.L("\t\t\tengine,")
	s.L("\t\t\tfunc() *%s { return &%s{} },", entity, entity)
	s.L("\t\t),")
	s.L("\t}")
	s.Blank()

	emitConstraints(s, m)

	s.L("\tr.WithSchema(schemas.%sSchema())", m.Entity.Pascal)
	emitWithJoins(s, m)
	s.L("\treturn r")
	s.L("}")
	s.Blank()
	s.L("var _ persistence.ScopedRepository[*%s] = (*%sRepository)(nil)", entity, m.Entity.Pascal)

	return goFile("internal/infra/"+m.Entity.Snake+"_repository.go", fsplan.Owned,
		fmt.Sprintf("the %s repository and its constraint bindings", m.Entity.Pascal), s)
}

// emitConstraints binds database violations to notifications.
//
// Without a binding, a duplicate key surfaces as a raw 500. The binding turns
// it into the intended 409. The KEY FORM DIFFERS PER DIALECT — the SQL engines
// report a constraint name while SQLite reports the column list — so every
// target dialect's form is bound, and binding one alone would silently miss.
func emitConstraints(s *src, m *ir.Model) {
	s.L("\t// A violation reaches the caller as the notification bound here; without a")
	s.L("\t// binding it would surface as a raw 500. Each dialect reports the violation")
	s.L("\t// differently, so every target dialect's key form is bound.")
	s.L("\tr.Constraints = map[string]write.ConstraintBinding{")
	for _, c := range m.Constraints {
		for _, key := range constraintKeys(c, m.Dialects) {
			s.L("\t\t%s: {Notification: %s, Field: %s},",
				quote(key), qualifyNotification(c.Notification), quote(c.Field))
		}
	}
	s.L("\t}")
	s.Blank()
}

// constraintKeys renders the violation key each target dialect reports.
func constraintKeys(c ir.Constraint, dialects []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(k string) {
		if k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for _, d := range dialects {
		switch c.Kind {
		case "primary-key":
			switch d {
			case "mysql":
				add("PRIMARY")
			case "sqlite":
				add(c.Table + "." + c.Columns[0])
			default:
				add(c.Table + "_pkey")
			}
		case "unique":
			switch d {
			case "sqlite":
				// SQLite names the COLUMN LIST rather than the index, and for a
				// multi-column constraint it lists every column, comma-separated,
				// in the index's own order: "t.a, t.b". The framework's dialect
				// hands that string through verbatim, so the key is built the same
				// way here or the binding silently never matches.
				add(sqliteUniqueKey(c))
			case "mysql":
				add(uniqueName(c))
			default:
				add(uniqueName(c))
			}
		}
	}
	return out
}

// sqliteUniqueKey renders the violation key SQLite reports: every constrained
// column, qualified by the table, in declaration order.
func sqliteUniqueKey(c ir.Constraint) string {
	parts := make([]string, 0, len(c.Columns))
	for _, col := range c.Columns {
		parts = append(parts, c.Table+"."+col)
	}
	return strings.Join(parts, ", ")
}

// uniqueName is the index name, which is also the violation key on every engine
// but SQLite. A multi-column constraint names every column, in order — the
// convention Postgres itself uses for a composite unique.
func uniqueName(c ir.Constraint) string {
	return c.Table + "_" + strings.Join(c.Columns, "_") + "_key"
}

func emitView(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package views")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote("go.mongodb.org/mongo-driver/bson"))
	s.L("\t%s", quote(fwImport("infra/db/query")))
	s.L("\t%s", quote(m.ImportPath("internal/infra/schemas")))
	s.L(")")
	s.Blank()

	if m.Read.Backing == "relational" {
		return emitRelationalView(s, m)
	}
	return emitMongoView(s, m)
}

// emitRelationalView writes the read model served straight from the SoR.
//
// It is a DIFFERENT TYPE from a projected view, not a flag on one, and almost
// everything a projected view carries is absent by construction: no version, no
// schema argument, no collection, no indexes, no archive-drop, no TTL. The
// loader is the only structural input — it carries the schema, so the two
// cannot disagree and there is no boot guard to satisfy.
func emitRelationalView(s *src, m *ir.Model) (fsplan.File, error) {
	doc := []string{
		fmt.Sprintf("%sView is the read side of the aggregate, served straight from the "+
			"relational tables.", m.Entity.Pascal),
		"",
		"There is no projection between the write and this read: a write is visible " +
			"to the very next read, with no CDC hop to wait for.",
		"",
		"The loader carries the schema, so nothing about the shape is restated here — " +
			"and there is no Version, because a read model with no materialisation has " +
			"no stored shape to grow stale against.",
	}
	s.Doc(doc...)

	s.L("func %sView(loader query.AggregateReader) *query.RelationalViewDefinition {", m.Entity.Pascal)
	s.L("\t// The aggregate's OWN loader, shared with the repository — never a second one.")
	s.L("\tv := query.RelationalView(%s, loader)", quote(m.Read.ViewName))
	if m.Read.MaxLimit > 0 {
		s.L("\t// A ceiling on how much one request may pull back.")
		s.L("\tv = v.MaxLimit(%d)", m.Read.MaxLimit)
	}
	s.L("\treturn v")
	s.L("}")

	return goFile("internal/infra/views/"+m.Entity.Snake+"_view.go", fsplan.Owned,
		fmt.Sprintf("the %s view (relational-backed)", m.Read.ViewName), s)
}

// emitMongoView writes the projected read model: a collection, a version and
// the Mongo-only options.
func emitMongoView(s *src, m *ir.Model) (fsplan.File, error) {
	s.Doc(
		fmt.Sprintf("%sView is the read side of the aggregate.", m.Entity.Pascal),
		"",
		"The schema alone materialises the document — the fields it maps are what the "+
			"view serves, with no field list repeated here.",
		"",
		"Version identifies the shape. Bump it whenever the projected shape changes, "+
			"or the framework aborts the boot rather than serving a stale projection.")

	s.L("func %sView() *query.ViewDefinition {", m.Entity.Pascal)
	s.L("\tv := query.View(%s).", quote(m.Read.ViewName))
	s.L("\t\tVersion(%d).", m.Read.Version)
	s.L("\t\tSchema(schemas.%sSchema())", m.Entity.Pascal)
	if m.Read.MaxLimit > 0 {
		s.L("\t// A ceiling on how much one request may pull back.")
		s.L("\tv = v.MaxLimit(%d)", m.Read.MaxLimit)
	}
	if m.Read.DeleteOnArchive {
		s.L("\t// Archived rows leave the projection entirely rather than being hidden")
		s.L("\t// at read time — the collection stays the size of the live data.")
		s.L("\tv = v.DeleteOnArchive()")
	}
	if len(m.Read.Indexes) > 0 {
		s.L("\tv = v.Indexes(")
		for _, idx := range m.Read.Indexes {
			s.L("\t\t%s,", indexExpr(idx))
		}
		s.L("\t)")
	}
	if m.Read.TTLSeconds > 0 {
		s.L("\t// Expiry is expressed as a DURATION: a bare number would be read as")
		s.L("\t// nanoseconds and round down to zero, which deletes every document at once.")
		s.L("\tv = v.Indexes(query.Index(%s).TTL(%d * time.Second))",
			quote(m.Managed.ArchivedAt), m.Read.TTLSeconds)
	}
	s.L("\treturn v")
	s.L("}")

	return goFile("internal/infra/views/"+m.Entity.Snake+"_view.go", fsplan.Owned,
		fmt.Sprintf("the %s view (mongo-backed)", m.Read.ViewName), s)
}

// qualifyNotification renders a notification reference from inside the infra
// package, where the service's own notifications need their package qualifier.
func qualifyNotification(literal string) string {
	if strings.HasPrefix(literal, "domain.") {
		return literal
	}
	return "appdomain." + literal
}

// baseLinkColumn is the column that points at the shared identity.
//
// With a shared primary key the role's id IS the identity's id; with a separate
// key the role keeps its own id and carries a foreign key beside it.
func baseLinkColumn(m *ir.Model) string {
	// A shared-pk role has no column to name: its own primary key IS the
	// identity's, which is the framework's contract rather than a choice. Any
	// other link says its column in the spec.
	if m.Base.Link == "shared-pk" {
		return "id"
	}
	return m.Base.LinkColumn
}

// emitBaseSchema writes the shared identity.
//
// The natural key is what the identity is deduplicated by, and the framework
// derives the primary key from it deterministically — so the same key always
// resolves to the same identity, with no read-back and no race.
func emitBaseSchema(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package schemas")
	s.Blank()
	needsVOs, needsTime := ir.HasComposites(m.BaseFields()), schemaNeedsTime(m.BaseFields())
	if needsVOs || needsTime {
		// The shared identity is TYPE-LESS, but a composite it decomposes is not:
		// the value object is named here and resolved against each role's struct
		// at SharedBase(...) time.
		s.L("import (")
		s.L("\t%s", quote(fwImport("infra/db/core")))
		if needsVOs {
			s.L("\t%s", quote(m.ImportPath("internal/domain/vos")))
		}
		if needsTime {
			s.L("\t%s", quote("time"))
		}
		s.L(")")
	} else {
		s.L("import %s", quote(fwImport("infra/db/core")))
	}
	s.Blank()

	doc := []string{fmt.Sprintf("%s is the identity %s plays a role over.", m.Base.FuncName, m.Entity.Pascal)}
	if m.Base.Description != "" {
		doc = append(doc, "", m.Base.Description)
	}
	doc = append(doc, "",
		fmt.Sprintf("It is deduplicated by %s: the framework derives the identity's key "+
			"from that value, so two roles created with the same one resolve to a single "+
			"identity rather than to two.", m.Base.NaturalKey),
		"",
		"It has no type parameter and no lifecycle of its own — it converges from its "+
			"roles.")
	s.Doc(doc...)

	s.L("func %s() *core.TableSchema {", m.Base.FuncName)
	s.L("\treturn core.NewSharedBaseSchema(%s).", quote(m.Base.Table))
	s.L("\t\tID(%s).", quote("id"))
	s.L("\t\tRevision(%s).", quote(m.Managed.Revision))
	for _, c := range schemaFieldCalls(m.BaseFields(), "\t\t") {
		s.L("\t\t%s.", c)
	}
	s.L("\t\tNaturalID(%s).", quote(naturalColumn(m)))
	tail := []string{}
	if m.Managed.ArchivedAt != "" {
		tail = append(tail, fmt.Sprintf("DeletedAt(%s)", quote(m.Managed.ArchivedAt)))
	}
	if m.Managed.CreatedAt != "" {
		tail = append(tail, fmt.Sprintf("CreatedAt(%s)", quote(m.Managed.CreatedAt)))
	}
	if m.Managed.UpdatedAt != "" {
		tail = append(tail, fmt.Sprintf("UpdatedAt(%s)", quote(m.Managed.UpdatedAt)))
	}
	tail = append(tail, fmt.Sprintf("OrphanPolicy(core.%s)", orphanConst(m.Base.OrphanPolicy)))
	// A base-owned collection hangs here, not on the role: it belongs to the
	// identity, so it survives this role and every other role over the same
	// identity reads the same rows.
	for _, c := range m.BaseChildren() {
		tail = append(tail, fmt.Sprintf("Child(%sSchema())", c.Name))
	}
	for i, call := range tail {
		if i == len(tail)-1 {
			s.L("\t\t%s", call)
		} else {
			s.L("\t\t%s.", call)
		}
	}
	s.L("}")

	return goFile("internal/infra/schemas/"+m.Base.Table+"_base_schema.go",
		fsplan.Owned, fmt.Sprintf("the %s shared identity schema", m.Base.Table), s)
}

func naturalColumn(m *ir.Model) string {
	for _, f := range m.Fields {
		if f.Name == m.Base.NaturalKey {
			return f.Column
		}
	}
	return naming.Snake(m.Base.NaturalKey)
}

// orphanConst names what happens to the identity when its last role goes.
func orphanConst(policy string) string {
	if policy == "delete-when-unreferenced" {
		return "DeleteWhenUnreferenced"
	}
	return "KeepOrphan"
}

// roleColumns are the fields stored on THIS table. In a shared-base model the
// identity's own fields belong to the base schema and must not be mapped twice.
func roleColumns(m *ir.Model) []ir.Field {
	if m.IsRole() {
		return m.RoleFields()
	}
	return m.Fields
}

// repoBase picks the repository the schema demands.
//
// The pairing is enforced both ways at the first request, not at boot: a plain
// repository refuses a shared-base schema and the shared-base one refuses a
// plain schema. So the choice is made from the model, never guessed.
func repoBase(m *ir.Model) string {
	if m.IsRole() {
		return "SharedBaseRoleRepository"
	}
	return "BaseAggregateRepository"
}

// indexExpr renders one index.
//
// A single field and several fields are DIFFERENT constructors — passing many
// names to the single-field one does not compile — so the choice is made here
// rather than left to whoever writes the spec.
func indexExpr(idx ir.Index) string {
	var expr string
	switch {
	case idx.Text:
		expr = fmt.Sprintf("query.TextIndex(%s)", quoteList(idx.Columns))
	case len(idx.Columns) > 1:
		expr = fmt.Sprintf("query.Compound(%s)", quoteList(idx.Columns))
	default:
		expr = fmt.Sprintf("query.Index(%s)", quote(idx.Columns[0]))
	}
	if idx.Name != "" {
		expr += fmt.Sprintf(".Name(%s)", quote(idx.Name))
	}
	if idx.Unique {
		expr += ".Unique()"
	}
	if idx.Sparse {
		expr += ".Sparse()"
	}
	if idx.Order == "desc" {
		expr += ".Desc()"
	}
	if idx.Partial != "" {
		expr += fmt.Sprintf(".Partial(%s)", idx.Partial)
	}
	return expr
}

func quoteList(items []string) string {
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, quote(i))
	}
	return strings.Join(out, ", ")
}

// emitWithJoins declares the aggregate's READ JOINS on the repository.
//
// The repository is where they belong, and the placement is the design rather
// than a convention. A TableSchema declares the form of ONE entity; a foreign
// key to another aggregate is already an ordinary column of that form, and what
// a join adds is permission to TRAVERSE it — access, not storage. Keeping it off
// the schema is also what keeps it invisible to the projection side, which
// shares that schema with the Mongo composer.
//
// Declared here it hangs off the LOADER, so ONE declaration reaches FindOne,
// FindAll, Exists, Aggregate, the request-scoped ScopedReader, FindByID (which
// the write-side auto handlers load through) — and any relational read model
// declared over this loader, which inherits the reach and declares nothing.
//
// It stays read-only STRUCTURALLY: WriteFields walks the TableSchema, so a
// joined column can never enter an INSERT or UPDATE.
func emitWithJoins(s *src, m *ir.Model) {
	if len(m.Joins) == 0 {
		return
	}
	s.Blank()
	s.L("\t// Read joins: read-only traversals across a foreign key into another")
	s.L("\t// aggregate. They fill ordinary fields of the entity on every load and are")
	s.L("\t// absent from the TableSchema, so no write can carry them.")
	s.L("\tr.WithJoins(")
	for _, j := range m.Joins {
		emitOneJoin(s, j)
	}
	s.L("\t)")
}

func emitOneJoin(s *src, j ir.Join) {
	head := fmt.Sprintf("read.%s(schemas.%s())", j.Verb(), j.TargetSchemaFunc)
	if j.Child != "" {
		// Two positional schema arguments would be swappable without a compile
		// error, so the framework puts the CHILD behind the verb and the target
		// behind .To(...).
		head = fmt.Sprintf("read.%sInChild(schemas.%s()).To(schemas.%s())",
			j.Verb(), j.ChildSchemaFunc, j.TargetSchemaFunc)
	}
	for _, line := range wrapComment(joinCallNote(j), 70) {
		s.L("\t\t// %s", line)
	}
	s.L("\t\t%s.", head)
	s.L("\t\t\tOn(%s).", quote(j.FKColumn))
	for i, f := range j.Fields {
		sep := "."
		if i == len(j.Fields)-1 {
			sep = ","
		}
		// Same two arguments, same order, as TableSchema.Field: the Go field on
		// this side, then the column on the joined one.
		s.L("\t\t\tField(%s, %s)%s", quote(f.Name), quote(f.Column), sep)
	}
}

// joinCallNote states what the traversal costs and what a missing counterpart
// does, which is the pair a reader of the constructor needs.
func joinCallNote(j ir.Join) string {
	switch {
	case j.Child != "" && j.Kind == "inner":
		return fmt.Sprintf("%s → %s, on every loaded entry. No counterpart drops the ENTRY, "+
			"not the root — a hole in the collection.", j.Child, j.Target)
	case j.Child != "":
		return fmt.Sprintf("%s → %s, on every loaded entry; nil where there is none.",
			j.Child, j.Target)
	case j.Kind == "inner":
		return fmt.Sprintf("→ %s, always in the FROM. No counterpart drops the aggregate "+
			"from EVERY read, FindByID included — which is why the framework allows it "+
			"only over a non-nullable key.", j.Target)
	default:
		return fmt.Sprintf("→ %s, always in the FROM. No counterpart leaves the fields nil "+
			"— an absence, never the zero value.", j.Target)
	}
}

// wrapComment breaks a note into lines a reader can take in. gofmt does not
// reflow comments, so an emitter that writes one long line writes one long line
// into somebody's editor forever.
func wrapComment(text string, width int) []string {
	var out []string
	line := ""
	for _, w := range strings.Fields(text) {
		switch {
		case line == "":
			line = w
		case len(line)+1+len(w) <= width:
			line += " " + w
		default:
			out = append(out, line)
			line = w
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
