package emit

import (
	"fmt"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// emitExports mounts the CSV and XLSX renderings of the listing.
//
// They mount at the APPLICATION ROOT, not inside the entity's group: a path
// like /students.csv under /students would be matched by /students/:id first
// and the export would be read as an id.
//
// They also deliberately reuse the listing's own request type, so the filters,
// the search and the field selection are the same ones — an export whose
// filters drift from the list it exports is worse than no export.
func emitExports(s *src, m *ir.Model) {
	if !m.Surfaces.CSV && !m.Surfaces.XLSX {
		return
	}
	op := m.Op("byParams")
	if op == nil {
		return
	}
	handler := listHandler(m)

	if m.Surfaces.CSV {
		s.L("\t// Mounted at the app root: under the group, %s/:id would match first.", m.Entity.Route)
		s.L("\t//")
		s.L("\t// The export projects the SAME Response the JSON listing does, so its")
		s.L("\t// columns are that DTO's fields and its headers their exportLabelKey.")
		s.L("\t// One consequence worth knowing: ?fields= speaks one vocabulary here and")
		s.L("\t// on GET %s — a selection that works on one works on the other.", m.Entity.Route)
		s.L("\tcsvH, csvSpec := fwweb.QueryAsCSVSpec(d.Pipeline,")
		s.L("\t\trequests.%s{},", op.RequestType)
		s.L("\t\trequests.%s{}.FromResult,", op.ResponseType)
		s.L("\t\tview,")
		s.L("\t\td.Export,")
		s.L("\t\t%s,", handler)
		// %q of the rune, not a raw '%s': a quote or a backslash as the
		// delimiter passed validation ("one character") and emitted a literal
		// that did not parse.
		s.L("\t\texport.WithDelimiter(%q))", firstRune(m.Surfaces.CSVDelimiter))
		s.L("\tfwopenapi.Mount(d.OpenAPIRegistry, app, fiber.MethodGet, %s,",
			quote("/"+m.Entity.PluralSnake+".csv"))
		s.L("\t\tcsvH, csvSpec,")
		s.L("\t\tfwopenapi.Doc{")
		s.L("\t\t\tSummary: %s,", quote("Export "+m.Entity.PluralCamel+" as CSV"))
		s.L("\t\t\tDescription: %s,", quote(exportDescription(m, "CSV")))
		s.L("\t\t\tTags: []string{%s},", quote(m.Entity.PluralPascal))
		s.L("\t\t},")
		s.L("\t\tfwopenapi.RequirePermission(%s))", quote(op.Permission))
		s.Blank()
	}

	if m.Surfaces.XLSX {
		s.L("\txlsxH, xlsxSpec := fwweb.QueryAsXLSXSpec(d.Pipeline,")
		s.L("\t\trequests.%s{},", op.RequestType)
		s.L("\t\trequests.%s{}.FromResult,", op.ResponseType)
		s.L("\t\tview,")
		s.L("\t\td.Export,")
		s.L("\t\t%s,", handler)
		s.L("\t\texport.WithSheetName(%s))", quote(m.Surfaces.XLSXSheet))
		s.L("\tfwopenapi.Mount(d.OpenAPIRegistry, app, fiber.MethodGet, %s,",
			quote("/"+m.Entity.PluralSnake+".xlsx"))
		s.L("\t\txlsxH, xlsxSpec,")
		s.L("\t\tfwopenapi.Doc{")
		s.L("\t\t\tSummary: %s,", quote("Export "+m.Entity.PluralCamel+" as Excel"))
		s.L("\t\t\tDescription: %s,", quote(exportDescription(m, "Excel workbook")))
		s.L("\t\t\tTags: []string{%s},", quote(m.Entity.PluralPascal))
		s.L("\t\t},")
		s.L("\t\tfwopenapi.RequirePermission(%s))", quote(op.Permission))
		s.Blank()
	}
}

func exportDescription(m *ir.Model, format string) string {
	return fmt.Sprintf(
		"The same read as GET %s, rendered as %s: identical filters, search and "+
			"field selection. Pagination does not apply — the export returns the whole "+
			"filtered set, capped by the configured export limit. Column headers are the "+
			"fields' labels in the request's language.",
		m.Entity.Route, format)
}

// emitGraphQL registers the same handlers the REST routes use.
//
// Not a second implementation: the handler, the DTOs and the permission are the
// same objects. That is what keeps the two surfaces from drifting — a rule
// added to a command reaches both, because there is only one command.
// firstRune is the delimiter as a rune — validation guarantees exactly one
// character, but a defensive default beats an index panic in an emitter.
func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return ','
}

