package fsplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/gofile"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ownedFile builds a file the way the generator really does: sealed with the
// header whose checksum is what the plan reads. Testing against unsealed bytes
// would exercise a shape that never reaches disk.
func ownedFile(path, content string) File {
	sealed := gofile.ApplyHeader([]byte(content), gofile.Meta{
		Describes: "test file", Spec: "specs/x.omnicore.yaml",
		Entity: "X", Date: "2026-01-01",
	}, nil)
	return File{Path: path, Class: Owned, Content: sealed}
}

// writeSealed puts a sealed file on disk, as a previous run would have.
func writeSealed(t *testing.T, root, rel, content string) {
	t.Helper()
	f := ownedFile(rel, content)
	write(t, root, rel, string(f.Content))
}

// TestRefusesHandEditedFile is the core promise: a file the author changed is
// never overwritten.
func TestRefusesHandEditedFile(t *testing.T) {
	root := t.TempDir()
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{}}

	first := []File{ownedFile("internal/domain/student.go", "package domain // v1\n")}
	d, _ := Plan(root, "Student", first, lock, nil)
	if d[0].Action != Create {
		t.Fatalf("first run should create, got %s", d[0].Action)
	}
	if err := Apply(root, "Student", "s.yaml", "h", "v0.47.2", nil, ViewState{}, d, lock); err != nil {
		t.Fatal(err)
	}

	// A hand edit: the bytes change, the checksum in the header no longer covers
	// them, and that is what the plan detects.
	write(t, root, "internal/domain/student.go", "package domain // HAND EDITED\n")

	second := []File{ownedFile("internal/domain/student.go", "package domain // v2\n")}
	d2, _ := Plan(root, "Student", second, lock, nil)
	if d2[0].Action != RefusedEdited {
		t.Fatalf("a hand-edited file must be refused, got %s", d2[0].Action)
	}
	if err := Apply(root, "Student", "s.yaml", "h", "v0.47.2", nil, ViewState{}, d2, lock); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "internal/domain/student.go"))
	if string(got) != "package domain // HAND EDITED\n" {
		t.Errorf("the hand edit was clobbered: %q", got)
	}
	// The refusal must SURVIVE: a third run has to refuse again. Recording the
	// on-disk hash here would make the next comparison match and the file the
	// generator just declined to touch would be overwritten.
	d3, _ := Plan(root, "Student", second, lock, nil)
	if d3[0].Action != RefusedEdited {
		t.Fatalf("a refusal must persist across runs, got %s", d3[0].Action)
	}
	_ = Apply(root, "Student", "s.yaml", "h", "v0.47.2", nil, ViewState{}, d3, lock)
	got, _ = os.ReadFile(filepath.Join(root, "internal/domain/student.go"))
	if string(got) != "package domain // HAND EDITED\n" {
		t.Errorf("the hand edit was clobbered on the third run: %q", got)
	}
}

func TestForceOverwritesOnlyNamedPath(t *testing.T) {
	root := t.TempDir()
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{}}
	files := []File{
		ownedFile("a.go", "package a // v1\n"),
		ownedFile("b.go", "package b // v1\n"),
	}
	d, _ := Plan(root, "E", files, lock, nil)
	_ = Apply(root, "E", "s.yaml", "h", "v1", nil, ViewState{}, d, lock)
	write(t, root, "a.go", "package a // edited\n")
	write(t, root, "b.go", "package b // edited\n")

	next := []File{ownedFile("a.go", "package a // v2\n"), ownedFile("b.go", "package b // v2\n")}
	d2, _ := Plan(root, "E", next, lock, map[string]bool{"a.go": true})

	byPath := map[string]Action{}
	for _, dec := range d2 {
		byPath[dec.File.Path] = dec.Action
	}
	if byPath["a.go"] != Update {
		t.Errorf("the forced path should be overwritten, got %s", byPath["a.go"])
	}
	if byPath["b.go"] != RefusedEdited {
		t.Errorf("force must be scoped — b.go should still be refused, got %s", byPath["b.go"])
	}
}

