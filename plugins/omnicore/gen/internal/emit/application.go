package emit

import (
	"fmt"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

func emitApplication(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File

	for _, op := range m.WriteOps() {
		f, err := emitCommand(m, op)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if len(m.Children) > 0 {
		f, err := emitChildResults(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if m.Read.Enabled {
		fs, err := emitQueries(m)
		if err != nil {
			return nil, err
		}
		out = append(out, fs...)
	}
	return out, nil
}

func commandImports(s *src, m *ir.Model, needsDomain bool) {
	s.L("import (")
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote(fwImport("application/configuration")))
	s.L("\t%s", quote(fwImport("application/pipeline")))
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\tfwresults %s", quote(fwImport("application/results")))
	if needsDomain {
		s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
		s.L("\t%s", quote(m.ImportPath("internal/domain/vos")))
		s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
		s.L("\t%s", quote(m.ImportPath("internal/application/dtos")))
	}
	s.L(")")
	s.Blank()
}

func emitCommand(m *ir.Model, op ir.Operation) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package commands")
	s.Blank()
	commandImports(s, m, true)

	entity := "appdomain." + m.Entity.Pascal

	if op.Bodyless {
		emitBodylessCommand(s, m, op, entity)
	} else {
		emitBodyCommand(s, m, op, entity)
	}

	return goFile("internal/application/commands/"+fileNameFor(op, m)+".go", fsplan.Owned,
		fmt.Sprintf("the %s command and result", op.Verb), s)
}

func fileNameFor(op ir.Operation, m *ir.Model) string {
	return op.Verb + "_" + m.Entity.Snake + "_command"
}

// emitBodylessCommand covers delete, archive and unarchive: the id comes from
// the path, there is no body, and the response carries no content.
func emitBodylessCommand(s *src, m *ir.Model, op ir.Operation, entity string) {
	s.Doc(fmt.Sprintf(
		"%s carries only the id, taken from the route. The verb has no body and "+
			"answers 204: there is nothing to send back.", op.CommandType))
	s.L("type %s struct{ %s }", op.CommandType, op.CommandBase)
	s.Blank()
	s.Doc("ApplyTo is the hook where identity-derived state would reach the entity. " +
		"This verb needs none, so it is a no-op.")
	s.L("func (c *%s) ApplyTo(_ *configuration.AppContext, _ *%s) error {", op.CommandType, entity)
	s.L("\treturn nil")
	s.L("}")
	s.Blank()
	s.Doc("FromEntity returns the framework's empty result: a bodyless verb has nothing to project.")
	s.L("func (c *%s) FromEntity(_ *configuration.AppContext, _ *%s) (fwresults.None, error) {",
		op.CommandType, entity)
	s.L("\treturn fwresults.None{}, nil")
	s.L("}")
}

func emitBodyCommand(s *src, m *ir.Model, op ir.Operation, entity string) {
	partial := op.InputMethod == "ApplyPartiallyTo"

	s.Doc(fmt.Sprintf("%s carries the writable fields of the request.", op.CommandType))
	if partial {
		s.Doc("",
			"Every field is a pointer because a partial update is tri-state: a nil field "+
				"means the caller did not send it, which is different from sending an empty "+
				"value.")
	}
	s.L("type %s struct {", op.CommandType)
	s.L("\t%s", op.CommandBase)
	for _, f := range commandFields(m, partial) {
		s.L("\t%s %s", f.Name, commandFieldType(f, partial))
	}
	if op.InputMethod == "ToEntity" || op.InputMethod == "ApplyTo" {
		for _, c := range m.Children {
			s.L("\t%s []dtos.%s", c.GoPlural, c.InputType)
		}
	}
	s.L("}")
	s.Blank()

	switch op.InputMethod {
	case "ToEntity":
		emitToEntity(s, m, op, entity)
	case "ApplyTo":
		emitApplyTo(s, m, op, entity)
	case "ApplyPartiallyTo":
		emitApplyPartiallyTo(s, m, op, entity)
	}

	emitResult(s, m, op, entity)
}

// writableFields are the ones a client may set. The managed columns and the
// runtime-only authz fields never appear in a command.
func writableFields(m *ir.Model) []ir.Field {
	// A sibling facet is not a separate input: its fields are more fields of the
	// owner, and the row is materialised only when at least one carries a value.
	return m.WritableFields()
}

// commandFields are the fields a given verb accepts.
//
// A partial update drops what the spec put off-limits, so the excluded field is
// ABSENT from the type rather than merely ignored: a reader of the DTO sees the
// truth, and a caller who sends it gets told, instead of having it quietly
// dropped or quietly applied.
func commandFields(m *ir.Model, partial bool) []ir.Field {
	if partial {
		return m.PatchableFields()
	}
	return m.WritableFields()
}

func commandFieldType(f ir.Field, partial bool) string {
	if partial && !f.Nullable {
		return "*" + f.BaseGoType
	}
	return f.GoType
}

func emitToEntity(s *src, m *ir.Model, op ir.Operation, entity string) {
	s.Doc("ToEntity builds the aggregate the framework will validate and persist.")
	s.L("func (c *%s) ToEntity(ctx *configuration.AppContext) (*%s, error) {", op.CommandType, entity)
	s.L("\te := &%s{}", entity)
	for _, f := range writableFields(m) {
		s.L("\te.%s = %s", f.Name, entityValue(f, "c."+f.Name))
	}
	emitChildAdds(s, m)
	emitAssignedFields(s, m)
	emitIdentityFeed(s, m)
	s.L("\treturn e, nil")
	s.L("}")
	s.Blank()
}

func emitApplyTo(s *src, m *ir.Model, op ir.Operation, entity string) {
	if op.Verb == "insert" {
		s.Doc(
			"ApplyTo writes the request onto the identity.",
			"",
			"The handler may call this TWICE — once to read the natural key, then again "+
				"on the identity it loaded — so it must stay pure and repeatable. Anything "+
				"with a side effect here would happen twice.",
		)
	} else {
		s.Doc("ApplyTo replaces the writable state of the loaded aggregate. " +
			"Every field is assigned unconditionally — that is what makes this a full replacement.")
	}
	s.L("func (c *%s) ApplyTo(ctx *configuration.AppContext, e *%s) error {", op.CommandType, entity)
	for _, f := range writableFields(m) {
		s.L("\te.%s = %s", f.Name, entityValue(f, "c."+f.Name))
	}
	emitChildAdds(s, m)
	// An insert through the identity path assigns too; an update must not —
	// re-reading the caller would hand the row to whoever edited it last.
	if op.Verb == "insert" {
		emitAssignedFields(s, m)
	}
	emitIdentityFeed(s, m)
	s.L("\treturn nil")
	s.L("}")
	s.Blank()
}

func emitApplyPartiallyTo(s *src, m *ir.Model, op ir.Operation, entity string) {
	s.Doc(
		"ApplyPartiallyTo assigns only what the caller sent.",
		"",
		"Each field is guarded on nil, which is what makes a partial update partial. "+
			"Note the consequence: this verb can never set a value back to null, because "+
			"an absent field and an explicit null are indistinguishable here.")
	s.L("func (c *%s) ApplyPartiallyTo(ctx *configuration.AppContext, e *%s) error {", op.CommandType, entity)
	for _, f := range m.PatchableFields() {
		s.L("\tif c.%s != nil {", f.Name)
		if f.Nullable {
			s.L("\t\te.%s = %s", f.Name, entityValue(f, "c."+f.Name))
		} else {
			s.L("\t\te.%s = %s", f.Name, entityValue(f, "*c."+f.Name))
		}
		s.L("\t}")
	}
	emitIdentityFeed(s, m)
	s.L("\treturn nil")
	s.L("}")
	s.Blank()
}

// emitAssignedFields writes the persisted fields the SERVER owns.
//
// It runs on insert only. The value is the caller's, not the client's: the
// field is absent from the request, so there is nothing to ignore, and a later
// update leaves it alone because it is absent from that mapper too — which is
// what makes "who created this row" a fact rather than a claim.
func emitAssignedFields(s *src, m *ir.Model) {
	assigned := m.AssignedFields()
	if len(assigned) == 0 {
		return
	}
	s.Blank()
	s.L("\t// Filled from the caller's identity, never from the request: these fields")
	s.L("\t// are not part of any write DTO. Only an insert sets them.")
	s.L("\tif id := ctx.Identity(); id != nil {")
	for _, f := range assigned {
		if f.AssignedFrom == "identity-subject" {
			s.L("\t\te.%s = %s", f.Name, entityValue(f, "id.Subject"))
			continue
		}
		s.L("\t\tif raw, ok := id.Claims[%s].(string); ok {", quote(f.Claim))
		s.L("\t\t\te.%s = %s", f.Name, entityValue(f, "raw"))
		s.L("\t\t}")
	}
	s.L("\t}")
}

// emitIdentityFeed populates the runtime-only fields the rules read.
//
// This is the one place below the web layer that touches the request identity:
// the command feeds it onto the entity, and BuildRules enforces with it.
func emitIdentityFeed(s *src, m *ir.Model) {
	if len(m.Runtime) == 0 {
		if len(m.AssignedFields()) == 0 {
			s.L("\t_ = ctx")
		}
		return
	}
	s.Blank()
	s.L("\t// Identity-derived state the rules read. It is never persisted.")
	s.L("\t//")
	s.L("\t// The framework does not opine on which custom claims a token carries, so")
	s.L("\t// the claim name comes from the spec rather than from a convention.")
	s.L("\tif id := ctx.Identity(); id != nil {")
	for _, f := range m.Runtime {
		if f.BaseGoType == "bool" {
			// A JSON token carries a yes/no as a real boolean, but plenty of
			// issuers stringify it. Both are accepted and anything else leaves
			// the field false, which is the safe answer for a privilege.
			s.L("\t\tswitch raw := id.Claims[%s].(type) {", quote(f.Claim))
			s.L("\t\tcase bool:")
			s.L("\t\t\te.%s = raw", f.Name)
			s.L("\t\tcase string:")
			s.L("\t\t\te.%s = raw == \"true\"", f.Name)
			s.L("\t\t}")
			continue
		}
		s.L("\t\tif raw, ok := id.Claims[%s].(string); ok {", quote(f.Claim))
		s.L("\t\t\te.%s = raw", f.Name)
		s.L("\t\t}")
	}
	s.L("\t}")
}

func emitResult(s *src, m *ir.Model, op ir.Operation, entity string) {
	s.Doc(fmt.Sprintf("%s is the write response, projected from the entity.", op.ResultType))
	s.L("type %s struct {", op.ResultType)
	s.L("\tID domain.ID")
	for _, f := range m.AllOwnerFields() {
		s.L("\t%s %s", f.Name, f.GoType)
	}
	for _, c := range m.Children {
		s.L("\t%s []%sResult", c.GoPlural, c.Name)
	}
	s.L("}")

	s.Blank()

	s.Doc(
		"FromEntity projects the aggregate AFTER it was validated and written.",
		"",
		"It reads the entity, never the command: the domain may have normalised or "+
			"defaulted a value, and echoing the input back would hide that from the caller.")
	s.L("func (c *%s) FromEntity(_ *configuration.AppContext, e *%s) (%s, error) {",
		op.CommandType, entity, op.ResultType)
	s.L("\treturn %s{", op.ResultType)
	s.L("\t\tID: *e.GetID(),")
	for _, f := range m.AllOwnerFields() {
		s.L("\t\t%s: %s,", f.Name, wireValue(f, "e"))
	}
	for _, c := range m.Children {
		s.L("\t\t%s: %s,", c.GoPlural, c.Projector+"(e)")
	}
	s.L("\t}, nil")
	s.L("}")
	s.Blank()
	s.L("var _ = time.Time{}")
}

// emitChildAdds routes each collection through the aggregate's own method
// rather than touching the framework primitive here: the method is where a
// duplicate is judged, and bypassing it would skip that judgement.
func emitChildAdds(s *src, m *ir.Model) {
	for _, c := range m.Children {
		s.L("\tfor _, item := range c.%s {", c.GoPlural)
		s.L("\t\te.%s(item.To%s())", c.AddMethod, c.Name)
		s.L("\t}")
	}
}

// emitChildProjectors reads the children back OUT of the framework's collection.
//
// They are read through the framework rather than from a struct field because
// that collection is the only place they exist — and it is also where the
// persister has just written the minted ids back.
func emitChildProjectors(s *src, m *ir.Model, entity string) {
	for _, c := range m.Children {
		s.L("func %s(e *%s) []%sResult {", c.Projector, entity, c.Name)
		s.L("\titems := domain.GetCurrentItemsOf[aggregatevos.%s](&e.AggregateRoot)", c.Name)
		s.L("\tout := make([]%sResult, 0, len(items))", c.Name)
		s.L("\tfor _, item := range items {")
		s.L("\t\tout = append(out, %sResult{", c.Name)
		s.L("\t\t\tID: item.GetID(),")
		for _, f := range c.Fields {
			s.L("\t\t\t%s: %s,", f.Name, wireValue(f, "item"))
		}
		s.L("\t\t})")
		s.L("\t}")
		s.L("\treturn out")
		s.L("}")
		s.Blank()
	}
}

func emitQueries(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File

	if m.Read.ByID {
		s := &src{}
		s.Blank()
		s.L("package queries")
		s.Blank()
		s.L("import (")
		s.L("\t%s", quote(fwImport("application/configuration")))
		s.L("\tfwqueries %s", quote(fwImport("application/queries")))
		s.L(")")
		s.Blank()
		s.L("type %s struct {", m.Read.QueryByID)
		s.L("\tfwqueries.QueryByIDBase")
		if m.Managed.Archiving {
			s.L("\tIncludeArchived bool")
		}
		s.L("}")
		s.Blank()
		s.Doc("ToCriteria is where identity-derived read restrictions are injected.")
		s.L("func (q %s) ToCriteria(ctx *configuration.AppContext) (fwqueries.ReadCriteria, error) {",
			m.Read.QueryByID)
		if m.Managed.Archiving {
			s.L("\tcrit := fwqueries.ReadCriteria{IncludeArchived: q.IncludeArchived}")
		} else {
			s.L("\tcrit := fwqueries.ReadCriteria{}")
		}
		emitFieldRestrictions(s, m, "crit")
		s.L("\treturn crit, nil")
		s.L("}")
		s.Blank()
		s.Doc("ContextName labels this aggregate in the error envelope.")
		s.L("func (q %s) ContextName() string { return %s }", m.Read.QueryByID, quote(m.Entity.Pascal))

		f, err := goFile("internal/application/queries/find_"+m.Entity.Snake+"_by_id_query.go",
			fsplan.Owned, "the by-id query", s)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}

	if m.Read.ByParams {
		s := &src{}
		s.Blank()
		s.L("package queries")
		s.Blank()
		s.L("import (")
		s.L("\t%s", quote(fwImport("application/configuration")))
		s.L("\tfwqueries %s", quote(fwImport("application/queries")))
		s.L(")")
		s.Blank()
		s.Doc("The criteria arrive already parsed from the query string by the framework; " +
			"this type exists so identity-derived restrictions have somewhere to be added.")
		s.L("type %s struct {", m.Read.QueryList)
		s.L("\tfwqueries.QueryWithParamsBase")
		s.L("\tCriteria fwqueries.ReadCriteria")
		s.L("}")
		s.Blank()
		s.L("func (q %s) ToCriteria(ctx *configuration.AppContext) (fwqueries.ReadCriteria, error) {",
			m.Read.QueryList)
		emitFieldRestrictions(s, m, "q.Criteria")
		s.L("\treturn q.Criteria, nil")
		s.L("}")
		s.Blank()
		s.L("func (q %s) ContextName() string { return %s }", m.Read.QueryList, quote(m.Entity.Pascal))

		f, err := goFile("internal/application/queries/find_"+m.Entity.PluralSnake+"_by_params_query.go",
			fsplan.Owned, "the listing query", s)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// emitChildResults holds the collection shapes and their projectors in ONE
// place, because every verb that returns the aggregate needs the same ones.
func emitChildResults(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package commands")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
	s.L(")")
	s.Blank()

	entity := "appdomain." + m.Entity.Pascal
	for _, c := range m.Children {
		if c.Mounted {
			continue // declared, with this shape, by the role that owns the identity
		}
		s.Doc(fmt.Sprintf("%sResult mirrors one persisted %s.", c.Name, c.Name),
			"",
			"The id is included because the persister writes the minted ids back before "+
				"this projection runs, and the caller needs them to address the entry later.")
		s.L("type %sResult struct {", c.Name)
		s.L("\tID domain.ID")
		for _, f := range c.Fields {
			s.L("\t%s %s", f.Name, f.GoType)
		}
		s.L("}")
		s.Blank()
	}
	emitChildProjectors(s, m, entity)
	s.L("var _ = time.Time{}")

	return goFile("internal/application/commands/"+m.Entity.Snake+"_child_results.go",
		fsplan.Owned, fmt.Sprintf("the shapes for %d child collection(s)", len(m.Children)), s)
}

// emitFieldRestrictions hides fields the caller may not see.
//
// The field is OMITTED rather than the request refused: a caller without the
// permission gets the rest of the record instead of a 403, which is what makes
// this usable on a listing. The pruning reaches every surface at once —
// ?fields=, the GraphQL selection and the exports all read the same criteria.
func emitFieldRestrictions(s *src, m *ir.Model, target string) {
	emitRowScoping(s, m, target)
	if len(m.Read.FieldRestrict) == 0 {
		if m.Authz.DataAccess == "anyone-with-permission" {
			s.L("\t_ = ctx")
		}
		return
	}
	s.L("\t// A caller without the permission does not receive these fields.")
	s.L("\t//")
	s.L("\t// The error is PROPAGATED, not discarded: the framework answers 403 when a")
	s.L("\t// caller actively named a field it may not see, and silently omits it when")
	s.L("\t// it merely did not ask. Swallowing the error would collapse the two and")
	s.L("\t// reopen the inference leak that distinction exists to close.")
	s.L("\tallowed := func(string) bool { return false }")
	s.L("\tif id := ctx.Identity(); id != nil {")
	s.L("\t\tallowed = id.HasPermission")
	s.L("\t}")
	for _, fr := range m.Read.FieldRestrict {
		s.L("\tif !allowed(%s) {", quote(fr.Permission))
		s.L("\t\tif err := %s.Restrict(%s); err != nil {", target, quote(fr.Field))
		s.L("\t\t\treturn %s, err", target)
		s.L("\t\t}")
		s.L("\t}")
	}
}

// emitRowScoping narrows a read to the rows the caller may see.
//
// This is a different question from the permission gate: the gate decides
// whether the caller may use the endpoint at all, this decides WHICH ROWS the
// answer contains. Leaving it out means anyone who can read, reads everything.
func emitRowScoping(s *src, m *ir.Model, target string) {
	switch m.Authz.DataAccess {
	case "owner-only":
		if m.Authz.OwnerColumn == "" {
			// Validation refuses a runtime owner field, so an empty column here
			// is generator inconsistency, and returning quietly would ship a
			// service that says owner-only and serves everything. Refuse loudly.
			panic("owner-only with no owner column: validation should have refused this spec")
		}
		s.L("\t// Callers see only their own rows. Filter is a map keyed by the Go")
		s.L("\t// field path, and the scope is FORCED: a value the caller sent for this")
		s.L("\t// field is overwritten, never merged.")
		s.L("\tif %s.Filter == nil {", target)
		s.L("\t\t%s.Filter = map[string]any{}", target)
		s.L("\t}")
		s.L("\tif id := ctx.Identity(); id != nil {")
		s.L("\t\t%s.Filter[%s] = id.Subject", target, quote(m.Authz.OwnerField.Name))
		s.L("\t} else {")
		s.L("\t\t// No identity: no rows. Failing open here would expose every row.")
		s.L("\t\t%s.Filter[%s] = \"\"", target, quote(m.Authz.OwnerField.Name))
		s.L("\t}")
	case "tenant":
		if m.Authz.TenantColumn == "" {
			// Same contract as owner-only above: this shape is refused at
			// validation, and shipping a tenant service with no tenant filter
			// is the one thing this function exists to prevent.
			panic("tenant with no tenant column: validation should have refused this spec")
		}
		s.L("\t// Callers see only their tenant's rows. The scope is FORCED: a value")
		s.L("\t// the caller sent for this field is overwritten, never merged.")
		s.L("\tif %s.Filter == nil {", target)
		s.L("\t\t%s.Filter = map[string]any{}", target)
		s.L("\t}")
		s.L("\tif id := ctx.Identity(); id != nil {")
		s.L("\t\t%s.Filter[%s] = id.TenantID()", target, quote(m.Authz.TenantField.Name))
		s.L("\t} else {")
		s.L("\t\t// No tenant claim: no rows, rather than every tenant's.")
		s.L("\t\t%s.Filter[%s] = \"\"", target, quote(m.Authz.TenantField.Name))
		s.L("\t}")
	}
}