func emitGraphQL(m *ir.Model) (*src, bool) {
	if !m.Surfaces.GraphQL {
		return nil, false
	}
	s := &src{}
	entity := "appdomain." + m.Entity.Pascal

	s.Doc(
		fmt.Sprintf("Mount%sGraphQL exposes %s on the GraphQL surface.",
			m.Entity.PluralPascal, m.Entity.Camel),
		"",
		"Every field here reuses the handler its REST twin uses, with the same "+
			"permission attached. There is no second implementation to keep in step.",
	)
	s.L("func Mount%sGraphQL(", m.Entity.PluralPascal)
	s.L("\treg *fwgraphql.Registry,")
	s.L("\trepo persistence.ScopedRepository[*%s],", entity)
	if m.Service != nil {
		s.L("\tsvc domain.Service,")
	}
	s.L("\tview *query.ViewDefinition,")
	s.L("\td bootstrap.Deps,")
	s.L(") {")

	if m.Surfaces.GQLConnection && m.Read.ByParams {
		op := m.Op("byParams")
		s.L("\t// A paged connection over the same view the REST listing reads — same")
		s.L("\t// handler, same Response, so the node type and the JSON body cannot drift.")
		s.L("\treg.Register(fwgraphql.QueryWithParams[requests.%s](", op.RequestType)
		s.L("\t\t%s, %s,", quote(m.Entity.PluralCamel), quote(m.Entity.Pascal))
		s.L("\t\trequests.%s{}.FromResult,", op.ResponseType)
		s.L("\t\t%s,", graphQLListHandler(m))
		s.L("\t\tfwgraphql.RequirePermission(%s)))", quote(op.Permission))
		s.Blank()
	}
	if m.Surfaces.GQLConnection && m.Read.ByID {
		op := m.Op("byId")
		s.L("\t// The singular twin of the connection: one nullable node, the same")
		s.L("\t// entity name, so both fields resolve to ONE type in the schema. A")
		s.L("\t// missing id answers null with the canonical not-found in errors[].")
		s.L("\treg.Register(fwgraphql.QueryByID[requests.%s](", op.RequestType)
		s.L("\t\t%s, %s,", quote(m.Entity.Camel), quote(m.Entity.Pascal))
		s.L("\t\trequests.%s{}.FromResult,", op.ResponseType)
		s.L("\t\t%s,", graphQLByIDHandler(m))
		s.L("\t\tfwgraphql.RequirePermission(%s)))", quote(op.Permission))
		s.Blank()
	}

	for _, op := range m.WriteOps() {
		if !m.Surfaces.GQLMutations[gqlVerbOf(op.Verb)] {
			continue
		}
		emitGQLMutation(s, m, op, entity)
	}
	emitFacetClearMutations(s, m, entity)
	s.L("}")
	return s, true
}

// gqlVerbOf maps an operation back to the verb a spec names in its mutation
// list — patch and put are both "update" there.
func gqlVerbOf(verb string) string {
	if verb == "patch" {
		return "update"
	}
	return verb
}

