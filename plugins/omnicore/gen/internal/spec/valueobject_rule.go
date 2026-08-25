package spec

import "fmt"

// VOValidationKind says HOW a field's value object is validated IN PLACE, which
// is the one thing a `valueObject` rule has to know and the spec does not always
// state:
//
//   - "raw"  — the type answers for itself: `e.Field.IsValid("Field", ctx)`. A
//     composite is one of these (it writes IsValid and no Value), a hand-written
//     one is too, and so is a plain `id`: domain.ID writes IsValid, which is
//     exactly how the automatic pass discovers and validates it.
//   - "enum" — the type declares members and the answer for a value outside
//     them, and the framework checks membership: `domain.ValidateEnum(...)`.
//   - ""     — there is nothing here to validate, or the type is one this
//     generator cannot classify. Both are refused, with different words.
//
// The `reuse` case is why this takes an inventory: the type lives in the
// project and this spec never described it, so the only authority on which kind
// it is, is the package it was read from.
func VOValidationKind(s *Spec, f Field, existing map[string]string) string {
	if f.VO == nil || f.VO.Kind == "" || f.VO.Kind == "none" {
		if f.Type == "id" {
			return "raw"
		}
		return ""
	}
	switch f.VO.Kind {
	case "enum":
		return "enum"
	case "raw", "composite", "manual":
		return "raw"
	case "reuse":
		// A spec may still declare the type it reuses (a second role over the
		// same shared base does), and what it says beats the inventory: the file
		// on disk may be the one this run is about to rewrite.
		if vo := findVO(s.ValueObjects, f.VO.Ref); vo != nil {
			return VOValidationKind(s, Field{VO: &FieldVO{Kind: vo.Kind, Ref: vo.Name}}, existing)
		}
		return existing[f.VO.Ref]
	}
	return ""
}

// validateValueObjectRule holds the one rule kind that validates nothing of its
// own to what it can actually reach.
//
// It exists because the framework's automatic value-object pass runs AFTER
// BuildRules, so a value object can never be the PREMISE of the rules below it
// — and a scope field, a foreign key or a state that the next rule dereferences
// usually is. The rule pulls that validation forward. What it must not do is
// invent a second answer for the same field, which is why everything that
// carries an answer is refused here rather than quietly ignored.
func validateValueObjectRule(s *Spec, r Rule, scopeFields []Field, w string, ps *Problems, opt Options) {
	for _, name := range []struct{ key, val string }{
		{"notification", r.Notification},
		{"attachTo", r.AttachTo},
		{"skipWhen", r.SkipWhen},
		{"operator", r.Operator},
		{"other", r.Other},
	} {
		if name.val == "" {
			continue
		}
		ps.BlockerFix(w+"."+name.key,
			fmt.Sprintf("a valueObject rule raises nothing of its own, so %s has nothing to say",
				name.key),
			"the value object owns its answer — the notification it raises, the field it "+
				"lands on, and whether an absent value is a violation are all declared with "+
				"the value object, not here")
	}
	if r.EchoValue != nil {
		ps.BlockerFix(w+".echoValue",
			"a valueObject rule raises nothing of its own, so there is no message to echo into",
			"the value object decides what its own notification carries")
	}
	if r.Only != nil {
		ps.BlockerFix(w+".only",
			"a valueObject rule counts nothing, so there is no set to restrict",
			"only: narrows what a collection rule counts; this kind validates the fields "+
				"it names, and which of them it names is the whole restriction")
	}
	if r.Min != nil || r.Max != nil {
		ps.BlockerFix(w,
			"a valueObject rule checks nothing of its own, so a bound has nothing to bind",
			"declare the bound as its own rule (kind: range or length) — it is a second "+
				"invariant, and it should read like one")
	}

	for _, fn := range r.Fields {
		f, ok := fieldNamed(scopeFields, fn)
		if !ok {
			continue // validateRuleFields already said this names no field
		}
		switch VOValidationKind(s, f, opt.ExistingVOKinds) {
		case "raw", "enum":
			// Reachable: the emitter knows which call to write.
		case "":
			if f.VO != nil && f.VO.Kind == "reuse" {
				ps.BlockerFix(w+".fields",
					fmt.Sprintf("%s reuses %s, and this build cannot tell whether that type "+
						"validates itself (IsValid) or by membership (an enum) — the two are "+
						"validated with different calls", fn, orUnnamed(f.VO.Ref)),
					"the type is read from internal/domain/vos: give it the shape the "+
						"framework expects (IsValid for a raw or composite one, Values + "+
						"UnknownNotification for an enum), or drop this rule and let the "+
						"automatic pass validate it at the end, where the kind does not matter")
				continue
			}
			ps.BlockerFix(w+".fields",
				fmt.Sprintf("%s is backed by no value object, so there is no validation to "+
					"pull forward", fn),
				"this kind only moves WHEN a value object is checked, it never adds a "+
					"check — state the invariant with a kind that does (required, length, "+
					"range), or give the field a value object")
		}
	}
}

// validateValueObjectHoists refuses the one composition that turns this rule
// into the duplicate it exists to prevent.
//
// `IgnoreValueObject` is a set of names, so naming a field twice is harmless.
// The validation call is not: it EMITS, and two calls against one field in one
// pass hand the caller the same complaint twice — which is the exact failure
// the language already warns about for `required` over a value object.
//
// Two rules collide when their verbs do: `insert` and `insertOrUpdate` are
// different scopes that both run on an insert, and reading the yaml is not
// enough to see it. So the check is over the MODES each rule expands to, not
// over the words the author wrote.
func validateValueObjectHoists(rs Rules, where string, ps *Problems) {
	type claim struct {
		rule string
		at   string
	}
	byFieldMode := map[string]claim{}
	for i, r := range rs.List {
		if r.Kind != "valueObject" {
			continue
		}
		w := fmt.Sprintf("%s.list[%d] (%s)", where, i, orUnnamed(r.ID))
		seenHere := map[string]bool{}
		for _, fn := range r.Fields {
			if seenHere[fn] {
				ps.BlockerFix(w+".fields",
					fmt.Sprintf("%s is named twice by the same rule", fn),
					"name it once — validating it twice reports the same value twice")
				continue
			}
			seenHere[fn] = true
			for _, mode := range modesOfScopes(r.Scope) {
				key := fn + "/" + mode
				if prev, dup := byFieldMode[key]; dup {
					ps.BlockerFix(w+".fields",
						fmt.Sprintf("%s is already validated in place by %s, and both rules "+
							"run on %s — the caller would be told twice about one value",
							fn, prev.rule, mode),
						fmt.Sprintf("keep ONE of the two (%s declares it at %s): the barrier "+
							"is positional, so if what you wanted was a second stop, move the "+
							"guard key to the rule that should carry it", prev.rule, prev.at))
					continue
				}
				byFieldMode[key] = claim{rule: orUnnamed(r.ID), at: w}
			}
		}
	}
}

// modesOfScopes expands the scopes a rule declares into the verbs it actually
// runs on. `insertOrUpdate` is the only one that is not already a verb, and it
// is precisely the one that makes an overlap invisible in the yaml.
func modesOfScopes(scopes []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(m string) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	for _, sc := range scopes {
		if sc == "insertOrUpdate" {
			add("insert")
			add("update")
			continue
		}
		add(sc)
	}
	return out
}
