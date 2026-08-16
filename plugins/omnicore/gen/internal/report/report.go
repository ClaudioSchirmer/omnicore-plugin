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
	// StaleRegistrations names what a shared registration file declares with
	// content that no longer matches this spec.
	StaleRegistrations []string
	Orphans            []string
	MigrationsKept     []string
	TargetTables       []emit.TargetTable
	CompatLevel        string
	CompatMessage      string
	FrameworkPinned    string
	Warnings           []string
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
		for _, f := range manual {
			fmt.Fprintf(b, "**`%s(%s) %s`**\n\n", f.Name, factSignature(f), f.ReturnType)
			fmt.Fprintf(b, "> %s\n\n", strings.ReplaceAll(f.Description, "\n", " "))
		}
		b.WriteString("The method returns a plain value and no error, so decide what an " +
			"unavailable source means. Failing loudly is the safe default — returning a " +
			"plausible answer skips the rule this exists to enforce.\n\n")
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

	// A field that vanished from the API is the kind of thing a reader notices
	// as a bug rather than as a decision, so the report says it was one.
	if assigned := m.AssignedFields(); len(assigned) > 0 {
		b.WriteString("### Fields the server fills\n\n")
		for _, f := range assigned {
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
		b.WriteString("The three verbs that address ONE entry — add, change, remove — have " +
			"generated tests in `" + fmt.Sprintf("internal/application/commands/%s_commands_test.go",
			m.Entity.Snake) + "`: the entry is applied and projected back, the change keeps its " +
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

	if m.Managed.Archiving {
		fmt.Fprintf(b, "| Removal | archive (reversible) | `DELETE` is a permanent purge and is "+
			"%s. |\n", presence(m.Op("delete") != nil, "also mounted", "not mounted"))
	} else {
		b.WriteString("| Removal | no archive column | Rows are removed permanently or not at all. |\n")
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
		fmt.Fprintf(b, "| Unique | `%s` — scope `%s` (%s) | %s; a duplicate is refused at "+
			"the database and reported as `%s`. |\n",
			f.Name, scope, f.Unique.Enforce, reuse, f.Unique.Notification)
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

	if m.Read.Enabled {
		fmt.Fprintf(b, "| Read backing | %s | %s |\n", m.Read.Backing,
			backingNote(m.Read.Backing))
	}
	b.WriteString("\n")
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

func backingNote(backing string) string {
	if backing == "relational" {
		return "Reads come straight from the tables, so a write is visible immediately."
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
