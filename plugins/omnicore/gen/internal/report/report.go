// Package report writes gen-report.md, the handoff between the generator and
// whoever finishes the work.
//
// The section order is the whole design: what still needs implementing comes
// FIRST, then what to check, and only then what was generated. A report that
// opens with a file list reads as a completion notice and gets skimmed — and
// the one thing that must not be skimmed is the list of rules nobody has
// written yet.
package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/emit"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
)

// createdThisRun reports whether a hook file was written by THIS run, as opposed
// to having been on disk already. A hook is never rewritten, so the two states
// mean opposite things to the reader: one is work to start, the other is work to
// verify.
func createdThisRun(decisions []fsplan.Decision, path string) bool {
	for _, d := range decisions {
		if d.File.Path == path {
			return d.Action == fsplan.Create
		}
	}
	return false
}

// Input is everything the report describes.
type Input struct {
	Model               *ir.Model
	SpecPath            string
	Decisions           []fsplan.Decision
	MissingTranslations []string
	// UntranslatedEnumValues names the enum members the spec declared no text
	// for, per catalog.
	UntranslatedEnumValues []string
	// StaleRegistrations names what a shared registration file declares with
	// content that no longer matches this spec.
	StaleRegistrations []string
	Orphans            []string
	// ExistingVOs are the value objects the project ALREADY declares, as
	// discovery found them BEFORE this run wrote anything. The report needs it
	// for one question: whether a `kind: manual` value object is still owed or
	// was written on an earlier run — the difference between "the package does
	// not compile" and "check that it still matches the spec".
	ExistingVOs []string
	// UnimplementedFacts names the manual facts the EXISTING service hook does
	// not answer — the shape a spec that gained a fact after its first
	// generation lands in. The hook is written once, so nothing else in this
	// run mentions the gap, and the package does not compile until it is
	// closed.
	UnimplementedFacts []string
	// UnimplementedRedactors names the hand-written redactors the EXISTING hook
	// file does not declare — the same shape as UnimplementedFacts, and the same
	// consequence: the schema calls a function nothing defines, so the package
	// does not compile.
	UnimplementedRedactors []string
	MigrationsKept         []string
	TargetTables           []emit.TargetTable
	CompatLevel            string
	CompatMessage          string
	FrameworkPinned        string
	Warnings               []string
}

// Render produces the markdown.
func Render(in Input) string {
	var b strings.Builder
	m := in.Model

	fmt.Fprintf(&b, "# %s — generation report\n\n", m.Entity.Pascal)
	fmt.Fprintf(&b, "Generated from `%s`.\n\n", in.SpecPath)
	if m.Language != "" {
		// Which language the human-facing text is in is not decoration: every
		// description, example and label below was written in it, and a reviewer
		// checking a Portuguese domain against English wording is checking the
		// wrong thing.
		fmt.Fprintf(&b, "The descriptions, examples and labels quoted here are in **%s**, "+
			"as the spec declares.\n\n", m.Language)
	}

	renderTodo(&b, in)
	renderCheck(&b, in)
	renderGenerated(&b, in)
	renderNotGenerated(&b, in)
	renderCompat(&b, in)

	return b.String()
}

