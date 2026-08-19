package report

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// TestStorageLineTellsTheTruth pins a line that was wrong for every shared-base
// entity ever generated.
//
// "What to check" is the handover: it is where a reviewer confirms the
// decisions the spec made. The storage row said "flat table X" unconditionally,
// so a role over a shared identity was described as a flat one — and the
// reviewer who trusts the summary then verifies the wrong thing, while the one
// who checks and finds it false has no reason to trust the rest of the table.
// It was caught by a reader comparing the report against the generated schemas,
// which is exactly the work the report exists to spare them.
func TestStorageLineTellsTheTruth(t *testing.T) {
	flat := Render(Input{Model: &ir.Model{
		Entity: ir.Names{Pascal: "Sala"}, Table: "salas",
	}, SpecPath: "omnicore-gen/sala.omnicore.yaml"})
	if !strings.Contains(flat, "| Storage | flat table `salas` |") {
		t.Errorf("a flat entity is not described as flat:\n%s", storageLine(flat))
	}

	role := Render(Input{Model: &ir.Model{
		Entity: ir.Names{Pascal: "Professor"}, Table: "professores",
		Base: &ir.Base{Table: "pessoas", Link: "separate-fk", NaturalKey: "Documento"},
	}, SpecPath: "omnicore-gen/professor.omnicore.yaml"})
	line := storageLine(role)
	for _, want := range []string{"role `professores`", "`pessoas`", "separate-fk", "Documento"} {
		if !strings.Contains(line, want) {
			t.Errorf("the storage line of a shared-base role omits %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "flat table") {
		t.Errorf("a role over a shared identity is described as a flat table:\n%s", line)
	}
}

// TestUniqueLineCarriesTheScope pins the other half of the same decision.
//
// Enforcement alone reads as "unique, period". Whether an archived row keeps
// holding the value is the part a reviewer has an opinion about — and the run
// that motivated this had a business rule ("a cancelled listing frees its
// code") that the report was silent about, so nobody could see the two had
// drifted apart until a code could not be reused.
func TestUniqueLineCarriesTheScope(t *testing.T) {
	for scope, want := range map[string]string{
		"active-only": "frees it",
		"all":         "never free again",
		"":            "never free again", // unset means all
	} {
		out := Render(Input{Model: &ir.Model{
			Entity: ir.Names{Pascal: "Anuncio"}, Table: "anuncios",
			Fields: []ir.Field{{
				Name: "Codigo", Column: "codigo",
				Unique: &ir.Unique{
					Enforce: "constraint-only", Notification: "CodigoJaExisteNotification",
					Scope: scope,
				},
			}},
		}, SpecPath: "omnicore-gen/anuncio.omnicore.yaml"})

		line := lineWith(out, "| Unique |")
		if !strings.Contains(line, want) {
			t.Errorf("scope %q does not say what it means for reuse (%q):\n%s", scope, want, line)
		}
	}
}

// TestSecondRemovalDoorNamesWhatItSkips pins the warning for the one write that
// is not the verb it looks like.
//
// A reviewer reads the routes and sees an update and an archive as two doors.
// `delete.archiveWhen` makes one of them also the other, and a write through it
// passes NEITHER gate the entity declares for removal: not the archive
// permission (it arrived on the update's route) and not the archive-scoped
// rules (IfArchive does not fire in ModeUpdate). The row said only the first,
// which is the half that names nothing the reviewer can go and look at — so an
// ownerCheck guarding removal read as covered when it was being walked past.
func TestSecondRemovalDoorNamesWhatItSkips(t *testing.T) {
	model := func(clauses []ir.Clause) *ir.Model {
		return &ir.Model{
			Entity: ir.Names{Pascal: "Teacher"}, Table: "teachers",
			ArchiveWhen: &ir.ArchiveWhen{
				Field: ir.Field{Name: "Status"}, Equals: "terminated", Becomes: "suspended",
			},
			Clauses: clauses,
		}
	}

	guarded := lineWith(Render(Input{
		Model: model([]ir.Clause{{Gate: "IfArchive", Rules: []ir.Rule{
			{ID: "only-the-owner-archives", Kind: "ownerCheck"},
		}}}),
		SpecPath: "omnicore-gen/teacher.omnicore.yaml",
	}), "| Removal, second door |")

	for _, want := range []string{
		"only-the-owner-archives", // the rule that does not run, by name
		"scope: [update]",         // and what to do about it
		"UPDATE's permission",     // the other gate, still said
	} {
		if !strings.Contains(guarded, want) {
			t.Errorf("the second-door row omits %q, so the reviewer cannot act on it:\n%s",
				want, guarded)
		}
	}

	// With nothing scoped to archive there is no rule to name, and inventing one
	// would be worse than the generic sentence: the warning still has to say the
	// path exists, because a rule added LATER lands on the same silence.
	bare := lineWith(Render(Input{
		Model:    model(nil),
		SpecPath: "omnicore-gen/teacher.omnicore.yaml",
	}), "| Removal, second door |")
	if strings.Contains(bare, "never runs on this path") {
		t.Errorf("a rule is named where the entity declares none:\n%s", bare)
	}
	if !strings.Contains(bare, "IfArchive") {
		t.Errorf("the row stops mentioning the path it warns about:\n%s", bare)
	}
}

// TestManualValueObjectStopsBeingOwedOnceWritten pins the state the report was
// blind to.
//
// A `kind: manual` value object is the one outstanding item that stops the
// package compiling, so the first run says exactly that. Every run after the
// author writes it said it AGAIN — which is how a report claims work is
// outstanding that somebody finished three runs ago, the standard the hook-file
// block two sections above already holds itself to. What is owed and what is
// merely worth re-reading are different asks, and the second one is real: the
// generator never opens a file it does not own, so a description the spec
// rewrote to mean something stricter leaves a stale rule behind it.
func TestManualValueObjectStopsBeingOwedOnceWritten(t *testing.T) {
	model := &ir.Model{
		Entity: ir.Names{Pascal: "Student"}, Table: "students",
		ValueObjects: []ir.ValueObject{{
			Name: "NationalID", Kind: "manual", Backing: "string", GoBacking: "string",
			Description: "Valid by its own check digits.",
		}},
	}

	owed := Render(Input{Model: model, SpecPath: "omnicore-gen/student.omnicore.yaml"})
	if !strings.Contains(owed, "### Value objects you write") {
		t.Error("a value object nobody has written is not asked for")
	}
	if !strings.Contains(owed, "does not compile until each one exists") {
		t.Error("the report does not say what an unwritten value object costs")
	}
	if !strings.Contains(owed, "func (v NationalID) IsValid(") {
		t.Error("the shape the implementer has to match is missing, which is the whole " +
			"reason this block carries code at all")
	}

	written := Render(Input{
		Model: model, SpecPath: "omnicore-gen/student.omnicore.yaml",
		ExistingVOs: []string{"NationalID"},
	})
	if strings.Contains(written, "does not compile until each one exists") {
		t.Error("the report still bills a value object that is already on disk as " +
			"blocking the build")
	}
	if !strings.Contains(written, "### Value objects you already wrote") {
		t.Error("a written value object drops off the report entirely — a spec that moves " +
			"under it then leaves a stale rule nobody is told to re-read")
	}
	if !strings.Contains(written, "NationalID") {
		t.Errorf("the written block names no value object:\n%s", written)
	}
	// The backing is a contract on every run, not only on the first: it is what
	// the emitted mappers convert through.
	if !strings.Contains(written, "`.Value()`") {
		t.Error("the written block drops the backing contract")
	}
}

func storageLine(report string) string { return lineWith(report, "| Storage |") }

func lineWith(report, prefix string) string {
	for _, l := range strings.Split(report, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return "(the line is missing entirely)"
}
