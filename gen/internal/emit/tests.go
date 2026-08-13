package emit

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
)

// Tests are generated from the same declarations the code is generated from,
// which bounds what they can prove: they show the rules the SPEC declared are
// wired and fire, not that those were the right rules.
//
// They are still worth generating. Every declared rule gets a case that trips
// it, so a rule that silently stopped firing — the failure nobody notices —
// fails the build instead. What they deliberately do NOT cover is the hook
// file: the generator does not know what those rules mean, and a test written
// without that knowledge would assert the implementation rather than the
// intent.
func emitTests(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File

	dom, err := emitDomainTests(m)
	if err != nil {
		return nil, err
	}
	out = append(out, dom)

	if len(m.WriteOps()) > 0 {
		cmd, err := emitCommandTests(m)
		if err != nil {
			return nil, err
		}
		out = append(out, cmd)
	}
	if len(m.ValueObjects) > 0 {
		vo, err := emitVOTests(m)
		if err != nil {
			return nil, err
		}
		out = append(out, vo)
	}
	return out, nil
}

func emitDomainTests(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.header(m, fmt.Sprintf("Tests for %s's rules.", m.Entity.Pascal))
	s.Blank()
	s.L("package domain")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("errors"))
	s.L("\t%s", quote("strings"))
	s.L("\t%s", quote("testing"))
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/vos")))
	s.L(")")
	s.Blank()

	emitTestHelpers(s, m)
	emitServiceStub(s, m)
	emitValidEntityBuilder(s, m)
	emitRuleCases(s, m)

	return goFile("internal/domain/"+m.Entity.Snake+"_test.go", fsplan.Owned,
		fmt.Sprintf("tests for %s's rules", m.Entity.Pascal), s)
}

// emitTestHelpers writes the two helpers every case needs: one to read the
// notifications out of a rejection, and one to assert a specific field failed.
//
// Asserting on the FIELD rather than on "an error happened" is what makes a
// failing test say which rule broke.
func emitTestHelpers(s *src, m *ir.Model) {
	s.Doc(
		"rejectedFields lists the fields a rejection blamed.",
		"",
		"Rules report through a carrier rather than by returning the first problem, so "+
			"a rejection can name several fields at once — asserting on the set is what "+
			"tells a failing test which rule actually broke.",
	)
	s.L("func %sRejectedFields(err error) []string {", m.Entity.Camel)
	s.L("\tvar carrier domain.NotificationCarrier")
	s.L("\tif !errors.As(err, &carrier) {")
	s.L("\t\treturn nil")
	s.L("\t}")
	s.L("\tvar out []string")
	s.L("\tfor _, ctx := range carrier.NotificationContexts() {")
	s.L("\t\tfor _, msg := range ctx.Messages() {")
	s.L("\t\t\tname := msg.FieldName")
	s.L("\t\t\tif msg.Override != \"\" {")
	s.L("\t\t\t\tname = msg.Override")
	s.L("\t\t\t}")
	s.L("\t\t\tfor _, seg := range msg.Path {")
	s.L("\t\t\t\tif seg.Name != \"\" {")
	s.L("\t\t\t\t\tname = seg.Name")
	s.L("\t\t\t\t}")
	s.L("\t\t\t}")
	s.L("\t\t\tout = append(out, name)")
	s.L("\t\t}")
	s.L("\t}")
	s.L("\treturn out")
	s.L("}")
	s.Blank()

	s.L("func %sBlames(err error, field string) bool {", m.Entity.Camel)
	s.L("\tfor _, f := range %sRejectedFields(err) {", m.Entity.Camel)
	s.L("\t\tif f == field || strings.HasSuffix(f, \".\"+field) {")
	s.L("\t\t\treturn true")
	s.L("\t\t}")
	s.L("\t}")
	s.L("\treturn false")
	s.L("}")
	s.Blank()
}

