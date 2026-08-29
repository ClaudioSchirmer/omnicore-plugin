package emit

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
)

// A domain service is how a rule asks a question the aggregate cannot answer
// alone — "is this number already taken?", "how many active ones are there?".
//
// The port lives in the domain and returns PURE VALUES, never an error: the
// domain has no IO and must not receive an infra failure it can only panic on,
// swallow, or turn into a wrong answer. The implementation lives in infra,
// where IO belongs, and fails loudly there.
func emitService(m *ir.Model) ([]fsplan.File, error) {
	if m.Service == nil {
		return nil, nil
	}
	port, err := emitServicePort(m)
	if err != nil {
		return nil, err
	}
	impl, err := emitServiceImpl(m)
	if err != nil {
		return nil, err
	}
	out := []fsplan.File{port, impl}

	// The ELSE: the generator declared methods it does not know how to answer, so
	// it leaves a file for whoever does, with a body that panics naming itself.
	//
	// A stub rather than nothing, deliberately: a package that does not compile
	// cannot be tested or booted at all, so the whole verify flow would be dead
	// on arrival. The stub keeps the project buildable and makes the gap
	// impossible to miss the moment the rule actually runs — the pipeline turns
	// the panic into a 500 and the write does not happen. What it never does is
	// answer, which is the one outcome that would be worse than failing.
	if hasManualFacts(m) {
		stub, err := emitServiceStubFile(m)
		if err != nil {
			return nil, err
		}
		out = append(out, stub)
	}
	return out, nil
}

func hasManualFacts(m *ir.Model) bool {
	for _, f := range m.Service.Facts {
		if f.Manual {
			return true
		}
	}
	return false
}

func emitServicePort(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package domain")
	s.Blank()
	emitServiceImports(s)
	s.Blank()

	iface := m.Entity.Pascal + "Service"
	s.Doc(
		fmt.Sprintf("%s answers the questions %s's rules cannot answer alone.", iface, m.Entity.Pascal),
		"",
		"Every method returns a plain value and NO error. That is deliberate: the "+
			"domain performs no IO, so an error here would leave a rule with three bad "+
			"options — panic, swallow it, or invent an answer. A failed query is infra's "+
			"problem and is raised there.",
	)
	s.L("type %s interface {", iface)
	s.L("\tdomain.Service")
	for _, f := range m.Service.Facts {
		s.Blank()
		for _, line := range wrap(factDoc(f), 72) {
			s.L("\t// %s", line)
		}
		s.L("\t%s(%s) %s", f.Name, factParams(f), factResults(f))
	}
	s.L("}")

	emitAnswerTypes(s, m)

	return goFile("internal/domain/"+m.Entity.Snake+"_service.go", fsplan.Owned,
		fmt.Sprintf("the %s service port (%d fact(s))", m.Entity.Pascal, len(m.Service.Facts)), s)
}

// emitServiceImports writes the domain-side import block.
//
// "time" is here because a fact may be narrowed by a timestamp — a declared
// time field, or one of the framework's own stamped columns — and the parameter
// that carries it is time.Time. The emitter never ADDED an import, so such a
// fact produced a port that named a package nothing imported and a tree that
// did not build. gofile prunes the line for every service that takes no
// instant, so nothing changes for the specs that do not.
func emitServiceImports(s *src) {
	s.L("import (")
	s.L("\t%s", quote("time"))
	s.Blank()
	s.L("\t%s", quote(fwImport("domain")))
	s.L(")")
}

// emitStubImports is emitServiceImports plus the app domain, which the hook
// file needs for the ENTRY CARRIER of a batched per-entry fact: the type is
// declared beside the port, and this file is in infra. gofile prunes the line
// for every service whose facts take no carrier, which is most of them.
func emitStubImports(s *src, m *ir.Model) {
	s.L("import (")
	s.L("\t%s", quote("time"))
	s.Blank()
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
	s.L(")")
}

// emitAnswerTypes writes the shapes a fact speaks in when a bare scalar is not
// one: one row of a per-group fact, the struct an ungrouped fact answering
// SEVERAL numbers returns, and the carrier ONE entry of a batched per-entry
// fact arrives as.
//
// They live here, in the domain, and not in infra: the port is what the rules
// speak to, and a domain that had to name the framework's own *read.Group to
// read an answer would import infra to state an invariant. The entry carrier is
// the same argument on the way IN — the rule builds those values.
func emitAnswerTypes(s *src, m *ir.Model) {
	for _, f := range m.Service.Facts {
		if f.PerEntry.Carrier() {
			emitEntryType(s, f)
		}
		switch {
		case f.Grouped():
			emitGroupType(s, f)
		case f.Multi:
			emitResultType(s, f)
		}
	}
}

