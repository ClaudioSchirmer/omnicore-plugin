package gofile

import (
	"strings"
	"testing"
)

func metaOn(date string) Meta {
	return Meta{
		Describes: "a test file", Spec: "omnicore-gen/x.omnicore.yaml",
		Entity: "X", Date: date,
	}
}

// TestUnchangedContentKeepsItsDateAcrossDays is the promise the header makes:
// a no-op regeneration is a no-op even when the calendar moved. The failure it
// pins down compared the new content against itself-with-the-old-date, which
// only ever agreed when the two dates were already equal — so the first run on
// a later day rewrote every owned file in the tree.
func TestUnchangedContentKeepsItsDateAcrossDays(t *testing.T) {
	content := []byte("package x\n\nfunc F() int { return 1 }\n")

	day1 := ApplyHeader(content, metaOn("2026-01-01"), nil)
	day2 := ApplyHeader(content, metaOn("2026-08-15"), day1)

	if string(day1) != string(day2) {
		t.Errorf("regenerating unchanged content on a later day changed the bytes:\n--- day1\n%s\n--- day2\n%s",
			day1, day2)
	}
	if !strings.Contains(string(day2), "generated:  2026-01-01") {
		t.Error("the previous date was not kept")
	}
}

// TestChangedContentAdvancesTheDate — the date is a statement about when the
// CONTENT last changed, so a real change must move it.
func TestChangedContentAdvancesTheDate(t *testing.T) {
	day1 := ApplyHeader([]byte("package x\n\nfunc F() int { return 1 }\n"), metaOn("2026-01-01"), nil)
	day2 := ApplyHeader([]byte("package x\n\nfunc F() int { return 2 }\n"), metaOn("2026-08-15"), day1)

	if !strings.Contains(string(day2), "generated:  2026-08-15") {
		t.Error("changed content must carry the new date")
	}
}

// TestUnchangedContentSameDayIsByteIdentical — the case the golden gate always
// exercised (regenerate within one day) must keep holding.
func TestUnchangedContentSameDayIsByteIdentical(t *testing.T) {
	content := []byte("package x\n\nfunc F() int { return 1 }\n")
	run1 := ApplyHeader(content, metaOn("2026-01-01"), nil)
	run2 := ApplyHeader(content, metaOn("2026-01-01"), run1)
	if string(run1) != string(run2) {
		t.Error("same-day regeneration of unchanged content is not byte-identical")
	}
}

// TestSealedFileStillVerifiesAfterDatePreservation — keeping the old date must
// not break the checksum the file carries.
func TestSealedFileStillVerifiesAfterDatePreservation(t *testing.T) {
	content := []byte("package x\n\nfunc F() int { return 1 }\n")
	day1 := ApplyHeader(content, metaOn("2026-01-01"), nil)
	day2 := ApplyHeader(content, metaOn("2026-08-15"), day1)
	okay, tracked := VerifyHeader(day2)
	if !tracked || !okay {
		t.Errorf("the regenerated file no longer verifies (ok=%v tracked=%v)", okay, tracked)
	}
}