// emitValidEntityBuilder produces an aggregate that passes every declared rule.
//
// Every negative case starts from it and breaks exactly ONE thing, so a failure
// points at the rule under test instead of at whatever else happened to be
// wrong.
func emitValidEntityBuilder(s *src, m *ir.Model) {
	s.Doc(
		fmt.Sprintf("valid%s returns an aggregate that satisfies every declared rule.", m.Entity.Pascal),
		"",
		"Each negative case below starts here and breaks one thing, so the failure "+
			"points at the rule under test rather than at unrelated invalid state.",
	)
	s.L("func valid%s() *%s {", m.Entity.Pascal, m.Entity.Pascal)
	s.L("\treturn &%s{", m.Entity.Pascal)
	for _, f := range m.AllOwnerFields() {
		s.L("\t\t%s: %s,", f.Name, sampleValue(f))
	}
	s.L("\t}")
	s.L("}")
	s.Blank()

	s.Doc(fmt.Sprintf("A valid %s must be accepted — otherwise every negative case "+
		"below would pass for the wrong reason.", m.Entity.Pascal))
	s.L("func TestValid%sIsAccepted(t *testing.T) {", m.Entity.Pascal)
	s.L("\tif _, err := domain.GetInsertable(valid%s(), %s, %s); err != nil {",
		m.Entity.Pascal, serviceArg(m), quote(insertAction(m)))
	s.L("\t\tt.Fatalf(\"a valid %s was rejected: %%v (fields: %%v)\", err, %sRejectedFields(err))",
		m.Entity.Pascal, m.Entity.Camel)
	s.L("\t}")
	s.L("}")
	s.Blank()
}

// insertAction is the label the framework passes for the insert path. A shared
// base takes the upsert label because the identity may already exist.
func insertAction(m *ir.Model) string {
	if m.IsRole() {
		return "GetUpsertable"
	}
	return "GetInsertable"
}

// serviceArg passes the service the rules consult.
//
// It cannot be nil when the entity requires one: the framework refuses the
// write before any rule runs, and every case would then fail for that reason
// instead of the one under test.
func serviceArg(m *ir.Model) string {
	if m.Service != nil {
		return "&stub" + m.Entity.Pascal + "Service{}"
	}
	return "nil"
}

// emitServiceStub answers every probe with "nothing found".
//
// That is the deliberate default: it lets a valid aggregate through, so the
// cases below fail for the rule they are testing rather than for a duplicate
// the stub invented. Asserting the service-backed rules themselves needs a real
// store, which belongs to the end-to-end suite, not here.
func emitServiceStub(s *src, m *ir.Model) {
	if m.Service == nil {
		return
	}
	name := "stub" + m.Entity.Pascal + "Service"
	s.Doc(
		fmt.Sprintf("%s answers every probe with \"nothing found\".", name),
		"",
		"A nil service is not an option: the framework refuses the write before any "+
			"rule runs, and every case here would fail for that instead of for the rule "+
			"under test. Answering \"nothing found\" lets a valid aggregate through.",
	)
	s.L("type %s struct {", name)
	s.L("	domain.ServiceBase")
	s.L("}")
	s.Blank()
	for _, f := range m.Service.Facts {
		s.L("func (%s) %s(%s) %s { return %s }",
			name, f.Name, stubParams(f), f.ReturnType, stubZero(f.ReturnType))
	}
	s.Blank()
}

// stubParams mirrors the port's signature exactly. An approximate one does not
// satisfy the interface, and the assertion in the rule panics at run time
// rather than failing to compile.
func stubParams(f ir.Fact) string {
	var out []string
	for i, p := range f.Params {
		out = append(out, fmt.Sprintf("_ %s", p.GoType))
		_ = i
	}
	return strings.Join(out, ", ")
}

func stubZero(t string) string {
	switch t {
	case "bool":
		return "false"
	case "string":
		return `""`
	default:
		return "0"
	}
}

func emitRuleCases(s *src, m *ir.Model) {
	for _, clause := range m.Clauses {
		for _, rule := range clause.Rules {
			emitRuleCase(s, m, clause.Gate, rule)
		}
	}
}

