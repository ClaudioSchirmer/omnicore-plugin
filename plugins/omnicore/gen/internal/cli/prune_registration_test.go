package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/emit"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
)

// Everything in this file is about the ONE thing the generator does that cannot
// be undone by running it again: removing a declaration from a file it does not
// own. A shared file carries other entities' content and a translation catalog
// carries text nobody can reconstruct, so a wrong answer here is not a bad
// regeneration — it is somebody else's work deleted.
//
// The four verdicts are the whole safety argument, and none of them was covered
// by a test: still declared (leave alone), gone from the file (forget the
// record), edited by hand (keep and say so), claimed by a neighbour (keep and
// say so), and only then delete.

const (
	notifPath   = "internal/domain/notifications.go"
	catalogPath = "internal/application/translations/pt_br.go"
)

// sharedNotifications is a registration file as the generator writes one: two
// declarations, each with the doc comment that must leave with it.
const sharedNotifications = `package domain

// PapelNomeTakenNotification says the name is in use.
type PapelNomeTakenNotification struct{}

// PapelSemDonoNotification says the owner is missing.
type PapelSemDonoNotification struct{}

// OutroTemDonoNotification belongs to another entity.
type OutroTemDonoNotification struct{}
`

const sharedCatalog = `package translations

var ptBR = map[string]string{
	"papel.nome.taken": "Nome já em uso.",
	"papel.sem.dono":   "Informe o dono.",
}
`

// registeredHash is what the lock records for a declaration: the hash of the
// text the generator wrote, read back from the file itself. Computing it here
// the same way the writer did is what makes "unchanged" mean unchanged.
func registeredHash(t *testing.T, src, name string, catalog bool) string {
	t.Helper()
	text, ok := emit.RegisteredText(src, name, catalog)
	if !ok {
		t.Fatalf("%q is not in the fixture — the fixture is wrong, not the code", name)
	}
	return emit.HashText(text)
}

func writeProjectFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func regNamed(regs []pruneReg, name string) (pruneReg, bool) {
	for _, r := range regs {
		if r.Name == name {
			return r, true
		}
	}
	return pruneReg{}, false
}

// pruneFixture is one project with both kinds of shared file on disk and a lock
// that says Papel wrote four names into them.
func pruneFixture(t *testing.T) (root string, lock *fsplan.Lock) {
	t.Helper()
	root = t.TempDir()
	writeProjectFile(t, root, notifPath, sharedNotifications)
	writeProjectFile(t, root, catalogPath, sharedCatalog)

	return root, &fsplan.Lock{Version: 1, Entities: map[string]fsplan.LockEntity{
		"Papel": {Registrations: map[string]map[string]string{
			notifPath: {
				"PapelNomeTakenNotification": registeredHash(t, sharedNotifications, "PapelNomeTakenNotification", false),
				"PapelSemDonoNotification":   registeredHash(t, sharedNotifications, "PapelSemDonoNotification", false),
				// Recorded, but no longer in the file at all: somebody removed
				// it by hand between two runs.
				"PapelJaArquivadoNotification": "whatever it once was",
			},
			catalogPath: {
				"papel.nome.taken": registeredHash(t, sharedCatalog, "papel.nome.taken", true),
				"papel.sem.dono":   registeredHash(t, sharedCatalog, "papel.sem.dono", true),
			},
		}},
		// A second entity that also claims one of the same declarations. This is
		// the case that used to be reasoned about and never exercised.
		"Outro": {Registrations: map[string]map[string]string{
			notifPath: {
				"PapelSemDonoNotification": registeredHash(t, sharedNotifications, "PapelSemDonoNotification", false),
			},
		}},
	}}
}

