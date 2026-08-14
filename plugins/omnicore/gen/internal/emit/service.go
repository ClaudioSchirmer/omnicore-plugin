package emit

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
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
	s.header(m, fmt.Sprintf("The %s domain service port.", m.Entity.Pascal))
	s.Blank()
	s.L("package domain")
	s.Blank()
	s.L("import %s", quote(fwImport("domain")))
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
		s.L("\t%s(%s) %s", f.Name, factParams(f), f.ReturnType)
	}
	s.L("}")

	return goFile("internal/domain/"+m.Entity.Snake+"_service.go", fsplan.Owned,
		fmt.Sprintf("the %s service port (%d fact(s))", m.Entity.Pascal, len(m.Service.Facts)), s)
}

func factDoc(f ir.Fact) string {
	if f.Description != "" {
		return f.Description
	}
	switch f.Kind {
	case "exists":
		return fmt.Sprintf("%s reports whether a matching row already exists.", f.Name)
	case "count":
		return fmt.Sprintf("%s counts the matching rows.", f.Name)
	default:
		return fmt.Sprintf("%s is the %s of %s over the matching rows.", f.Name, f.Kind, f.Field)
	}
}

func factParams(f ir.Fact) string {
	var params []string
	for _, p := range f.Params {
		params = append(params, p.Name+" "+p.GoType)
	}
	return strings.Join(params, ", ")
}

func emitServiceImpl(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.header(m, fmt.Sprintf("The %s domain service, implemented.", m.Entity.Pascal))
	s.Blank()
	s.L("package infra")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("context"))
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

	s.L("var (")
	s.L("\t_ %s                             = (*%s)(nil)", iface, impl)
	s.L("\t_ persistence.ScopedServiceProvider = (*%s)(nil)", impl)
	s.L(")")

	return goFile("internal/infra/"+m.Entity.Snake+"_service.go", fsplan.Owned,
		fmt.Sprintf("the %s service implementation", m.Entity.Pascal), s)
}

func emitFactImpl(s *src, m *ir.Model, impl string, f ir.Fact) {
	s.Doc(
		fmt.Sprintf("%s %s", f.Name, strings.TrimPrefix(factDoc(f), f.Name+" ")),
		"",
		"It asks the database the question directly instead of loading aggregates and "+
			"counting them in Go — the probe exists precisely so a yes/no question does "+
			"not pay for full hydration.",
		"",
		"On a query failure it PANICS, and that is the intended behaviour: the "+
			"pipeline turns the panic into a 500 and the write never happens. Returning "+
			"a plausible answer instead would skip the very invariant this exists to "+
			"enforce.",
	)
	s.L("func (s *%s) %s(%s) %s {", impl, f.Name, factParams(f), f.ReturnType)

	s.L("\tconds := []criteria.Expr{")
	for _, p := range f.Params {
		if p.Role == "exclude-self" {
			continue
		}
		s.L("\t\tcriteria.Eq(%s, %s),", quote(p.Field), p.Name)
	}
	s.L("\t}")
	for _, p := range f.Params {
		if p.Role != "exclude-self" {
			continue
		}
		s.L("\t// Exclude the row being updated: without this, updating a unique field")
		s.L("\t// would always report the row colliding with itself.")
		s.L("\tif !%s.IsEmpty() {", p.Name)
		s.L("\t\tconds = append(conds, criteria.Ne(%s, %s))", quote("ID"), p.Name)
		s.L("\t}")
	}
	s.L("\tq := criteria.Where(criteria.And(conds...))")
	if f.ActiveOnly {
		s.L("\t// Archived rows do not take part: a removed row must not block a new one.")
		s.L("\t// The active scope is the query default, so nothing is added here.")
	} else {
		s.L("\tq = q.IncludeArchived()")
	}
	s.Blank()

	switch f.Kind {
	case "exists":
		s.L("\tfound, err := s.repo.Loader.Exists(s.queryContext(), q)")
		s.L("\tif err != nil {")
		s.L("\t\tpanic(%s)", quote(fmt.Sprintf("%s: %s probe failed", m.Entity.Pascal, f.Name)))
		s.L("\t}")
		s.L("\treturn found")
	case "count":
		s.L("\tvar n int64")
		s.L("\tif err := s.repo.Loader.Aggregate(s.queryContext(), q, read.Count(&n)); err != nil {")
		s.L("\t\tpanic(%s)", quote(fmt.Sprintf("%s: %s probe failed", m.Entity.Pascal, f.Name)))
		s.L("\t}")
		s.L("\treturn n")
	default:
		s.L("\t// %s over %s, computed in the database rather than in Go.", f.Kind, f.Field)
		s.L("\tvar out %s", f.ReturnType)
		s.L("\tif err := s.repo.Loader.Aggregate(s.queryContext(), q,")
		s.L("\t\tread.%s(%s, &out)); err != nil {", pascal(f.Kind), quote(f.Field))
		s.L("\t\tpanic(%s)", quote(fmt.Sprintf("%s: %s probe failed", m.Entity.Pascal, f.Name)))
		s.L("\t}")
		s.L("\treturn out")
	}
	s.L("}")
	s.Blank()
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
		"Two constraints the port imposes. The method returns a plain value and NO "+
			"error, so decide here what an unavailable source means — failing loudly is "+
			"the safe default, because returning a plausible answer skips the very rule "+
			"this exists to enforce. And it runs inside the write, so a slow call is a "+
			"slow write: use the bound context.",
	)
	s.Blank()
	s.L("package infra")
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
		s.L("func (s *%s) %s(%s) %s {", impl, f.Name, factParams(f), f.ReturnType)
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
