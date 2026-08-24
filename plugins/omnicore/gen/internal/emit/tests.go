package emit

import (
	"fmt"
	"sort"
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
	// Only the value objects the generator WROTE can be asserted about. When
	// every one of them is hand-written the file would carry a header claiming
	// tests and hold none — a green package that proves nothing, which is worse
	// than an absent file because it reads as coverage.
	if m.GeneratedValueObjects() > 0 {
		vo, err := emitVOTests(m)
		if err != nil {
			return nil, err
		}
		out = append(out, vo)
	}
	sch, err := emitSchemaTests(m)
	if err != nil {
		return nil, err
	}
	out = append(out, sch)
	if len(m.Children) > 0 {
		ch, err := emitChildTests(m)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)

		if m.HasOwnedChildren() {
			dto, err := emitChildInputTests(m)
			if err != nil {
				return nil, err
			}
			out = append(out, dto)
		}
	}
	if len(m.Notifications) > 0 {
		tr, err := emitTranslationTests(m)
		if err != nil {
			return nil, err
		}
		out = append(out, tr)
	}
	if len(m.WriteOps()) > 0 {
		rq, err := emitRequestTests(m)
		if err != nil {
			return nil, err
		}
		if rq.Path != "" {
			out = append(out, rq)
		}
	}
	for _, fn := range []func(*ir.Model) (fsplan.File, error){emitQueryTests, emitViewTests} {
		f, err := fn(m)
		if err != nil {
			return nil, err
		}
		if f.Path != "" {
			out = append(out, f)
		}
	}
	return out, nil
}

func emitDomainTests(m *ir.Model) (fsplan.File, error) {
	s := &src{}
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
	if m.HasNotificationsIn("aggregatevos") || len(m.Children) > 0 {
		s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
	}
	s.L(")")
	s.Blank()

	emitTestHelpers(s, m)
	emitServiceStub(s, m)
	emitValidEntityBuilder(s, m)
	emitAggregateContractTest(s, m)
	emitRowScopeCases(s, m)
	emitNotificationSemantics(s, m)
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

// emitTestIdentity attaches an identity to a command test's context.
//
// Without one, `ctx.Identity()` is nil and every mapper's identity feed is
// skipped — so the test exercises the empty branch of the one thing that decides
// whether a scoped write is the caller's to make. That is exactly the code a
// bodyless verb consists of: its mapper used to be a flat no-op, and giving it a
// body without giving the test an identity would leave the new body unrun and
// the coverage number saying so.
func emitTestIdentity(s *src, m *ir.Model, indent string) {
	if len(m.Runtime) == 0 {
		return
	}
	claims := map[string]string{}
	for _, f := range m.Runtime {
		switch f.IdentitySource {
		case "tenant":
			claims["tenant_id"] = testScopeValue(m)
		case "subject", "permission", "super-admin", "present":
			// None of these is a custom claim. Subject is a field of the
			// Identity; both permission questions — the concrete one and the
			// super-admin one — are answered from the permissions claim; and
			// PRESENCE is the nil check itself, so it has no claim name at all.
			// Falling through would put an entry keyed on the empty string into
			// the map, which reads as though the framework looked something up
			// by that name.
		default:
			if f.BaseGoType == "bool" {
				claims[f.Claim] = "true"
			} else {
				claims[f.Claim] = "someone@example.test"
			}
		}
	}
	s.L("%s// A request has a caller. With no identity the mappers' identity feed is", indent)
	s.L("%s// skipped entirely, and what a scoped write is checked against is exactly", indent)
	s.L("%s// what the feed carries.", indent)
	s.L("%sctx.SetIdentity(&configuration.Identity{", indent)
	if m.Authz.ScopeField != nil && m.Authz.DataAccess == "owner-only" {
		s.L("%s\tSubject: %s,", indent, quote(testScopeValue(m)))
	}
	if len(claims) > 0 {
		s.L("%s\tClaims: map[string]any{", indent)
		for _, k := range sortedClaimNames(claims) {
			s.L("%s\t\t%s: %s,", indent, quote(k), quote(claims[k]))
		}
		s.L("%s\t},", indent)
	}
	s.L("%s})", indent)
}

// emitIdentityArrived asserts the feed actually ran. Coverage alone would be
// satisfied by calling the mapper; what matters is that the value landed, since
// a feed that assigns nothing leaves a scoped write comparing against "".
func emitIdentityArrived(s *src, m *ir.Model, indent string) {
	caller := m.Authz.ScopeField
	if caller == nil {
		return
	}
	s.L("%sif e.%s != %s {", indent, caller.Name, quote(testScopeValue(m)))
	s.L("%s\tt.Errorf(%s, e.%s)", indent,
		quote("the caller's scope did not reach the entity (%q) — a write outside it could not be refused"),
		caller.Name)
	s.L("%s}", indent)
}

// testScopeValue is the caller's own scope in the generated tests. It matches
// what the domain fixture uses, so a test that builds both halves agrees with
// itself.
func testScopeValue(m *ir.Model) string {
	if f := m.Authz.ScopeSubject(); f != nil {
		return strings.Trim(scopeFixtureValue(*f), `"`)
	}
	return "caller"
}

// sortedClaimNames keeps a generated map literal stable run to run — an emitter
// that ranges a Go map writes a different file every time and turns every
// regeneration into a diff.
func sortedClaimNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// emitRowScopeCases proves the WRITE half of a scoped dataAccess, verb by verb.
//
// This is the assertion the security finding asked for by name: generate a
// tenant-scoped entity and check that a write carrying a FOREIGN scope is
// refused — on insert, on update and on archive — with the caller's identity
// supplied. Each verb is listed separately because each used to be a separate
// hole, and archive is the one that surprises: it does not dispatch under
// IfUpdate, and it loads through the repository, which the read filter never
// touches.
//
// The positive direction is covered by valid<Entity>() itself, which sets the
// caller's scope to the row's — so if the guard ever fires on a legitimate
// write, every case in this file fails at once.
func emitRowScopeCases(s *src, m *ir.Model) {
	subject, caller := m.Authz.ScopeSubject(), m.Authz.ScopeField
	if subject == nil || caller == nil {
		return
	}
	what := "tenant"
	if m.Authz.DataAccess == "owner-only" {
		what = "owner"
	}
	e := m.Entity.Pascal
	// Any value that is not the fixture's. What matters is only that the two
	// differ: the guard compares them and nothing else.
	foreign := quote("somebody-else")

	verbs := [][2]string{}
	seen := map[string]bool{}
	for _, op := range m.Ops {
		switch op.Verb {
		case "insert":
			if !seen["insert"] {
				seen["insert"] = true
				verbs = append(verbs, [2]string{"Insert", "insert"})
			}
		case "update", "patch":
			if !seen["update"] {
				seen["update"] = true
				verbs = append(verbs, [2]string{"Update", "update"})
			}
		case "archive":
			if !seen["archive"] {
				seen["archive"] = true
				verbs = append(verbs, [2]string{"Archive", "archive"})
			}
		case "unarchive":
			if !seen["unarchive"] {
				seen["unarchive"] = true
				verbs = append(verbs, [2]string{"Unarchive", "unarchive"})
			}
		case "delete":
			if !seen["delete"] {
				seen["delete"] = true
				verbs = append(verbs, [2]string{"Delete", "delete"})
			}
		}
	}

	for _, v := range verbs {
		s.Doc(
			fmt.Sprintf("A %s of a row in another %s is refused.", v[1], what),
			"",
			fmt.Sprintf("The caller holds the %s permission — that is a different question, "+
				"and it is already answered by the route. This is about WHICH ROW: the read "+
				"side would never have shown it to them, and without this the write side "+
				"would let them have it anyway.", v[1]))
		s.L("func Test%s_%sOutside%s_IsRefused(t *testing.T) {", e, v[0], naming.Pascal(what))
		s.L("\te := valid%s()", e)
		s.L("\te.%s = %s", caller.Name, foreign)
		switch v[0] {
		case "Insert":
			s.L("\t_, err := domain.GetInsertable(e, %s, %s)", serviceArg(m), quote("GetInsertable"))
		case "Update":
			s.L("\t_, err := domain.GetUpdatable(e, func(*%s) error { return nil }, %s, %s)",
				e, serviceArg(m), quote("GetUpdatable"))
		case "Archive":
			s.L("\t_, err := domain.GetArchivable(e, %s, %s)", serviceArg(m), quote("GetArchivable"))
		case "Unarchive":
			s.L("\t_, err := domain.GetUnarchivable(e, %s, %s)", serviceArg(m), quote("GetUnarchivable"))
		case "Delete":
			s.L("\t_, err := domain.GetDeletable(e, %s, %s)", serviceArg(m), quote("GetDeletable"))
		}
		s.L("\tif err == nil {")
		s.L("\t\tt.Fatal(%s)",
			quote("a "+v[1]+" into another "+what+" was accepted — the caller cannot even read this row back"))
		s.L("\t}")
		s.L("\tif !%sBlames(err, %s) {", m.Entity.Camel, quote(subject.Name))
		s.L("\t\tt.Errorf(%s, %sRejectedFields(err))",
			quote("the rejection should name "+subject.Name+", it named %v"), m.Entity.Camel)
		s.L("\t}")
		s.L("}")
		s.Blank()
	}

	if m.Authz.BypassField != nil {
		s.Doc(
			fmt.Sprintf("The %s bypass crosses the %s scope.", m.Authz.Bypass, what),
			"",
			"It is the operator supporting a customer: they have to be able to repair a "+
				"row that is not theirs. Without a test the key can be declared, read off "+
				"the identity and never consulted — which looks exactly like it working.")
		s.L("func Test%s_BypassCrossesTheScope(t *testing.T) {", e)
		s.L("\te := valid%s()", e)
		s.L("\te.%s = %s", caller.Name, foreign)
		s.L("\te.%s = true", m.Authz.BypassField.Name)
		s.L("\tif _, err := domain.GetInsertable(e, %s, %s); err != nil {",
			serviceArg(m), quote("GetInsertable"))
		s.L("\t\tt.Fatalf(%s, err)",
			quote("the bypass holder was refused a row outside their "+what+": %v"))
		s.L("\t}")
		s.L("}")
		s.Blank()
	}

	if m.Authz.NoIdentity == "stand-down" {
		s.Doc(
			"With no identity at all the guard stands down, as authz.noIdentity says.",
			"",
			"Only a dev bench reaches it — the middleware is bypassable solely with "+
				"auth.mode disabled, which the framework refuses outside APP_PROFILE=dev — "+
				"and without it a scoped entity refuses every write on the machine it is "+
				"first tried on.")
		s.L("func Test%s_NoIdentityStandsDown(t *testing.T) {", e)
		s.L("\te := valid%s()", e)
		s.L("\t// No caller at all — NOT merely an empty scope. A token that simply")
		s.L("\t// carries no such claim is an ordinary production request and is")
		s.L("\t// refused; the case below it proves that.")
		s.L("\te.%s = false", m.Authz.PresenceField.Name)
		s.L("\te.%s = \"\"", caller.Name)
		s.L("\tif _, err := domain.GetInsertable(e, %s, %s); err != nil {",
			serviceArg(m), quote("GetInsertable"))
		s.L("\t\tt.Fatalf(%s, err)",
			quote("an anonymous write was refused under stand-down: %v"))
		s.L("\t}")
		s.L("}")
		s.Blank()

		// The other half of the same value, and the one that is NOT a bench: a
		// real, signed, valid token that carries no such claim reaches the
		// domain with the same empty scope and must still be refused. Standing
		// down on the VALUE instead of on PRESENCE is what handed the whole
		// guard to anyone holding a claimless token.
		s.Doc(
			fmt.Sprintf("A caller WITH an identity but no %s claim is still refused.", what),
			"",
			"It arrives at the domain looking exactly like the anonymous case above — "+
				"an empty scope — and it is the opposite situation: an authenticated "+
				"request that simply cannot be placed in any "+what+". Standing down for "+
				"it would let any claimless token write anywhere.")
		s.L("func Test%s_IdentityWithoutTheClaim_IsRefused(t *testing.T) {", e)
		s.L("\te := valid%s()", e)
		s.L("\te.%s = \"\"", caller.Name)
		s.L("\tif _, err := domain.GetInsertable(e, %s, %s); err == nil {",
			serviceArg(m), quote("GetInsertable"))
		s.L("\t\tt.Fatal(%s)",
			quote("a token carrying no "+what+" claim wrote into a "+what+" that is not theirs"))
		s.L("\t}")
		s.L("}")
		s.Blank()
	}
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
	emitEntityLiteralFields(s, m.AllOwnerFields(), "\t\t")
	emitScopeFixture(s, m, "\t\t")
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

// emitScopeFixture makes the valid aggregate one the CALLER is entitled to
// write.
//
// A row-scoped entity has a rule nobody declared: the row's tenant (or owner)
// must be the caller's. The fixture builds the entity directly, with no request
// behind it, so the caller's scope is the zero value and the guard refuses —
// which would ship a red generated test on every scoped entity, and teach the
// author to distrust the suite rather than to read it.
//
// So the fixture states what it is: a write by someone entitled to make it. The
// negative case — a write into a scope that is not the caller's — is an
// end-to-end question (it needs a request, an identity and a route), and it
// belongs to the contract suite, not here.
func emitScopeFixture(s *src, m *ir.Model, indent string) {
	if m.Authz.PresenceField == nil {
		return
	}
	s.L("%s// A REQUEST made this, by a caller entitled to make it. Every rule that", indent)
	s.L("%s// stands down for an absent principal reads the flag below, so a fixture", indent)
	s.L("%s// that left it false would test those rules standing down rather than", indent)
	s.L("%s// running — which is how a negative case passes while proving nothing.", indent)
	s.L("%s%s: true,", indent, m.Authz.PresenceField.Name)

	if subject, caller := m.Authz.ScopeSubject(), m.Authz.ScopeField; subject != nil && caller != nil {
		s.L("%s// The row is in the caller's own %s.", indent, subject.Name)
		s.L("%s%s: %s,", indent, caller.Name, scopeFixtureValue(*subject))
	}
	// An ownerCheck compares a runtime field against a column, and both halves
	// have to agree for the aggregate to be VALID. Leaving the runtime half
	// empty used to pass only because the check stood down on it — the very
	// shape that let a claimless token through in production.
	for _, owned := range ownerCheckPairs(m) {
		s.L("%s// The caller owns this row: %s matches %s.",
			indent, owned.runtime.Name, owned.column.Name)
		s.L("%s%s: %s,", indent, owned.runtime.Name, scopeFixtureValue(owned.column))
	}
}

// ownerCheckPairs lists the (runtime principal, row's owner column) pairs an
// ownerCheck compares, so the fixture can make them agree.
func ownerCheckPairs(m *ir.Model) []struct{ runtime, column ir.Field } {
	var out []struct{ runtime, column ir.Field }
	seen := map[string]bool{}
	for _, c := range m.Clauses {
		for _, r := range c.Rules {
			if r.Kind != "ownerCheck" || r.OwnerField == nil || len(r.Fields) == 0 {
				continue
			}
			if seen[r.OwnerField.Name] {
				continue
			}
			seen[r.OwnerField.Name] = true
			out = append(out, struct{ runtime, column ir.Field }{*r.OwnerField, r.Fields[0]})
		}
	}
	return out
}

// scopeFixtureValue is the same value the fixture gave the scope COLUMN,
// rendered as the text a claim carries.
//
// It has to track literalFor exactly: the guard compares the two, so a fixture
// whose caller and whose row disagree by one character produces a red test that
// says nothing about the rule under it.
func scopeFixtureValue(f ir.Field) string {
	lit := literalFor(f)
	// An id's literal is a constructor call; the claim is the string inside it.
	if inner := strings.TrimSuffix(strings.TrimPrefix(lit, "domain.NewID("), ")"); inner != lit {
		return inner
	}
	return lit
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
			name, f.Name, stubParams(f), factResults(f), stubResult(f))
	}
	s.Blank()
}

