package emit

import (
	"strings"
	"testing"
)

// A registration file is shared by every entity of the project and carries no
// header of its own, so there is no whole-file checksum to lean on. The lock
// records one hash per declaration instead, and these tests pin the three
// answers that record produces — because getting any of them wrong is silent:
// too eager discards somebody's edit, too shy leaves a struct that does not
// compile, and the difference is invisible until a regeneration months later.

const notifSkeleton = `package domain

import "github.com/ClaudioSchirmer/omnicore/domain"
`

func declOf(name, body string) TypeDecl {
	return TypeDecl{Name: name, Doc: name + " reaches the caller as 422.", Body: body}
}

var plainDecl = "type LimitNotification struct{ domain.DomainNotificationBase }"

var tvarDecl = "type LimitNotification struct {\n" +
	"\tdomain.DomainNotificationBase\n" +
	"\tMax string `tvar:\"max\"`\n" +
	"}"

// TestFirstRunAppendsAndRecords is the baseline the other two lean on: the hash
// a run records has to be the hash of what it actually wrote, or every later
// comparison reads its own output as a stranger's.
func TestFirstRunAppendsAndRecords(t *testing.T) {
	out, changed, stale, written := MergeTypeDecls(notifSkeleton, []TypeDecl{
		declOf("LimitNotification", plainDecl),
	}, nil)
	if !changed {
		t.Fatal("the declaration was not appended")
	}
	if len(stale) != 0 {
		t.Fatalf("nothing was there to be stale, got %v", stale)
	}
	if !strings.Contains(out, plainDecl) {
		t.Fatal("the appended text is not in the file")
	}
	if written["LimitNotification"] != HashText(plainDecl) {
		t.Fatal("the run must record the hash of what it wrote")
	}
}

// TestSpecMovedAndTheTextIsStillOursReplacesIt is the case that started this:
// a notification gains a tvar, the rules emitted for it write N{Max: "50"}, and
// a struct without the field stops the package compiling.
func TestSpecMovedAndTheTextIsStillOursReplacesIt(t *testing.T) {
	first, _, _, written := MergeTypeDecls(notifSkeleton, []TypeDecl{
		declOf("LimitNotification", plainDecl),
	}, nil)

	out, changed, stale, now := MergeTypeDecls(first, []TypeDecl{
		declOf("LimitNotification", tvarDecl),
	}, written)

	if !changed {
		t.Fatal("a declaration the generator itself wrote must be replaced when the spec moves")
	}
	if len(stale) != 0 {
		t.Fatalf("replacing it is not drift to report, got %v", stale)
	}
	if !strings.Contains(out, `Max string `+"`"+`tvar:"max"`+"`") {
		t.Fatalf("the new field is missing:\n%s", out)
	}
	if strings.Count(out, "type LimitNotification") != 1 {
		t.Fatalf("the declaration was duplicated instead of replaced:\n%s", out)
	}
	if now["LimitNotification"] != HashText(tvarDecl) {
		t.Fatal("the replacement must be recorded, or the next run sees it as a hand edit")
	}
}

// TestAHandEditIsKeptAndReported is the whole reason the hash exists. The
// generator cannot ask the file who wrote it, so a declaration that does not
// match what was recorded is somebody's work and stays.
func TestAHandEditIsKeptAndReported(t *testing.T) {
	first, _, _, written := MergeTypeDecls(notifSkeleton, []TypeDecl{
		declOf("LimitNotification", plainDecl),
	}, nil)

	edited := strings.Replace(first, plainDecl,
		"type LimitNotification struct {\n\tdomain.DomainNotificationBase\n\tContext string\n}", 1)

	out, changed, stale, _ := MergeTypeDecls(edited, []TypeDecl{
		declOf("LimitNotification", tvarDecl),
	}, written)

	if changed {
		t.Fatal("a hand-edited declaration must not be rewritten")
	}
	if !strings.Contains(out, "Context string") {
		t.Fatal("the hand-written field was discarded")
	}
	if len(stale) != 1 || stale[0] != "LimitNotification" {
		t.Fatalf("the edit must be REPORTED, not silently ignored, got %v", stale)
	}
}

