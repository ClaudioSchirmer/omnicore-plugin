package spec

import (
	"strings"
	"testing"
)

// A `valueObject` rule moves WHEN a value object is validated; it never adds a
// check and it never invents an answer. Everything below is a way of asking it
// to do one of those two things, and each one has to be refused where the
// author can still see the yaml — an accepted key that emits nothing is the one
// outcome a rule language must not have.

func voRuleSpec(rules ...Rule) *Spec {
	s := minimalSpec()
	s.Modes = []string{"display", "insert", "update"}
	s.Update = Update{Shape: "put"}
	s.Authz.Permissions["update"] = "student:write"
	s.Fields = append(s.Fields, Field{
		Name: "TenantID", Type: "id", Column: "tenant_id", LivesOn: "root",
		Example: "0e37df36-f698-11e6-8dd4-cb9ced3df976", Description: "O tenant.",
	})
	s.Rules.List = rules
	return s
}

func voRule() Rule {
	return Rule{
		ID: "tenant-first", Kind: "valueObject",
		Scope: []string{"insertOrUpdate"}, Fields: []string{"TenantID"}, Guard: true,
	}
}

func problemsFor(t *testing.T, s *Spec, opt Options) string {
	t.Helper()
	ps := Validate(s, opt)
	if !ps.HasBlockers() {
		return ""
	}
	return ps.Error().Error()
}

// The plain case: an id IS a value object to the framework (domain.ID writes
// IsValid), so the rule reaches it with nothing special declared.
func TestValueObjectRuleOverAnIDValidates(t *testing.T) {
	if got := problemsFor(t, voRuleSpec(voRule()), Options{}); got != "" {
		t.Fatalf("pulling an id's validation forward is refused:\n%s", got)
	}
}

// The value object owns its answer. Every key that would supply a second one is
// dead configuration, and dead configuration in a rule reads, to the next
// author, like a rule that does something.
func TestValueObjectRuleRefusesASecondAnswer(t *testing.T) {
	no := false
	cases := map[string]func(r *Rule){
		"notification": func(r *Rule) { r.Notification = "RequiredFieldNotification" },
		"attachTo":     func(r *Rule) { r.AttachTo = "TenantID" },
		"skipWhen":     func(r *Rule) { r.SkipWhen = "empty" },
		"echoValue":    func(r *Rule) { r.EchoValue = &no },
		"min":          func(r *Rule) { m := 1.0; r.Min = &m },
	}
	for key, mutate := range cases {
		r := voRule()
		mutate(&r)
		got := problemsFor(t, voRuleSpec(r), Options{})
		if got == "" {
			t.Errorf("%s is accepted on a valueObject rule, and it decides nothing", key)
			continue
		}
		if !strings.Contains(got, "valueObject rule") {
			t.Errorf("%s is refused without saying why it cannot apply:\n%s", key, got)
		}
	}
}

// The kind moves a check; it cannot conjure one. A field with no value object
// has nothing to pull forward, and accepting it would emit a call on a plain
// string.
func TestValueObjectRuleNeedsAValueObject(t *testing.T) {
	r := voRule()
	r.Fields = []string{"Name"}
	got := problemsFor(t, voRuleSpec(r), Options{})
	if got == "" {
		t.Fatal("a rule over a field with no value object is accepted")
	}
	if !strings.Contains(got, "no value object") {
		t.Errorf("the refusal does not name the reason:\n%s", got)
	}
}

// A reused value object is a type this spec never described, and the two kinds
// are validated with different calls. The project's own vos package is the
// authority; without an answer from it the rule is refused rather than guessed.
func TestValueObjectRuleOverAReusedTypeAsksTheProject(t *testing.T) {
	withRef := func() *Spec {
		s := voRuleSpec(Rule{
			ID: "email-first", Kind: "valueObject",
			Scope: []string{"insertOrUpdate"}, Fields: []string{"Name"},
		})
		s.Fields[0].VO = &FieldVO{Kind: "reuse", Ref: "Email"}
		return s
	}

	unknown := problemsFor(t, withRef(), Options{ExistingVOs: []string{"Email"}})
	if unknown == "" {
		t.Fatal("a reused type of unknown shape is accepted, and the emitted call may not compile")
	}
	if !strings.Contains(unknown, "IsValid") || !strings.Contains(unknown, "membership") {
		t.Errorf("the refusal does not say which distinction is missing:\n%s", unknown)
	}

	known := problemsFor(t, withRef(), Options{
		ExistingVOs:     []string{"Email"},
		ExistingVOKinds: map[string]string{"Email": "raw"},
	})
	if known != "" {
		t.Fatalf("a reused type the project could classify is still refused:\n%s", known)
	}
}

// The exclusion is a set of names, so naming a field twice costs nothing. The
// validation call is not: it EMITS, and two of them in one pass hand the caller
// the same complaint twice — the exact duplicate this kind exists to prevent.
func TestValueObjectRuleRefusesTwoChecksInOnePass(t *testing.T) {
	// insert and insertOrUpdate are different scopes that both run on an
	// insert, which is what makes this invisible in the yaml.
	overlapping := voRuleSpec(
		Rule{ID: "tenant-on-insert", Kind: "valueObject",
			Scope: []string{"insert"}, Fields: []string{"TenantID"}},
		Rule{ID: "tenant-on-write", Kind: "valueObject",
			Scope: []string{"insertOrUpdate"}, Fields: []string{"TenantID"}, Guard: true},
	)
	got := problemsFor(t, overlapping, Options{})
	if got == "" {
		t.Fatal("one value object validated twice on the same verb is accepted")
	}
	if !strings.Contains(got, "is already validated in place by") || !strings.Contains(got, "both rules run on insert") {
		t.Errorf("the refusal does not say what collides, or on which verb:\n%s", got)
	}

	// Two rules that never meet are fine: the whole point of the kind is that
	// each verb decides for itself.
	disjoint := voRuleSpec(
		Rule{ID: "tenant-on-insert", Kind: "valueObject",
			Scope: []string{"insert"}, Fields: []string{"TenantID"}},
		Rule{ID: "tenant-on-update", Kind: "valueObject",
			Scope: []string{"update"}, Fields: []string{"TenantID"}, Guard: true},
	)
	if got := problemsFor(t, disjoint, Options{}); got != "" {
		t.Errorf("two rules that cannot both run are refused:\n%s", got)
	}

	same := voRule()
	same.Fields = []string{"TenantID", "TenantID"}
	if got := problemsFor(t, voRuleSpec(same), Options{}); got == "" {
		t.Error("one rule naming the same field twice is accepted")
	}
}

// A composite value object's rules run inside its own IsValid, which is handed
// a NotificationContext and no Rules: there is no pass there to pull anything
// forward into, and nothing to exclude a field from.
func TestValueObjectRuleIsRefusedInsideAValueObject(t *testing.T) {
	s := voRuleSpec()
	s.ValueObjects = []ValueObject{{
		Name: "Period", Kind: "composite", Description: "Um periodo.",
		Parts: []VOPart{
			{Name: "Start", Type: "time", Description: "Inicio."},
			{Name: "End", Type: "time", Description: "Fim."},
		},
		Rules: Rules{List: []Rule{{
			ID: "start-first", Kind: "valueObject", Fields: []string{"Start"},
		}}},
	}}
	got := problemsFor(t, s, Options{})
	if got == "" {
		t.Fatal("a valueObject rule inside a composite is accepted, and it can emit nothing")
	}
	if !strings.Contains(got, "rule of the ENTITY") {
		t.Errorf("the refusal does not say where the rule belongs instead:\n%s", got)
	}
}
