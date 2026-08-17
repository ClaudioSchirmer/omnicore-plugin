package fsplan

import (
	"os"
	"path/filepath"
	"testing"
)

// Pruning is the only thing here that DELETES, so its tests are about what it
// refuses to touch as much as about what it removes.

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func kindOf(files []PruneFile, path string) (PruneKind, bool) {
	for _, f := range files {
		if f.Path == path {
			return f.Kind, true
		}
	}
	return "", false
}

func TestPlanPruneTellsTheFourCasesApart(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/a.go", "generated")
	writeFile(t, root, "internal/edited.go", "generated, then changed by hand")
	writeFile(t, root, "internal/adopted.go", "deliberately different")
	writeFile(t, root, "internal/still.go", "generated")
	writeFile(t, root, "migrations/sqlite/0001_e_manual.up.sql", "CREATE TABLE …")

	lock := &Lock{Version: 1, Entities: map[string]LockEntity{
		"E": {Files: map[string]LockFile{
			"internal/a.go":      {Class: Owned, Hash: Hash([]byte("generated"))},
			"internal/edited.go": {Class: Owned, Hash: Hash([]byte("generated"))},
			"internal/adopted.go": {Class: Owned, Hash: Hash([]byte("generated")),
				AdjustedFor: "v0.52.0", Why: "a framework fix"},
			"internal/gone.go":                       {Class: Owned, Hash: Hash([]byte("generated"))},
			"internal/still.go":                      {Class: Owned, Hash: Hash([]byte("generated"))},
			"migrations/sqlite/0001_e_manual.up.sql": {Class: Owned, Hash: "whatever"},
		}},
	}}

	// The current run still produces still.go, and nothing else that is recorded.
	plan := PlanPrune(root, "E", []File{{Path: "internal/still.go"}}, lock)

	want := map[string]PruneKind{
		"internal/a.go":       PruneDelete,
		"internal/edited.go":  PruneKeep,
		"internal/adopted.go": PruneKeep,
		"internal/gone.go":    PruneForget,
	}
	for path, kind := range want {
		got, ok := kindOf(plan, path)
		if !ok {
			t.Errorf("%s was not planned at all", path)
			continue
		}
		if got != kind {
			t.Errorf("%s planned as %s, want %s", path, got, kind)
		}
	}
	if _, planned := kindOf(plan, "internal/still.go"); planned {
		t.Error("a file the spec still produces was offered for removal")
	}
	// A migration that ran cannot be taken back by deleting the file, so it is
	// never a candidate however orphaned it looks.
	if _, planned := kindOf(plan, "migrations/sqlite/0001_e_manual.up.sql"); planned {
		t.Error("a migration was offered for removal")
	}
}

func TestApplyPruneRemovesOnlyWhatItPlannedToRemove(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/a.go", "generated")
	writeFile(t, root, "internal/edited.go", "changed")
	writeFile(t, root, layoutLockRel(), `{"version":1,"entities":{}}`)

	lock := &Lock{Version: 1, Entities: map[string]LockEntity{
		"E": {Files: map[string]LockFile{
			"internal/a.go":      {Class: Owned, Hash: Hash([]byte("generated"))},
			"internal/edited.go": {Class: Owned, Hash: Hash([]byte("generated"))},
			"internal/gone.go":   {Class: Owned, Hash: Hash([]byte("generated"))},
		}},
	}}
	plan := PlanPrune(root, "E", nil, lock)
	if err := ApplyPrune(root, "E", plan, lock); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "internal/a.go")); !os.IsNotExist(err) {
		t.Error("the unchanged orphan is still on disk")
	}
	if _, err := os.Stat(filepath.Join(root, "internal/edited.go")); err != nil {
		t.Error("the hand-edited file was deleted — it is the author's")
	}

	files := lock.Entities["E"].Files
	if _, still := files["internal/a.go"]; still {
		t.Error("the lock still records a file that was removed")
	}
	if _, still := files["internal/gone.go"]; still {
		t.Error("the lock still records a file that was already gone — this is exactly " +
			"the `is gone` line doctor would keep printing forever")
	}
	if _, still := files["internal/edited.go"]; !still {
		t.Error("the lock forgot a file that is still on disk and still the author's")
	}
}

func TestForgetRegistrationDropsOneNameAndEmptiesThePath(t *testing.T) {
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{
		"E": {Registrations: map[string]map[string]string{
			"internal/application/translations/eng.go": {"AField": "h1", "BField": "h2"},
			"internal/domain/notifications.go":         {"ANotification": "h3"},
		}},
	}}
	lock.ForgetRegistration("E", "internal/application/translations/eng.go", "AField")
	regs := lock.Entities["E"].Registrations
	if _, still := regs["internal/application/translations/eng.go"]["AField"]; still {
		t.Error("the forgotten key is still recorded")
	}
	if _, kept := regs["internal/application/translations/eng.go"]["BField"]; !kept {
		t.Error("a sibling key was dropped with it")
	}

	lock.ForgetRegistration("E", "internal/domain/notifications.go", "ANotification")
	if _, still := regs["internal/domain/notifications.go"]; still {
		t.Error("a path whose last name is gone must not linger as an empty map")
	}
}

// layoutLockRel keeps the test honest about where Save writes, without
// importing the layout package for one string.
func layoutLockRel() string { return LockName }
