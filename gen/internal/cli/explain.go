package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// Explain documents the spec language offline.
//
// It is generated FROM the vocabularies rather than written beside them, so a
// closed set can never drift from its own documentation: adding a value to a
// set adds it here in the same edit.
func Explain(topic string) string {
	topics := map[string]func() string{
		"vocabulary": explainVocabulary,
		"rules":      explainRules,
		"coverage":   explainCoverage,
		"ownership":  explainOwnership,
	}
	if topic == "" {
		var names []string
		for k := range topics {
			names = append(names, k)
		}
		sort.Strings(names)
		return fmt.Sprintf(
			"omnicore-gen explain <topic>\n\nTopics: %s\n", strings.Join(names, ", "))
	}
	fn, ok := topics[topic]
	if !ok {
		var names []string
		for k := range topics {
			names = append(names, k)
		}
		sort.Strings(names)
		return fmt.Sprintf("unknown topic %q — try one of: %s\n", topic, strings.Join(names, ", "))
	}
	return fn()
}

func explainVocabulary() string {
	var b strings.Builder
	b.WriteString("Closed vocabularies of the spec language\n")
	b.WriteString("=======================================\n\n")
	b.WriteString("Every key below accepts ONLY the listed values. A value outside the set is\n")
	b.WriteString("refused with the whole set printed — never accepted and ignored.\n\n")

	rows := []struct {
		key string
		set spec.StringSet
	}{
		{"storage.kind", spec.StorageKinds},
		{"storage.base.link", spec.LinkModels},
		{"storage.base.rowUniqueness", spec.RowUniqueness},
		{"storage.base.orphanPolicy", spec.OrphanPolicies},
		{"fields[].type", spec.FieldTypes},
		{"fields[].vo.kind", spec.VOKinds},
		{"fields[].unique.enforce", spec.UniqueEnforcements},
		{"valueObjects[].backing", spec.VOBackings},
		{"children[].ownedBy", spec.ChildOwners},
		{"children[].editStrategy", spec.EditStrategies},
		{"modes", spec.Modes},
		{"update.shape", spec.UpdateShapes},
		{"delete.root", spec.DeleteRoot},
		{"rules.list[].kind", spec.RuleKinds},
		{"rules.list[].scope", spec.RuleScopes},
		{"notifications[].semantic", spec.Semantics},
		{"service.facts[].kind", spec.FactKinds},
		{"read.backing", spec.ReadBackings},
		{"read.identityView", spec.IdentityViews},
		{"read.byParams.filters[].ops", spec.FilterOps},
		{"authz.dataAccess", spec.DataAccess},
		{"authz.permissions keys", spec.AuthzOperations},
	}
	width := 0
	for _, r := range rows {
		if len(r.key) > width {
			width = len(r.key)
		}
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, r.key, r.set.String())
	}
	b.WriteString("\nNote on 409: `conflict` is a duplicate (already exists); `state-conflict` is a\n")
	b.WriteString("wrong state (\"cannot ship a cancelled order\"). Both map to 409 and they are\n")
	b.WriteString("not interchangeable.\n")
	return b.String()
}

func explainRules() string {
	return `The rule DSL
============

Declare invariants; the generator writes BuildRules. Every kind below compiles to
a clause the framework dispatches by verb.

  required        the field must carry a value. Emptiness is per type:
                  "" for text, the zero instant for a date, 0 for a number.
  immutable       the value cannot change once set. Compares against the
                  pre-write snapshot, so it is never scoped to insert.
  length          min and/or max characters. Text only.
  range           min and/or max value. Numbers and dates.
  comparison      one field against another (other + operator), nil-safe.
  transition      an enum may only move along declared edges.
  requiredIf      required when another field has a value (see skipWhen).
  groupCap        at most N rows per key. Needs a service fact to count them.
  childDuplicate  two entries of a collection share a business identity.
  ownerCheck      the caller must own the row. Reads a runtime-only field fed
                  from the request identity.

Anything else goes under rules.manual as a NAMED entry with a description. That
description is what the generated report tells the implementer to write, and the
hook file <entity>_rules_manual.go is where they write it. An entry without a
description is refused — an unnamed escape hatch is an empty TODO.

Two things the DSL deliberately does NOT cover, because the framework already
does them:
  • format, length and closed-set checks on a value-object field — declare the
    value object and the framework validates it on every write, automatically;
  • presence of a value object — same mechanism.
`
}

func explainCoverage() string {
	var b strings.Builder
	b.WriteString("What this build generates\n")
	b.WriteString("=========================\n\n")
	b.WriteString("The spec LANGUAGE is complete; the emitters arrive in phases. Anything not\n")
	b.WriteString("covered is REFUSED with the phase that brings it — never accepted and\n")
	b.WriteString("silently dropped.\n\n")
	var done, pending []string
	for _, c := range spec.AllCapabilities() {
		if spec.Implemented(c) {
			done = append(done, string(c))
		} else {
			pending = append(pending, string(c))
		}
	}
	sort.Strings(done)
	sort.Strings(pending)
	b.WriteString("Generated now:\n")
	for _, d := range done {
		fmt.Fprintf(&b, "  ✓ %s\n", d)
	}
	b.WriteString("\nRefused for now:\n")
	for _, p := range pending {
		fmt.Fprintf(&b, "  · %s\n", p)
	}
	return b.String()
}

func explainOwnership() string {
	return `Which files the generator owns
==============================

  owned         written in full by the generator and hashed. If you edit one, the
                next run REFUSES it and leaves your edit alone — it never
                overwrites your work. Use --force=<path> to deliberately discard
                an edit, one path at a time.

  hook          the two "manual" escapes, named after the yaml keys that ask for
                them: <entity>_rules_manual.go (rules.manual) and
                <entity>_service_manual.go (a fact whose kind is manual).
                Written once when missing, then never touched and never hashed —
                which is what keeps regeneration routine.

                They are NOT equally quiet, and the header of each says which it
                is: unwritten rules leave invariants unenforced and the service
                runs on; an unwritten fact panics the moment a rule asks.

  registration  wire.go, the notification files, the seven translation catalogs.
                Inserted into and removed from, never rewritten.

  everything else is untouched.

Hashes normalise line endings, so a Windows checkout or a format-on-save editor
does not make the whole tree look hand-written.

If a framework newer than this build forces a small fix in an owned file, run
"omnicore-gen adopt <path>": the fix is recorded against that framework version
and survives regeneration instead of resurfacing as an unexplained refusal.
`
}
