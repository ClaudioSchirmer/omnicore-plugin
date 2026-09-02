package fsplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/layout"
)

// The lock is the generator's whole memory. Everything it refuses to overwrite,
// every migration number it does not allocate twice, and every prune verdict
// come from what it says — so the ways it can be READ wrong are the ways a
// project gets damaged by a tool that believes it is being careful.

// TestNoLockIsAFirstRunNotAFailure. A project that has never been generated has
// no lock, and reading that as an error would make `init`, `check` and `doctor`
// fail on exactly the project they exist to start.
func TestNoLockIsAFirstRunNotAFailure(t *testing.T) {
	lock, err := LoadLock(t.TempDir())
	if err != nil {
		t.Fatalf("a missing lock must read as an empty one: %v", err)
	}
	if lock.Version != 1 || lock.Entities == nil {
		t.Fatalf("the empty lock must be usable without a nil check: %+v", lock)
	}
	// Usable means writable: the first run records into it immediately.
	lock.Entities["Papel"] = LockEntity{Files: map[string]LockFile{}}
}

// TestACorruptLockIsRefusedRatherThanReset is the difference between a bad
// morning and a lost tree. An unreadable lock read as "empty" makes the next
// run believe it wrote nothing, and every generated file in the project then
// looks hand-written — the whole tree refused at once, with --force as the only
// apparent way out.
func TestACorruptLockIsRefusedRatherThanReset(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(layout.DirIn(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.LockIn(root), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock, err := LoadLock(root)
	if err == nil {
		t.Fatalf("a corrupt lock must be an error, got %+v", lock)
	}
	if !strings.Contains(err.Error(), "version control") {
		t.Errorf("the error must say what to do instead of deleting it, got: %v", err)
	}
}

// TestSaveThenLoadKeepsEverythingTheNextRunNeeds. Each of these fields answers
// a question a later run cannot re-derive: which files are the generator's,
// which migration numbers are taken, what the view projected, and what this
// entity wrote into shared files.
func TestSaveThenLoadKeepsEverythingTheNextRunNeeds(t *testing.T) {
	root := t.TempDir()
	want := &Lock{Version: 1, Entities: map[string]LockEntity{
		"Papel": {
			Spec:        layout.SpecRel("papel"),
			Files:       map[string]LockFile{"internal/domain/papel.go": {Class: Owned, Hash: "abc"}},
			Ordinals:    map[string]int{"postgres": 7, "sqlite": 3},
			ViewShape:   "shape-hash",
			ViewVersion: 2,
			Registrations: map[string]map[string]string{
				"internal/domain/notifications.go": {"PapelTakenNotification": "decl-hash"},
			},
		},
	}}
	if err := want.Save(root); err != nil {
		t.Fatalf("saving: %v", err)
	}
	got, err := LoadLock(root)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	e := got.Entities["Papel"]
	if e.Files["internal/domain/papel.go"].Hash != "abc" {
		t.Errorf("the file hash did not survive the round trip: %+v", e.Files)
	}
	if got.OrdinalsOf("Papel")["postgres"] != 7 || got.OrdinalsOf("Papel")["sqlite"] != 3 {
		t.Errorf("a lost ordinal means a DUPLICATE migration next run: %+v", got.OrdinalsOf("Papel"))
	}
	if e.ViewShape != "shape-hash" || e.ViewVersion != 2 {
		t.Errorf("the view pair did not survive: shape=%q version=%d", e.ViewShape, e.ViewVersion)
	}
	if got.RegistrationsOf("Papel")["internal/domain/notifications.go"]["PapelTakenNotification"] != "decl-hash" {
		t.Errorf("a lost registration is a declaration nothing can ever prune: %+v", got.RegistrationsOf("Papel"))
	}
	// And it is where the convention says, not loose at the root.
	if _, err := os.Stat(filepath.Join(root, "specs", "omnicore-gen", "lock.json")); err != nil {
		t.Errorf("the lock is not where every other tool looks for it: %v", err)
	}
}

// TestOrdinalsOfAnUnknownEntityIsEmpty. Asked about an entity nobody generated
// yet, the answer is "no numbers taken" — a nil map reads that way, and a panic
// would make the first migration of every new entity a crash.
func TestOrdinalsOfAnUnknownEntityIsEmpty(t *testing.T) {
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{}}
	if n := lock.OrdinalsOf("Novo")["postgres"]; n != 0 {
		t.Errorf("an entity with no history owns no ordinal, got %d", n)
	}
}

// TestRegistrationsExceptIsWhatKeepsOneEntityOutOfAnothersDeclarations. The
// prune verdict "another entity declares it too" is read from here, so an
// off-by-one entity name is a deleted declaration somebody else's code imports.
func TestRegistrationsExceptIsWhatKeepsOneEntityOutOfAnothersDeclarations(t *testing.T) {
	path := "internal/domain/notifications.go"
	lock := &Lock{Version: 1, Entities: map[string]LockEntity{
		"Papel": {Registrations: map[string]map[string]string{path: {"Shared": "h", "OnlyPapel": "h"}}},
		"Outro": {Registrations: map[string]map[string]string{path: {"Shared": "h"}}},
	}}
	foreign := lock.RegistrationsExcept("Papel")
	if foreign[path]["Shared"] == "" {
		t.Errorf("Outro also declares Shared and that must be visible from Papel's side")
	}
	if foreign[path]["OnlyPapel"] != "" {
		t.Errorf("Papel's OWN declaration must not read as a foreign claim on itself")
	}
	own := lock.RegistrationsOf("Papel")
	if len(own[path]) != 2 {
		t.Errorf("RegistrationsOf must report exactly what this entity wrote: %+v", own[path])
	}
}

// TestViewShapeChangedWithoutBump covers the guard whose whole value is being
// right in four situations, three of which must stay silent. A false alarm
// trains an author to ignore it; the missed one is a service that refuses to
// BOOT, minutes later, for whoever deploys it.
func TestViewShapeChangedWithoutBump(t *testing.T) {
	const entity = "Papel"
	lockWith := func(shape string, version int) *Lock {
		return &Lock{Version: 1, Entities: map[string]LockEntity{
			entity: {ViewShape: shape, ViewVersion: version},
		}}
	}
	for _, tc := range []struct {
		name    string
		lock    *Lock
		now     ViewState
		want    bool
		wantWas int
	}{
		{"the shape moved and the version did not", lockWith("old", 1), ViewState{"new", 1}, true, 1},
		{"the shape moved and so did the version", lockWith("old", 1), ViewState{"new", 2}, false, 0},
		{"nothing about the shape moved", lockWith("old", 1), ViewState{"old", 1}, false, 0},
		{"a first generation has nothing to compare", &Lock{Version: 1, Entities: map[string]LockEntity{}}, ViewState{"new", 1}, false, 0},
		{"an entity with no view side", lockWith("", 0), ViewState{"new", 1}, false, 0},
		{"a spec that stopped declaring a view", lockWith("old", 1), ViewState{"", 1}, false, 0},
	} {
		was, changed := tc.lock.ViewShapeChangedWithoutBump(entity, tc.now)
		if changed != tc.want {
			t.Errorf("%s: want changed=%v, got %v", tc.name, tc.want, changed)
		}
		if was != tc.wantWas {
			t.Errorf("%s: the reported previous version is what the author must move past, want %d got %d",
				tc.name, tc.wantWas, was)
		}
	}
}

// TestUnwrittenHookReasonIsToldPerKind. One sentence for all three is the
// sentence that gets somebody paged: an empty service hook PANICS a running
// service, an empty derivation renders a field absent, and an empty rules hook
// accepts a write the spec calls invalid. Same file state, three consequences.
func TestUnwrittenHookReasonIsToldPerKind(t *testing.T) {
	for path, want := range map[string]string{
		"internal/infra/papel_service_manual.go":   "PANIC",
		"internal/domain/papel_computed_manual.go": "ABSENT",
		"internal/domain/papel_rules_manual.go":    "NOT enforced",
		"internal/application/papel_something.go":  "not happening",
	} {
		got := UnwrittenHookReason(path)
		if !strings.Contains(got, want) {
			t.Errorf("%s: the reason must name what this KIND costs (%q), got %q", path, want, got)
		}
	}
}
