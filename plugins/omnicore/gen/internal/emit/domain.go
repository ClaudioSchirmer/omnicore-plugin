package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
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
	for _, f := range m.Fields {
		s.L("\t%s %s `labelKey:%s`%s", f.Name, f.EntityType, quote(f.LabelKey), fieldComment(f))
	}
	for _, sib := range m.SiblingsOn("") {
		s.Blank()
		s.L("\t// The %s facet. It lives in its own table sharing this row's key, but", sib.Name)
		s.L("\t// there is no separate Go type: the split is physical only. All-nil")
		s.L("\t// means the row does not exist.")
		for _, f := range sib.Fields {
			s.L("\t%s %s `labelKey:%s`%s", f.Name, f.EntityType, quote(f.LabelKey), fieldComment(f))
		}
	}
	if len(m.Children) > 0 {
		s.Blank()
		s.L("\t// No slice field for the children, deliberately: the framework keeps them")
		s.L("\t// in its own collection. A slice here would stay empty on every read and")
		s.L("\t// be ignored on every write.")
	}
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

	if len(m.Clauses) == 0 && !m.HasHookFile {
		s.L("\t// The spec declares no rule for this aggregate. The method still exists")
		s.L("\t// because the framework's entity contract requires it.")
		s.L("\t_ = actionName")
		s.L("\t_ = service")
		s.L("\t_ = r")
		s.L("}")
		s.Blank()
		return
	}

	for _, clause := range m.Clauses {
		s.L("\tr.%s(func() {", clause.Gate)
		for _, rule := range clause.Rules {
			emitRule(s, m, clause.Gate, rule)
		}
		s.L("\t})")
		s.Blank()
	}

	if m.HasHookFile {
		s.L("\t// Invariants the spec could not express declaratively. They live in")
		s.L("\t// %s_rules_manual.go, which the generator writes once and never touches again.", m.Entity.Snake)
		s.L("\te.customRules(actionName, service, r)")
	} else {
		s.L("\t_ = actionName")
		s.L("\t_ = service")
	}
	s.L("}")
	s.Blank()
}

func emitRule(s *src, m *ir.Model, gate string, rule ir.Rule) {
	emitRuleWith(s, m, gate, rule, "e")
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
		quote(left.Name), notifIn(m, rule.Notification), echoArg(rule, left))
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
	s.L("\t\t// Tolerates an empty principal: with authentication disabled in")
	s.L("\t\t// development, and inside tests that bypass the middleware, no identity")
	s.L("\t\t// is attached and the check would otherwise reject every call.")
	if len(rule.Fields) == 0 {
		return
	}
	target := rule.Fields[0]
	// The owner field is a runtime string (a token claim), so the field it is
	// compared against has to be a string too — and a value-object field is not
	// one until it is unwrapped. Comparing them directly does not compile, which
	// is the good case; what it looked like was an example that validated and
	// produced a tree that did not build.
	cond := fmt.Sprintf("%s.%s != \"\" && %s != %s.%s",
		recv, owner.Name, wireValue(target, recv), recv, owner.Name)
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

	for _, gate := range order {
		s.Blank()
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
	s.Blank()
	s.L("\t_ = actionName")
	s.L("\t_ = service")
	s.L("\t_ = r")
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
	s.L("\t\t// The database unique index is the backstop for the race between this")
	s.L("\t\t// check and the commit; asking here is what lets the duplicate be")
	s.L("\t\t// reported together with the other problems instead of alone, later.")
	s.L("\t\tif %s {", notEmpty(f, "e"))
	var args []string
	needsSelf := false
	for _, p := range rule.Fact.Params {
		if p.Role == "exclude-self" {
			needsSelf = true
			args = append(args, "selfID")
			continue
		}
		args = append(args, wireValue(fieldNamed(m, p.Field), "e"))
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
		quote(f.Name), notifIn(m, rule.Notification), echoArg(rule, f))
	s.L("\t\t\t}")
	s.L("\t\t}")
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
			s.L("\t\t\te.AddNotification(%s, %s)", quote(c.GoPlural), notifIn(m, c.DuplicateNotification))
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
}

