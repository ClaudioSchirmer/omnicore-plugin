package emit

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
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
		files, err := emitChildDTOs(m)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
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
	// Both markers, every time: the pruner drops whichever a file does not
	// reference, and deciding it here would mean re-deriving per file what the
	// emitters already know.
	s.L("\tfwrequests %s", quote(fwImport("web/requests")))
	s.L("\tfwresponses %s", quote(fwImport("web/responses")))
	s.L("\t%s", quote(m.ImportPath("internal/application/commands")))
	s.L("\tappqueries %s", quote(m.ImportPath("internal/application/queries")))
	// The entry shapes every operation carrying a collection names. Pruned for
	// an entity with no children.
	s.L("\t%s %s", webDTOAlias, quote(m.ImportPath(webDTOPkg)))
	s.L(")")
	s.Blank()
}

func emitWriteDTO(m *ir.Model, op ir.Operation) (fsplan.File, error) {
	partial := op.InputMethod == "ApplyPartiallyTo"

	s := &src{}
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
	s.L("\t%s", autoRequestEmbed)
	s.Blank()
	for _, f := range commandFields(m, op) {
		emitBypassSettableNote(s, m, f)
		s.L("\t%s %s `json:%s example:%s`", f.Name,
			commandFieldType(f, partial), quote(jsonTag(f, partial)), quote(f.Example))
	}
	if !partial {
		for _, c := range m.Children {
			s.L("\t%s []%s `json:%s`", c.GoPlural, childWireRequest(c), quote(c.Segment))
		}
	}
	s.L("}")
	s.Blank()

	emitAutoToCommand(s, op.RequestType, op.CommandType)
	s.Blank()

	// ── response
	s.Doc(fmt.Sprintf("%s is what %s returns.", op.ResponseType, op.Summary))
	s.L("type %s struct {", op.ResponseType)
	s.L("\t%s", autoResponseEmbed)
	s.Blank()
	s.L("\tID domain.ID `json:\"id\" example:\"7b3c1f10-3c7e-4a8d-9f0e-9d2a8e6d4b51\"`")
	for _, f := range m.ResponseFields() {
		s.L("\t%s %s `json:%s example:%s`", f.Name, f.GoType, quote(jsonTag(f, false)), quote(f.Example))
	}
	// The runtime values this verb hands over — declared with renderIn, minted by
	// a rule the author wrote, and on no row. This response is the only surface
	// that carries them: no read renders them, because there is nothing stored to
	// render. A GraphQL mutation reusing this Response renders them too, which is
	// one shape by design and not an oversight.
	for _, f := range m.RenderedRuntimeFields(ir.GateModeOf(op.Verb)) {
		s.L("\t%s %s `json:%s example:%s`", f.Name, f.GoType,
			quote(jsonTag(f, false)), quote(f.Example))
	}
	// The derived fields this verb can answer. A caller that just created the
	// record gets the same rendering a read would give it, instead of having to
	// fetch it again to see what it made.
	for _, c := range writeComputedFields(m) {
		s.L("\t%s %s `json:%s computed:%s example:%s`", c.Name, c.GoType,
			quote(c.JSONName), quote(strings.Join(c.Sources, ",")), quote(c.Example))
	}
	for _, c := range m.Children {
		s.L("\t%s []%s `json:%s`", c.GoPlural, childWireResponse(c), quote(c.Segment))
	}
	s.L("}")
	s.Blank()
	emitAutoFromResult(s, op.ResponseType, "commands."+op.ResultType,
		"",
		"It reads the entity as STORED, never an echo of the request: the domain may "+
			"have normalised or defaulted a value, and echoing the input back would hide "+
			"that from the caller.")

	return goFile("internal/web/requests/"+op.Verb+"_"+m.Entity.Snake+".go", fsplan.Owned,
		fmt.Sprintf("the %s request and response", op.Verb), s)
}