// emitEntryType writes what ONE entry of a batched per-entry fact contributes.
//
// A struct rather than parallel slices, and that is the whole reason it exists:
// two slices the caller has to keep index-aligned is a defect that compiles,
// runs, and answers about the wrong entry. With one field it would be pure
// ceremony, so the language does not build it — the parameter is a plain slice
// of the key there.
func emitEntryType(s *src, f ir.Fact) {
	s.Blank()
	names := make([]string, 0, len(f.PerEntry.Fields))
	for _, p := range f.PerEntry.Fields {
		names = append(names, p.Field)
	}
	s.Doc(
		fmt.Sprintf("%s is ONE entry of %s, as %s is asked about it.",
			f.PerEntry.EntryType, f.PerEntry.Collection, f.Name),
		"",
		fmt.Sprintf("The question needs %s of the same entry, and they travel together "+
			"for one reason: two parallel slices are two things a caller can put out of "+
			"step, and the answer would then be about a different entry than the one "+
			"whose values were sent.", strings.Join(names, " and ")),
		"",
		fmt.Sprintf("%s is what the answer is keyed by.", f.PerEntry.Key.Field),
	)
	s.L("type %s struct {", f.PerEntry.EntryType)
	for _, p := range f.PerEntry.Fields {
		s.L("\t%s %s", p.Field, p.GoType)
	}
	s.L("}")
}

// emitGroupType writes one row of a per-group answer: the key, and this group's
// number(s).
//
// The key is a string on every backend. The framework normalises a group key to
// a driver-neutral Go value handed over as `any`, and rendering it is the one
// reading that cannot fail on either engine — a key is read to be compared or
// reported, not to be summed.
func emitGroupType(s *src, f ir.Fact) {
	s.Blank()
	keys := make([]string, 0, len(f.GroupKeys))
	for _, k := range f.GroupKeys {
		keys = append(keys, k.Name)
	}
	s.Doc(
		fmt.Sprintf("%s is one group of %s: the key, and this group's %s.",
			f.GroupType, f.Name, answerNoun(f)),
		"",
		fmt.Sprintf("A group exists BECAUSE at least one row matched, so an empty set "+
			"yields no groups at all rather than a row of zeroes. The key is %s.",
			strings.Join(keys, " + ")),
	)
	s.L("type %s struct {", f.GroupType)
	for _, k := range f.GroupKeys {
		s.L("\t%s %s", k.Name, k.GoType)
	}
	emitSlotFields(s, f)
	s.L("}")
}

// emitResultType writes what an ungrouped fact answering several numbers
// returns. A struct rather than a tuple: the numbers are named in the spec, and
// a caller reading four positional returns has to count them.
func emitResultType(s *src, f ir.Fact) {
	s.Blank()
	names := make([]string, 0, len(f.Slots))
	for _, sl := range f.Slots {
		names = append(names, sl.Name)
	}
	s.Doc(
		fmt.Sprintf("%s is what %s answers: %s, computed over the same rows in ONE query.",
			f.ResultType, f.Name, strings.Join(names, ", ")),
		"",
		"Asked as one fact each, these would be one query per number over identical "+
			"criteria — and two answers a rule compares would never have been "+
			"guaranteed to be about the same instant.",
	)
	s.L("type %s struct {", f.ResultType)
	emitSlotFields(s, f)
	s.L("}")
}

// emitSlotFields writes one field per number the fact answers, plus the Found
// flag for the ones where zero could pass for an answer nobody computed.
//
// The flag means a slightly different thing in each shape, and the comment says
// which: ungrouped, there may have been no matching row at all; per group, the
// group exists and the aggregated column was null in every row of it.
func emitSlotFields(s *src, f ir.Fact) {
	for _, sl := range f.Slots {
		s.L("\t%s %s", sl.Name, sl.ReturnType)
		if !sl.Found {
			continue
		}
		s.L("\t// %sFound is false when there was nothing to %s, and a zero read",
			sl.Name, sl.Kind)
		s.L("\t// alone would pass for a real %s.", sl.Kind)
		if f.Grouped() {
			s.L("\t//")
			s.L("\t// The group EXISTS — a row matched — so the only way this happens is")
			s.L("\t// %s being null in every row of it.", sl.Field)
		} else {
			s.L("\t//")
			s.L("\t// SQL answers NULL over an empty set, which is what this tells apart")
			s.L("\t// from a %s that really is zero.", sl.Kind)
		}
		s.L("\t%sFound bool", sl.Name)
	}
}

// answerNoun reads the group doc naturally for either shape.
func answerNoun(f ir.Fact) string {
	if len(f.Slots) > 1 {
		return "numbers"
	}
	return "value"
}

func factDoc(f ir.Fact) string {
	if f.Description != "" {
		return f.Description + perEntryNote(f)
	}
	switch f.Kind {
	case "exists":
		return fmt.Sprintf("%s reports whether a matching row already exists.", f.Name)
	case "notExists":
		return fmt.Sprintf("%s reports whether NO matching row exists — the same probe as "+
			"an exists fact, read the other way round so the question is named for the "+
			"problem the rule raises.", f.Name)
	case "count":
		if f.Grouped() {
			return fmt.Sprintf("%s counts the matching rows per group, in one query.", f.Name)
		}
		return fmt.Sprintf("%s counts the matching rows.", f.Name)
	default:
		if f.Grouped() {
			return fmt.Sprintf("%s is the %s of %s per group, computed by the database in one query.",
				f.Name, f.Kind, f.Field)
		}
		doc := fmt.Sprintf("%s is the %s of %s over the matching rows.", f.Name, f.Kind, f.Field)
		if f.ReturnsFound {
			doc += " The second return is false when NO row matched: over an empty set " +
				"there is no " + f.Kind + ", and the zero beside it is not one."
		}
		return doc
	}
}