// notifLiteralFor builds the notification value, filling the interpolation
// variables the rule can supply.
//
// Declaring a variable and never setting it is worse than not declaring it: the
// catalog keeps its {min}, the renderer substitutes nothing, and the end user
// reads "between  and ." — a message that looks written rather than broken.
func notifLiteralFor(rule ir.Rule, m *ir.Model) string {
	name := rule.Notification
	if name == "" || frameworkNotifications[name] || m == nil {
		return notifLiteral(name)
	}
	var parts []string
	for _, v := range tvarsOf(m, name) {
		switch v {
		case "min":
			if rule.Min != nil {
				parts = append(parts, fmt.Sprintf("Min: %q", trimNumber(*rule.Min)))
			}
		case "max":
			if rule.Max != nil {
				parts = append(parts, fmt.Sprintf("Max: %q", trimNumber(*rule.Max)))
			}
		}
	}
	if len(parts) == 0 {
		return name + "{}"
	}
	return name + "{" + strings.Join(parts, ", ") + "}"
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
		s.L("\t\t\t\t\t\t\tr.AddNotification(%s, %s)",
			quote(rule.AttachTo), notifIn(m, rule.Notification))
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
		s.L("\t\t\t\t\t\tr.AddNotification(%s, %s)",
			quote(rule.AttachTo), notifIn(m, rule.Notification))
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
	s.L("\t\t\t\t\t\tr.AddNotification(%s, %s)",
		quote(c.GoPlural), notifIn(m, rule.Notification))
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
			quote(rule.AttachTo), notif, factEcho(rule, "g.Value"))
		s.L("\t\t\t\tbreak")
		s.L("\t\t\t}")
		s.L("\t\t}")
	case f.ReturnsFound:
		s.L("\t\t// No matching row means there is no %s to compare — the rule stands", f.Kind)
		s.L("\t\t// down rather than treating the zero as an answer.")
		s.L("\t\tif v, ok := %s; ok && %s {", call, factBoundCond("v", rule))
		s.L("\t\t\tr.AddNotification(%s, %s%s)",
			quote(rule.AttachTo), notif, factEcho(rule, "v"))
		s.L("\t\t}")
	default:
		s.L("\t\tif v := %s; %s {", call, factBoundCond("v", rule))
		s.L("\t\t\tr.AddNotification(%s, %s%s)",
			quote(rule.AttachTo), notif, factEcho(rule, "v"))
		s.L("\t\t}")
	}
}

// factEcho passes the ANSWER back when the rule asked for it.
//
// What comes back is the number the service computed, not a field of the entity
// — that is the whole difference from an ordinary echo, and it is the useful
// half: "the limit is 50" plus "you are at 51" is a message someone can act on.
// In the grouped form the offending group's own value is echoed, and WHICH group
// it was is carried by attachTo, which is normally the key field itself.
func factEcho(rule ir.Rule, v string) string {
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
		args = append(args, wireValue(fieldNamed(m, p.Field), "e"))
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
			s.L("\t\t\t\tr.AddNotification(%s, %s)",
				quote(c.GoPlural), notifIn(m, rule.Notification))
			s.L("\t\t\t}")
			s.L("\t\t}")
			return
		}
		s.L("\t\t\tn := 0")
		s.L("\t\t\tfor _, item := range items {")
		countOne("\t\t\t\t", "n++")
		s.L("\t\t\t}")
		s.L("\t\t\tif n > %d {", rule.Cap)
		s.L("\t\t\t\tr.AddNotification(%s, %s)",
			quote(c.GoPlural), notifIn(m, rule.Notification))
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
	s.L("\t\t\t\t\tr.AddNotification(%s, %s)",
		quote(c.GoPlural), notifIn(m, rule.Notification))
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
