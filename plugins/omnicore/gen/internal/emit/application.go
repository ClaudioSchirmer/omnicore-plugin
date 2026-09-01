package emit

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
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
		files, err := emitChildResults(m)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
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
		// The derivations behind computed fields live on the read side and are
		// called from BOTH, so a write response renders the same value a read
		// does. The import is pruned when this entity computes nothing.
		s.L("\tappqueries %s", quote(m.ImportPath("internal/application/queries")))
		// The shapes and projectors EVERY verb answering with a collection needs.
		// Both lines are pruned for an entity with no children.
		s.L("\t%s %s", cmdDTOAlias, quote(m.ImportPath(cmdDTOPkg)))
		s.L("\t%s %s", cmdUtilAlias, quote(m.ImportPath(cmdUtilPkg)))
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
	// A bodyless verb still has a caller, and under a scoped dataAccess that is
	// the whole question: archiving a row is a WRITE to it, and the row was
	// loaded through the repository, which the read side's filter never touches.
	// This hook being a flat no-op is what let a caller archive another tenant's
	// row with the ordinary archive permission — and not be able to read back
	// what they had just archived.
	if len(m.ClaimRuntimeFields()) > 0 {
		s.Doc("ApplyTo carries the caller's identity onto the entity, so BuildRules can " +
			"refuse a write to a row outside the caller's scope. The verb has no body: " +
			"this is the only thing it applies.")
		s.L("func (c *%s) ApplyTo(ctx *configuration.AppContext, e *%s) error {", op.CommandType, entity)
		emitIdentityFeed(s, m)
		s.L("\treturn nil")
		s.L("}")
		s.Blank()
	} else {
		s.Doc("ApplyTo is the hook where identity-derived state would reach the entity. " +
			"This verb needs none, so it is a no-op.")
		s.L("func (c *%s) ApplyTo(_ *configuration.AppContext, _ *%s) error {", op.CommandType, entity)
		s.L("\treturn nil")
		s.L("}")
		s.Blank()
	}
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
	for _, f := range commandFields(m, op) {
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

// commandFields are the fields a given verb accepts.
//
// A partial update drops what the spec put off-limits, so the excluded field is
// ABSENT from the type rather than merely ignored: a reader of the DTO sees the
// truth, and a caller who sends it gets told, instead of having it quietly
// dropped or quietly applied. A body-sourced runtime field is narrowed the same
// way and for the same reason — it is on the verbs its `modes` name and on no
// others, so a caller reading the type sees where the value is accepted.
//
// A sibling facet is not a separate input: its fields are more fields of the
// owner, and the row is materialised only when at least one carries a value.
func commandFields(m *ir.Model, op ir.Operation) []ir.Field {
	return m.CommandFields(op.Verb)
}

func commandFieldType(f ir.Field, partial bool) string {
	// WireOptional is the same POINTER for a different reason: not "the caller
	// may leave it unchanged" but "the caller may leave it to the server". Both
	// need the nil, so both take the pointer, and a nullable field already has
	// one.
	if (partial || f.WireOptional) && !f.Nullable {
		return "*" + f.BaseGoType
	}
	return f.GoType
}

func emitToEntity(s *src, m *ir.Model, op ir.Operation, entity string) {
	s.Doc("ToEntity builds the aggregate the framework will validate and persist.")
	s.L("func (c *%s) ToEntity(ctx *configuration.AppContext) (*%s, error) {", op.CommandType, entity)
	s.L("\te := &%s{}", entity)
	emitFieldAssignments(s, ir.Mappable(commandFields(m, op)), "\t", "e", "c")
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
	emitFieldAssignments(s, ir.Mappable(commandFields(m, op)), "\t", "e", "c")
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
	plain, groups := ir.PlainAndComposites(commandFields(m, op))
	for _, f := range plain {
		s.L("\tif c.%s != nil {", f.Name)
		if f.Nullable {
			s.L("\t\te.%s = %s", f.Name, entityValue(f, "c."+f.Name))
		} else {
			s.L("\t\te.%s = %s", f.Name, entityValue(f, "*c."+f.Name))
		}
		s.L("\t}")
	}
	for _, g := range groups {
		emitCompositePatch(s, g, "\t", "e."+g.Owner(), "c")
	}
	emitIdentityFeed(s, m)
	s.L("\treturn nil")
	s.L("}")
	s.Blank()
}

// emitAssignedFields writes the persisted fields the SERVER reads from the
// caller's identity.
//
// It runs on insert only. The value is the caller's, not the client's: the
// field is absent from the request, so there is nothing to ignore, and a later
// update leaves it alone because it is absent from that mapper too — which is
// what makes "who created this row" a fact rather than a claim.
//
// A `derived` field is server-assigned too and is deliberately NOT here: what
// computes it is a rule in the entity's own hook file, so there is no
// assignment for this mapper to write.
func emitAssignedFields(s *src, m *ir.Model) {
	emitClientIPFields(s, m)

	assigned := m.IdentityAssignedFields()
	if len(assigned) == 0 {
		// No stated-scope override either: the field the guard compares is the
		// one an identity fills, so an entity with none has no scope to state.
		return
	}
	s.Blank()
	s.L("\t// Filled from the caller's identity, never from the request: these fields")
	s.L("\t// are not part of any write DTO. Only an insert sets them.")
	s.L("\tif id := ctx.Identity(); id != nil {")
	for _, f := range assigned {
		if f.AssignedFrom == "identity-subject" {
			s.L("\t\te.%s = %s", f.Name, identityValue(f, "id.Subject"))
			continue
		}
		s.L("\t\tif raw, ok := id.Claims[%s].(string); ok {", quote(f.Claim))
		s.L("\t\t\te.%s = %s", f.Name, identityValue(f, "raw"))
		s.L("\t\t}")
	}
	s.L("\t}")
	emitStatedScope(s, m)
}

// emitClientIPFields records WHERE the write came from.
//
// Outside the identity check, and that is the whole difference from the block
// below: the origin is resolved by the framework's HTTP middleware, not carried
// in a token, so it exists for an anonymous route and on a bench with
// authentication switched off. Reading it inside `if id := ctx.Identity()`
// would silently drop it on exactly those requests.
//
// `ClientIP()` answers "" off the inbound request path — a consumer handler, a
// background job, a test fixture. The column says which of the two shapes the
// author chose for that: a nullable field records the absence as NULL and is
// left untouched, a plain one records it as the empty string.
func emitClientIPFields(s *src, m *ir.Model) {
	fields := m.ClientIPAssignedFields()
	if len(fields) == 0 {
		return
	}
	s.Blank()
	s.L("\t// The request's network origin, as the framework resolved it. Not from the")
	s.L("\t// token and not from the body: no write DTO carries it, and an anonymous")
	s.L("\t// caller has one just the same. Only an insert sets it — it records where")
	s.L("\t// the row CAME FROM, not where the last edit came from.")
	s.L("\t//")
	s.L("\t// Behind a reverse proxy this is only as good as http.trustProxy says it")
	s.L("\t// is: undeclared, the framework reads the socket peer, which names the")
	s.L("\t// balancer on every request.")
	for _, f := range fields {
		if f.Nullable {
			s.L("\tif ip := ctx.ClientIP(); ip != \"\" {")
			s.L("\t\te.%s = &ip", f.Name)
			s.L("\t}")
			continue
		}
		s.L("\te.%s = ctx.ClientIP()", f.Name)
	}
}

// emitStatedScope lets the caller's OWN word override the line above, on the
// one field the row-scope guard compares.
//
// Three things about this deserve to be read slowly, because each one looks
// like a bug and is the point.
//
// It runs OUTSIDE the identity check: the value came from the request, not from
// the token, so an absent identity is no reason to drop it — and on a bench with
// authentication off it is the only value there is.
//
// It does not ask whether the caller MAY state it. Writing the value onto the
// entity unconditionally is what hands the question to `refuseForeign…`, which
// already compares this exact field against the caller's own scope and already
// stands down for the bypass. Asking here instead would mean deciding, in a
// mapper whose only failure channel is an error, something the domain answers
// with a notification — and the caller would get a 500, or worse, a silent 201
// with the record filed under the wrong scope.
//
// And it is emitted for the INSERT alone, because that is the only verb whose
// mapper calls it: a record does not change scope by being updated.
func emitStatedScope(s *src, m *ir.Model) {
	f := m.BypassSettableField()
	if f == nil {
		return
	}
	what := "tenant"
	if m.Authz.DataAccess == "owner-only" {
		what = "owner"
	}
	s.Blank()
	for _, line := range wrap(fmt.Sprintf("…unless the caller stated the %s themselves. "+
		"Absent means \"mine\", which the line above already wrote. Present, it is applied "+
		"HERE and judged in BuildRules: a caller who may not cross the row scope meets "+
		"the same refusal a write into a foreign %s meets, instead of having the value "+
		"quietly replaced by their own.", what, what), 70) {
		s.L("\t// %s", line)
	}
	s.L("\tif c.%s != nil {", f.Name)
	s.L("\t\te.%s = %s", f.Name, entityValue(*f, "*c."+f.Name))
	s.L("\t}")
}

// superAdminTest is the expression that answers "is the caller a super-admin",
// over an *Identity the generated code has already checked for nil. Which
// framework method that is, and why it is not HasPermission, is
// ir.SuperAdminMethod.
func superAdminTest(recv string) string {
	return recv + "." + ir.SuperAdminMethod + "()"
}

// identityParam names the AppContext parameter of a write mapper.
//
// A mapper that carries nothing from the request onto the entity leaves it `_`,
// which says so at the signature. One that does has to name it — and the set of
// verbs that do is wider than the root's own: a per-entry child verb and the
// facet-clearing mutation both write the ROOT, under ModeUpdate, so a row
// scope's guard runs for them exactly as it runs for a patch. Leaving the
// context unnamed there generated the guard and then never fed it, and the
// guard stands down on an absent identity — so a caller holding nothing but the
// update permission wrote into another tenant's aggregate one entry at a time,
// with a green build and a green generated suite.
func identityParam(m *ir.Model) string {
	// The CLAIM-fed fields only: a body-sourced runtime field is read off the
	// command, not off the context, so a mapper that carries nothing else still
	// has no identity to name.
	if len(m.ClaimRuntimeFields()) == 0 {
		return "_"
	}
	return "ctx"
}

// emitIdentityFeed populates the runtime-only fields the rules read.
//
// This is the one place below the web layer that touches the request identity:
// the command feeds it onto the entity, and BuildRules enforces with it.
func emitIdentityFeed(s *src, m *ir.Model) {
	runtime := m.ClaimRuntimeFields()
	if len(runtime) == 0 {
		return
	}
	s.Blank()
	s.L("\t// Identity-derived state the rules read. It is never persisted.")
	if m.HasNamedClaimFields() {
		s.L("\t//")
		s.L("\t// The framework does not opine on which custom claims a token carries, so")
		s.L("\t// the claim name comes from the spec rather than from a convention.")
	}
	s.L("\tif id := ctx.Identity(); id != nil {")
	for _, f := range runtime {
		// The identity SOURCES do not come from a claim looked up by name — the
		// tenant is whichever claim the framework is configured to read, the
		// subject is the subject, and a permission is a question the permission
		// model answers. Reading any of them through Claims[...] would hardcode a
		// name the deployment is free to change, and for a permission it would
		// answer a narrower question than the service is gated by: a raw claim
		// resolves no resource wildcard and honours no *:* grant.
		//
		// Whether the field was declared in the spec or synthesised for the row
		// scope makes no difference here, and deliberately so.
		switch f.IdentitySource {
		case "tenant":
			s.L("\t\te.%s = id.TenantID()", f.Name)
			continue
		case "subject":
			s.L("\t\te.%s = id.Subject", f.Name)
			continue
		case "permission":
			s.L("\t\te.%s = id.HasPermission(%s)", f.Name, quote(f.Permission))
			continue
		case "super-admin":
			s.L("\t\t// The super-admin grant, not asked through HasPermission: that")
			s.L("\t\t// method panics on a wildcard, since the CLAIM wildcards and the")
			s.L("\t\t// question does not. The framework gives the wildcard its own")
			s.L("\t\t// question, and this is it.")
			s.L("\t\te.%s = %s", f.Name, superAdminTest("id"))
			continue
		case "present":
			// Assigned inside the nil check, which is the whole point: reaching
			// this line IS the fact being recorded.
			s.L("\t\te.%s = true", f.Name)
			continue
		}
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
	// The computed fields a write CAN answer. A read derives them from the
	// projected document; here the entity is in hand, which is strictly more —
	// the exception is a derivation reading a framework-stamped column, which the
	// entity does not carry, and those stay off the write shapes.
	// The runtime values this verb hands over. No column backs them and no read
	// will ever render them again: the row keeps a hash, and this response is the
	// one place the minted value exists on the wire.
	rendered := m.RenderedRuntimeFields(ir.GateModeOf(op.Verb))
	for _, f := range rendered {
		s.L("\t// %s is RUNTIME: no column holds it, and FromEntity reads it off the", f.Name)
		s.L("\t// entity after the write — whatever the rules minted there.")
		s.L("\t%s %s", f.Name, f.GoType)
	}
	writeComputed := writeComputedFields(m)
	for _, c := range writeComputed {
		s.L("\t// %s is COMPUTED: no column backs it, and FromEntity fills it", c.Name)
		s.L("\t// from %s.", strings.Join(c.Sources, "+"))
		s.L("\t%s %s", c.Name, c.GoType)
	}
	for _, c := range m.Children {
		s.L("\t%s []%s", c.GoPlural, childResultType(c))
	}
	s.L("}")

	s.Blank()

	doc := []string{
		"FromEntity projects the aggregate AFTER it was validated and written.",
		"",
		"It reads the entity, never the command: the domain may have normalised or " +
			"defaulted a value, and echoing the input back would hide that from the caller.",
	}
	if len(rendered) > 0 {
		doc = append(doc, "",
			"That is also what makes the runtime fields here possible: nothing persisted "+
				"them, so the entity in hand is the only place they exist. Whatever the "+
				"rules put there is what the caller receives, once.")
	}
	ctxParam := "_"
	if len(writeComputed) > 0 {
		ctxParam = "ctx"
		doc = append(doc, "",
			"The computed fields are derived through the SAME function the reads call, "+
				"in internal/application/queries. Deriving them a second time here is how "+
				"one question grows two answers.")
	}
	s.Doc(doc...)
	s.L("func (c *%s) FromEntity(%s *configuration.AppContext, e *%s) (%s, error) {",
		op.CommandType, ctxParam, entity, op.ResultType)
	// With no composite in play the projection is ONE expression, which is how
	// this has always read. A composite needs statements — an optional one is a
	// nil check — so the literal is bound to a local and filled in after.
	plain, groups := ir.PlainAndComposites(m.AllOwnerFields())
	head, tail := "\treturn "+op.ResultType+"{", "\t}, nil"
	if len(groups) > 0 || len(writeComputed) > 0 {
		head, tail = "\tout := "+op.ResultType+"{", "\t}"
	}
	s.L("%s", head)
	s.L("\t\tID: *e.GetID(),")
	for _, f := range plain {
		s.L("\t\t%s: %s,", f.Name, wireValue(f, "e"))
	}
	for _, f := range rendered {
		s.L("\t\t%s: %s,", f.Name, wireValue(f, "e"))
	}
	for _, c := range m.Children {
		s.L("\t\t%s: %s,", c.GoPlural, childProjector(c)+"(e)")
	}
	s.L("%s", tail)
	if len(groups) > 0 || len(writeComputed) > 0 {
		for _, g := range groups {
			emitCompositeUnfold(s, g, "\t", "out", "e")
		}
		emitWriteComputedCalls(s, m, writeComputed, "out", "e")
		s.L("\treturn out, nil")
	}
	s.L("}")
}

// writeComputedFields are the computed read fields a WRITE response can carry.
//
// A read derives from the projected document; a write has the entity, which
// holds strictly more — except for the framework-stamped columns, which are
// stamped by the engine and are on no entity. A derivation that reads one of
// those is served by the reads alone, and the write shapes leave it out rather
// than rendering it empty.
func writeComputedFields(m *ir.Model) []ir.ComputedField {
	var out []ir.ComputedField
	for _, c := range m.Read.Computed {
		ok := true
		for _, src := range c.Sources {
			if _, found := entitySourceField(m, src); !found {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, c)
		}
	}
	return out
}

// entitySourceField resolves a derivation source against the fields the ENTITY
// carries — a composite's part included, since the aggregate holds the value
// object it belongs to.
func entitySourceField(m *ir.Model, name string) (ir.Field, bool) {
	for _, f := range m.AllOwnerFields() {
		if f.Name == name {
			return f, true
		}
	}
	return ir.Field{}, false
}

// emitWriteComputedCalls fills the write Result's computed fields by calling the
// SAME derivation the reads call, reading each source off the entity.
func emitWriteComputedCalls(s *src, m *ir.Model, computed []ir.ComputedField, target, recv string) {
	for _, c := range computed {
		var args []string
		for _, src := range c.Sources {
			// Every source resolves: writeComputedFields already dropped any
			// derivation with one that does not, precisely so the argument list
			// here lines up with the signature the hook declares. A skip would
			// silently call it with one argument fewer, which does not compile —
			// and that is the good failure, not one to code around.
			f, _ := entitySourceField(m, src)
			args = append(args, factArgValue(f, recv))
		}
		s.L("\t{")
		s.L("\t\tv, err := appqueries.%s(ctx, %s)", computedFuncName(m.Entity, c), strings.Join(args, ", "))
		s.L("\t\tif err != nil {")
		s.L("\t\t\treturn %s, err", target)
		s.L("\t\t}")
		s.L("\t\t%s.%s = v", target, c.Name)
		s.L("\t}")
	}
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

// emitChildProjector reads ONE collection back OUT of the framework's own.
//
// It is read through the framework rather than from a struct field because that
// collection is the only place the entries exist — and it is also where the
// persister has just written the minted ids back.
func emitChildProjector(s *src, m *ir.Model, c ir.Child, entity string) {
	result := childResultType(c)
	s.Doc(fmt.Sprintf("%s reads %s's %s back off the saved aggregate.",
		c.Projector, m.Entity.Pascal, c.Segment),
		"",
		"Every verb that answers with the whole aggregate calls this one, which is "+
			"why it is here and not in any of their files.")
	s.L("func %s(e *%s) []%s {", c.Projector, entity, result)
	s.L("\titems := domain.GetCurrentItemsOf[aggregatevos.%s](&e.AggregateRoot)", c.Name)
	s.L("\tout := make([]%s, 0, len(items))", result)
	s.L("\tfor _, item := range items {")
	plain, groups := ir.PlainAndComposites(c.Fields)
	head, tail := "\t\tout = append(out, "+result+"{", "\t\t})"
	if len(groups) > 0 {
		head, tail = "\t\tentry := "+result+"{", "\t\t}"
	}
	s.L("%s", head)
	s.L("\t\t\tID: item.GetID(),")
	for _, f := range plain {
		s.L("\t\t\t%s: %s,", f.Name, wireValue(f, "item"))
	}
	s.L("%s", tail)
	if len(groups) > 0 {
		for _, g := range groups {
			emitCompositeUnfold(s, g, "\t\t", "entry", "item")
		}
		s.L("\t\tout = append(out, entry)")
	}
	s.L("\t}")
	s.L("\treturn out")
	s.L("}")
}

// emitChildResults writes what every verb answering with a collection needs:
// the entry's write-side shape, and the projector that fills it.
//
// Both are shared — the root's insert, its update and each of the entry's own
// verbs read the same ones — so neither can sit beside a Command without making
// that Command's file the place a reader has to already know about. They split
// by KIND: the shape is a structure and goes to commands/dtos, the projector is
// a function several files call and goes to commands/utils. One file per
// collection in each, named for the collection.
func emitChildResults(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File
	entity := "appdomain." + m.Entity.Pascal
	for _, c := range m.Children {
		if !c.Mounted {
			// A MOUNTED collection's shape is declared, with exactly this shape,
			// by the role that owns the shared identity. The PROJECTOR below is
			// not: it takes THIS owner's type.
			f, err := emitChildResultDTO(m, c)
			if err != nil {
				return nil, err
			}
			out = append(out, f)
		}

		s := &src{}
		s.Blank()
		s.L("package utils")
		s.Blank()
		s.L("import (")
		s.L("\t%s", quote("time"))
		s.L("\t%s", quote(fwImport("domain")))
		s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
		s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
		s.L("\t%s %s", cmdDTOAlias, quote(m.ImportPath(cmdDTOPkg)))
		s.L(")")
		s.Blank()
		emitChildProjector(s, m, c, entity)
		if wantsEntryProjector(c) {
			s.Blank()
			emitChildEntryProjector(s, c)
		}

		// Qualified by the OWNER, not by the collection alone: the projector
		// takes this entity's type, so two roles over one shared identity write
		// two different functions for the same collection — and an unqualified
		// path would have the second one overwrite the first.
		f, err := goFile(cmdUtilPkg+"/"+m.Entity.Snake+"_"+naming.Snake(c.Name)+"_projection.go",
			fsplan.Owned,
			fmt.Sprintf("the projectors for the %s collection", c.Segment), s)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// emitChildResultDTO writes one collection's write-side shape.
func emitChildResultDTO(m *ir.Model, c ir.Child) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package dtos")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote(fwImport("domain")))
	s.L(")")
	s.Blank()

	s.Doc(fmt.Sprintf("%s mirrors one persisted %s.", childResultTypeName(c), c.Name),
		"",
		"The id is included because the persister writes the minted ids back before "+
			"this projection runs, and the caller needs them to address the entry later.")
	s.L("type %s struct {", childResultTypeName(c))
	s.L("\tID domain.ID")
	for _, f := range c.Fields {
		s.L("\t%s %s", f.Name, f.GoType)
	}
	s.L("}")

	return goFile(cmdDTOPkg+"/"+naming.Snake(c.Name)+"_result.go", fsplan.Owned,
		fmt.Sprintf("the write shape of one %s entry", c.Name), s)
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
	if !m.Authz.Scoped() {
		return
	}
	field := m.Authz.ScopeSubject()
	if field == nil {
		// Validation refuses a runtime or missing scope field, so an empty one
		// here is generator inconsistency — and returning quietly would ship a
		// service that says owner-only and serves everything. Refuse loudly.
		panic(m.Authz.DataAccess + " with no scope field: validation should have refused this spec")
	}
	whose, from := "their tenant's", "id.TenantID()"
	if m.Authz.DataAccess == "owner-only" {
		whose, from = "their own", "id.Subject"
	}

	s.L("\t// Callers see only %s rows. Filter is a map keyed by the Go field", whose)
	s.L("\t// path, and the scope is FORCED: a value the caller sent for this field is")
	s.L("\t// overwritten, never merged.")
	s.L("\tif %s.Filter == nil {", target)
	s.L("\t\t%s.Filter = map[string]any{}", target)
	s.L("\t}")
	s.L("\tif id := ctx.Identity(); id != nil {")
	switch {
	case m.Authz.BypassWildcard:
		// The same exception, asked of the wildcard itself. It cannot be handed
		// to HasPermission — that panics — so it is asked with the framework's
		// own question for it. See ir.SuperAdminMethod.
		s.L("\t\t// A super-admin crosses the scope: the operator supporting a customer")
		s.L("\t\t// reads across tenants. Not asked through HasPermission — the framework")
		s.L("\t\t// panics when a wildcard is the QUESTION, so the %s a super-admin", m.Authz.Bypass)
		s.L("\t\t// carries has a question of its own, and this is it.")
		s.L("\t\tif !%s {", superAdminTest("id"))
		s.L("\t\t\t%s.Filter[%s] = %s", target, quote(field.Name), from)
		s.L("\t\t}")
	case m.Authz.Bypass != "":
		// Without this, a platform operator holding every permission there is
		// was still filtered to their own tenant, so supporting a customer
		// through the API was impossible. The permission is named CONCRETELY
		// here on purpose: a wildcard is not a legal argument to HasPermission,
		// and the policy "a super-admin crosses" is the other case above.
		s.L("\t\t// %s crosses the scope: the operator supporting a customer", m.Authz.Bypass)
		s.L("\t\t// reads across tenants. Asked as a CONCRETE permission — the framework")
		s.L("\t\t// panics on a wildcard here, since the claim wildcards and the question")
		s.L("\t\t// does not.")
		s.L("\t\tif !id.HasPermission(%s) {", quote(m.Authz.Bypass))
		s.L("\t\t\t%s.Filter[%s] = %s", target, quote(field.Name), from)
		s.L("\t\t}")
	default:
		s.L("\t\t%s.Filter[%s] = %s", target, quote(field.Name), from)
	}
	s.L("\t} else {")
	if m.Authz.NoIdentity == "stand-down" {
		// authz.noIdentity: stand-down. Only a dev bench reaches this branch —
		// the middleware is bypassable solely with auth.mode disabled, which the
		// framework refuses outside APP_PROFILE=dev — and the default is the
		// other way round precisely because this one serves everything.
		s.L("\t\t// No identity at all: the scope stands down, as authz.noIdentity says.")
		s.L("\t\t// Reachable only on a dev bench — auth.mode disabled is refused outside")
		s.L("\t\t// APP_PROFILE=dev — and it serves EVERY row, which is the point: a")
		s.L("\t\t// tenant-scoped entity is otherwise unusable on the machine it is")
		s.L("\t\t// first tried on, answering every listing empty.")
	} else {
		s.L("\t\t// No identity: no rows. Failing open here would expose every row.")
		s.L("\t\t%s.Filter[%s] = \"\"", target, quote(field.Name))
	}
	s.L("\t}")
}