// jsonTag builds the json tag value. The omitempty option belongs INSIDE the
// quoted value: outside it, the tag silently stops parsing as a struct tag and
// every option after it is lost.
// emitBypassSettableNote explains, at the one field that needs it, why a
// server-assigned value is in a request body at all.
//
// Without it the field reads as an ordinary optional input, and the next
// reader's reasonable conclusion — "the caller picks the tenant" — is exactly
// the misunderstanding that would get the pointer removed and the value taken
// from everyone.
func emitBypassSettableNote(s *src, m *ir.Model, f ir.Field) {
	if !f.WireOptional || !f.BypassMaySet {
		return
	}
	who := "a caller holding " + m.Authz.Bypass
	if m.Authz.BypassWildcard {
		who = "a super-admin (" + m.Authz.Bypass + ")"
	}
	for _, line := range wrap(fmt.Sprintf("Optional, and server-assigned for almost "+
		"everyone: leave it out and the value is read from the caller's own identity. "+
		"It is here for %s, who crosses the row scope and has to be able to say which "+
		"one a NEW record belongs to. A value from anyone else is not ignored — it "+
		"reaches the aggregate, where the row-scope guard refuses it exactly as it "+
		"refuses a write into a record that is not the caller's.", who), 68) {
		s.L("\t// %s", line)
	}
}

func jsonTag(f ir.Field, optional bool) string {
	if optional || f.Nullable || f.WireOptional {
		return f.JSONName + ",omitempty"
	}
	return f.JSONName
}

func emitByIDDTO(m *ir.Model) (fsplan.File, error) {
	op := m.Op("byId")
	s := &src{}
	s.Blank()
	s.L("package requests")
	s.Blank()
	requestImports(s, m, true)

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

	// ToQuery takes the parsed criteria, exactly like the listing's does. A
	// by-id read speaks a SMALLER wire vocabulary — one reserved control — but
	// it is the same seat, so the control this DTO declares reaches the query
	// without anyone unwrapping a *bool by hand.
	s.Doc("ToQuery is the web→application boundary: pure mapping, no ctx. " +
		"Identity-derived overlays layer onto the criteria inside the query's " +
		"ToCriteria(ctx), which is the only layer below the web boundary that may " +
		"read the AppContext.")
	s.L("func (r %s) ToQuery(criteria fwqueries.ReadCriteria) *appqueries.%s {",
		op.RequestType, m.Read.QueryByID)
	s.L("\treturn &appqueries.%s{Criteria: criteria}", m.Read.QueryByID)
	s.L("}")
	s.Blank()

	s.Doc(
		fmt.Sprintf("%s is the projected document.", op.ResponseType),
		"",
		"It carries the WHOLE aggregate — the root's fields, the facets' fields and "+
			"the collections. Reading one record and getting less of it than the listing "+
			"gives for the same record is the shape nobody expects, and there is no "+
			"second request that would fill the gap: the document was already fetched.",
		"",
		responseAuthorityDoc)
	s.L("type %s struct {", op.ResponseType)
	s.L("\t%s", autoResponseEmbed)
	s.Blank()
	s.L("\tID string `json:\"id\" example:\"7b3c1f10-3c7e-4a8d-9f0e-9d2a8e6d4b51\"`")
	for _, f := range m.ResponseFields() {
		s.L("\t%s %s `%s`", f.Name, f.GoType, readFieldTag(f, false))
	}
	for _, f := range m.Read.Managed {
		s.L("\t%s %s `%s`", f.Name, f.GoType, readFieldTag(f, false))
	}
	for _, f := range m.ResponseJoinFields() {
		s.L("\t%s %s `%s`", f.Name, f.GoType, readFieldTag(f, false))
	}
	emitComputedResponseFields(s, m, false)
	for _, c := range m.Children {
		s.L("\t%s []%s `json:%s`", c.GoPlural, childWireRow(c), quote(c.Segment))
	}
	s.L("}")
	s.Blank()
	emitReadFromResult(s, op.ResponseType, m.Read.ResultByID)

	return goFile("internal/web/requests/find_"+m.Entity.Snake+"_by_id.go", fsplan.Owned,
		"the by-id request and response", s)
}

