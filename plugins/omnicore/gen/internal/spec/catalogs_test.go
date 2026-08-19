package spec

import (
	"reflect"
	"sort"
	"testing"
)

// TestCatalogSetIsWrittenOnce keeps the two halves of "the seven catalogs" in
// step: CatalogCodes is the ORDER anything listing them iterates in, Texts.Map
// is the mapping from yaml key to catalog code. Neither can be derived from the
// other — a map has no order, a list carries no yaml key — so they are two
// declarations of one fact, which is exactly the shape that drifts.
//
// The drift is expensive and silent. An eighth catalog added to Texts and not
// to CatalogCodes is accepted from the spec, validated against nothing, and
// never emitted: the author writes the translation, the build passes, and the
// language renders raw keys. The reverse — a code in the list with no field
// behind it — reports every notification as missing that translation, forever.
func TestCatalogSetIsWrittenOnce(t *testing.T) {
	var codes []string
	for code := range (Texts{}).Map() {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	want := append([]string(nil), CatalogCodes...)
	sort.Strings(want)

	if !reflect.DeepEqual(codes, want) {
		t.Errorf("the catalog set is declared twice and the two disagree:\n"+
			"  CatalogCodes: %v\n  Texts.Map():  %v\n"+
			"a code in only one of them is a language that either renders raw keys or "+
			"is reported missing on every notification", want, codes)
	}
}

// TestEveryCatalogIsReachableThroughMap pins the half a set comparison cannot
// see: that each code reaches its OWN field. Two fields swapped in Map compare
// equal above and put every Italian string in the Dutch catalog.
func TestEveryCatalogIsReachableThroughMap(t *testing.T) {
	for _, code := range CatalogCodes {
		// The value is the code itself, so whichever field Map reads for it has
		// to be the one that was set.
		var texts Texts
		field := map[string]*string{
			"ptbr": &texts.PTBR, "eng": &texts.ENG, "esp": &texts.ESP, "fra": &texts.FRA,
			"deu": &texts.DEU, "ita": &texts.ITA, "nld": &texts.NLD,
		}[code]
		if field == nil {
			t.Errorf("%q is in CatalogCodes but this test knows no field for it — if the "+
				"catalog is new, add it here too", code)
			continue
		}
		*field = code
		if got := texts.Map()[code]; got != code {
			t.Errorf("Texts.Map reads the wrong field for %q: got %q", code, got)
		}
	}
}