// TestADeclarationWithNoRecordIsLeftAlone covers every tree that predates the
// record — and any declaration written by another tool that happens to share a
// name. Not knowing who wrote something is not a licence to overwrite it.
func TestADeclarationWithNoRecordIsLeftAlone(t *testing.T) {
	existing := notifSkeleton + "\n" + plainDecl + "\n"

	out, changed, stale, _ := MergeTypeDecls(existing, []TypeDecl{
		declOf("LimitNotification", tvarDecl),
	}, nil)

	if changed {
		t.Fatal("with no record of having written it, the generator must not replace it")
	}
	if !strings.Contains(out, plainDecl) {
		t.Fatal("the existing declaration was altered")
	}
	if len(stale) != 1 {
		t.Fatalf("it must still be reported as out of step, got %v", stale)
	}
}

// TestOtherEntitiesAreNeverTouched is the invariant the append was protecting
// all along, and replacing in place must not weaken it.
func TestOtherEntitiesAreNeverTouched(t *testing.T) {
	other := "type OtherEntityNotification struct{ domain.DomainNotificationBase }"
	first, _, _, written := MergeTypeDecls(notifSkeleton+"\n"+other+"\n", []TypeDecl{
		declOf("LimitNotification", plainDecl),
	}, nil)

	out, _, _, _ := MergeTypeDecls(first, []TypeDecl{
		declOf("LimitNotification", tvarDecl),
	}, written)

	if !strings.Contains(out, other) {
		t.Fatalf("another entity's declaration was disturbed:\n%s", out)
	}
}

// TestReplacementIsIdempotent guards the normal case: most runs change nothing,
// and a merge that reported a change every time would rewrite a shared file on
// every regeneration for no reason.
func TestReplacementIsIdempotent(t *testing.T) {
	first, _, _, w1 := MergeTypeDecls(notifSkeleton, []TypeDecl{
		declOf("LimitNotification", tvarDecl),
	}, nil)
	second, changed, stale, w2 := MergeTypeDecls(first, []TypeDecl{
		declOf("LimitNotification", tvarDecl),
	}, w1)

	if changed {
		t.Fatal("re-running an unchanged spec must not touch the file")
	}
	if len(stale) != 0 {
		t.Fatalf("an unchanged declaration is not drift, got %v", stale)
	}
	if second != first {
		t.Fatal("the file changed on a no-op run")
	}
	if w2["LimitNotification"] != w1["LimitNotification"] {
		t.Fatal("a confirming run must keep the record, or the next one loses authorship")
	}
}

// TestGofmtAlignmentIsNotAnEdit: adding a longer field makes gofmt realign the
// ones beside it. Comparing raw text would read that as somebody's edit and
// refuse to maintain the declaration ever again.
func TestGofmtAlignmentIsNotAnEdit(t *testing.T) {
	spaced := strings.Replace(tvarDecl, "\tMax string", "\tMax    string", 1)
	if HashText(spaced) != HashText(tvarDecl) {
		t.Fatal("whitespace must not change a declaration's identity")
	}
}