// TestHookFileIsWrittenOnceAndKept covers the escape hatch: created when
// missing, then owned by the author forever.
func TestHookFileIsWrittenOnceAndKept(t *testing.T) {
	root := t.TempDir()
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{}}
	hook := File{Path: "internal/domain/student_rules_manual.go", Class: Hook,
		Content: []byte("package domain\n\nfunc (s *Student) customRules() {}\n")}

	d, _ := Plan(root, "Student", []File{hook}, lock, nil)
	if d[0].Action != Create {
		t.Fatalf("a missing hook file should be created, got %s", d[0].Action)
	}
	_ = Apply(root, "Student", "s.yaml", "h", "v1", nil, ViewState{}, d, lock)

	write(t, root, "internal/domain/student_rules_manual.go", "package domain\n// real rules\n")
	d2, _ := Plan(root, "Student", []File{hook}, lock, nil)
	if d2[0].Action != KeptHook {
		t.Fatalf("an existing hook file must be kept, got %s", d2[0].Action)
	}
	if !d2[0].Expected() {
		t.Error("keeping a hook file is expected, not a failure to report")
	}
	_ = Apply(root, "Student", "s.yaml", "h", "v1", nil, ViewState{}, d2, lock)
	got, _ := os.ReadFile(filepath.Join(root, "internal/domain/student_rules_manual.go"))
	if string(got) != "package domain\n// real rules\n" {
		t.Errorf("the hook file was overwritten: %q", got)
	}
	if _, hashed := lock.Entities["Student"].Files["internal/domain/student_rules_manual.go"]; hashed {
		t.Error("a hook file must never be hashed — hashing it would make edits look like drift")
	}
}

// TestRegenIsIdempotent: running twice with the same spec changes nothing.
func TestRegenIsIdempotent(t *testing.T) {
	root := t.TempDir()
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{}}
	files := []File{ownedFile("x.go", "package x\n")}

	d, _ := Plan(root, "E", files, lock, nil)
	_ = Apply(root, "E", "s.yaml", "h", "v1", nil, ViewState{}, d, lock)

	d2, _ := Plan(root, "E", files, lock, nil)
	if d2[0].Action != Unchanged {
		t.Fatalf("an unchanged regeneration should be a no-op, got %s", d2[0].Action)
	}
}

// TestCRLFDoesNotLookLikeAnEdit pins the mass-refusal failure: a Windows
// checkout would otherwise mark the entire tree as hand-written.
func TestCRLFDoesNotLookLikeAnEdit(t *testing.T) {
	root := t.TempDir()
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{}}
	files := []File{ownedFile("x.go", "package x\nfunc A() {}\n")}
	d, _ := Plan(root, "E", files, lock, nil)
	_ = Apply(root, "E", "s.yaml", "h", "v1", nil, ViewState{}, d, lock)

	// The same file with Windows line endings: different bytes, same content.
	sealed := ownedFile("x.go", "package x\nfunc A() {}\n")
	write(t, root, "x.go", strings.ReplaceAll(string(sealed.Content), "\n", "\r\n"))
	d2, _ := Plan(root, "E", files, lock, nil)
	if d2[0].Action == RefusedEdited {
		t.Error("a CRLF checkout must not read as a hand edit")
	}
}

func TestAdoptPreservesFixAcrossRegen(t *testing.T) {
	root := t.TempDir()
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{}}
	files := []File{ownedFile("x.go", "package x\n")}
	d, _ := Plan(root, "E", files, lock, nil)
	_ = Apply(root, "E", "s.yaml", "h", "v0.47.2", nil, ViewState{}, d, lock)

	write(t, root, "x.go", "package x\n// fixed for a newer framework\n")
	if err := Adopt(root, "E", "x.go", "v0.49.0", "the framework moved and one call had to change", lock); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	d2, _ := Plan(root, "E", files, lock, nil)
	if d2[0].Action != KeptHook {
		t.Fatalf("an adopted fix must survive regeneration, got %s", d2[0].Action)
	}
	if !contains(d2[0].Reason, "v0.49.0") {
		t.Errorf("the reason should name the framework version, got %q", d2[0].Reason)
	}
}

func TestAdoptRejectsUnknownFile(t *testing.T) {
	root := t.TempDir()
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{}}
	if err := Adopt(root, "E", "nope.go", "v1", "adopted in a test", lock); err == nil {
		t.Error("adopting a file the generator never wrote must fail")
	}
}

func TestOrphansListsWhatTheSpecStoppedProducing(t *testing.T) {
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{
		"E": {Files: map[string]LockFile{
			"internal/domain/e.go":    {Class: Owned},
			"internal/domain/gone.go": {Class: Owned},
		}},
	}}
	orph := Orphans("E", []File{ownedFile("internal/domain/e.go", "")}, lock)
	if len(orph) != 1 || orph[0] != "internal/domain/gone.go" {
		t.Fatalf("unexpected orphans: %v", orph)
	}
	if !IsMigration("migrations/postgres/0003_e_manual.up.sql") {
		t.Error("a path under migrations/ ending in .sql is a migration")
	}
	if IsMigration("internal/domain/e.go") {
		t.Error("a Go file is not a migration")
	}
}