// TestPlanRegistrationPruneTellsTheFiveCasesApart is the whole verdict table in
// one run, because the cases only mean anything against each other: what makes
// "delete" safe is that the other four were recognised first.
func TestPlanRegistrationPruneTellsTheFiveCasesApart(t *testing.T) {
	root, lock := pruneFixture(t)

	// What the spec would write NOW: it still declares the taken notification
	// and its catalog key, and nothing else.
	now := map[string]map[string]string{
		notifPath:   {"PapelNomeTakenNotification": "hash-does-not-matter-here"},
		catalogPath: {"papel.nome.taken": "hash-does-not-matter-here"},
	}

	regs, err := planRegistrationPrune(root, "Papel", now, lock)
	if err != nil {
		t.Fatalf("planning: %v", err)
	}

	if _, still := regNamed(regs, "PapelNomeTakenNotification"); still {
		t.Errorf("a declaration the spec STILL writes must not appear in a prune plan at all:\n%+v", regs)
	}
	if _, still := regNamed(regs, "papel.nome.taken"); still {
		t.Errorf("a catalog key the spec STILL writes must not appear in a prune plan at all:\n%+v", regs)
	}

	for _, tc := range []struct {
		name    string
		kind    fsplan.PruneKind
		catalog bool
		reason  string
	}{
		{"PapelSemDonoNotification", fsplan.PruneKeep, false, "another entity"},
		{"PapelJaArquivadoNotification", fsplan.PruneForget, false, "only the lock"},
		{"papel.sem.dono", fsplan.PruneDelete, true, "no longer declares"},
	} {
		got, ok := regNamed(regs, tc.name)
		if !ok {
			t.Errorf("%s is missing from the plan:\n%+v", tc.name, regs)
			continue
		}
		if got.Kind != tc.kind {
			t.Errorf("%s: want %s, got %s (%s)", tc.name, tc.kind, got.Kind, got.Reason)
		}
		if got.Catalog != tc.catalog {
			t.Errorf("%s: a catalog key and a Go declaration are removed by different "+
				"means, so telling them apart is not cosmetic; Catalog=%v", tc.name, got.Catalog)
		}
		if !strings.Contains(got.Reason, tc.reason) {
			t.Errorf("%s: the reason is what a human acts on, got %q", tc.name, got.Reason)
		}
	}
}

// TestAHandEditedDeclarationIsNeverRemoved is the rule the whole design rests
// on: the recorded hash is the ONLY proof the text is still the generator's,
// and text that stopped matching is somebody's work.
func TestAHandEditedDeclarationIsNeverRemoved(t *testing.T) {
	root, lock := pruneFixture(t)
	edited := strings.Replace(sharedNotifications,
		"type PapelSemDonoNotification struct{}",
		"type PapelSemDonoNotification struct{ Field string }", 1)
	writeProjectFile(t, root, notifPath, edited)
	// Drop the neighbour's claim, so the ONLY thing that can save this
	// declaration is the hash no longer matching.
	delete(lock.Entities, "Outro")

	regs, err := planRegistrationPrune(root, "Papel", nil, lock)
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	got, ok := regNamed(regs, "PapelSemDonoNotification")
	if !ok {
		t.Fatalf("the edited declaration is missing from the plan:\n%+v", regs)
	}
	if got.Kind != fsplan.PruneKeep {
		t.Fatalf("an edited declaration must be KEPT, got %s (%s)", got.Kind, got.Reason)
	}
	if !strings.Contains(got.Reason, "edited by hand") {
		t.Errorf("the reason must name the edit, got %q", got.Reason)
	}
}