// renderTodo is section A and opens the file.
func renderTodo(b *strings.Builder, in Input) {
	m := in.Model
	b.WriteString("## What still needs implementing\n\n")

	empty := true

	// The root's hook, then one per collection that declared its own. A manual
	// rule under children[] was once parsed and dropped without a hook OR a line
	// here, so the only trace of it was the spec that asked for it.
	hooks := []struct {
		path  string
		scope string
		rules []ir.ManualRule
	}{{
		path:  fmt.Sprintf("internal/domain/%s_rules_manual.go", m.Entity.Snake),
		scope: "",
		rules: m.ManualRules,
	}}
	for _, c := range m.Children {
		if len(c.ManualRules) == 0 {
			continue
		}
		hooks = append(hooks, struct {
			path  string
			scope string
			rules []ir.ManualRule
		}{
			path: fmt.Sprintf("internal/domain/aggregatevos/%s_rules_manual.go", naming.Snake(c.Name)),
			scope: fmt.Sprintf(" They run scoped to ONE `%s`, so a notification they raise "+
				"addresses that entry's position in the collection.", c.Name),
			rules: c.ManualRules,
		})
	}
	// The value objects the spec declared and the generator deliberately did not
	// write — split by whether the author has written them YET.
	//
	// Saying the same sentence either way is how a report claims work is
	// outstanding that somebody finished three runs ago, which is the standard
	// the hook-file block above already holds itself to. The two states are not
	// a nuance here: one stops the package compiling and one is a file to
	// re-read against a spec that may have moved since.
	var owedVOs, writtenVOs []ir.ValueObject
	for _, vo := range m.ValueObjects {
		if !vo.HandWritten() {
			continue
		}
		if names(in.ExistingVOs).has(vo.Name) {
			writtenVOs = append(writtenVOs, vo)
			continue
		}
		owedVOs = append(owedVOs, vo)
	}
	// The owed ones go FIRST of everything: this is the only outstanding item
	// that stops the package COMPILING, and it carries the exact shape, because
	// a hand-written type with a different underlying type fails at a call site
	// that names neither this report nor the spec.
	if len(owedVOs) > 0 {
		empty = false
		b.WriteString("### Value objects you write\n\n")
		b.WriteString("Declared as `kind: manual` (a scalar whose rule is beyond this " +
			"language) or as a composite with `written: manual` (its shape is declared, its " +
			"file is yours), so the generator wrote NO file for them — the emitted code " +
			"already declares fields of these types and converts to and from them, so the " +
			"package does not compile until each one exists:\n\n")
		for _, vo := range owedVOs {
			fmt.Fprintf(b, "- **`%s`** — `internal/domain/vos/%s.go`. %s\n",
				vo.Name, naming.Snake(vo.Name), vo.Description)
			if vo.IsComposite() {
				writeOwedComposite(b, vo)
				continue
			}
			fmt.Fprintf(b, "  ```go\n"+
				"  type %[1]s %[2]s\n"+
				"  func (v %[1]s) Value() %[2]s { return %[2]s(v) }\n"+
				"  func (v %[1]s) IsValid(fieldName string, ctx *domain.NotificationContext) bool\n"+
				"  ```\n"+
				"  The underlying type is `%[2]s` and is not negotiable: the mappers convert "+
				"with `vos.%[1]s(x)` and read back with `.Value()`. `IsValid` is the "+
				"framework's entry point — it is found by TYPE, with no registration, and "+
				"reports every problem it finds through the context rather than returning "+
				"one, so a caller sees all of them at once.\n", vo.Name, vo.GoBacking)
		}
		b.WriteString("\n")
	}
	// Already on disk. The generator did not open them — it never reads inside a
	// file it does not own — so it cannot say whether the rule still matches
	// what the spec describes, and it asks rather than announcing completion.
	// The spec is the thing that moves: a description rewritten to mean
	// something stricter changes nothing in a type nobody re-read.
	if len(writtenVOs) > 0 {
		empty = false
		b.WriteString("### Value objects you already wrote\n\n")
		b.WriteString("Written by hand — `kind: manual`, or a composite with `written: " +
			"manual` — and already in the project. The generator did not open them and " +
			"cannot tell whether what they enforce still matches what the spec says they " +
			"enforce — listed so a description that moved does not leave a stale rule " +
			"behind it:\n\n")
		var writtenComposite bool
		for _, vo := range writtenVOs {
			fmt.Fprintf(b, "- **`%s`** — `internal/domain/vos/%s.go`. %s\n",
				vo.Name, naming.Snake(vo.Name), vo.Description)
			writtenComposite = writtenComposite || vo.IsComposite()
		}
		b.WriteString("\nThe backing stays a contract across every run: the mappers convert " +
			"with `vos.<Name>(x)` and read back with `.Value()`, so changing the underlying " +
			"type of one of these breaks call sites that name neither this report nor the " +
			"spec.")
		// A composite is converted by field, not by backing, so the sentence above
		// names the wrong contract for it — and the wrong contract read as the
		// right one is how a part gets renamed in a file the generator never opens.
		if writtenComposite {
			b.WriteString(" For a composite the contract is its FIELD SET instead: the " +
				"mappers build it as a `vos.<Name>{Part: v, …}` literal, so a part renamed " +
				"or retyped there breaks the same way, and the spec's `parts` are what says " +
				"which names those are.")
		}
		b.WriteString("\n\n")
	}

	// A DERIVED field before the hooks: it is the one thing on this list that
	// the generator changed the API for on the author's word. The field left
	// every write DTO because the spec said the server owns it — and if nothing
	// assigns it, the column holds its zero value on every insert and no error
	// anywhere says so. That is the quietest failure this build can produce.
	if derived := m.DerivedFields(); len(derived) > 0 {
		empty = false
		b.WriteString("### Fields declared DERIVED, which nothing here computes\n\n")
		for _, f := range derived {
			fmt.Fprintf(b, "- **`%s`** — `assignedFrom: derived` took it out of every write "+
				"request, command and OpenAPI request schema, so a client cannot set it. "+
				"WRITING it is yours: ", f.Name)
			// A NULLABLE derived field is a different piece of work, and saying
			// "scoped to insert" over it sends the reviewer to look for code
			// that should not exist. Null is the value the row starts with, on
			// purpose — a verification timestamp is written by whatever verifies
			// — so what has to be checked is that SOMETHING eventually writes
			// it, not that the insert does.
			if f.Nullable {
				b.WriteString("the column starts NULL and stays null until something " +
					"writes it — a `rules.manual` entry on the verb that produces the " +
					"value, or hand-written code beyond this spec. Null is a state the " +
					"response shows honestly (the field is simply absent), so check that " +
					"the write exists rather than that the insert fills it.\n")
				continue
			}
			b.WriteString("a `rules.manual` entry scoped to insert, assigning it " +
				"from the fields it derives from. Idempotent by construction when it is a " +
				"pure function of an immutable field, which is the case this exists for.\n")
		}
		b.WriteString("\n")
	}

	for _, h := range hooks {
		if len(h.rules) == 0 {
			continue
		}
		empty = false
		fmt.Fprintf(b, "### `%s`\n\n", h.path)
		// Created THIS run or already on disk? The generator knows, and saying
		// the same sentence either way is how a report claims work is
		// outstanding that somebody finished three runs ago. It still cannot say
		// whether the bodies are written — it never reads inside a hook — so the
		// second wording asks for a check rather than announcing completion.
		if createdThisRun(in.Decisions, h.path) {
			b.WriteString("The spec declared these invariants as ones it could not express. " +
				"The file was just created, with a stub for each; the code is yours to write, " +
				"and regeneration will never touch it." + h.scope + "\n\n")
		} else {
			b.WriteString("This file already exists and is YOURS — the generator did not open " +
				"it and cannot tell whether these are implemented. It lists them so you can " +
				"check the file still covers what the spec declares, which is where a rule " +
				"added to the spec later goes unnoticed." + h.scope + "\n\n")
		}
		for _, r := range h.rules {
			fmt.Fprintf(b, "**`%s`**\n\n", r.ID)
			fmt.Fprintf(b, "> %s\n\n", strings.ReplaceAll(r.Description, "\n", " "))
			var facts []string
			if len(r.Gates) > 0 {
				facts = append(facts, "fires under `"+strings.Join(r.Gates, "`, `")+"`")
			}
			if r.Notification != "" {
				facts = append(facts, "raise `"+r.Notification+"{}`")
			}
			if r.AttachTo != "" {
				facts = append(facts, "attach it to `"+r.AttachTo+"`")
			}
			if len(facts) > 0 {
				fmt.Fprintf(b, "- %s\n\n", strings.Join(facts, " · "))
			}
		}
		if createdThisRun(in.Decisions, h.path) {
			fmt.Fprintf(b, "Its tests are yours too — the generator does not know what these rules mean.\n\n")
		} else {
			fmt.Fprintf(b, "The tests for them are yours too, and the same check applies.\n\n")
		}
	}

	if manual := manualFacts(m); len(manual) > 0 {
		empty = false
		fmt.Fprintf(b, "### `internal/infra/%s_service_manual.go`\n\n", m.Entity.Snake)
		b.WriteString("The spec marked these questions as ones the generator cannot answer, " +
			"so it declared them on the port and left the bodies to you. **They panic " +
			"until you write them** — the project still builds and boots, and the failure " +
			"arrives the moment the rule asks, as a 500 with the write rolled back. The " +
			"outcome being avoided is the other one: a query against the wrong source " +
			"would compile, return, and mean nothing.\n\n")
		owed := map[string]bool{}
		for _, name := range in.UnimplementedFacts {
			owed[name] = true
		}
		for _, f := range manual {
			fmt.Fprintf(b, "**`%s(%s) %s`**\n\n", f.Name, factSignature(f), f.ReturnType)
			fmt.Fprintf(b, "> %s\n\n", strings.ReplaceAll(f.Description, "\n", " "))
		}
		b.WriteString("The method returns a plain value and no error, so decide what an " +
			"unavailable source means. Failing loudly is the safe default — returning a " +
			"plausible answer skips the rule this exists to enforce.\n\n")
		// A fact added AFTER the hook was first written is the one case where
		// the section above is not enough: the file already exists, so this run
		// wrote no stub for it, and the port now declares a method with nothing
		// behind it. Nothing else in the run says so — the file is listed under
		// "left untouched (yours, by design)", which is true and reads like
		// reassurance. The package does not compile until these are added.
		if len(in.UnimplementedFacts) > 0 {
			b.WriteString("⚠ **The file already existed, so no stub was written for the " +
				"fact(s) below.** They are declared on the port and implemented nowhere, " +
				"which is a compile error, not a panic-when-asked:\n\n")
			for _, f := range manual {
				if !owed[f.Name] {
					continue
				}
				fmt.Fprintf(b, "- `func (s *%s) %s(%s) %s`\n",
					m.Service.Impl, f.Name, factSignature(f), f.ReturnType)
			}
			b.WriteString("\nAdd them to that file — it is yours, and the generator will " +
				"not write into it again.\n\n")
		}
	}

	if hooks := ir.RedactionHooks(m); len(hooks) > 0 {
		empty = false
		path := fmt.Sprintf("internal/infra/schemas/%s_redactors_manual.go", m.Entity.Snake)
		fmt.Fprintf(b, "### `%s`\n\n", path)
		b.WriteString("These fields declare `redact` with `kind: hook` — the mask the closed " +
			"family does not reach. The generator wrote the CALL, inside the schema, and left " +
			"the body to you. **They panic until you write them**, and the write that " +
			"carried the field is rolled back.\n\n")
		b.WriteString("That is the deliberate direction. A stub returning a placeholder would " +
			"fail safe and still be the wrong answer, and this is the one place a wrong " +
			"answer is expensive: the framework CANNOT see that a hook's body changed — a " +
			"closure has no portable identity, so the view's rebuild hash mixes in only the " +
			"KIND. Documents projected through a placeholder are not repaired by fixing the " +
			"function; they are repaired by bumping `read.view.version` and rebuilding.\n\n")
		owed := map[string]bool{}
		for _, name := range in.UnimplementedRedactors {
			owed[name] = true
		}
		for _, h := range hooks {
			fmt.Fprintf(b, "**`func %s(v string) string`** — masks `%s` in %s\n\n",
				h.HookFunc, h.Owner, redactAxisReach(h.Axis))
		}
		b.WriteString("Each one is handed the REAL value and returns what the copy carries. " +
			"It must be PURE: it runs inside the write transaction AND in the composer, " +
			"which is the rebuild path, possibly months later in another binary — a clock, " +
			"randomness or mutable config there makes a rebuilt document disagree with the " +
			"one the sync wrote, and nothing reports it. Returning `\"\"` for a non-empty " +
			"value panics by design: an empty scalar reads as a REMOVED facet row in the " +
			"payload contract.\n\n")
		if len(in.UnimplementedRedactors) > 0 {
			b.WriteString("⚠ **The file already existed, so no stub was written for the " +
				"redactor(s) below.** The schema calls them and nothing declares them, which " +
				"is a compile error rather than a panic-when-asked:\n\n")
			for _, h := range hooks {
				if owed[h.HookFunc] {
					fmt.Fprintf(b, "- `func %s(v string) string`\n", h.HookFunc)
				}
			}
			b.WriteString("\nAdd them to that file — it is yours, and the generator will " +
				"not write into it again.\n\n")
		}
	}

	if emit.HasDerivations(m) {
		empty = false
		path := fmt.Sprintf("internal/application/queries/%s_computed_manual.go", m.Entity.Snake)
		fmt.Fprintf(b, "### `%s`\n\n", path)
		// The failure mode here is the opposite of the manual facts' above, and
		// that is the whole reason this section is worded separately: a fact
		// PANICS until it is written, a derivation is silent. Nothing reports an
		// empty one — the read succeeds, the row is right, and one column is
		// blank on every surface at once.
		if createdThisRun(in.Decisions, path) {
			b.WriteString("The spec declared these read fields as DERIVED — no column holds " +
				"them, so the framework fetches their sources and hands them to you. The file " +
				"was just created, with one stub per FIELD taking the sources it declared; the " +
				"bodies are yours, and regeneration will never touch them.\n\n")
		} else {
			b.WriteString("This file already exists and is YOURS — the generator did not open " +
				"it and cannot tell whether these are filled. It lists them so you can check " +
				"the file still covers what the spec declares, which is where a field added " +
				"to the spec later goes unnoticed.\n\n")
			// The shape changed once, and a file written before that change no
			// longer satisfies the call sites. The compiler says so — loudly, at
			// the exact line — but it names a symbol and not a decision, so the
			// report says what the decision was and what the fix looks like.
			b.WriteString("**If this file predates the one-function-per-field shape, the build " +
				"will not find these.** The derivations used to be one function per READ SHAPE, " +
				"each handed a whole Result, which meant writing the same derivation twice and " +
				"keeping the two in step by hand. Each is now one exported function taking the " +
				"sources it declared — the generator unwraps whatever the shape holds and calls " +
				"it, and the WRITE responses call the same one. Move each body into the " +
				"signature below and delete the old per-shape functions.\n\n")
		}
		for _, c := range m.Read.Computed {
			fmt.Fprintf(b, "**`%s` (%s)** ← `%s`\n\n", c.Name, c.GoType, strings.Join(c.Sources, "`, `"))
			if c.Description != "" {
				fmt.Fprintf(b, "> %s\n\n", strings.ReplaceAll(c.Description, "\n", " "))
			}
			fmt.Fprintf(b, "```go\n%s\n```\n\n", emit.ComputedSignature(m, c))
		}
		// The per-entry derivations, said to be per-entry. A reviewer reading a
		// signature that takes one id has no way to tell from it alone whether
		// the body runs once per record or once per row of a collection — and
		// that is the difference between a lookup and N lookups per response.
		for _, ch := range m.Children {
			for _, c := range ch.Computed {
				fmt.Fprintf(b, "**`%s.%s` (%s)** ← `%s` — ONCE PER ENTRY of `%s`\n\n",
					ch.Plural, c.Name, c.GoType, strings.Join(c.Sources, "`, `"), ch.Plural)
				if c.Description != "" {
					fmt.Fprintf(b, "> %s\n\n", strings.ReplaceAll(c.Description, "\n", " "))
				}
				fmt.Fprintf(b, "```go\n%s\n```\n\n", emit.ChildComputedSignature(m, ch, c))
			}
		}
		b.WriteString("**Until a body is written the field renders absent, and nothing says so** " +
			"— unlike a manual fact, which panics. The read answers 200, the other columns are " +
			"correct, and this one is empty on REST, on GraphQL and in the export at once. What " +
			"the declaration already bought needs no code: `?fields=` on the field fetches its " +
			"sources instead, `?orderBy=` on it is a typed 400, and the export keeps the column " +
			"under its label.\n\n")
	}

	// A field the generator declares and nobody it wrote fills. It belongs in the
	// OWED section and not in "what to check", because there is no generated code
	// to check: the obligation is entirely the author's, and nothing anywhere
	// reports it going unmet. An unfilled field reads as the zero value, and every
	// rule over it judges that — quietly, and as though it had been told something.
	if manual := m.ManualRuntimeFields(); len(manual) > 0 {
		empty = false
		b.WriteString("### Fields nothing generated fills\n\n")
		b.WriteString("Declared `runtime: true` with `source: manual`. Each one is on the " +
			"aggregate so your rules can read it, and on nothing else: no write request " +
			"DTO, no command, no mapper, no OpenAPI schema — and no column, so no " +
			"migration, `TableSchema`, outbox payload, audit event or response either. " +
			"**No generated code puts a value there.** That is the declaration, not an " +
			"omission.\n\n")
		b.WriteString("| field | type | what it is for |\n")
		b.WriteString("|---|---|---|\n")
		for _, f := range manual {
			what := f.Description
			if what == "" {
				what = "—"
			}
			fmt.Fprintf(b, "| `%s` | `%s` | %s |\n", f.Name, f.EntityType,
				strings.ReplaceAll(what, "\n", " "))
		}
		b.WriteString("\nWrite the assignment in the operation that owns it — the " +
			"hand-written command whose mapper has both the request and the entity. The " +
			"shape this exists for is an operation that dispatches the same mode a " +
			"generated verb does and is told apart by its action name: it needs the value " +
			"on the aggregate, and the ordinary write bodies must not grow a field for it.\n\n")
		b.WriteString("**Until something assigns it, the field is the zero value on every " +
			"write, and nothing says so.** A rule reading it does not fail — it judges `\"\"` " +
			"(or `false`, or `0`) and answers accordingly, which for a possession check is " +
			"the answer that looks like a pass. Value objects are the one part already " +
			"handled: the automatic pass would judge this field on every generated write, " +
			"so its validation is excluded under every gate, and what checks the value is " +
			"the rule you write.\n\n")
	}

	if len(in.MigrationsKept) > 0 {
		empty = false
		renderMigrationHandoff(b, in)
	}

	if len(in.MissingTranslations) > 0 {
		empty = false
		b.WriteString("### Missing translations\n\n")
		b.WriteString("These were emitted as marked placeholders (`TODO(LANG): …`). " +
			"They are real strings your end users will see, so they need real translations, " +
			"not English copied seven times.\n\n")
		for _, t := range in.MissingTranslations {
			fmt.Fprintf(b, "- %s\n", t)
		}
		b.WriteString("\n")
	}

	if len(in.UntranslatedEnumValues) > 0 {
		empty = false
		b.WriteString("### Enum values reading in the wrong language\n\n")
		b.WriteString("Each enum member is now registered in the seven catalogs under the key " +
			"the framework derives for it (`<Type>.<value>`), which is what " +
			"`translator.EnumDescription(lang, v)` looks up. These had no `text` in the spec " +
			"for the catalog named, so they were filled with the member's own name — a real " +
			"word, not a placeholder, which is why nothing else reports them. It is still the " +
			"member name in one language, shown to everyone:\n\n")
		for _, t := range in.UntranslatedEnumValues {
			fmt.Fprintf(b, "- %s\n", t)
		}
		b.WriteString("\nDeclare `valueObjects[].members[].text` and regenerate. The wire is " +
			"unaffected either way — it carries the raw value by design.\n\n")
	}

	if len(in.StaleRegistrations) > 0 {
		empty = false
		b.WriteString("### Yours in a shared file, and out of step with the spec\n\n")
		b.WriteString("The notification declarations and the seven translation catalogs are " +
			"shared by every entity of the project. The generator maintains what IT wrote " +
			"there — it records a hash of each declaration and each message, so a spec that " +
			"moves takes its own text with it. These did NOT match what it recorded, which " +
			"means somebody edited them, or they predate that record. Either way they are " +
			"not the generator's to overwrite, so they were left exactly as they are:\n\n")
		for _, t := range in.StaleRegistrations {
			fmt.Fprintf(b, "- %s\n", t)
		}
		b.WriteString("\nA notification DECLARATION on this list can stop the package " +
			"compiling, and the error will not point here: if the spec gave it a `tvars` " +
			"entry, the rules emitted for it now write `N{Max: \"50\"}` and the struct has no " +
			"such field — add it, and it goes away. A translation on this list is cosmetic " +
			"by comparison: the end user simply reads the older wording. If your version is " +
			"the better one, put it in the spec; the two will then agree and it drops off " +
			"this list.\n\n")
	}

	if empty {
		b.WriteString("Nothing. Every rule the spec declared was generated, and every " +
			"translation was supplied.\n\n")
	}
}