// perEntryNote says, once, that a fact is asked about ONE ENTRY of a collection
// rather than about the write as a whole.
//
// It matters to whoever writes the body: the question arrives once per entry, so
// a remote call inside it multiplies by the size of the collection, and the
// answer must be about THAT entry rather than about the aggregate. Nothing in
// the signature says so on its own — a `permissionID domain.ID` reads exactly
// like a root field would.
func perEntryNote(f ir.Fact) string {
	if f.Batched() {
		key := f.PerEntry.Key.Field
		note := fmt.Sprintf(" Asked ONCE for the WHOLE of %s and answered per entry, "+
			"keyed by %s.", f.PerEntry.Collection, key)
		if f.PerEntry.Carrier() {
			note += fmt.Sprintf(" An entry contributes more than its key, so the entries "+
				"travel as %s rather than as parallel slices a caller could misalign.",
				f.PerEntry.EntryType)
		}
		// The one thing a map's type cannot say, and the one two services would
		// otherwise read two ways.
		return note + fmt.Sprintf(" A key MISSING from the answer is this fact answering "+
			"NOTHING for that entry, and at the call site Go's zero value settles what "+
			"that means: %s, so nothing is raised. Say \"this entry IS the problem\" by "+
			"putting the answer in the map — never by leaving the key out, which is "+
			"silence rather than a verdict. Where the source could not be reached at "+
			"all, fail: the port returns no error precisely so that decision is made "+
			"here.", absentReading(f.ReturnType))
	}
	var of []string
	seen := map[string]bool{}
	for _, p := range f.Params {
		if p.PerEntry == "" || seen[p.PerEntry] {
			continue
		}
		seen[p.PerEntry] = true
		of = append(of, p.PerEntry)
	}
	if len(of) == 0 {
		return ""
	}
	// A SET operator already asks about the whole collection at once, so the
	// once-per-entry sentence would be false of the very signature it sits on:
	// the parameter is a slice. What is still missing there is the answer,
	// which is one scalar for all of them — the thing perEntry exists to fix.
	if setValued(f) {
		return fmt.Sprintf(" Narrowed by a SET of values from %s, so the question is "+
			"asked ONCE about the whole collection — and answered as ONE %s for all of "+
			"it, which cannot say WHICH entry the answer is about. Where the rule needs "+
			"to name the entry, declare perEntry: <collection>.<field> and the answer "+
			"comes back keyed by it.",
			strings.Join(of, " and "), f.ReturnType)
	}
	return fmt.Sprintf(" Asked ONCE PER ENTRY of %s, so the answer is about that entry "+
		"and the cost of the body is multiplied by the size of the collection. Where "+
		"that cost matters, declare perEntry: <collection>.<field> — the entries arrive "+
		"together and the answer comes back keyed by that field.",
		strings.Join(of, " and "))
}

// setValued reports whether a per-entry filter compares against a SET, which
// makes the whole collection arrive in one call already.
func setValued(f ir.Fact) bool {
	for _, p := range f.Params {
		if p.PerEntry != "" && strings.HasPrefix(p.GoType, "[]") {
			return true
		}
	}
	return false
}

// absentReading names what a missing key reads as, in the fact's own return
// type. It is stated rather than left to the reader because the alternative
// reading — absent means "unresolvable", so treat it as a problem — is what
// hand-written per-entry bodies do today, and two readings of one silence is a
// bug generator.
func absentReading(returnType string) string {
	if returnType == "bool" {
		return "absent reads as false, which is the answer a fact named for the " +
			"PROBLEM wants"
	}
	return "absent reads as the zero " + returnType
}

// factResults renders what a fact answers with. The second return exists only
// where the empty set is ambiguous — see ir.Fact.ReturnsFound — and it is
// rendered here rather than at each call site so the port, the implementation
// and the generated stub cannot disagree about the signature.
func factResults(f ir.Fact) string { return factResultsIn(f, "") }

// factResultsIn is the same, qualified for a package that is not the domain's.
// The group type is DECLARED in the domain beside the port, so infra names it
// through its import alias while the port and the generated stub name it bare.
func factResultsIn(f ir.Fact, pkg string) string {
	if f.Batched() {
		// One question, one answer per entry. A key MISSING from the map is the
		// fact answering nothing for that entry, and for the problem-named bool
		// this language asks for, Go's zero value already reads that correctly
		// at the call site.
		return fmt.Sprintf("map[%s]%s", f.PerEntry.Key.GoType, f.ReturnType)
	}
	if f.Grouped() {
		return "[]" + pkg + f.GroupType
	}
	if f.Multi {
		return pkg + f.ResultType
	}
	if f.ReturnsFound {
		return "(" + f.ReturnType + ", bool)"
	}
	return f.ReturnType
}