func emitRuleCase(s *src, m *ir.Model, gate string, rule ir.Rule) {
	if len(rule.Fields) == 0 {
		return
	}
	f := rule.Fields[0]

	switch rule.Kind {
	case "required":
		if f.SpecType == "bool" {
			return
		}
		s.Doc(fmt.Sprintf("%s is required, so an empty one must be refused.", f.Name))
		s.L("func Test%s_%s_IsRequired(t *testing.T) {", m.Entity.Pascal, f.Name)
		s.L("\te := valid%s()", m.Entity.Pascal)
		s.L("\te.%s = %s", f.Name, zeroValue(f))
		s.L("\t_, err := domain.GetInsertable(e, %s, %s)", serviceArg(m), quote(insertAction(m)))
		s.L("\tif err == nil {")
		s.L("\t\tt.Fatal(\"an empty %s was accepted\")", f.Name)
		s.L("\t}")
		s.L("\tif !%sBlames(err, %s) {", m.Entity.Camel, quote(f.Name))
		s.L("\t\tt.Errorf(\"the rejection should name %s, it named %%v\", %sRejectedFields(err))", f.Name, m.Entity.Camel)
		s.L("\t}")
		s.L("}")
		s.Blank()

	case "range":
		if rule.Max != nil {
			emitBoundCase(s, m, rule, f, "Above", overMax(f, *rule.Max))
		}
		if rule.Min != nil {
			emitBoundCase(s, m, rule, f, "Below", underMin(f, *rule.Min))
		}

	case "immutable":
		// The rule compares against the pre-write snapshot, which only exists on
		// the update path — asserting it through an insert would prove nothing.
		s.Doc(fmt.Sprintf("%s cannot change once set.", f.Name),
			"",
			"It is driven through the update path because the rule reads the previous "+
				"value, and on an insert there is no previous value to read.")
		s.L("func Test%s_%s_IsImmutable(t *testing.T) {", m.Entity.Pascal, f.Name)
		s.L("\te := valid%s()", m.Entity.Pascal)
		s.L("\t_, err := domain.GetUpdatable(e, func(x *%s) error {", m.Entity.Pascal)
		s.L("\t\tx.%s = %s", f.Name, alternateValue(f))
		s.L("\t\treturn nil")
		s.L("\t}, %s, %s)", serviceArg(m), quote("GetUpdatable"))
		s.L("\tif err == nil {")
		s.L("\t\tt.Fatal(\"changing %s was accepted\")", f.Name)
		s.L("\t}")
		s.L("\tif !%sBlames(err, %s) {", m.Entity.Camel, quote(f.Name))
		s.L("\t\tt.Errorf(\"the rejection should name %s, it named %%v\", %sRejectedFields(err))", f.Name, m.Entity.Camel)
		s.L("\t}")
		s.L("}")
		s.Blank()

	case "comparison":
		if rule.Other == nil {
			return
		}
		s.Doc(fmt.Sprintf("%s must stay %s %s.", f.Name, rule.Operator, rule.Other.Name))
		s.L("func Test%s_%s_Comparison(t *testing.T) {", m.Entity.Pascal, f.Name)
		s.L("\te := valid%s()", m.Entity.Pascal)
		s.L("\te.%s = %s", f.Name, pointerize(f, violatingComparison(f, *rule.Other, rule.Operator)))
		s.L("\t_, err := domain.GetInsertable(e, %s, %s)", serviceArg(m), quote(insertAction(m)))
		s.L("\tif err == nil {")
		s.L("\t\tt.Fatal(\"the comparison between %s and %s was not enforced\")", f.Name, rule.Other.Name)
		s.L("\t}")
		s.L("}")
		s.Blank()
	}
}

func emitBoundCase(s *src, m *ir.Model, rule ir.Rule, f ir.Field, side, value string) {
	s.Doc(fmt.Sprintf("%s is bounded, so a value %s the bound must be refused.",
		f.Name, strings.ToLower(side)))
	s.L("func Test%s_%s_%sBound(t *testing.T) {", m.Entity.Pascal, f.Name, side)
	s.L("\te := valid%s()", m.Entity.Pascal)
	s.L("\te.%s = %s", f.Name, value)
	s.L("\t_, err := domain.GetInsertable(e, %s, %s)", serviceArg(m), quote(insertAction(m)))
	s.L("\tif err == nil {")
	s.L("\t\tt.Fatal(\"an out-of-range %s was accepted\")", f.Name)
	s.L("\t}")
	s.L("\tif !%sBlames(err, %s) {", m.Entity.Camel, quote(f.Name))
	s.L("\t\tt.Errorf(\"the rejection should name %s, it named %%v\", %sRejectedFields(err))", f.Name, m.Entity.Camel)
	s.L("\t}")
	s.L("}")
	s.Blank()
}

