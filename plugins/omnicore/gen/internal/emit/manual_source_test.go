package emit

import (
	"regexp"
	"strings"
	"testing"
)

// theManualCase is the matrix case that declares a field no generated write
// fills — the change-password shape.
const theManualCase = "37-campo-que-ninguem-preenche.yaml"

// TestAManualFieldIsOnTheAggregateAndNowhereElse is the whole declaration, and
// both halves are load-bearing. On the aggregate, or hand-written rules cannot
// read it. Off every write surface, or the ordinary PATCH body grows a field
// that belongs to one hand-written endpoint — which is the leak the spelling
// exists to close.
func TestAManualFieldIsOnTheAggregateAndNowhereElse(t *testing.T) {
	m := matrixModels(t)[theManualCase]
	if m == nil {
		t.Fatalf("%s is missing from the coverage matrix", theManualCase)
	}
	onAggregate := false
	for path, src := range goSources(emitAll(t, m)) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		switch {
		case strings.HasSuffix(path, "domain/conta.go"):
			// Matched with the spacing left open: gofmt aligns a struct's field
			// types on the longest NAME in the block, so a literal " vos.Senha"
			// stops matching the day a neighbouring field gets a longer name —
			// which says nothing about the property under test.
			onAggregate = regexp.MustCompile(`SenhaAtual\s+vos\.Senha`).MatchString(src)
		case strings.Contains(path, "web/requests"),
			strings.Contains(path, "application/commands"),
			strings.Contains(path, "infra/schemas"):
			if strings.Contains(src, "SenhaAtual") {
				t.Errorf("%s mentions the manual field; no generated write carries it", path)
			}
		}
	}
	if !onAggregate {
		t.Error("the manual field is not declared on the aggregate, so no rule can read it")
	}
}

// TestAManualFieldsValueObjectIsExcludedUnderEveryGate is the half that decides
// whether the entity works at all.
//
// The framework's automatic pass discovers value objects by walking the STRUCT —
// which is what makes a columnless field validated for free, and what would make
// this one validated on every generated write, where nothing put a value. Without
// the exclusion each ordinary insert and patch is answered "the password is too
// short" for a field the request had no business carrying.
//
// Under EVERY gate, and not under the ones a body field happens to skip: a manual
// field is carried by no verb at all.
func TestAManualFieldsValueObjectIsExcludedUnderEveryGate(t *testing.T) {
	m := matrixModels(t)[theManualCase]
	if m == nil {
		t.Fatalf("%s is missing from the coverage matrix", theManualCase)
	}
	var aggregate string
	for path, src := range goSources(emitAll(t, m)) {
		if strings.HasSuffix(path, "domain/conta.go") {
			aggregate = src
		}
	}
	if aggregate == "" {
		t.Fatal("no aggregate was emitted")
	}
	gates := 0
	for _, line := range strings.Split(aggregate, "\n") {
		if strings.Contains(line, `r.IgnoreValueObject("SenhaAtual")`) {
			gates++
		}
	}
	// The entity mounts insert and update, so both gates must carry it.
	if gates < 2 {
		t.Errorf("the manual field's value object is excluded under %d gate(s); "+
			"every generated write would be refused for a value nothing assigned", gates)
	}
}

// TestAManualFieldIsNotFedFromTheIdentity. ClaimRuntimeFields selects by
// exclusion, and it excluded only `body` until a third source existed — a manual
// field swept in there would have the identity feed assign the caller's token to
// a field the author's own code owns.
func TestAManualFieldIsNotFedFromTheIdentity(t *testing.T) {
	m := matrixModels(t)[theManualCase]
	if m == nil {
		t.Fatalf("%s is missing from the coverage matrix", theManualCase)
	}
	for _, f := range m.ClaimRuntimeFields() {
		if f.Source == "manual" {
			t.Errorf("%s is fed by the identity feed, and nothing generated fills it", f.Name)
		}
	}
	if len(m.ManualRuntimeFields()) == 0 {
		t.Error("the matrix case declares no manual field, so this proves nothing")
	}
}
