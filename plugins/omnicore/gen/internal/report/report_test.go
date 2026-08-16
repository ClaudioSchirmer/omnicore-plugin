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

func storageLine(report string) string { return lineWith(report, "| Storage |") }

func lineWith(report, prefix string) string {
	for _, l := range strings.Split(report, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return "(the line is missing entirely)"
}
