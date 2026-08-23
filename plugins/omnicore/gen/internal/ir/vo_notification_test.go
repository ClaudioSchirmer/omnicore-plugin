package ir

import "testing"

// TestVONotificationIsQualifiedByWhereItLives pins a defect that shipped for
// several releases and that the generator itself invited.
//
// When a value object names a notification the spec does not declare, the
// refusal offers the framework's own by name — "or name one of the framework's:
// …, SchemaViolationNotification". Taking that advice produced a generated
// value-object file that referenced the identifier BARE, inside package vos,
// where nothing declares it: the spec passed `check` and the tree did not
// compile, against the generator's own promise that a green spec builds.
//
// The two cases differ only by where the type lives. A notification the service
// declares is generated INTO the vos package beside the value objects that raise
// it, so it is referenced bare; a framework one lives in the framework's domain
// package, which every generated value-object file already imports.
func TestVONotificationIsQualifiedByWhereItLives(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"a framework notification", "SchemaViolationNotification", "domain.SchemaViolationNotification"},
		{"another framework one", "RequiredFieldNotification", "domain.RequiredFieldNotification"},
		{"one the service declares", "InvalidEmailNotification", "InvalidEmailNotification"},
		{"none at all", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := qualifyVONotification(tc.in); got != tc.want {
				t.Errorf("qualifyVONotification(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