func emitListDTO(m *ir.Model) (fsplan.File, error) {
	op := m.Op("byParams")
	s := &src{}
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
		// `sort:` beside `filter:` when this leaf is in the ordering vocabulary.
		// The two tags answer different questions about the same path — which
		// operations it accepts, and which directions it may be ordered in — and
		// the framework reads them independently.
		s.L("\t%s *%s `query:%s filter:%s%s%s`", f.Field.Name, f.Field.BaseGoType,
			quote(f.Field.JSONName), quote(strings.Join(f.Ops, ",")),
			sortTagFor(m, f.Field.Name), descTagFor(f.Field))
	}
	// A path that is orderable and NOT filtered is a leaf of its own: it declares
	// the vocabulary, carries no value on the wire, and emits no query parameter
	// of its own. Ordering by a field nobody filters by is an ordinary ask, and
	// before v0.55.0 it came free from whatever the Response rendered.
	for _, f := range m.Read.Sortable {
		if filteredBy(m, f.Name) {
			continue
		}
		s.L("\t%s *%s `query:%s sort:%s%s`", f.Name, f.BaseGoType,
			quote(f.JSONName), quote(sortDirections), descTagFor(f))
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
	s.Doc(fmt.Sprintf("%s is one row of the listing.", op.ResponseType), "", responseAuthorityDoc)
	if pointered {
		s.Doc("",
			"Every field is a pointer with omitempty because this listing serves "+
				"?fields=: partial projection has to be able to leave a field out, and the "+
				"framework refuses to build the endpoint otherwise.")
	}
	s.L("type %s struct {", op.ResponseType)
	s.L("\t%s", autoResponseEmbed)
	s.Blank()
	if pointered {
		s.L("\tID *string `json:\"id,omitempty\" example:\"7b3c1f10-3c7e-4a8d-9f0e-9d2a8e6d4b51\"`")
	} else {
		s.L("\tID string `json:\"id\" example:\"7b3c1f10-3c7e-4a8d-9f0e-9d2a8e6d4b51\"`")
	}
	// The facet's fields and the collections belong here, not only on the by-id
	// read. The whole aggregate is already in hand when the listing is served,
	// so leaving them out discards data that was fetched anyway and pushes the
	// caller into one extra request per row to get it back.
	for _, f := range m.ResponseFields() {
		typ := f.GoType
		if pointered {
			typ = "*" + f.BaseGoType
		}
		s.L("\t%s %s `%s`", f.Name, typ, readFieldTag(f, pointered))
	}
	for _, f := range m.Read.Managed {
		typ := f.GoType
		if pointered {
			typ = "*" + f.BaseGoType
		}
		s.L("\t%s %s `%s`", f.Name, typ, readFieldTag(f, pointered))
	}
	// A LEFT join's field is already a pointer, and it stays one even where the
	// shape is not sparse: nil there means "no counterpart", which is a
	// different answer from the zero value and the one the framework insists on
	// preserving all the way to the wire.
	for _, f := range m.ResponseJoinFields() {
		typ := f.GoType
		if pointered {
			typ = "*" + f.BaseGoType
		}
		s.L("\t%s %s `%s`", f.Name, typ, readFieldTag(f, pointered))
	}
	emitComputedResponseFields(s, m, pointered)
	for _, c := range m.Children {
		s.L("\t%s []%s `json:%s`", c.GoPlural, childWireRow(c), quote(c.Segment+",omitempty"))
	}
	s.L("}")
	s.Blank()
	emitReadFromResult(s, op.ResponseType, m.Read.ResultList)

	return goFile("internal/web/requests/find_"+m.Entity.PluralSnake+"_by_params.go", fsplan.Owned,
		"the listing request and response", s)
}

// emitReadControls declares exactly the reserved controls the spec asked for.
//
// The vocabulary is closed and checked at boot: a key that is not one of the
// framework's controls panics when the wrapper is built, which is why these
// names are never improvised.
// sortDirections is what a declared path admits. Both directions, always: the
// spec names the PATHS that may be ordered by, and a listing that can be sorted
// newest-first can be sorted oldest-first for the same index. Narrowing to one
// direction is a real capability of the tag (`sort:"asc"`), kept in reserve for
// a key that asks for it rather than guessed at per field.
const sortDirections = "asc,desc"