// TestApplyRegistrationPruneRemovesTheTextAndTheRecord covers both halves of
// one act. Removing the text and leaving the lock record would make the next
// run report a phantom; forgetting the record and leaving the text would leave
// a declaration nothing can ever clean up again.
func TestApplyRegistrationPruneRemovesTheTextAndTheRecord(t *testing.T) {
	root, lock := pruneFixture(t)
	regs := []pruneReg{
		{notifPath, "PapelSemDonoNotification", fsplan.PruneDelete, false, "the spec no longer declares it"},
		{catalogPath, "papel.sem.dono", fsplan.PruneDelete, true, "the spec no longer declares it"},
		{notifPath, "PapelJaArquivadoNotification", fsplan.PruneForget, false, "already removed from the file"},
		{notifPath, "PapelNomeTakenNotification", fsplan.PruneKeep, false, "it was edited by hand since it was written"},
	}
	if err := applyRegistrationPrune(root, "Papel", regs, lock); err != nil {
		t.Fatalf("applying: %v", err)
	}

	notifs := readProjectFile(t, root, notifPath)
	if strings.Contains(notifs, "PapelSemDonoNotification") {
		t.Errorf("the deleted declaration is still in the file:\n%s", notifs)
	}
	if strings.Contains(notifs, "says the owner is missing") {
		t.Errorf("the doc comment describes a type that is gone and must leave with it:\n%s", notifs)
	}
	for _, want := range []string{"PapelNomeTakenNotification", "OutroTemDonoNotification"} {
		if !strings.Contains(notifs, want) {
			t.Errorf("%s was not this run's to remove:\n%s", want, notifs)
		}
	}

	catalog := readProjectFile(t, root, catalogPath)
	if strings.Contains(catalog, "papel.sem.dono") {
		t.Errorf("the deleted catalog key is still in the map:\n%s", catalog)
	}
	if !strings.Contains(catalog, `"papel.nome.taken"`) {
		t.Errorf("the surviving key was taken with it:\n%s", catalog)
	}

	recorded := lock.RegistrationsOf("Papel")
	for _, gone := range []string{"PapelSemDonoNotification", "PapelJaArquivadoNotification"} {
		if _, still := recorded[notifPath][gone]; still {
			t.Errorf("%s was removed but the lock still records it — the next run "+
				"would report a file that no longer says anything about it", gone)
		}
	}
	if _, still := recorded[notifPath]["PapelNomeTakenNotification"]; !still {
		t.Errorf("a KEPT declaration must keep its record, or nothing tracks it anymore")
	}
	if _, still := recorded[catalogPath]["papel.sem.dono"]; still {
		t.Errorf("the deleted catalog key is still recorded in the lock")
	}
	if _, still := recorded[catalogPath]["papel.nome.taken"]; !still {
		t.Errorf("the surviving catalog key lost its record")
	}
	// The neighbour never asked for any of this.
	if _, still := lock.RegistrationsOf("Outro")[notifPath]["PapelSemDonoNotification"]; !still {
		t.Errorf("pruning Papel forgot a record that belongs to Outro")
	}
}

func readProjectFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The value-object half of the same question. A VO file is generated by ONE
// spec and referenced by every spec that names the type, so the lock alone
// says "Papel wrote it" and deleting on that basis breaks a package the author
// was not touching.

const neighbourSpec = `specVersion: 1
entity: Usuario
plural: Usuarios
valueObjects:
  - {name: Email, kind: raw, backing: string, description: A valid e-mail address.}
fields:
  - {name: Contato, type: string, column: contato, vo: {kind: reuse, ref: Email}}
`

func voProject(t *testing.T, root string, siblings ...string) *discover.Project {
	t.Helper()
	p := &discover.Project{Root: root}
	for _, rel := range siblings {
		p.SiblingSpecs = append(p.SiblingSpecs, discover.SpecClaim{Path: rel})
	}
	return p
}

func voFiles() []fsplan.PruneFile {
	return []fsplan.PruneFile{
		{Path: "internal/domain/vos/email.go", Kind: fsplan.PruneDelete, Reason: "the spec no longer declares it"},
		{Path: "internal/domain/vos/apelido.go", Kind: fsplan.PruneDelete, Reason: "the spec no longer declares it"},
	}
}

