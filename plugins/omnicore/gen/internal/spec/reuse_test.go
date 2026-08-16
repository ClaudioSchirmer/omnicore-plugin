package spec

import (
	"strings"
	"testing"
)

// TestReusingAnotherEntitysValueObject is the case a shared identity creates and
// this build refused: two roles over one base share a column, so they share the
// value object the first role's spec generated. Reuse is the whole point of the
// kind, and it was reachable only for value objects nobody had generated.
func TestReusingAnotherEntitysValueObject(t *testing.T) {
	s := minimalSpec()
	s.Entity, s.Plural = "SaleListing", "SaleListings"
	s.Fields[0].VO = &FieldVO{Kind: "reuse", Ref: "UF"}

	opt := Options{
		ExistingVOs: []string{"UF"},
		VOOwner:     map[string]string{"UF": "RentalListing"},
	}
	if ps := Validate(s, opt); ps.HasBlockers() {
		t.Fatalf("reusing a value object another entity generated is refused:\n%v", ps.Error())
	}
}

// TestRedeclaringIsRefusedButReRunningIsNot separates the two questions the
// inventory answers. Referencing is open; declaring a SECOND copy is refused,
// because two copies of a rule can disagree — except for the entity that
// already owns it, whose own re-run would otherwise be refused the file its
// last run wrote.
func TestRedeclaringIsRefusedButReRunningIsNot(t *testing.T) {
	declare := func(entity string) *Problems {
		s := minimalSpec()
		s.Entity, s.Plural = entity, entity+"s"
		s.ValueObjects = []ValueObject{{
			Name: "UF", Kind: "raw", Backing: "string",
			MinLength: 2, MaxLength: 2,
			Notification: "UnknownUFNotification",
		}}
		s.Notifications = []Notification{{
			Name: "UnknownUFNotification", Semantic: "validation", Package: "vos",
			Text: Texts{
				PTBR: "UF desconhecida.", ENG: "Unknown state code.",
				ESP: "Estado desconocido.", FRA: "État inconnu.",
				DEU: "Unbekanntes Bundesland.", ITA: "Stato sconosciuto.",
				NLD: "Onbekende provincie.",
			},
		}}
		s.Fields[0].VO = &FieldVO{Kind: "raw", Ref: "UF"}
		return Validate(s, Options{
			ExistingVOs: []string{"UF"},
			VOOwner:     map[string]string{"UF": "RentalListing"},
		})
	}

	if ps := declare("SaleListing"); !ps.HasBlockers() {
		t.Error("a second entity redeclaring an existing value object is accepted — " +
			"that is two copies of one rule, free to drift")
	}
	if ps := declare("RentalListing"); ps.HasBlockers() {
		t.Errorf("the owner regenerating its own value object is refused:\n%v", ps.Error())
	}
}

// TestNotificationNamedAsAValueObjectSaysSo pins the message, not just the
// refusal. An author reached for reuse, was handed a list that happened to be
// notifications, took one, and validation accepted it — so the blocker has to
// name the confusion instead of printing the same list again.
func TestNotificationNamedAsAValueObjectSaysSo(t *testing.T) {
	s := minimalSpec()
	s.Fields[0].VO = &FieldVO{Kind: "reuse", Ref: "UnknownUFNotification"}

	ps := Validate(s, Options{ExistingVOs: []string{"UF"}})
	if !ps.HasBlockers() {
		t.Fatal("a notification passed off as a value object is accepted")
	}
	if !strings.Contains(ps.Error().Error(), "is a notification, not a value object") {
		t.Errorf("the blocker does not name the confusion:\n%v", ps.Error())
	}
}