func emitGQLMutation(s *src, m *ir.Model, op ir.Operation, entity string) {
	field := op.Verb + m.Entity.Pascal
	if op.Verb == "insert" {
		field = "create" + m.Entity.Pascal
	}

	switch {
	case op.Bodyless:
		s.L("\treg.Register(fwgraphql.MutationByID(")
		s.L("\t\t%s,", quote(field))
		s.L("\t\t&%s[*%s, *commands.%s, fwresults.None]{", op.HandlerType, entity, op.CommandType)
		s.L("\t\t\tRepo: repo,%s", serviceField(m))
		s.L("\t\t},")
		s.L("\t\tfwgraphql.RequirePermission(%s)))", quote(op.Permission))
	case op.Verb == "insert":
		s.L("\treg.Register(fwgraphql.MutationWithBody[requests.%s](", op.RequestType)
		s.L("\t\t%s, requests.%s{}.FromResult,", quote(field), op.ResponseType)
		s.L("\t\t&%s[*%s, *commands.%s, commands.%s]{",
			op.HandlerType, entity, op.CommandType, op.ResultType)
		s.L("\t\t\tRepo: repo,%s", serviceField(m))
		s.L("\t\t},")
		s.L("\t\tfwgraphql.RequirePermission(%s)))", quote(op.Permission))
	default:
		s.L("\treg.Register(fwgraphql.MutationWithBodyID[requests.%s](", op.RequestType)
		s.L("\t\t%s, requests.%s{}.FromResult,", quote(field), op.ResponseType)
		s.L("\t\t&%s[*%s, *commands.%s, commands.%s]{",
			op.HandlerType, entity, op.CommandType, op.ResultType)
		s.L("\t\t\tRepo: repo,%s", serviceField(m))
		s.L("\t\t},")
		s.L("\t\tfwgraphql.RequirePermission(%s)))", quote(op.Permission))
	}
	s.Blank()
}

// emitFacetClearMutations gives GraphQL the one thing REST gets for free.
//
// A 1:1 facet is cleared by the ROOT's PUT with its fields null — the
// framework's native path, and on REST it works. On GraphQL it cannot exist,
// and not for one reason but two:
//
//   - a LENIENT (patch-shaped) mutation cannot say it, because an omitted field
//     and an explicit null are the same thing by the time the input reaches the
//     DTO: the args map is marshalled to JSON and decoded, and both leave the
//     pointer nil. "Clear this" and "leave this alone" become one message.
//   - a STRICT (full-body) mutation cannot say it either: every field of its
//     input is NonNull in the SDL, so null is refused at parse.
//
// Without a third way, a caller could grant a facet through GraphQL and never
// revoke it — a contract one surface cannot keep.
//
// So the intent gets its own mutation, which is the idiom the conventions
// prescribe: no body to express null WITH, just a verb that says what it does.
// It is emitted rather than declared because it is not a modelling choice —
// it is the GraphQL spelling of a capability the entity already has.
func emitFacetClearMutations(s *src, m *ir.Model, entity string) {
	if !m.Surfaces.GraphQL {
		return
	}
	for _, sib := range m.SiblingsOn("") {
		s.L("\t// The REST clear path — PUT with the facet's fields null — has no")
		s.L("\t// equivalent here, and both ways round are closed. A lenient mutation")
		s.L("\t// cannot express it because an OMITTED field and an explicit null arrive")
		s.L("\t// the same way (the input map is marshalled to JSON and decoded into the")
		s.L("\t// DTO, and both leave the pointer nil) — so \"clear this\" is")
		s.L("\t// indistinguishable from \"leave this alone\". A strict one cannot express")
		s.L("\t// it either: its input is NonNull throughout, so null is rejected at parse.")
		s.L("\t// An intent has no such ambiguity, which is why it gets its own verb.")
		s.L("\treg.Register(fwgraphql.MutationByID(")
		s.L("\t\t%s,", quote("clear"+sib.Name+"Of"+m.Entity.Pascal))
		// The UPDATE handler, not the partial one, and the difference IS the
		// feature: the framework's sibling write skips an all-nil facet on a
		// partial update and deletes its row on a full one.
		s.L("\t\t&handlers.UpdateCommandHandler[*%s, *commands.Clear%sCommand, commands.Clear%sResult]{",
			entity, sib.Name, sib.Name)
		s.L("\t\t\tRepo: repo,%s", serviceField(m))
		s.L("\t\t},")
		s.L("\t\tfwgraphql.RequirePermission(%s)))", quote(m.UpdatePermission()))
		s.Blank()
	}
}

