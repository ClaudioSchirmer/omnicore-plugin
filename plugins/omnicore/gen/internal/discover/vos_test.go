package discover

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiscoverVOsSeesGeneratedOnesAndNotNotifications pins both halves of the
// inventory that a real run got wrong in both directions at once.
//
// The project it describes is the one that exposed it: a shared identity with
// two roles, where the first role's spec generated the UF enum and the second
// role has to reuse it. The old inventory skipped every generated file — so UF,
// URL and the status enum were invisible — and collected every exported type of
// the hand-written notifications.go, so the answer to "which value objects does
// this project have?" was three notifications. The author was told to reuse one
// of those, did, and validation accepted it.
func TestDiscoverVOsSeesGeneratedOnesAndNotNotifications(t *testing.T) {
	dir := t.TempDir()
	vos := filepath.Join(dir, "internal", "domain", "vos")
	if err := os.MkdirAll(vos, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(vos, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("uf.go", "// "+generatedBanner+"-gen. DO NOT EDIT.\n"+`//
// entity:     RentalListing
// spec:       omnicore-gen/rental_listing.omnicore.yaml

package vos

type UF string

func (v UF) Value() string { return string(v) }
`)
	// A second generated one, so the inventory is exercised past the first hit
	// — and it is named URL on purpose: notifications.go below carries an
	// InvalidURLNotification, and telling the two apart is the whole trap.
	write("url.go", "// "+generatedBanner+"-gen. DO NOT EDIT.\n"+`//
// entity:     RentalListing
// spec:       omnicore-gen/rental_listing.omnicore.yaml

package vos

type URL string

func (v URL) Value() string { return string(v) }
`)
	// Hand-written, and a value object all the same: ownership decides who may
	// rewrite the file, never whether the type exists.
	write("cpf.go", `package vos

type CPF string

func (v CPF) Value() string { return string(v) }
`)
	// The trap: notifications share the package, and one of them even carries a
	// method whose name starts the same way.
	write("notifications.go", `package vos

type UnknownUFNotification struct{}

func (n UnknownUFNotification) ValueOfField() string { return "" }

type InvalidURLNotification struct{}
`)

	// The two kinds a caller has to tell apart to validate a value object IN
	// PLACE: one answers for itself, the other is checked for membership by the
	// framework, and the calls are not interchangeable.
	write("email.go", `package vos

type Email string

func (v Email) Value() string { return string(v) }

func (v Email) IsValid(fieldName string, ctx *domain.NotificationContext) bool { return true }
`)
	write("status.go", `package vos

type Status string

func (v Status) Value() string { return string(v) }

func (v Status) Values() []Status { return []Status{"active"} }

func (v Status) UnknownNotification() domain.Notification { return UnknownStatusNotification{} }
`)

	names, owner, kind := discoverVOs(dir)

	want := []string{"CPF", "Email", "Status", "UF", "URL"}
	if len(names) != len(want) {
		t.Fatalf("inventory = %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Fatalf("inventory = %v, want %v", names, want)
		}
	}
	if owner["UF"] != "RentalListing" {
		t.Errorf("UF is owned by %q, want RentalListing — reuse works off the inventory, "+
			"but only the owner may rewrite the file", owner["UF"])
	}
	if by, ok := owner["CPF"]; !ok || by != "" {
		t.Errorf("a hand-written value object reports owner %q, want \"\" (nobody's to rewrite)", by)
	}
	if kind["Email"] != "raw" {
		t.Errorf("Email is %q, want raw — it writes its own IsValid", kind["Email"])
	}
	if kind["Status"] != "enum" {
		t.Errorf("Status is %q, want enum — it declares members and an unknown answer, "+
			"and writes no IsValid", kind["Status"])
	}
	// Not a guess: a type the reader cannot classify is left out, and the rule
	// that needs the answer refuses rather than emitting a call that may not
	// compile.
	if k, ok := kind["CPF"]; ok {
		t.Errorf("CPF was classified as %q from a Value() alone", k)
	}
}