// FactSignature is the method as it reads on the port: name, parameters and
// what it answers with.
//
// Exported because the REPORT hands these to whoever writes the bodies, and it
// built the line itself from the parameter list and the return TYPE. That
// stopped being the whole answer the moment a fact could answer a map or take a
// generated carrier — the report would have printed a signature the author
// could not paste. One function, one truth.
func FactSignature(f ir.Fact) string {
	return fmt.Sprintf("%s(%s) %s", f.Name, factParams(f), factResults(f))
}

func factParams(f ir.Fact) string { return factParamsIn(f, "") }

// factParamsIn is the same, qualified for a package that is not the domain's.
// Only the generated ENTRY CARRIER needs it: every other parameter is a scalar
// or a framework type both packages already name the same way.
func factParamsIn(f ir.Fact, pkg string) string {
	var params []string
	for _, p := range f.Params {
		goType := p.GoType
		if pkg != "" && f.PerEntry.Carrier() && p.Role == "per-entry" {
			goType = "[]" + pkg + f.PerEntry.EntryType
		}
		params = append(params, p.Name+" "+goType)
	}
	return strings.Join(params, ", ")
}

func emitServiceImpl(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package infra")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("context"))
	// Pruned when no fact is narrowed by an instant — see emitServiceImports.
	s.L("\t%s", quote("time"))
	s.Blank()
	s.L("\t%s", quote(fwImport("application/configuration")))
	s.L("\t%s", quote(fwImport("application/persistence")))
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\t%s", quote(fwImport("infra/db/command/read")))
	s.L("\t%s", quote(fwImport("infra/db/criteria")))
	s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
	s.L(")")
	s.Blank()

	impl := m.Entity.Pascal + "ServiceImpl"
	iface := "appdomain." + m.Entity.Pascal + "Service"

	s.Doc(
		fmt.Sprintf("%s answers the port's questions straight from the store.", impl),
		"",
		"It carries a request context so its queries run under the request's deadline "+
			"and cancellation. The wired instance is a singleton shared by every "+
			"request, so binding is done by returning a COPY rather than by mutating "+
			"the receiver.",
	)
	s.L("type %s struct {", impl)
	s.L("\tdomain.ServiceBase")
	s.L("\trepo *%sRepository", m.Entity.Pascal)
	s.L("\tctx  *configuration.AppContext")
	s.L("}")
	s.Blank()

	s.L("func New%s(repo *%sRepository) *%s {", impl, m.Entity.Pascal, impl)
	s.L("\treturn &%s{repo: repo}", impl)
	s.L("}")
	s.Blank()

	s.Doc(
		"ScopedService binds this service to one request.",
		"",
		"The framework calls it before the rules run. Without it, every probe would "+
			"query on a background context — outside the request's timeout, its "+
			"cancellation and its trace.",
	)
	s.L("func (s *%s) ScopedService(ctx *configuration.AppContext) domain.Service {", impl)
	s.L("\tbound := *s")
	s.L("\tbound.ctx = ctx")
	s.L("\treturn &bound")
	s.L("}")
	s.Blank()

	s.Doc("queryContext falls back to a background context only for use outside a " +
		"request — tests and background jobs.")
	s.L("func (s *%s) queryContext() context.Context {", impl)
	s.L("\tif s.ctx != nil {")
	s.L("\t\treturn s.ctx")
	s.L("\t}")
	s.L("\treturn context.Background()")
	s.L("}")
	s.Blank()

	for _, f := range m.Service.Facts {
		if f.Manual {
			// Deliberately absent. The method is declared on the port and its body
			// lives in the hand-written file beside this one; leaving a plausible
			// implementation here would be the generator guessing an answer.
			continue
		}
		emitFactImpl(s, m, impl, f)
	}
	emitFactSetHelper(s, m)

	s.L("var (")
	s.L("\t_ %s                             = (*%s)(nil)", iface, impl)
	s.L("\t_ persistence.ScopedServiceProvider = (*%s)(nil)", impl)
	s.L(")")

	return goFile("internal/infra/"+m.Entity.Snake+"_service.go", fsplan.Owned,
		fmt.Sprintf("the %s service implementation", m.Entity.Pascal), s)
}

// factCostNote says what the generated body buys, in the terms of the question
// it actually asks.
//
// It used to say the same sentence for every kind — "the probe exists precisely
// so a YES/NO QUESTION does not pay for full hydration" — on top of sums,
// averages and grouped counts, none of which are yes/no questions. The point
// being made is true of all of them and the words were true of one.
func factCostNote(f ir.Fact) string {
	const instead = "It asks the database the question directly instead of loading " +
		"aggregates and folding the answer in Go"
	switch {
	case existsKind(f.Kind):
		return instead + " — the probe exists precisely so a yes/no question does not " +
			"pay for full hydration."
	case f.Grouped():
		return instead + " — one GROUP BY answers every key at once, where the same " +
			"question asked per key is one query per bucket."
	case f.Multi:
		return instead + " — and every number in ONE pass, so they are all about the " +
			"same instant, which separate queries never guarantee."
	default:
		return instead + " — the aggregate is computed where the rows are, and only " +
			"the number travels back."
	}
}