// emitFacetClearCommands writes the command behind that mutation.
//
// It dispatches through the UPDATE handler, and that is the whole feature: the
// framework's sibling write SKIPS an all-nil facet on a partial update and
// deletes its row on a full one. A PATCH-shaped clear answers 200 and changes
// nothing — which is worse than not offering it, because the caller believes
// the facet is gone.
func emitFacetClearCommands(m *ir.Model) ([]fsplan.File, error) {
	if !m.Surfaces.GraphQL {
		return nil, nil
	}
	var out []fsplan.File
	for _, sib := range m.SiblingsOn("") {
		s := &src{}
		s.Blank()
		s.L("package commands")
		s.Blank()
		s.L("import (")
		s.L("\t%s", quote(fwImport("application/configuration")))
		s.L("\t%s", quote(fwImport("application/pipeline")))
		s.L("\t%s", quote(fwImport("domain")))
		s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
		s.L(")")
		s.Blank()

		s.Doc(
			fmt.Sprintf("Clear%sCommand removes the %s facet of one %s.",
				sib.Name, sib.Name, m.Entity.Pascal),
			"",
			"It exists for GraphQL, where a full-body mutation cannot carry an explicit "+
				"null and the root's PUT-with-nulls path is therefore unavailable. On REST "+
				"the same effect is a PUT with the facet's fields null — this command is "+
				"the same intent, spelled as a verb instead of as a value.",
			"",
			"It carries no body: everything it needs is the id and what it means.")
		s.L("type Clear%sCommand struct {", sib.Name)
		s.L("\tpipeline.CommandWithBodyIDBase")
		s.L("}")
		s.Blank()
		s.L("func (cmd *Clear%sCommand) ApplyTo(%s *configuration.AppContext, e *appdomain.%s) error {",
			sib.Name, identityParam(m), m.Entity.Pascal)
		s.L("\t// ApplyTo, not ApplyPartiallyTo: the framework's sibling write leaves an")
		s.L("\t// all-nil facet UNTOUCHED on a partial update and DELETES its row on a")
		s.L("\t// full one. Everything this command does not assign keeps the value it")
		s.L("\t// was loaded with, so \"full\" costs nothing here.")
		plain, groups := ir.PlainAndComposites(sib.Fields)
		for _, f := range plain {
			s.L("\te.%s = nil", f.Name)
		}
		// A composite goes as a WHOLE: its parts are not fields of the entity, and
		// the value object is what the facet's row carries.
		for _, g := range groups {
			s.L("\te.%s = nil", g.Owner())
		}
		emitIdentityFeed(s, m)
		s.L("\treturn nil")
		s.L("}")
		s.Blank()
		s.Doc(fmt.Sprintf("Clear%sResult answers with the owner alone: the facet is gone.", sib.Name))
		s.L("type Clear%sResult struct {", sib.Name)
		s.L("\t%sID domain.ID", m.Entity.Pascal)
		s.L("}")
		s.Blank()
		s.L("func (cmd *Clear%sCommand) FromEntity(_ *configuration.AppContext, e *appdomain.%s) (Clear%sResult, error) {",
			sib.Name, m.Entity.Pascal, sib.Name)
		s.L("\treturn Clear%sResult{%sID: *e.GetID()}, nil", sib.Name, m.Entity.Pascal)
		s.L("}")

		f, err := goFile("internal/application/commands/clear_"+naming.Snake(sib.Name)+"_command.go",
			fsplan.Owned, fmt.Sprintf("the %s facet clear command, for the surface that cannot send null", sib.Name), s)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}