// stubParams mirrors the port's signature exactly. An approximate one does not
// satisfy the interface, and the assertion in the rule panics at run time
// rather than failing to compile.
func stubParams(f ir.Fact) string {
	var out []string
	for _, p := range f.Params {
		out = append(out, fmt.Sprintf("_ %s", p.GoType))
	}
	return strings.Join(out, ", ")
}

// stubResult answers "nothing found" in whatever shape the fact declares. For
// a fact that carries Found, that is the zero AND false — a stub claiming a
// minimum exists would let a rule through for a reason no test wrote down.
func stubResult(f ir.Fact) string {
	if f.Grouped() {
		// No groups is the honest "nothing found" for a per-group fact, and it
		// is what an empty table really answers.
		return "nil"
	}
	if f.ReturnsFound {
		return stubZero(f.ReturnType) + ", false"
	}
	return stubZero(f.ReturnType)
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
	// A rule scoped to more than one gate appears in one clause PER gate, and
	// the test it drives is named after the rule alone — so without this set a
	// `scope: [insert, update]` rule declared the same Test function twice and
	// the file did not compile.
	seen := map[string]bool{}
	for _, clause := range m.Clauses {
		for _, rule := range clause.Rules {
			emitRuleCase(s, m, clause.Gate, rule, seen)
		}
	}
}