// anySlotFound reports whether any of the fact's numbers carries a Found flag.
func anySlotFound(f ir.Fact) bool {
	for _, sl := range f.Slots {
		if sl.Found {
			return true
		}
	}
	return false
}

func emitFactImpl(s *src, m *ir.Model, impl string, f ir.Fact) {
	s.Doc(
		fmt.Sprintf("%s %s", f.Name, strings.TrimPrefix(factDoc(f), f.Name+" ")),
		"",
		factCostNote(f),
		"",
		"On a query failure it PANICS, and that is the intended behaviour: the "+
			"pipeline turns the panic into a 500 and the write never happens. Returning "+
			"a plausible answer instead would skip the very invariant this exists to "+
			"enforce.",
	)
	s.L("func (s *%s) %s(%s) %s {", impl, f.Name, factParams(f), factResultsIn(f, "appdomain."))

	emitFactQuery(s, m, f)
	emitFactScope(s, f)
	s.Blank()

	if f.Grouped() {
		emitGroupedFactBody(s, m, f)
		s.L("}")
		s.Blank()
		return
	}

	if existsKind(f.Kind) {
		s.L("\tfound, err := s.repo.Loader.Exists(s.queryContext(), q)")
		s.L("\tif err != nil {")
		s.L("\t\tpanic(%s)", quote(fmt.Sprintf("%s: %s probe failed", m.Entity.Pascal, f.Name)))
		s.L("\t}")
		if f.Kind == "notExists" {
			s.L("\t// The fact is named for the PROBLEM, so the probe's answer is inverted")
			s.L("\t// here rather than at every call site: the rule asks one question and")
			s.L("\t// raises its notification when the answer is yes.")
			s.L("\treturn !found")
		} else {
			s.L("\treturn found")
		}
		s.L("}")
		s.Blank()
		return
	}
	emitScalarFactBody(s, m, f)
	s.L("}")
	s.Blank()
}

// existsKind reports whether the fact answers yes/no. notExists is the same
// probe read the other way round.
func existsKind(kind string) bool { return kind == "exists" || kind == "notExists" }

// emitFactScope writes the archived gate — the framework's own three-way one,
// said out loud in every body so a reader never has to remember which of the
// three is the default.
//
// The default is `all`, and it is the one that is easy to get wrong: a fact
// with no scope key has always included the archived rows, so narrowing it here
// would change what every spec written before the key asks.
func emitFactScope(s *src, f ir.Fact) {
	switch f.Scope {
	case "active":
		s.L("\t// Archived rows do not take part: a removed row must not block a new one.")
		s.L("\t// The active scope is the query default, so nothing is added here.")
	case "archivedOnly":
		s.L("\t// The archived rows and nothing else — what \"was this taken and then")
		s.L("\t// withdrawn\" asks, and the one scope a predicate cannot express: the")
		s.L("\t// gate is the framework's, on whichever column this entity marks with.")
		s.L("\tq = q.OnlyArchived()")
	default:
		s.L("\tq = q.IncludeArchived()")
	}
}

// emitFactQuery binds the criteria the fact narrows by.
//
// The empty case is a shape of its own and not a degenerate one: a fact with no
// filters asks about every row the scope admits, and criteria.And() with NO
// operands is refused by the framework at run time rather than read as "match
// everything". A query with no predicate is how that is said — Where(nil) — and
// the loader has always accepted it.
//
// It matters more than it looks: nothing in generate → gofmt → vet → build
// exercises a query, so a fact with no filters compiled, shipped, and panicked
// the first time a rule asked it.
func emitFactQuery(s *src, m *ir.Model, f ir.Fact) {
	var self *ir.FactParam
	for i := range f.Params {
		if f.Params[i].Role == "exclude-self" {
			self = &f.Params[i]
		}
	}
	if len(f.Where) == 0 && self == nil {
		s.L("\t// No narrowing of its own: the question is about every row the archived")
		s.L("\t// scope admits, which is a query with no predicate rather than an empty")
		s.L("\t// conjunction — the framework refuses that one.")
		s.L("\tq := criteria.Where(nil)")
		return
	}

	s.L("\tconds := []criteria.Expr{")
	for _, c := range f.Where {
		for _, line := range factCondLines(m, c, "\t\t") {
			s.L("%s", line)
		}
	}
	s.L("\t}")
	if self != nil {
		s.L("\t// Exclude the row being updated: without this, updating a unique field")
		s.L("\t// would always report the row colliding with itself.")
		s.L("\tif !%s.IsEmpty() {", self.Name)
		s.L("\t\tconds = append(conds, criteria.Ne(%s, %s))", quote("ID"), self.Name)
		s.L("\t}")
	}
	if len(f.Where) == 0 {
		// The only condition is appended behind a run-time guard, so the slice
		// is empty on an insert — where there is no row yet to exclude.
		s.L("\t// On an insert nothing was appended above, and an empty conjunction is")
		s.L("\t// not a predicate — the query then carries none at all.")
		s.L("\tvar where criteria.Expr")
		s.L("\tif len(conds) > 0 {")
		s.L("\t\twhere = criteria.And(conds...)")
		s.L("\t}")
		s.L("\tq := criteria.Where(where)")
		return
	}
	s.L("\tq := criteria.Where(criteria.And(conds...))")
}

