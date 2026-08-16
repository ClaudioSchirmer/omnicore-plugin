package emit

import (
	"strings"
	"testing"
)

// The removal half of the registration merge. It is tested apart from the merge
// because its failure mode is the opposite one: a merge that goes wrong writes
// something visible, a removal that goes wrong takes a NEIGHBOUR with it — and
// these files carry other entities' declarations.

const regFile = `package domain

// AlphaNotification is raised when alpha is wrong.
type AlphaNotification struct{}

func (AlphaNotification) Semantic() string { return "validation" }

// BetaNotification is raised when beta is wrong.
type BetaNotification struct{}

// GammaNotification is raised when gamma is wrong.
type GammaNotification struct{}
`

func TestRemoveTypeDeclTakesTheDeclarationItsMethodsAndItsDoc(t *testing.T) {
	out, ok := RemoveTypeDecl(regFile, "AlphaNotification")
	if !ok {
		t.Fatal("the declaration was there and was not reported as removed")
	}
	for _, gone := range []string{"AlphaNotification", "is raised when alpha", "Semantic()"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q survived the removal:\n%s", gone, out)
		}
	}
	// The neighbours are the point: a removal that trims one line too far is a
	// broken shared file for every other entity in the project.
	for _, kept := range []string{"BetaNotification", "is raised when beta",
		"GammaNotification", "is raised when gamma", "package domain"} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q was taken with it:\n%s", kept, out)
		}
	}
}

func TestRemoveTypeDeclLeavesAnUnknownNameAlone(t *testing.T) {
	out, ok := RemoveTypeDecl(regFile, "DeltaNotification")
	if ok {
		t.Error("a name that is not in the file must not report a removal")
	}
	if out != regFile {
		t.Error("the file was rewritten for a declaration it does not hold")
	}
}

func TestRemoveTypeDeclRemovesTheLastOneCleanly(t *testing.T) {
	out, _ := RemoveTypeDecl(regFile, "GammaNotification")
	if strings.HasSuffix(out, "\n\n") {
		t.Errorf("removing the last declaration left a trailing blank line:\n%q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("the file must still end in a newline")
	}
}

const catalogFile = `package translations

func (eng) Translations() map[string]string {
	return map[string]string{
		"AlphaField": "Alpha",
		"BetaField":  "Beta",
		"GammaField": "Gamma",
	}
}
`

func TestRemoveMapEntryTakesOneKeyAndNothingElse(t *testing.T) {
	out, ok := RemoveMapEntry(catalogFile, "BetaField")
	if !ok {
		t.Fatal("the key was there and was not reported as removed")
	}
	if strings.Contains(out, "BetaField") {
		t.Errorf("the key survived:\n%s", out)
	}
	for _, kept := range []string{`"AlphaField": "Alpha"`, `"GammaField": "Gamma"`} {
		if !strings.Contains(out, kept) {
			t.Errorf("%s was taken with it:\n%s", kept, out)
		}
	}
	if strings.Contains(out, "\n\n\t\t") {
		t.Errorf("a blank line was left inside the literal:\n%s", out)
	}
}

func TestRemoveMapEntryLeavesAnUnknownKeyAlone(t *testing.T) {
	out, ok := RemoveMapEntry(catalogFile, "DeltaField")
	if ok || out != catalogFile {
		t.Error("a key that is not in the catalog must not change the file")
	}
}

// RegisteredText is what lets a caller compare the file against the hash the
// lock kept, so it has to read back BOTH shapes — and say so when the name is
// simply not there anymore.
func TestRegisteredTextReadsBothShapes(t *testing.T) {
	if got, ok := RegisteredText(catalogFile, "AlphaField", true); !ok || got != "Alpha" {
		t.Errorf("catalog entry read back as (%q, %v)", got, ok)
	}
	if got, ok := RegisteredText(regFile, "BetaNotification", false); !ok ||
		!strings.Contains(got, "type BetaNotification struct{}") {
		t.Errorf("declaration read back as (%q, %v)", got, ok)
	}
	if _, ok := RegisteredText(regFile, "DeltaNotification", false); ok {
		t.Error("a declaration that is gone must read back as absent")
	}
}

func TestIsCatalogPathTellsTheTwoFileKindsApart(t *testing.T) {
	if !IsCatalogPath("internal/application/translations/eng.go") {
		t.Error("a translation catalog was not recognised as one")
	}
	if IsCatalogPath("internal/domain/notifications.go") {
		t.Error("a notification file was mistaken for a catalog — its entries would be " +
			"looked for as map keys and never found")
	}
}