func emitRuleCase(s *src, m *ir.Model, gate string, rule ir.Rule, seen map[string]bool) {
	if len(rule.Fields) == 0 {
		return
	}
	f := rule.Fields[0]
	once := func(name string) bool {
		if seen[name] {
			return false
		}
		seen[name] = true
		return true
	}

	switch rule.Kind {
	case "required":
		if f.SpecType == "bool" {
			return
		}
		if !once(fmt.Sprintf("Test%s_%s_IsRequired", m.Entity.Pascal, f.Name)) {
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
		if rule.Max != nil && once(fmt.Sprintf("Test%s_%s_AboveBound", m.Entity.Pascal, f.Name)) {
			emitBoundCase(s, m, rule, f, "Above", overMax(f, *rule.Max))
		}
		if rule.Min != nil && once(fmt.Sprintf("Test%s_%s_BelowBound", m.Entity.Pascal, f.Name)) {
			emitBoundCase(s, m, rule, f, "Below", underMin(f, *rule.Min))
		}

	case "immutable":
		if !once(fmt.Sprintf("Test%s_%s_IsImmutable", m.Entity.Pascal, f.Name)) {
			return
		}
		// The rule compares against the pre-write snapshot, which only exists on
		// the update path — asserting it through an insert would prove nothing.
		s.Doc(fmt.Sprintf("%s cannot change once set.", f.Name),
			"",
			"It is driven through the update path because the rule reads the previous "+
				"value, and on an insert there is no previous value to read.")
		s.L("func Test%s_%s_IsImmutable(t *testing.T) {", m.Entity.Pascal, f.Name)
		s.L("\te := valid%s()", m.Entity.Pascal)
		// A composite is assigned as a whole — the entity carries the concept,
		// not the columns — so the alternate is a value object literal rather
		// than a scalar. Without this the test assigned 99 to a struct.
		alt := alternateValue(f)
		if g := compositeGroupNamed(m, f.Name); g != nil {
			alt = compositeAlternate(*g)
		}
		s.L("\t_, err := domain.GetUpdatable(e, func(x *%s) error {", m.Entity.Pascal)
		s.L("\t\tx.%s = %s", f.Name, alt)
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
		if !once(fmt.Sprintf("Test%s_%s_Comparison", m.Entity.Pascal, f.Name)) {
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
	if f.Nullable {
		// A rule may target an optional field — a facet's columns are all
		// nullable, and a facet is where "optional but bounded" usually lives.
		// Assigning the bare value to a pointer does not compile.
		s.L("\tout%s := %s(%s)", f.Name, f.BaseEntityType, value)
		s.L("\te.%s = &out%s", f.Name, f.Name)
	} else {
		s.L("\te.%s = %s", f.Name, value)
	}
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
	s.Blank()
	s.L("package commands")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("testing"))
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote(fwImport("application/configuration")))
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\tappdomain %s", quote(m.ImportPath("internal/domain")))
	if len(m.Children) > 0 {
		s.L("\t%s", quote(m.ImportPath("internal/application/dtos")))
	}
	// entitySample renders a value object as vos.T("…"), so the package is needed
	// the moment any field of the entity has one.
	if m.UsesVOs() {
		s.L("\t%s", quote(m.ImportPath("internal/domain/vos")))
	}
	s.L(")")
	s.Blank()

	if op := m.Op("insert"); op != nil {
		s.Doc("The insert mapper must carry every field through.",
			"",
			"A field silently dropped here is the quietest bug in the stack: the write "+
				"succeeds and the value is simply not there.")
		s.L("func TestInsert%sMapsEveryField(t *testing.T) {", m.Entity.Pascal)
		s.L("\tctx := &configuration.AppContext{}")
		emitTestIdentity(s, m, "\t")
		s.L("\tc := &%s{", op.CommandType)
		// The command carries what a CLIENT may send. A server-assigned field is
		// not in the type at all, so naming it here would not compile.
		for _, f := range m.WritableFields() {
			s.L("\t\t%s: %s,", f.Name, wireSample(f))
		}
		for _, c := range m.Children {
			s.L("\t\t%s: []dtos.%s{{", c.GoPlural, c.InputType)
			for _, f := range c.Fields {
				if f.Nullable {
					continue
				}
				s.L("\t\t\t%s: %s,", f.Name, wireSample(f))
			}
			s.L("\t\t}},")
		}
		s.L("\t}")
		if op.InputMethod == "ToEntity" {
			s.L("\te, err := c.ToEntity(ctx)")
			s.L("\tif err != nil {")
			s.L("\t\tt.Fatalf(%s, err)", quote("ToEntity: %v"))
			s.L("\t}")
			emitIdentityArrived(s, m, "\t")
		} else {
			s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
			s.L("\tif err := c.ApplyTo(ctx, e); err != nil {")
			s.L("\t\tt.Fatalf(%s, err)", quote("ApplyTo: %v"))
			s.L("\t}")
		}
		for _, f := range m.WritableFields() {
			if f.Nullable {
				continue
			}
			s.L("\tif %s != %s {", entityAsWire(f, "e"), wireSample(f))
			s.L("\t\tt.Errorf(\"%s did not survive the mapper\")", f.Name)
			s.L("\t}")
		}
		s.L("}")
		s.Blank()

		emitFromEntityTest(s, m, op)
	}

	// The "absent fields are left alone" case needs ONE field to plant a value in
	// and read back, and it has to be a field of the entity — a composite's part
	// is not one, and the value object as a whole is not what the command carries.
	// A spec whose every patchable field is a composite part simply skips it; the
	// twin case below still covers the other half of the contract.
	patchWitness := firstPlain(m.PatchableFields())
	if op := m.Op("patch"); op != nil && patchWitness != nil {
		s.Doc("A partial update must leave absent fields alone.",
			"",
			"This is the whole contract of the verb: sending nothing must not blank "+
				"anything out.")
		s.L("func TestPatch%sLeavesAbsentFieldsAlone(t *testing.T) {", m.Entity.Pascal)
		s.L("\tctx := &configuration.AppContext{}")
		emitTestIdentity(s, m, "\t")
		s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
		first := *patchWitness
		// The sample is HOISTED so the assertion compares against the very
		// value that was assigned. Minting the sample twice compared two
		// distinct pointers whenever the field is nullable, and the test
		// failed against a correct mapper.
		s.L("\torig := %s", entitySample(first))
		s.L("\te.%s = orig", first.Name)
		s.L("\tc := &%s{} // nothing sent", op.CommandType)
		s.L("\tif err := c.ApplyPartiallyTo(ctx, e); err != nil {")
		s.L("\t\tt.Fatalf(%s, err)", quote("ApplyPartiallyTo: %v"))
		s.L("\t}")
		s.L("\tif e.%s != orig {", first.Name)
		s.L("\t\tt.Error(\"an absent field was overwritten\")")
		s.L("\t}")
		s.L("}")
		s.Blank()

		s.Doc(
			"A partial update applies every field it DOES carry.",
			"",
			"The twin of the case above: leaving absent fields alone is only half the "+
				"contract, and a mapper that leaves everything alone passes that half.")
		s.L("func TestPatch%sAppliesWhatItCarries(t *testing.T) {", m.Entity.Pascal)
		s.L("\tctx := &configuration.AppContext{}")
		emitTestIdentity(s, m, "\t")
		s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
		s.L("\tc := &%s{", op.CommandType)
		for _, f := range m.PatchableFields() {
			if m.PatchExcludes[f.Name] {
				continue
			}
			s.L("\t\t%s: %s,", f.Name, patchSample(f))
		}
		s.L("\t}")
		s.L("\tif err := c.ApplyPartiallyTo(ctx, e); err != nil {")
		s.L("\t\tt.Fatalf(%s, err)", quote("ApplyPartiallyTo: %v"))
		s.L("\t}")
		for _, f := range m.PatchableFields() {
			if m.PatchExcludes[f.Name] || f.Nullable {
				continue
			}
			s.L("\tif %s != %s {", entityAsWire(f, "e"), literalFor(f))
			s.L("\t\tt.Errorf(\"%s was sent and not applied\")", f.Name)
			s.L("\t}")
		}
		s.L("}")
		s.Blank()

		emitPartialResultTest(s, m, op)
	}

	for _, op := range m.WriteOps() {
		if op.Verb == "insert" || op.Verb == "patch" {
			continue // covered above, field by field
		}
		s.Doc(
			fmt.Sprintf("%s must apply cleanly to a well-formed entity.", op.CommandType),
			"",
			"Archive and unarchive carry no body, so what is under test is that the "+
				"mapper leaves the entity alone and reports no error — a mapper that "+
				"errors here turns a legitimate verb into a 400.")
		s.L("func Test%sApplies(t *testing.T) {", op.CommandType)
		s.L("\tctx := &configuration.AppContext{}")
		emitTestIdentity(s, m, "\t")
		s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
		s.L("\t// The result carries the id, and a verb that reaches this mapper has always")
		s.L("\t// loaded the row first — so the entity under test has one too.")
		s.L("\te.SetID(domain.NewID(%s))", quote("019ffd00-0000-7000-8000-000000000000"))
		s.L("\tc := &%s{", op.CommandType)
		if op.InputMethod == "ApplyTo" || op.InputMethod == "ApplyPartiallyTo" {
			for _, f := range m.WritableFields() {
				if m.PatchExcludes[f.Name] {
					continue
				}
				s.L("\t\t%s: %s,", f.Name, wireSample(f))
			}
		}
		s.L("\t}")
		s.L("\tif err := c.%s(ctx, e); err != nil {", applyMethod(op))
		s.L("\t\tt.Fatalf(%s, err)", quote("the mapper failed: %v"))
		s.L("\t}")
		emitIdentityArrived(s, m, "\t")
		s.L("\tif _, err := c.FromEntity(ctx, e); err != nil {")
		s.L("\t\tt.Errorf(%s, err)", quote("projecting the result failed: %v"))
		s.L("\t}")
		s.L("}")
		s.Blank()
	}

	// The facet-clear commands, which exist only when GraphQL does. They are
	// small and their whole behaviour is "assign nil to exactly these fields",
	// which is precisely the kind of thing a later edit breaks silently.
	if m.Surfaces.GraphQL {
		for _, sib := range m.SiblingsOn("") {
			s.Doc(
				fmt.Sprintf("Clear%sCommand empties the facet and touches nothing else.", sib.Name),
				"",
				"It is dispatched through the UPDATE handler on purpose: the framework's "+
					"sibling write leaves an all-nil facet untouched on a PARTIAL update and "+
					"deletes its row on a full one, so a partial-shaped clear would answer "+
					"success and change nothing.")
			s.L("func TestClear%sEmptiesTheFacet(t *testing.T) {", sib.Name)
			s.L("\tctx := &configuration.AppContext{}")
			emitTestIdentity(s, m, "\t")
			s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
			facetPlain, facetGroups := ir.PlainAndComposites(sib.Fields)
			for _, f := range facetPlain {
				s.L("\te.%s = %s", f.Name, entitySample(f))
			}
			for _, g := range facetGroups {
				s.L("\te.%s = %s", g.Owner(), compositeSample(g))
			}
			s.L("\tif err := (&Clear%sCommand{}).ApplyTo(ctx, e); err != nil {", sib.Name)
			s.L("\t\tt.Fatalf(%s, err)", quote("ApplyTo: %v"))
			s.L("\t}")
			emitIdentityArrived(s, m, "\t")
			for _, f := range facetPlain {
				s.L("\tif e.%s != nil {", f.Name)
				s.L("\t\tt.Errorf(\"%s survived the clear\")", f.Name)
				s.L("\t}")
			}
			for _, g := range facetGroups {
				s.L("\tif e.%s != nil {", g.Owner())
				s.L("\t\tt.Errorf(\"%s survived the clear\")", g.Owner())
				s.L("\t}")
			}
			s.L("}")
			s.Blank()

			s.Doc(fmt.Sprintf("Clear%sCommand answers with the owner it emptied.", sib.Name))
			s.L("func TestClear%sAnswersTheOwner(t *testing.T) {", sib.Name)
			s.L("\tctx := &configuration.AppContext{}")
			s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
			s.L("\te.SetID(domain.NewID(%s))", quote("019ffd00-0000-7000-8000-000000000000"))
			s.L("\tout, err := (&Clear%sCommand{}).FromEntity(ctx, e)", sib.Name)
			s.L("\tif err != nil {")
			s.L("\t\tt.Fatalf(%s, err)", quote("FromEntity: %v"))
			s.L("\t}")
			s.L("\tif out.%sID != *e.GetID() {", m.Entity.Pascal)
			s.L("\t\tt.Error(\"the result does not carry the owner it cleared\")")
			s.L("\t}")
			s.L("}")
			s.Blank()
		}
	}

	emitPerChildOpTests(s, m)

	return goFile("internal/application/commands/"+m.Entity.Snake+"_commands_test.go",
		fsplan.Owned, "tests for the command mappers", s)
}

func emitVOTests(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package vos")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("strings"))
	// A composite's parts may be instants, and a comparison rule's case is built
	// from two of them. The import is pruned when nothing here uses it.
	s.L("\t%s", quote("time"))
	s.L("\t%s", quote("testing"))
	s.Blank()
	s.L("\t%s", quote(fwImport("domain")))
	s.L(")")
	s.Blank()

	for _, vo := range m.ValueObjects {
		// A hand-written value object gets no generated test: the generator does
		// not know the rule, so anything it asserted here would be its own guess
		// failing in the author's file. The one property it COULD check — that
		// the type exists with the right shape — the compiler already checks,
		// louder, on the same run.
		if vo.HandWritten() {
			continue
		}
		if vo.Kind == "composite" {
			emitCompositeVOTests(s, m, vo)
			continue
		}
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

			s.Doc(
				fmt.Sprintf("A declared member of %s is accepted and reads back unchanged.", vo.Name),
				"",
				"The pair matters: a value object that rejects everything passes the "+
					"negative test below and is still broken.")
			s.Doc(
				fmt.Sprintf("A value outside %s's set is not one of its members.", vo.Name),
				"",
				"An enum declares no rule of its own: the framework decides membership "+
					"from Values(), and answers with the unknown notification. So what is "+
					"under test is the SET — that a member is in it and a stranger is not.")
			s.L("func Test%sMembership(t *testing.T) {", vo.Name)
			s.L("\tin := func(v %s) bool {", vo.Name)
			s.L("\t\tfor _, m := range v.Values() {")
			s.L("\t\t\tif m == v {")
			s.L("\t\t\t\treturn true")
			s.L("\t\t\t}")
			s.L("\t\t}")
			s.L("\t\treturn false")
			s.L("\t}")
			s.L("\tif !in(%s) {", vo.Members[0].ConstName)
			s.L("\t\tt.Error(\"a declared member is not in its own set\")")
			s.L("\t}")
			s.L("\tif in(%s(%s)) {", vo.Name, invalidSample(vo))
			s.L("\t\tt.Error(\"a value outside the set was found in it\")")
			s.L("\t}")
			if vo.GoBacking != "int" {
				s.L("\tif %s.Value() == \"\" {", vo.Members[0].ConstName)
				s.L("\t\tt.Error(\"a member reads back empty\")")
				s.L("\t}")
			}
			s.L("\tif %s.UnknownNotification() == nil {", vo.Members[0].ConstName)
			s.L("\t\tt.Error(\"the enum has no notification for an unknown value\")")
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

		s.Doc(
			fmt.Sprintf("%s refuses every shape its rule names.", vo.Name),
			"",
			"One case per bound, because a rule that checks only the first one still "+
				"passes a single negative test.")
		s.L("func Test%sRefusesEachWay(t *testing.T) {", vo.Name)
		s.L("\tctx := domain.NewNotificationContext(%s)", quote(vo.Name))
		s.L("\tfor what, v := range map[string]%s{", vo.Name)
		// The zero is asserted only when the rule actually refuses it: a string
		// backing always does, an int backing only when 0 is out of range —
		// asserting it unconditionally failed the test against a correct value
		// object whose range legitimately admits 0.
		if rejectsZero(vo) {
			s.L("\t\t%s: %s(%s),", quote("empty"), vo.Name, emptyFor(vo))
		}
		if vo.MaxLength > 0 {
			s.L("\t\t%s: %s(%s),", quote("too long"), vo.Name, tooLongFor(vo))
		}
		if vo.Max != nil {
			s.L("\t\t%s: %s(%d),", quote("above the maximum"), vo.Name, int(*vo.Max)+1)
		}
		if vo.Min != nil {
			s.L("\t\t%s: %s(%d),", quote("below the minimum"), vo.Name, int(*vo.Min)-1)
		}
		s.L("\t} {")
		s.L("\t\tif v.IsValid(%s, ctx) {", quote(vo.Name))
		s.L("\t\t\tt.Errorf(%s, what)", quote("a value that is %s was accepted"))
		s.L("\t\t}")
		s.L("\t}")
		s.L("}")
		s.Blank()

		if validSample(m, vo) == "" {
			continue // no example to trust; guessing at the rule would be worse
		}
		s.Doc(fmt.Sprintf("%s accepts a well-formed value and reads it back.", vo.Name))
		s.L("func Test%sAcceptsValid(t *testing.T) {", vo.Name)
		s.L("\tctx := domain.NewNotificationContext(%s)", quote(vo.Name))
		s.L("\tv := %s(%s)", vo.Name, validSample(m, vo))
		s.L("\tif !v.IsValid(%s, ctx) {", quote(vo.Name))
		s.L("\t\tt.Error(\"a well-formed value was refused\")")
		s.L("\t}")
		s.L("\tif v.Value() != %s {", validSample(m, vo))
		s.L("\t\tt.Error(\"the value did not read back unchanged\")")
		s.L("\t}")
		s.L("}")
		s.Blank()
	}

	return goFile("internal/domain/vos/"+m.Entity.Snake+"_vos_test.go", fsplan.Owned,
		fmt.Sprintf("tests for %d value object(s)", m.GeneratedValueObjects()), s)
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
	// A composite's part is not a field of the entity: it is read THROUGH the
	// value object. Every caller skips nullable fields, and every part of an
	// optional composite is nullable, so no nil guard is ever needed here.
	if f.Composite != nil {
		ref := recv + "." + f.Composite.Owner + "." + f.Composite.PartName
		if f.VOKind != "" {
			return ref + ".Value()"
		}
		return ref
	}
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
		// A real constructor call, deterministic and non-zero: the bare
		// composite literal `domain.ID{}` is a parse error inside an
		// if-condition, and a zero id fails any required rule the fixture
		// is supposed to satisfy.
		if f.Example != "" {
			return fmt.Sprintf("domain.NewID(%s)", quote(f.Example))
		}
		return `domain.NewID("00000000-0000-0000-0000-000000000001")`
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

// alternateValue is a value guaranteed to differ from literalFor's sample, in
// the field's own entity type. It mirrors entitySample's wrapping exactly —
// handling the value-object and nullable layers for strings only, as this once
// did, emitted a bare time.Date into a *time.Time field and a 99 into a
// pointer, an id or a bool.
func alternateValue(f ir.Field) string {
	var base string
	switch f.SpecType {
	case "string":
		base = quote("changed-value")
	case "time":
		base = "time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)"
	case "bool":
		// literalFor answers true unless the example says otherwise.
		base = "false"
	case "id":
		base = `domain.NewID("00000000-0000-0000-0000-000000000002")`
	default:
		base = "99"
	}
	if f.VOKind != "" {
		base = fmt.Sprintf("%s(%s)", f.BaseEntityType, base)
	}
	if f.Nullable {
		return "func() " + f.EntityType + " { v := " + f.BaseEntityType + "(" + base + "); return &v }()"
	}
	return base
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

// invalidSample is a value the value object is GUARANTEED to reject. For a
// numeric backing that means one step past a declared bound — the old fixed -1
// was a legitimate value for any rule whose range starts below zero, and the
// generated test failed against a correct generator.
func invalidSample(vo ir.ValueObject) string {
	if vo.GoBacking == "int" {
		if vo.Min != nil {
			return fmt.Sprintf("%d", int(*vo.Min)-1)
		}
		if vo.Max != nil {
			return fmt.Sprintf("%d", int(*vo.Max)+1)
		}
		return "-1"
	}
	return quote("!!not-valid!!")
}

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

// emitSchemaTests exercises every schema BUILDER.
//
// It is the cheapest boot-trap catcher there is. The framework validates a
// schema by panicking while it is declared — a child carrying a revision, a
// facet carrying a lifecycle, a collection whose name is not spellable as a Go
// field — and those panics land at boot, which means the feedback costs a
// service start. Calling the builders in a unit test moves every one of them to
// `go test`.
func emitSchemaTests(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package schemas")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("testing"))
	s.Blank()
	s.L("\t%s", quote(fwImport("infra/db/core")))
	if len(m.Children) > 0 {
		s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
	}
	s.L(")")
	s.Blank()

	s.Doc(
		"The builders must not panic, and must map a table.",
		"",
		"The framework validates a schema AS IT IS DECLARED, by panicking: a child "+
			"carrying a revision, a facet carrying a lifecycle, a collection whose name "+
			"is not spellable as a Go field. Every one of those aborts the boot. Calling "+
			"the builders here turns a failed start into a failed test.")
	s.L("func Test%sSchemasBuild(t *testing.T) {", m.Entity.Pascal)
	s.L("\tfor name, build := range map[string]func() *core.TableSchema{")
	s.L("\t\t%s: %sSchema,", quote(m.Entity.Pascal), m.Entity.Pascal)
	if m.IsRole() && !m.Base.Reuse {
		s.L("\t\t%s: %s,", quote(m.Base.FuncName), m.Base.FuncName)
	}
	for _, c := range m.Children {
		s.L("\t\t%s: %sSchema,", quote(c.Name), c.Name)
	}
	s.L("\t} {")
	s.L("\t\tschema := build()")
	s.L("\t\tif schema == nil {")
	s.L("\t\t\tt.Fatalf(%s, name)", quote("%s built a nil schema"))
	s.L("\t\t}")
	s.L("\t\tif schema.Table() == \"\" {")
	s.L("\t\t\tt.Errorf(%s, name)", quote("%s built a schema with no table"))
	s.L("\t\t}")
	s.L("\t}")
	s.L("}")
	s.Blank()

	s.Doc(
		fmt.Sprintf("%s must map the table the spec names.", m.Entity.Pascal),
		"",
		"The table is the one thing a schema cannot get wrong quietly: pointed at "+
			"another table it still compiles, still boots, and reads someone else's rows.")
	s.L("func Test%sSchemaMapsTheDeclaredTable(t *testing.T) {", m.Entity.Pascal)
	s.L("\tif got := %sSchema().Table(); got != %s {", m.Entity.Pascal, quote(m.Table))
	s.L("\t\tt.Errorf(%s, got, %s)", quote("the schema maps %q, the spec says %q"), quote(m.Table))
	s.L("\t}")
	s.L("}")

	if len(m.Children) > 0 {
		s.Blank()
		s.Doc(
			"Each collection answers the name the spec declared.",
			"",
			"That name is a PERSISTED key — the segment the projection nests the "+
				"collection under and the field a read DTO must carry — so a change here "+
				"changes the document shape rather than a label.")
		s.L("func Test%sCollectionsKeepTheirNames(t *testing.T) {", m.Entity.Pascal)
		// A slice, not a map keyed by the answered name: two collections that
		// wrongly answered the SAME name would collapse into one entry and the
		// collision would pass.
		s.L("\tfor _, tc := range []struct{ got, want string }{")
		for _, c := range m.Children {
			s.L("\t\t{aggregatevos.%s{}.CollectionName(), %s},", c.Name, quote(c.Plural))
		}
		s.L("\t} {")
		s.L("\t\tif tc.got != tc.want {")
		s.L("\t\t\tt.Errorf(%s, tc.got, tc.want)", quote("a collection answers %q, the spec says %q"))
		s.L("\t\t}")
		s.L("\t}")
		s.L("}")
	}

	return goFile("internal/infra/schemas/"+m.Entity.Snake+"_schemas_test.go", fsplan.Owned,
		"the schema builder tests — they run the builders, so a boot panic is a test failure", s)
}

// emitChildTests covers the collection types.
//
// A child carries its own rules and its own definition of sameness, and both
// run on every write of the owner — so a break here is a break of the whole
// aggregate, reported against a type nobody tests by hand.
func emitChildTests(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package aggregatevos")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("reflect"))
	s.L("\t%s", quote("testing"))
	s.L("\t%s", quote("time"))
	s.Blank()
	s.L("\t%s", quote(fwImport("domain")))
	if m.UsesVOsInChildren() {
		s.L("\t%s", quote(m.ImportPath("internal/domain/vos")))
	}
	s.L(")")
	s.Blank()

	emitChildRuleSeat(s, m)

	for _, c := range m.Children {
		s.Doc(
			fmt.Sprintf("rulesFor%s runs one entry through the framework's own seat.", c.Name),
			"",
			"Not %s.BuildRules directly. A rule may end the validation pass — the "+
				"`guard: true` barrier — and the framework unwinds that from inside the "+
				"seat that invoked the rules; a body called by hand would let the unwind "+
				"escape as a panic, and every test below would fail on a rule doing "+
				"exactly what it was declared to do.",
			"",
			"The seat also validates the entry's value objects, which is what a write "+
				"does, so what these tests see is what the service sees.")
		s.L("func rulesFor%s%s(v %s) *domain.NotificationContext {", m.Entity.Pascal, c.Name, c.Name)
		s.L("\thost := &%sRuleHost{}", naming.Camel(m.Entity.Pascal))
		s.L("\tdomain.ValidateAggregateChild(host, v, domain.ModeInsert, %s, nil)", quote("insert"))
		s.L("\treturn host.GetAggregateRoot().NotificationContext()")
		s.L("}")
		s.Blank()
		emitValidChildBuilder(s, m, c)

		s.Doc(
			fmt.Sprintf("%s's collection name is a PERSISTED key, so it is pinned here.", c.Name),
			"",
			"It is the segment the projection nests the collection under and the field a "+
				"read DTO carries. Changing it is a data migration wearing a rename's "+
				"clothes: the old documents keep the old key and nothing reads them back.")
		s.L("func Test%s%s_CollectionNameIsTheDocumentKey(t *testing.T) {", m.Entity.Pascal, c.Name)
		s.L("\tif got := (%s{}).CollectionName(); got != %s {", c.Name, quote(c.Plural))
		s.L("\t\tt.Errorf(%s, got, %s)",
			quote("the collection is written under %q, and the documents already say %q"), quote(c.Plural))
		s.L("\t}")
		s.L("}")
		s.Blank()

		if len(c.Identity) > 0 {
			s.Doc(
				fmt.Sprintf("Two %s entries are the same one when their business identity matches.", c.Name),
				"",
				"Sameness is the business identity, never the id: the same entry typed "+
					"twice in one request carries two ids and is still one entry, and the "+
					"aggregate refuses the duplicate on exactly this answer.")
			s.L("func Test%s%s_SamenessIsTheBusinessIdentity(t *testing.T) {", m.Entity.Pascal, c.Name)
			s.L("\ta := valid%s%s()", m.Entity.Pascal, c.Name)
			s.L("\tb := valid%s%s()", m.Entity.Pascal, c.Name)
			s.L("\tif !a.IsSameBusinessIdentity(b) {")
			s.L("\t\tt.Error(\"two entries with the same business identity were seen as different\")")
			s.L("\t}")
			f := c.Identity[0]
			s.L("\tb.%s = %s", f.Name, alternateValue(f))
			s.L("\tif a.IsSameBusinessIdentity(b) {")
			s.L("\t\tt.Errorf(\"changing %s did not change the identity\")", f.Name)
			s.L("\t}")
			s.L("}")
			s.Blank()
		}

		s.Doc(
			fmt.Sprintf("A well-formed %s passes its own rules.", c.Name),
			"",
			"The negative cases below are only meaningful if the positive one holds: a "+
				"builder that never validates would make every rejection vacuous.")
		s.L("func Test%s%s_ValidPasses(t *testing.T) {", m.Entity.Pascal, c.Name)
		s.L("\tctx := rulesFor%s%s(valid%s%s())", m.Entity.Pascal, c.Name, m.Entity.Pascal, c.Name)
		s.L("\tif msgs := ctx.Messages(); len(msgs) > 0 {")
		s.L("\t\tt.Errorf(\"a valid %s was refused: %%v\", msgs)", c.Name)
		s.L("\t}")
		s.L("}")
		s.Blank()

		// Same dedup as the root's emitRuleCases: a rule scoped to two gates
		// sits in two clauses and would declare its test twice.
		seenChild := map[string]bool{}
		for _, clause := range c.Clauses {
			for _, rule := range clause.Rules {
				if rule.Kind != "required" || len(rule.Fields) == 0 {
					continue
				}
				f := rule.Fields[0]
				if f.SpecType == "bool" {
					continue
				}
				name := fmt.Sprintf("Test%s%s_%s_IsRequired", m.Entity.Pascal, c.Name, f.Name)
				if seenChild[name] {
					continue
				}
				seenChild[name] = true
				s.Doc(fmt.Sprintf("%s.%s is required.", c.Name, f.Name))
				s.L("func Test%s%s_%s_IsRequired(t *testing.T) {", m.Entity.Pascal, c.Name, f.Name)
				s.L("\tv := valid%s%s()", m.Entity.Pascal, c.Name)
				s.L("\tv.%s = %s", f.Name, zeroValue(f))
				s.L("\tctx := rulesFor%s%s(v)", m.Entity.Pascal, c.Name)
				s.L("\tif len(ctx.Messages()) == 0 {")
				s.L("\t\tt.Errorf(\"an empty %s was accepted\")", f.Name)
				s.L("\t}")
				s.L("}")
				s.Blank()
			}
		}
	}

	return goFile("internal/domain/aggregatevos/"+m.Entity.Snake+"_children_test.go",
		fsplan.Owned, "tests for the collection types", s)
}

// emitChildRuleSeat writes the stand-in root the entry tests validate through.
//
// domain.ValidateAggregateChild is the framework's public seat for running ONE
// entry's rules, and it takes an AggregateRootProvider. The real root lives in
// internal/domain, which imports THIS package — so it cannot be imported back.
// A local stand-in satisfies the interface with the framework types this file
// already has in hand, and costs the tests nothing: the seat only reads the
// root for the notification context and the collection's index.
func emitChildRuleSeat(s *src, m *ir.Model) {
	if len(m.Children) == 0 {
		return
	}
	name := naming.Camel(m.Entity.Pascal) + "RuleHost"
	s.Doc(
		fmt.Sprintf("%s stands in for the aggregate root so an entry can be validated "+
			"through the framework's own seat.", name),
		"",
		"The real root is in internal/domain, which imports this package: importing it "+
			"back is a cycle. What the seat needs of a root is the notification context "+
			"and the collection index, both of which this carries.")
	s.L("type %s struct {", name)
	s.L("\tdomain.AggregateRoot")
	s.L("}")
	s.Blank()
	s.L("func (h *%s) GetAggregateRoot() *domain.AggregateRoot { return &h.AggregateRoot }", name)
	s.Blank()
	s.L("func (h *%s) AggregateChildren() []domain.AggregateValueObject {", name)
	s.L("\treturn []domain.AggregateValueObject{")
	for _, c := range m.Children {
		s.L("\t\t%s{},", c.Name)
	}
	s.L("\t}")
	s.L("}")
	s.Blank()
}

func emitValidChildBuilder(s *src, m *ir.Model, c ir.Child) {
	s.Doc(fmt.Sprintf("valid%s is one entry every rule accepts.", c.Name))
	s.L("func valid%s%s() %s {", m.Entity.Pascal, c.Name, c.Name)
	s.L("\treturn %s{", c.Name)
	plain, groups := ir.PlainAndComposites(c.Fields)
	for _, f := range plain {
		if f.Nullable {
			continue
		}
		s.L("\t\t%s: %s,", f.Name, entitySample(f))
	}
	// A composite is built WHOLE — its parts are not fields of the entry — and an
	// optional one is skipped for the same reason a nullable field is: the
	// fixture carries what every rule needs, and absence is never that.
	for _, g := range groups {
		if g.Optional() {
			continue
		}
		s.L("\t\t%s: %s,", g.Owner(), compositeSample(g))
	}
	s.L("\t}")
	s.L("}")
	s.Blank()
}

// emitTranslationTests proves every notification can be rendered in every
// language.
//
// A key the catalog does not know renders as the key itself — the message the
// caller sees becomes "InvalidStateNotification", which reads as a crash and is
// not one. Nothing else in the build catches it: the catalogs are maps, and a
// missing entry is not a compile error.
func emitTranslationTests(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package translations")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("testing"))
	s.Blank()
	s.L("\t%s", quote(fwImport("application/configuration")))
	s.L("\t%s", quote(fwImport("application/translation")))
	s.L(")")
	s.Blank()

	s.Doc(
		"Each of the seven catalogs answers every notification this entity raises.",
		"",
		"A missing entry is not a compile error and not a crash: the caller simply "+
			"receives the key instead of a sentence, in one language, usually the one "+
			"nobody on the team reads.")
	s.L("func Test%sNotificationsAreTranslated(t *testing.T) {", m.Entity.Pascal)
	s.L("\tkeys := []string{")
	for _, n := range m.Notifications {
		s.L("\t\t%s,", quote(n.Name))
	}
	s.L("\t}")
	s.L("\tcatalogs := map[string]map[string]string{")
	for _, c := range catalogs {
		s.L("\t\t%s: %s{}.Translations(),", quote(c.Code), c.Type)
	}
	s.L("\t}")
	s.L("\tfor lang, catalog := range catalogs {")
	s.L("\t\tfor _, key := range keys {")
	s.L("\t\t\tif text, ok := catalog[key]; !ok || text == \"\" {")
	s.L("\t\t\t\tt.Errorf(%s, key, lang)", quote("%s has no text in %s"))
	s.L("\t\t\t}")
	s.L("\t\t}")
	s.L("\t}")
	s.L("}")
	s.Blank()

	s.Doc(
		"Each catalog is registered as the language it claims.",
		"",
		"The framework picks a catalog by the constant the module answers, and the "+
			"constants are not spelled like the files (esp/ES, fra/FR, deu/DE, ita/IT, "+
			"nld/NL) — a pairing that is easy to write once and wrong forever.")
	s.L("func Test%sCatalogsAnswerTheirOwnLanguage(t *testing.T) {", m.Entity.Pascal)
	s.L("	for want, module := range map[configuration.Language]translation.Module{")
	for _, c := range catalogs {
		s.L("\t\tconfiguration.%s: %s(),", c.LangConst, c.Ctor)
	}
	s.L("	} {")
	s.L("\t\tif got := module.Language(); got != want {")
	s.L("\t\t\tt.Errorf(%s, got, want)", quote("a catalog answers %v, it is registered as %v"))
	s.L("\t\t}")
	s.L("\t}")
	s.L("}")

	return goFile("internal/application/translations/"+m.Entity.Snake+"_translations_test.go",
		fsplan.Owned, "the translation coverage test — every notification must be translatable in every catalog", s)
}

// emitRequestTests covers the wire→command mapping.
//
// It is the layer where a dropped field is quietest: the request parses, the
// command runs, the write succeeds, and the value the caller sent is simply not
// in the row.
func emitRequestTests(m *ir.Model) (fsplan.File, error) {
	var ops []ir.Operation
	for _, op := range m.WriteOps() {
		if !op.Bodyless {
			ops = append(ops, op)
		}
	}
	if len(ops) == 0 && !m.HasPerChildOps() {
		return fsplan.File{}, nil
	}

	s := &src{}
	s.Blank()
	s.L("package requests")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("testing"))
	s.L("\t%s", quote("time"))
	s.Blank()
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\tfwqueries %s", quote(fwImport("application/queries")))
	s.L("\t%s", quote(m.ImportPath("internal/application/commands")))
	s.L("\tappqueries %s", quote(m.ImportPath("internal/application/queries")))
	s.L(")")
	s.Blank()

	for _, op := range ops {
		s.Doc(
			fmt.Sprintf("%s must carry every field into the command.", op.RequestType),
			"",
			"A field dropped here is invisible: the request parses, the write succeeds, "+
				"and the value the caller sent is not in the row.")
		s.L("func Test%s_CarriesEveryField(t *testing.T) {", op.RequestType)
		s.L("\tr := %s{", op.RequestType)
		for _, f := range m.WritableFields() {
			if m.PatchExcludes[f.Name] && op.Verb == "patch" {
				continue
			}
			s.L("\t\t%s: %s,", f.Name, requestSample(f, op))
		}
		s.L("\t}")
		if len(m.Children) > 0 && op.Verb != "patch" {
			for _, c := range m.Children {
				s.L("\tr.%s = []%sRequest{{", c.GoPlural, c.Name)
				for _, f := range c.Fields {
					if f.Nullable {
						continue
					}
					s.L("\t\t%s: %s,", f.Name, wireSample(f))
				}
				s.L("\t}}")
			}
		}
		s.L("\tcmd := r.ToCommand()")
		s.L("\tif cmd == nil {")
		s.L("\t\tt.Fatal(\"the mapper produced no command\")")
		s.L("\t}")
		for _, c := range m.Children {
			if op.Verb == "patch" {
				continue
			}
			s.L("\tif len(cmd.%s) != 1 {", c.GoPlural)
			s.L("\t\tt.Errorf(\"the %s collection did not reach the command\")", c.Name)
			s.L("\t}")
		}
		for _, f := range m.WritableFields() {
			if m.PatchExcludes[f.Name] && op.Verb == "patch" {
				continue
			}
			if f.Nullable || op.Verb == "patch" {
				continue // a pointer compares by address; the value is asserted below
			}
			s.L("\tif cmd.%s != r.%s {", f.Name, f.Name)
			s.L("\t\tt.Errorf(\"%s did not reach the command\")", f.Name)
			s.L("\t}")
		}
		s.L("}")
		s.Blank()

		if op.ResponseType != "" {
			s.Doc(
				fmt.Sprintf("%s must carry the result back out.", op.ResponseType),
				"",
				"The response is the caller's only view of what was written, so a field "+
					"lost here reads as a field that was never saved.")
			s.L("func Test%s_CarriesTheResult(t *testing.T) {", op.ResponseType)
			s.L("\tvar in commands.%s", op.ResultType)
			s.L("\t_ = %s{}.FromResult(in)", op.ResponseType)
			s.L("}")
			s.Blank()
		}
	}

	if m.Read.ByID {
		s.Doc(
			"The by-id request maps onto the query it stands for.",
			"",
			"It carries the controls the spec declared for a single read; one dropped "+
				"here is a parameter the caller sends and the read ignores.")
		s.L("func TestFind%sByIDRequestBuildsItsQuery(t *testing.T) {", m.Entity.Pascal)
		s.L("\tif q := (Find%sByIDRequest{}).ToQuery(fwqueries.ReadCriteria{}); q == nil {", m.Entity.Pascal)
		s.L("\t\tt.Fatal(\"the by-id request produced no query\")")
		s.L("\t}")
		s.L("\t// The criteria travels WHOLE. A by-id read takes it the same way the")
		s.L("\t// paged one does — the wrapper parses the wire, the mapper carries it")
		s.L("\t// through — so a mapper that rebuilt a fresh criteria instead would")
		s.L("\t// drop whatever the wire said, silently and on every request.")
		s.L("\tq := (Find%sByIDRequest{}).ToQuery(fwqueries.ReadCriteria{IncludeArchived: true})", m.Entity.Pascal)
		s.L("\tif q == nil || !q.Criteria.IncludeArchived {")
		s.L("\t\tt.Error(\"the parsed criteria did not reach the query\")")
		s.L("\t}")
		s.L("}")
		s.Blank()
	}

	if m.Read.ByParams {
		s.Doc(
			"The listing request maps onto the query it stands for.",
			"",
			"Every control it carries is one the spec declared; one dropped here is a "+
				"parameter the caller sends and the read silently ignores.")
		s.L("func TestFind%sRequestBuildsItsQuery(t *testing.T) {", m.Entity.PluralPascal)
		s.L("\tq := %s{}.ToQuery(fwqueries.ReadCriteria{})", "Find"+m.Entity.PluralPascal+"Request")
		s.L("\tif q == nil {")
		s.L("\t\tt.Fatal(\"the listing request produced no query\")")
		s.L("\t}")
		s.L("}")
		s.Blank()
	}

	emitReadMappingTests(s, m)

	emitPerChildRequestTests(s, m)

	return goFile("internal/web/requests/"+m.Entity.Snake+"_requests_test.go",
		fsplan.Owned, "the request mapper tests", s)
}

// requestSample renders a value for a wire DTO field.
//
// A patch body is all pointers — that is what lets it say "leave this alone" —
// so the same field is a pointer there and a plain value everywhere else.
func requestSample(f ir.Field, op ir.Operation) string {
	if op.Verb == "patch" && !f.Nullable {
		return "func() *" + f.BaseGoType + " { v := " + f.BaseGoType + "(" + literalFor(f) + "); return &v }()"
	}
	return wireSample(f)
}

// emitQueryTests covers the read side's criteria mappers.
//
// A query that builds the wrong criteria answers 200 with the wrong rows, which
// no status code and no log line will ever tell anyone about.
func emitQueryTests(m *ir.Model) (fsplan.File, error) {
	if !m.Read.Enabled || (!m.Read.ByID && !m.Read.ByParams) {
		return fsplan.File{}, nil
	}
	// The BODY first, the header after: this file's imports are decided from
	// what it actually writes, and it writes values it did not used to. A
	// derivation whose source is an `id` builds a domain.ID; one whose source is
	// a timestamp builds a time.Date. Either under a hard-coded import block is
	// `undefined: domain` in a file the author did not write.
	s := &src{}

	s.Doc(
		"The criteria mappers must not fail on a well-formed query.",
		"",
		"A mapper that errors here turns a legitimate read into a 400, and a mapper "+
			"that silently drops a restriction turns it into someone else's rows — so "+
			"the mapping is exercised rather than assumed.")
	s.L("func Test%sCriteriaBuild(t *testing.T) {", m.Entity.Pascal)
	s.L("\t// No identity attached: the anonymous path, which is the one that")
	s.L("\t// applies every restriction rather than skipping them.")
	s.L("\tctx := &configuration.AppContext{}")
	if m.Read.ByID {
		s.L("\tif _, err := (%s{}).ToCriteria(ctx); err != nil {", m.Read.QueryByID)
		s.L("\t\tt.Errorf(%s, err)", quote("the by-id criteria failed: %v"))
		s.L("\t}")
	}
	if m.Read.ByParams {
		s.L("\tif _, err := (%s{}).ToCriteria(ctx); err != nil {", m.Read.QueryList)
		s.L("\t\tt.Errorf(%s, err)", quote("the listing criteria failed: %v"))
		s.L("\t}")
	}
	s.L("}")
	s.Blank()

	emitFromQueryResultTest(s, m)
	emitScopeIsForcedTest(s, m)
	emitBypassCrossesTheReadTest(s, m)

	if m.Read.ByParams && len(m.Read.FieldRestrict) > 0 {
		s.Blank()
		s.Doc(
			"Naming a restricted field is an ERROR, not a silent omission.",
			"",
			"The distinction is the whole point of the restriction: a caller that "+
				"merely did not ask for the field gets a page without it, and a caller "+
				"that asked for it by name gets a 403. Collapsing the two is what leaks "+
				"the field's existence.")
		s.L("func Test%sRestrictedFieldIsRefused(t *testing.T) {", m.Entity.Pascal)
		s.L("\tctx := &configuration.AppContext{}")
		s.L("\tq := %s{}", m.Read.QueryList)
		s.L("\tq.Criteria.Filter = map[string]any{%s: %s}",
			quote(m.Read.FieldRestrict[0].Field), restrictSample(m, m.Read.FieldRestrict[0].Field))
		s.L("\tif _, err := q.ToCriteria(ctx); err == nil {")
		s.L("\t\tt.Errorf(%s, %s)", quote("filtering by %s was accepted without the permission"),
			quote(m.Read.FieldRestrict[0].Field))
		s.L("\t}")
		s.L("}")
	}
	s.Blank()

	s.Doc(
		"Both queries report the same context name.",
		"",
		"It is the name every notification of this read is grouped under, so two "+
			"spellings split one entity's errors into two piles for whoever reads them.")
	s.L("func Test%sQueriesShareAContext(t *testing.T) {", m.Entity.Pascal)
	s.L("\tfor _, got := range []string{")
	if m.Read.ByID {
		s.L("\t\t%s{}.ContextName(),", m.Read.QueryByID)
	}
	if m.Read.ByParams {
		s.L("\t\t%s{}.ContextName(),", m.Read.QueryList)
	}
	s.L("\t} {")
	s.L("\t\tif got != %s {", quote(m.Entity.Pascal))
	s.L("\t\t\tt.Errorf(%s, got, %s)", quote("a query reports the context %q, the entity is %q"), quote(m.Entity.Pascal))
	s.L("\t\t}")
	s.L("\t}")
	s.L("}")

	out := &src{}
	out.Blank()
	out.L("package queries")
	out.Blank()
	queryTestImports(out, queryTestTypeNames(m))
	out.Blank()
	out.Write(s.Bytes())

	return goFile("internal/application/queries/"+m.Entity.Snake+"_queries_test.go",
		fsplan.Owned, "the read criteria tests", out)
}

// queryTestTypeNames is every type this test file NAMES beyond the builtins.
//
// Two places construct typed values in it, and both took a widening in the same
// round that per-entry derivations arrived: the derivation cases build one
// Result with every source filled, and the fieldRestrict case builds a sample
// for the restricted field. Both go through literalFor, which spells an id as
// domain.NewID and a timestamp as time.Date.
func queryTestTypeNames(m *ir.Model) []string {
	out := derivationTypeNames(m)
	if len(m.Read.FieldRestrict) > 0 {
		for _, f := range m.AllOwnerFields() {
			if f.Name == m.Read.FieldRestrict[0].Field {
				out = append(out, f.GoType, f.BaseGoType)
			}
		}
	}
	return out
}

// queryTestImports writes the block, adding `time` and `domain` only when a type
// above names them — an import Go does not need is as fatal as one it does.
func queryTestImports(s *src, types []string) {
	joined := strings.Join(types, " ")
	needTime := strings.Contains(joined, "time.")
	needDomain := strings.Contains(joined, "domain.")
	s.L("import (")
	s.L("\t%s", quote("testing"))
	if needTime {
		s.L("\t%s", quote("time"))
	}
	s.Blank()
	s.L("\t%s", quote(fwImport("application/configuration")))
	if needDomain {
		s.L("\t%s", quote(fwImport("domain")))
	}
	s.L(")")
}

// emitViewTests builds the view definition.
//
// Like the schemas, a view is validated as it is DECLARED — an index on a path
// the view cannot resolve, a projection of a field that is not there — and the
// panic lands at boot. Building it here moves that to `go test`.
func emitViewTests(m *ir.Model) (fsplan.File, error) {
	if !m.Read.Enabled {
		return fsplan.File{}, nil
	}
	s := &src{}
	s.Blank()
	s.L("package views")
	s.Blank()
	s.L("import %s", quote("testing"))
	s.Blank()

	doc := []string{
		"The view builds and answers the name the spec declared.",
		"",
		"A view is validated as it is declared: an index on a path it cannot resolve, " +
			"a projection of a field that is not there. Every one of those aborts the " +
			"boot, and building the definition here turns that into a test failure.",
	}
	if m.Read.Backing == "relational" {
		doc = append(doc, "",
			"The loader is nil on purpose — nothing is read here; the declaration "+
				"itself is what is under test. The framework's own boot validation "+
				"rejects a nil loader, which is a service-wide check over every "+
				"declared read model rather than a property of this one.")
	}
	s.Doc(doc...)
	s.L("func Test%sViewBuilds(t *testing.T) {", m.Entity.Pascal)
	if m.Read.Backing == "relational" {
		s.L("\tv := %sView(nil)", m.Entity.Pascal)
	} else {
		s.L("\tv := %sView()", m.Entity.Pascal)
	}
	s.L("\tif v == nil {")
	s.L("\t\tt.Fatal(\"the view definition is nil\")")
	s.L("\t}")
	s.L("\tif got := v.Name(); got != %s {", quote(m.Read.ViewName))
	s.L("\t\tt.Errorf(%s, got, %s)", quote("the view is named %q, the spec says %q"), quote(m.Read.ViewName))
	s.L("\t}")
	s.L("}")

	return goFile("internal/infra/views/"+m.Entity.Snake+"_view_test.go",
		fsplan.Owned,
		"the view definition test — it builds the definition, so a boot panic is a test failure", s)
}

// applyMethod names the mapper a verb uses. A bodyless verb still has one: it
// is where the framework hands the entity over, and it must not refuse.
func applyMethod(op ir.Operation) string {
	if op.InputMethod != "" {
		return op.InputMethod
	}
	return "ApplyTo"
}

// validSample is a value the value object's own rule accepts.
//
// It comes from the spec's example where there is one, because a test that
// reads as documentation is worth more than one built from "aaa".
func validSample(m *ir.Model, vo ir.ValueObject) string {
	if vo.GoBacking == "int" {
		if vo.Min != nil {
			return fmt.Sprintf("%d", int(*vo.Min))
		}
		return "1"
	}
	// The example of a FIELD that uses this value object: it is the one string
	// the spec guarantees the rule accepts. Inventing one here would mean
	// guessing at a regex, and a test that fails on a correct generator is worse
	// than no test.
	//
	// The comparison is against the QUALIFIED name — a field records its VO as
	// `vos.Email`, never as `Email`. Comparing the bare name matched nothing, so
	// no string-backed value object ever got its accepts-a-valid-value test: the
	// half that proves the rule does not reject everything, and the only caller
	// of Value(). Both read as untested for as long as that stood.
	want := "vos." + vo.Name
	for _, f := range m.AllOwnerFields() {
		if f.BaseEntityType == want && f.Example != "" {
			return quote(f.Example)
		}
	}
	for _, c := range m.Children {
		for _, f := range c.Fields {
			if f.BaseEntityType == want && f.Example != "" {
				return quote(f.Example)
			}
		}
	}
	return ""
}

// patchSample renders a value for a PATCH command field, which is a pointer
// even when the field is not: that is what lets the body distinguish "set this
// to nothing" from "do not touch this".
func patchSample(f ir.Field) string {
	if f.Nullable {
		return wireSample(f)
	}
	return "func() *" + f.BaseGoType + " { v := " + f.BaseGoType + "(" + literalFor(f) + "); return &v }()"
}

// emitNotificationSemantics pins each notification to the status it produces.
//
// The semantic IS the HTTP status: a rejection declared as a conflict answers
// 409 and one declared as validation answers 422. Nothing else in the build
// checks that mapping, and getting it wrong changes what a client retries.
func emitNotificationSemantics(s *src, m *ir.Model) {
	if len(m.Notifications) == 0 {
		return
	}
	s.Doc(
		"Every notification answers the semantic the spec declared.",
		"",
		"The semantic is the status code: conflict is 409, validation is 422, "+
			"forbidden is 403. A wrong one still compiles and still returns a "+
			"rejection — just the kind a client handles differently.")
	s.L("func Test%sNotificationSemantics(t *testing.T) {", m.Entity.Pascal)
	// EVERY declared notification, whatever package it lives in. Filtering to
	// the domain package left the ones a value object or a collection raises
	// untested — and those are the rejections a caller meets most, because they
	// fire on the shape of what was sent.
	//
	// A SLICE, not a map keyed by the actual semantic: most notifications share
	// "validation", so keying by the answer collapsed the entries onto each
	// other and only the last notification per status was ever checked.
	s.L("\tfor _, tc := range []struct {")
	s.L("\t\tname string")
	s.L("\t\tgot  domain.NotificationSemantic")
	s.L("\t\twant domain.NotificationSemantic")
	s.L("\t}{")
	for _, n := range m.Notifications {
		s.L("\t\t{%s, %s{}.Semantic(), %s},",
			quote(n.Name), notificationRef(m, n), semanticConst(n.Semantic))
	}
	s.L("\t} {")
	s.L("\t\tif tc.got != tc.want {")
	s.L("\t\t\tt.Errorf(%s, tc.name, tc.got, tc.want)",
		quote("%s answers %v, the spec says %v"))
	s.L("\t\t}")
	s.L("\t}")
	s.L("}")
	s.Blank()
}

// emptyFor is the zero of a value object's backing: the case every rule checks
// first, and the one a test that only sends garbage never reaches.
func emptyFor(vo ir.ValueObject) string {
	if vo.GoBacking == "int" {
		return "0"
	}
	return quote("")
}

// rejectsZero answers whether the value object's own rule refuses its backing's
// zero — the condition for asserting the "empty" case at all.
func rejectsZero(vo ir.ValueObject) bool {
	if vo.GoBacking != "int" {
		return true // a raw string value object always refuses ""
	}
	if vo.Min != nil && *vo.Min > 0 {
		return true
	}
	if vo.Max != nil && *vo.Max < 0 {
		return true
	}
	return false
}

// tooLongFor builds a value one character past the declared ceiling.
func tooLongFor(vo ir.ValueObject) string {
	return fmt.Sprintf("strings.Repeat(%s, %d)", quote("a"), vo.MaxLength+1)
}

// emitScopeIsForcedTest proves the row scope carries the CALLER's own value.
//
// The build test above only proves the mapper does not error, and an owner
// filter that is silently dropped does not error — it answers with everybody's
// rows. That gap was found the expensive way: a real project's author noticed
// the generated tests asserted nothing about the value and wrote two files by
// hand to cover it. This is those files, generated.
//
// Three things are asserted, and each one is a different way to leak a row:
// the filter carries the identity that asked (checked with TWO identities, so a
// hardcoded value cannot pass); a value the CALLER sent for that field is
// overwritten rather than merged; and no identity at all yields the empty
// scope, never the unfiltered one.
func emitScopeIsForcedTest(s *src, m *ir.Model) {
	if !m.Read.ByParams {
		return
	}
	var field, from, a, b string
	switch m.Authz.DataAccess {
	case "owner-only":
		if m.Authz.OwnerField == nil {
			return
		}
		field, from = m.Authz.OwnerField.Name, "Subject"
		a, b = "ana@example.test", "bruno@example.test"
	case "tenant":
		if m.Authz.TenantField == nil {
			return
		}
		field, from = m.Authz.TenantField.Name, "tenant"
		a, b = "tenant-a", "tenant-b"
	default:
		return
	}

	s.Blank()
	s.Doc(
		fmt.Sprintf("The listing is scoped to the caller, and %s is not the caller's to choose.", field),
		"",
		"Two identities, because one proves nothing: a mapper that pinned a constant "+
			"would satisfy a single case and hand every row to the second caller. The "+
			"query also ARRIVES with a value for the field, which is what a caller "+
			"probing for someone else's rows would send — it must be overwritten, not "+
			"merged.")
	s.L("func Test%sScopeIsForced(t *testing.T) {", m.Entity.Pascal)
	s.L("\tfor _, want := range []string{%s, %s} {", quote(a), quote(b))
	s.L("\t\tctx := &configuration.AppContext{}")
	if from == "Subject" {
		s.L("\t\tctx.SetIdentity(&configuration.Identity{Subject: want})")
	} else {
		s.L("\t\tctx.SetIdentity(&configuration.Identity{Claims: map[string]any{%s: want}})",
			quote("tenant_id"))
	}
	s.L("\t\tq := %s{}", m.Read.QueryList)
	s.L("\t\t// What a caller fishing for someone else's rows would send.")
	s.L("\t\tq.Criteria.Filter = map[string]any{%s: %s}", quote(field), quote("somebody-else"))
	s.L("\t\tout, err := q.ToCriteria(ctx)")
	s.L("\t\tif err != nil {")
	s.L("\t\t\tt.Fatalf(%s, err)", quote("the listing criteria failed: %v"))
	s.L("\t\t}")
	s.L("\t\tif got := out.Filter[%s]; got != want {", quote(field))
	s.L("\t\t\tt.Errorf(%s, got, want)",
		quote("the scope is %v, the caller is "+field+" %v — the caller's rows are not the ones being read"))
	s.L("\t\t}")
	s.L("\t}")
	s.Blank()
	// What an ABSENT identity means is the spec's decision, so the assertion is
	// the spec's decision too. Asserting the empty scope under stand-down would
	// ship a red test for a policy the author declared on purpose — and, worse,
	// teach them the suite is wrong rather than the code.
	if m.Authz.NoIdentity == "stand-down" {
		s.L("\t// No identity: the scope STANDS DOWN, as authz.noIdentity says. Only a")
		s.L("\t// dev bench reaches this — auth.mode disabled is refused outside")
		s.L("\t// APP_PROFILE=dev — and it is what makes a scoped entity usable there")
		s.L("\t// at all, instead of answering every listing empty.")
		s.L("\tanon := &configuration.AppContext{}")
		s.L("\tout, err := (%s{}).ToCriteria(anon)", m.Read.QueryList)
		s.L("\tif err != nil {")
		s.L("\t\tt.Fatalf(%s, err)", quote("the anonymous listing criteria failed: %v"))
		s.L("\t}")
		s.L("\tif _, ok := out.Filter[%s]; ok {", quote(field))
		s.L("\t\tt.Error(%s)",
			quote("an anonymous read was scoped anyway — stand-down means no scope, not an empty one"))
		s.L("\t}")
		s.L("}")
		emitByIDScopeTest(s, m, field, from, a)
		return
	}
	s.L("\t// No identity: the EMPTY scope, which matches nothing. Leaving the")
	s.L("\t// filter out here would answer with every row in the table.")
	s.L("\tanon := &configuration.AppContext{}")
	s.L("\tout, err := (%s{}).ToCriteria(anon)", m.Read.QueryList)
	s.L("\tif err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("the anonymous listing criteria failed: %v"))
	s.L("\t}")
	s.L("\tif got, ok := out.Filter[%s]; !ok || got != \"\" {", quote(field))
	s.L("\t\tt.Errorf(%s, got, ok)",
		quote("an anonymous read scoped to %v (present=%v) — with no identity it must match nothing"))
	s.L("\t}")
	s.L("}")
	emitByIDScopeTest(s, m, field, from, a)
}

// emitBypassCrossesTheReadTest proves the row scope steps aside for whoever
// authz.bypass says may cross it — asked of a REAL identity, not of a boolean
// somebody set.
//
// The domain side already has a bypass test, and it sets the runtime field by
// hand: it proves the rule honours the flag, and nothing about whether anything
// ever raises it. This one goes through the framework's own HasPermission with
// a claim set on the context, so it fails if the question the generator emits
// is the wrong question.
//
// That matters most for the WILDCARD bypass, where the question cannot be the
// obvious one: `*:*` panics as an argument to HasPermission, so the guard asks
// IsSuperAdmin instead, and this test is what proves a wildcard claim answers
// it — against the framework, not against a comment.
func emitBypassCrossesTheReadTest(s *src, m *ir.Model) {
	if !m.Read.ByParams || m.Authz.Bypass == "" {
		return
	}
	var field, sample string
	switch m.Authz.DataAccess {
	case "owner-only":
		if m.Authz.OwnerField == nil {
			return
		}
		field, sample = m.Authz.OwnerField.Name, "ana@example.test"
	case "tenant":
		if m.Authz.TenantField == nil {
			return
		}
		field, sample = m.Authz.TenantField.Name, "tenant-a"
	default:
		return
	}

	held := m.Authz.Bypass
	who := "A holder of " + held
	if m.Authz.BypassWildcard {
		who = "A super-admin (" + held + ")"
	}
	s.Blank()
	s.Doc(
		fmt.Sprintf("%s reads across the %s scope.", who, field),
		"",
		"The identity is a real one and the question is the framework's own "+
			"HasPermission, so what is under test is the QUESTION the criteria asks — "+
			"the domain's bypass test sets the flag by hand and cannot see it.")
	s.L("func Test%sBypassCrossesTheReadScope(t *testing.T) {", m.Entity.Pascal)
	s.L("\tctx := &configuration.AppContext{}")
	s.L("\tctx.SetIdentity(&configuration.Identity{")
	if m.Authz.DataAccess == "owner-only" {
		s.L("\t\tSubject: %s,", quote(sample))
	}
	s.L("\t\tClaims: map[string]any{")
	if m.Authz.DataAccess == "tenant" {
		s.L("\t\t\t%s: %s,", quote("tenant_id"), quote(sample))
	}
	s.L("\t\t\t%s: []any{%s},", quote("permissions"), quote(held))
	s.L("\t\t},")
	s.L("\t})")
	s.L("\tout, err := (%s{}).ToCriteria(ctx)", m.Read.QueryList)
	s.L("\tif err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("the listing criteria failed: %v"))
	s.L("\t}")
	s.L("\tif got, ok := out.Filter[%s]; ok {", quote(field))
	s.L("\t\tt.Errorf(%s, got)",
		quote("the bypass holder was scoped to %v anyway — they cannot support a customer"))
	s.L("\t}")
	s.L("}")
}

// emitByIDScopeTest covers the OTHER read.
//
// The listing and the by-id read are written by the same emitter and asserted by
// different tests, and only the listing had one — so the scope on the read a
// caller uses to open ONE record was never checked. That is the read where a
// leak is most direct: the caller already has the id.
func emitByIDScopeTest(s *src, m *ir.Model, field, from, sample string) {
	if !m.Read.ByID {
		return
	}
	s.Blank()
	s.Doc(
		fmt.Sprintf("Opening one record is scoped too, and %s is not the caller's to choose.", field),
		"",
		"The by-id read and the listing are two functions written by one emitter, and "+
			"for a while only the listing was asserted. It is the read where a missing "+
			"scope leaks most directly: the caller already holds the id.")
	s.L("func Test%sByIDScopeIsForced(t *testing.T) {", m.Entity.Pascal)
	s.L("\tctx := &configuration.AppContext{}")
	if from == "Subject" {
		s.L("\tctx.SetIdentity(&configuration.Identity{Subject: %s})", quote(sample))
	} else {
		s.L("\tctx.SetIdentity(&configuration.Identity{Claims: map[string]any{%s: %s}})",
			quote("tenant_id"), quote(sample))
	}
	s.L("\tq := %s{}", m.Read.QueryByID)
	s.L("\t// What a caller fishing for someone else's record would send.")
	s.L("\tq.Criteria.Filter = map[string]any{%s: %s}", quote(field), quote("somebody-else"))
	s.L("\tout, err := q.ToCriteria(ctx)")
	s.L("\tif err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("the by-id criteria failed: %v"))
	s.L("\t}")
	s.L("\tif got := out.Filter[%s]; got != %s {", quote(field), quote(sample))
	s.L("\t\tt.Errorf(%s, got)",
		quote("the by-id read is scoped to %v — the caller's own scope is not what it forced"))
	s.L("\t}")
	s.L("}")
}

// restrictSample is any value of the restricted field: what is under test is
// that NAMING it is refused, not what it was compared against.
func restrictSample(m *ir.Model, name string) string {
	for _, f := range m.AllOwnerFields() {
		if f.Name == name {
			return literalFor(f)
		}
	}
	return quote("x")
}

// notificationRef spells a notification from the domain test's package: bare
// when it lives there, qualified when it lives in vos or aggregatevos.
func notificationRef(m *ir.Model, n ir.Notification) string {
	switch n.Package {
	case "", "domain":
		return n.Name
	default:
		return n.Package + "." + n.Name
	}
}

// emitPerChildOpTests covers the verbs that address ONE entry — the ones
// children[].operations mounts, which is all three unless the spec says less.
//
// They had no generated tests at all, and the gap was invisible in the usual
// way: the root's command tests are thorough, so a coverage report looked
// healthy while AddXCommand, ChangeXCommand and RemoveXCommand — the mappers a
// per-entry collection is edited through — sat at zero. Two consecutive real
// runs closed it by hand, writing the same four shapes each time. That is the
// definition of something a generator should be writing.
// It writes into the command-test file the generator already owns, and declares
// NOTHING at package scope — no fixture constant, no helper function. A project
// that closed this gap by hand before the generator learned to (both real runs
// did) would otherwise stop compiling on the upgrade, over a name nobody chose
// deliberately. Test function names can still coincide; that one the compiler
// reports honestly, and the answer is to delete the now-redundant file.
func emitPerChildOpTests(s *src, m *ir.Model) {
	for _, c := range m.Children {
		if !c.PerChild {
			continue
		}
		// One test per verb the collection actually mounts. A test for a command
		// that was not generated does not compile, which is the loudest possible
		// way to discover a spec key — and the wrong one.
		if c.MountsAdd {
			emitAddChildOpTest(s, m, c)
		}
		if c.MountsChange {
			emitChangeChildOpTest(s, m, c)
		}
		if c.MountsRemove {
			emitRemoveChildOpTest(s, m, c)
		}
	}
}

// ownerFixture seeds a loaded aggregate, inline. The verbs it feeds always run
// against a row that was read first, so the fixture carries an id like one.
func ownerFixture(s *src, m *ir.Model) {
	s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
	s.L("\te.SetID(domain.NewID(%s))", quote(fixtureOwnerID))
}

// fixtureOwnerID is a valid UUIDv7 the framework accepts. It is a constant
// rather than a generated one because a test that reads differently on every
// run cannot be diffed.
const fixtureOwnerID = "019ffd00-0000-7000-8000-000000000000"

// seededEntryID is the id the change/remove fixtures address.
const seededEntryID = "019ffd00-0000-7000-8000-0000000000a1"

func emitAddChildOpTest(s *src, m *ir.Model, c ir.Child) {
	s.Doc(fmt.Sprintf("Add%sCommand appends the entry and projects it back with the id "+
		"the server minted for it — the id the caller addresses it by afterwards.", c.OpBase))
	s.L("func TestAdd%sCommand_AppliesAndProjects(t *testing.T) {", c.OpBase)
	s.L("\tctx := &configuration.AppContext{}")
	emitTestIdentity(s, m, "\t")
	ownerFixture(s, m)
	s.L("\tcmd := &Add%sCommand{%s}", c.OpBase, childFieldsInline(c))
	s.L("\tif err := cmd.ApplyTo(ctx, e); err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("ApplyTo: %v"))
	s.L("\t}")
	emitIdentityArrived(s, m, "\t")
	s.L("\tout, err := cmd.FromEntity(ctx, e)")
	s.L("\tif err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("FromEntity: %v"))
	s.L("\t}")
	s.L("\tif out.%sID.Value() != %s {", m.Entity.Pascal, quote(fixtureOwnerID))
	s.L("\t\tt.Error(%s)", quote("the result does not carry the owner id"))
	s.L("\t}")
	for _, f := range c.Fields {
		if f.Nullable {
			continue // a nil sample proves nothing about the mapping
		}
		s.L("\tif out.%s.%s != %s {", c.Name, f.Name, literalFor(f))
		s.L("\t\tt.Errorf(%s)", quote("the projected entry lost "+f.Name))
		s.L("\t}")
	}
	s.L("}")
	s.Blank()
}

func emitChangeChildOpTest(s *src, m *ir.Model, c ir.Child) {
	s.Doc(fmt.Sprintf("Change%sCommand replaces the named entry and KEEPS its id: the row "+
		"is updated rather than removed and re-added, which is what makes the history read "+
		"as a change.", c.OpBase))
	s.L("func TestChange%sCommand_KeepsTheEntryID(t *testing.T) {", c.OpBase)
	s.L("\tctx := &configuration.AppContext{}")
	emitTestIdentity(s, m, "\t")
	ownerFixture(s, m)
	s.L("\tseeded := domain.WithID(")
	s.L("\t\t%s.To%s(),", childInputLiteral(m, c, false), c.Name)
	s.L("\t\tdomain.NewID(%s),", quote(seededEntryID))
	s.L("\t)")
	s.L("\te.AggregateConstructor([]domain.AggregateValueObject{seeded})")
	s.Blank()
	s.L("\tcmd := &Change%sCommand{", c.OpBase)
	s.L("\t\t%sID: %s,", c.Name, quote(seededEntryID))
	s.L("\t\t%s,", childFieldsInline(c))
	s.L("\t}")
	s.L("\tif err := cmd.ApplyTo(ctx, e); err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("ApplyTo: %v"))
	s.L("\t}")
	emitIdentityArrived(s, m, "\t")
	s.L("\tout, err := cmd.FromEntity(ctx, e)")
	s.L("\tif err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("FromEntity: %v"))
	s.L("\t}")
	s.L("\tif out.%s.ID.Value() != %s {", c.Name, quote(seededEntryID))
	s.L("\t\tt.Error(%s)", quote("the entry lost its id across the change"))
	s.L("\t}")
	s.L("}")
	s.Blank()

	s.Doc(fmt.Sprintf("An id the collection does not hold leaves the result's %s "+
		"zero-valued. The 404 is the handler's answer; what is checked here is that the "+
		"projection does not invent an entry to fill the gap.", c.Name))
	s.L("func TestChange%sCommand_UnknownIDProjectsNothing(t *testing.T) {", c.OpBase)
	s.L("\tctx := &configuration.AppContext{}")
	ownerFixture(s, m)
	s.L("\tcmd := &Change%sCommand{%sID: %s}",
		c.OpBase, c.Name, quote("019ffd00-0000-7000-8000-0000000000ff"))
	s.L("\tout, err := cmd.FromEntity(ctx, e)")
	s.L("\tif err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("FromEntity: %v"))
	s.L("\t}")
	s.L("\tif !out.%s.ID.IsEmpty() {", c.Name)
	s.L("\t\tt.Errorf(%s, out.%s)", quote("an unknown id projected an entry: %+v"), c.Name)
	s.L("\t}")
	s.L("}")
	s.Blank()
}

func emitRemoveChildOpTest(s *src, m *ir.Model, c ir.Child) {
	s.Doc(fmt.Sprintf("Remove%sCommand takes the entry out and answers with the owner "+
		"alone — the entry it names is gone, so there is nothing to project.", c.OpBase))
	s.L("func TestRemove%sCommand_AppliesAndProjects(t *testing.T) {", c.OpBase)
	s.L("\tctx := &configuration.AppContext{}")
	emitTestIdentity(s, m, "\t")
	ownerFixture(s, m)
	s.L("\tseeded := domain.WithID(")
	s.L("\t\t%s.To%s(),", childInputLiteral(m, c, false), c.Name)
	s.L("\t\tdomain.NewID(%s),", quote(seededEntryID))
	s.L("\t)")
	s.L("\te.AggregateConstructor([]domain.AggregateValueObject{seeded})")
	s.Blank()
	s.L("\tcmd := &Remove%sCommand{%sID: %s}", c.OpBase, c.Name, quote(seededEntryID))
	s.L("\tif err := cmd.ApplyTo(ctx, e); err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("ApplyTo: %v"))
	s.L("\t}")
	emitIdentityArrived(s, m, "\t")
	s.L("\tout, err := cmd.FromEntity(ctx, e)")
	s.L("\tif err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("FromEntity: %v"))
	s.L("\t}")
	s.L("\tif out.%sID.Value() != %s {", m.Entity.Pascal, quote(fixtureOwnerID))
	s.L("\t\tt.Error(%s)", quote("the result does not carry the owner id"))
	s.L("\t}")
	s.L("}")
	s.Blank()
}

// childInputLiteral builds the entry's input DTO. `empty` produces the zero
// value, for the case where the entry is never meant to be found.
func childInputLiteral(m *ir.Model, c ir.Child, empty bool) string {
	if empty {
		return fmt.Sprintf("dtos.%s{}", c.InputType)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "dtos.%s{", c.InputType)
	for i, f := range c.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: %s", f.Name, wireSample(f))
	}
	b.WriteString("}")
	return b.String()
}

// emitAggregateContractTest covers the three methods the FRAMEWORK calls and no
// rule test ever reaches: the declared child set, the service opt-in, and the
// Add<Child> door.
//
// They were the largest untested surface of a generated entity, and none of
// them fails loudly: AggregateChildren disagreeing with the schema is a bind
// refusal at boot, RequiresService flipping to false hands the rules a nil
// service, and an Add that does not reach the collection writes a root with no
// children and no error. A test that calls them is the difference between
// finding that here and finding it in a running service.
func emitAggregateContractTest(s *src, m *ir.Model) {
	if len(m.Children) == 0 && m.Service == nil {
		return
	}
	s.Doc(
		fmt.Sprintf("%s declares the aggregate contract the framework reads.", m.Entity.Pascal),
		"",
		"Nothing here is called by a rule, which is exactly why it is worth a test: "+
			"the framework calls it, and a disagreement surfaces at boot or as a write "+
			"that quietly saves nothing.")
	s.L("func Test%sDeclaresItsAggregateContract(t *testing.T) {", m.Entity.Pascal)
	s.L("\te := valid%s()", m.Entity.Pascal)
	if len(m.Children) > 0 {
		s.L("\tif got, want := len(e.AggregateChildren()), %d; got != want {", len(m.Children))
		s.L("\t\tt.Errorf(%s, got, want)",
			quote("the aggregate declares %d child collection(s), want %d — the schema binding compares this set"))
		s.L("\t}")
		for _, c := range m.Children {
			if c.Mounted {
				continue // the owner spec declares and tests it
			}
			s.L("\te.Add%s(aggregatevos.%s{})", c.Name, c.Name)
			s.L("\tif got := len(domain.GetCurrentItemsOf[aggregatevos.%s](&e.AggregateRoot)); got != 1 {", c.Name)
			s.L("\t\tt.Errorf(%s, got)",
				quote("Add"+c.Name+" left the collection at %d entries — the write would save a root with no children"))
			s.L("\t}")
		}
	}
	if m.Service != nil {
		s.L("\tif !e.RequiresService() {")
		s.L("\t\tt.Error(%s)",
			quote("the entity stopped requiring the domain service, and the rules that ask it would be handed a nil"))
		s.L("\t}")
	}
	s.L("}")
	s.Blank()
}

// emitFromEntityTest closes the OTHER half of the mapper: what the caller reads
// back after the write.
//
// The insert test above proves the request reaches the entity. Nothing proved
// the entity reaches the RESPONSE — and that direction has its own way of going
// wrong, because it unwraps every value object and projects every collection. A
// field dropped there is a write that succeeded and an answer that omits what
// was just saved, which reads to the caller as data loss.
func emitFromEntityTest(s *src, m *ir.Model, op *ir.Operation) {
	s.Doc(
		"What was written reads back through the result mapper.",
		"",
		"The round trip is the point: the same command builds the entity and then "+
			"projects it, so a field that survives one direction and not the other "+
			"fails here rather than in a caller's response.",
		"",
		"A SHARED-BASE role reaches the entity through ApplyTo rather than ToEntity "+
			"— its insert is an upsert, because another role may already have created "+
			"the identity — and this test used to skip that shape entirely. The result "+
			"mapper is the same mapper either way, and on a role it is also where a "+
			"computed field's derivation is called, so skipping it left the one seat "+
			"whose body is hand-written with no generated coverage at all.")
	s.L("func Test%sResultCarriesWhatWasWritten(t *testing.T) {", op.CommandType)
	s.L("\tctx := &configuration.AppContext{}")
	s.L("\tc := &%s{", op.CommandType)
	for _, f := range m.WritableFields() {
		s.L("\t\t%s: %s,", f.Name, wireSample(f))
	}
	for _, c := range m.Children {
		s.L("\t\t%s: []dtos.%s{{", c.GoPlural, c.InputType)
		for _, f := range c.Fields {
			if f.Nullable {
				continue
			}
			s.L("\t\t\t%s: %s,", f.Name, wireSample(f))
		}
		s.L("\t\t}},")
	}
	s.L("\t}")
	if op.InputMethod == "ToEntity" {
		s.L("\te, err := c.ToEntity(ctx)")
		s.L("\tif err != nil {")
		s.L("\t\tt.Fatalf(%s, err)", quote("ToEntity: %v"))
		s.L("\t}")
	} else {
		// ApplyTo is handed an entity that may already exist, and it must stay
		// pure because the handler may run it twice. An empty one is the
		// first-write case, which is the one this asserts.
		s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
		s.L("\tif err := c.ApplyTo(ctx, e); err != nil {")
		s.L("\t\tt.Fatalf(%s, err)", quote("ApplyTo: %v"))
		s.L("\t}")
	}
	// An id is minted by the framework on write; the projection dereferences it,
	// so the test has to stand one in or it panics on a nil.
	s.L("\te.SetID(domain.NewRandomID())")
	s.L("\tres, err := c.FromEntity(ctx, e)")
	s.L("\tif err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("FromEntity: %v"))
	s.L("\t}")
	for _, f := range m.WritableFields() {
		if f.Nullable {
			continue
		}
		s.L("\tif res.%s != %s {", f.Name, wireSample(f))
		s.L("\t\tt.Errorf(\"%s did not reach the result\")", f.Name)
		s.L("\t}")
	}
	for _, c := range m.Children {
		s.L("\tif len(res.%s) != 1 {", c.GoPlural)
		s.L("\t\tt.Errorf(%s, len(res.%s))",
			quote("the "+c.Plural+" collection reached the result with %d entries, want 1"), c.GoPlural)
		s.L("\t}")
	}
	s.L("}")
	s.Blank()
}

// emitChildInputTests covers the wire→domain mapper of every collection entry.
//
// It is its own file for a reason that is easy to miss: coverage is measured
// per PACKAGE, and `dtos` had no test of its own. The mapper was in fact
// exercised — the insert test builds children through it — but every report read
// it as 0%, so the one number a reviewer checks said "untested" about code that
// was covered, and said nothing about the day it stops being. A test inside the
// package answers both.
//
// What it asserts is the thing that goes wrong silently: a field added to the
// collection and forgotten in the mapper. The entry is accepted, the write
// succeeds, and the value is simply not there.
func emitChildInputTests(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package dtos")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("testing"))
	// Same reason as the DTO this covers: a `type: id` entry field samples as
	// domain.NewID(...), so the package is needed here too. Unconditional, and
	// pruned when unused.
	s.L("\t%s", quote(fwImport("domain")))
	if m.UsesVOsInChildren() {
		s.L("\t%s", quote("time"))
	}
	s.L(")")
	s.Blank()

	for _, c := range m.Children {
		if c.Mounted {
			// The input type belongs to the spec that DECLARES the identity's
			// collection; this run only mounts a surface over it. Testing it here
			// would put the same function name in the shared dtos package twice —
			// once per role — and neither copy would be testing this spec's code.
			continue
		}
		s.Doc(
			fmt.Sprintf("A %s entry carries every field it was sent with.", c.Name),
			"",
			"A field added to the collection and forgotten in this mapper is accepted "+
				"on the wire, saved without it, and reported nowhere.")
		s.L("func Test%s%sInputCarriesEveryField(t *testing.T) {", m.Entity.Pascal, c.Name)
		s.L("\tin := %s{", c.InputType)
		for _, f := range c.Fields {
			if f.Nullable {
				continue
			}
			s.L("\t\t%s: %s,", f.Name, wireSample(f))
		}
		s.L("\t}")
		s.L("\tgot := in.To%s()", c.Name)
		for _, f := range c.Fields {
			if f.Nullable {
				continue
			}
			s.L("\tif %s != %s {", entityAsWire(f, "got"), wireSample(f))
			s.L("\t\tt.Errorf(\"%s did not survive the mapper\")", f.Name)
			s.L("\t}")
		}
		s.L("}")
		s.Blank()
	}

	return goFile("internal/application/dtos/"+m.Entity.Snake+"_dtos_test.go", fsplan.Owned,
		fmt.Sprintf("tests for the %d collection input mapper(s)", len(m.Children)), s)
}

// emitPartialResultTest is the result half for a command that MUTATES an entity
// rather than building one — PATCH and PUT.
//
// Same reason as the insert twin: the response mapper unwraps the value objects
// and projects the collections, and a field missing there is a write that
// worked and an answer that does not show it. The construction differs only in
// how the entity comes to exist.
func emitPartialResultTest(s *src, m *ir.Model, op *ir.Operation) {
	if op.InputMethod == "ToEntity" || len(m.PatchableFields()) == 0 {
		return
	}
	s.Doc(
		"What a partial update applied reads back through the result mapper.",
		"",
		"The verb's own test proves the fields reach the ENTITY. This one proves they "+
			"reach the CALLER, which is a separate mapper with its own way of dropping "+
			"one.")
	s.L("func Test%sResultCarriesWhatWasApplied(t *testing.T) {", op.CommandType)
	s.L("\tctx := &configuration.AppContext{}")
	s.L("\te := &appdomain.%s{}", m.Entity.Pascal)
	s.L("\tc := &%s{", op.CommandType)
	for _, f := range m.PatchableFields() {
		if m.PatchExcludes[f.Name] {
			continue
		}
		s.L("\t\t%s: %s,", f.Name, patchSample(f))
	}
	s.L("\t}")
	s.L("\tif err := c.%s(ctx, e); err != nil {", op.InputMethod)
	s.L("\t\tt.Fatalf(%s, err)", quote(op.InputMethod+": %v"))
	s.L("\t}")
	s.L("\te.SetID(domain.NewRandomID())")
	s.L("\tres, err := c.FromEntity(ctx, e)")
	s.L("\tif err != nil {")
	s.L("\t\tt.Fatalf(%s, err)", quote("FromEntity: %v"))
	s.L("\t}")
	for _, f := range m.PatchableFields() {
		if m.PatchExcludes[f.Name] || f.Nullable {
			continue
		}
		s.L("\tif res.%s != %s {", f.Name, literalFor(f))
		s.L("\t\tt.Errorf(\"%s was applied and did not reach the result\")", f.Name)
		s.L("\t}")
	}
	s.L("}")
	s.Blank()
}

// emitPerChildRequestTests covers the wire types of the per-entry verbs.
//
// They were the last generated file at zero: the whole Add/Change/Remove
// surface of a collection — three requests, three responses, six mappers — with
// nothing calling them. The command side has tests, and that is precisely what
// made the gap invisible: the operation looked covered while the layer that
// feeds it was not, and a field dropped HERE is a request that parses, a
// command that runs and an entry saved without it.
func emitPerChildRequestTests(s *src, m *ir.Model) {
	for _, c := range m.Children {
		if !c.PerChild {
			continue
		}
		op := c.OpBase
		fields := writableChildFields(c)
		if len(fields) == 0 {
			continue
		}

		// A verb the collection does not mount has no wire types to assert
		// about, and a test naming them would not compile.
		if c.MountsAdd {
			emitAddChildRequestTest(s, c, op, fields)
		}
		if c.MountsChange {
			emitChangeChildRequestTest(s, c, op, fields)
		}
		if c.MountsAdd {
			emitAddChildResponseTest(s, m, c, op, fields)
		}
		if c.MountsChange {
			emitChangeChildResponseTest(s, m, c, op, fields)
		}
		if c.MountsRemove {
			emitRemoveChildWireTest(s, m, c, op)
		}
	}
}

// emitAddChildRequestTest proves the entry reaches the command that stores it.
func emitAddChildRequestTest(s *src, c ir.Child, op string, fields []ir.Field) {
	s.Doc(
		fmt.Sprintf("Add%sRequest carries the entry into its command.", op),
		"",
		"The body is the same entry shape the root's own body carries, so a field "+
			"forgotten here is saved as missing on a request that answered 201.")
	s.L("func TestAdd%sRequest_CarriesEveryField(t *testing.T) {", op)
	s.L("\tr := Add%sRequest{%sRequest: %sRequest{", op, c.Name, c.Name)
	for _, f := range fields {
		s.L("\t\t%s: %s,", f.Name, wireSample(f))
	}
	s.L("\t}}")
	s.L("\tcmd := r.ToCommand()")
	for _, f := range fields {
		s.L("\tif cmd.%s != %s {", f.Name, wireSample(f))
		s.L("\t\tt.Errorf(\"%s did not reach the command\")", f.Name)
		s.L("\t}")
	}
	s.L("}")
	s.Blank()
}

// emitChangeChildRequestTest proves BOTH halves arrive: the id from the path
// and the replacement from the body.
func emitChangeChildRequestTest(s *src, c ir.Child, op string, fields []ir.Field) {
	s.Doc(
		fmt.Sprintf("Change%sRequest names the entry AND carries the replacement.", op),
		"",
		"The id comes from the path and the body is a full replacement, so both "+
			"halves have to arrive: an id that does not reach the command changes the "+
			"wrong entry, or none.")
	s.L("func TestChange%sRequest_CarriesTheEntryAndItsID(t *testing.T) {", op)
	s.L("\tr := Change%sRequest{%sID: %s, %sRequest: %sRequest{", op, c.Name,
		quote("01890000-0000-7000-8000-000000000000"), c.Name, c.Name)
	for _, f := range fields {
		s.L("\t\t%s: %s,", f.Name, wireSample(f))
	}
	s.L("\t}}")
	s.L("\tcmd := r.ToCommand()")
	// The command carries the id as the WIRE type — a string; the conversion to
	// domain.ID happens further in. Comparing it as an ID would not compile.
	s.L("\tif cmd.%sID != %s {", c.Name, quote("01890000-0000-7000-8000-000000000000"))
	s.L("\t\tt.Error(\"the entry id did not reach the command, so the wrong entry would be replaced\")")
	s.L("\t}")
	for _, f := range fields {
		s.L("\tif cmd.%s != %s {", f.Name, wireSample(f))
		s.L("\t\tt.Errorf(\"%s did not reach the command\")", f.Name)
		s.L("\t}")
	}
	s.L("}")
	s.Blank()
}

// emitAddChildResponseTest proves the stored entry — and the id the server
// minted for it — reach the caller.
func emitAddChildResponseTest(s *src, m *ir.Model, c ir.Child, op string, fields []ir.Field) {
	s.Doc(
		fmt.Sprintf("The %s responses carry the stored entry back to the caller.", op),
		"",
		"The entry comes back with the id the SERVER minted, which is how the caller "+
			"addresses it afterwards — a response mapper that drops it answers 201 with "+
			"nothing to act on.")
	s.L("func TestAdd%sResponse_CarriesTheStoredEntry(t *testing.T) {", op)
	s.L("	ownerID := domain.NewRandomID()")
	s.L("	entryID := domain.NewRandomID()")
	s.L("	res := Add%sResponse{}.FromResult(commands.Add%sResult{", op, op)
	s.L("		%sID: ownerID,", m.Entity.Pascal)
	// The ENTRY result type is the COLLECTION's own, never the qualified
	// operation name: for a collection mounted from a shared identity, the
	// entry type belongs to the spec that declares the identity, and
	// `<Entity><Child>Result` names a type nothing declares.
	s.L("		%s: commands.%sResult{ID: entryID,", c.Name, c.Name)
	for _, f := range fields {
		s.L("			%s: %s,", f.Name, wireSample(f))
	}
	s.L("		},")
	s.L("	})")
	s.L("	if res.%sID != ownerID {", m.Entity.Pascal)
	s.L("		t.Error(\"the owner id did not reach the response\")")
	s.L("	}")
	s.L("	if res.%s.ID != entryID {", c.Name)
	s.L("		t.Error(\"the entry id the server minted did not reach the response\")")
	s.L("	}")
	for _, f := range fields {
		s.L("	if res.%s.%s != %s {", c.Name, f.Name, wireSample(f))
		s.L("		t.Errorf(\"%s did not reach the response\")", f.Name)
		s.L("	}")
	}
	s.L("}")
	s.Blank()
}

// emitChangeChildResponseTest proves the entry comes back with the id it KEPT.
func emitChangeChildResponseTest(s *src, m *ir.Model, c ir.Child, op string, fields []ir.Field) {
	// The CHANGE verb's response mapper had a request test and no response
	// test, while both its siblings had one — so the coverage report read 0%
	// for that one function, about code the other two paths prove is
	// reachable. A number that says "untested" about tested code is worse
	// than a low number: it is the one a reviewer stops trusting.
	s.Doc(
		fmt.Sprintf("The Change%s response carries the entry back with the id it KEEPS.", op),
		"",
		"A change replaces the entry's values and holds on to its row — so the id "+
			"coming back is the one the caller addressed, and a mapper that drops it "+
			"answers 200 with nothing to address next.")
	s.L("func TestChange%sResponse_CarriesTheStoredEntry(t *testing.T) {", op)
	s.L("	ownerID := domain.NewRandomID()")
	s.L("	entryID := domain.NewRandomID()")
	s.L("	res := Change%sResponse{}.FromResult(commands.Change%sResult{", op, op)
	s.L("		%sID: ownerID,", m.Entity.Pascal)
	s.L("		%s: commands.%sResult{ID: entryID,", c.Name, c.Name)
	for _, f := range fields {
		s.L("			%s: %s,", f.Name, wireSample(f))
	}
	s.L("		},")
	s.L("	})")
	s.L("	if res.%sID != ownerID {", m.Entity.Pascal)
	s.L("		t.Error(\"the owner id did not reach the response\")")
	s.L("	}")
	s.L("	if res.%s.ID != entryID {", c.Name)
	s.L("		t.Error(\"the entry kept its id and the response did not carry it\")")
	s.L("	}")
	for _, f := range fields {
		s.L("	if res.%s.%s != %s {", c.Name, f.Name, wireSample(f))
		s.L("		t.Errorf(\"%s did not reach the response\")", f.Name)
		s.L("	}")
	}
	s.L("}")
	s.Blank()
}

// emitRemoveChildWireTest covers the pair of the verb that answers with the
// owner alone.
func emitRemoveChildWireTest(s *src, m *ir.Model, c ir.Child, op string) {
	s.Doc(fmt.Sprintf("Remove%s answers with the owner, which is all it has to carry.", op))
	s.L("func TestRemove%sRequestAndResponse(t *testing.T) {", op)
	s.L("	r := Remove%sRequest{%sID: %s}", op, c.Name,
		quote("01890000-0000-7000-8000-000000000000"))
	s.L("	if r.ToCommand().%sID != %s {", c.Name,
		quote("01890000-0000-7000-8000-000000000000"))
	s.L("		t.Error(\"the entry id did not reach the command, so the wrong entry would be removed\")")
	s.L("	}")
	s.L("	ownerID := domain.NewRandomID()")
	// The composite literal is PARENTHESISED: at the head of an `if`, Go reads
	// the opening brace of `T{}` as the start of the block and refuses the file.
	s.L("	if (Remove%sResponse{}).FromResult(commands.Remove%sResult{%sID: ownerID}).%sID != ownerID {",
		op, op, m.Entity.Pascal, m.Entity.Pascal)
	s.L("		t.Error(\"the owner id did not reach the response\")")
	s.L("	}")
	s.L("}")
	s.Blank()
}

// writableChildFields are the entry fields a CALLER sends: the nullable ones are
// skipped by every mapper assertion here, for the same reason the root's tests
// skip them — a pointer sample compares by address, and the test would fail
// against a correct mapper.
func writableChildFields(c ir.Child) []ir.Field {
	var out []ir.Field
	for _, f := range c.Fields {
		if f.Nullable || f.Facet != "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// firstPlain is the first field of a list that stands on its own — not a part of
// a composite value object. It is what a test needs when it has to name one
// field of the ENTITY and any will do.
func firstPlain(fields []ir.Field) *ir.Field {
	for i := range fields {
		if fields[i].Composite == nil {
			return &fields[i]
		}
	}
	return nil
}

// childFieldsInline renders an entry's fields as they sit on a per-entry
// command — flat, one key per field, no wrapper.
//
// The nested spelling it replaced (`{Parcela: dtos.ParcelaInput{…}}`) belonged
// to a command that carried the entry as one value. A per-entry verb handles
// exactly ONE entry, so the entry IS the command.
func childFieldsInline(c ir.Child) string {
	var b strings.Builder
	for i, f := range c.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: %s", f.Name, wireSample(f))
	}
	return b.String()
}
