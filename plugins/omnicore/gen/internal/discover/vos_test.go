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

	write("uf.go", "// "+generatedBanner+"-plugin-gen. DO NOT EDIT.\n"+`//
// entity:     RentalListing
// spec:       specs/rental_listing.omnicore.yaml

package vos

type UF string

func (v UF) Value() string { return string(v) }
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

	names, owner := discoverVOs(dir)

	want := []string{"CPF", "UF"}
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
}
