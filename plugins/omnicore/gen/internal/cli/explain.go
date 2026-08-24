package cli

import (
	_ "embed"
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
		"names":      explainNames,

		"keys": explainKeys,
	}
	if topic == "" {
		var names []string
		for k := range topics {
			names = append(names, k)
		}
		names = append(names, "example [flat|sharedbase]")
		sort.Strings(names)
		return fmt.Sprintf(
			"omnicore-gen explain <topic>\n\nTopics: %s\n", strings.Join(names, ", "))
	}
	// `example` takes an argument; every other topic is a bare word.
	if head, arg, _ := strings.Cut(topic, " "); head == "example" {
		return explainExample(strings.TrimSpace(arg))
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

	// Rendered from spec.Vocabularies(), never from a list of its own: the list
	// this replaces was written by hand beside the sets and fell eight of them
	// behind — including the one an author went looking for, did not find, and
	// worked around by editing generated SQL.
	rows := spec.Vocabularies()
	width := 0
	for _, r := range rows {
		if len(r.Path) > width {
			width = len(r.Path)
		}
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, r.Path, r.Set.String())
		fmt.Fprintf(&b, "  %-*s  %s\n", width, "", r.Why)
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
                  NOT for a field backed by a value object: those are validated
                  automatically on every write, and a string-backed raw VO
                  already answers an empty value with RequiredFieldNotification
                  while an enum answers it with its unknown-member one. Declared
                  on top of either, the caller reads the same complaint TWICE for
                  one empty field, and check warns about it by name.
  immutable       the value cannot change once set. Compares against the
                  pre-write snapshot, so it is never scoped to insert.
  length          min and/or max characters. Text only.
  range           min and/or max value. Numbers and dates.
  comparison      one field against another (other + operator), nil-safe.
  transition      an enum may only move along declared edges. The field is a
                  non-nullable, string-backed enum declared in THIS spec, and
                  every state named in the map is one of its member values.
  requiredIf      required when another field has a value (see skipWhen).
  groupCap        at most N entries per key — counted IN GO, over what this
                  write carries. Use it for the collection being written; for a
                  cap over rows already in the TABLE, declare a service fact
                  with groupBy and let the database group them. The cap reaches
                  the message: declare tvars: [max] (or [cap]) on the
                  notification and write {max} in the seven texts instead of
                  spelling the number into each one.
  factRange       a service fact's ANSWER must stay within min/max. It is the
                  other half of service.facts: the fact asks the database the
                  number, this says what the number may be, and the call, the
                  comparison and the notification are written for you. Over a
                  grouped fact it fires when ANY group is out of bounds — the
                  same shape as groupCap, but over rows already in the table.
  childDuplicate  two entries of a collection share a business identity.
  ownerCheck      the caller must own the row. Reads a runtime-only field fed
                  from the request identity.

Anything else goes under rules.manual as a NAMED entry with a description. That
description is what the generated report tells the implementer to write, and the
hook file <entity>_rules_manual.go is where they write it. An entry without a
description is refused — an unnamed escape hatch is an empty TODO.

NAME A FACT FOR THE PROBLEM, NOT FOR THE HEALTHY STATE. The generated test suite
stubs the service so every probe answers "nothing found" — which is what lets a
valid fixture through, so each negative case fails for the rule it is testing
rather than for a duplicate the stub invented. A fact named for the problem
(TenantIsUnavailable, WorkspaceTaken, PermissionKeyTaken) reads false under that
stub and the happy path passes. The same question named for the healthy state
(TenantIsActive) reads false too — and now means the tenant is gone, so the
generated happy-path test fails against a spec that is perfectly correct. The
convention is not a style preference; it is what makes the generated suite green
on the day it is written.

A fact may also ask about ONE ENTRY of a collection: filters: [<collection>.<field>]
— emits a method taking that entry field's type, asked once per entry. Only on
kind: manual: a computed fact is a query over this entity's own table, and the
entry's column is on another one.

NAMING A COLLECTION, once, for every key that does it. A collection has TWO
names and both are real: "name" is the entry's Go type, "plural" is the
collection's name — the document segment, the read DTO's field, the notification
wire path. Every key that addresses a collection takes EITHER of them:

    joins[].inChild · rules.list[].fields (childDuplicate, groupCap)
    read.computed.from · children[].computed · siblings[].attachTo: child:<...>
    service.facts[].filters

Messages here quote the plural. The keys used to disagree — three resolved the
singular, one the plural, and its refusal argued the opposite of the other three
— so a spec could be correct and refused for spelling one collection the way the
key beside it demanded. What IS refused now is the ambiguity that would make
"either name" impossible: one word naming the entry type of one collection and
the plural of another.

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

// explainNames lists the names the generator refuses to invent.
//
// It exists because the failure it prevents is silent: a guessed plural is a
// persisted key, so the code compiles, the service boots, and the wrongness
// shows up as a document written under a name nothing reads back.
func explainNames() string {
	return `Names you declare — the generator invents none
=============================================

There is no pluraliser and no singulariser in this build. Every name below is
read from the spec verbatim, because each one is either PERSISTED (a column, a
document key) or PUBLIC (a route, a JSON field) — it outlives the rule that
would have guessed it. An English rule lands on "Matriculas" only by luck, and
writes "Analysiss", "Persons" and "Animals" the rest of the time.

  plural                     REQUIRED. The route path (/matriculas), the
                             feature name, the listing types.

  children[].plural          REQUIRED, and the heaviest of them: it is the
                             child's CollectionName() — the segment the
                             projection nests the collection under, the read
                             DTO's Go field, the ?fields=/CSV token, and
                             lower-camelled, the notification wire path
                             (responsaveis[0].documento). Changing it later
                             changes the document shape: bump read.view.version.

  children[].parentColumn    REQUIRED. The foreign key back to the owner.
                             Renaming a column is a migration.

  storage.base.schemaFunc    REQUIRED for sharedbase-role. The exported Go
                             function that returns the shared identity schema,
                             e.g. PessoaBase.

  storage.base.linkColumn    REQUIRED when base.link is separate-fk — and
                             REFUSED when it is shared-pk, where the role's own
                             primary key IS the identity's and there is no
                             second column to name.

  storage.table,
  fields[].column, ...       Always yours. The generator only changes the CASE
                             of a name you already gave (PascalCase → snake_case
                             for a file, → lowerCamel for a JSON key).

If a name is missing, "check" says which one and what it reaches. It never
picks one for you.
`
}

func explainOwnership() string {
	return `Which files the generator owns
==============================

  owned         written in full by the generator and hashed. If you edit one, the
                next run REFUSES it and leaves your edit alone — it never
                overwrites your work. Use --force=<path> to deliberately discard
                an edit, one path at a time.

  hook          written once when missing, then never touched and never hashed —
                which is what keeps regeneration routine. Everything named
                *_manual is one, and there are four:

                  <entity>_rules_manual.go     rules.manual — an invariant the
                                               DSL cannot express
                  <entity>_service_manual.go   a fact whose kind is manual
                  <entity>_computed_manual.go  read.computed — a read field no
                                               column holds
                  NNNN_<entity>_manual.sql     the migration pair

                The three Go ones are NOT equally quiet, and the header of each
                says which it is: unwritten rules leave invariants unenforced and
                the service runs on; an unwritten fact panics the moment a rule
                asks; an unwritten derivation renders one column empty on every
                surface and reports nothing at all.

                The MIGRATION is a hook for a different reason, and it is the
                sharpest one here: its effect outlives the file. Once it has run
                anywhere, the framework's tracking table records it as applied,
                so rewriting the file would change what the file CLAIMS without
                changing a single table — a service that boots green and fails on
                the first query touching the change. So the generator creates a
                schema and never evolves one: it writes the pair once, and a
                later change is a NEW numbered pair, written by whoever knows
                where the first one has been. The report prints the shape the
                regenerated code expects, so that comparison is possible without
                re-deriving it from the spec.

  registration  wire.go, the notification files, the seven translation catalogs.
                Inserted into, and its OWN entries replaced when the spec moves
                them; never rewritten whole. Those files carry other entities'
                declarations and hand-written notes, so a WRITE never deletes
                from them: a labelKey dropped from the spec stays in all seven
                catalogs. "omnicore-gen prune" is the asked-for act that takes it
                out — it removes only entries the lock still recognises as this
                generator's own text, byte for byte, and reports the rest.

  everything else is untouched.

Hashes normalise line endings, so a Windows checkout or a format-on-save editor
does not make the whole tree look hand-written.

A spec that SHRINKS leaves the same kind of residue in owned files: the Go files
its old shape produced still compile and mean nothing. "omnicore-gen prune" lists
them (and the lock records for files already deleted by hand, which is why doctor
keeps reporting "is gone"), and removes them with -apply. A migration is never a
candidate: its effect outlived the file the moment it ran.

When an owned file has to carry a hand edit — a framework newer than this build,
or something this generator simply does not cover — run
"omnicore-gen adopt <path> -why '<what the spec could not express>'". The edit is
then recorded and survives regeneration instead of resurfacing as an unexplained
refusal.

Adopt LAST, and know the price. An adopted file stops tracking the spec: every
later improvement to the emitters lands everywhere except there, quietly, for as
long as the file exists. Before adopting, look for the key that says it
(uniqueness among active rows, a conditional requirement, per-entry endpoints —
they exist and are easy to miss), change the spec, and regenerate; and if the
invariant genuinely cannot be declared, rules.manual is the escape that does not
fight the generator.
`
}

//go:embed example.omnicore.yaml
var exampleSpec string

//go:embed example_sharedbase.omnicore.yaml
var exampleSharedBaseSpec string

// explainExample prints a whole spec that works.
//
// The other topics are reference: they answer "what may this key hold?". This
// one answers the question an author actually has first — "what does a spec
// look like?" — and it is the one that decides whether a first draft is written
// or guessed. A vocabulary alone leaves the SHAPE to be invented, and the shape
// is where a draft goes wrong: rules nested under `list`, a value object's
// members, where a collection's name lives, how a facet attaches.
//
// It is embedded rather than described, and a test asserts it still validates,
// so an example that stopped working fails the build instead of misleading
// someone at 2am.
// explainExample prints a whole spec that works.
//
// There are two, because storage.kind is ONE value and a single spec can
// therefore never show both postures: the flat one and the shared-identity one
// together cover the language. Asking for neither prints the flat one and says
// the other exists — a reader who does not know there is a second example never
// looks for it.
func explainExample(posture string) string {
	switch posture {
	case "sharedbase", "shared-base", "sharedbase-role":
		return "A complete spec that validates — the SHARED-IDENTITY posture\n" +
			"===========================================================\n\n" +
			exampleSharedBaseSpec
	default:
		return "A complete spec that validates — the FLAT posture\n" +
			"=================================================\n\n" +
			"For a role over a shared identity (and for per-child collections, the\n" +
			"full rule set, indexes and exports), run `explain example sharedbase`.\n\n" +
			exampleSpec
	}
}
