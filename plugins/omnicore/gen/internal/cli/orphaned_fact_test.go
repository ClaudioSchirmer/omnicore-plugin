package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// The service hook drifts from the spec in BOTH directions, and only one of
// them was ever detected.
//
// A fact ADDED after the file existed is unimplementedFacts' business — a
// method on the port with nothing behind it. A fact REMOVED is this one, and it
// was invisible: the body stayed, still compiling, in a file the generator is
// not allowed to open, so no run and no report ever said the file had drifted.
//
// It stopped being merely dead when a fact could take a generated entry
// carrier: that type leaves with the fact, so the stranded body fails to build
// on a symbol nobody can trace back to a decision.

const serviceHookPath = "internal/infra/papel_service_manual.go"

func orphanModel(factNames ...string) *ir.Model {
	m := &ir.Model{
		Entity:  ir.Names{Pascal: "Papel", Snake: "papel"},
		Service: &ir.ServiceModel{Impl: "PapelServiceImpl"},
	}
	for _, n := range factNames {
		m.Service.Facts = append(m.Service.Facts, ir.Fact{
			Name: n, Kind: "manual", Manual: true, ReturnType: "bool",
		})
	}
	return m
}

// writeServiceHook lays down a hook file declaring the given method names, the
// way a previous run's stubs would have left it.
func writeServiceHook(t *testing.T, root string, methods ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(serviceHookPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package infra\n"
	for _, m := range methods {
		body += "\nfunc (s *PapelServiceImpl) " + m + "() bool { return false }\n"
	}
	if err := os.WriteFile(filepath.Join(root, serviceHookPath), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func keptServiceHook() []fsplan.Decision {
	return []fsplan.Decision{{
		File:   fsplan.File{Path: serviceHookPath, Class: fsplan.Hook},
		Action: fsplan.KeptHook,
	}}
}

// TestABodyTheSpecNoLongerAsksForIsNamed. The whole point: the run succeeds,
// the file is listed under "left untouched (yours, by design)", and it answers
// for a question nobody asks any more.
func TestABodyTheSpecNoLongerAsksForIsNamed(t *testing.T) {
	root := t.TempDir()
	writeServiceHook(t, root, "PermissaoIndisponivel", "RotuloNaoConfere")

	got := orphanedFacts(root, orphanModel("PermissaoIndisponivel"), keptServiceHook())
	if len(got) != 1 || got[0] != "RotuloNaoConfere" {
		t.Errorf("the stranded body was not named: got %v", got)
	}
}

// TestAFactStillDeclaredIsNotAnOrphan is the other half. A check that reported
// every method would report the whole file on every run, which is how a report
// stops being read.
func TestAFactStillDeclaredIsNotAnOrphan(t *testing.T) {
	root := t.TempDir()
	writeServiceHook(t, root, "PermissaoIndisponivel")

	if got := orphanedFacts(root, orphanModel("PermissaoIndisponivel"), keptServiceHook()); got != nil {
		t.Errorf("a fact the spec still declares was reported as stranded: %v", got)
	}
}

// TestEveryKindCountsAsDeclared, not only the manual ones. A fact promoted from
// `manual` to a computed kind keeps its name and stops being the author's to
// write — the body is now dead, but the METHOD is still on the port, so
// reporting it here would send someone to delete a method the interface
// requires.
func TestEveryKindCountsAsDeclared(t *testing.T) {
	root := t.TempDir()
	writeServiceHook(t, root, "PermissaoIndisponivel")

	m := orphanModel()
	m.Service.Facts = []ir.Fact{{Name: "PermissaoIndisponivel", Kind: "exists", ReturnType: "bool"}}
	if got := orphanedFacts(root, m, keptServiceHook()); got != nil {
		t.Errorf("a fact that merely changed kind was reported as stranded: %v", got)
	}
}

// TestAFreshlyWrittenServiceHookOwesNothing. The file this run created carries
// exactly this run's stubs, so nothing in it can be stranded.
func TestAFreshlyWrittenServiceHookOwesNothing(t *testing.T) {
	root := t.TempDir()
	writeServiceHook(t, root, "PermissaoIndisponivel", "RotuloNaoConfere")

	created := []fsplan.Decision{{
		File:   fsplan.File{Path: serviceHookPath, Class: fsplan.Hook},
		Action: fsplan.Create,
	}}
	if got := orphanedFacts(root, orphanModel("PermissaoIndisponivel"), created); got != nil {
		t.Errorf("a freshly written hook was reported as carrying orphans: %v", got)
	}
}

// TestNoServiceAsksNoQuestions keeps the check off the path of every spec that
// declares no service at all — it must not read a file, and must not care that
// none is there.
func TestNoServiceAsksNoQuestions(t *testing.T) {
	plain := &ir.Model{Entity: ir.Names{Pascal: "Sala", Snake: "sala"}}
	if got := orphanedFacts(t.TempDir(), plain, keptServiceHook()); got != nil {
		t.Errorf("an entity with no service reported orphans: %v", got)
	}
}

// TestTheSameNameIsReportedOnce. A hook file that somehow declares a method
// twice does not build, but the report must not compound one problem into two
// lines — a reader who sees the same name twice starts doubting the list.
func TestTheSameNameIsReportedOnce(t *testing.T) {
	root := t.TempDir()
	writeServiceHook(t, root, "RotuloNaoConfere", "RotuloNaoConfere")

	if got := orphanedFacts(root, orphanModel(), keptServiceHook()); len(got) != 1 {
		t.Errorf("the same stranded name was reported %d times: %v", len(got), got)
	}
}
