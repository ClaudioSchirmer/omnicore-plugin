package emit

import (
	"fmt"

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
	handler := fmt.Sprintf("&handlers.FindByParamsQueryHandler[*appqueries.%s]{\n"+
		"\t\t\tReader: d.ViewReader, View: viewName,\n\t\t}", m.Read.QueryList)

	if m.Surfaces.CSV {
		s.L("\t// Mounted at the app root: under the group, %s/:id would match first.", m.Entity.Route)
		s.L("\tcsvH, csvSpec := fwweb.QueryAsCSVSpec(d.Pipeline,")
		s.L("\t\trequests.%s{},", op.RequestType)
		s.L("\t\tview,")
		s.L("\t\td.Export,")
		s.L("\t\t%s,", handler)
		s.L("\t\texport.WithDelimiter('%s'))", m.Surfaces.CSVDelimiter)
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
		s.L("\t// A paged connection over the same view the REST listing reads.")
		s.L("\treg.Register(fwgraphql.QueryWithParams[")
		s.L("\t\trequests.%s,", op.RequestType)
		s.L("\t\trequests.%s,", op.ResponseType)
		s.L("\t](")
		s.L("\t\t%s, %s,", quote(m.Entity.PluralCamel), quote(m.Entity.Pascal))
		s.L("\t\t&handlers.FindByParamsQueryHandler[*appqueries.%s]{", m.Read.QueryList)
		s.L("\t\t\tReader: d.ViewReader, View: view.Name(),")
		s.L("\t\t},")
		s.L("\t\tfwgraphql.RequirePermission(%s)))", quote(op.Permission))
		s.Blank()
	}

	for _, op := range m.WriteOps() {
		if !m.Surfaces.GQLMutations[gqlVerbOf(op.Verb)] {
			continue
		}
		emitGQLMutation(s, m, op, entity)
	}
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