// TestADeclarationWithItsMethodsIsOneUnit is the shape that broke it in a real
// project. A notification whose semantic is not `validation` is emitted as a
// struct FOLLOWED BY its Semantic() method, and the two are one declaration as
// far as this file is concerned. Reading back only the struct meant the text
// never matched the hash recorded for the pair, so every conflict-semantic
// notification read as hand-edited from its second regeneration onwards — six
// of the twelve in the project where it was found.
func TestADeclarationWithItsMethodsIsOneUnit(t *testing.T) {
	withMethod := "type ConflictNotification struct {\n" +
		"\tdomain.DomainNotificationBase\n" +
		"}\n\n" +
		"func (ConflictNotification) Semantic() domain.NotificationSemantic { return domain.SemanticConflict }"

	first, _, _, written := MergeTypeDecls(notifSkeleton, []TypeDecl{
		declOf("ConflictNotification", withMethod),
	}, nil)

	// Re-running the SAME spec must be a no-op — this is what failed.
	second, changed, stale, _ := MergeTypeDecls(first, []TypeDecl{
		declOf("ConflictNotification", withMethod),
	}, written)
	if changed || len(stale) != 0 {
		t.Fatalf("an unchanged conflict notification must be neither rewritten nor reported "+
			"(changed=%v stale=%v)", changed, stale)
	}
	if second != first {
		t.Fatal("the file changed on a no-op run")
	}

	// And when the spec DOES move it, the method moves with the struct.
	moved := strings.Replace(withMethod, "SemanticConflict", "SemanticStateConflict", 1)
	out, changed, stale, _ := MergeTypeDecls(first, []TypeDecl{
		declOf("ConflictNotification", moved),
	}, written)
	if !changed || len(stale) != 0 {
		t.Fatalf("the semantic moved in the spec and should follow (changed=%v stale=%v)",
			changed, stale)
	}
	if strings.Contains(out, "SemanticConflict {") || !strings.Contains(out, "SemanticStateConflict") {
		t.Fatalf("the method was not updated with its type:\n%s", out)
	}
	if strings.Count(out, "func (ConflictNotification) Semantic()") != 1 {
		t.Fatalf("the method was duplicated:\n%s", out)
	}
}

// TestAMethodFurtherDownIsNotSwallowed guards the walk that absorbs methods: it
// stops at the first declaration that is not this type's, so an unrelated
// function between declarations keeps everything after it out of the range.
func TestAMethodFurtherDownIsNotSwallowed(t *testing.T) {
	src := notifSkeleton + "\n" + plainDecl + "\n\n" +
		"type OtherNotification struct{ domain.DomainNotificationBase }\n\n" +
		"func (OtherNotification) Semantic() domain.NotificationSemantic { return domain.SemanticConflict }\n"

	start, end, ok := declRange(src, "LimitNotification")
	if !ok {
		t.Fatal("the declaration was not found")
	}
	if strings.Contains(src[start:end], "OtherNotification") {
		t.Fatalf("the range swallowed the next declaration:\n%s", src[start:end])
	}
}

// TestADeclarationANOTHEREntityWroteIsRecognised is the false positive found in
// a real project: two roles over one identity both expose the same collection,
// so both specs declare the notification about it. The first to run writes it
// and records the hash under its own name; the second used to read text it had
// never written and report a hand edit that never happened — every run, forever.
func TestADeclarationANOTHEREntityWroteIsRecognised(t *testing.T) {
	first, _, _, written := MergeTypeDecls(notifSkeleton, []TypeDecl{
		declOf("SharedNotification", "type SharedNotification struct{ domain.DomainNotificationBase }"),
	}, nil)

	// The SECOND entity: same declaration, nothing of its own recorded yet.
	out, changed, stale, mine := MergeTypeDeclsWith(first, []TypeDecl{
		declOf("SharedNotification", "type SharedNotification struct{ domain.DomainNotificationBase }"),
	}, nil, written)

	if changed {
		t.Fatal("the declaration already says what this spec says — nothing to write")
	}
	if len(stale) != 0 {
		t.Fatalf("a declaration the generator wrote is not a hand edit, got %v", stale)
	}
	if out != first {
		t.Fatal("the file was touched")
	}
	if mine["SharedNotification"] == "" {
		t.Error("the second entity should record it too, so it can maintain it later")
	}
}