// descTagFor renders the ` description:"…"` half of a query leaf's tag, which
// the framework turns into the parameter's description in the OpenAPI document.
//
// It is the ONE place a field's own prose reaches Swagger, and it reaches it
// where a caller cannot miss it: a query parameter's description renders inside
// the Parameters table, beside the input, with no tab to click. (The body half —
// a description on a `json:` field — is inert: the framework's body-schema
// walker reads path, query, json and example, and never description. Emitting
// one there would be a tag nothing reads.)
//
// A composite's PART carries the part's own description, which is what makes
// this worth doing at all: `resource` and `action` reach the wire as two
// unrelated-looking strings, and the sentence explaining each is already in the
// spec with nowhere to go.
//
// The sanitisation is not cosmetic. A struct tag is delimited by BACKTICKS, so a
// backtick inside the prose does not compile — and prose about an API is exactly
// where `code` spans get written. They become single quotes. Newlines go the
// same way: a tag is one line, and the description is a YAML block scalar that
// usually is not.
func descTagFor(f ir.Field) string {
	desc := strings.Join(strings.Fields(strings.ReplaceAll(f.Description, "`", "'")), " ")
	if desc == "" {
		return ""
	}
	return " description:" + quote(desc)
}

// sortTagFor renders the ` sort:"…"` half of a filter leaf's tag, or nothing
// when the path is not in the ordering vocabulary.
func sortTagFor(m *ir.Model, name string) string {
	for _, f := range m.Read.Sortable {
		if f.Name == name {
			return " sort:" + quote(sortDirections)
		}
	}
	return ""
}

// filteredBy reports whether a path already has a filter leaf, so the ordering
// vocabulary adds its tag there instead of declaring the field twice.
func filteredBy(m *ir.Model, name string) bool {
	for _, f := range m.Read.Filters {
		if f.Field.Name == name {
			return true
		}
	}
	return false
}

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
	s.L("\tview %s,", viewType(m))
	s.L("\td bootstrap.Deps,")
	s.L(") {")
	// The two HTTP surfaces are mounted independently, because they ARE
	// independent: the CRUD routes answer to surfaces.rest, the two export paths
	// to surfaces.exports, and a spec may ask for either one alone. What they
	// share is this function and the view behind it, so a project serving only
	// the spreadsheet still wires exactly what a full one does.
	switch {
	case !m.Surfaces.REST && !m.Surfaces.Exports():
		// Neither HTTP surface: this entity is reachable through GraphQL only.
		// The mount still exists and is still called, so the wiring, the
		// repository and the view stay identical — what changes is that no HTTP
		// route is registered, which is the whole of what the author asked for.
		s.L("\t// No HTTP routes: surfaces.rest is false and no export is declared, so")
		s.L("\t// %s is served through GraphQL alone. Everything else is wired", m.Entity.Pascal)
		s.L("\t// exactly as it would be.")
		s.L("}")
	default:
		if m.Surfaces.REST {
			s.L("\tgroup := app.Group(%s)", quote(m.Entity.Route))
		}
		// The exports read through the same handler the listing does, so they
		// need the view's name whether or not a REST route asked for it first.
		if (m.Surfaces.REST && m.Read.Enabled) || m.Surfaces.Exports() {
			s.L("\tviewName := view.Name()")
		}
		s.Blank()

		if m.Surfaces.REST {
			for _, op := range m.Ops {
				emitRoute(s, m, op, entity)
			}
			emitPerChildRoutes(s, m, entity)
		}
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
		s.L("\t\trequests.%s{}.FromResult,", op.ResponseType)
		s.L("\t\t%s)", listHandler(m))
	case op.Verb == "byId":
		s.L("\t%s, %s := fwweb.QueryByIDSpec(d.Pipeline,", hv, sv)
		s.L("\t\trequests.%s{},", op.RequestType)
		s.L("\t\trequests.%s{}.FromResult,", op.ResponseType)
		s.L("\t\t%s)", byIDHandler(m))
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
	writeDoc(s, "\t\t\t", routeDescription(m, op))
	s.L("\t\t\tTags: []string{%s},", quote(m.Entity.PluralPascal))
	s.L("\t\t},")
	s.L("\t\tfwopenapi.RequirePermission(%s))", quote(op.Permission))
	s.Blank()
}