func emitCommandTests(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.header(m, "Tests for the command mappers.")
	s.Blank()
	s.L("package commands")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("testing"))
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote(fwImport("application/configuration")))
	s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
	s.L(")")
	s.Blank()

	if op := m.Op("insert"); op != nil {
		s.Doc("The insert mapper must carry every field through.",
			"",
			"A field silently dropped here is the quietest bug in the stack: the write "+
				"succeeds and the value is simply not there.")
		s.L("func TestInsert%sMapsEveryField(t *testing.T) {", m.Entity.Pascal)
		s.L("\tctx := &configuration.AppContext{}")
		s.L("\tc := &%s{", op.CommandType)
		for _, f := range m.AllOwnerFields() {
			s.L("\t\t%s: %s,", f.Name, wireSample(f))
		}
		s.L("\t}")
		if op.InputMethod == "ToEntity" {
			s.L("\te, err := c.ToEntity(ctx)")
			s.L("\tif err != nil {")
			s.L("\t\tt.Fatalf(%s, err)", quote("ToEntity: %v"))
			s.L("\t}")
		} else {
			s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
			s.L("\tif err := c.ApplyTo(ctx, e); err != nil {")
			s.L("\t\tt.Fatalf(%s, err)", quote("ApplyTo: %v"))
			s.L("\t}")
		}
		for _, f := range m.AllOwnerFields() {
			if f.Nullable {
				continue
			}
			s.L("\tif %s != %s {", entityAsWire(f, "e"), wireSample(f))
			s.L("\t\tt.Errorf(\"%s did not survive the mapper\")", f.Name)
			s.L("\t}")
		}
		s.L("}")
		s.Blank()
	}

	if op := m.Op("patch"); op != nil {
		s.Doc("A partial update must leave absent fields alone.",
			"",
			"This is the whole contract of the verb: sending nothing must not blank "+
				"anything out.")
		s.L("func TestPatch%sLeavesAbsentFieldsAlone(t *testing.T) {", m.Entity.Pascal)
		s.L("\tctx := &configuration.AppContext{}")
		s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
		first := m.AllOwnerFields()[0]
		s.L("\te.%s = %s", first.Name, entitySample(first))
		s.L("\tc := &%s{} // nothing sent", op.CommandType)
		s.L("\tif err := c.ApplyPartiallyTo(ctx, e); err != nil {")
		s.L("\t\tt.Fatalf(%s, err)", quote("ApplyPartiallyTo: %v"))
		s.L("\t}")
		s.L("\tif e.%s != %s {", first.Name, entitySample(first))
		s.L("\t\tt.Error(\"an absent field was overwritten\")")
		s.L("\t}")
		s.L("}")
		s.Blank()
	}
	s.L("var _ = time.Time{}")

	return goFile("internal/application/commands/"+m.Entity.Snake+"_commands_test.go",
		fsplan.Owned, "tests for the command mappers", s)
}

func emitVOTests(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.header(m, "Tests for the value objects.")
	s.Blank()
	s.L("package vos")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("testing"))
	s.L("\t%s", quote(fwImport("domain")))
	s.L(")")
	s.Blank()

	for _, vo := range m.ValueObjects {
		if vo.Kind == "enum" {
			s.Doc(fmt.Sprintf("%s declares its members; the framework validates membership "+
				"against exactly this set.", vo.Name))
			s.L("func Test%sMembers(t *testing.T) {", vo.Name)
			s.L("\tmembers := %s(%s).Values()", vo.Name, vo.UnknownValue)
			s.L("\tif len(members) != %d {", len(vo.Members))
			s.L("\t\tt.Fatalf(\"expected %d members, got %%d\", len(members))", len(vo.Members))
			s.L("\t}")
			s.L("\t// The unknown sentinel must NOT be a member: it is where an out-of-set")
			s.L("\t// value lands, so accepting it would defeat the whole check.")
			s.L("\tfor _, m := range members {")
			s.L("\t\tif m == %s {", vo.UnknownName)
			s.L("\t\t\tt.Error(\"the unknown sentinel must not be a declared member\")")
			s.L("\t\t}")
			s.L("\t}")
			s.L("}")
			s.Blank()
			continue
		}

		s.Doc(fmt.Sprintf("%s validates its own rule, so the aggregate never has to.", vo.Name))
		s.L("func Test%sRejectsInvalid(t *testing.T) {", vo.Name)
		s.L("\tctx := domain.NewNotificationContext(%s)", quote(vo.Name))
		s.L("\tif %s(%s).IsValid(%s, ctx) {", vo.Name, invalidSample(vo), quote(vo.Name))
		s.L("\t\tt.Error(\"an invalid value was accepted\")")
		s.L("\t}")
		s.L("}")
		s.Blank()
	}

	return goFile("internal/domain/vos/"+m.Entity.Snake+"_vos_test.go", fsplan.Owned,
		fmt.Sprintf("tests for %d value object(s)", len(m.ValueObjects)), s)
}

// ── sample values ──────────────────────────────────────────────────────────
//
// Deriving them from the spec's own `example:` keeps a test readable as
// documentation rather than as a wall of "aaa" and 1s.

