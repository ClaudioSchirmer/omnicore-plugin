package emit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
)

// The read side, application half.
//
// A read is anatomically the write side reversed. A command declares a Result
// and projects the entity into it with FromEntity; a query declares a Result
// and the framework fills it from the stored document, then hands it to
// FromQueryResult. Both Results are application-pure — no wire tags — and both
// are turned into a wire Response by that Response's own FromResult.
//
// The consequence worth stating, because it decides what this file emits: the
// Result owns field EXISTENCE. A field absent from it can reach no surface —
// not REST, not GraphQL, not the CSV/XLSX export — because none of them ever
// sees the document again.

// readShape is the pointer discipline one read serves under.
//
// `?fields=` is what forces it: when a caller can ask for a subset, every
// leaf has to be able to arrive ABSENT, and a value type cannot tell absent
// from zero. The framework boot-guards the rule on the Result and on the
// Response, so it is not a style choice — an endpoint that declares the
// control and a value-typed leaf does not start.
type readShape struct{ sparse bool }

// resultType renders one field's type under this shape.
func (sh readShape) resultType(f ir.Field) string {
	if sh.sparse {
		return "*" + f.BaseGoType
	}
	return f.GoType
}

func (sh readShape) computedType(c ir.ComputedField) string {
	if sh.sparse {
		return "*" + c.BaseGoType
	}
	return c.GoType
}

func (sh readShape) idType() string {
	if sh.sparse {
		return "*string"
	}
	return "string"
}

// listShape is the listing's discipline: sparse exactly when the endpoint
// declares `?fields=`. The by-id read never declares it, so it is never sparse.
func listShape(m *ir.Model) readShape { return readShape{sparse: m.Read.Controls.Fields} }