// writeDoc emits the Doc.Description field, one Go string per PARAGRAPH joined
// with +, rather than one %q of the whole thing.
//
// The difference is only legibility, and it is worth a helper because the
// author's prose is markdown: a description with four paragraphs collapses into
// a single 900-column line whose \n\n escapes are the only clue that structure
// exists. Split, the generated route file shows the same paragraphs the spec
// shows, which is what a reviewer diffing the two needs.
//
// The escaping stays %q throughout, so a backtick, a quote or a tab in the
// prose is emitted correctly — the reason this can be a Go string literal at all
// while a struct tag cannot.
func writeDoc(s *src, indent, desc string) {
	paras := strings.Split(desc, "\n\n")
	if len(paras) == 1 {
		s.L("%sDescription: %s,", indent, quote(desc))
		return
	}
	s.L("%sDescription: %s +", indent, quote(paras[0]+"\n\n"))
	for i, p := range paras[1:] {
		if i == len(paras)-2 {
			s.L("%s\t%s,", indent, quote(p))
			continue
		}
		s.L("%s\t%s +", indent, quote(p+"\n\n"))
	}
}

// routeDescription writes documentation a reader actually benefits from: what
// the endpoint does and the one behaviour that surprises people — followed by
// whatever the SPEC's docs block says about this entity and this operation.
//
// The generator's sentence always comes first and is never replaceable. It
// states framework behaviour the author does not own — that PATCH cannot set a
// value back to null, that this service mounts no unarchive — and an entity able
// to overwrite it would be one where a caller quietly stops being told.
func routeDescription(m *ir.Model, op ir.Operation) string {
	own := verbDescription(m, op)
	if prose := m.Docs.For(op.Verb); prose != "" {
		return own + "\n\n" + prose
	}
	return own
}

// childRouteDescription is routeDescription's twin for a collection's own
// doors, which carry their sentence already written rather than deriving it
// from a verb.
func childRouteDescription(m *ir.Model, own string) string {
	if prose := strings.TrimSpace(m.Docs.Description); prose != "" {
		return own + "\n\n" + prose
	}
	return own
}