func sampleValue(f ir.Field) string {
	return entitySample(f)
}

func entitySample(f ir.Field) string {
	base := literalFor(f)
	if f.VOKind != "" {
		base = fmt.Sprintf("%s(%s)", f.BaseEntityType, base)
	}
	if f.Nullable {
		return "func() " + f.EntityType + " { v := " + f.BaseEntityType + "(" + base + "); return &v }()"
	}
	return base
}

func wireSample(f ir.Field) string {
	base := literalFor(f)
	if f.Nullable {
		return "func() " + f.GoType + " { v := " + f.BaseGoType + "(" + base + "); return &v }()"
	}
	return base
}

func entityAsWire(f ir.Field, recv string) string {
	if f.VOKind != "" {
		return recv + "." + f.Name + ".Value()"
	}
	return recv + "." + f.Name
}

// literalFor prefers the spec's own example.
//
// The example is the author saying what a plausible value looks like, so using
// it is what makes the "valid" fixture actually valid — a hardcoded 1 fails
// every field whose declared range starts above it, and the failure looks like
// a broken rule rather than a broken sample.
func literalFor(f ir.Field) string {
	switch f.SpecType {
	case "string":
		if f.Example != "" {
			return quote(f.Example)
		}
		return quote(strings.ToLower(f.Name))
	case "time":
		if t, err := time.Parse(time.RFC3339, f.Example); err == nil {
			return fmt.Sprintf("time.Date(%d, %d, %d, %d, %d, %d, 0, time.UTC)",
				t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
		}
		return "time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)"
	case "bool":
		if f.Example == "false" {
			return "false"
		}
		return "true"
	case "int", "int64":
		if n, err := strconv.ParseInt(strings.TrimSpace(f.Example), 10, 64); err == nil {
			return strconv.FormatInt(n, 10)
		}
		return "1"
	case "float64":
		if v, err := strconv.ParseFloat(strings.TrimSpace(f.Example), 64); err == nil {
			return strings.TrimSuffix(fmt.Sprintf("%g", v), ".0")
		}
		return "1"
	case "id":
		return "domain.ID{}"
	default:
		return "1"
	}
}

func zeroValue(f ir.Field) string {
	if f.Nullable {
		return "nil"
	}
	switch f.SpecType {
	case "string":
		if f.VOKind != "" {
			return f.BaseEntityType + `("")`
		}
		return `""`
	case "time":
		return "time.Time{}"
	case "bool":
		return "false"
	case "id":
		return "domain.ID{}"
	default:
		return "0"
	}
}

func alternateValue(f ir.Field) string {
	switch f.SpecType {
	case "string":
		v := quote("changed-value")
		if f.VOKind != "" {
			v = fmt.Sprintf("%s(%s)", f.BaseEntityType, v)
		}
		if f.Nullable {
			return "func() " + f.EntityType + " { v := " + v + "; return &v }()"
		}
		return v
	case "time":
		return "time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)"
	default:
		return "99"
	}
}

func overMax(f ir.Field, max float64) string {
	return numberIn(max+1, goNumeric(f))
}

func underMin(f ir.Field, min float64) string {
	return numberIn(min-1, goNumeric(f))
}

func goNumeric(f ir.Field) string {
	if f.SpecType == "int" || f.SpecType == "int64" {
		return "int"
	}
	return "float"
}

func violatingComparison(f, other ir.Field, op string) string {
	// Drive the compared field to a value that certainly breaks the relation.
	if f.SpecType == "time" {
		switch op {
		case "gte", "gt":
			return "time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)"
		default:
			return "time.Date(2090, 1, 1, 0, 0, 0, 0, time.UTC)"
		}
	}
	switch op {
	case "gte", "gt":
		return "-1"
	default:
		return "999999"
	}
}

func invalidSample(vo ir.ValueObject) string {
	if vo.GoBacking == "int" {
		return "-1"
	}
	return quote("!!not-valid!!")
}

var _ = naming.Snake

// pointerize wraps a literal for a nullable field.
//
// Assigning a value to a pointer field does not compile, and the case that
// exposes it is the one a test is most likely to touch: an optional field is
// exactly where a boundary rule lives.
func pointerize(f ir.Field, literal string) string {
	if !f.Nullable {
		return literal
	}
	return "func() " + f.EntityType + " { v := " + f.BaseEntityType + "(" + literal + "); return &v }()"
}
