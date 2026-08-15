package ir

import (
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// TestChildRulesKeepEveryAttribute is the regression for a whole class of bug,
// not for one rule.
//
// The collection had a resolver of its own, and it was a copy of the root's
// that had fallen behind: it carried the kind, the fields and the notification,
// and dropped Transitions, GroupBy, Cap, SkipWhen, AdminField and OwnerField.
// So a `transition` under children[] validated, generated its notification and
// all seven translations, and emitted a clause with no edges. Nothing failed:
// not the build, not the tests, not the report. The author found it by reading
// the generated file and noticing an empty IfUpdate block.
//
// The assertion is therefore per ATTRIBUTE. Checking that "a transition works"
// would pass again the next time a different attribute is forgotten.
func TestChildRulesKeepEveryAttribute(t *testing.T) {
	transitions := map[string][]string{
		"UnderReview": {"Accepted", "Rejected"},
		"Rejected":    {"UnderReview"},
	}
	rules := spec.Rules{List: []spec.Rule{{
		ID: "status-transitions", Kind: "transition", Scope: []string{"update"},
		Fields: []string{"Status"}, Notification: "InvalidTransitionNotification",
		Transitions: transitions, SkipWhen: "empty",
	}}}

	scope := []Field{
		{Name: "Status", Column: "status", SpecType: "string", EntityType: "string"},
	}
	clauses := resolveClausesFor(rules, scope)
	if len(clauses) != 1 || len(clauses[0].Rules) != 1 {
		t.Fatalf("the rule did not survive resolution: %+v", clauses)
	}
	got := clauses[0].Rules[0]

	if len(got.Transitions) != len(transitions) {
		t.Errorf("Transitions is %v — an emitted transition with no edges allows every "+
			"move, which is the opposite of what was declared", got.Transitions)
	}
	if got.SkipWhen != "empty" {
		t.Errorf("SkipWhen is %q, want \"empty\" — the rule fires on a value the spec "+
			"said to stand down on", got.SkipWhen)
	}
	if len(got.Fields) != 1 || got.Fields[0].Name != "Status" {
		t.Errorf("the rule lost its field: %+v", got.Fields)
	}
	if got.AttachTo != "Status" {
		t.Errorf("AttachTo is %q — the notification reaches the caller unaddressed", got.AttachTo)
	}
}

// TestChildManualRulesReachAHook pins the other half. A manual rule declared
// under children[] was read, validated for a description, and then dropped: no
// hook file, no call site, no report line. The spec asked for an invariant and
// the generator answered by forgetting it.
func TestChildManualRulesReachAHook(t *testing.T) {
	got := resolveManualRules(spec.Rules{Manual: []spec.ManualRule{{
		ID:          "proposta-comeca-em-analise",
		Description: "Uma proposta recém-criada precisa entrar como UnderReview.",
		Scope:       []string{"insert"},
	}}})
	if len(got) != 1 {
		t.Fatalf("a manual rule declared on a collection is dropped: %+v", got)
	}
	if got[0].ID != "proposta-comeca-em-analise" || len(got[0].Gates) != 1 {
		t.Errorf("the manual rule lost its identity or its gate: %+v", got[0])
	}
}
