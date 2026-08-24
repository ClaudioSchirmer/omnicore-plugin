package emit

import (
	"sort"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// A LABEL is a field's short human name; a DESCRIPTION is a sentence about what
// the field means. They have different audiences, different lengths and
// different homes — the description becomes the column COMMENT, the label
// reaches `fieldLabel` in a 422 payload and the header of a CSV/XLSX column.
//
// The generator used to seed the label from the description, in the single
// catalog matching the spec's declared `language:`. A folded description is one
// line, so the whole paragraph became the label, and a real validation payload
// came back carrying it. The other six catalogs fell back to the field name and
// were right — the one language that got special treatment was the one that
// came out wrong.
//
// These two tests are what keeps that dead: the first over whatever the matrix
// happens to contain, the second on the function itself.

// TestNoCatalogLabelIsASentence checks the PROPERTY rather than the path: no
// matter which emitter fills a label, what lands in the catalog has to read as
// a name. A description leaking in by some other route fails here too.
func TestNoCatalogLabelIsASentence(t *testing.T) {
	for name, m := range matrixModels(t) {
		t.Run(name, func(t *testing.T) {
			labels := map[string]bool{}
			for _, f := range labelledFields(m) {
				labels[f.LabelKey] = true
			}
			var bad []string
			for lang, entries := range catalogEntries(m) {
				for _, e := range entries {
					if !labels[e.Key] {
						continue // a notification's message IS a sentence
					}
					if len(e.Value) > 48 || strings.Contains(e.Value, ". ") {
						bad = append(bad, lang+"."+e.Key+" = "+e.Value)
					}
				}
			}
			sort.Strings(bad)
			if len(bad) > 0 {
				t.Errorf("these catalog entries are field LABELS and read as prose — a "+
					"validation payload puts each of them in fieldLabel and an export puts "+
					"it in a column header:\n  %s", strings.Join(bad, "\n  "))
			}
		})
	}
}

// TestLabelComesFromTextNotDescription pins the two answers labelText may give.
func TestLabelComesFromTextNotDescription(t *testing.T) {
	f := ir.Field{
		Name:        "TenantID",
		LabelKey:    "TenantTenantIDField",
		Description: "Public key of the tenant, derived from the workspace handle; it reaches URLs and logs.",
		Text:        map[string]string{"eng": "Tenant ID"},
	}
	if got := labelText(f, "eng"); got != "Tenant ID" {
		t.Errorf("a declared label is what the catalog gets; got %q", got)
	}
	// Every catalog the spec left out falls back to the field name — a
	// placeholder a translator can find, never the description.
	if got := labelText(f, "ptbr"); got != "Tenant ID" {
		t.Errorf("an undeclared catalog falls back to the spaced field name; got %q", got)
	}
	bare := ir.Field{Name: "Workspace", Description: "Immutable handle of the tenant."}
	if got := labelText(bare, "eng"); got != "Workspace" {
		t.Errorf("with no text at all the label is the field name; got %q", got)
	}
}

// An ENUM MEMBER's text is a label too, and it follows the label discipline
// rather than the notification one: an empty catalog falls back to the member's
// own name, never to a marked placeholder. "TODO(FRA): Aberto" in a status
// column is what the end user reads; "Aberto" in a French screen is a word in
// the wrong language, which is worse than nothing only in the report.
func TestEnumMemberTextFallsBackToTheMemberName(t *testing.T) {
	mem := ir.EnumMember{
		Name:           "EmAndamento",
		DescriptionKey: "SituacaoCurso.em_andamento",
		Text:           map[string]string{"ptbr": "Em andamento"},
	}
	if got := memberText(mem, "ptbr"); got != "Em andamento" {
		t.Errorf("a declared text is what the catalog gets; got %q", got)
	}
	if got := memberText(mem, "fra"); got != "Em Andamento" {
		t.Errorf("an undeclared catalog falls back to the spaced member name; got %q", got)
	}
	if got := memberText(mem, "fra"); got == mem.DescriptionKey {
		t.Error("the fallback is never the key — that is exactly what the framework " +
			"already answers when no entry exists, so the entry would buy nothing")
	}
}

// The report is the other half of closing the silence: an entry filled from the
// member name is a real string, so nothing in the emitted code looks wrong and
// only the hand-off can say the screen is in one language.
func TestUntranslatedEnumValuesNamesTheMissingCatalogs(t *testing.T) {
	m := &ir.Model{ValueObjects: []ir.ValueObject{{
		Name: "SituacaoCurso", Kind: "enum",
		Members: []ir.EnumMember{
			{Name: "Aberto", DescriptionKey: "SituacaoCurso.aberto"},
			{Name: "Fechado", DescriptionKey: "SituacaoCurso.fechado", Missing: []string{"fra", "deu"}},
		},
	}}}
	got := UntranslatedEnumValues(m)
	want := []string{"SituacaoCurso.fechado / DEU", "SituacaoCurso.fechado / FRA"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("UntranslatedEnumValues = %v, want %v", got, want)
	}
}