// hookFile builds a file the generator writes once, the way migrations are now
// planned.
func hookFile(path, content string) File {
	return File{Path: path, Class: Hook, Content: []byte(content)}
}

// TestMigrationIsWrittenOnceAndNeverAgain pins the whole posture: the SQL is
// created on the first run, kept untouched on every later one, and NEVER
// recorded in the lock — so it can no longer be reported as an orphan the way
// it was when a regeneration silently stopped emitting it.
func TestMigrationIsWrittenOnceAndNeverAgain(t *testing.T) {
	root := t.TempDir()
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{}}
	path := "migrations/sqlite/0001_e_manual.up.sql"

	files := []File{hookFile(path, "CREATE TABLE es (id BLOB);\n")}
	d, _ := Plan(root, "E", files, lock, nil)
	if d[0].Action != Create {
		t.Fatalf("first run must create the migration, got %s", d[0].Action)
	}
	if err := Apply(root, "E", "s.yaml", "h", "v0.49.0", map[string]int{"sqlite": 1}, ViewState{}, d, lock); err != nil {
		t.Fatal(err)
	}
	if _, recorded := lock.Entities["E"].Files[path]; recorded {
		t.Error("a migration must not enter the lock — it is the author's from the moment it exists")
	}

	// A second run resolving a DIFFERENT body must still leave the file alone:
	// the one on disk may already have run somewhere.
	changed := []File{hookFile(path, "CREATE TABLE es (id BLOB, extra INTEGER);\n")}
	d2, _ := Plan(root, "E", changed, lock, nil)
	if d2[0].Action != KeptHook {
		t.Fatalf("a migration that exists must be kept, got %s", d2[0].Action)
	}
	if err := Apply(root, "E", "s.yaml", "h", "v0.49.0", map[string]int{"sqlite": 1}, ViewState{}, d2, lock); err != nil {
		t.Fatal(err)
	}
	on, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(on), "extra") {
		t.Error("the migration on disk was rewritten — its effect outlives the file, so it must not be")
	}
	if orph := Orphans("E", changed, lock); len(orph) != 0 {
		t.Errorf("a migration can never be an orphan, got %v", orph)
	}
	if !strings.Contains(d2[0].Reason, "never rewritten") {
		t.Errorf("the reason must explain the posture, got %q", d2[0].Reason)
	}
}

// TestApplyDropsARecordWhenAFileChangesClass covers the transition migrations
// themselves went through. A path recorded as owned by an older build, now
// planned as a hook, must LOSE its record: leaving it would keep `doctor`
// verifying a checksum on a file the author is now invited to edit, and report
// every edit as drift.
func TestApplyDropsARecordWhenAFileChangesClass(t *testing.T) {
	root := t.TempDir()
	path := "migrations/sqlite/0001_e.up.sql"
	writeSealed(t, root, path, "CREATE TABLE es (id BLOB);\n")
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{
		"E": {Files: map[string]LockFile{path: {Class: Owned, Hash: "stale"}}},
	}}

	d, _ := Plan(root, "E", []File{hookFile(path, "whatever")}, lock, nil)
	if d[0].Action != KeptHook {
		t.Fatalf("expected the existing file to be kept, got %s", d[0].Action)
	}
	if err := Apply(root, "E", "s.yaml", "h", "v0.49.0", nil, ViewState{}, d, lock); err != nil {
		t.Fatal(err)
	}
	if _, recorded := lock.Entities["E"].Files[path]; recorded {
		t.Error("a reclassified file must not stay in the lock as owned")
	}
}

// TestApplyKeepsAnAdoptedRecord guards the one KeptHook that MUST survive: an
// adopted owned file, whose record is the adoption itself.
func TestApplyKeepsAnAdoptedRecord(t *testing.T) {
	root := t.TempDir()
	path := "internal/domain/e.go"
	writeSealed(t, root, path, "package domain\n")
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{
		"E": {Files: map[string]LockFile{
			path: {Class: Owned, Hash: "h", AdjustedFor: "v0.49.0", Why: "a framework fix"},
		}},
	}}

	d, _ := Plan(root, "E", []File{ownedFile(path, "package domain // regenerated\n")}, lock, nil)
	if d[0].Action != KeptHook {
		t.Fatalf("an adopted file must be kept, got %s", d[0].Action)
	}
	if err := Apply(root, "E", "s.yaml", "h", "v0.49.0", nil, ViewState{}, d, lock); err != nil {
		t.Fatal(err)
	}
	rec, recorded := lock.Entities["E"].Files[path]
	if !recorded || rec.AdjustedFor != "v0.49.0" {
		t.Error("the adoption record must survive — it is the only thing that makes the edit deliberate")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