// writeDescriptionNote says what a REWORDED description costs, and it depends on
// the engines this service targets.
//
// The unconditional version of this paragraph was noise on a SQLite-only project:
// there is no catalogue slot for a description there, so rewording one is a
// provable no-op — and a warning that is wrong for the reader's project every
// single run is a warning they learn to skip, including on the run where it
// matters. Three shapes, because there are three truths: every target stores it,
// none does, or some do and the paragraph has to name which.
// redactorPhrase renders one axis in the report's own words rather than as the
// spec's keyword: a reviewer checking a security decision should not have to
// map `keep-last` onto what a consumer will actually see.
// identityQuestion and identityAnswer are the two halves of one row: WHAT the
// field asks about the caller, and WHICH framework accessor answers it. They are
// separate because a reviewer chasing a value in the generated code searches for
// the accessor, and a reviewer chasing a policy reads the question.
func identityQuestion(f ir.Field) string {
	switch f.IdentitySource {
	case "subject":
		return "who the caller is"
	case "tenant":
		return "which tenant the caller belongs to"
	case "permission":
		return "whether the caller holds `" + f.Permission + "`"
	case "super-admin":
		return "whether the caller holds the `" + ir.SuperAdminGrant + "` grant"
	default:
		return "whether the request carried an identity at all"
	}
}

func identityAnswer(f ir.Field) string {
	switch f.IdentitySource {
	case "subject":
		return "`Identity().Subject`"
	case "tenant":
		return "`Identity().TenantID()`"
	case "permission":
		return "`Identity().HasPermission(\"" + f.Permission + "\")`"
	case "super-admin":
		return "`Identity()." + ir.SuperAdminMethod + "()` — `HasPermission` panics on a " +
			"wildcard, so the grant has a question of its own"
	default:
		return "`ctx.Identity() != nil` — a fact no VALUE can carry, since an empty one " +
			"means both \"nobody\" and \"a real token without that claim\""
	}
}

// identityGrants are the concrete permissions the entity asks about, which is
// what a deployment has to issue. The super-admin grant is deliberately not one
// of them: nothing becomes grantable by asking that question.
func identityGrants(fields []ir.Field) []string {
	var out []string
	for _, f := range fields {
		if f.IdentitySource == "permission" {
			out = append(out, f.Permission)
		}
	}
	sort.Strings(out)
	return out
}

func backticked(vs []string) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = "`" + v + "`"
	}
	return out
}

// wildcardOf is the resource wildcard that satisfies a concrete permission —
// what "wider than being granted it" means, spelled for the reader instead of
// left as a claim about the framework.
func wildcardOf(permission string) string {
	if i := strings.Index(permission, ":"); i >= 0 {
		return permission[:i+1] + "*"
	}
	return permission
}

// bodyRuntimeCheck says what a reviewer has to look for on ONE such field. The
// two answers differ by whether the field carries a value object, because that
// is what decides whether anything at all checks the value automatically.
func bodyRuntimeCheck(f ir.Field) string {
	if f.VOKind == "" {
		return "nothing validates it — every check on this value is a rule you declared"
	}
	return fmt.Sprintf("`%s` judges the value's shape on the verbs above, and nothing else does",
		f.BaseEntityType)
}

func redactorPhrase(r ir.Redactor) string {
	switch r.Kind {
	case "fixed":
		return fmt.Sprintf("replaced by `%s`", r.Value)
	case "keep-last":
		return fmt.Sprintf("every character but the last %d masked (a value that short is masked whole)", r.Keep)
	case "hook":
		return fmt.Sprintf("masked by `%s`, which you write", r.HookFunc)
	default:
		return "**the real value**, declared plain"
	}
}

// redactAxisReach spells out what one axis governs, for the hook list.
func redactAxisReach(axis string) string {
	if axis == "InAudit" {
		return "the audit event"
	}
	return "the outbox payload, and with it the topic, the consumers and the document"
}

// backingClearNote is the half of the read-side answer that depends on the
// backing, and it is opposite in the two cases — which is exactly why it is
// written out instead of left as a general caution.
func backingClearNote(m *ir.Model) string {
	if m.Read.Backing == "mongo" {
		return "This read model is mongo-backed, so the projected document IS the redacted " +
			"payload: the reads serve the mask. Confirm no client depended on the full value."
	}
	return "This read model is RELATIONAL — it selects the columns, which hold the real " +
		"value — so every redacted field it projects is served in the clear unless it is " +
		"`hidden: true` or behind `read.fieldRestrict`. Check each row of the table above " +
		"against that."
}