func verbDescription(m *ir.Model, op ir.Operation) string {
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
		// The second sentence describes the unarchive endpoint, so it is written
		// only when that endpoint is mounted. Documenting an undo that no route,
		// mutation or command implements is worse than saying nothing: Swagger is
		// where a caller checks whether the action can be taken back.
		d := fmt.Sprintf("Archives the %s: the row stays but is hidden from reads "+
			"unless they ask for archived rows.", e)
		if m.Op("unarchive") != nil {
			return d + " Reversible through unarchive."
		}
		return d + " This service mounts no unarchive: the row stays as history and " +
			"nothing brings it back into the active set."
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

// emitChildDTOs writes the wire shapes for the collections — ONE FILE PER
// COLLECTION, under requests/dtos.
//
// The three types here are read by every operation that carries the collection:
// the root's insert and update, both reads, and each of the entry's own verbs.
// That is what keeps them out of any single operation's file — the web layer
// files by operation, and a shape belonging to six of them belongs beside none.
//
// The request carries no id: on a create the server mints it, and accepting one
// from the caller would let them choose a key they have no business choosing.
// The response does carry it, because that is how the caller addresses the entry
// afterwards.
func emitChildDTOs(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File
	for _, c := range m.Children {
		if c.Mounted {
			// The entry's wire shape is the collection's, not this role's: it is
			// declared once, by the spec that owns the identity, and both roles
			// send and return the same JSON for the same row.
			continue
		}
		s := &src{}
		s.Blank()
		s.L("package dtos")
		s.Blank()
		s.L("import (")
		s.L("\t%s", quote("time"))
		s.L("\t%s", quote(fwImport("domain")))
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
			s.Doc(fmt.Sprintf("%sRow is one entry of the %s collection as a read returns it.",
				c.Name, c.Segment))
			s.L("type %sRow struct {", c.Name)
			if pointered {
				s.L("\tID *string `json:\"id,omitempty\"`")
			} else {
				s.L("\tID string `json:\"id\"`")
			}
			for _, f := range c.Fields {
				typ := f.GoType
				if pointered {
					typ = "*" + f.BaseGoType
				}
				// exportLabelKey rides the nested row too: a hierarchical export
				// gives every level its own columns, and each needs a header.
				s.L("\t%s %s `%s`", f.Name, typ, readFieldTag(f, pointered))
			}
			for _, f := range m.ResponseChildJoinFields(c) {
				typ := f.GoType
				if pointered {
					typ = "*" + f.BaseGoType
				}
				s.L("\t%s %s `%s`", f.Name, typ, readFieldTag(f, pointered))
			}
			// The entry's DERIVED fields, carrying the same `computed:` tag the
			// root's do. It is what earns them the whole contract at this level
			// too: ?fields=<segment>.<name> pushes the entry's SOURCES down
			// instead of a name no column has, and ?orderBy= over it is a typed
			// 400 rather than a query the store cannot answer.
			emitComputedFieldTags(s, c.Computed, pointered)
			s.L("}")
			s.Blank()
		}

		s.Doc(fmt.Sprintf("%sRequest is one entry sent in the %s collection.", c.Name, c.Segment),
			"",
			"It carries no id: the server mints one.",
			"",
			"It declares no mapper of its own. This type is never the top of a "+
				"request — it is reached as an element of the root's collection or "+
				"through the per-entry verbs' embed — and the generic Request→Command "+
				"mapping recurses into it by field name, so the entry travels without a "+
				"seat here. The marker rides the type at the TOP of each walk.")
		s.L("type %sRequest struct {", c.Name)
		for _, f := range c.Fields {
			s.L("\t%s %s `json:%s example:%s`", f.Name, f.GoType,
				quote(jsonTag(f, false)), quote(f.Example))
		}
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

		f, err := goFile(webDTOPkg+"/"+naming.Snake(c.Name)+".go", fsplan.Owned,
			fmt.Sprintf("the wire shapes of one %s entry", c.Name), s)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// emitPerChildRoutes mounts the verbs that address ONE entry — up to three of
// them, and exactly the ones children[].operations names.
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
		if !c.PerChild || !c.OnREST {
			// Not on this surface: children[].surfaces took the collection off
			// REST, or the entity serves none. Its commands and wire types are
			// still generated — the verbs answer on GraphQL instead.
			continue
		}
		opName := c.OpBase // qualified when the collection is mounted from a shared identity
		// The Go type string is for the CODE positions below; the OpenAPI
		// summaries are read by an API consumer, who got "a appdomain.Person".
		human := m.Entity.Pascal

		// Which of the three the collection mounts is the spec's decision
		// (children[].operations), and a verb it does not mount leaves NO trace:
		// no route, no OpenAPI entry, no permission line. A collection whose only
		// field is its identity has no change to offer, and mounting one anyway
		// would publish an endpoint whose single effect is to turn one entry into
		// another while keeping the first one's row id.
		var ops []perChildOp
		if c.MountsAdd {
			ops = append(ops, perChildOp{
				verb: "Add", permKey: "add", method: fiberMethod(c, "add"), path: pathOf(c, "add"),
				request: "Add" + opName + "Request", response: "Add" + opName + "Response",
				result: "commands.Add" + opName + "Result", status: "fiber.StatusCreated",
				summary: fmt.Sprintf("Add one %s to %s %s", c.Name, articleFor(human), human),
				doc: fmt.Sprintf("Adds ONE entry to the %s collection of an existing %s, "+
					"in the owner's transaction. 404 when the owner is not there. The "+
					"response carries the entry AS STORED, including the id the server "+
					"minted for it — that id is how the caller addresses it afterwards.",
					c.Segment, human),
			})
		}
		if c.ChangesByPut() {
			ops = append(ops, perChildOp{
				verb: "Change", permKey: "change", method: fiberMethod(c, "change"), path: pathOf(c, "change"),
				request: "Change" + opName + "Request", response: "Change" + opName + "Response",
				result: "commands.Change" + opName + "Result", status: "fiber.StatusOK",
				summary: fmt.Sprintf("Replace one %s of %s %s", c.Name, articleFor(human), human),
				doc: fmt.Sprintf("Full replacement of ONE entry, keeping its id — the row " +
					"is updated rather than removed and re-added, so the audit trail reads " +
					"as a change. 404 when the owner is not there, and 404 when the owner " +
					"exists but holds no entry with that id."),
			})
		}
		// The same operation in its partial shape, and a route of its own, exactly
		// as update.shape: both gives the root a PUT and a PATCH over /:id. The
		// permission is the change's — one verb asked twice is not two jobs.
		if c.ChangesByPatch() {
			ops = append(ops, perChildOp{
				verb: "Patch", permKey: "patch", method: fiberMethod(c, "patch"), path: pathOf(c, "patch"),
				request: "Patch" + opName + "Request", response: "Patch" + opName + "Response",
				result: "commands.Patch" + opName + "Result", status: "fiber.StatusOK",
				handler: "handlers.PartialUpdateCommandHandler",
				summary: fmt.Sprintf("Update one %s of %s %s (partial)", c.Name, articleFor(human), human),
				doc: "Partial change of ONE entry, keeping its id. Only the fields present " +
					"in the body change; the rest keep what the entry already holds, which " +
					"is why the business identity is not among them — it comes from the " +
					"stored entry and cannot be moved here. Because an absent field and an " +
					"explicit null cannot be told apart, this verb cannot set a value back " +
					"to null. 404 when the owner is not there, and 404 when the owner " +
					"exists but holds no entry with that id.",
			})
		}
		if c.MountsRemove {
			ops = append(ops, removeOp(c, human, opName))
		}

		for _, op := range ops {
			hv := "h" + op.verb + opName
			sv := "s" + op.verb + opName
			s.L("\t%s, %s := fwweb.CommandWithBodyIDSpec(d.Pipeline,", hv, sv)
			s.L("\t\trequests.%s{},", op.request)
			// A bodyless verb has no Response type to project through: it answers
			// 204, and the framework's own NoBody is what renders the envelope
			// without a "data" field. Naming a projection here would emit one.
			if op.bodyless {
				s.L("\t\tfwresponses.NoBody,")
			} else {
				s.L("\t\trequests.%s{}.FromResult,", op.response)
			}
			s.L("\t\t&%s[*%s, *commands.%s, %s]{",
				op.handlerType(), entity, op.verb+opName+"Command", op.result)
			s.L("\t\t\tRepo: repo,%s", serviceField(m))
			s.L("\t\t}, %s)", op.status)
			s.L("\tfwopenapi.Mount(d.OpenAPIRegistry, group, %s, %s,", op.method, quote(op.path))
			s.L("\t\t%s, %s,", hv, sv)
			s.L("\t\tfwopenapi.Doc{")
			s.L("\t\t\tSummary: %s,", quote(op.summary))
			// An entry route is an operation of this entity, so the entity-wide
			// prose reaches it too. The per-operation map deliberately does not:
			// its keys name the ROOT's verbs, and a collection's doors are the
			// child's own business — children[] is where that would be declared.
			writeDoc(s, "\t\t\t", childRouteDescription(m, op.doc))
			s.L("\t\t\tTags: []string{%s},", quote(m.Entity.PluralPascal))
			s.L("\t\t},")
			// Per VERB, not per collection: the entry verbs inherit the root's
			// update permission unless children[].permissions says otherwise, and
			// a collection may gate only the one that widens privilege. The IR
			// resolved both cases into one map, so this position never repeats
			// the fallback and never disagrees with the report.
			s.L("\t\tfwopenapi.RequirePermission(%s))",
				quote(c.Permissions[op.permKey]))
			s.Blank()
		}
	}
}

