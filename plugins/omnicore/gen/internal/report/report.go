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
)

// Input is everything the report describes.
type Input struct {
	Model               *ir.Model
	SpecPath            string
	Decisions           []fsplan.Decision
	MissingTranslations []string
	Orphans             []string
	MigrationsSkipped   bool
	TargetTables        []emit.TargetTable
	AlreadyGenerated    bool
	CompatLevel         string
	CompatMessage       string
	FrameworkPinned     string
	Warnings            []string
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

	if len(m.ManualRules) > 0 {
		empty = false
		fmt.Fprintf(b, "### `internal/domain/%s_rules_manual.go`\n\n", m.Entity.Snake)
		b.WriteString("The spec declared these invariants as ones it could not express. " +
			"The file was created with a stub for each; the code is yours to write, and " +
			"regeneration will never touch it.\n\n")
		for _, r := range m.ManualRules {
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
		fmt.Fprintf(b, "Its tests are yours too — the generator does not know what these rules mean.\n\n")
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

	if in.MigrationsSkipped {
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

	if empty {
		b.WriteString("Nothing. Every rule the spec declared was generated, and every " +
			"translation was supplied.\n\n")
	}
}

// renderMigrationHandoff is the whole point of skipping the DDL: the person or
// agent who now owns it has to be told plainly, with enough to act on.
//
// It sits in "what still needs implementing" rather than in a footnote because
// it is the one gap here that a database will not forgive.
func renderMigrationHandoff(b *strings.Builder, in Input) {
	m := in.Model
	b.WriteString("### The migration — yours to write\n\n")

	if in.AlreadyGenerated {
		b.WriteString("**No DDL was written.** This entity already existed, so this run was " +
			"not a creation — and creating is the only thing this generator does to a " +
			"database.\n\n")
		b.WriteString("It does not evolve a schema, and that is a deliberate limit rather " +
			"than a missing feature. It cannot see your staging or production databases, " +
			"so it cannot know whether the original migration already ran. Rewriting one " +
			"that did would leave the tables as they were, the file claiming otherwise, " +
			"and the framework's tracking table reporting nothing pending — a service " +
			"that boots green and fails on the first query touching the change.\n\n")
		b.WriteString("**So writing the `ALTER` is yours.** The Go code below was " +
			"regenerated and already expects the shape in the table that follows, so until " +
			"the database matches it, the two disagree.\n\n")
	} else {
		b.WriteString("**No DDL was written**, because this run was told not to " +
			"(`--migrations=no`). The Go code expects the shape below.\n\n")
	}

	fmt.Fprintf(b, "Target shape for `%s`:\n\n", m.Table)
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
		b.WriteString("\n")
	}

	dialects := strings.Join(m.Dialects, ", ")
	fmt.Fprintf(b, "Write one migration pair per dialect this service targets (%s), "+
		"numbered after the highest existing one. Every `.up.sql` needs its `.down.sql` "+
		"or the service refuses to boot.\n\n", dialects)

	b.WriteString("Two things worth being deliberate about, because they are where data " +
		"is lost: adding a NOT NULL column to a table that already has rows fails unless " +
		"it carries a default, and a rename done as drop-then-add takes the data with " +
		"it.\n\n")

	b.WriteString("If this entity has not shipped anywhere yet and you would rather the " +
		"generator just rewrite the original migration, re-run it with " +
		"`--migrations=yes`. That REWRITES the existing file in place, so it is only " +
		"safe while you are still the only one who has run it.\n\n")
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
		fmt.Fprintf(b, "%s raised by a child's rule, so declared in "+
			"`internal/domain/aggregatevos` rather than in `internal/domain`: the child's "+
			"type lives there, and `domain` imports `aggregatevos` — holding it in `domain` "+
			"would be an import cycle, not a style choice.\n\n",
			strings.Join(moved, ", "))
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

	fmt.Fprintf(b, "| Storage | flat table `%s` | A field group that should be shared with another "+
		"role later would need a real migration to extract. |\n", m.Table)

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
		fmt.Fprintf(b, "| Unique | `%s` (%s) | A duplicate is refused at the database and "+
			"reported as `%s`. |\n", f.Name, f.Unique.Enforce, f.Unique.Notification)
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
			note := ""
			if fsplan.IsAppliedMigration(o) {
				note = " — **not deleted**: if this migration ran in any environment, " +
					"removing the file does not undo it. Write a new migration that drops what it created."
			}
			fmt.Fprintf(b, "- `%s`%s\n", o, note)
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
	b.WriteString("- changing this entity once it exists — still a manual edit; " +
		"the generator creates, it does not evolve\n\n")

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