// TestAValueObjectANeighbourStillNamesIsKept is the case that motivated the
// guard: Papel stops using Email, Usuario still does, and the file Papel's lock
// claims is the file Usuario's generated code imports.
func TestAValueObjectANeighbourStillNamesIsKept(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "specs/omnicore-gen/usuario.omnicore.yaml", neighbourSpec)
	proj := voProject(t, root, "specs/omnicore-gen/usuario.omnicore.yaml")

	got := keepWhatNeighboursStillNeed(proj, filepath.Join(root, "specs/omnicore-gen/papel.omnicore.yaml"), voFiles())

	kept, _ := pruneFileNamed(got, "internal/domain/vos/email.go")
	if kept.Kind != fsplan.PruneKeep {
		t.Fatalf("a VO another spec still names must be KEPT, got %s (%s)", kept.Kind, kept.Reason)
	}
	if !strings.Contains(kept.Reason, "usuario.omnicore.yaml") {
		t.Errorf("the reason must name the spec that still needs it, got %q", kept.Reason)
	}
	unused, _ := pruneFileNamed(got, "internal/domain/vos/apelido.go")
	if unused.Kind != fsplan.PruneDelete {
		t.Errorf("a VO NOBODY names is still this run's to remove, got %s", unused.Kind)
	}
}

// TestTheSpecBeingPrunedDoesNotSaveItsOwnTypes. The spec under prune is in the
// project's own list of specs, so reading it as a "neighbour" would make every
// value object it still mentions unremovable — the guard would silently turn
// prune into a no-op for the whole vos package.
func TestTheSpecBeingPrunedDoesNotSaveItsOwnTypes(t *testing.T) {
	root := t.TempDir()
	self := "specs/omnicore-gen/usuario.omnicore.yaml"
	writeProjectFile(t, root, self, neighbourSpec)
	proj := voProject(t, root, self)

	got := keepWhatNeighboursStillNeed(proj, filepath.Join(root, self), voFiles())

	email, _ := pruneFileNamed(got, "internal/domain/vos/email.go")
	if email.Kind != fsplan.PruneDelete {
		t.Fatalf("the spec being pruned must not vouch for its own types, got %s (%s)",
			email.Kind, email.Reason)
	}
}

// TestAnUnreadableNeighbourIsNotALicenceToDelete. A sibling spec that does not
// parse says NOTHING about what it needs, and the safe reading of nothing is
// not "delete it".
func TestAnUnreadableNeighbourIsNotALicenceToDelete(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "specs/omnicore-gen/broken.omnicore.yaml", "entity: [this is not\n  a spec")
	proj := voProject(t, root, "specs/omnicore-gen/broken.omnicore.yaml", "specs/omnicore-gen/usuario.omnicore.yaml")
	writeProjectFile(t, root, "specs/omnicore-gen/usuario.omnicore.yaml", neighbourSpec)

	got := keepWhatNeighboursStillNeed(proj, filepath.Join(root, "specs/omnicore-gen/papel.omnicore.yaml"), voFiles())

	email, _ := pruneFileNamed(got, "internal/domain/vos/email.go")
	if email.Kind != fsplan.PruneKeep {
		t.Fatalf("the readable neighbour's claim was lost because another spec is broken, got %s", email.Kind)
	}
}

func pruneFileNamed(files []fsplan.PruneFile, path string) (fsplan.PruneFile, bool) {
	for _, f := range files {
		if filepath.ToSlash(f.Path) == path {
			return f, true
		}
	}
	return fsplan.PruneFile{}, false
}

// TestCountableCountsOnlyWhatWouldChange is what decides whether prune says
// "nothing to do" and exits. Counting a KEEP would make an author run -apply
// against a plan that removes nothing.
func TestCountableCountsOnlyWhatWouldChange(t *testing.T) {
	files := []fsplan.PruneFile{
		{Path: "a.go", Kind: fsplan.PruneDelete},
		{Path: "b.go", Kind: fsplan.PruneForget},
		{Path: "c.go", Kind: fsplan.PruneKeep},
	}
	regs := []pruneReg{
		{Path: notifPath, Name: "X", Kind: fsplan.PruneDelete},
		{Path: notifPath, Name: "Y", Kind: fsplan.PruneKeep},
	}
	if n := countable(files, regs); n != 3 {
		t.Errorf("want 3 actionable items (2 files + 1 declaration), got %d", n)
	}
	if n := countable(nil, nil); n != 0 {
		t.Errorf("an empty plan is nothing to do, got %d", n)
	}
}