// removeOp picks the VERB that tells the truth about what removal does here.
//
// A child that declares softRemove keeps its row and its archive stamp, so the
// entry stops being returned instead of being purged. Mounting that as DELETE
// would promise an irreversible purge the endpoint does not perform — and
// DELETE is the one verb a caller is entitled to read as permanent. A child
// WITHOUT an archive column really is deleted, so there DELETE is the honest
// spelling.
//
// What the two do NOT differ in is what the caller can do next. There is no
// per-entry unarchive: children[].operations is closed at add|change|remove,
// unarchive is a ROOT mode, and the loader gates archived rows out of the
// aggregate, so no command can address an archived entry to bring it back. The
// archive branch therefore says the row is kept, and says plainly that the way
// back is a fresh add with a NEW id — the same discipline the root applies at
// ir.byIDWriteOp's caller, where "(reversible)" is only claimed when the
// unarchive endpoint is actually mounted.
//
// Both branches are BODYLESS. The entry is named by the path, it is gone when
// the verb answers, and the only thing left to put in a body is the owner id
// the caller itself sent — so they answer 204, exactly like the root's own
// archive/delete. add and change are the opposite case and keep their 200/201:
// they answer with the entry AS STORED, which is how the caller learns the id
// the server minted.
func removeOp(c ir.Child, entity, opName string) perChildOp {
	op := perChildOp{
		verb: c.RemoveVerbPascal(), permKey: "remove", method: fiberMethod(c, "remove"), path: pathOf(c, "remove"),
		request:  c.RemoveVerbPascal() + opName + "Request",
		result:   "fwresults.None",
		status:   "fiber.StatusNoContent",
		bodyless: true,
	}
	// The ROUTE already knows which of the two this is — PerEntryRoute reads the
	// same archive column — so what is decided here is the WORDING, and only the
	// wording. Two copies of the method and the path were how the summary and
	// the endpoint drifted apart in the first place.
	if c.ArchivedAt != "" {
		op.summary = fmt.Sprintf("Archive one %s of %s %s", c.Name, articleFor(entity), entity)
		op.doc = "Archives ONE entry: the row stays, stamped, and stops being " +
			"returned — which is why this is not a DELETE. There is no per-entry " +
			"unarchive: an entry taken out this way does not come back, and adding " +
			"the same value again mints a NEW entry with a new id. Answers 204 with " +
			"no body. 404 when the owner is not there, and 404 when it holds no " +
			"entry with that id."
		return op
	}
	op.summary = fmt.Sprintf("Remove one %s from %s %s", c.Name, articleFor(entity), entity)
	op.doc = "Removes ONE entry for good: this child declares no archive column, " +
		"so there is nothing to bring back. Answers 204 with no body. 404 when the " +
		"owner is not there, and 404 when it holds no entry with that id."
	return op
}