func writeDescriptionNote(b *strings.Builder, dialects []string) {
	var stores, dont []string
	for _, d := range dialects {
		if d == "sqlite" {
			dont = append(dont, d)
			continue
		}
		stores = append(stores, d)
	}

	if len(stores) == 0 {
		b.WriteString("**Rewording a `description:` costs nothing here.** SQLite has no " +
			"catalogue slot for one, so a description lives only as a `--` line in the " +
			"migration file — and that file is frozen. The reworded text reaches the " +
			"generated Go source and stops there; there is no database state to update and " +
			"no migration to write.\n\n")
		return
	}

	fmt.Fprintf(b, "**A changed `description:` is a storage change too, on %s.** The "+
		"description is stored IN the database — a COMMENT on postgres, mysql and oracle, "+
		"an `MS_Description` extended property on sqlserver — so that someone holding a "+
		"connection and not this repository can read it. The code regenerates from the "+
		"spec; that catalogue entry does not. Rewording a description therefore needs a new "+
		"pair carrying just the `COMMENT ON` / `sp_addextendedproperty` statements, or the "+
		"database keeps answering with the old wording.",
		strings.Join(stores, ", "))
	if len(dont) > 0 {
		b.WriteString(" On sqlite there is nothing to write: it has no catalogue slot for a " +
			"description, so the same reword is a no-op for that engine alone.")
	}
	b.WriteString("\n\n")
}