// emitScalarFactBody writes the ungrouped answer: ONE query computing every
// number the fact declares.
//
// One query for all of them is the whole point of the plural form. The
// framework's loader takes as many specs as it is handed and the aggregate
// specs CARRY their answers — they are not out-parameters — so the body binds
// one local per number and reads the values back off them.
func emitScalarFactBody(s *src, m *ir.Model, f ir.Fact) {
	for _, sl := range f.Slots {
		if sl.Kind != "count" {
			s.L("\t// %s over %s, computed in the database rather than in Go.", sl.Kind, sl.Field)
		}
		s.L("\t%s := read.%s(%s)", sl.Var, sl.AggFn, aggArgs(sl))
	}
	s.L("\tif err := s.repo.Loader.Aggregate(s.queryContext(), q, %s); err != nil {",
		strings.Join(slotVars(f), ", "))
	s.L("\t\tpanic(%s)", quote(fmt.Sprintf("%s: %s probe failed", m.Entity.Pascal, f.Name)))
	s.L("\t}")

	if f.Multi {
		s.L("\treturn appdomain.%s{", f.ResultType)
		for _, sl := range f.Slots {
			s.L("\t\t%s: %s.Value,", sl.Name, sl.Var)
			if sl.Found {
				s.L("\t\t%sFound: %s.Found,", sl.Name, sl.Var)
			}
		}
		s.L("\t}")
		return
	}
	one := f.Slots[0]
	if f.ReturnsFound {
		s.L("\t// Found is the answer to \"was there anything to %s?\" — over no rows", one.Kind)
		s.L("\t// SQL says NULL, and a zero returned alone would read as a real result.")
		s.L("\treturn %s.Value, %s.Found", one.Var, one.Var)
		return
	}
	s.L("\treturn %s.Value", one.Var)
}

// aggArgs renders what a builder takes: the aggregated field, or nothing at all
// for a count, which counts rows rather than values.
func aggArgs(sl ir.FactSlot) string {
	if sl.Kind == "count" {
		return ""
	}
	return quote(sl.Field)
}

// slotVars names the locals, in declaration order — the order the specs are
// handed to the loader and the order the answer's fields are written.
func slotVars(f ir.Fact) []string {
	out := make([]string, 0, len(f.Slots))
	for _, sl := range f.Slots {
		out = append(out, sl.Var)
	}
	return out
}

// factCondLines renders one node of a fact's criteria tree, as the lines of a
// single entry in the conds slice.
//
// It is a translation and nothing more: the IR already decided what the query
// asks, and every node here has exactly one builder in the framework's criteria
// package. That is why the vocabulary is closed at the spec — a node this
// function could not name would be a query the store has no way to receive.
func factCondLines(m *ir.Model, c ir.FactCond, indent string) []string {
	if c.Leaf() {
		return []string{indent + factLeafExpr(m, c) + ","}
	}
	// criteria.Not takes ONE expression, so several nodes under a `not` are
	// ANDed before it — the reading the spec's key spells out.
	inner := c.Nodes
	open := fmt.Sprintf("%scriteria.%s(", indent, factGroupFn(c.Group))
	if c.Group == "not" && len(inner) > 1 {
		out := []string{open, indent + "\tcriteria.And("}
		for _, n := range inner {
			out = append(out, factCondLines(m, n, indent+"\t\t")...)
		}
		return append(out, indent+"\t),", indent+"),")
	}
	out := []string{open}
	for _, n := range inner {
		out = append(out, factCondLines(m, n, indent+"\t")...)
	}
	return append(out, indent+"),")
}

// factGroupFn names the connective's builder. `not` takes one expression, and
// the caller has already reduced several to one.
func factGroupFn(group string) string {
	switch group {
	case "or":
		return "Or"
	case "not":
		return "Not"
	default:
		return "And"
	}
}

