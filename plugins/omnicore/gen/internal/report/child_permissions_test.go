package report

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// TestCollectionRowShowsWhatGatesThePerEntryVerbs closes a reporting gap that
// predates children[].permissions.
//
// The decisions table lists the ROOT's operations and says each one is a route
// with a permission. The per-entry verbs are routes with permissions too, and
// they appeared in no table at all — so a reviewer could not see what gated the
// collection edge, let alone that it was the root's update by inheritance. That
// is the one edge where the answer is sometimes meant to differ: adding a role
// to a group is not renaming the group.
func TestCollectionRowShowsWhatGatesThePerEntryVerbs(t *testing.T) {
	model := func(perms map[string]string, declared map[string]bool) *ir.Model {
		return &ir.Model{
			Entity: ir.Names{Pascal: "Group", PluralSnake: "groups"},
			Table:  "groups",
			Children: []ir.Child{{
				Name: "GroupRole", Plural: "Roles", Segment: "roles",
				PerChild: true, MountsAdd: true, MountsRemove: true,
				Permissions: perms, Declared: declared,
			}},
		}
	}

	inherited := lineWith(Render(Input{
		Model:    model(map[string]string{"add": "group:update", "remove": "group:update"}, nil),
		SpecPath: "omnicore-gen/group.omnicore.yaml",
	}), "| Collection `Roles` |")

	for _, want := range []string{
		"`add` → `group:update` (inherited)",
		"`remove` → `group:update` (inherited)",
		"/groups/:id/roles",
		"children[].permissions", // the key that answers it, by name
	} {
		if !strings.Contains(inherited, want) {
			t.Errorf("the collection row omits %q, so the inheritance is invisible:\n%s",
				want, inherited)
		}
	}

	// Declared is the other half. The value alone cannot say which it is — a
	// collection may deliberately declare the very permission it would have
	// inherited — and the reviewer's question is about the intent, not the
	// string.
	gated := lineWith(Render(Input{
		Model: model(
			map[string]string{"add": "group:grant", "remove": "group:grant"},
			map[string]bool{"add": true, "remove": true},
		),
		SpecPath: "omnicore-gen/group.omnicore.yaml",
	}), "| Collection `Roles` |")

	for _, want := range []string{"`add` → `group:grant` (declared)", "403"} {
		if !strings.Contains(gated, want) {
			t.Errorf("the collection row omits %q — a permission nobody has been granted "+
				"starts refusing a route that used to answer:\n%s", want, gated)
		}
	}
}

// An atomic-replace collection has no per-entry route, so it must produce no
// row: a table line about permissions on endpoints that do not exist is worse
// than silence, because it reads as a guard.
func TestAtomicReplaceCollectionGetsNoPermissionRow(t *testing.T) {
	out := Render(Input{Model: &ir.Model{
		Entity:   ir.Names{Pascal: "Group", PluralSnake: "groups"},
		Table:    "groups",
		Children: []ir.Child{{Name: "GroupRole", Plural: "Roles", Segment: "roles"}},
	}, SpecPath: "omnicore-gen/group.omnicore.yaml"})

	if strings.Contains(out, "| Collection `Roles` |") {
		t.Errorf("a collection with no per-entry routes is given a permission row:\n%s",
			lineWith(out, "| Collection `Roles` |"))
	}
}