// renderMigrationHandoff speaks to the reader holding a migration that an
// EARLIER run created — the only case where the code and the database can have
// drifted apart without anyone being told.
//
// It sits in "what still needs implementing" rather than in a footnote because
// it is the one gap here that a database will not forgive. And it never asks
// for an `ALTER`: writing one is a decision about live data, which is the
// author's, and a generator that guessed at it would be guessing about
// environments it cannot see.
func renderMigrationHandoff(b *strings.Builder, in Input) {
	m := in.Model
	b.WriteString("### The migration — already yours\n\n")

	b.WriteString("The SQL for this entity was written on an earlier run and **was not " +
		"touched**:\n\n")
	for _, p := range in.MigrationsKept {
		fmt.Fprintf(b, "- `%s`\n", p)
	}
	b.WriteString("\nThat is permanent, and it is the same posture as the `_manual` rule " +
		"files: created once, never regenerated. A migration is the only thing here whose " +
		"effect outlives the file — once it has run anywhere, the framework's tracking " +
		"table records it as applied, so rewriting the file would change what the file " +
		"CLAIMS without changing a single table. A service that boots green and fails on " +
		"the first query touching the change is the outcome being avoided.\n\n")

	b.WriteString("**If the shape below no longer matches what that migration created, " +
		"the fix is a NEW numbered pair in the same folder** — never an edit to one that " +
		"may have run. Two things are worth being deliberate about, because they are where " +
		"data is lost: adding a NOT NULL column to a table that already has rows fails " +
		"unless it carries a default, and a rename done as drop-then-add takes the data " +
		"with it.\n\n")

	b.WriteString("If nothing about the storage changed this run, there is nothing to do " +
		"here — read the shape as confirmation, not as a task.\n\n")

	writeDescriptionNote(b, m.Dialects)

	fmt.Fprintf(b, "The shape the regenerated code expects, for `%s`:\n\n", m.Table)
	for _, t := range in.TargetTables {
		fmt.Fprintf(b, "**`%s`** — %s\n\n", t.Name, t.Purpose)
		b.WriteString("| Column | Type | Null | Note |\n|---|---|---|---|\n")
		for _, c := range t.Columns {
			typ := c.Type
			if c.Length > 0 {
				typ = fmt.Sprintf("%s(%d)", c.Type, c.Length)
			}
			null := "no"
			if c.Nullable {
				null = "yes"
			}
			fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", c.Name, typ, null, c.Note)
		}
		// The constraints, right after the columns. A uniqueness whose scope
		// changed adds no column, so a shape that stops at the columns looks
		// already satisfied while the rule the domain relies on is still the old
		// one — and that gap surfaces as a value the API accepts and the database
		// refuses.
		if len(t.Indexes) > 0 {
			b.WriteString("\nIndexes it expects:\n\n")
			for _, idx := range t.Indexes {
				kind := "index"
				if idx.Unique {
					kind = "UNIQUE"
				}
				scope := "over every row"
				if idx.ActiveOnly {
					scope = "over the ACTIVE rows only — an archived one frees the value"
				}
				fmt.Fprintf(b, "- `%s` — %s on (%s), %s; %s\n",
					idx.Name, kind, strings.Join(idx.Columns, ", "), scope, idx.Note)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	dialects := strings.Join(m.Dialects, ", ")
	fmt.Fprintf(b, "A new pair goes in every dialect this service targets (%s), numbered "+
		"after the highest existing one. Every `.up.sql` needs its `.down.sql` or the "+
		"service refuses to boot.\n\n", dialects)

	b.WriteString("If this entity has NOT shipped anywhere yet — you are still the only " +
		"one who ever ran it — deleting the pair above and regenerating writes it fresh " +
		"from the current spec. That is safe exactly while that is true, and never after.\n\n")
}

// renderCheck is section B: the decisions that cost a full regeneration if wrong.
func renderCheck(b *strings.Builder, in Input) {
	m := in.Model
	// A type that is not where its author put it deserves one line, not a
	// surprise the next time they go looking for it.
	var moved []string
	for _, n := range m.Notifications {
		if n.Moved {
			moved = append(moved, n.Name)
		}
	}
	if len(moved) > 0 {
		b.WriteString("### Notifications placed by the generator\n\n")
		fmt.Fprintf(b, "%s raised by a child's rule or by a value object, so declared "+
			"beside the type that raises them rather than in `internal/domain`: `domain` "+
			"imports both packages, so holding one there would be an import cycle, not a "+
			"style choice.\n\n",
			strings.Join(moved, ", "))
	}

	// A field the caller never receives. It is the one shape a reviewer cannot
	// see by reading the generated DTOs — the field is simply not in them, and
	// absence looks the same as an oversight. Saying it out loud is also the only
	// place the CSV/XLSX consequence is written down: the exports render the
	// listing, so a column somebody used to get is gone with it.
	var hidden []string
	for _, f := range m.AllOwnerFields() {
		if f.Hidden {
			hidden = append(hidden, fmt.Sprintf("`%s`", f.Name))
		}
	}
	// Redaction is the one decision in this report that is invisible in every
	// generated file except the schema, and whose consequence lands OUTSIDE this
	// service: the topic, the consumers, the audit trail. A reviewer reading the
	// DTOs sees the field in full and concludes nothing was masked.
	if red := ir.RedactedFields(m); len(red) > 0 {
		b.WriteString("### Fields whose copies carry a mask\n\n")
		b.WriteString("The real value stays in the COLUMN and in the hydrated entity — the " +
			"rules read it, the writes store it. What is masked is every copy the framework " +
			"makes of the row:\n\n")
		b.WriteString("| field | in the sync (payload → topic → consumers → document) | in the audit event |\n")
		b.WriteString("|---|---|---|\n")
		for _, r := range red {
			fmt.Fprintf(b, "| `%s.%s` | %s | %s |\n", r.Seat, r.Field.Name,
				redactorPhrase(r.Field.Redaction.InSync), redactorPhrase(r.Field.Redaction.InAudit))
		}
		b.WriteString("\nThree things to check, in the order they bite.\n\n")
		b.WriteString("1. **The read side is NOT covered by this.** Redaction governs what the " +
			"framework copies; what this service's own API returns is yours. " +
			backingClearNote(m) + "\n")
		b.WriteString("2. **A rebuild is what fixes documents already written.** Declaring a " +
			"redaction, or changing a redactor or its parameter, changes the projected shape: " +
			"the framework's own drift check fires, `read.view.version` must be bumped, and " +
			"the resulting rebuild is what replaces the values an earlier policy already " +
			"projected. Without it, turning a field redacted protects future writes while " +
			"every document already in the read model keeps its plaintext.\n")
		b.WriteString("3. **A `hook` is invisible to that check.** A closure has no portable " +
			"identity, so the hash mixes in only the KIND — changing what the function " +
			"RETURNS is a shape change nobody detects for you. Bump the version yourself " +
			"when you change one.\n\n")
		b.WriteString("It is forward-only, and it does not protect the database: the column " +
			"holds the real value, and anyone with SQL access reads it.\n\n")
	}

	if len(hidden) > 0 {
		b.WriteString("### Fields nobody receives\n\n")
		fmt.Fprintf(b, "%s — declared `hidden: true`, so stored, filterable and writable, "+
			"and absent from every response: the by-id read, each row of the listing, the "+
			"write responses, and the CSV/XLSX exports that render the listing. This is not "+
			"`read.fieldRestrict`, which returns the field to callers holding a permission; "+
			"nobody receives these. Check that a client is not expected to read back what it "+
			"just wrote.\n\n", strings.Join(hidden, ", "))
	}

	// A field the caller sends and the database never sees. It is the mirror of
	// the case above and needs saying for the same reason: a reviewer reading
	// the migration finds no column and concludes the field was forgotten, and
	// one reading the entity finds it and concludes it is stored.
	if body := m.BodyRuntimeFields(); len(body) > 0 {
		b.WriteString("### Fields the caller sends and nothing stores\n\n")
		b.WriteString("Declared `runtime: true` with `source: body`. Each one crosses the " +
			"write request, the command and the aggregate — so the rules can read it — and " +
			"stops there. There is no column, so it is in no migration, no `TableSchema`, " +
			"no outbox payload, no audit event and no response.\n\n")
		b.WriteString("| field | carried by | what to check |\n")
		b.WriteString("|---|---|---|\n")
		for _, f := range body {
			fmt.Fprintf(b, "| `%s` | %s | %s |\n", f.Name,
				strings.Join(f.Modes, ", "), bodyRuntimeCheck(f))
		}
		b.WriteString("\nTwo consequences worth reading twice. **Nothing compares it for " +
			"you**: the value object on the field checks the value's SHAPE, and \"the " +
			"confirmation matches the password\" is a rule — declare it (`kind: comparison`) " +
			"or the field is collected and ignored. And **the verbs it does not name skip " +
			"its value object entirely**, because a write that never carried the field has " +
			"nothing to judge; if a verb must require it, name that verb under the field's " +
			"`modes`.\n\n")
	}

	// What the aggregate knows about whoever is asking. It is reported for two
	// readers at once. A REVIEWER meets a field on the entity that no column
	// backs and no caller sends, and has to be able to find out where its value
	// came from. And whoever runs the DEPLOYMENT has to know which permissions
	// the domain now asks about — a grant nobody issues is not an error anywhere:
	// the field is simply false, every rule reading it takes the deny branch, and
	// the service refuses work it was supposed to allow.
	if identity := m.DeclaredIdentityFields(); len(identity) > 0 {
		b.WriteString("### What this entity asks about the caller\n\n")
		b.WriteString("Declared `runtime: true` with an identity `source`. The domain is " +
			"handed no request and no `ctx`, so each of these rides onto the aggregate in " +
			"the command mapper — `if id := ctx.Identity(); id != nil { … }` — and the " +
			"rules read it from there. None of them is stored: no column, no migration, no " +
			"`TableSchema`, no outbox payload, no audit event, no response.\n\n")
		b.WriteString("| field | asks | answered by |\n")
		b.WriteString("|---|---|---|\n")
		for _, f := range identity {
			fmt.Fprintf(b, "| `%s` | %s | %s |\n", f.Name,
				identityQuestion(f), identityAnswer(f))
		}
		if grants := identityGrants(identity); len(grants) > 0 {
			fmt.Fprintf(b, "\n**Grant these, or the answer is always no.** This entity now "+
				"asks about %s. HOLDING one is wider than being granted it: `HasPermission` "+
				"resolves the resource wildcard (`%s`) and the `*:*` grant, which is the "+
				"point of asking the permission model instead of reading a boolean claim off "+
				"the token.\n",
				strings.Join(backticked(grants), ", "), wildcardOf(grants[0]))
		}
		b.WriteString("\nThe claim NAMES behind these are the framework's to resolve, not " +
			"this code's: the tenant claim is `authorization.tenant.claim` and the " +
			"permissions claim is `authorization.permissionsClaim`. The generated feed " +
			"calls the accessor and never spells either name — the generated unit tests " +
			"build their fixture Identity under the framework's DEFAULTS, which is the " +
			"only name a test with no yaml can honestly use.\n\n")
	}

	// The one field that is server-assigned and in a request body anyway. A
	// reviewer reading the insert DTO sees a caller choosing their own scope and
	// is right to stop; what makes it safe is a guard in another file, and this
	// is where the two are put side by side.
	if f := m.BypassSettableField(); f != nil {
		what, guardName := "tenant", "refuseForeignTenant"
		if m.Authz.DataAccess == "owner-only" {
			what, guardName = "owner", "refuseForeignOwner"
		}
		who := "a caller holding `" + m.Authz.Bypass + "`"
		if m.Authz.BypassWildcard {
			who = "a super-admin (`" + m.Authz.Bypass + "`)"
		}
		b.WriteString("### The " + what + " is server-assigned, and the insert accepts one anyway\n\n")
		fmt.Fprintf(b, "`%s` is declared `assignedFrom: %s` with `bypassMaySet: true`, so it is "+
			"filled from the caller's identity on every insert and is in no update or patch "+
			"body. The INSERT body carries it as an OPTIONAL value, for one reason: %s crosses "+
			"the row scope, and without a field to name the %s in they could repair a "+
			"customer's records and never create one.\n\n",
			f.Name, f.AssignedFrom, who, what)
		fmt.Fprintf(b, "**Check the guard, not the mapper.** The mapper applies whatever was "+
			"sent, deliberately: what refuses a caller who may not state a %s is `%s` in "+
			"`internal/domain/%s.go`, which compares `%s` against the caller's own and stands "+
			"down only for the bypass. Two things follow. A caller who names someone else's "+
			"%s gets the same refusal a write into that %s gets — not a silent 201 filed "+
			"under their own. And if that guard is ever removed or narrowed, this field "+
			"becomes a %s anyone can choose.\n\n",
			what, guardName, m.Entity.Snake, f.Name, what, what, what)
	}

	// Same principle, for rules: one declared on a collection but enforced from
	// the root. A reader who goes looking for it in the child's file and does
	// not find it has to be told where it went, and why it could not stay.
	var hoisted []string
	for _, cl := range m.Clauses {
		for _, r := range cl.Rules {
			if r.Hoisted {
				hoisted = append(hoisted, fmt.Sprintf("`%s` (on `%s`)", r.ID, r.Collection))
			}
		}
	}
	if len(hoisted) > 0 {
		b.WriteString("### Rules enforced from the root\n\n")
		fmt.Fprintf(b, "%s — declared on the collection, enforced in the aggregate's "+
			"`BuildRules`. A rule that compares a record against the way it WAS needs the "+
			"previous version, and an entry does not have one: the framework exposes the "+
			"prior entries from the aggregate root. The check pairs each surviving entry "+
			"with its former self%s, so a notification it raises is addressed to the "+
			"collection rather than to one entry's field.\n\n",
			strings.Join(hoisted, ", "), pairingNote(m, hoisted))
	}

	// A barrier changes what a REVIEWER has to check, which is why it is here and
	// not only in the file: a 422 that carries one field instead of four is the
	// correct behaviour of a guard and the classic symptom of a broken one, and
	// nothing else in this report would tell the two apart.
	var barriers []string
	for _, cl := range m.Clauses {
		for _, r := range cl.Rules {
			if r.Guard {
				barriers = append(barriers, fmt.Sprintf("`%s` (in `%s`)", r.ID, cl.Gate))
			}
		}
	}
	if len(barriers) > 0 {
		b.WriteString("### Rules that END the validation pass\n\n")
		fmt.Fprintf(b, "%s — declared `guard: true`. After each of them the pass stops if "+
			"anything has already been rejected: the rules below it, this entity's "+
			"automatic value-object validation, and the `BuildRules` and value objects of "+
			"every collection. A clean write is unaffected — the barrier fires only where "+
			"something was ALREADY refused — so what changes is the SHAPE of a 422: it "+
			"carries what was found up to the barrier, not that plus every other field the "+
			"write would have failed on. Check that against what the API consumers expect "+
			"to receive in one response.\n\n", strings.Join(barriers, ", "))
	}

	// The other half of the same reviewer's question. A barrier changes what a
	// 422 leaves OUT; a value object validated early changes what it puts back
	// IN — and the field the reader will look for in BuildRules is one the
	// entity's documentation says is never checked there.
	var pulled []string
	for _, cl := range m.Clauses {
		for _, r := range cl.Rules {
			if r.Kind != "valueObject" {
				continue
			}
			for _, f := range r.Fields {
				pulled = append(pulled, fmt.Sprintf("`%s` (in `%s`)", f.Name, cl.Gate))
			}
		}
	}
	if len(pulled) > 0 {
		b.WriteString("### Value objects validated with the rules, not after them\n\n")
		fmt.Fprintf(b, "%s — declared `kind: valueObject`. The framework validates every "+
			"value-object field on every write, but that pass runs AFTER `BuildRules`, so "+
			"a value object cannot be the premise of the rules below it. Each field above "+
			"is checked where it is declared instead — by the value object's own answer, "+
			"never a second one — and excluded from the automatic pass in those verbs, so "+
			"nothing is reported twice. Two consequences worth checking: the value is now "+
			"validated BEFORE any barrier that follows, so a 422 that used to hide it "+
			"behind another failure now carries both; and in the verbs this rule does not "+
			"cover, the field is still validated at the end, exactly as before.\n\n",
			strings.Join(pulled, ", "))
	}

	// A field that vanished from the API is the kind of thing a reader notices
	// as a bug rather than as a decision, so the report says it was one.
	if assigned := m.AssignedFields(); len(assigned) > 0 {
		b.WriteString("### Fields the server fills\n\n")
		for _, f := range assigned {
			if f.AssignedFrom == "derived" {
				// Server-assigned too, but the generator wrote no assignment for
				// it: it is outstanding WORK, so it is listed where outstanding
				// work is, at the top of this report.
				continue
			}
			source := "the caller's own identifier (`Identity().Subject`)"
			if f.AssignedFrom == "identity-claim" {
				source = fmt.Sprintf("the `%s` claim of the caller's token", f.Claim)
			}
			fmt.Fprintf(b, "- **`%s`** — written on insert from %s. It is **absent from every "+
				"write request and command**: a client cannot set it, and an update does not "+
				"touch it. Confirm the callers are authenticated on the insert route — with no "+
				"identity the field stays empty, and nothing else will say so.\n", f.Name, source)
		}
		b.WriteString("\n")
	}

	// A collection this spec exposes but does not own: the reader has to know
	// which half is theirs before they go looking for a file that is not here.
	var mounted []ir.Child
	for _, c := range m.Children {
		if c.Mounted {
			mounted = append(mounted, c)
		}
	}
	if len(mounted) > 0 {
		b.WriteString("### Collections of the shared identity\n\n")
		for _, c := range mounted {
			fmt.Fprintf(b, "- **`%s`** (`%s`) — this role EXPOSES it; the role that declares "+
				"the identity owns it. No table was created, no entry type or input DTO was "+
				"written, and none of it is this spec's to change: adding a field to the "+
				"collection means adding it THERE, and restating it here. What this run "+
				"generated is the surface — the routes under `%s`, their commands and their "+
				"wire types, named `%s…` so they cannot collide with the other role's.\n",
				c.Name, c.Table, "/"+m.Entity.PluralSnake+"/:id/"+c.Segment, c.OpBase)
		}
		b.WriteString("\n")
	}

	// Newly generated tests can collide with ones an author wrote to fill the
	// same gap. The compiler says so, but it says it as "redeclared in this
	// block", which reads like a generator bug rather than a redundancy.
	if m.HasPerChild() {
		b.WriteString("### Per-entry command tests are generated now\n\n")
		b.WriteString("The verbs that address ONE entry — " + perChildVerbs(m) + " — have " +
			"generated tests in `" + fmt.Sprintf("internal/application/commands/%s_commands_test.go",
			m.Entity.Snake) + "`: the entry is applied and projected back, a change keeps its " +
			"id, an unknown id projects nothing.\n\n")
		b.WriteString("**If you wrote your own tests for those mappers before this run**, the " +
			"package will not compile until you delete them — Go reports it as `redeclared in " +
			"this block`, which reads like a generator bug and is not one. The generated cases " +
			"cover the same ground; anything yours asserts beyond them is worth keeping under a " +
			"different name.\n\n")
	}

	b.WriteString("## What to check\n\n")

	// One line per raw value object. The generator cannot know whether a set is
	// closed — but the question is worth asking every time, because the wrong
	// answer is invisible: a shape check accepts every string that LOOKS right,
	// including the ones that do not exist.
	var raws []string
	for _, vo := range m.ValueObjects {
		if vo.Kind == "raw" {
			raws = append(raws, vo.Name)
		}
	}
	if len(raws) > 0 {
		fmt.Fprintf(b, "- **Is the set of %s really open?** They are declared as shapes "+
			"(`kind: raw`), so anything matching the pattern is accepted. If the valid "+
			"values are FINITE and known — a state code, a status, a category — it is an "+
			"`enum` instead: the caller gets the list in OpenAPI, the code gets named "+
			"constants, and an out-of-set value converges to Unknown rather than being "+
			"stored.\n\n", strings.Join(raws, ", "))
	}

	// The two composite decisions a reviewer cannot see from the Go code alone,
	// and that both cost something real to change afterwards: the names the parts
	// are EXPOSED under (a wire contract, because nothing above the schema knows
	// a composite exists) and whether the value object is optional as a whole
	// (which decides the nullability of every one of its columns).
	for _, g := range ir.Composites(m.AllOwnerFields()) {
		names := make([]string, 0, len(g.Parts))
		for _, p := range g.Parts {
			names = append(names, "`"+p.JSONName+"`")
		}
		presence := "mandatory — it is always there, and each part follows its own type"
		if g.Optional() {
			presence = "OPTIONAL as a whole — every one of its columns is NULL-able, because " +
				"absence is written and read as all-NULL"
		}
		fmt.Fprintf(b, "- **Are %s the names you want on the wire?** They are the parts of the "+
			"composite value object `%s`, and they are the ONLY names the outside world ever "+
			"sees — the filter, `?fields=`, `orderBy`, the JSON field, the export column and "+
			"the projected document key — because nothing above the schema learns a composite "+
			"exists. Renaming one later is a wire break, not a refactor. The value object is "+
			"%s.\n\n", strings.Join(names, ", "), g.Head.VOName, presence)
	}

	b.WriteString("These are the decisions the spec made that are expensive to change later. " +
		"Read them against what you actually meant.\n\n")
	b.WriteString("| Decision | Value | Why it matters |\n|---|---|---|\n")

	// The posture, not an assumption about it. This line said "flat table" for
	// every entity, including a shared-base role — and a reviewer who trusts the
	// summary then verifies the wrong thing, while one who does not trust it has
	// no reason to trust the rest of the table either.
	if m.IsRole() {
		fmt.Fprintf(b, "| Storage | role `%s` over the shared identity `%s` (%s) | The "+
			"identity is deduplicated by %s and outlives this role; fields marked "+
			"`livesOn: base` are seen by every other role over the same one. |\n",
			m.Table, m.Base.Table, m.Base.Link, m.Base.NaturalKey)
	} else {
		fmt.Fprintf(b, "| Storage | flat table `%s` | A field group that should be shared with "+
			"another role later would need a real migration to extract. |\n", m.Table)
	}

	modes := make([]string, 0, len(m.Ops))
	for _, op := range m.Ops {
		modes = append(modes, op.Verb)
	}
	fmt.Fprintf(b, "| Operations | %s | Each one is a route with a permission; an unwanted "+
		"one is a surface you did not mean to expose. |\n", "`"+strings.Join(modes, "`, `")+"`")

	// The per-entry verbs are routes with permissions too, and the row above
	// does not carry them — they are not operations of the root, so they were in
	// no table at all. A reviewer had no way to see what gates the collection
	// edge, which is precisely the edge where the answer is sometimes meant to
	// be different: adding a role to a group is not renaming the group, and one
	// permission for both is how an administrator widens their own.
	for _, c := range m.Children {
		if !c.PerChild {
			continue
		}
		fmt.Fprintf(b, "| Collection `%s` | %s | These routes hang off `/%s/:id/%s`. %s%s |\n",
			c.Plural, childPermissionCells(c), m.Entity.PluralSnake, c.Segment,
			childPermissionAdvice(c), childRemovalNote(c))
	}

	if m.Managed.Archiving {
		// "(reversible)" names ANOTHER endpoint, so it may only be claimed when
		// that endpoint exists — the same rule the archive SUMMARY already
		// follows. Archiving without unarchive is a legitimate model: the row
		// survives as history and nothing brings it back, and a reviewer told
		// otherwise signs off on an undo the service does not have.
		kind, note := "archive (one-way: no unarchive is mounted)", ""
		if m.Op("unarchive") != nil {
			kind, note = "archive (reversible)", " The unarchive endpoint is what reverses it."
		}
		fmt.Fprintf(b, "| Removal | %s | `DELETE` is a permanent purge and is "+
			"%s.%s |\n", kind, presence(m.Op("delete") != nil, "also mounted", "not mounted"), note)
	} else {
		b.WriteString("| Removal | no archive column | Rows are removed permanently or not at all. |\n")
	}

	// A reviewer reading the routes sees an update and an archive as two doors.
	// This is the case where one of them is also the other, and nothing on the
	// wire says so: the caller sends a plain PUT/PATCH and the row retires.
	//
	// Two gates decide a write, and this door changes neither: it arrives on the
	// update's ROUTE, under the update's permission, and it runs the update's
	// RULES. Saying only the first — which this table did — leaves the reviewer
	// checking half of it, and the half it left out is the one that names
	// something concrete: the archive-scoped rules of THIS entity, which the
	// generator can list.
	if aw := m.ArchiveWhen; aw != nil {
		rest := "the row is archived holding that value"
		if aw.Becomes != "" {
			rest = fmt.Sprintf("the row is archived holding `%s`", aw.Becomes)
		}
		skipped := "and `IfArchive` does not fire on it, so any rule you later scope to " +
			"`archive` will not see it either"
		if ids := archiveScopedRuleIDs(m); len(ids) > 0 {
			skipped = fmt.Sprintf("and `IfArchive` does NOT fire on it, so %s "+
				"(scoped to `archive`) never runs on this path — restate it with "+
				"`scope: [update]` if it has to guard both doors",
				"`"+strings.Join(ids, "`, `")+"`")
		}
		fmt.Fprintf(b, "| Removal, second door | an update that sets `%s` to `%s` | "+
			"The DOMAIN retires the row from an ordinary update: full archive — stamp, "+
			"child cascade, `ARCHIVED` event, archive audit entry — and %s. It runs under "+
			"the UPDATE's permission, not `archive`'s, %s. Check that the callers of the "+
			"update route are the ones allowed to retire a record. |\n",
			aw.Field.Name, aw.Equals, rest, skipped)
	}

	for _, f := range m.Fields {
		if f.Unique == nil {
			continue
		}
		// The SCOPE belongs here as much as the enforcement: whether an archived
		// row keeps holding the value is the half a reviewer actually has an
		// opinion about, and leaving it out reads as "unique, period".
		scope := f.Unique.Scope
		if scope == "" {
			scope = "all"
		}
		reuse := "an archived row keeps holding it, so the value is never free again"
		if scope == "active-only" {
			reuse = "an archived row frees it, so the value can be taken again"
		}
		// WITHIN WHAT is the first question a reviewer has about a natural key,
		// and the one the report used to answer by saying nothing — which reads
		// as "across the whole table" whether or not that is what was built.
		// It is stated from the constraint, so this line and the DDL cannot
		// drift apart.
		//
		// The constraint is looked up by the key it was FILED under, which is
		// the VALUE OBJECT's name for a composite and the field's own only for
		// a plain one. Matching the part's JSONName — which is what this did —
		// never found a composite's constraint at all, so one scoped `within`
		// something reported as unique across the whole table while the
		// migration created the scoped index.
		key := f.JSONName
		if f.Composite != nil {
			key = naming.Camel(f.Composite.Owner)
		}
		within := "across the whole table"
		for _, c := range m.Constraints {
			if c.Kind == "unique" && c.Field == key && len(c.Within) > 0 {
				within = "per " + strings.Join(c.Within, " + ")
			}
		}
		// WHAT is unique. Uniqueness declared on a composite lands on its FIRST
		// PART, so the constraint is built once over the whole run — and naming
		// that part here described a single-column index over one half of the
		// value object, while the migration created one over the tuple. The
		// reviewer who trusts the summary verifies a constraint that does not
		// exist; the one who reads it against the DDL has no reason to trust
		// the rest of the table.
		subject := "`" + f.Name + "`"
		tuple := ""
		if f.Composite != nil {
			subject = "`" + f.Composite.VOName + "` (" +
				strings.Join(compositePartNames(m.Fields, f), " + ") + ")"
			tuple = " Unique as a TUPLE: the parts identify together, so a row differing " +
				"in either one is a different value — a constraint over a single part " +
				"would refuse rows the domain accepts."
		}
		fmt.Fprintf(b, "| Unique | %s — %s, scope `%s` (%s) | %s; a duplicate is refused at "+
			"the database and reported as `%s`.%s |\n",
			subject, within, scope, f.Unique.Enforce, reuse, f.Unique.Notification, tuple)
	}

	if m.Surfaces.GraphQL {
		for _, sib := range m.SiblingsOn("") {
			fmt.Fprintf(b, "| Clearing the %s facet | REST: `PUT` with its fields null · "+
				"GraphQL: `clear%sOf%s` | On GraphQL an omitted field and an explicit null "+
				"reach the DTO identically, so \"clear it\" cannot be told from \"leave it\" "+
				"— the intent needs its own verb. Both surfaces end in the same write. |\n", sib.Name, sib.Name, m.Entity.Pascal)
		}
	}

	fmt.Fprintf(b, "| Data access | %s | %s |\n", m.Authz.DataAccess,
		dataAccessNote(m.Authz.DataAccess))

	// A scope with an exception is two decisions, and the second one is the one
	// a reviewer has to be told about: it is the line that lets somebody read
	// and repair rows that are not theirs.
	if m.Authz.Bypass != "" {
		fmt.Fprintf(b, "| Crossing the scope | `%s` | %s |\n", m.Authz.Bypass, bypassNote(m))
	}

	if m.Read.Enabled {
		fmt.Fprintf(b, "| Read backing | %s | %s |\n", m.Read.Backing,
			backingNote(m.Read.Backing))
	}
	// A read join reaches OUTSIDE this aggregate, which is the one decision on
	// this page a reviewer cannot see by reading the entity — the fields look
	// like the entity's own. Each one gets its own row, with the two things that
	// bite: what a missing counterpart does, and whether the value is on the
	// wire.
	for _, j := range m.Joins {
		fmt.Fprintf(b, "| Read join → %s | `%s` on `%s`%s | %s |\n",
			j.Target, j.Verb(), j.FKColumn, joinWhere(j), joinNote(m, j))
	}
	b.WriteString("\n")
}

// compositePartNames lists the exposed names of the composite value object
// whose first part is `head`, in declaration order — the tuple its unique
// constraint covers, in the spelling the entity carries the parts under.
func compositePartNames(fields []ir.Field, head ir.Field) []string {
	var out []string
	for _, g := range ir.Composites(fields) {
		if g.Head.Owner != head.Composite.Owner {
			continue
		}
		for _, p := range g.Parts {
			out = append(out, "`"+p.Name+"`")
		}
	}
	return out
}

func joinWhere(j ir.Join) string {
	if j.Child == "" {
		return ""
	}
	return ", from " + j.Child
}

// joinNote is what a reviewer has to check about one traversal.
func joinNote(m *ir.Model, j ir.Join) string {
	var b strings.Builder
	switch {
	case j.Child != "" && j.Kind == "inner":
		b.WriteString("An entry with no counterpart is NOT returned — a silent hole in the " +
			"collection, not a missing aggregate. Prefer left wherever the relationship is " +
			"genuinely optional. ")
	case j.Child != "":
		b.WriteString("Filled on every loaded entry; nil where there is no counterpart. " +
			"Load-only: no filter and no `?orderBy=` reaches a field of a 1:N collection. ")
	case j.Kind == "inner":
		b.WriteString("An aggregate with no counterpart is NOT returned, on EVERY read " +
			"through this repository — FindByID included, which the write handlers load " +
			"through. Legal only because the foreign key is non-nullable. ")
	default:
		b.WriteString("Always in the FROM, so the fields are populated on every read; nil " +
			"means there is no counterpart, never the zero value. ")
	}
	b.WriteString("Nothing here is a write path: the fields are absent from the TableSchema, " +
		"so no INSERT or UPDATE can carry them and no migration creates them. ")

	// The one row on this page the generator did NOT verify. With a target it can
	// read, the column, its type and its nullability are checked against that
	// spec; with a hand-written one there is nothing to check them against and
	// the declaration was taken on the author's word. The framework checks the
	// same things at repository construction — so the cost of a wrong word here
	// is a boot that refuses, and a reviewer is the last chance to catch it
	// before that.
	if j.TargetHandWritten {
		fmt.Fprintf(&b, "%s is HAND-WRITTEN — no spec of this project declares it — so the "+
			"column names, their types and their nullability came from the spec's author, "+
			"unchecked here. Confirm each against %s's own schema: the framework validates "+
			"them at repository construction, and a nullable column landing in a "+
			"non-pointer field aborts the boot rather than the build. ", j.Target, j.Target)
	}

	var served, hidden []string
	for _, f := range j.Fields {
		if f.Hidden {
			hidden = append(hidden, f.Name)
			continue
		}
		served = append(served, f.Name)
	}
	if len(hidden) > 0 {
		fmt.Fprintf(&b, "On the entity and OFF the wire: %s — read by the rules, in no "+
			"response body and in no export. ", strings.Join(hidden, ", "))
	}
	if len(served) > 0 && m.Read.Backing == "mongo" {
		fmt.Fprintf(&b, "The read side is Mongo-backed, so %s reach the entity and the "+
			"rules but NOT the view — a projection is composed from the TableSchema, "+
			"which a join never enters.", strings.Join(served, ", "))
	}
	return strings.TrimSpace(b.String())
}

func dataAccessNote(kind string) string {
	switch kind {
	case "anyone-with-permission":
		return "Any caller holding the permission sees and edits every row. If some callers should only see their own, this is the line to change."
	case "owner-only":
		return "Callers are restricted to rows they own."
	case "tenant":
		return "Callers are restricted to their tenant's rows."
	}
	return ""
}

// bypassNote explains the exception to the row scope, and each of the two forms
// has one thing the reader is likely to get wrong.
//
// For a concrete permission it is that HOLDING it is not the same as being
// granted it: a resource wildcard covers it too, so `platform:*` crosses the
// scope as surely as the permission itself. For the wildcard form it is that
// the guard calls a method the reader may not know is there, since every other
// authorization question in the generated code goes through HasPermission.
func bypassNote(m *ir.Model) string {
	if m.Authz.BypassWildcard {
		return "Only a super-admin crosses the scope, and nothing new became grantable — " +
			"what crosses is the claim they already carry. The wildcard cannot be handed " +
			"to the framework's HasPermission (it panics on one), so the generated guard " +
			"calls `Identity." + ir.SuperAdminMethod + "()` instead — the framework's own " +
			"question for the `*:*` grant, nil-safe and honouring the configured " +
			"permissions claim. A resource wildcard like `role:*` does NOT answer it."
	}
	res := m.Authz.Bypass
	if i := strings.Index(res, ":"); i > 0 {
		res = res[:i]
	}
	return "A caller holding it reads and writes outside the scope — the operator " +
		"supporting a customer. HOLDING it is wider than being granted it: the framework " +
		"answers yes for `" + res + ":*` and for `*:*` too, so anyone with the resource " +
		"wildcard crosses the scope as well. If the intent was \"only a super-admin\", " +
		"declare `bypass: \"*:*\"` instead of minting a permission."
}

func backingNote(backing string) string {
	if backing == "relational" {
		return "Reads come straight from the tables, so a write is visible immediately. " +
			"Nothing is materialised: there is no collection, no version and no rebuild — " +
			"a shape change here needs no bump and no operational step."
	}
	return "Reads come from a projection, which is updated shortly after a write rather than instantly."
}

// renderGenerated is section C: the WHAT before the paths.
func renderGenerated(b *strings.Builder, in Input) {
	b.WriteString("## What was generated\n\n")

	byAction := map[fsplan.Action][]fsplan.Decision{}
	for _, d := range in.Decisions {
		byAction[d.Action] = append(byAction[d.Action], d)
	}

	written := append(append([]fsplan.Decision{}, byAction[fsplan.Create]...), byAction[fsplan.Update]...)
	sort.SliceStable(written, func(i, j int) bool { return written[i].File.Path < written[j].File.Path })

	if len(written) > 0 {
		b.WriteString("| What | File |\n|---|---|\n")
		for _, d := range written {
			desc := d.File.Describes
			if desc == "" {
				desc = "—"
			}
			fmt.Fprintf(b, "| %s | `%s` |\n", desc, d.File.Path)
		}
		b.WriteString("\n")
	}

	if kept := byAction[fsplan.KeptHook]; len(kept) > 0 {
		b.WriteString("**Left untouched** (yours, by design):\n\n")
		for _, d := range kept {
			fmt.Fprintf(b, "- `%s` — %s\n", d.File.Path, d.Reason)
		}
		b.WriteString("\n")
	}

	if refused := byAction[fsplan.RefusedEdited]; len(refused) > 0 {
		b.WriteString("**Refused** — these differ from what the generator last wrote, so they " +
			"were left exactly as they are:\n\n")
		for _, d := range refused {
			fmt.Fprintf(b, "- `%s` — %s\n", d.File.Path, d.Reason)
		}
		b.WriteString("\nTo let the generator take one back, pass `--force=<path>`; to keep a fix " +
			"deliberately, run `omnicore-gen adopt <path>`.\n\n")
	}

	if unchanged := byAction[fsplan.Unchanged]; len(unchanged) > 0 {
		fmt.Fprintf(b, "%d file(s) were already up to date.\n\n", len(unchanged))
	}

	if len(in.Orphans) > 0 {
		b.WriteString("**No longer generated** — the spec changed and these are left over:\n\n")
		for _, o := range in.Orphans {
			fmt.Fprintf(b, "- `%s`\n", o)
		}
		b.WriteString("\n")
	}
}

// renderNotGenerated is section D. It exists so the report cannot be misread as
// "everything is covered".
func renderNotGenerated(b *strings.Builder, in Input) {
	m := in.Model
	b.WriteString("## What was NOT generated\n\n")

	b.WriteString("Owned by other tools:\n\n")
	b.WriteString("- the gRPC surface and its proto contract — `/omnicore:implement`\n")
	b.WriteString("- integration events (publish/subscribe) — `/omnicore:implement`\n")
	b.WriteString("- read models spanning more than this entity — `/omnicore:scaffold-view`\n")
	b.WriteString("- changing this entity once it exists — `/omnicore:evolve-entity`, " +
		"which edits this spec and regenerates. The CODE comes back from the spec; the " +
		"DATABASE never does — the migration a change needs is written by hand, and that " +
		"skill's impact map is what carries it, along with the orphans a shrinking spec " +
		"leaves and everything outside this generator's ownership\n\n")

	if m.Read.ByParams {
		var undeclared []string
		c := m.Read.Controls
		if !c.Fields {
			undeclared = append(undeclared, "`?fields=`")
		}
		if len(c.Search) == 0 {
			undeclared = append(undeclared, "`?search=`")
		}
		if !c.OnlyTotal {
			undeclared = append(undeclared, "`?onlyTotal`")
		}
		if !c.IncludeArchived {
			undeclared = append(undeclared, "`?includeArchived`")
		}
		if len(undeclared) > 0 {
			b.WriteString("Read controls this listing does NOT serve: " +
				strings.Join(undeclared, ", ") + ". That is a contract, not an omission — " +
				"sending one is answered with a typed 400 rather than being ignored.\n\n")
		}
	}

	if len(in.Warnings) > 0 {
		b.WriteString("Warnings raised during generation:\n\n")
		for _, w := range in.Warnings {
			fmt.Fprintf(b, "- %s\n", w)
		}
		b.WriteString("\n")
	}
}

// renderCompat is section E.
func renderCompat(b *strings.Builder, in Input) {
	m := in.Model
	b.WriteString("## Framework compatibility and next steps\n\n")
	fmt.Fprintf(b, "Verdict: **%s**", in.CompatLevel)
	if in.FrameworkPinned != "" {
		fmt.Fprintf(b, " (project pins %s)", in.FrameworkPinned)
	}
	b.WriteString("\n\n")
	if in.CompatMessage != "" {
		fmt.Fprintf(b, "%s\n\n", in.CompatMessage)
	}

	b.WriteString("Verify what was generated:\n\n```\n")
	engines := m.Dialects
	if len(engines) == 0 {
		engines = []string{"postgres"}
	}
	for _, d := range engines {
		fmt.Fprintf(b, "go build -tags '%s' ./...\n", d)
	}
	fmt.Fprintf(b, "go vet -tags '%s' ./...\ngo test -tags '%s' ./... -count=1\n", engines[0], engines[0])
	b.WriteString("```\n\n")
	b.WriteString("A service that builds with a transport tag (kafka, nats) needs it IN " +
		"ADDITION to the engine tag on every command above — an engine tag alone may " +
		"not select a buildable configuration there.\n\n")
	b.WriteString("Then exercise the endpoints end to end — a green build proves the code " +
		"compiles, not that the entity works.\n")
}

func presence(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

func manualFacts(m *ir.Model) []ir.Fact {
	if m.Service == nil {
		return nil
	}
	var out []ir.Fact
	for _, f := range m.Service.Facts {
		if f.Manual {
			out = append(out, f)
		}
	}
	return out
}

func factSignature(f ir.Fact) string {
	var parts []string
	for _, p := range f.Params {
		parts = append(parts, p.Name+" "+p.GoType)
	}
	return strings.Join(parts, ", ")
}

// perChildVerbs is the union of the per-entry verbs the collections mount, in
// the order they are mounted.
//
// It is a union rather than a per-collection list because the sentence it feeds
// is about one FILE: every collection's per-entry tests land in the same
// generated test file. Saying "add, change, remove" there when a collection
// dropped one sends the reader looking for a test that was never written.
// childPermissionCells says, per mounted per-entry verb, which permission its
// route requires AND where that permission came from.
//
// The origin is half the information. "role:update" on an add route means one
// thing if the spec chose it and another if it fell out of the root's update by
// default, and the reviewer's question — is this edge gated on purpose? — can
// only be answered by the second half.
func childPermissionCells(c ir.Child) string {
	var parts []string
	for _, v := range mountedPerChildOps(c) {
		origin := "inherited"
		if c.Declared[v] {
			origin = "declared"
		}
		parts = append(parts, fmt.Sprintf("`%s` → `%s` (%s)", v, c.Permissions[v], origin))
	}
	return strings.Join(parts, "; ")
}

// childPermissionAdvice is the line that turns the cell into a decision.
//
// Inheritance is the default and stays the default, so saying it is present is
// not enough: the reader has to be told that separating them is available and
// what it is for. When the spec HAS separated them, the note flips to what that
// costs at deployment — a permission nobody has been granted refuses a route
// that used to answer.
// childRemovalNote says what taking ONE entry out actually does, and it is here
// because a reviewer reading "archive" reasonably assumes an undo exists. None
// does at this level: there is no per-entry unarchive to mount, and an archived
// entry is not loaded into the aggregate, so nothing can address it afterwards.
// On an authorization model that is the difference between "revoked, recoverable"
// and "revoked, and the way back mints a different grant".
func childRemovalNote(c ir.Child) string {
	if !c.MountsRemove {
		return ""
	}
	if c.ArchivedAt == "" {
		return " Removing ONE entry is a real `DELETE` — the row is gone."
	}
	return " Removing ONE entry ARCHIVES it (204, no body) and is one-way: there is no " +
		"per-entry unarchive, so the only way back is a fresh add, with a NEW entry id."
}

func childPermissionAdvice(c ir.Child) string {
	for _, v := range mountedPerChildOps(c) {
		if c.Declared[v] {
			return "Gated on its own through `children[].permissions`, not by the root's " +
				"update. Grant that permission before the routes go live — a holder of the " +
				"root's update alone now gets a 403 here."
		}
	}
	return "They inherit the root's update permission, which is the default. If changing " +
		"what this collection holds is a different job from editing the record — a role " +
		"assignment, a grant — declare `children[].permissions` so one does not carry the " +
		"other."
}

// mountedPerChildOps lists the verbs of ONE collection, in route order.
func mountedPerChildOps(c ir.Child) []string {
	var out []string
	for _, v := range []struct {
		on   bool
		name string
	}{{c.MountsAdd, "add"}, {c.MountsChange, "change"}, {c.MountsRemove, "remove"}} {
		if v.on {
			out = append(out, v.name)
		}
	}
	return out
}

func perChildVerbs(m *ir.Model) string {
	var add, change, remove bool
	for _, c := range m.Children {
		if !c.PerChild {
			continue
		}
		add = add || c.MountsAdd
		change = change || c.MountsChange
		remove = remove || c.MountsRemove
	}
	var out []string
	for _, v := range []struct {
		on   bool
		name string
	}{{add, "add"}, {change, "change"}, {remove, "remove"}} {
		if v.on {
			out = append(out, v.name)
		}
	}
	return strings.Join(out, ", ")
}

// pairingNote names how entries are matched to their former selves, because it
// is the one assumption of a hoisted rule a reviewer can disagree with: a
// collection replaced wholesale hands back entries with no id, so sameness has
// to be the business identity the spec declared.
func pairingNote(m *ir.Model, hoisted []string) string {
	perChild, replace := false, false
	for _, c := range m.Children {
		for _, cl := range m.Clauses {
			for _, r := range cl.Rules {
				if !r.Hoisted || r.Collection != c.Name {
					continue
				}
				if c.PerChild {
					perChild = true
				} else {
					replace = true
				}
			}
		}
	}
	switch {
	case perChild && replace:
		return " — by id where the collection is edited one entry at a time, by business identity where it is replaced wholesale"
	case perChild:
		return " by id"
	case replace:
		return " by the business identity the spec declared, because a wholesale replace returns entries with no id"
	}
	return ""
}

// names is a set of identifiers read as a lookup. It is a type rather than a
// loop so the call site reads as the question it is asking.
type names []string

func (n names) has(want string) bool {
	for _, got := range n {
		if got == want {
			return true
		}
	}
	return false
}

// archiveScopedRuleIDs names the rules that fire only when the write IS an
// archive — the ones an archiveWhen path silently walks past.
//
// It reads the resolved clauses rather than the spec so it sees the gate the
// emitter actually wrote, which is the thing that decides whether a rule runs.
func archiveScopedRuleIDs(m *ir.Model) []string {
	var out []string
	for _, c := range m.Clauses {
		if c.Gate != "IfArchive" {
			continue
		}
		for _, r := range c.Rules {
			if r.ID != "" {
				out = append(out, r.ID)
			}
		}
	}
	return out
}

// writeOwedComposite prints the exact struct a hand-written COMPOSITE has to be.
//
// It is a different shape from the scalar half above and a stricter contract: a
// scalar is asked for a backing, and this one is asked for a field set, because
// the field NAMES and TYPES are what the command mappers write into. The
// generator folds the flat wire fields into a `vos.<Name>{Part: v, …}` literal,
// so a part renamed or retyped here is a compile error at a call site that names
// neither this report nor the spec.
//
// The two things nothing else would say: no `Value()` — its absence is what
// tells the framework to decompose the value across columns instead of storing
// the rendering in one — and the labelKey tags, whose keys are already in the
// seven catalogs because the parts are declared in the spec. A tag left out is
// not a compile error; it is a column header and a notification label that
// silently fall back to the field's own name.
func writeOwedComposite(b *strings.Builder, vo ir.ValueObject) {
	b.WriteString("  ```go\n")
	fmt.Fprintf(b, "  type %s struct {\n", vo.Name)
	width, typeWidth := 0, 0
	for _, p := range vo.Parts {
		if len(p.Name) > width {
			width = len(p.Name)
		}
		if len(p.GoType) > typeWidth {
			typeWidth = len(p.GoType)
		}
	}
	for _, p := range vo.Parts {
		fmt.Fprintf(b, "  \t%-*s %-*s `labelKey:%q`\n",
			width, p.Name, typeWidth, p.GoType, p.LabelKey)
	}
	b.WriteString("  }\n")
	fmt.Fprintf(b, "  func (v %s) IsValid(fieldName string, ctx *domain.NotificationContext) bool\n",
		vo.Name)
	b.WriteString("  ```\n")
	fmt.Fprintf(b, "  The field names and types are the contract: the command mappers build "+
		"this value with a `vos.%s{…}` literal, part by part. It must NOT declare `Value()` "+
		"— that absence is what tells the framework its value spans SEVERAL columns and has "+
		"to be decomposed rather than stored as one. `IsValid` is the framework's entry "+
		"point, found by TYPE with no registration, and it is where every invariant over "+
		"the parts lives — including the ones this language cannot state. A rendering "+
		"(`String()`, `Format()`) is yours to add under any name but `Value()`.\n", vo.Name)
}
