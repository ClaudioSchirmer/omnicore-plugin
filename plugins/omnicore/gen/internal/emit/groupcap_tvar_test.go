package emit

import (
	"strings"
	"testing"
)

// A declared variable that is never bound is the one defect class the end user
// meets before anybody else does.
//
// The struct field was generated, the seven catalogs were generated, the cap
// was hard-coded one line above the raise site — and the raise site still spelled
// the notification `{}`. The 422 rendered "A profile may grant at most
// permissions.", with a hole, in every language. gofmt, vet, the build and the
// generated suite are all silent about it: nothing here is a Go error.
//
// The three assertions below are the three raise sites emitGroupCap has, which
// is why the fixture carries three caps: a plain one, a per-key one, and a
// restricted one. Before this, all three passed through notifIn, which cannot
// bind anything — it returns a constant literal.
func TestGroupCapBindsItsCapIntoTheMessage(t *testing.T) {
	m := identityFeedModel(t, "tenant")
	got := fileNamed(t, m, "internal/domain/perfil.go")

	// The plain cap — the reported case. `max` is the name the message uses, and
	// the value lives under the spec's `cap:` key; the binding crosses that gap
	// rather than asking the sentence to be rewritten.
	if !strings.Contains(got, `PermissoesDemaisNotification{Max: "200"}`) {
		t.Errorf("the collection cap renders with a hole where {max} belongs:\n%s", got)
	}
	// The restricted cap, whose raise site is a different branch of the emitter.
	if !strings.Contains(got, `LeiturasDemaisNotification{Max: "7"}`) {
		t.Errorf("the restricted cap does not bind its bound:\n%s", got)
	}
	// The per-key cap, third branch — and a spec that names the variable after
	// the key it comes from rather than after the sentence.
	if !strings.Contains(got, `PermissoesDemaisPorEscopoNotification{Cap: "3"}`) {
		t.Errorf("the per-key cap does not bind its bound:\n%s", got)
	}
}

// The field the binding assigns has to be the one the struct declares, or the
// fix compiles nowhere. Both come from the same tvar name, so this is really a
// check that they still agree.
func TestGroupCapNotificationDeclaresTheFieldItBinds(t *testing.T) {
	m := identityFeedModel(t, "tenant")
	got := fileNamed(t, m, "internal/domain/notifications.go")
	for _, want := range []string{
		"Max string `tvar:\"max\"`",
		"Cap string `tvar:\"cap\"`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the notification struct has no field for the bound:\n%s\nwant: %s", got, want)
		}
	}
}
