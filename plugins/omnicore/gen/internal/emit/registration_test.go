package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/gofile"
)

// TestMergeIntoEmptyCatalog pins the case a project with no entities yet hits
// FIRST: an empty catalog writes its whole literal on one line, so inserting at
// the start of that line puts the entries before the return and the file stops
// parsing.
//
// It went unnoticed for a while because the gate ran against a service whose
// catalogs were already populated — the create-from-nothing path was never
// exercised. That is the whole reason the host is vendored rather than borrowed.
func TestMergeIntoEmptyCatalog(t *testing.T) {
	src := catalogSkeleton("ptbr", "ptbr", "PTBR", "LangPTBR")
	out, changed, added, _, _ := MergeMapEntries(src, []MapEntry{
		{Key: "SomeNotification", Value: "Alguma mensagem."},
	}, nil)
	if !changed || len(added) != 1 {
		t.Fatalf("the entry was not inserted (changed=%v added=%v)", changed, added)
	}
	if _, err := gofile.Finalize([]byte(out)); err != nil {
		t.Fatalf("the merged catalog does not parse: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"SomeNotification": "Alguma mensagem."`) {
		t.Errorf("the entry is missing:\n%s", out)
	}
}

func TestMergeIntoPopulatedCatalog(t *testing.T) {
	src := catalogSkeleton("eng", "eng", "ENG", "LangENG")
	src, _, _, _, _ = MergeMapEntries(src, []MapEntry{{Key: "First", Value: "one"}}, nil)
	out, changed, added, _, _ := MergeMapEntries(src, []MapEntry{{Key: "Second", Value: "two"}}, nil)
	if !changed || len(added) != 1 {
		t.Fatalf("the second entry was not inserted")
	}
	if _, err := gofile.Finalize([]byte(out)); err != nil {
		t.Fatalf("the merged catalog does not parse: %v\n%s", err, out)
	}
	if !strings.Contains(out, "First") || !strings.Contains(out, "Second") {
		t.Errorf("both entries should survive:\n%s", out)
	}
}

// TestMergeIsIdempotent: the second run is the normal case, and a merge that
// appended blindly would duplicate every key.
func TestMergeIsIdempotent(t *testing.T) {
	src := catalogSkeleton("esp", "esp", "ESP", "LangES")
	entries := []MapEntry{{Key: "Key", Value: "valor"}}
	once, _, _, _, _ := MergeMapEntries(src, entries, nil)
	twice, changed, _, _, _ := MergeMapEntries(once, entries, nil)
	if changed {
		t.Error("re-merging the same key should change nothing")
	}
	if once != twice {
		t.Error("re-merging altered the file")
	}
}

// TestMergeNeverOverwritesExistingWording: a translator may well have improved
// a generated string, and silently reverting that is the worst kind of help.
func TestMergeNeverOverwritesExistingWording(t *testing.T) {
	src := catalogSkeleton("fra", "fra", "FRA", "LangFR")
	src, _, _, _, _ = MergeMapEntries(src, []MapEntry{{Key: "Msg", Value: "generated"}}, nil)
	src = strings.Replace(src, `"generated"`, `"rewritten by a human"`, 1)

	out, changed, _, _, _ := MergeMapEntries(src, []MapEntry{{Key: "Msg", Value: "generated"}}, nil)
	if changed {
		t.Error("an existing key must be left alone")
	}
	if !strings.Contains(out, "rewritten by a human") {
		t.Errorf("the human wording was lost:\n%s", out)
	}
}

// TestEveryCatalogSkeletonCompiles guards the language constants.
//
// They are NOT uniform with the file names — the framework spells them LangES,
// LangFR, LangDE, LangIT — so deriving one from the other by rule produces four
// identifiers that do not exist. Nothing catches that until a project without
// catalogs is generated into.
func TestEveryCatalogSkeletonCompiles(t *testing.T) {
	for _, c := range catalogs {
		out, err := gofile.Finalize([]byte(catalogSkeleton(c.Code, c.Type, c.Ctor, c.LangConst)))
		if err != nil {
			t.Errorf("the %s skeleton does not parse: %v", c.Code, err)
			continue
		}
		if !strings.Contains(string(out), "configuration."+c.LangConst) {
			t.Errorf("the %s skeleton lost its language constant", c.Code)
		}
	}
	known := map[string]bool{
		"LangPTBR": true, "LangENG": true, "LangES": true,
		"LangFR": true, "LangDE": true, "LangIT": true, "LangNL": true,
	}
	for _, c := range catalogs {
		if !known[c.LangConst] {
			t.Errorf("%q is not a language constant the framework declares", c.LangConst)
		}
	}
}
