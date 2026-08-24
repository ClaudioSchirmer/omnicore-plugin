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

// TestSharedHeaderDoesNotDependOnWhoGeneratedIt is the guarantee that makes a
// per-project file idempotent in a project with more than one entity.
//
// The failure it pins down: the vos package comment is emitted by EVERY spec
// that declares a value object, and each one stamped its own name into the
// header. Three entities meant three owners for one file, and generating any of
// them rewrote what the other two had just written — `generate` could never
// leave the working tree clean, and a CI check for "regenerating changes
// nothing" was impossible to satisfy.
func TestSharedHeaderDoesNotDependOnWhoGeneratedIt(t *testing.T) {
	content := []byte("// Package vos …\npackage vos\n")

	byA := ApplyHeader(content, Meta{
		Describes: "the vos package documentation", Spec: "specs/omnicore-gen/tenant.omnicore.yaml",
		Entity: "Tenant", Date: "2026-01-01", Shared: true,
	}, nil)
	byB := ApplyHeader(content, Meta{
		Describes: "the vos package documentation", Spec: "specs/omnicore-gen/role.omnicore.yaml",
		Entity: "Role", Date: "2026-01-01", Shared: true,
	}, byA)

	if string(byA) != string(byB) {
		t.Errorf("two entities produced different bytes for the same shared file:\n--- A\n%s\n--- B\n%s", byA, byB)
	}
	for _, claim := range []string{"entity:", "spec:", "Tenant", "Role"} {
		if strings.Contains(string(byA), claim) {
			t.Errorf("a shared header still attributes itself: found %q in\n%s", claim, byA)
		}
	}
	if !strings.Contains(string(byA), "shared:") {
		t.Errorf("a shared header does not say it is shared:\n%s", byA)
	}
	if intact, tracked := VerifyHeader(byA); !tracked || !intact {
		t.Error("a shared file is still sealed and must verify like any other owned one")
	}
}

// TestSharedIsOnlyTheAttribution — everything else about an owned file stays: it
// is generated, it says DO NOT EDIT, and it carries a date and a checksum.
func TestSharedIsOnlyTheAttribution(t *testing.T) {
	out := string(ApplyHeader([]byte("package vos\n"), Meta{
		Describes: "the vos package documentation", Entity: "Tenant",
		Spec: "specs/omnicore-gen/tenant.omnicore.yaml", Date: "2026-01-01", Shared: true,
	}, nil))
	for _, want := range []string{"DO NOT EDIT", "generated:  2026-01-01", "checksum:   sha256:"} {
		if !strings.Contains(out, want) {
			t.Errorf("a shared header lost %q:\n%s", want, out)
		}
	}
}
