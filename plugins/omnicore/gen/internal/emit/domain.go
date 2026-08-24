package emit

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
)

func emitDomain(m *ir.Model) ([]fsplan.File, error) {
	var out []fsplan.File

	f, err := emitAggregate(m)
	if err != nil {
		return nil, err
	}
	out = append(out, f)

	if m.HasHookFile {
		f, err := emitRulesHook(m)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// emitAggregate writes the root aggregate: the struct, its modes and its rules.
func emitAggregate(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package domain")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote("time"))
	// The row-scope guard answers with the framework's own TenantMismatch, which
	// lives beside the other application notifications — it is offered there
	// precisely so a service does not declare its own per aggregate, and its
	// Forbidden semantic is already translated in all seven catalogs. Pruned
	// when the spec scopes nothing.
	s.L("\t%s", quote(fwImport("application/notifications")))
	s.L("\t%s", quote(fwImport("domain")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/vos")))
	s.L("\t%s", quote(m.ImportPath("internal/domain/aggregatevos")))
	s.L(")")
	s.Blank()

	// The struct.
	if m.TableDescription != "" {
		s.Doc(m.TableDescription)
		s.Doc("")
	}
	embedNote := "It embeds BaseEntity: this entity owns no child collection, so it is " +
		"not an aggregate root and does not implement AggregateRootProvider — which is " +
		"what routes it down the simpler single-table write path."
	if len(m.Children) > 0 {
		embedNote = "It embeds AggregateRoot — BaseEntity plus the carrier its child " +
			"collections live in — and implements AggregateRootProvider below, which is " +
			"what makes the root and its children one atomic write."
	}
	s.Doc(
		embedNote,
		"",
		"Persisted fields carry a labelKey tag and nothing else. There is no json tag "+
			"here on purpose: a domain aggregate is not a wire DTO, wire names live on the "+
			"web-layer types, and a json tag on this struct would corrupt the snapshot the "+
			"framework takes to compare old and new state.",
	)
	s.L("type %s struct {", m.Entity.Pascal)
	s.L("\t%s", rootEmbed(m))
	emitStructFields(s, m.Fields)
	for _, sib := range m.SiblingsOn("") {
		s.Blank()
		s.L("\t// The %s facet. It lives in its own table sharing this row's key, but", sib.Name)
		s.L("\t// there is no separate Go type: the split is physical only. All-nil")
		s.L("\t// means the row does not exist.")
		emitStructFields(s, sib.Fields)
	}
	if len(m.Children) > 0 {
		s.Blank()
		s.L("\t// No slice field for the children, deliberately: the framework keeps them")
		s.L("\t// in its own collection. A slice here would stay empty on every read and")
		s.L("\t// be ignored on every write.")
	}
	emitJoinStructFields(s, m.RootJoins(), "this entity")
	if len(m.Runtime) > 0 {
		s.Blank()
		s.L("\t// Fed from the caller's identity by the command mapper and read by the")
		s.L("\t// rules below. Never persisted, so it carries no labelKey and no column.")
		for _, f := range m.Runtime {
			s.L("\t%s %s%s", f.Name, f.GoType, fieldComment(f))
		}
	}
	s.L("}")
	s.Blank()

	emitModes(s, m)
	emitAggregateChildren(s, m)
	if m.Service != nil {
		s.Doc(
			"RequiresService opts in to the domain service.",
			"",
			"Declaring it obliges the wiring to inject one: the framework refuses the "+
				"write at invocation if it is missing, rather than passing a nil the rules "+
				"would dereference.",
		)
		s.L("func (e *%s) RequiresService() bool { return true }", m.Entity.Pascal)
		s.Blank()
	}
	emitBuildRules(s, m)
	emitChildMethods(s, m)

	return goFile("internal/domain/"+m.Entity.Snake+".go", fsplan.Owned,
		"the "+m.Entity.Pascal+" aggregate root, its modes and its rules", s)
}

func fieldComment(f ir.Field) string {
	if f.Description == "" {
		return ""
	}
	return " // " + strings.TrimSuffix(f.Description, ".")
}

func emitModes(s *src, m *ir.Model) {
	s.Doc(
		"Modes advertises the lifecycle verbs this aggregate accepts.",
		"",
		"The framework cross-checks this list against the schema at repository "+
			"construction: an archive verb without the archive column declared, or the "+
			"reverse, aborts the boot rather than failing later at a write.",
	)
	s.L("func (e *%s) Modes() []domain.EntityMode {", m.Entity.Pascal)
	s.L("\treturn []domain.EntityMode{")
	for _, mode := range modeConstants(m) {
		s.L("\t\t%s,", mode)
	}
	s.L("\t}")
	s.L("}")
	s.Blank()
}

func modeConstants(m *ir.Model) []string {
	var out []string
	seen := map[string]bool{}
	add := func(c string) {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	if m.Read.Enabled {
		add("domain.ModeDisplay")
	}
	for _, op := range m.Ops {
		switch op.Verb {
		case "insert":
			add("domain.ModeInsert")
		case "update", "patch":
			add("domain.ModeUpdate")
		case "delete":
			add("domain.ModeDelete")
		case "archive":
			add("domain.ModeArchive")
		case "unarchive":
			add("domain.ModeUnarchive")
		}
	}
	return out
}

func emitBuildRules(s *src, m *ir.Model) {
	s.Doc(
		"BuildRules declares the invariants. The framework dispatches each clause by "+
			"the verb being executed.",
		"",
		"Nothing here validates a value object: the framework discovers value-object "+
			"fields by type and validates them on every write, so writing those checks "+
			"again would only report the same problem twice.",
	)
	s.L("func (e *%s) BuildRules(actionName string, service domain.Service, r *domain.Rules) {",
		m.Entity.Pascal)

	// The row-scope guard goes FIRST, and deliberately: it decides whether this
	// caller may touch this row at all, so every invariant below it is being
	// checked on a write that is already established as the caller's to make.
	scoped := emitRowScopeGuard(s, m)

	if len(m.Clauses) == 0 && !m.HasHookFile && m.ArchiveWhen == nil && !scoped {
		s.L("\t// The spec declares no rule for this aggregate. The method still exists")
		s.L("\t// because the framework's entity contract requires it.")
		s.L("}")
		s.Blank()
		return
	}

	sawUpdate := false
	for _, clause := range m.Clauses {
		s.L("\tr.%s(func() {", clause.Gate)
		for i, rule := range clause.Rules {
			emitRule(s, m, clause.Gate, rule)
			emitGuardBarrier(s, rule, "\t\t", i < len(clause.Rules)-1)
		}
		// The lifecycle decision goes LAST in the update clause: every invariant
		// above it has had its say, and what this reads is the entity as the
		// write leaves it.
		if clause.Gate == "IfUpdate" {
			sawUpdate = true
			emitArchiveWhen(s, m)
		}
		s.L("\t})")
		s.Blank()
	}
	// An entity can declare the decision and no update rules at all, and the
	// clause has to exist for it to live in.
	if !sawUpdate && m.ArchiveWhen != nil {
		s.L("\tr.IfUpdate(func() {")
		emitArchiveWhen(s, m)
		s.L("\t})")
		s.Blank()
	}

	if m.HasHookFile {
		s.L("\t// Invariants the spec could not express declaratively. They live in")
		s.L("\t// %s_rules_manual.go, which the generator writes once and never touches again.", m.Entity.Snake)
		s.L("\te.customRules(actionName, service, r)")
	}
	s.L("}")
	s.Blank()

	emitRowScopeCheck(s, m)
}

// emitRowScopeGuard is the WRITE half of owner-only / tenant data access.
//
// The read half lives in the query, which forces the caller's scope into the
// filter. For a long time that was the only half generated, and the output
// looked complete: a reviewer read tenant isolation on the listings and
// reasonably concluded the posture was in place. It was not. A caller holding
// nothing but the ordinary permissions could
//
//   - CREATE a row inside another tenant — the insert mapper copies the tenant
//     straight from the request body, and no rule objected;
//   - EDIT one — the write path loads through the repository, not through the
//     filtered query, so the read-side filter is not on that path at all;
//   - ARCHIVE one — same load, and the bodyless verb's ApplyTo was a no-op.
//
// The asymmetry is what made it dangerous: the caller could not read back the
// row they had just archived, so the damage was invisible from their own side.
//
// So the check is here, in BuildRules, which is the one place every write verb
// passes through — and it reads the caller off the entity, because the domain
// performs no IO and has no other way to know who is asking.
//
// Returns whether anything was emitted, so the "this aggregate declares no
// rule" shortcut above does not fire over a guard that is right there.
func emitRowScopeGuard(s *src, m *ir.Model) bool {
	subject, caller := m.Authz.ScopeSubject(), m.Authz.ScopeField
	if !m.Authz.Scoped() || subject == nil || caller == nil {
		return false
	}
	what := "tenant"
	if m.Authz.DataAccess == "owner-only" {
		what = "owner"
	}

	s.L("\t// Row scoping, WRITE side. The read filter decides what this caller may")
	s.L("\t// SEE; this decides what they may create, edit and archive. Without it a")
	s.L("\t// caller writes into a %s that is not theirs and cannot read back what", what)
	s.L("\t// they wrote — damage that is invisible from the side that caused it.")
	s.L("\t//")
	s.L("\t// Every WRITE gate, one by one, and no display gate: a read is narrowed")
	s.L("\t// by the query's filter, and refusing it here would answer 403 where the")
	s.L("\t// contract is an empty page.")
	for _, gate := range scopeGates(m) {
		s.L("\tr.%s(func() { e.refuseForeign%s(r) })", gate, naming.Pascal(what))
	}
	s.Blank()
	return true
}

// scopeGates lists the write clauses the guard is registered under.
//
// Each verb is named EXPLICITLY rather than covered by one catch-all, because
// the framework dispatches a clause by mode and there is no "any write" gate:
// IfInsertOrUpdate covers POST/PUT/PATCH, and archive, unarchive and delete are
// each their own EntityMode with their own entry point. Archive in particular
// does NOT dispatch under IfUpdate — which is exactly the verb the report found
// a caller could use on another tenant's row.
func scopeGates(m *ir.Model) []string {
	var out []string
	seen := map[string]bool{}
	add := func(g string) {
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	for _, op := range m.Ops {
		switch op.Verb {
		case "insert", "update", "patch":
			add("IfInsertOrUpdate")
		case "archive":
			add("IfArchive")
		case "unarchive":
			add("IfUnarchive")
		case "delete":
			add("IfDelete")
		}
	}
	return out
}

// emitRowScopeCheck writes the guard's body, once, as a method the gates call.
//
// One body rather than one per gate: the question is identical in every verb —
// "is this row the caller's?" — and four copies of it are four places for the
// bypass or the policy to be edited into disagreement.
func emitRowScopeCheck(s *src, m *ir.Model) {
	subject, caller := m.Authz.ScopeSubject(), m.Authz.ScopeField
	if !m.Authz.Scoped() || subject == nil || caller == nil {
		return
	}
	what := "tenant"
	if m.Authz.DataAccess == "owner-only" {
		what = "owner"
	}

	var conds []string
	if m.Authz.NoIdentity == "stand-down" && m.Authz.PresenceField != nil {
		// A request with NO identity at all is provably a development bench:
		// the middleware is only bypassable with auth.mode disabled, and the
		// framework refuses that mode outside APP_PROFILE=dev. Under `refuse`
		// this clause is absent, so an empty caller matches no row and every
		// scoped write is refused — which is the default, for a reason.
		//
		// The question is PRESENCE, never the scope's emptiness. Those are two
		// different facts that arrive as one value: a token carrying no such
		// claim also lands here with an empty scope, and it is an ordinary
		// production request. Standing down for it would hand the whole write
		// guard to anyone holding a token without the claim.
		conds = append(conds, "e."+m.Authz.PresenceField.Name)
	}
	if m.Authz.BypassField != nil {
		conds = append(conds, "!e."+m.Authz.BypassField.Name)
	}
	conds = append(conds, fmt.Sprintf("%s != e.%s", scopeText(*subject, "e"), caller.Name))

	doc := []string{
		fmt.Sprintf("refuseForeign%s refuses a write to a row that is not the caller's.",
			naming.Pascal(what)),
		"",
		fmt.Sprintf("%s is filled from the request identity by every write command's "+
			"mapper, including the bodyless ones — an archive is a write to the row like "+
			"any other, and it loads through the repository, which the read side's filter "+
			"never touches.", caller.Name),
	}
	switch {
	case m.Authz.NoIdentity == "stand-down":
		doc = append(doc, "",
			"With NO IDENTITY AT ALL the check stands down: that is provably a "+
				"development bench, since the middleware is only bypassable with "+
				"auth.mode disabled and the framework refuses that mode outside "+
				"APP_PROFILE=dev. It asks whether an identity was PRESENT, never "+
				"whether the scope came out empty — a token that simply carries no "+
				"such claim is an ordinary production request and is still refused.")
	default:
		doc = append(doc, "",
			"With no identity at all the caller's scope is empty, so every write to a "+
				"row that HAS one is refused. That is the safe default and it is also "+
				"why a dev bench with auth disabled writes nothing here — declare "+
				"authz.noIdentity: stand-down if that bench is wanted.")
	}
	if m.Authz.BypassField != nil {
		who := "A caller holding " + m.Authz.Bypass
		if m.Authz.BypassWildcard {
			who = "A super-admin (" + m.Authz.Bypass + ")"
		}
		doc = append(doc, "",
			fmt.Sprintf("%s crosses the scope: the operator supporting a customer, who "+
				"has to be able to repair a row that is not theirs.", who))
	}
	s.Doc(doc...)
	s.L("func (e *%s) refuseForeign%s(r *domain.Rules) {", m.Entity.Pascal, naming.Pascal(what))
	s.L("\tif %s {", strings.Join(conds, " && "))
	s.L("\t\tr.AddNotification(%s, notifications.TenantMismatchNotification{})",
		quote(subject.Name))
	s.L("\t}")
	s.L("}")
	s.Blank()
}

// scopeText renders the row's own scope value as TEXT, which is what the
// caller's claim is. An id is unwrapped with Value(), a value object with the
// same call, and a plain string is already there.
func scopeText(f ir.Field, recv string) string {
	if f.SpecType == "id" {
		return recv + "." + f.Name + ".Value()"
	}
	return wireValue(f, recv)
}

func emitRule(s *src, m *ir.Model, gate string, rule ir.Rule) {
	emitRuleWith(s, m, gate, rule, "e")
}

// emitGuardBarrier writes the line that ends the validation pass after a rule
// the spec marked `guard: true`.
//
// It goes OUTSIDE the rule's own block — at the clause's own indentation, on
// the lines after it — and that placement is the whole design. Pushed inside
// the if, the barrier would fire on the first arm that rejected and hide the
// rest of what the same rule found; out here, every rule declared above it has
// already had its say, so four preconditions that must all be reported are four
// ordinary rules with the key on the LAST of them.
//
// StopIfInvalid is itself the condition — it returns without doing anything
// when nothing has been rejected — so there is no `if` to write around it and
// no `return` to write after it. The framework unwinds the body itself.
// more says whether a rule still follows in this clause; it only decides the
// blank line that sets the barrier apart from what it guards. A barrier that
// ends the clause has nothing to be set apart from, and the blank would be a
// gap above the closing brace.
func emitGuardBarrier(s *src, rule ir.Rule, indent string, more bool) {
	if !rule.Guard {
		return
	}
	s.L("%s// guard (%s): the rules below depend on these having passed.", indent, rule.ID)
	s.L("%sr.StopIfInvalid()", indent)
	if more {
		s.Blank()
	}
}

// emitRuleOn writes a rule against an arbitrary receiver, which is what lets a
// child reuse the same emitters as the root.
func emitRuleOn(s *src, gate string, rule ir.Rule, recv string) {
	emitRuleWith(s, nil, gate, rule, recv)
}

func emitRuleWith(s *src, m *ir.Model, gate string, rule ir.Rule, recv string) {
	if rule.Description != "" {
		for _, line := range wrap(rule.Description, 68) {
			s.L("\t\t// %s", line)
		}
	}
	// A hoisted rule's subject is the ENTRY, reached inside the pairing loop the
	// child emitters open — never the root receiver, which has no such field.
	// Its guard is emitted in there, on the entry.
	if guard := skipGuard(rule, recv); guard != "" && !rule.Hoisted {
		s.L("\t\t// %s", skipReason(rule))
		s.L("\t\tif %s {", guard)
		defer s.L("\t\t}")
	}
	switch rule.Kind {
	case "required":
		for _, f := range rule.Fields {
			if f.SpecType == "bool" {
				continue // a false boolean is a value; there is nothing to require
			}
			s.L("\t\tif %s {", zeroCheck(f, recv))
			s.L("\t\t\tr.AddNotification(%s, %s)", quote(f.Name), notifIn(m, rule.Notification))
			s.L("\t\t}")
		}
	case "immutable":
		emitImmutable(s, rule, recv, m)
	case "range":
		emitRange(s, rule, recv, m)
	case "length":
		emitLength(s, rule, recv, m)
	case "comparison":
		emitComparison(s, rule, recv, m)
	case "requiredIf":
		emitRequiredIf(s, rule, recv, m)
	case "transition":
		emitTransition(s, rule, recv, m)
	case "childTransition":
		if m != nil {
			emitChildTransition(s, m, rule)
		}
	case "childImmutable":
		if m != nil {
			emitChildImmutable(s, m, rule)
		}
	case "childDuplicate":
		if m != nil {
			emitChildDuplicate(s, m, rule)
		}
	case "groupCap":
		if m != nil {
			emitGroupCap(s, m, rule)
		}
	case "ownerCheck":
		emitOwnerCheck(s, rule, recv, m)
	case "uniquePrecheck":
		// Only the root has a service to ask.
		if m != nil {
			emitUniquePrecheck(s, m, rule)
		}
	case "factRange":
		if m != nil {
			emitFactRange(s, m, rule)
		}
	default:
		// Unreachable: validation refuses an unknown kind. Emitting a marker
		// rather than nothing means a gap can never pass silently.
		s.L("\t\t// unsupported rule kind %q (id %s) — this is a generator bug", rule.Kind, rule.ID)
	}
}

// pointerNeq renders "the two pointers do not carry the same value": nil and
// nil are the same, nil and set differ, set and set compare the pointed-at
// values. It is inlined into the generated file because the framework
// deliberately carries no helper for it — an earlier version emitted a
// `domain.SamePointer` that existed nowhere, and the file did not compile.
func pointerNeq(a, b string) string {
	return fmt.Sprintf("(%s == nil) != (%s == nil) || (%s != nil && *%s != *%s)", a, b, a, a, b)
}

// pointerEq is the affirmative twin of pointerNeq, parenthesised so it can be
// joined with && by a caller.
func pointerEq(a, b string) string {
	return fmt.Sprintf("((%s == nil) == (%s == nil) && (%s == nil || *%s == *%s))", a, b, a, a, b)
}

// emitImmutable compares against the pre-write snapshot.
//
// The nil guard is not defensive noise: on an insert there is no previous
// state, and dereferencing the snapshot there would panic.
func emitImmutable(s *src, rule ir.Rule, recv string, m *ir.Model) {
	s.L("\t\tif old := domain.Old(%s); old != nil {", recv)
	for _, f := range rule.Fields {
		cmp := fmt.Sprintf("old.%s != %s.%s", f.Name, recv, f.Name)
		if f.Nullable {
			cmp = pointerNeq("old."+f.Name, fmt.Sprintf("%s.%s", recv, f.Name))
		}
		s.L("\t\t\tif %s {", cmp)
		s.L("\t\t\t\tr.AddNotification(%s, %s%s)",
			quote(f.Name), notifIn(m, rule.Notification), echoArgOn(rule, f, recv))
		s.L("\t\t\t}")
	}
	s.L("\t\t}")
}

func emitRange(s *src, rule ir.Rule, recv string, m *ir.Model) {
	for _, f := range rule.Fields {
		var conds []string
		val := deref(f, recv)
		if rule.Min != nil {
			conds = append(conds, fmt.Sprintf("%s < %s", val, number(*rule.Min, f.SpecType)))
		}
		if rule.Max != nil {
			conds = append(conds, fmt.Sprintf("%s > %s", val, number(*rule.Max, f.SpecType)))
		}
		body := strings.Join(conds, " || ")
		if f.Nullable {
			s.L("\t\tif %s.%s != nil && (%s) {", recv, f.Name, body)
		} else {
			s.L("\t\tif %s {", body)
		}
		s.L("\t\t\tr.AddNotification(%s, %s%s)",
			quote(f.Name), notifLiteralFor(rule, m), echoArgOn(rule, f, recv))
		s.L("\t\t}")
	}
}

func emitLength(s *src, rule ir.Rule, recv string, m *ir.Model) {
	for _, f := range rule.Fields {
		val := deref(f, recv)
		var conds []string
		if rule.Min != nil {
			conds = append(conds, fmt.Sprintf("len(%s) < %d", val, int(*rule.Min)))
		}
		if rule.Max != nil {
			conds = append(conds, fmt.Sprintf("len(%s) > %d", val, int(*rule.Max)))
		}
		body := strings.Join(conds, " || ")
		if f.Nullable {
			s.L("\t\tif %s.%s != nil && (%s) {", recv, f.Name, body)
		} else {
			s.L("\t\tif %s {", body)
		}
		s.L("\t\t\tr.AddNotification(%s, %s%s)",
			quote(f.Name), notifLiteralFor(rule, m), echoArgOn(rule, f, recv))
		s.L("\t\t}")
	}
}

// emitComparison guards both operands before comparing.
//
// The nil-and-zero guard mirrors what hand-written rules actually need: an
// absent optional date must not be compared, and a zero instant is absence too.
func emitComparison(s *src, rule ir.Rule, recv string, m *ir.Model) {
	if rule.Other == nil || len(rule.Fields) == 0 {
		return
	}
	left, right := rule.Fields[0], *rule.Other
	// A field that cannot be absent needs no guard, and emitting "true" for it
	// is not merely noise: two of them in a row is `true && true`, which vet
	// rejects as a redundant condition and which therefore fails the generated
	// project's own checks.
	var conds []string
	for _, g := range []string{comparisonGuard(left, recv), comparisonGuard(right, recv)} {
		if g != "" && g != "true" {
			conds = append(conds, g)
		}
	}
	conds = append(conds, comparisonExpr(left, right, rule.Operator, recv))
	s.L("\t\tif %s {", strings.Join(conds, " && "))
	s.L("\t\t\tr.AddNotification(%s, %s%s)",
		quote(left.Name), notifIn(m, rule.Notification), echoArgOn(rule, left, recv))
	s.L("\t\t}")
}

func comparisonGuard(f ir.Field, recv string) string {
	ref := recv + "." + f.Name
	switch {
	case f.Nullable && f.SpecType == "time":
		return fmt.Sprintf("(%s != nil && !%s.IsZero())", ref, ref)
	case f.Nullable:
		return fmt.Sprintf("%s != nil", ref)
	case f.SpecType == "time":
		return fmt.Sprintf("!%s.IsZero()", ref)
	default:
		return "true"
	}
}

// comparisonExpr renders the FAILING condition — the rule fires when the
// declared relation does NOT hold.
func comparisonExpr(left, right ir.Field, op, recv string) string {
	l, r := deref(left, recv), deref(right, recv)
	if left.SpecType == "time" {
		// A pointer receiver calls the method directly. Dereferencing first
		// parses as *(x.Before(y)) — indirection binds looser than the call —
		// which is a type error rather than the comparison that was meant.
		l = recv + "." + left.Name
		if right.Nullable {
			r = "*" + recv + "." + right.Name
		} else {
			r = recv + "." + right.Name
		}
		switch op {
		case "gte":
			return fmt.Sprintf("%s.Before(%s)", l, r)
		case "gt":
			return fmt.Sprintf("!%s.After(%s)", l, r)
		case "lte":
			return fmt.Sprintf("%s.After(%s)", l, r)
		case "lt":
			return fmt.Sprintf("!%s.Before(%s)", l, r)
		case "eq":
			return fmt.Sprintf("!%s.Equal(%s)", l, r)
		case "ne":
			return fmt.Sprintf("%s.Equal(%s)", l, r)
		}
	}
	inverse := map[string]string{"gte": "<", "gt": "<=", "lte": ">", "lt": ">=", "eq": "!=", "ne": "=="}
	return fmt.Sprintf("%s %s %s", l, inverse[op], r)
}

func emitOwnerCheck(s *src, rule ir.Rule, recv string, m *ir.Model) {
	if rule.OwnerField == nil {
		return
	}
	owner := rule.OwnerField
	s.Doc("")
	s.L("\t\t// Stands down when the request carried NO IDENTITY: with authentication")
	s.L("\t\t// disabled in development, and inside tests that bypass the middleware,")
	s.L("\t\t// none is attached and the check would otherwise reject every call.")
	s.L("\t\t//")
	s.L("\t\t// It asks about PRESENCE, never about the principal being empty. Those")
	s.L("\t\t// are two different facts that arrive as one value: a real, signed token")
	s.L("\t\t// that simply carries no such claim also leaves the field empty, and it")
	s.L("\t\t// is an ordinary production request. Standing down for it would let")
	s.L("\t\t// anyone holding a claimless token edit a row that is not theirs.")
	if len(rule.Fields) == 0 {
		return
	}
	target := rule.Fields[0]
	// The owner field is a runtime string (a token claim), so the field it is
	// compared against has to be a string too — and a value-object field is not
	// one until it is unwrapped. Comparing them directly does not compile, which
	// is the good case; what it looked like was an example that validated and
	// produced a tree that did not build.
	// scopeText, not wireValue: the row's owner may be an `id`, and an id is
	// compared to a claim by its text. wireValue leaves a domain.ID whole, which
	// does not compile against a string — the good failure, but only reached
	// after the spec had already said yes.
	// The resolver synthesises the presence field for every spec carrying an
	// ownerCheck, and the kind is refused on a collection (where there is no
	// model), so this is always the presence form in practice. The fallback is
	// kept only so the function stays total.
	present := fmt.Sprintf("%s.%s != \"\"", recv, owner.Name)
	if m != nil && m.Authz.PresenceField != nil {
		present = fmt.Sprintf("%s.%s", recv, m.Authz.PresenceField.Name)
	}
	cond := fmt.Sprintf("%s && %s != %s.%s",
		present, scopeText(target, recv), recv, owner.Name)
	if rule.AdminField != nil {
		// The bypass is a separate question from the permission: the permission
		// says who may attempt the verb, this says who may attempt it on a row
		// that is not theirs.
		s.L("\t\t// %s lets a privileged caller through a row that is not theirs.", rule.AdminField.Name)
		cond += fmt.Sprintf(" && !%s.%s", recv, rule.AdminField.Name)
	}
	s.L("\t\tif %s {", cond)
	s.L("\t\t\tr.AddNotification(%s, %s)", quote("ID"), notifIn(m, rule.Notification))
	s.L("\t\t}")
}

// notifLiteral renders a notification value, qualifying the framework's own.
func notifLiteral(name string) string {
	if name == "" {
		return "domain.RequiredFieldNotification{}"
	}
	if frameworkNotifications[name] {
		return "domain." + name + "{}"
	}
	return name + "{}"
}

var frameworkNotifications = map[string]bool{
	"RequiredFieldNotification":      true,
	"SchemaViolationNotification":    true,
	"RecordNotFoundNotification":     true,
	"EntityAlreadyAddedNotification": true,
	"EntityDoesNotExistNotification": true,
	"ArchiveNotAllowedNotification":  true,
}

// echoArg passes the rejected value back so the caller sees what was refused.
//
// It hardcodes the ROOT's receiver. A rule declared on a collection is emitted
// inside aggregatevos against the entry's own receiver, where `e` does not
// exist — reach for echoArgOn there. A comparison rule on a child went through
// this one and emitted `e.Field` into a method that has no `e`, which nothing
// noticed while every fixture left echoValue off.
func echoArg(rule ir.Rule, f ir.Field) string { return echoArgOn(rule, f, "e") }

func echoArgOn(rule ir.Rule, f ir.Field, recv string) string {
	if !rule.EchoValue {
		return ""
	}
	return ", " + recv + "." + f.Name
}

func number(v float64, specType string) string {
	switch specType {
	case "int", "int64":
		return fmt.Sprintf("%d", int64(v))
	default:
		return strings.TrimSuffix(fmt.Sprintf("%g", v), ".0")
	}
}

// writeManualRuleGates emits the residual rules GROUPED BY VERB: one
// r.IfInsert / r.IfInsertOrUpdate / … block holding every rule scoped to it,
// each with the description the spec wrote for it.
//
// The grouping is the whole point. Emitting a gate per rule produced a file of
// near-identical two-line closures where the reader had to diff the wrappers to
// find the rules, and made the framework run the same verb check once per rule
// on every write. Rules that fire on the same verb read as one block, which is
// also the shape the generated BuildRules above it already has.
//
// A rule scoped to several verbs appears under each of them: it genuinely runs
// on each, and the doc comment tells the author to implement it once as a method
// and call it from both blocks.
func writeManualRuleGates(s *src, rules []ir.ManualRule) {
	byGate := map[string][]ir.ManualRule{}
	var order []string
	for _, mr := range rules {
		gates := mr.Gates
		if len(gates) == 0 {
			gates = []string{"IfInsertOrUpdate"}
		}
		for _, g := range gates {
			if _, seen := byGate[g]; !seen {
				order = append(order, g)
			}
			byGate[g] = append(byGate[g], mr)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		return ir.GateRank(order[i]) < ir.GateRank(order[j])
	})

	for i, gate := range order {
		// Between the gates, never before the first: with nothing above it, a
		// leading blank line is just a gap under the signature.
		if i > 0 {
			s.Blank()
		}
		s.L("\tr.%s(func() {", gate)
		for i, mr := range byGate[gate] {
			if i > 0 {
				s.Blank()
			}
			s.L("\t\t// ── %s ──", mr.ID)
			for _, line := range wrap(mr.Description, 66) {
				s.L("\t\t// %s", line)
			}
			if mr.Notification != "" {
				s.L("\t\t// Notification to raise: %s{}", mr.Notification)
			}
			if mr.AttachTo != "" {
				s.L("\t\t// Attach it to the field: %s", quote(mr.AttachTo))
			}
			s.L("\t\t// TODO(%s): implement the rule described above.", mr.ID)
		}
		s.L("\t})")
	}
}

// manualGateDoc is the shape instruction both hook files open with.
//
// It exists because the natural way to write these — one gate per rule — is
// also the wrong one, and a hook file that shows the wrong shape teaches it:
// the next rule is added the way the ones above it look.
const manualGateDoc = "ONE gate per verb, holding every rule that runs on that verb — the " +
	"shape the generated BuildRules already has. A gate per rule reads as a wall of " +
	"near-identical closures and makes the framework dispatch the same verb once per rule " +
	"on every write; rules that share a verb belong in the same block. A rule that appears " +
	"under two gates is still ONE rule: write it as a method and call it from both, rather " +
	"than as two copies that can drift apart."

// emitRulesHook writes the escape hatch: created once, then the author's.
//
// It is generated WITH the spec's own description of each residual rule, so the
// person implementing it reads what they have to enforce instead of an empty
// function.
func emitRulesHook(m *ir.Model) (fsplan.File, error) {
	s := &src{}
	s.Blank()
	s.L("package domain")
	s.Blank()
	s.L("import %s", quote(fwImport("domain")))
	s.Blank()

	s.Doc(
		"customRules is called at the end of the generated BuildRules, with the same "+
			"arguments, and reports a violation the same way: r.AddNotification.",
		"",
		manualGateDoc,
	)
	s.L("func (e *%s) customRules(actionName string, service domain.Service, r *domain.Rules) {",
		m.Entity.Pascal)
	writeManualRuleGates(s, m.ManualRules)
	s.L("}")

	f, err := goFile("internal/domain/"+m.Entity.Snake+"_rules_manual.go", fsplan.Hook,
		fmt.Sprintf("the hand-written rules for %s (%d to implement)",
			m.Entity.Pascal, len(m.ManualRules)), s)
	if err != nil {
		return f, err
	}
	f.Consequence = "Until these are written the service runs and accepts writes — it " +
		"simply does not enforce the invariants the spec described. That is quiet, " +
		"which is exactly why it is worth doing now rather than later."
	return f, nil
}

// emitUniquePrecheck asks the domain service whether the value is taken.
//
// The assertion is done in one line with no comma-ok and no nil guard, on
// purpose: RequiresService already guarantees a non-nil service of the right
// type before rules run, so a defensive branch here would be unreachable code
// that only makes the rule harder to read. A domain file has no panic path —
// the single way a rule rejects is AddNotification.
func emitUniquePrecheck(s *src, m *ir.Model, rule ir.Rule) {
	if rule.Fact == nil || len(rule.Fields) == 0 {
		return
	}
	f := rule.Fields[0]
	// A composite is unique as a TUPLE, so the rule was synthesised on the first
	// part — the one carrying the key — and everything the reader sees has to
	// name the value object instead: the gate reads every part off it, and the
	// notification lands on the concept rather than on whichever part came
	// first, which is the same name the constraint binding reports.
	gate, attach, echo := notEmpty(f, "e"), f.Name, echoArg(rule, f)
	if f.Composite != nil {
		// And the echo goes silent: the part's logical name is not a field of the
		// entity (the entity holds the value object), and echoing the owner hands
		// back a formatted struct. What was refused is the TUPLE, which no single
		// value stands for — the same reason a multi-field business identity
		// echoes nothing.
		gate, attach, echo = compositeNotEmpty(m, f.Composite.Owner, "e"), f.Composite.Owner, ""
	}
	s.L("\t\t// The database unique index is the backstop for the race between this")
	s.L("\t\t// check and the commit; asking here is what lets the duplicate be")
	s.L("\t\t// reported together with the other problems instead of alone, later.")
	s.L("\t\tif %s {", gate)
	var args []string
	needsSelf := false
	for _, p := range rule.Fact.Params {
		if p.Role == "exclude-self" {
			needsSelf = true
			args = append(args, "selfID")
			continue
		}
		args = append(args, factArgValue(fieldNamed(m, p.Field), "e"))
	}
	if needsSelf {
		s.L("\t\t\t// On an insert there is no row yet, so there is nothing to exclude —")
		s.L("\t\t\t// and the id is not minted until after the rules run.")
		s.L("\t\t\tvar selfID domain.ID")
		s.L("\t\t\tif id := e.GetID(); id != nil {")
		s.L("\t\t\t\tselfID = *id")
		s.L("\t\t\t}")
	}
	// The aggregate and its port live in the SAME package, so the interface is
	// named bare rather than qualified.
	s.L("\t\t\tif service.(%sService).%s(%s) {",
		m.Entity.Pascal, rule.Fact.Name, strings.Join(args, ", "))
	s.L("\t\t\t\tr.AddNotification(%s, %s%s)",
		quote(attach), notifIn(m, rule.Notification), echo)
	s.L("\t\t\t}")
	s.L("\t\t}")
}

// compositeNotEmpty gates the probe on the value object carrying a value, part
// by part. There is no single reference to test — the entity holds the concept
// and the parts live inside it — so the run is read off the owner and joined,
// which also matches what the query is about to ask: the whole tuple.
func compositeNotEmpty(m *ir.Model, owner, recv string) string {
	var conds []string
	for _, f := range m.AllOwnerFields() {
		if f.Composite == nil || f.Composite.Owner != owner {
			continue
		}
		part := f
		part.Name = f.Composite.PartName
		part.Nullable = f.Composite.PartNullable
		if c := notEmpty(part, recv+"."+owner); c != "true" {
			conds = append(conds, c)
		}
	}
	if len(conds) == 0 {
		return "true"
	}
	return strings.Join(conds, " && ")
}

// notEmpty gates the probe on there being a value to look up — asking the
// database about an empty string is a query that can only waste a round trip.
func notEmpty(f ir.Field, recv string) string {
	ref := recv + "." + f.Name
	if f.Nullable {
		return ref + " != nil"
	}
	switch f.SpecType {
	case "string":
		return ref + " != \"\""
	case "time":
		return "!" + ref + ".IsZero()"
	case "id":
		return "!" + ref + ".IsEmpty()"
	case "bool":
		// A flag always carries a value; validation refuses unique on one, so
		// this is only reachable defensively.
		return "true"
	default:
		return ref + " != 0"
	}
}

func fieldNamed(m *ir.Model, name string) ir.Field {
	for _, f := range m.Fields {
		if f.Name == name {
			return f
		}
	}
	return ir.Field{Name: name}
}

// emitAggregateChildren declares the TYPE set of the aggregate's children.
//
// The framework cross-checks it against the schema's child declarations and
// panics when the two disagree, so both are generated from the same source.
// rootEmbed is what the aggregate embeds, and the choice is made to say
// something TRUE about the entity rather than to cover both cases.
//
// AggregateRoot is BaseEntity plus the carrier a root keeps its child
// collections in. The framework dispatches on the INTERFACE — an entity is
// treated as an aggregate when it implements AggregateRootProvider, which this
// generator emits only for an entity that HAS children — so embedding the
// carrier in a childless entity changed no behaviour at all. What it did was
// tell every reader of the file that the entity has collections, in the one
// place they would look to find out.
func rootEmbed(m *ir.Model) string {
	if len(m.Children) > 0 {
		return "domain.AggregateRoot"
	}
	return "domain.BaseEntity"
}

func emitAggregateChildren(s *src, m *ir.Model) {
	if len(m.Children) == 0 {
		return
	}
	s.Doc(
		"GetAggregateRoot exposes the carrier the framework keeps the children in.",
		"",
		"An aggregate WITHOUT children does not declare it, and that absence is what "+
			"routes it down the simpler single-table write path.",
	)
	s.L("func (e *%s) GetAggregateRoot() *domain.AggregateRoot {", m.Entity.Pascal)
	s.L("\treturn &e.AggregateRoot")
	s.L("}")
	s.Blank()

	s.Doc(
		"AggregateChildren declares which value objects belong to this aggregate.",
		"",
		"The framework matches this set against the schema's child declarations and "+
			"refuses to bind them when they disagree.",
	)
	s.L("func (e *%s) AggregateChildren() []domain.AggregateValueObject {", m.Entity.Pascal)
	var items []string
	for _, c := range m.Children {
		items = append(items, "aggregatevos."+c.Name+"{}")
	}
	s.L("\treturn []domain.AggregateValueObject{%s}", strings.Join(items, ", "))
	s.L("}")
	s.Blank()
}

// emitChildMethods writes the aggregate's vocabulary for its collections.
//
// None of these methods prepares the entity for anything. The add-with-guard
// emits before delegating and, on the insert path, runs inside ToEntity — ahead
// of everything the framework does — and that is fine: an entity carries its
// notification context from construction.
func emitChildMethods(s *src, m *ir.Model) {
	for _, c := range m.Children {
		s.Doc(
			fmt.Sprintf("%s adds one entry to the %s collection.", c.AddMethod, c.Segment),
			"",
			"A duplicate is rejected by business identity, not by comparing every "+
				"field, so re-sending an entry with a cosmetic change updates it in place "+
				"instead of creating a second one.",
		)
		s.L("func (e *%s) %s(item aggregatevos.%s) {", m.Entity.Pascal, c.AddMethod, c.Name)
		if c.PerChild && c.DuplicateNotification != "" {
			s.L("\t// Adding ONE entry can collide with what is already there, and the")
			s.L("\t// caller asked for this entry rather than for a whole collection — so")
			s.L("\t// the collision is an answer, not a silent merge.")
			s.L("\tfor _, existing := range domain.GetCurrentItemsOf[aggregatevos.%s](e.GetAggregateRoot()) {", c.Name)
			s.L("\t\tif existing.IsSameBusinessIdentity(item) {")
			s.L("\t\t\te.AddNotification(%s, %s%s)", quote(c.GoPlural),
				notifIn(m, c.DuplicateNotification), addedEcho(c))
			s.L("\t\t\treturn")
			s.L("\t\t}")
			s.L("\t}")
		}
		s.L("\tdomain.AddAggregateChild(e, item)")
		s.L("}")
		s.Blank()

		if !c.PerChild {
			continue
		}

		// A verb the collection does not mount gets no domain method either.
		// Writing one anyway would leave `ChangeXByID` on the aggregate with
		// nothing calling it — an invitation to a hand-written route that
		// reintroduces exactly the verb the spec decided against.
		if c.MountsChange {
			emitChangeChildMethod(s, m, c)
		}
		if c.MountsRemove {
			emitRemoveChildMethod(s, m, c)
		}
	}
}

// emitChangeChildMethod is the aggregate's "replace ONE entry, keep its id".
func emitChangeChildMethod(s *src, m *ir.Model, c ir.Child) {
	s.Doc(
		fmt.Sprintf("%s replaces ONE entry, keeping its id.", c.ChangeMethod),
		"",
		"Keeping the id is the whole point: the row is updated rather than "+
			"removed and re-added, so whatever references it still does, and the "+
			"audit trail reads as a change instead of as a deletion plus a creation.",
		"",
		"An id that is not in the collection is NOT silently ignored — it answers "+
			"not-found, because the caller addressed a specific entry.")
	s.L("func (e *%s) %s(id string, replacement aggregatevos.%s) {", m.Entity.Pascal, c.ChangeMethod, c.Name)
	s.L("\tfor _, current := range domain.GetCurrentItemsOf[aggregatevos.%s](e.GetAggregateRoot()) {", c.Name)
	s.L("\t\tif current.GetID().Value() == id {")
	s.L("\t\t\treplacement.SetID(domain.NewID(id))")
	s.L("\t\t\tdomain.ChangeAggregateChild(e, current, replacement)")
	s.L("\t\t\treturn")
	s.L("\t\t}")
	s.L("\t}")
	s.L("\te.AddNotification(%s, domain.RecordNotFoundNotification{}, id)", quote(c.Name))
	s.L("}")
	s.Blank()
}

// emitRemoveChildMethod is the aggregate's "take ONE entry out".
func emitRemoveChildMethod(s *src, m *ir.Model, c ir.Child) {
	s.Doc(
		fmt.Sprintf("%s takes ONE entry out of the collection.", c.RemoveMethod),
		"",
		"Same not-found posture as the change: the caller named an entry, so a "+
			"missing one is an answer rather than a no-op.")
	s.L("func (e *%s) %s(id string) {", m.Entity.Pascal, c.RemoveMethod)
	s.L("\tfor _, current := range domain.GetCurrentItemsOf[aggregatevos.%s](e.GetAggregateRoot()) {", c.Name)
	s.L("\t\tif current.GetID().Value() == id {")
	s.L("\t\t\tdomain.RemoveAggregateChild(e, current)")
	s.L("\t\t\treturn")
	s.L("\t\t}")
	s.L("\t}")
	s.L("\te.AddNotification(%s, domain.RecordNotFoundNotification{}, id)", quote(c.Name))
	s.L("}")
	s.Blank()
}

// notifLiteralFor builds the notification value for a rule emitted where the
// type is spelled BARE — inside vos, or inside aggregatevos, which is where the
// kinds that fill variables mostly land.
func notifLiteralFor(rule ir.Rule, m *ir.Model) string {
	return bindTVars(notifLiteral(rule.Notification), rule, m)
}

// notifInFor is the same thing for a rule emitted in the ROOT's package, where a
// notification the resolver moved has to be qualified.
//
// The two exist separately because the qualification is relative to the package
// DOING the emitting, not to the notification: notifIn's answer is right in
// domain and wrong in vos, where it would spell vos.X inside vos itself.
func notifInFor(m *ir.Model, rule ir.Rule) string {
	return bindTVars(notifIn(m, rule.Notification), rule, m)
}

// bindTVars fills the interpolation variables the rule can supply onto an
// already-spelled literal.
//
// Declaring a variable and never setting it is worse than not declaring it: the
// catalog keeps its {min}, the renderer substitutes nothing, and the end user
// reads "between  and ." — a message that looks written rather than broken.
func bindTVars(base string, rule ir.Rule, m *ir.Model) string {
	name := rule.Notification
	if name == "" || frameworkNotifications[name] || m == nil {
		return base
	}
	var parts []string
	for _, v := range tvarsOf(m, name) {
		if arg, ok := tvarValue(rule, v); ok {
			parts = append(parts, arg)
		}
	}
	if len(parts) == 0 {
		return base
	}
	return strings.TrimSuffix(base, "{}") + "{" + strings.Join(parts, ", ") + "}"
}

// tvarValue answers where ONE declared variable gets its value from, or says it
// cannot be sourced. It is the whole binding vocabulary in one place, so a kind
// that gains a bound cannot quietly leave the placeholder empty.
//
// A cap answers {max} as well as {cap}: "at most {max} permissions" is how a
// domain writes the message, and refusing to fill it because the KEY in the
// spec is spelled `cap` would be the generator arguing about vocabulary with
// the end user's sentence. It fills {cap} too, for a spec that names the bound
// after the key.
func tvarValue(rule ir.Rule, v string) (string, bool) {
	switch v {
	case "min":
		if rule.Min != nil {
			return fmt.Sprintf("Min: %q", trimNumber(*rule.Min)), true
		}
	case "max":
		if rule.Max != nil {
			return fmt.Sprintf("Max: %q", trimNumber(*rule.Max)), true
		}
		if rule.Cap > 0 {
			return fmt.Sprintf("Max: %q", strconv.Itoa(rule.Cap)), true
		}
	case "cap":
		if rule.Cap > 0 {
			return fmt.Sprintf("Cap: %q", strconv.Itoa(rule.Cap)), true
		}
	}
	return "", false
}

func tvarsOf(m *ir.Model, name string) []string {
	for _, n := range m.Notifications {
		if n.Name == name {
			return n.TVars
		}
	}
	return nil
}

func trimNumber(v float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%g", v), ".0")
}

// emitRequiredIf writes "this field is required, but only when that one is
// filled in".
//
// The pair is what makes it different from `required`: asking for a
// justification on every record is a different rule from asking for one only
// when the record was rejected, and the second is the one most domains want.
func emitRequiredIf(s *src, rule ir.Rule, recv string, m *ir.Model) {
	if rule.Other == nil || len(rule.Fields) == 0 {
		return
	}
	s.L("\t\tif %s {", presentCheck(*rule.Other, recv))
	for _, f := range rule.Fields {
		s.L("\t\t\tif %s {", zeroCheck(f, recv))
		s.L("\t\t\t\tr.AddNotification(%s, %s%s)",
			quote(f.Name), notifIn(m, rule.Notification), echoArgOn(rule, f, recv))
		s.L("\t\t\t}")
	}
	s.L("\t\t}")
}

// emitTransition writes the state machine.
//
// It reads the PREVIOUS value, so it only exists on update — an insert has
// nothing to move from. A value that did not change is always allowed: the
// alternative would reject an unrelated edit that merely carries the state
// along, which every PUT does.
func emitTransition(s *src, rule ir.Rule, recv string, m *ir.Model) {
	if len(rule.Fields) == 0 || len(rule.Transitions) == 0 {
		return
	}
	// Validation refuses a nullable state, so the reads below never need a nil
	// guard — an absent state is modelled as an explicit enum member instead.
	f := rule.Fields[0]
	ref := recv + "." + f.Name
	value := ref
	if f.VOKind != "" {
		value = ref + ".Value()"
	}

	s.L("\t\t// The allowed moves. A state not listed here can only stay where it is,")
	s.L("\t\t// and staying is always allowed — a PUT carries the value along.")
	s.L("\t\tif old := domain.Old(%s); old != nil {", recv)
	oldValue := "old." + f.Name
	if f.VOKind != "" {
		oldValue += ".Value()"
	}
	s.L("\t\t\tallowed := map[%s][]%s{", transitionKeyType(f), transitionKeyType(f))
	for _, from := range sortedKeys(rule.Transitions) {
		var quoted []string
		for _, to := range rule.Transitions[from] {
			quoted = append(quoted, quote(to))
		}
		s.L("\t\t\t\t%s: {%s},", quote(from), strings.Join(quoted, ", "))
	}
	s.L("\t\t\t}")
	s.L("\t\t\tif %s != %s {", oldValue, value)
	s.L("\t\t\t\tok := false")
	s.L("\t\t\t\tfor _, to := range allowed[%s] {", oldValue)
	s.L("\t\t\t\t\tif to == %s {", value)
	s.L("\t\t\t\t\t\tok = true")
	s.L("\t\t\t\t\t\tbreak")
	s.L("\t\t\t\t\t}")
	s.L("\t\t\t\t}")
	s.L("\t\t\t\tif !ok {")
	s.L("\t\t\t\t\tr.AddNotification(%s, %s%s)",
		quote(f.Name), notifIn(m, rule.Notification), echoArgOn(rule, f, recv))
	s.L("\t\t\t\t}")
	s.L("\t\t\t}")
	s.L("\t\t}")
}

func transitionKeyType(f ir.Field) string { return "string" }

// emitChildPairing opens a block that walks the entries this write leaves
// behind, each next to the way it was before.
//
// It is the root's answer to a rule an ENTRY cannot decide. A collection's
// BuildRules is handed one entry and no history — domain.Old is defined over
// Entity — so the rules that compare against the previous version are emitted
// here, where the framework does expose both sides: the surviving entries from
// the aggregate root, the previous ones from the ghost the write carries.
//
// Pairing is by id when the collection is edited one entry at a time, and by
// business identity when it is replaced wholesale — a replace hands back
// entries with no id, so every one of them would read as new.
func emitChildPairing(s *src, c *ir.Child, body func(cur, old string)) {
	s.L("\t\t{")
	s.L("\t\t\tcurrent := domain.GetCurrentItemsOf[aggregatevos.%s](e.GetAggregateRoot())", c.Name)
	s.L("\t\t\tvar previous []aggregatevos.%s", c.Name)
	s.L("\t\t\tif ghost := domain.Old(e); ghost != nil {")
	s.L("\t\t\t\tprevious = domain.GetCurrentItemsOf[aggregatevos.%s](ghost.GetAggregateRoot())", c.Name)
	s.L("\t\t\t}")
	s.L("\t\t\tfor _, cur := range current {")
	s.L("\t\t\t\tfor _, was := range previous {")
	if c.PerChild {
		s.L("\t\t\t\t\tif cur.GetID().Value() != was.GetID().Value() {")
	} else {
		s.L("\t\t\t\t\tif !cur.IsSameBusinessIdentity(was) {")
	}
	s.L("\t\t\t\t\t\tcontinue")
	s.L("\t\t\t\t\t}")
	body("cur", "was")
	s.L("\t\t\t\t\tbreak")
	s.L("\t\t\t\t}")
	s.L("\t\t\t}")
	s.L("\t\t}")
}

// emitChildTransition is `transition` declared on a collection.
func emitChildTransition(s *src, m *ir.Model, rule ir.Rule) {
	c := childNamed(m, rule)
	if c == nil || len(rule.Fields) == 0 || len(rule.Transitions) == 0 {
		return
	}
	// Validation refuses a nullable state (see emitTransition), so no nil
	// handling is needed here either.
	f := rule.Fields[0]
	read := func(recv string) string {
		v := recv + "." + f.Name
		if f.VOKind != "" {
			v += ".Value()"
		}
		return v
	}
	emitChildPairing(s, c, func(cur, old string) {
		if guard := skipGuard(rule, cur); guard != "" {
			s.L("\t\t\t\t\t// %s", skipReason(rule))
			s.L("\t\t\t\t\tif %s {", guard)
			defer s.L("\t\t\t\t\t}")
		}
		s.L("\t\t\t\t\tallowed := map[string][]string{")
		for _, from := range sortedKeys(rule.Transitions) {
			var quoted []string
			for _, to := range rule.Transitions[from] {
				quoted = append(quoted, quote(to))
			}
			s.L("\t\t\t\t\t\t%s: {%s},", quote(from), strings.Join(quoted, ", "))
		}
		s.L("\t\t\t\t\t}")
		s.L("\t\t\t\t\tif %s != %s {", read(old), read(cur))
		s.L("\t\t\t\t\t\tok := false")
		s.L("\t\t\t\t\t\tfor _, to := range allowed[%s] {", read(old))
		s.L("\t\t\t\t\t\t\tif to == %s {", read(cur))
		s.L("\t\t\t\t\t\t\t\tok = true")
		s.L("\t\t\t\t\t\t\t\tbreak")
		s.L("\t\t\t\t\t\t\t}")
		s.L("\t\t\t\t\t\t}")
		s.L("\t\t\t\t\t\tif !ok {")
		s.L("\t\t\t\t\t\t\tr.AddNotification(%s, %s%s)",
			quote(rule.AttachTo), notifIn(m, rule.Notification), echoOf(rule, read(cur)))
		s.L("\t\t\t\t\t\t}")
		s.L("\t\t\t\t\t}")
	})
}

// emitChildImmutable is `immutable` declared on a collection: the entry may be
// edited, this field may not.
func emitChildImmutable(s *src, m *ir.Model, rule ir.Rule) {
	c := childNamed(m, rule)
	if c == nil || len(rule.Fields) == 0 {
		return
	}
	f := rule.Fields[0]
	emitChildPairing(s, c, func(cur, old string) {
		// Same contract as the root's emitImmutable: a nullable field compares
		// by pointed-at value. `!=` on two pointers compares identity, and the
		// ghost and the incoming entry are always distinct allocations — so the
		// plain comparison rejected every update that merely carried the value.
		cmp := fmt.Sprintf("%s.%s != %s.%s", old, f.Name, cur, f.Name)
		if f.Nullable {
			cmp = pointerNeq(fmt.Sprintf("%s.%s", old, f.Name), fmt.Sprintf("%s.%s", cur, f.Name))
		}
		s.L("\t\t\t\t\tif %s {", cmp)
		s.L("\t\t\t\t\t\tr.AddNotification(%s, %s%s)",
			quote(rule.AttachTo), notifIn(m, rule.Notification), echoOf(rule, childEcho(f, cur)))
		s.L("\t\t\t\t\t}")
	})
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// emitChildDuplicate refuses two entries of one collection that mean the same
// thing.
//
// "The same thing" is the child's own business identity, not its id: two
// entries typed twice in one request have different ids and are still the same
// guardian. The framework already exposes that comparison, so the rule asks it
// rather than re-deciding what sameness is.
func emitChildDuplicate(s *src, m *ir.Model, rule ir.Rule) {
	c := childNamed(m, rule)
	if c == nil {
		return
	}
	s.L("\t\t{")
	s.L("\t\t\titems := domain.GetCurrentItemsOf[aggregatevos.%s](%s.GetAggregateRoot())", c.Name, "e")
	s.L("\t\t\tfor i := range items {")
	s.L("\t\t\t\tfor j := i + 1; j < len(items); j++ {")
	s.L("\t\t\t\t\tif items[i].IsSameBusinessIdentity(items[j]) {")
	s.L("\t\t\t\t\t\tr.AddNotification(%s, %s%s)",
		quote(c.GoPlural), notifIn(m, rule.Notification), duplicateEcho(rule, *c))
	s.L("\t\t\t\t\t\tbreak")
	s.L("\t\t\t\t\t}")
	s.L("\t\t\t\t}")
	s.L("\t\t\t}")
	s.L("\t\t}")
}

// emitFactRange enforces a limit on what the service answered.
//
// It is the other half of a fact: the port declares the question, this writes
// the comparison. Without it every limit over rows already in the table — a cap
// per category, a total that may not be exceeded — was a hand-written clause in
// the manual hook, for an invariant whose shape never varies. The three answer
// shapes a fact can have are all handled here, because leaving one out would
// have meant a spec that validates and emits nothing.
func emitFactRange(s *src, m *ir.Model, rule ir.Rule) {
	f := rule.Fact
	if f == nil {
		return
	}
	args := factCallArgs(s, m, f)
	call := fmt.Sprintf("service.(%sService).%s(%s)", m.Entity.Pascal, f.Name, strings.Join(args, ", "))

	// The notification is built exactly as a range over a FIELD builds it: {min}
	// and {max} are filled from the same bounds the comparison uses, so the text
	// the caller reads states the limit the code enforced rather than a number
	// someone typed into a catalog and has to keep in step by hand.
	notif := notifLiteralFor(rule, m)

	switch {
	case f.Grouped():
		s.L("\t\t// One group at a time: the database already reduced the table to one")
		s.L("\t\t// row per key, so the loop compares answers rather than counting rows.")
		s.L("\t\tfor _, g := range %s {", call)
		s.L("\t\t\tif %s {", factBoundCond("g.Value", rule))
		s.L("\t\t\t\tr.AddNotification(%s, %s%s)",
			quote(rule.AttachTo), notif, echoOf(rule, "g.Value"))
		s.L("\t\t\t\tbreak")
		s.L("\t\t\t}")
		s.L("\t\t}")
	case f.ReturnsFound:
		s.L("\t\t// No matching row means there is no %s to compare — the rule stands", f.Kind)
		s.L("\t\t// down rather than treating the zero as an answer.")
		s.L("\t\tif v, ok := %s; ok && %s {", call, factBoundCond("v", rule))
		s.L("\t\t\tr.AddNotification(%s, %s%s)",
			quote(rule.AttachTo), notif, echoOf(rule, "v"))
		s.L("\t\t}")
	default:
		s.L("\t\tif v := %s; %s {", call, factBoundCond("v", rule))
		s.L("\t\t\tr.AddNotification(%s, %s%s)",
			quote(rule.AttachTo), notif, echoOf(rule, "v"))
		s.L("\t\t}")
	}
}

// childEcho reads ONE field off a collection entry, unwrapping a value object
// so what travels back is the value the caller sent rather than the type
// wrapping it. A nullable field is passed as the pointer: the framework formats
// a nil as an absent value, which is exactly what the caller sent.
func childEcho(f ir.Field, recv string) string {
	v := recv + "." + f.Name
	if f.VOKind != "" && !f.Nullable {
		v += ".Value()"
	}
	return v
}

// addedEcho names the entry the collection ALREADY held, on the per-entry add
// door. Same single-field rule as duplicateEcho, and the same reason: the
// caller sent one entry, so telling them which value collided is the whole
// actionable part of the answer.
//
// It is not gated on echoValue, because this refusal has no rules.list entry to
// carry the key — it comes from children[].duplicateNotification.
func addedEcho(c ir.Child) string {
	if len(c.Identity) != 1 {
		return ""
	}
	return ", " + childEcho(c.Identity[0], "item")
}

// duplicateEcho names WHICH entry came twice, when the collection's business
// identity is a single field.
//
// A composite identity is deliberately left silent: echoing one half of a
// two-field key points at the wrong thing, and echoing the whole entry hands
// back a formatted struct nobody asked for. The message still says which
// collection, and attachTo says which field.
func duplicateEcho(rule ir.Rule, c ir.Child) string {
	if len(c.Identity) != 1 {
		return ""
	}
	return echoOf(rule, childEcho(c.Identity[0], "items[i]"))
}

// echoOf passes a value back with the refusal when the rule allows it.
//
// echoArgOn covers the common case — the subject field of the entity being
// written. This one takes an arbitrary expression, for the refusals whose
// useful value is not a field: the number a service fact computed, or the count
// that broke a cap. That is the half of the message someone can act on: "the
// limit is 50" alone states the rule, "and you are at 51" says what to change.
func echoOf(rule ir.Rule, v string) string {
	if !rule.EchoValue {
		return ""
	}
	return ", " + v
}

// factBoundCond renders the VIOLATION, not the allowed range: the emitted code
// raises a notification, so the condition it guards is the failing one.
func factBoundCond(v string, rule ir.Rule) string {
	var conds []string
	if rule.Min != nil {
		conds = append(conds, fmt.Sprintf("%s < %s", v, number(*rule.Min, factNumberType(rule))))
	}
	if rule.Max != nil {
		conds = append(conds, fmt.Sprintf("%s > %s", v, number(*rule.Max, factNumberType(rule))))
	}
	return strings.Join(conds, " || ")
}

// factNumberType keeps the literal in the fact's own arithmetic: comparing an
// int64 count against 5.0 does not compile, and comparing a float64 average
// against 5 loses the point of declaring a fraction.
func factNumberType(rule ir.Rule) string {
	if rule.Fact != nil {
		return rule.Fact.ReturnType
	}
	return "float64"
}

// factCallArgs fills a fact's parameters from the entity being written, the
// same way the unique precheck does — a filter names a field, so the value is
// the one this write carries.
func factCallArgs(s *src, m *ir.Model, f *ir.Fact) []string {
	var args []string
	needsSelf := false
	for _, p := range f.Params {
		if p.Role == "exclude-self" {
			needsSelf = true
			args = append(args, "selfID")
			continue
		}
		args = append(args, factArgValue(fieldNamed(m, p.Field), "e"))
	}
	if needsSelf {
		s.L("\t\t// On an insert there is no row yet, so there is nothing to exclude —")
		s.L("\t\t// and the id is not minted until after the rules run.")
		s.L("\t\tvar selfID domain.ID")
		s.L("\t\tif id := e.GetID(); id != nil {")
		s.L("\t\t\tselfID = *id")
		s.L("\t\t}")
	}
	return args
}

// emitGroupCap writes "at most N of these per key".
//
// It counts what the aggregate currently holds — the entries surviving this
// write, not the rows in the table — because that is what the write is about to
// make true, and it is the only count the aggregate can answer without IO.
func emitGroupCap(s *src, m *ir.Model, rule ir.Rule) {
	c := childNamed(m, rule)
	if c == nil || rule.Cap <= 0 {
		return
	}
	s.L("\t\t{")
	s.L("\t\t\titems := domain.GetCurrentItemsOf[aggregatevos.%s](%s.GetAggregateRoot())", c.Name, "e")

	// The restriction, when there is one: an entry that does not match is not
	// counted at all. Without it the cap lands on every value of the grouping
	// field equally, so "at most 3 under review" also capped rejected and
	// withdrawn at 3 — a rule nobody declared, enforced silently.
	countOne := func(indent, body string) {
		if rule.OnlyField != nil {
			s.L("%s// Only the entries this rule is about: %s == %s.",
				indent, rule.OnlyField.Name, quote(rule.OnlyEquals))
			s.L("%sif %s {", indent, onlyCondition(rule))
			s.L("%s\t%s", indent, body)
			s.L("%s}", indent)
			return
		}
		s.L("%s%s", indent, body)
	}

	if len(rule.GroupBy) == 0 {
		// No grouping: the cap is on the collection as a whole, which is what
		// "at most N of these" means when no key is named.
		//
		// With no restriction either, the count IS the collection's length, and
		// it has to be written that way: a loop whose body is only "n++" never
		// mentions the entry it ranges over, and Go refuses "declared and not
		// used: item". That combination — no groupBy, no only — is a rule the
		// validator deliberately accepts ("at most 30 photos"), so the emitter
		// owes it code that compiles.
		if rule.OnlyField == nil {
			s.L("\t\t\tif len(items) > %d {", rule.Cap)
			s.L("\t\t\t\tr.AddNotification(%s, %s%s)",
				quote(c.GoPlural), notifInFor(m, rule), echoOf(rule, "len(items)"))
			s.L("\t\t\t}")
			s.L("\t\t}")
			return
		}
		s.L("\t\t\tn := 0")
		s.L("\t\t\tfor _, item := range items {")
		countOne("\t\t\t\t", "n++")
		s.L("\t\t\t}")
		s.L("\t\t\tif n > %d {", rule.Cap)
		s.L("\t\t\t\tr.AddNotification(%s, %s%s)",
			quote(c.GoPlural), notifInFor(m, rule), echoOf(rule, "n"))
		s.L("\t\t\t}")
		s.L("\t\t}")
		return
	}

	s.L("\t\t\tperKey := map[string]int{}")
	s.L("\t\t\tfor _, item := range items {")
	var parts []string
	for _, g := range rule.GroupBy {
		parts = append(parts, groupKeyExpr(c, g))
	}
	countOne("\t\t\t\t", fmt.Sprintf("perKey[%s]++", strings.Join(parts, " + \"|\" + ")))
	s.L("\t\t\t}")
	s.L("\t\t\tfor _, n := range perKey {")
	s.L("\t\t\t\tif n > %d {", rule.Cap)
	s.L("\t\t\t\t\tr.AddNotification(%s, %s%s)",
		quote(c.GoPlural), notifInFor(m, rule), echoOf(rule, "n"))
	s.L("\t\t\t\t\tbreak")
	s.L("\t\t\t\t}")
	s.L("\t\t\t}")
	s.L("\t\t}")
}

// onlyCondition renders the whole match test for one entry, unwrapping a value
// object so the comparison is against the stored value rather than the type.
// A nullable field gets a nil guard: an entry without the value simply does
// not match, it does not panic the rule.
func onlyCondition(rule ir.Rule) string {
	f := rule.OnlyField
	ref := "item." + f.Name
	val := ref
	if f.VOKind != "" {
		val = ref + ".Value()"
	}
	if f.Nullable {
		if f.VOKind == "" {
			val = "*" + ref
		}
		return fmt.Sprintf("%s != nil && %s == %s", ref, val, quote(rule.OnlyEquals))
	}
	return fmt.Sprintf("%s == %s", val, quote(rule.OnlyEquals))
}

// groupKeyExpr renders one grouping field as text, which is what makes a
// composite key expressible without generating a struct per rule.
func groupKeyExpr(c *ir.Child, name string) string {
	for _, f := range c.Fields {
		if f.Name != name {
			continue
		}
		// Validation refuses a nullable grouping key, so no nil handling here.
		ref := "item." + f.Name
		switch {
		case f.VOKind != "":
			return ref + ".Value()"
		case f.SpecType == "string":
			return ref
		default:
			return fmt.Sprintf("fmt.Sprint(%s)", ref)
		}
	}
	return quote("")
}

func childNamed(m *ir.Model, rule ir.Rule) *ir.Child {
	if len(rule.Collection) == 0 {
		return nil
	}
	for i := range m.Children {
		if m.Children[i].Name == rule.Collection {
			return &m.Children[i]
		}
	}
	return nil
}

// presentCheck is the inverse of zeroCheck: the field carries a value.
func presentCheck(f ir.Field, receiver string) string {
	z := zeroCheck(f, receiver)
	if z == "false" {
		return receiver + "." + f.Name // a bool is present when it is true
	}
	if strings.Contains(z, "||") {
		return "!(" + z + ")"
	}
	return "!(" + z + ")"
}

// skipGuard renders the condition under which the rule runs at all.
//
// A rule with a skip condition is an OPTIONAL field's rule: the value may be
// absent, and when it is, the checks below have nothing to say. Without the
// guard the same declaration would mean "required AND valid", which is a
// different contract and the more common mistake.
func skipGuard(rule ir.Rule, recv string) string {
	if rule.SkipWhen == "" || len(rule.Fields) == 0 {
		return ""
	}
	f := rule.Fields[0]
	if rule.SkipWhen == "null" {
		if !f.Nullable {
			return "" // a non-pointer is never nil; the guard would always pass
		}
		return fmt.Sprintf("%s.%s != nil", recv, f.Name)
	}
	return presentCheck(f, recv)
}

func skipReason(rule ir.Rule) string {
	if rule.SkipWhen == "null" {
		return "Skipped when the value is absent: this rule says what a value must " +
			"look like, not that there must be one."
	}
	return "Skipped when the value is empty: this rule says what a value must " +
		"look like, not that there must be one."
}

// notifIn spells a notification from the package that is emitting the rule.
//
// A child's rules are emitted INSIDE aggregatevos, so everything there is bare.
// The root's are emitted in domain, which imports aggregatevos and vos — so a
// notification the resolver placed in one of those has to be qualified. Getting
// this wrong does not compile, which is the good case; what it replaces is the
// generator emitting a type in a package that could never hold it.
func notifIn(m *ir.Model, name string) string {
	if m == nil || name == "" {
		return notifLiteral(name)
	}
	for _, n := range m.Notifications {
		if n.Name == name && n.Package != "" && n.Package != "domain" {
			return n.Package + "." + name + "{}"
		}
	}
	return notifLiteral(name)
}

// emitArchiveWhen writes the one condition that changes what the write IS.
//
// Everything else in BuildRules decides whether a write is ALLOWED. This decides
// that the row it is writing should not be left active, and hands that to the
// framework: CompleteAsArchive() makes the same statement carry the archive
// stamp, run the child cascade, converge the shared identity, emit ARCHIVED
// rather than UPDATED, and record an archive in the audit trail.
//
// The comment it emits is not decoration. A reader of this entity meets a plain
// update that quietly ends as an archive, and the two things they cannot guess
// are on the two lines above it: that IfArchive does not fire here, and that the
// value this closure leaves behind is therefore the one that gets persisted.
func emitArchiveWhen(s *src, m *ir.Model) {
	aw := m.ArchiveWhen
	if aw == nil {
		return
	}
	s.Blank()
	if aw.Description != "" {
		for _, line := range wrap(aw.Description, 66) {
			s.L("\t\t// %s", line)
		}
		s.L("\t\t//")
	}
	s.L("\t\t// The DOMAIN decides this update retires the row, so the framework runs the")
	s.L("\t\t// whole archive: the archive stamp, the child cascade, the ARCHIVED event the")
	s.L("\t\t// read side routes on, and an archive audit entry.")
	s.L("\t\t//")
	s.L("\t\t// IfArchive does NOT fire on this path — the rules run once, in ModeUpdate —")
	s.L("\t\t// so the value left here is the one that gets persisted.")

	ref := "e." + aw.Field.Name
	read := ref
	if aw.Field.VOKind != "" {
		read += ".Value()"
	}
	s.L("\t\tif %s == %s {", read, quote(aw.Equals))
	if aw.Becomes != "" {
		s.L("\t\t\t%s = %s", ref, entityLiteral(aw.Field, aw.Becomes))
	}
	s.L("\t\t\te.CompleteAsArchive()")
	s.L("\t\t}")
}

// entityLiteral renders a text value as the field's own type: a value object
// wraps it, a plain string is itself.
func entityLiteral(f ir.Field, value string) string {
	if f.VOKind != "" {
		return fmt.Sprintf("%s(%s)", f.BaseEntityType, quote(value))
	}
	return quote(value)
}

// emitJoinStructFields writes the fields a declared READ JOIN lands on the
// struct.
//
// They are ordinary exported fields — that is the whole design. They are simply
// absent from the TableSchema, which is what makes them read-only STRUCTURALLY:
// WriteFields walks the schema, so no INSERT or UPDATE can carry one, and the
// write repository holds a schema with no loader in sight. Nothing here has to
// be defended; the field cannot reach a write.
//
// They carry no labelKey tag for the same reason a runtime field does not: the
// tag is what the schema resolves a column through, and there is no column of
// this table behind a joined value.
func emitJoinStructFields(s *src, joins []ir.Join, owner string) {
	if len(joins) == 0 {
		return
	}
	s.Blank()
	s.L("\t// Filled by the READ JOINS the repository declares — see WithJoins in")
	s.L("\t// internal/infra. They are ordinary fields of %s, populated on EVERY", owner)
	s.L("\t// load, and readable by the rules like any other; they are absent from the")
	s.L("\t// TableSchema, so no write can carry them and no migration creates them.")
	for _, j := range joins {
		for _, f := range j.Fields {
			s.L("\t%s %s // %s", f.Name, f.GoType, joinFieldNote(j, f))
		}
	}
}

// joinFieldNote says where the value comes from and, for a left join, what a
// nil means — which is the distinction the pointer exists to preserve.
func joinFieldNote(j ir.Join, f ir.Field) string {
	note := fmt.Sprintf("%s.%s, via the %s on %s", j.Target, f.Column, j.Verb(), j.FKColumn)
	if j.Kind == "left" {
		note += "; nil = no counterpart, never the zero value"
	}
	if f.Description != "" {
		note = strings.TrimSuffix(f.Description, ".") + " — " + note
	}
	return note
}