// factLeafExpr renders one comparison.
//
// The value is the WIRE value in every branch — the parameter's own type is the
// column's underlying scalar, and a pinned literal was rendered the same way.
// criteria binds values, not domain types, and the translator resolves the Go
// FIELD name to a column, which is why the first argument is never a column.
func factLeafExpr(m *ir.Model, c ir.FactCond) string {
	field := quote(c.Field)
	switch c.Op {
	case "isnull":
		return fmt.Sprintf("criteria.IsNull(%s)", field)
	case "notnull":
		return fmt.Sprintf("criteria.NotNull(%s)", field)
	case "in", "nin":
		fn := "In"
		if c.Op == "nin" {
			fn = "Nin"
		}
		if len(c.Literals) > 0 {
			return fmt.Sprintf("criteria.%s(%s, %s)", fn, field, strings.Join(c.Literals, ", "))
		}
		// The set arrives typed and criteria takes ...any, because a comparison
		// is over VALUES rather than over one Go type. An empty set is a legal
		// answer and not an error: the framework renders it as the predicate
		// that matches nothing (and `nin` as the one that matches everything),
		// which is the same reading both of its stores give it.
		return fmt.Sprintf("criteria.%s(%s, %s(%s)...)", fn, field, factSetFn(m), c.Param)
	}
	return fmt.Sprintf("criteria.%s(%s, %s)", factLeafFn(c.Op), field, factLeafValue(c))
}

// factLeafValue is the one value a comparison takes: the parameter, or the
// constant the spec pinned in its place.
func factLeafValue(c ir.FactCond) string {
	if len(c.Literals) > 0 {
		return c.Literals[0]
	}
	return c.Param
}

// factLeafFn maps the spec's operator to the framework's builder. The names
// agree everywhere but the case, which is deliberate: the spec is written in
// the vocabulary criteria already publishes, so an author reading the emitted
// query recognises what they wrote.
func factLeafFn(op string) string {
	switch op {
	case "startswith":
		return "StartsWith"
	case "endswith":
		return "EndsWith"
	default:
		return pascal(op)
	}
}

// factSetFn names the per-entity widener. It is per-entity because every
// generated service implementation lands in the SAME infra package, so one
// shared name would be a redeclaration the moment a second entity asked a set
// question.
func factSetFn(m *ir.Model) string {
	return naming.Camel(m.Entity.Pascal) + "CriteriaSet"
}

// emitFactSetHelper writes the widener, for the services that ask a set
// question at all.
//
// It exists because criteria takes ...any and the port takes a typed slice, and
// those are both right: the port speaks the domain's vocabulary, and the query
// DSL compares values whose Go type it has no reason to know. Spreading one
// into the other is three lines nobody should write per fact.
func emitFactSetHelper(s *src, m *ir.Model) {
	if !factUsesSet(m) {
		return
	}
	s.Blank()
	s.Doc(
		fmt.Sprintf("%s widens a typed set into the values criteria compares against.",
			factSetFn(m)),
		"",
		"The port takes a typed slice because that is the question the domain asks; "+
			"the DSL takes ...any because a comparison is over values and not over one "+
			"Go type. This is the seam between the two, written once.",
	)
	s.L("func %s[T any](vs []T) []any {", factSetFn(m))
	s.L("\tout := make([]any, 0, len(vs))")
	s.L("\tfor _, v := range vs {")
	s.L("\t\tout = append(out, v)")
	s.L("\t}")
	s.L("\treturn out")
	s.L("}")
	s.Blank()
}

// factUsesSet reports whether any generated query spreads a set parameter.
func factUsesSet(m *ir.Model) bool {
	var used bool
	var walk func(nodes []ir.FactCond)
	walk = func(nodes []ir.FactCond) {
		for _, c := range nodes {
			if !c.Leaf() {
				walk(c.Nodes)
				continue
			}
			if (c.Op == "in" || c.Op == "nin") && c.Param != "" {
				used = true
			}
		}
	}
	for _, f := range m.Service.Facts {
		if f.Manual {
			continue
		}
		walk(f.Where)
	}
	return used
}

// emitGroupedFactBody writes the per-group answer: ONE select, grouped by the
// declared keys, over the same criteria as the ungrouped form.
//
// The specs handed to AggregateBy are TEMPLATES — they carry no result after
// the call, and each group's own copy is read back through GroupResult with the
// template as the handle. Reusing one template instance per fact is what makes
// that lookup work, so the variable is declared once, above the loop.
func emitGroupedFactBody(s *src, m *ir.Model, f ir.Fact) {
	keys := make([]string, 0, len(f.GroupKeys))
	for _, k := range f.GroupKeys {
		keys = append(keys, quote(k.Field))
	}
	s.L("\tby := read.By(%s)", strings.Join(keys, ", "))
	for _, sl := range f.Slots {
		s.L("\t%s := read.%s(%s)", sl.Var, sl.AggFn, aggArgs(sl))
	}
	s.L("\tgroups, err := s.repo.Loader.AggregateBy(s.queryContext(), q, by, %s)",
		strings.Join(slotVars(f), ", "))
	s.L("\tif err != nil {")
	s.L("\t\tpanic(%s)", quote(fmt.Sprintf("%s: %s probe failed", m.Entity.Pascal, f.Name)))
	s.L("\t}")
	s.Blank()
	s.L("\t// One entry per distinct key. An empty set yields NO groups at all, so")
	s.L("\t// there is no row of zeroes to tell apart from a real one.")
	if anySlotFound(f) {
		// Only where one exists. A count carries no Found — it cannot — and
		// naming it in the file the developer is meant to read sends them
		// looking for a field that is not in the struct above.
		s.L("\t// Where a group's own scalar can still be null, its Found says so.")
	}
	s.L("\tout := make([]appdomain.%s, 0, len(groups))", f.GroupType)
	s.L("\tfor _, g := range groups {")
	s.L("\t\tout = append(out, appdomain.%s{", f.GroupType)
	for _, k := range f.GroupKeys {
		s.L("\t\t\t%s: g.KeyString(%s),", k.Name, quote(k.Field))
	}
	for _, sl := range f.Slots {
		s.L("\t\t\t%s: read.GroupResult(g, %s).Value,", sl.Name, sl.Var)
		if sl.Found {
			// The aggregated column is nullable, so a group whose every row
			// leaves it null answers NULL — which the carrier reports as
			// Found=false with a zero Value. Dropping the flag here reported
			// "nothing to average" as an average of zero.
			s.L("\t\t\t%sFound: read.GroupResult(g, %s).Found,", sl.Name, sl.Var)
		}
	}
	s.L("\t\t})")
	s.L("\t}")
	s.L("\treturn out")
}