// fiberMethod and pathOf spell ONE decision — ir.Child.PerEntryRoute — in the
// two forms this file needs it: the fiber constant the mount call takes, and the
// path relative to the entity's group. The report prints the same answer, so a
// route that moves moves in both places or in neither.
func fiberMethod(c ir.Child, verb string) string {
	method, _ := c.PerEntryRoute(verb)
	return "fiber.Method" + method[:1] + strings.ToLower(method[1:])
}

func pathOf(c ir.Child, verb string) string {
	_, path := c.PerEntryRoute(verb)
	return path
}

// perChildOp is one mounted per-entry verb, fully decided.
//
// result is the Go type the handler projects — QUALIFIED, because a bodyless
// verb's is the framework's fwresults.None and the others' live in the service's
// own commands package. response is empty exactly when bodyless is set: there is
// no wire type to name.
type perChildOp struct {
	verb, method, path, request, response, result, summary, doc string
	status                                                      string
	bodyless                                                    bool
	// permKey is the SPEC's word for this operation, which is what
	// children[].permissions is keyed by. It parts company with verb on the
	// removal alone: the generated names say archive or delete, because that is
	// what the route does, while the spec keeps one word for the operation and
	// lets softRemove decide the outcome. Lower-casing verb would look for a
	// permission called "archive" that no spec declares.
	permKey string
	// handler is the framework handler this verb runs through, and it is empty
	// for all but one of them. Every per-entry verb is an UPDATE of the owner, so
	// the full-body handler is the answer everywhere except the PARTIAL change:
	// UpdateCommandHandler embeds pipeline.FullBody, which makes the wrapper
	// require every exported field of the command in the body — the exact demand
	// a partial verb exists to drop.
	handler string
}

func (op perChildOp) handlerType() string {
	if op.handler != "" {
		return op.handler
	}
	return "handlers.UpdateCommandHandler"
}
