package spec

import (
	"strings"
	"testing"
)

// retiringSpec is the smallest entity that can declare delete.archiveWhen: it
// updates, it archives, and it has a state field the decision can read.
func retiringSpec() *Spec {
	s := minimalSpec()
	s.Storage.Managed.ArchivedAt = "deleted_at"
	s.Fields = append(s.Fields, Field{
		Name: "Status", Type: "string", Column: "status", Length: 20,
		LivesOn: "root", Example: "active", Description: "Situação da matrícula.",
	})
	s.Modes = []string{"display", "insert", "update", "archive", "unarchive"}
	s.Update = Update{Shape: "patch"}
	s.Delete = Delete{
		Root: "soft",
		ArchiveWhen: &ArchiveWhen{
			Field: "Status", Equals: "dropped",
			Description: "Uma matrícula trancada não é um registro ativo.",
		},
	}
	s.Authz.Permissions = map[string]string{
		"insert": "student:write", "patch": "student:write",
		"archive": "student:archive", "unarchive": "student:archive",
		"read": "student:read",
	}
	return s
}

func warningsAt(ps *Problems, where string) []string {
	var out []string
	for _, p := range ps.Warnings() {
		if p.Where == where {
			out = append(out, p.String())
		}
	}
	return out
}

// TestRetiringSpecIsClean guards the fixture: every "X warns" assertion below
// is vacuous if the baseline already warns about the same place.
func TestRetiringSpecIsClean(t *testing.T) {
	ps := Validate(retiringSpec(), Options{})
	if ps.HasBlockers() {
		t.Fatalf("the baseline should validate cleanly, got:\n%v", ps.Error())
	}
	if w := warningsAt(ps, "delete.archiveWhen.field"); len(w) > 0 {
		t.Fatalf("the baseline already warns about the deciding field: %v", w)
	}
}

// TestTriggerNoUpdateCanReachWarns covers the two ways the condition compiles
// and no caller can ever set it off.
//
// This is the same silence the enum-member check refuses one line above it: a
// trigger nothing can reach retires nothing, forever, and the generated code
// looks exactly like the working version. The difference is that these two are
// warnings — a row INSERTED already holding the trigger still reaches it, and
// is then retired by the next update that touches it, whatever it changes.
// That is strange enough to say out loud and legal enough not to refuse.
func TestTriggerNoUpdateCanReachWarns(t *testing.T) {
	t.Run("excluded from the only update shape there is", func(t *testing.T) {
		s := retiringSpec()
		s.Update.PatchExcludes = []string{"Status"}

		ps := Validate(s, Options{})
		if ps.HasBlockers() {
			t.Fatalf("this is a warning, not a refusal:\n%v", ps.Error())
		}
		got := strings.Join(warningsAt(ps, "delete.archiveWhen.field"), "\n")
		for _, want := range []string{"patchExcludes", "INSERTED", "serve put as well"} {
			if !strings.Contains(got, want) {
				t.Errorf("the warning omits %q, so it does not say what to do:\n%s", want, got)
			}
		}
	})

	// put and both still carry the field in a full body, so the trigger is
	// reachable and there is nothing to warn about. Warning anyway would train
	// the reader to skip the message that matters.
	t.Run("excluded from patch, but put is also served", func(t *testing.T) {
		s := retiringSpec()
		s.Update.Shape = "both"
		s.Update.PatchExcludes = []string{"Status"}

		if w := warningsAt(Validate(s, Options{}), "delete.archiveWhen.field"); len(w) > 0 {
			t.Errorf("a field a PUT can still set is reported as unreachable: %v", w)
		}
	})

	t.Run("immutable on update", func(t *testing.T) {
		s := retiringSpec()
		s.Rules.List = []Rule{{
			ID: "status-imutavel", Kind: "immutable", Scope: []string{"update"},
			Fields: []string{"Status"}, Notification: "RequiredFieldNotification",
		}}

		ps := Validate(s, Options{})
		if ps.HasBlockers() {
			t.Fatalf("this is a warning, not a refusal:\n%v", ps.Error())
		}
		got := strings.Join(warningsAt(ps, "delete.archiveWhen.field"), "\n")
		for _, want := range []string{"immutable", "status-imutavel", "never change"} {
			if !strings.Contains(got, want) {
				t.Errorf("the warning omits %q, so the reader cannot find the other rule:\n%s",
					want, got)
			}
		}
	})

	// Immutability scoped to insert says nothing about updates, which is the
	// verb this decision lives on.
	t.Run("immutable on insert only", func(t *testing.T) {
		s := retiringSpec()
		s.Rules.List = []Rule{{
			ID: "status-fixo-no-insert", Kind: "immutable", Scope: []string{"insert"},
			Fields: []string{"Status"}, Notification: "RequiredFieldNotification",
		}}

		if w := warningsAt(Validate(s, Options{}), "delete.archiveWhen.field"); len(w) > 0 {
			t.Errorf("a rule that does not fire on update is read as blocking one: %v", w)
		}
	})
}