func pascal(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// emitServiceStubFile is the service-side twin of <entity>_rules_manual.go.
//
// Same contract as the rules hook: written once, never regenerated, carrying
// the spec's own words about what each method must answer. The difference is
// that this one is not optional — the port declares these methods, so the
// package does not compile until they exist.
func emitServiceStubFile(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	// The header already says whose file this is and what happens while it is
	// empty. What it cannot carry is the practical part: WHERE the answer comes
	// from, and what the port's shape forbids.
	s.Doc(
		"How to answer one of these depends on where the truth lives: another "+
			"service of your own is normally gRPC, a third-party API is the HTTP "+
			"client, and data you need to filter and sort locally is usually better "+
			"mirrored in through an upstream subscription than fetched per request.",
		"",
		"If the truth is THIS database — a question the spec could not phrase "+
			"declaratively — ask it the way the generated facts beside this file do: "+
			"the loader's existence probe for a yes/no, its aggregate DSL for a count, "+
			"total, average or extreme, and the grouped form for per-key facts. Loading "+
			"the rows and folding the answer in Go reads a whole table to compute what "+
			"one SELECT computes, on the write path.",
		"",
		"If it is this database but ANOTHER TABLE — another aggregate's child table, a "+
			"control table, a lookup — this repository cannot reach it, and hand-written "+
			"SQL is not the next step. Check whether the pinned framework documents a "+
			"DIRECT schema (one table, no aggregate behind it): it anchors the same "+
			"probes and the same aggregate DSL on that table, in every dialect, and "+
			"joins the caller's open transaction. Declaring a whole aggregate for a "+
			"table that exists only to be counted is the other wrong answer.",
		"",
		"Two constraints the port imposes. The method returns a plain value and NO "+
			"error, so decide here what an unavailable source means — failing loudly is "+
			"the safe default, because returning a plausible answer skips the very rule "+
			"this exists to enforce. And it runs inside the write, so a slow call is a "+
			"slow write: use the bound context.",
	)
	s.Blank()
	s.L("package infra")
	s.Blank()
	// The stub is a HOOK — written once and owned by the author — but what is
	// handed over still has to compile, or a first run reads as a broken
	// generation rather than as a TODO. A fact filtered by an id, and every
	// excludeSelf fact, take a domain.ID; one narrowed by an instant takes a
	// time.Time. The block was missing entirely, and then missing "time".
	// gofile prunes whichever the specs' facts do not take.
	emitStubImports(s, m)
	s.Blank()

	impl := m.Service.Impl
	for _, f := range m.Service.Facts {
		if !f.Manual {
			continue
		}
		s.Blank()
		for _, line := range wrap(f.Description, 72) {
			s.L("// %s", line)
		}
		s.L("//")
		s.L("// TODO(%s): implement. The request context is s.queryContext().", f.Name)
		s.L("func (s *%s) %s(%s) %s {", impl, f.Name,
			factParamsIn(f, "appdomain."), factResults(f))
		s.L("\tpanic(%s)", quote(fmt.Sprintf(
			"%s.%s is not implemented yet — see the generation report", m.Entity.Pascal, f.Name)))
		s.L("}")
	}

	f, err := goFile("internal/infra/"+m.Entity.Snake+"_service_manual.go", fsplan.Hook,
		fmt.Sprintf("the hand-written facts for %s (%d to implement)",
			m.Entity.Pascal, countManual(m)), s)
	if err != nil {
		return f, err
	}
	f.Consequence = "Unlike the rules hook, these are NOT quiet: each one panics until " +
		"it is written. The service builds, boots and serves everything else, and the " +
		"failure arrives the moment a rule asks — as a 500, with the write rolled back."
	return f, nil
}

func countManual(m *ir.Model) int {
	n := 0
	for _, f := range m.Service.Facts {
		if f.Manual {
			n++
		}
	}
	return n
}
