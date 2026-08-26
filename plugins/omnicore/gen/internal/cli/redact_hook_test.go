package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// hookModel is a model owing two hand-written redactors.
func hookModel() *ir.Model {
	return &ir.Model{
		Entity: ir.Names{Pascal: "Paciente", Snake: "paciente"},
		Fields: []ir.Field{
			{Name: "Email", Redaction: &ir.Redaction{
				InSync: ir.Redactor{Kind: "hook", HookFunc: "redactPacienteEmailInSync"},
			}},
			{Name: "Documento", Redaction: &ir.Redaction{
				InSync: ir.Redactor{Kind: "hook", HookFunc: "redactPacienteDocumentoInSync"},
			}},
		},
	}
}

const hookPath = "internal/infra/schemas/paciente_redactors_manual.go"

// TestUnimplementedRedactorsReadsTheFileThatWasKept is the run nobody sees
// coming: a `kind: hook` added to a spec that already generated.
//
// The hook file is written ONCE, so this run wrote no stub for it, and the
// schema now calls a function nothing declares. Every other signal in the run
// says the opposite — the file is listed under "left untouched (yours, by
// design)", the summary reports success — and the package does not compile.
func TestUnimplementedRedactorsReadsTheFileThatWasKept(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(hookPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	// The file as it stood before the spec gained the second hook.
	if err := os.WriteFile(filepath.Join(root, hookPath), []byte(
		"package schemas\n\nfunc redactPacienteEmailInSync(v string) string { return \"***\" }\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	kept := []fsplan.Decision{{
		File:   fsplan.File{Path: hookPath, Class: fsplan.Hook},
		Action: fsplan.KeptHook,
	}}

	got := unimplementedRedactors(root, hookModel(), kept)
	if len(got) != 1 || got[0] != "redactPacienteDocumentoInSync" {
		t.Errorf("the hook the kept file does not declare was not named: got %v", got)
	}
}

// TestAFreshlyWrittenHookOwesNothing. The file this run created carries a stub
// for every hook it planned, so naming any of them here would report work as
// outstanding that the run just did — which is how a report stops being read.
func TestAFreshlyWrittenHookOwesNothing(t *testing.T) {
	created := []fsplan.Decision{{
		File:   fsplan.File{Path: hookPath, Class: fsplan.Hook},
		Action: fsplan.Create,
	}}
	if got := unimplementedRedactors(t.TempDir(), hookModel(), created); got != nil {
		t.Errorf("a freshly written hook file was reported as owing %v", got)
	}
}

// TestNoHooksAskNoQuestions keeps the check off the path of every spec that does
// not use the feature — it must not read a file, and must not care that none is
// there.
func TestNoHooksAskNoQuestions(t *testing.T) {
	plain := &ir.Model{Entity: ir.Names{Pascal: "Sala", Snake: "sala"},
		Fields: []ir.Field{{Name: "Nome"}}}
	if got := unimplementedRedactors(t.TempDir(), plain, nil); got != nil {
		t.Errorf("a spec with no redaction was asked about redactors: %v", got)
	}
}
