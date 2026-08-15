package emit

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

func emitWeb(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File

	for _, op := range m.WriteOps() {
		if op.Bodyless {
			continue // the id comes from the path; there is no DTO to write
		}
		f, err := emitWriteDTO(m, op)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if m.Read.ByID {
		f, err := emitByIDDTO(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if m.Read.ByParams {
		f, err := emitListDTO(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if len(m.Children) > 0 {
		f, err := emitChildDTOs(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	f, err := emitRoutes(m)
	if err != nil {
		return nil, err
	}
	return append(out, f), nil
}

func requestImports(s *src, m *ir.Model, needQueries bool) {
	s.L("import (")
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote(fwImport("domain")))
	if needQueries {
		s.L("\tfwqueries %s", quote(fwImport("application/queries")))
	}
	s.L("\t%s", quote(m.ImportPath("internal/application/commands")))
	s.L("\tappqueries %s", quote(m.ImportPath("internal/application/queries")))
	s.L(")")
	s.Blank()
}

func emitWriteDTO(m *ir.Model, op ir.Operation) (fsplan.File, error) {
	partial := op.InputMethod == "ApplyPartiallyTo"

	s := &src{}
	s.header(m, fmt.Sprintf("Wire types for the %s operation.", op.Verb))
	s.Blank()
	s.L("package requests")
	s.Blank()
	requestImports(s, m, false)

	// ── request
	s.Doc(fmt.Sprintf("%s is the body of %s.", op.RequestType, op.Summary))
	if partial {
		s.Doc("", "Every field is optional: omitting one leaves the stored value untouched.")
	}
	s.L("type %s struct {", op.RequestType)
	for _, f := range commandFields(m, partial) {
		s.L("\t%s %s `json:%s example:%s`", f.Name,
			commandFieldType(f, partial), quote(jsonTag(f, partial)), quote(f.Example))
	}
	if !partial {
		for _, c := range m.Children {
			s.L("\t%s []%sRequest `json:%s`", c.GoPlural, c.Name, quote(c.Segment))
		}
	}
	s.L("}")
	s.Blank()

	s.Doc("ToCommand hands the body to the application layer unchanged. " +
		"No normalisation happens here: the domain is what decides a value's final form.")
	s.L("func (r %s) ToCommand() *commands.%s {", op.RequestType, op.CommandType)
	s.L("\tcmd := &commands.%s{", op.CommandType)
	for _, f := range commandFields(m, partial) {
		s.L("\t\t%s: r.%s,", f.Name, f.Name)
	}
	s.L("\t}")
	if !partial {
		for _, c := range m.Children {
			s.L("\tfor _, item := range r.%s {", c.GoPlural)
			s.L("\t\tcmd.%s = append(cmd.%s, item.ToInput())", c.GoPlural, c.GoPlural)
			s.L("\t}")
		}
	}
	s.L("\treturn cmd")
	s.L("}")
	s.Blank()

	// ── response
	s.Doc(fmt.Sprintf("%s is what %s returns.", op.ResponseType, op.Summary))
	s.L("type %s struct {", op.ResponseType)
	s.L("\tID domain.ID `json:\"id\" example:\"7b3c1f10-3c7e-4a8d-9f0e-9d2a8e6d4b51\"`")
	for _, f := range m.AllOwnerFields() {
		s.L("\t%s %s `json:%s example:%s`", f.Name, f.GoType, quote(jsonTag(f, false)), quote(f.Example))
	}
	for _, c := range m.Children {
		s.L("\t%s []%sResponse `json:%s`", c.GoPlural, c.Name, quote(c.Segment))
	}
	s.L("}")
	s.Blank()
	s.L("func (%s) FromResult(r commands.%s) %s {", op.ResponseType, op.ResultType, op.ResponseType)
	s.L("\tout := %s{", op.ResponseType)
	s.L("\t\tID: r.ID,")
	for _, f := range m.AllOwnerFields() {
		s.L("\t\t%s: r.%s,", f.Name, f.Name)
	}
	s.L("\t}")
	for _, c := range m.Children {
		s.L("\tfor _, item := range r.%s {", c.GoPlural)
		s.L("\t\tout.%s = append(out.%s, %sResponse{", c.GoPlural, c.GoPlural, c.Name)
		s.L("\t\t\tID: item.ID,")
		for _, f := range c.Fields {
			s.L("\t\t\t%s: item.%s,", f.Name, f.Name)
		}
		s.L("\t\t})")
		s.L("\t}")
	}
	s.L("\treturn out")
	s.L("}")
	s.Blank()
	s.L("var _ = time.Time{}")

	return goFile("internal/web/requests/"+op.Verb+"_"+m.Entity.Snake+".go", fsplan.Owned,
		fmt.Sprintf("the %s request and response", op.Verb), s)
}

// jsonTag builds the json tag value. The omitempty option belongs INSIDE the
// quoted value: outside it, the tag silently stops parsing as a struct tag and
// every option after it is lost.
func jsonTag(f ir.Field, optional bool) string {
	if optional || f.Nullable {
		return f.JSONName + ",omitempty"
	}
	return f.JSONName
}

func emitByIDDTO(m *ir.Model) (fsplan.File, error) {
	op := m.Op("byId")
	s := &src{}
	s.header(m, "Wire types for the by-id read.")
	s.Blank()
	s.L("package requests")
	s.Blank()
	requestImports(s, m, false)

	s.Doc(
		fmt.Sprintf("%s declares the read controls this endpoint serves.", op.RequestType),
		"",
		"The set is a contract, not a convenience: a reserved control that is not "+
			"declared here is rejected with a typed 400 when it appears on the wire. "+
			"The route's own :id is bound by the framework and must never be declared.")
	s.L("type %s struct {", op.RequestType)
	if m.Managed.Archiving && m.Read.Controls.IncludeArchived {
		s.L("\tIncludeArchived *bool `query:\"includeArchived\"`")
	}
	s.L("}")
	s.Blank()

	s.L("func (r %s) ToQuery() *appqueries.%s {", op.RequestType, m.Read.QueryByID)
	if m.Managed.Archiving && m.Read.Controls.IncludeArchived {
		s.L("\tincludeArchived := false")
		s.L("\tif r.IncludeArchived != nil {")
		s.L("\t\tincludeArchived = *r.IncludeArchived")
		s.L("\t}")
		s.L("\treturn &appqueries.%s{IncludeArchived: includeArchived}", m.Read.QueryByID)
	} else {
		s.L("\treturn &appqueries.%s{}", m.Read.QueryByID)
	}
	s.L("}")
	s.Blank()

	s.Doc(
		fmt.Sprintf("%s is the projected document.", op.ResponseType),
		"",
		"It carries the WHOLE aggregate — the root's fields, the facets' fields and "+
			"the collections. Reading one record and getting less of it than the listing "+
			"gives for the same record is the shape nobody expects, and there is no "+
			"second request that would fill the gap: the document was already fetched.")
	s.L("type %s struct {", op.ResponseType)
	s.L("\tID string `json:\"id\" example:\"7b3c1f10-3c7e-4a8d-9f0e-9d2a8e6d4b51\"`")
	for _, f := range m.AllOwnerFields() {
		s.L("\t%s %s `json:%s example:%s`", f.Name, f.GoType, quote(f.JSONName), quote(f.Example))
	}
	for _, c := range m.Children {
		s.L("\t%s []%sRow `json:%s`", c.GoPlural, c.Name, quote(c.Segment))
	}
	s.L("}")
	s.Blank()
	s.L("var _ = time.Time{}")

	return goFile("internal/web/requests/find_"+m.Entity.Snake+"_by_id.go", fsplan.Owned,
		"the by-id request and response", s)
}

func emitListDTO(m *ir.Model) (fsplan.File, error) {
	op := m.Op("byParams")
	s := &src{}
	s.header(m, "Wire types for the listing.")
	s.Blank()
	s.L("package requests")
	s.Blank()
	requestImports(s, m, true)

	s.Doc(
		fmt.Sprintf("%s declares the filters and read controls this listing serves.", op.RequestType),
		"",
		"Every scalar is a pointer on purpose. The OpenAPI generator marks a "+
			"non-pointer parameter as required, so one value-typed filter would make an "+
			"optional query parameter mandatory and Swagger would refuse the call "+
			"without it.")
	s.L("type %s struct {", op.RequestType)
	for _, f := range m.Read.Filters {
		s.L("\t%s *%s `query:%s filter:%s`", f.Field.Name, f.Field.BaseGoType,
			quote(f.Field.JSONName), quote(strings.Join(f.Ops, ",")))
	}
	emitReadControls(s, m)
	s.L("}")
	s.Blank()

	s.L("func (r %s) ToQuery(criteria fwqueries.ReadCriteria) *appqueries.%s {",
		op.RequestType, m.Read.QueryList)
	s.L("\treturn &appqueries.%s{Criteria: criteria}", m.Read.QueryList)
	s.L("}")
	s.Blank()

	// The response is projected field by field by the framework's doc
	// projector; with ?fields= on, every field must be a pointer or slice with
	// omitempty, or the framework panics when the wrapper is built.
	pointered := m.Read.Controls.Fields
	s.Doc(fmt.Sprintf("%s is one row of the listing.", op.ResponseType))
	if pointered {
		s.Doc("",
			"Every field is a pointer with omitempty because this listing serves "+
				"?fields=: partial projection has to be able to leave a field out, and the "+
				"framework refuses to build the endpoint otherwise.")
	}
	s.L("type %s struct {", op.ResponseType)
	if pointered {
		s.L("\tID *string `json:\"id,omitempty\" example:\"7b3c1f10-3c7e-4a8d-9f0e-9d2a8e6d4b51\"`")
	} else {
		s.L("\tID string `json:\"id\" example:\"7b3c1f10-3c7e-4a8d-9f0e-9d2a8e6d4b51\"`")
	}
	// The facet's fields and the collections belong here, not only on the by-id
	// read. The whole aggregate is already in hand when the listing is served,
	// so leaving them out discards data that was fetched anyway and pushes the
	// caller into one extra request per row to get it back.
	for _, f := range m.AllOwnerFields() {
		typ, tag := f.GoType, quote(f.JSONName)
		if pointered {
			typ = "*" + f.BaseGoType
			tag = quote(f.JSONName + ",omitempty")
		}
		s.L("\t%s %s `json:%s example:%s`", f.Name, typ, tag, quote(f.Example))
	}
	for _, c := range m.Children {
		s.L("\t%s []%sRow `json:%s`", c.GoPlural, c.Name, quote(c.Segment+",omitempty"))
	}
	s.L("}")
	s.Blank()
	s.L("var _ = time.Time{}")

	return goFile("internal/web/requests/find_"+m.Entity.PluralSnake+"_by_params.go", fsplan.Owned,
		"the listing request and response", s)
}

// emitReadControls declares exactly the reserved controls the spec asked for.
//
// The vocabulary is closed and checked at boot: a key that is not one of the
// framework's controls panics when the wrapper is built, which is why these
// names are never improvised.
func emitReadControls(s *src, m *ir.Model) {
	c := m.Read.Controls
	if c.Pagination {
		s.L("\tFirst  *int64  `query:\"first\"`")
		s.L("\tLast   *int64  `query:\"last\"`")
		s.L("\tAfter  *string `query:\"after\"`")
		s.L("\tBefore *string `query:\"before\"`")
	}
	if c.OrderBy {
		s.L("\tOrderBy *string `query:\"orderBy\"`")
	}
	if c.Fields {
		s.L("\tFields *string `query:\"fields\"`")
	}
	if len(c.Search) > 0 {
		s.L("\tSearch *string `query:\"search\"`")
	}
	if c.OnlyTotal {
		s.L("\tOnlyTotal *bool `query:\"onlyTotal\"`")
	}
	if c.IncludeArchived {
		s.L("\tIncludeArchived *bool `query:\"includeArchived\"`")
	}
}

func emitRoutes(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.header(m, fmt.Sprintf("Every surface of %s is mounted here.", m.Entity.Pascal))
	s.Blank()
	s.L("package web")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote(fwImport("application/handlers")))
	s.L("\t%s", quote(fwImport("application/persistence")))
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\tfwresults %s", quote(fwImport("application/results")))
	s.L("\t%s", quote(fwImport("bootstrap")))
	s.L("\t%s", quote(fwImport("infra/db/query")))
	s.L("\tfwweb %s", quote(fwImport("web")))
	s.L("\tfwopenapi %s", quote(fwImport("web/openapi")))
	s.L("\tfwresponses %s", quote(fwImport("web/responses")))
	s.L("\t%s", quote(fwImport("web/export")))
	s.L("\tfwgraphql %s", quote(fwImport("web/graphql")))
	s.L("\t%s", quote(m.ImportPath("internal/application/commands")))
	s.L("\tappqueries %s", quote(m.ImportPath("internal/application/queries")))
	s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
	s.L("\t%s", quote(m.ImportPath("internal/web/requests")))
	s.L("\t%s", quote("github.com/gofiber/fiber/v3"))
	s.L(")")
	s.Blank()

	entity := "appdomain." + m.Entity.Pascal
	s.Doc(fmt.Sprintf("Mount%s registers every %s endpoint.", m.Entity.PluralPascal, m.Entity.Camel),
		"",
		"The repository arrives as an interface, not as the concrete infra type: the "+
			"web layer must not depend on infra.")
	s.L("func Mount%s(", m.Entity.PluralPascal)
	s.L("\tapp *fiber.App,")
	s.L("\trepo persistence.ScopedRepository[*%s],", entity)
	if m.Service != nil {
		s.L("\tsvc domain.Service,")
	}
	s.L("\tview *query.ViewDefinition,")
	s.L("\td bootstrap.Deps,")
	s.L(") {")
	if !m.Surfaces.REST {
		// surfaces.rest is false: this entity is reachable through GraphQL only.
		// The mount still exists and is still called, so the wiring, the
		// repository and the view stay identical — what changes is that no HTTP
		// route is registered, which is the whole of what the author asked for.
		s.L("\t// No HTTP routes: surfaces.rest is false, so %s is served through", m.Entity.Pascal)
		s.L("\t// GraphQL alone. Everything else is wired exactly as it would be.")
		s.L("\t_, _, _ = app, repo, view")
		if m.Service != nil {
			s.L("\t_ = svc")
		}
		s.L("\t_ = d")
		s.L("}")
	} else {
		s.L("\tgroup := app.Group(%s)", quote(m.Entity.Route))
		if m.Read.Enabled {
			s.L("\tviewName := view.Name()")
		} else {
			s.L("\t_ = view")
		}
		s.Blank()

		for _, op := range m.Ops {
			emitRoute(s, m, op, entity)
		}
		emitPerChildRoutes(s, m, entity)
		emitExports(s, m)
		s.L("}")
	}

	if gql, ok := emitGraphQL(m); ok {
		s.Blank()
		s.Write(gql.Bytes())
	}

	return goFile("internal/web/"+m.Entity.Snake+"_routes.go", fsplan.Owned,
		fmt.Sprintf("the %d %s endpoints", len(m.Ops), m.Entity.Camel), s)
}

func emitRoute(s *src, m *ir.Model, op ir.Operation, entity string) {
	v := op.Verb
	hv := v + "H"
	sv := v + "Spec"

	switch {
	case op.Verb == "byParams":
		s.L("\t%s, %s := fwweb.QueryWithParamsSpec(d.Pipeline,", hv, sv)
		s.L("\t\trequests.%s{},", op.RequestType)
		s.L("\t\tfwresponses.AutoFromDoc[requests.%s],", op.ResponseType)
		s.L("\t\t&handlers.FindByParamsQueryHandler[*appqueries.%s]{", m.Read.QueryList)
		s.L("\t\t\tReader: d.ViewReader, View: viewName,")
		s.L("\t\t})")
	case op.Verb == "byId":
		s.L("\t%s, %s := fwweb.QueryByIDSpec(d.Pipeline,", hv, sv)
		s.L("\t\trequests.%s{},", op.RequestType)
		s.L("\t\tfwresponses.AutoFromDoc[requests.%s],", op.ResponseType)
		s.L("\t\t&handlers.FindByIDQueryHandler[*appqueries.%s]{", m.Read.QueryByID)
		s.L("\t\t\tReader: d.ViewReader, View: viewName,")
		s.L("\t\t})")
	case op.Bodyless:
		s.L("\t%s, %s := fwweb.CommandByIDSpec(d.Pipeline,", hv, sv)
		s.L("\t\tfwresponses.NoBody,")
		s.L("\t\t&%s[*%s, *commands.%s, fwresults.None]{", op.HandlerType, entity, op.CommandType)
		s.L("\t\t\tRepo: repo,%s", serviceField(m))
		s.L("\t\t}, %s)", op.Status)
	case op.Verb == "insert":
		s.L("\t%s, %s := fwweb.CommandWithBodySpec(d.Pipeline,", hv, sv)
		s.L("\t\trequests.%s{},", op.RequestType)
		s.L("\t\trequests.%s{}.FromResult,", op.ResponseType)
		s.L("\t\t&%s[*%s, *commands.%s, commands.%s]{", op.HandlerType, entity, op.CommandType, op.ResultType)
		s.L("\t\t\tRepo: repo,%s", serviceField(m))
		s.L("\t\t}, %s)", op.Status)
	default:
		s.L("\t%s, %s := fwweb.CommandWithBodyIDSpec(d.Pipeline,", hv, sv)
		s.L("\t\trequests.%s{},", op.RequestType)
		s.L("\t\trequests.%s{}.FromResult,", op.ResponseType)
		s.L("\t\t&%s[*%s, *commands.%s, commands.%s]{", op.HandlerType, entity, op.CommandType, op.ResultType)
		s.L("\t\t\tRepo: repo,%s", serviceField(m))
		s.L("\t\t}, %s)", op.Status)
	}

	s.L("\tfwopenapi.Mount(d.OpenAPIRegistry, group, %s, %s,", op.Method, quote(op.Path))
	s.L("\t\t%s, %s,", hv, sv)
	s.L("\t\tfwopenapi.Doc{")
	s.L("\t\t\tSummary: %s,", quote(op.Summary))
	s.L("\t\t\tDescription: %s,", quote(routeDescription(m, op)))
	s.L("\t\t\tTags: []string{%s},", quote(m.Entity.PluralPascal))
	s.L("\t\t},")
	s.L("\t\tfwopenapi.RequirePermission(%s))", quote(op.Permission))
	s.Blank()
}

// routeDescription writes documentation a reader actually benefits from: what
// the endpoint does and the one behaviour that surprises people.
func routeDescription(m *ir.Model, op ir.Operation) string {
	e := strings.ToLower(m.Entity.Pascal)
	switch op.Verb {
	case "insert":
		return fmt.Sprintf("Creates %s %s and returns it as stored — the response reflects "+
			"any value the domain normalised or defaulted, not an echo of the request.",
			articleFor(e), e)
	case "update":
		return "Full replacement: every writable field must be present, and a nullable " +
			"field must arrive at least as an explicit null. Sending a facet as all-null " +
			"is how it gets cleared."
	case "patch":
		return "Partial update: only the fields present in the body change. Because an " +
			"absent field and an explicit null cannot be told apart here, this verb " +
			"cannot set a value back to null."
	case "delete":
		return fmt.Sprintf("Permanently removes the %s. This is irreversible — the "+
			"reversible removal is the archive endpoint.", e)
	case "archive":
		return fmt.Sprintf("Archives the %s: the row stays but is hidden from reads "+
			"unless they ask for archived rows. Reversible through unarchive.", e)
	case "unarchive":
		return fmt.Sprintf("Restores a previously archived %s.", e)
	case "byId":
		return fmt.Sprintf("Reads one %s. Only the controls this endpoint declares are "+
			"accepted; anything else is rejected with a typed 400.", e)
	case "byParams":
		return fmt.Sprintf("Paged listing of %s. Unknown filter keys and operators are "+
			"rejected with a typed 400 rather than silently ignored.", strings.ToLower(m.Entity.PluralPascal))
	}
	return op.Summary
}

func articleFor(word string) string {
	if word == "" {
		return "a"
	}
	switch word[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}

// serviceField injects the domain service into a write handler.
//
// It goes on EVERY write verb, unarchive included: bringing a row back can
// collide with one created while it was away, and a rule that cannot ask about
// that is a rule that silently does not run.
func serviceField(m *ir.Model) string {
	if m.Service == nil {
		return ""
	}
	return " Service: svc,"
}

// emitChildDTOs writes the wire shapes for the collections.
//
// The request carries no id: on a create the server mints it, and accepting one
// from the caller would let them choose a key they have no business choosing.
// The response does carry it, because that is how the caller addresses the entry
// afterwards.
func emitChildDTOs(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.header(m, "Wire types for the child collections.")
	s.Blank()
	s.L("package requests")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\t%s", quote(m.ImportPath("internal/application/dtos")))
	s.L(")")
	s.Blank()

	// The nested row BOTH reads return. One type, so a caller that walks the
	// listing and then opens one record does not meet two shapes for one thing.
	//
	// Pointer-and-omitempty throughout, RECURSIVELY, when the listing serves
	// ?fields=: partial projection has to be able to leave a field out, and the
	// framework refuses to build that endpoint otherwise — at boot, not at
	// compile time.
	if m.Read.Enabled {
		pointered := m.Read.Controls.Fields
		for _, c := range m.Children {
			if c.Mounted {
				continue // one shape, declared by the role that owns the identity
			}
			s.Doc(fmt.Sprintf("%sRow is one entry of the %s collection as a read returns it.",
				c.Name, c.Segment))
			s.L("type %sRow struct {", c.Name)
			if pointered {
				s.L("\tID *string `json:\"id,omitempty\"`")
			} else {
				s.L("\tID string `json:\"id\"`")
			}
			for _, f := range c.Fields {
				typ, tag := f.GoType, quote(f.JSONName)
				if pointered {
					typ = "*" + f.BaseGoType
					tag = quote(f.JSONName + ",omitempty")
				}
				s.L("\t%s %s `json:%s example:%s`", f.Name, typ, tag, quote(f.Example))
			}
			s.L("}")
			s.Blank()
		}
	}

	for _, c := range m.Children {
		if c.Mounted {
			// The entry's wire shape is the collection's, not this role's: it is
			// declared once, by the spec that owns the identity, and both roles
			// send and return the same JSON for the same row.
			continue
		}
		s.Doc(fmt.Sprintf("%sRequest is one entry sent in the %s collection.", c.Name, c.Segment),
			"",
			"It carries no id: the server mints one.")
		s.L("type %sRequest struct {", c.Name)
		for _, f := range c.Fields {
			s.L("\t%s %s `json:%s example:%s`", f.Name, f.GoType,
				quote(jsonTag(f, false)), quote(f.Example))
		}
		s.L("}")
		s.Blank()

		s.L("func (r %sRequest) ToInput() dtos.%s {", c.Name, c.InputType)
		s.L("\treturn dtos.%s{", c.InputType)
		for _, f := range c.Fields {
			s.L("\t\t%s: r.%s,", f.Name, f.Name)
		}
		s.L("\t}")
		s.L("}")
		s.Blank()

		s.Doc(fmt.Sprintf("%sResponse is one entry as stored, with the id it was given.", c.Name))
		s.L("type %sResponse struct {", c.Name)
		s.L("\tID domain.ID `json:\"id\"`")
		for _, f := range c.Fields {
			s.L("\t%s %s `json:%s example:%s`", f.Name, f.GoType,
				quote(jsonTag(f, false)), quote(f.Example))
		}
		s.L("}")
		s.Blank()
	}
	s.L("var _ = time.Time{}")

	return goFile("internal/web/requests/"+m.Entity.Snake+"_children.go", fsplan.Owned,
		fmt.Sprintf("the wire types for %d child collection(s)", len(m.Children)), s)
}

// emitPerChildRoutes mounts the three verbs that address ONE entry.
//
// They hang off the owner's path because the entry has no life of its own: it
// is loaded, changed and saved as part of the aggregate, in one transaction, so
// the collection's invariants are checked against what will actually be stored.
// A caller who wants the whole collection replaced still has the root's own
// update — these verbs exist so that adding one entry does not mean resending
// every other one, which is both wasteful and a lost-update race between two
// callers.
func emitPerChildRoutes(s *src, m *ir.Model, entity string) {
	for _, c := range m.Children {
		if !c.PerChild {
			continue
		}
		seg := c.Segment
		opName := c.OpBase // qualified when the collection is mounted from a shared identity
		idParam := lowerFirst(c.Name) + "Id"
		perm := updatePermission(m)
		// The Go type string is for the CODE positions below; the OpenAPI
		// summaries are read by an API consumer, who got "a appdomain.Person".
		human := m.Entity.Pascal

		for _, op := range []perChildOp{
			{
				verb: "Add", method: "fiber.MethodPost", path: "/:id/" + seg,
				request: "Add" + opName + "Request", response: "Add" + opName + "Response",
				result: "Add" + opName + "Result", status: "fiber.StatusCreated",
				summary: fmt.Sprintf("Add one %s to %s %s", c.Name, articleFor(human), human),
				doc: fmt.Sprintf("Adds ONE entry to the %s collection of an existing %s, "+
					"in the owner's transaction. 404 when the owner is not there. The "+
					"response carries the entry AS STORED, including the id the server "+
					"minted for it — that id is how the caller addresses it afterwards.",
					c.Segment, human),
			},
			{
				verb: "Change", method: "fiber.MethodPut", path: "/:id/" + seg + "/:" + idParam,
				request: "Change" + opName + "Request", response: "Change" + opName + "Response",
				result: "Change" + opName + "Result", status: "fiber.StatusOK",
				summary: fmt.Sprintf("Replace one %s of %s %s", c.Name, articleFor(human), human),
				doc: fmt.Sprintf("Full replacement of ONE entry, keeping its id — the row " +
					"is updated rather than removed and re-added, so the audit trail reads " +
					"as a change. 404 when the owner is not there, and 404 when the owner " +
					"exists but holds no entry with that id."),
			},
			removeOp(c, human, seg, idParam, opName),
		} {
			hv := "h" + op.verb + opName
			sv := "s" + op.verb + opName
			s.L("\t%s, %s := fwweb.CommandWithBodyIDSpec(d.Pipeline,", hv, sv)
			s.L("\t\trequests.%s{},", op.request)
			s.L("\t\trequests.%s{}.FromResult,", op.response)
			s.L("\t\t&handlers.UpdateCommandHandler[*%s, *commands.%s, commands.%s]{",
				entity, op.verb+opName+"Command", op.result)
			s.L("\t\t\tRepo: repo,%s", serviceField(m))
			s.L("\t\t}, %s)", op.status)
			s.L("\tfwopenapi.Mount(d.OpenAPIRegistry, group, %s, %s,", op.method, quote(op.path))
			s.L("\t\t%s, %s,", hv, sv)
			s.L("\t\tfwopenapi.Doc{")
			s.L("\t\t\tSummary: %s,", quote(op.summary))
			s.L("\t\t\tDescription: %s,", quote(op.doc))
			s.L("\t\t\tTags: []string{%s},", quote(m.Entity.PluralPascal))
			s.L("\t\t},")
			s.L("\t\tfwopenapi.RequirePermission(%s))", quote(perm))
			s.Blank()
		}
	}
}

// updatePermission is what a per-entry verb requires.
//
// Editing one entry is editing the aggregate, so it asks for the same
// permission the root's update asks for: a caller allowed to replace the whole
// collection but not to add one entry to it would be a distinction nobody
// asked for.
func updatePermission(m *ir.Model) string {
	for _, verb := range []string{"update", "patch", "insert"} {
		if op := m.Op(verb); op != nil && op.Permission != "" {
			return op.Permission
		}
	}
	return ""
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}

// removeOp picks the VERB that tells the truth about what removal does here.
//
// A child that declares softRemove keeps its row and its archive stamp: taking
// an entry out is reversible, and the framework performs it as an archive of
// that item. Mounting it as DELETE would promise an irreversible purge the
// endpoint does not perform — and DELETE is the one verb a caller is entitled
// to read as permanent. A child WITHOUT an archive column really is deleted, so
// there DELETE is the honest spelling.
//
// The two also differ in what the caller can do next: an archived entry can be
// brought back, and nothing about a purged one can.
func removeOp(c ir.Child, entity, seg, idParam, opName string) perChildOp {
	if c.ArchivedAt != "" {
		return perChildOp{
			verb: "Remove", method: "fiber.MethodPatch",
			path:    "/:id/" + seg + "/:" + idParam + "/archive",
			request: "Remove" + opName + "Request", response: "Remove" + opName + "Response",
			result: "Remove" + opName + "Result", status: "fiber.StatusOK",
			summary: fmt.Sprintf("Archive one %s of %s %s", c.Name, articleFor(entity), entity),
			doc: "Archives ONE entry: the row stays, stamped, and stops being " +
				"returned — reversible, which is why this is not a DELETE. 404 when the " +
				"owner is not there, and 404 when it holds no entry with that id.",
		}
	}
	return perChildOp{
		verb: "Remove", method: "fiber.MethodDelete",
		path:    "/:id/" + seg + "/:" + idParam,
		request: "Remove" + opName + "Request", response: "Remove" + opName + "Response",
		result: "Remove" + opName + "Result", status: "fiber.StatusOK",
		summary: fmt.Sprintf("Remove one %s from %s %s", c.Name, articleFor(entity), entity),
		doc: "Removes ONE entry for good: this child declares no archive column, " +
			"so there is nothing to bring back. 404 when the owner is not there, and " +
			"404 when it holds no entry with that id.",
	}
}

// perChildOp is one mounted per-entry verb, fully decided.
type perChildOp struct {
	verb, method, path, request, response, result, summary, doc string
	status                                                      string
}