// TestTwoSpecsDisagreeingOnOneDeclarationIsReported is the other half: recognised
// as the generator's, but NOT this spec's text. Rewriting it would hand the same
// declaration back and forth between two entities on every regeneration, so it is
// left alone and named.
func TestTwoSpecsDisagreeingOnOneDeclarationIsReported(t *testing.T) {
	theirs := "type SharedNotification struct{ domain.DomainNotificationBase }"
	first, _, _, written := MergeTypeDecls(notifSkeleton, []TypeDecl{
		declOf("SharedNotification", theirs),
	}, nil)

	mineDecl := "type SharedNotification struct {\n\tdomain.DomainNotificationBase\n\tMax string `tvar:\"max\"`\n}"
	out, changed, stale, _ := MergeTypeDeclsWith(first, []TypeDecl{
		declOf("SharedNotification", mineDecl),
	}, nil, written)

	if changed {
		t.Fatal("another entity's declaration must not be rewritten")
	}
	if len(stale) != 1 || !strings.Contains(stale[0], "another entity") {
		t.Fatalf("the disagreement must be reported, and say why, got %v", stale)
	}
	if !strings.Contains(out, theirs) {
		t.Fatal("the existing declaration was altered")
	}
}

// ── the catalogs ─────────────────────────────────────────────────────────────

// TestATranslatorsWordingSurvives is the same mechanism where it matters most:
// improving generated wording is the EXPECTED thing to do to a catalog, and
// reverting it on the next regeneration would be the worst kind of helpful.
func TestATranslatorsWordingSurvives(t *testing.T) {
	src := catalogSkeleton("ptbr", "ptbr", "PTBR", "LangPTBR")
	first, r1 := MergeMapEntries(src,
		[]MapEntry{{Key: "LimitNotification", Value: "Limite atingido."}}, nil)
	written := r1.Written

	improved := strings.Replace(first, "Limite atingido.", "Você atingiu o limite.", 1)

	out, r2 := MergeMapEntries(improved,
		[]MapEntry{{Key: "LimitNotification", Value: "Limite atingido."}}, written)
	changed, stale := r2.Changed, r2.Stale

	if changed {
		t.Fatal("a translator's wording must not be reverted")
	}
	if !strings.Contains(out, "Você atingiu o limite.") {
		t.Fatal("the improved text was lost")
	}
	if len(stale) != 1 {
		t.Fatalf("the divergence must be reported, got %v", stale)
	}
}

// TestSpecTextMovesWhenNobodyTouchedIt is the other half: the text on disk is
// still exactly what the generator wrote, so a spec that reworded it may move it.
func TestSpecTextMovesWhenNobodyTouchedIt(t *testing.T) {
	src := catalogSkeleton("ptbr", "ptbr", "PTBR", "LangPTBR")
	first, r1 := MergeMapEntries(src,
		[]MapEntry{{Key: "LimitNotification", Value: "Limite atingido."}}, nil)
	written := r1.Written

	out, r2 := MergeMapEntries(first,
		[]MapEntry{{Key: "LimitNotification", Value: "Limite de {max} atingido."}}, written)
	changed, stale, now := r2.Changed, r2.Stale, r2.Written

	if !changed {
		t.Fatal("text the generator itself wrote must follow the spec")
	}
	if !strings.Contains(out, "Limite de {max} atingido.") {
		t.Fatalf("the new text is missing:\n%s", out)
	}
	if strings.Contains(out, `"Limite atingido."`) {
		t.Fatalf("the old text was left behind:\n%s", out)
	}
	if len(stale) != 0 {
		t.Fatalf("this is not drift, got %v", stale)
	}
	if now["LimitNotification"] != HashText("Limite de {max} atingido.") {
		t.Fatal("the new text must be recorded")
	}
}

// TestOtherKeysInTheCatalogAreUntouched — a catalog holds every entity's keys.
func TestOtherKeysInTheCatalogAreUntouched(t *testing.T) {
	src := catalogSkeleton("ptbr", "ptbr", "PTBR", "LangPTBR")
	first, r1 := MergeMapEntries(src, []MapEntry{
		{Key: "LimitNotification", Value: "Limite atingido."},
		{Key: "OtherEntityNotification", Value: "Outra coisa."},
	}, nil)
	written := r1.Written

	out, _ := MergeMapEntries(first,
		[]MapEntry{{Key: "LimitNotification", Value: "Novo texto."}}, written)

	if !strings.Contains(out, "Outra coisa.") {
		t.Fatalf("another entity's key was disturbed:\n%s", out)
	}
}