func emitQueries(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File

	if m.Read.ByID {
		f, err := emitByIDQuery(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if m.Read.ByParams {
		f, err := emitListQuery(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if hasReadRows(m) {
		files, err := emitChildRowResults(m)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
	}
	if hasDerivations(m) {
		f, err := emitComputedHook(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// hasDerivations reports whether this entity derives ANY read field — at the
// root, or inside one of its collections. The two scopes share one hook file
// and one seat, so every gate that used to ask only about the root has to ask
// about both, or a spec that derives only per entry generates the field and
// nothing that fills it.
// HasDerivations is hasDerivations for the report, which asks the same question
// to decide whether the hook file has a section at all.
func HasDerivations(m *ir.Model) bool { return hasDerivations(m) }

func hasDerivations(m *ir.Model) bool {
	if len(m.Read.Computed) > 0 {
		return true
	}
	for _, c := range m.Children {
		if len(c.Computed) > 0 {
			return true
		}
	}
	return false
}

// StaleDerivationNames reports derivations whose hook file on disk still
// declares the OLD, unqualified name — `Compute<Field>` instead of
// `Compute<Entity><Field>`.
//
// The rename is what stopped two entities from emitting one function into one
// package, and on its own it is a loud, harmless failure: the call sites move,
// the old function goes unreferenced, and `go build` names the missing symbol.
// What is NOT harmless is the file it happens in. A hook is written once and
// never rewritten, so by the time this rename lands the body is the author's
// work — and a regeneration that says nothing leaves them with a file declaring
// a function nobody calls, next to call sites for a function nobody wrote, and
// no statement anywhere that the two are the same derivation.
//
// So it is refused BEFORE anything is written, naming the file, the function
// that is there and the one the tree now expects. Moving a body is a
// deliberate act; discovering it from a linker error is not.
//
// The match is textual on purpose: parsing the file would mean deciding what to
// do about one that does not parse, and a hook mid-edit legitimately does not.
// A false positive here costs one rename; reading the file is the author's next
// step either way.
func StaleDerivationNames(root string, m *ir.Model) []string {
	if len(m.Read.Computed) == 0 {
		return nil
	}
	body, err := os.ReadFile(filepath.Join(root, computedHookFile(m)))
	if err != nil {
		return nil // no hook yet: nothing to have been renamed
	}
	text := string(body)
	var out []string
	for _, c := range m.Read.Computed {
		want := computedFuncName(m.Entity, c)
		if strings.Contains(text, "func "+want+"(") {
			continue
		}
		if old := "Compute" + c.Name; strings.Contains(text, "func "+old+"(") {
			out = append(out, fmt.Sprintf("%s declares %s, and this tree now calls %s",
				computedHookFile(m), old, want))
		}
	}
	return out
}

// StaleDerivationFix is the one sentence that turns the report above into an
// action. It is shared so `check` and `generate` cannot phrase it differently.
const StaleDerivationFix = "rename the function in that file — the body is yours and is not " +
	"otherwise affected. The qualifier exists because every entity of a project writes its " +
	"derivations into one package, so two entities with a computed field of the same name " +
	"used to emit one function twice"

// childComputedFuncName qualifies a per-entry derivation by BOTH owners: the
// entity, because every entity of a project writes into one queries package, and
// the collection, because two collections of one entity may each want a Rotulo.
func childComputedFuncName(e ir.Names, c ir.Child, cf ir.ComputedField) string {
	return "Compute" + e.Pascal + c.Name + cf.Name
}

func emitByIDQuery(m *ir.Model) (fsplan.File, error) {
	sh := readShape{}
	s := &src{}
	s.Blank()
	s.L("package queries")
	s.Blank()
	queryImports(s, resultTypeNames(m, sh), true, childDTOImport(m))
	s.Blank()

	s.Doc(
		fmt.Sprintf("%s is the application-side transport for the by-id read.", m.Read.QueryByID),
		"",
		"QueryByIDBase supplies SetPathID, which the wrapper calls with the route's "+
			"own :id segment. Criteria arrives whole from the wrapper — the by-id read "+
			"takes its wire criteria the same way the paged one does, so the single "+
			"control this endpoint speaks (?includeArchived) needs no unwrapping by hand.")
	s.L("type %s struct {", m.Read.QueryByID)
	s.L("\tfwqueries.QueryByIDBase")
	s.L("\tCriteria fwqueries.ReadCriteria")
	s.L("}")
	s.Blank()

	s.Doc("ToCriteria is where identity-derived read restrictions are injected. " +
		"ReadByID merges Filter into the {id} + archived gate; the paging knobs on " +
		"the same criteria are ignored there by design.")
	s.L("func (q %s) ToCriteria(ctx *configuration.AppContext) (fwqueries.ReadCriteria, error) {",
		m.Read.QueryByID)
	s.L("\tcrit := q.Criteria")
	emitFieldRestrictions(s, m, "crit")
	s.L("\treturn crit, nil")
	s.L("}")
	s.Blank()

	emitFromQueryResult(s, m, m.Read.QueryByID, m.Read.ResultByID)
	s.Blank()

	s.Doc("ContextName labels this aggregate in the error envelope.")
	s.L("func (q %s) ContextName() string { return %s }", m.Read.QueryByID, quote(m.Entity.Pascal))
	s.Blank()

	s.Doc(
		fmt.Sprintf("%s is what the by-id read returns.", m.Read.ResultByID),
		"",
		"It carries the WHOLE aggregate — the root's fields, the facets' fields and "+
			"the collections. Reading one record and getting less of it than the listing "+
			"gives for the same record is the shape nobody expects, and there is no "+
			"second request that would fill the gap: the document was already fetched.",
		"",
		"No wire tags: naming the wire is the Response's job, and a tagged Result "+
			"is refused at boot. This read declares no ?fields=, so plain values are "+
			"right — an absent key fills as the zero value.")
	emitResultStruct(s, m, m.Read.ResultByID, sh)

	return goFile("internal/application/queries/find_"+m.Entity.Snake+"_by_id_query.go",
		fsplan.Owned, "the by-id query and its result", s)
}

func emitListQuery(m *ir.Model) (fsplan.File, error) {
	sh := listShape(m)
	s := &src{}
	s.Blank()
	s.L("package queries")
	s.Blank()
	queryImports(s, resultTypeNames(m, sh), true, childDTOImport(m))
	s.Blank()

	s.Doc(
		fmt.Sprintf("%s is the application-side transport for the paged read.", m.Read.QueryList),
		"",
		"The criteria arrive already parsed from the query string by the framework; "+
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

	emitFromQueryResult(s, m, m.Read.QueryList, m.Read.ResultList)
	s.Blank()

	s.L("func (q %s) ContextName() string { return %s }", m.Read.QueryList, quote(m.Entity.Pascal))
	s.Blank()

	doc := []string{
		fmt.Sprintf("%s is one row of the listing, as the application sees it.", m.Read.ResultList),
		"",
		"No wire tags: naming the wire is the Response's job, and a tagged Result " +
			"is refused at boot.",
	}
	if sh.sparse {
		doc = append(doc, "",
			"Every field is a pointer because the endpoint serves ?fields=: a column "+
				"the caller did not ask for must fill as ABSENT, not as a zero value, and "+
				"the framework boot-guards that contract on the Result as well as on the "+
				"Response.")
	}
	s.Doc(doc...)
	emitResultStruct(s, m, m.Read.ResultList, sh)

	return goFile("internal/application/queries/find_"+m.Entity.PluralSnake+"_by_params_query.go",
		fsplan.Owned, "the listing query and its result", s)
}

// emitFromQueryResult writes the read-side twin of a command's FromEntity.
//
// The hook is MANDATORY on both query shapes, and it is the only seat where
// read-side computation may happen: it runs once per document, below the web
// boundary, so REST, GraphQL and the tabular exports all render the same
// derived values. Doing it in a Response's FromResult instead would give each
// surface its own answer.
func emitFromQueryResult(s *src, m *ir.Model, query, result string) {
	computed := hasDerivations(m)
	doc := []string{
		"FromQueryResult is the read-side twin of a command's FromEntity: the framework " +
			"hands it the Result already filled from the stored document, BEFORE any " +
			"transport sees it.",
	}
	if computed {
		doc = append(doc, "",
			fmt.Sprintf("This read carries computed fields, so the derivation runs here — in "+
				"%s, which the generator wrote once and never touches again.", computedHookFile(m)))
	} else {
		doc = append(doc, "",
			"Nothing is derived on this read, so the filled Result is returned unchanged. "+
				"Declare read.computed in the spec to get a derivation seat.")
	}
	s.Doc(doc...)
	if computed {
		s.L("func (q %s) FromQueryResult(ctx *configuration.AppContext, r %s) (%s, error) {",
			query, result, result)
		emitComputedCalls(s, m, "r", result == m.Read.ResultList && listShape(m).sparse)
		s.L("\treturn r, nil")
		s.L("}")
		return
	}
	s.L("func (q %s) FromQueryResult(_ *configuration.AppContext, r %s) (%s, error) {",
		query, result, result)
	s.L("\treturn r, nil")
	s.L("}")
}

// emitResultStruct writes the Result's fields — the same set the matching
// Response carries, under the same names, because the mapper between them is
// name-based.
func emitResultStruct(s *src, m *ir.Model, name string, sh readShape) {
	s.L("type %s struct {", name)
	s.L("\tID %s", sh.idType())
	for _, f := range m.AllOwnerFields() {
		s.L("\t%s %s", f.Name, sh.resultType(f))
	}
	// The framework-stamped columns the read declared. They sit with the entity's
	// own fields because that is what they are by the time a document is read —
	// the schema resolves their logical names itself — and they are absent from
	// every WRITE shape, where the entity carries no field to fill them from.
	for _, f := range m.Read.Managed {
		s.L("\t%s %s", f.Name, sh.resultType(f))
	}
	// The ROOT joins' fields. They sit with the entity's own because that is
	// what they are on the loaded entity — the read model serves them through
	// the very loader that declares them, and it inherits the reach without
	// declaring anything itself.
	for _, f := range m.Read.JoinFields {
		s.L("\t%s %s", f.Name, sh.resultType(f))
	}
	for _, c := range m.Read.Computed {
		s.L("\t// %s is COMPUTED: no column backs it, and FromQueryResult fills it", c.Name)
		s.L("\t// from %s.", strings.Join(c.Sources, "+"))
		s.L("\t%s %s", c.Name, sh.computedType(c))
	}
	for _, c := range m.Children {
		s.L("\t%s []%s", c.GoPlural, childRowResult(c))
	}
	s.L("}")
}

// emitChildRowResults writes each collection's read shape — ONE FILE PER
// COLLECTION, under queries/dtos.
//
// Both reads project the same rows, so both consume the same type — the read
// twin of the single <Child>Row the two Responses already share. That is
// exactly what keeps it out of either query's file: a Result belongs beside its
// Query, and a type BOTH queries name belongs beside neither.
func emitChildRowResults(m *ir.Model) ([]fsplan.File, error) {
	sh := listShape(m)
	var out []fsplan.File
	for _, c := range m.Children {
		if c.Mounted {
			continue // declared, with this shape, by the role that owns the identity
		}
		s := &src{}
		s.Blank()
		s.L("package dtos")
		s.Blank()
		queryImports(s, childRowTypeNames(m, sh), false, "")
		s.Blank()

		doc := []string{
			fmt.Sprintf("%s is one entry of the %s collection as the application reads it.",
				childRowResultName(c), c.Segment),
		}
		if sh.sparse {
			doc = append(doc, "",
				"Pointers throughout, for the same reason the root is: the listing serves "+
					"?fields=, and the sparse-fill contract is enforced recursively.")
		}
		s.Doc(doc...)
		s.L("type %s struct {", childRowResultName(c))
		s.L("\tID %s", sh.idType())
		for _, f := range c.Fields {
			s.L("\t%s %s", f.Name, sh.resultType(f))
		}
		// A CHILD join's fields are served INSIDE the entry — the document
		// carries them under <segment>.<field>, which is also how ?fields= names
		// them. They are load-only: no filter and no order reaches them.
		for _, f := range m.ServedJoinFields(c) {
			s.L("\t%s %s", f.Name, sh.resultType(f))
		}
		// The entry's DERIVED fields. They sit at the end for the same reason the
		// root's do — nothing reads them off the document, FromQueryResult fills
		// them once per entry — and they are the only fields here that no column
		// anywhere backs.
		for _, cf := range c.Computed {
			s.L("\t// %s is COMPUTED per entry: no column backs it, and", cf.Name)
			s.L("\t// FromQueryResult fills it from %s.", strings.Join(cf.Sources, "+"))
			s.L("\t%s %s", cf.Name, sh.computedType(cf))
		}
		s.L("}")

		f, err := goFile(queryDTOPkg+"/"+naming.Snake(c.Name)+"_row_result.go",
			fsplan.Owned,
			fmt.Sprintf("the read shape of one %s entry", c.Name), s)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// hasReadRows reports whether this entity declares a collection shape of its
// own on the read side. A mounted child's shape belongs to the role that owns
// the identity, so a spec carrying only those declares no type here.
func hasReadRows(m *ir.Model) bool {
	for _, c := range m.Children {
		if !c.Mounted {
			return true
		}
	}
	return false
}

// computeFuncName is the derivation seat for one Result shape.
func computeFuncName(result string) string { return "compute" + result }

func computedHookFile(m *ir.Model) string {
	return "internal/application/queries/" + m.Entity.Snake + "_computed_manual.go"
}

// emitComputedHook writes the derivation the generator cannot write.
//
// It is the read side's `manual` fact: the language declares the SHAPE — the
// field, its type, the stored fields it reads — and the body is the author's,
// in a file created once and never rewritten. What the declaration already
// bought is real and needs no code: `?fields=<computed>` fetches the SOURCES
// instead of a name no column has, `?orderBy=<computed>` is a typed 400 on
// every surface, and the tabular exports keep the column under its label.
//
// ONE function per computed FIELD, taking the sources it declared. It used to be
// one function per READ SHAPE, each handed a whole Result — which made the
// author write the same derivation twice, once against plain values and once
// against the listing's sparse pointers, and kept them in step by hand. The
// shapes are the generator's problem: it unwraps what it has and calls this.
//
// The functions are EXPORTED because the write side calls them too: a POST that
// returns the record renders the same derived field, and deriving it a second
// time somewhere else is how two answers to one question start to differ.
func emitComputedHook(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package queries")
	s.Blank()
	computedHookImports(s, derivationTypeNames(m))
	s.Blank()

	s.Doc(
		"The derivations behind this entity's computed read fields.",
		"",
		"Each runs below the web boundary, so every surface renders the SAME value — "+
			"that is the whole reason read-side computation lives at this seat instead of "+
			"in a Response. A root derivation runs once per document and heads a column of "+
			"the CSV/XLSX export; a collection's runs once per ENTRY, is handed that entry, "+
			"and reaches REST and GraphQL only — a tabular row is flat, so no field of a "+
			"collection is in one, derived or stored.",
		"",
		"Until a body is written the field renders ABSENT, quietly. The framework "+
			"cannot detect that: as far as it is concerned the derivation ran and "+
			"produced nothing.")

	count := 0
	for _, c := range m.Read.Computed {
		emitOneDerivation(s, c, computedFuncName(m.Entity, c), "")
		count++
	}
	for _, ch := range m.Children {
		for _, c := range ch.Computed {
			emitOneDerivation(s, c, childComputedFuncName(m.Entity, ch, c), ch.Plural)
			count++
		}
	}

	f, err := goFile(computedHookFile(m), fsplan.Hook,
		fmt.Sprintf("the derivations for %d computed read field(s)", count), s)
	if err != nil {
		return f, err
	}
	f.Consequence = "every computed read field renders absent until its derivation is written — " +
		"quietly, because an empty derivation is indistinguishable from one that had nothing to say"
	return f, nil
}

// derivationTypeNames is every type string the hook file writes: each
// derivation's own type, and every source's.
//
// The file used to import `application/configuration` and nothing else, on the
// assumption that a derivation only ever sees builtins. It does not: `type: id`
// is `domain.ID` and `type: time` is `time.Time`, in a source OR in the derived
// value. A single such declaration emitted a hook that did not compile —
// `undefined: domain`, in a write-once file the author is then left to repair by
// hand. Deciding the imports from what is actually written is the same rule the
// query files already follow.
func derivationTypeNames(m *ir.Model) []string {
	var out []string
	add := func(cs []ir.ComputedField) {
		for _, c := range cs {
			out = append(out, c.GoType)
			for _, f := range c.SourceFields {
				out = append(out, f.GoType)
			}
		}
	}
	add(m.Read.Computed)
	for _, ch := range m.Children {
		add(ch.Computed)
	}
	return out
}

// computedHookImports writes the derivation file's import block. It always
// carries the AppContext — every signature takes one — and adds `time` and
// `domain` only when a type in the file names them.
//
// A caveat this cannot fix, and that the report says out loud instead: the file
// is written ONCE. A derivation added to the spec later arrives in a file the
// generator no longer touches, so a first `id` or `time` source among them needs
// its import added by hand. The compiler names it at the exact line.
func computedHookImports(s *src, types []string) {
	joined := strings.Join(types, " ")
	needTime := strings.Contains(joined, "time.")
	needDomain := strings.Contains(joined, "domain.")
	if !needTime && !needDomain {
		s.L("import %s", quote(fwImport("application/configuration")))
		return
	}
	s.L("import (")
	if needTime {
		s.L("\t%s", quote("time"))
		s.Blank()
	}
	s.L("\t%s", quote(fwImport("application/configuration")))
	if needDomain {
		s.L("\t%s", quote(fwImport("domain")))
	}
	s.L(")")
}

// emitOneDerivation writes one signature and the TODO body behind it.
//
// collection is empty for a root derivation and the collection's name for a
// per-entry one. It is the one sentence that tells the implementer how often
// their body runs and what it is looking at — an answer they would otherwise
// have to reconstruct from the call site.
func emitOneDerivation(s *src, c ir.ComputedField, fname, collection string) {
	params := computedParams(c)
	s.Blank()
	doc := []string{
		fmt.Sprintf("%s derives %s from %s.", fname, c.Name, strings.Join(c.Sources, ", ")),
	}
	if c.Description != "" {
		doc = append(doc, "", c.Description)
	}
	if collection != "" {
		doc = append(doc, "",
			fmt.Sprintf("It runs ONCE PER ENTRY of %s, and its sources are that entry's own "+
				"fields — the record around it is not in scope, and the framework would not "+
				"fetch it here if it were.", collection))
	}
	doc = append(doc,
		"",
		"A source declared NULLABLE arrives as a pointer, and nil is a value the "+
			"derivation has to decide about; the rest arrive as values, and the caller "+
			"has already established they were fetched.",
		"",
		"An error here fails the whole read, so return one only when the derivation "+
			"genuinely cannot produce a value — a missing source is absence, not a "+
			"failure.")
	s.Doc(doc...)
	// ONE format string for the emitted function and the one the report prints.
	// They used to be two that happened to agree, which is a pair that agrees
	// until somebody edits one — and the report is the hand-off, so a reviewer
	// copying a signature that no longer matches writes a function nothing calls.
	s.L("%s {", derivationSignature(fname, params, c.GoType))
	s.L("\t// TODO: derive %s from %s.", c.Name, strings.Join(params.names, ", "))
	s.L("\tvar %s %s", naming.Camel(c.Name), c.GoType)
	s.L("\treturn %s, nil", naming.Camel(c.Name))
	s.L("}")
}

// computedParams renders one derivation's parameter list: the sources it
// declared, under camelCase names, typed as the source itself is.
type computedParamSet struct {
	decls []string // "chaveRecurso string"
	names []string // "chaveRecurso"
	src   []ir.Field
}

// computedParams reads what the IR already resolved. It does NOT look a source
// up again: resolution happens once, in ir.Resolve, and a name that answers to
// nothing fails there rather than quietly costing this signature a parameter.
func computedParams(c ir.ComputedField) computedParamSet {
	var out computedParamSet
	for i, f := range c.SourceFields {
		param := naming.Camel(c.Sources[i])
		out.decls = append(out.decls, param+" "+f.GoType)
		out.names = append(out.names, param)
		out.src = append(out.src, f)
	}
	return out
}

// computedFuncName is the derivation's exported name, qualified by the ENTITY.
//
// The qualifier is not decoration. Every entity of a project writes its
// derivations into the same package — internal/application/queries — so two
// specs that each declare a computed field called Permission used to emit
// ComputePermission twice and the package stopped compiling. Worse than the
// build break: the file is a hook, written once and never rewritten, so the
// obvious way out is to edit one of the two by hand and lose whichever body was
// already there.
func computedFuncName(e ir.Names, c ir.ComputedField) string {
	return "Compute" + e.Pascal + c.Name
}

// ComputedSignature renders one derivation's Go signature, for the report to ask
// the implementer for. It is derived from the same place the file is written, so
// the two cannot describe different functions.
func ComputedSignature(m *ir.Model, c ir.ComputedField) string {
	return derivationSignature(computedFuncName(m.Entity, c), computedParams(c), c.GoType)
}

// derivationSignature is the single authority on what a derivation looks like.
// Everything that renders one — the hook, the report — goes through here.
func derivationSignature(fname string, p computedParamSet, goType string) string {
	return fmt.Sprintf("func %s(ctx *configuration.AppContext, %s) (%s, error)",
		fname, strings.Join(p.decls, ", "), goType)
}

// ChildComputedSignature is the same for a per-entry derivation.
func ChildComputedSignature(m *ir.Model, ch ir.Child, c ir.ComputedField) string {
	return derivationSignature(childComputedFuncName(m.Entity, ch, c), computedParams(c), c.GoType)
}

// derivationSeat is one place derivations run: the shape that HOLDS the derived
// fields, the indent its statements are written at, and what a failing
// derivation hands back.
//
// The three travel together because the per-entry seat differs from the root's
// in all three at once — it writes into r.Permissoes[i], one tab deeper, and
// still returns r, since the method's result is the whole document however deep
// the failure was.
type derivationSeat struct {
	recv   string
	fail   string
	pad    string
	sparse bool
}

// emitDerivationCalls unwraps what this seat holds, calls the derivation, and
// assigns what it returned.
//
// sparse says every field of the shape is a pointer because the read serves
// `?fields=`. There a source that was not selected is nil, and the derivation
// does not run at all — the field stays absent, which is what the caller asked
// for by not selecting the source.
func emitDerivationCalls(s *src, seat derivationSeat, computed []ir.ComputedField,
	name func(ir.ComputedField) string) {
	for _, c := range computed {
		p := computedParams(c)
		var args, guards []string
		for _, f := range p.src {
			ref := seat.recv + "." + f.Name
			if !seat.sparse || f.Nullable {
				// Either the shape holds values, or the source is a pointer on
				// both sides; nothing to unwrap in either case.
				args = append(args, ref)
				continue
			}
			guards = append(guards, ref+" != nil")
			args = append(args, "*"+ref)
		}
		if len(guards) > 0 {
			s.L("%sif %s {", seat.pad, strings.Join(guards, " && "))
		} else {
			s.L("%s{", seat.pad)
		}
		in := seat.pad + "\t"
		s.L("%sv, err := %s(ctx, %s)", in, name(c), strings.Join(args, ", "))
		s.L("%sif err != nil {", in)
		s.L("%s\treturn %s, err", in, seat.fail)
		s.L("%s}", in)
		if seat.sparse {
			s.L("%s%s.%s = &v", in, seat.recv, c.Name)
		} else {
			s.L("%s%s.%s = v", in, seat.recv, c.Name)
		}
		s.L("%s}", seat.pad)
	}
}

// emitComputedCalls writes every derivation this Result shape carries: the
// root's, then each collection's, once per entry.
//
// The collection loop is `for i := range` rather than `for _, e :=` on purpose —
// the ranged copy would be written into and thrown away, which is the silent
// version of not deriving at all.
func emitComputedCalls(s *src, m *ir.Model, recv string, sparse bool) {
	emitDerivationCalls(s,
		derivationSeat{recv: recv, fail: recv, pad: "\t", sparse: sparse},
		m.Read.Computed,
		func(c ir.ComputedField) string { return computedFuncName(m.Entity, c) })

	// The ENTRY's pointer discipline is not the root's. One <Child>RowResult
	// serves BOTH reads — that is the point of the shared type — so it is sparse
	// whenever the listing is, including inside the by-id Result, whose own
	// fields are plain values. Reading the root's shape here unwrapped a pointer
	// that was still a pointer and assigned a value where one was expected: two
	// compile errors, in the tree the author was handed.
	entryShape := listShape(m)
	for _, ch := range m.Children {
		if len(ch.Computed) == 0 {
			continue
		}
		s.L("\tfor i := range %s.%s {", recv, ch.GoPlural)
		emitDerivationCalls(s,
			derivationSeat{
				recv:   fmt.Sprintf("%s.%s[i]", recv, ch.GoPlural),
				fail:   recv,
				pad:    "\t\t",
				sparse: entryShape.sparse,
			},
			ch.Computed,
			func(c ir.ComputedField) string { return childComputedFuncName(m.Entity, ch, c) })
		s.L("\t}")
	}
}

// resultShape pairs a Result type with the sentence that tells the author what
// its pointer discipline means for the derivation they are about to write.
type resultShape struct{ result, note string }

func readResultShapes(m *ir.Model) []resultShape {
	var out []resultShape
	if m.Read.ByID {
		out = append(out, resultShape{
			result: m.Read.ResultByID,
			note: "The by-id read declares no ?fields=, so its sources are plain values: " +
				"an absent one reads as the zero value.",
		})
	}
	if m.Read.ByParams {
		note := "The listing's sources are plain values, like the by-id read's."
		if listShape(m).sparse {
			note = "The listing serves ?fields=, so every source is a POINTER and nil means " +
				"the caller did not ask for it — guard each one before dereferencing, and " +
				"leave the derived field nil when a source it needs is absent."
		}
		out = append(out, resultShape{result: m.Read.ResultList, note: note})
	}
	return out
}

func prefixed(p string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, p+n)
	}
	return out
}

// resultTypeNames lists every type string one read file emits, so the imports
// can be decided from what is actually written rather than from a guess.
func resultTypeNames(m *ir.Model, sh readShape) []string {
	out := []string{sh.idType()}
	for _, f := range m.AllOwnerFields() {
		out = append(out, sh.resultType(f))
	}
	for _, f := range m.Read.Managed {
		out = append(out, sh.resultType(f))
	}
	for _, f := range m.Read.JoinFields {
		out = append(out, sh.resultType(f))
	}
	for _, c := range m.Read.Computed {
		out = append(out, sh.computedType(c))
	}
	return out
}

func childRowTypeNames(m *ir.Model, sh readShape) []string {
	out := []string{sh.idType()}
	for _, c := range m.Children {
		if c.Mounted {
			continue
		}
		for _, f := range c.Fields {
			out = append(out, sh.resultType(f))
		}
		// The collection's JOINED fields, for the same reason resultTypeNames
		// walks the root's: emitChildRowResults writes them into the same
		// struct, so a type they alone bring in — time.Time is the only one the
		// join vocabulary has — needs its import decided from here too.
		for _, f := range m.ServedJoinFields(c) {
			out = append(out, sh.resultType(f))
		}
		for _, cf := range c.Computed {
			out = append(out, sh.computedType(cf))
		}
	}
	return out
}

// queryImports writes the import block a read file needs.
//
// It is decided from the types the file actually emits: a blank-identifier
// sentinel (`var _ = time.Time{}`) would work too, but it puts a line in every
// generated file whose only job is to excuse an import that did not have to be
// there.
// childDTOs is the queries/dtos import path when the file names a collection's
// read shape, and empty when it does not — the shapes' own package passes
// nothing, since a package cannot import itself.
func queryImports(s *src, types []string, carriesQuery bool, childDTOs string) {
	joined := strings.Join(types, " ")
	needTime := strings.Contains(joined, "time.")
	needDomain := strings.Contains(joined, "domain.")
	if !needTime && !needDomain && !carriesQuery && childDTOs == "" {
		return // a file of plain shapes over builtin types imports nothing
	}

	s.L("import (")
	if needTime {
		s.L("\t%s", quote("time"))
		if needDomain || carriesQuery || childDTOs != "" {
			s.Blank()
		}
	}
	if carriesQuery {
		s.L("\t%s", quote(fwImport("application/configuration")))
	}
	if needDomain {
		s.L("\t%s", quote(fwImport("domain")))
	}
	if carriesQuery {
		s.L("\tfwqueries %s", quote(fwImport("application/queries")))
	}
	if childDTOs != "" {
		s.Blank()
		s.L("\t%s %s", queryDTOAlias, quote(childDTOs))
	}
	s.L(")")
}

// childDTOImport is the queries/dtos path when this entity's reads name a
// collection at all, and empty otherwise. A read with no collections names no
// shape from there, and gofile would prune the line anyway — deciding it here
// keeps the emitted import block honest rather than merely harmless.
func childDTOImport(m *ir.Model) string {
	if len(m.Children) == 0 {
		return ""
	}
	return m.ImportPath(queryDTOPkg)
}

// ─── the read side, web half ────────────────────────────────────────────────

// responseAuthorityDoc is the one paragraph both read Responses open with.
//
// It is worth repeating on every generated DTO because it is the rule people
// get wrong: the Response is not "the REST shape". It is THE wire shape, for
// REST, GraphQL and the CSV/XLSX export alike, and a field it does not declare
// exists on none of them.
const responseAuthorityDoc = "This type is the SINGLE wire authority for the read, on every surface: " +
	"REST, GraphQL and the tabular exports all render exactly the fields declared " +
	"here, under these json names. A field outside it reaches no wire — and " +
	"?fields= speaks this same vocabulary, so the JSON listing and the CSV export " +
	"accept the same selection."

// readFieldTag builds the struct tag of one read Response field.
//
// exportLabelKey is where a tabular export's column HEADER comes from — the
// catalog key, translated per request language, falling back to the json name.
// It rides the Response rather than the schema because the Response is what
// the export projects: a column the DTO does not declare is exported nowhere,
// so its header has nowhere else to live.
func readFieldTag(f ir.Field, pointered bool) string {
	name := f.JSONName
	if pointered {
		name += ",omitempty"
	}
	parts := []string{"json:" + quote(name)}
	if f.LabelKey != "" {
		parts = append(parts, "exportLabelKey:"+quote(f.LabelKey))
	}
	parts = append(parts, "example:"+quote(f.Example))
	return strings.Join(parts, " ")
}

// emitComputedResponseFields declares the derived fields on the wire.
//
// The `computed:"A,B"` tag is what makes the field work rather than merely
// appear: it tells the framework which COLUMNS to fetch when a caller selects
// this field — there is none behind the field itself — and it is what earns the
// typed 400 on ?orderBy=<computed>, since ordering happens in the store.
//
// The sources need not appear here. One that exists only on the Result is
// read, feeds the derivation, and never reaches the wire.
func emitComputedResponseFields(s *src, m *ir.Model, pointered bool) {
	emitComputedFieldTags(s, m.Read.Computed, pointered)
}

// emitComputedFieldTags is the same for ANY level of the response.
//
// A collection's entry carries derived fields exactly as the root does, and the
// tag it needs is spelled the same way: the framework records a nested field's
// sources under the SAME segment prefix as the field itself, so the entry's
// sources are named bare here and arrive at the store as
// <collection>.<source>. Prefixing them here would ask for
// <collection>.<collection>.<source>, which resolves to nothing.
func emitComputedFieldTags(s *src, computed []ir.ComputedField, pointered bool) {
	for _, c := range computed {
		typ, name := c.GoType, c.JSONName
		if pointered {
			typ, name = "*"+c.BaseGoType, c.JSONName+",omitempty"
		}
		parts := []string{"json:" + quote(name)}
		if c.LabelKey != "" {
			parts = append(parts, "exportLabelKey:"+quote(c.LabelKey))
		}
		parts = append(parts, "computed:"+quote(strings.Join(c.Sources, ",")))
		parts = append(parts, "example:"+quote(c.Example))
		if c.Description != "" {
			for _, line := range wrap(c.Description, 70) {
				s.L("\t// %s", line)
			}
		}
		s.L("\t%s %s `%s`", c.Name, typ, strings.Join(parts, " "))
	}
}

// emitReadFromResult writes the Response's mapping seat.
//
// Map is the generic name-based mapper: it reads each Response field from the
// same-named Result field and writes it under the json tag. The alignment is
// boot-guarded, so a Response field with no Result behind it fails at mount
// rather than silently rendering empty. A Response that needs more than the
// tags replaces this method by hand — the constructors take any
// func(TResult) TResp.
//
// What does NOT belong here: computation. A derived value produced at this
// seat would be produced once per SURFACE, and the exports would disagree with
// the JSON. That is what the query's FromQueryResult is for.
func emitReadFromResult(s *src, response, result string) {
	emitAutoFromResult(s, response, "appqueries."+result,
		"",
		"Read-side COMPUTATION does not belong here: it would run once per SURFACE, "+
			"and the export would disagree with the JSON. It belongs in the query's "+
			"FromQueryResult, which runs once per document.")
}

// listHandler / byIDHandler are the read handlers, spelled once.
//
// Both are generic over TWO parameters now — the query AND the Result it
// declares — because the handler is what fills the Result from the document
// and calls FromQueryResult. A raw document never leaves the application
// layer, so the type has to be named here.
func listHandler(m *ir.Model) string {
	return fmt.Sprintf("&handlers.FindByParamsQueryHandler[*appqueries.%s, appqueries.%s]{\n"+
		"\t\t\tReader: d.ViewReader, View: %s,\n\t\t}", m.Read.QueryList, m.Read.ResultList, "viewName")
}

func byIDHandler(m *ir.Model) string {
	return fmt.Sprintf("&handlers.FindByIDQueryHandler[*appqueries.%s, appqueries.%s]{\n"+
		"\t\t\tReader: d.ViewReader, View: %s,\n\t\t}", m.Read.QueryByID, m.Read.ResultByID, "viewName")
}

// graphQLHandler is the same handler under the name GraphQL has for the view.
// The registry is built before any route group, so there is no viewName local
// there — the view definition is in hand instead.
func graphQLListHandler(m *ir.Model) string {
	return fmt.Sprintf("&handlers.FindByParamsQueryHandler[*appqueries.%s, appqueries.%s]{\n"+
		"\t\t\tReader: d.ViewReader, View: view.Name(),\n\t\t}", m.Read.QueryList, m.Read.ResultList)
}

func graphQLByIDHandler(m *ir.Model) string {
	return fmt.Sprintf("&handlers.FindByIDQueryHandler[*appqueries.%s, appqueries.%s]{\n"+
		"\t\t\tReader: d.ViewReader, View: view.Name(),\n\t\t}", m.Read.QueryByID, m.Read.ResultByID)
}

// emitReadMappingTests proves the Result→Response travel actually happens.
//
// The framework boot-guards the ALIGNMENT — a Response field with no Result
// behind it panics at mount — but a guard that only fires when a service starts
// is a guard nobody sees in a pull request. This runs in `go test`: it fills
// the Result the read declares and checks the value came out the other side
// under the Response's own type.
func emitReadMappingTests(tf *testFiles, m *ir.Model) {
	if !m.Read.Enabled {
		return
	}
	if m.Read.ByID {
		emitOneReadMappingTest(tf.at("find_"+m.Entity.Snake+"_by_id"), m,
			m.Op("byId").ResponseType, m.Read.ResultByID, readShape{})
	}
	if m.Read.ByParams {
		emitOneReadMappingTest(tf.at("find_"+m.Entity.PluralSnake+"_by_params"), m,
			m.Op("byParams").ResponseType, m.Read.ResultList, listShape(m))
	}
}

func emitOneReadMappingTest(s *src, m *ir.Model, response, result string, sh readShape) {
	s.Doc(
		fmt.Sprintf("%s is filled from %s and from nothing else.", response, result),
		"",
		"The mapping is name-based, so a rename on one side and not the other "+
			"produces a field that is silently always empty rather than a compile error. "+
			"The framework catches it at mount; this catches it here, where it is cheap.")
	s.L("func Test%s_IsFilledFromTheResult(t *testing.T) {", response)
	if sh.sparse {
		s.L("\tid := %s", quote(readProbeID))
		s.L("\tout := %s{}.FromResult(appqueries.%s{ID: &id})", response, result)
		s.L("\tif out.ID == nil || *out.ID != id {")
		s.L("\t\tt.Fatalf(%s, out.ID)", quote("the id did not survive the mapping: %v"))
		s.L("\t}")
	} else {
		s.L("\tout := %s{}.FromResult(appqueries.%s{ID: %s})", response, result, quote(readProbeID))
		s.L("\tif out.ID != %s {", quote(readProbeID))
		s.L("\t\tt.Fatalf(%s, out.ID)", quote("the id did not survive the mapping: %q"))
		s.L("\t}")
	}
	s.L("}")
	s.Blank()
}

// readProbeID is a value with no meaning beyond being recognisable in a failure
// message. Any string would do; a uuid-shaped one reads like real data.
const readProbeID = "7b3c1f10-3c7e-4a8d-9f0e-9d2a8e6d4b51"

// emitDerivationRunsTest is the other half of the seat's coverage: the
// empty-Result case each query's test file carries proves the derivation
// SURVIVES nothing, and this proves it RUNS.
//
// The two are different code paths, not two spellings of one. The generator
// guards each source the listing may not have selected, so an empty Result
// exercises only the branch where the derivation is skipped — the unwrapping,
// the call and the assignment would go untested, on the one seat whose body is
// written by hand.
func emitDerivationRunsTest(s *src, m *ir.Model) {
	if !hasDerivations(m) {
		return
	}
	s.Doc(
		"The derivations RUN when the sources they declared are there.",
		"",
		"The bodies are hand-written and may legitimately return nothing yet; what is "+
			"under test is the seat around them — that the generated unwrapping, the call "+
			"and the assignment hold together for a Result that carries every source.")
	s.L("func Test%sDerivationsRun(t *testing.T) {", m.Entity.Pascal)
	s.L("\tctx := &configuration.AppContext{}")
	if m.Read.ByID {
		emitFilledResultCase(s, m, m.Read.QueryByID, m.Read.ResultByID, readShape{}, "by-id")
	}
	if m.Read.ByParams {
		emitFilledResultCase(s, m, m.Read.QueryList, m.Read.ResultList, listShape(m), "listing")
	}
	s.L("}")
	s.Blank()
}

// emitFilledResultCase builds one Result with every declared source present and
// drives the seat with it. A pointer field needs an addressable value, so those
// sources land in a local first.
//
// A collection that derives anything gets ONE entry, filled the same way: the
// per-entry seat is a loop the root's inputs never enter, so a Result with an
// empty collection would leave the loop body — the unwrapping, the call, the
// write back through the index — untested.
func emitFilledResultCase(s *src, m *ir.Model, query, result string, sh readShape, label string) {
	// One namespace for every local in this case: a root source and an entry
	// source may legitimately share a name, and two `:=` of one name is a
	// generated tree that does not compile.
	used := map[string]bool{}
	local := func(base string) string {
		name := naming.Camel(base)
		for n := 2; used[name]; n++ {
			name = fmt.Sprintf("%s%d", naming.Camel(base), n)
		}
		used[name] = true
		return name
	}
	// The entry's own shape, which is the listing's whatever the root read is:
	// one <Child>RowResult serves both reads.
	entryShape := listShape(m)
	fill := func(computed []ir.ComputedField, prefix string, sh readShape) []string {
		seen := map[string]bool{}
		var assigns []string
		for _, c := range computed {
			for i, f := range c.SourceFields {
				name := c.Sources[i]
				if seen[name] {
					continue
				}
				seen[name] = true
				lit := literalFor(f)
				if sh.sparse || f.Nullable {
					l := local(prefix + name)
					s.L("\t\t%s := %s(%s)", l, f.BaseGoType, lit)
					assigns = append(assigns, fmt.Sprintf("%s: &%s", name, l))
					continue
				}
				assigns = append(assigns, fmt.Sprintf("%s: %s", name, lit))
			}
		}
		return assigns
	}

	s.L("\t{")
	assigns := fill(m.Read.Computed, "", sh)
	for _, ch := range m.Children {
		if len(ch.Computed) == 0 {
			continue
		}
		entry := fill(ch.Computed, naming.Camel(ch.Name), entryShape)
		assigns = append(assigns, fmt.Sprintf("%s: []%s{{%s}}",
			ch.GoPlural, childRowResult(ch), strings.Join(entry, ", ")))
	}
	s.L("\t\tr := %s{%s}", result, strings.Join(assigns, ", "))
	s.L("\t\tif _, err := (%s{}).FromQueryResult(ctx, r); err != nil {", query)
	s.L("\t\t\tt.Errorf(%s, err)", quote("the "+label+" derivation failed on a filled result: %v"))
	s.L("\t\t}")
	s.L("\t}")
}
