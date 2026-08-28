package emit

import (
	"regexp"
	"strings"
	"testing"
)

// The renderIn half of the manual-source case: the value the author's own rule
// minted, on its way out through the response of the verb that minted it.
//
// It rides the same matrix case as the field nobody fills, because it is the
// same declaration read in the other direction — and the two properties below
// are the ones that make it worth having. It ARRIVES on exactly one surface,
// and it arrives on no other.
const theRenderedField = "SenhaProvisoria"

// TestARenderedRuntimeFieldReachesTheMintingVerbsResponse is the promise. A
// credential minted inside the insert rules exists in one place — the entity
// that was just written — and this is the only seat that can hand it over.
//
// Both halves are asserted, because either one alone is a broken feature: a
// Result field with no Response never leaves the service, and a Response field
// with no Result behind it is a boot panic (the generic pair is checked at
// Mount), not a null.
func TestARenderedRuntimeFieldReachesTheMintingVerbsResponse(t *testing.T) {
	m := matrixModels(t)[theManualCase]
	if m == nil {
		t.Fatalf("%s is missing from the coverage matrix", theManualCase)
	}

	var result, response string
	for path, src := range goSources(emitAll(t, m)) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		switch {
		case strings.HasSuffix(path, "application/commands/insert_conta_command.go"):
			result = src
		case strings.HasSuffix(path, "web/requests/insert_conta.go"):
			response = src
		}
	}
	if result == "" || response == "" {
		t.Fatal("the insert command or its DTO file was not emitted")
	}

	// Spacing left open on purpose: gofmt aligns the Result's field types on the
	// longest name in the block, and the property is the field, not the padding.
	onResult := regexp.MustCompile(theRenderedField + `\s+string`).MatchString(result)
	if !onResult {
		t.Errorf("the insert Result does not carry %s, so the minted value is discarded "+
			"with the entity", theRenderedField)
	}
	// Read off the ENTITY, not the command: nothing put the value in the command
	// — that is what source: manual means — so a projection from the input would
	// render empty forever.
	if !strings.Contains(result, theRenderedField+": e."+theRenderedField+".Value(),") {
		t.Errorf("FromEntity does not read %s off the entity; the value the rules minted "+
			"is not what the caller would receive", theRenderedField)
	}
	for _, decl := range responseStructsIn(response) {
		if strings.Contains(decl, theRenderedField) {
			return
		}
	}
	t.Errorf("no response type in the insert DTO renders %s, so the credential never "+
		"leaves the service", theRenderedField)
}

// TestARenderedRuntimeFieldIsOnNoOtherSurface is the containment, and it is the
// half that would rot silently.
//
// renderIn names ONE verb. Everything else this generator writes must look
// exactly as it did before the key existed: the other write verbs, both reads,
// every request body, the table and its schema. A value with no column cannot
// be re-read, so a field that leaked into a read shape would render empty on
// every row — and a field that leaked into a request body would let the caller
// state their own credential.
func TestARenderedRuntimeFieldIsOnNoOtherSurface(t *testing.T) {
	m := matrixModels(t)[theManualCase]
	if m == nil {
		t.Fatalf("%s is missing from the coverage matrix", theManualCase)
	}

	for path, src := range goSources(emitAll(t, m)) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		switch {
		// The aggregate declares it (a rule has to write it), and the manual rule
		// hook names it in the obligation it prints. Both are the point.
		case strings.Contains(path, "domain/"):
			continue
		// The label catalogs carry a line per DECLARED field, renderIn or not:
		// that is what a 422 puts in fieldLabel for a value a rule refused, and a
		// manual field is refused by rules like any other.
		case strings.Contains(path, "application/translations/"):
			continue
		case strings.HasSuffix(path, "application/commands/insert_conta_command.go"),
			strings.HasSuffix(path, "web/requests/insert_conta.go"):
			// The minting verb, asserted above. Its REQUEST is checked there too:
			// a manual field is on no write body at all.
			if req := requestStructOf(src); strings.Contains(req, theRenderedField) {
				t.Errorf("%s: the insert request carries %s — a caller would state their "+
					"own credential", path, theRenderedField)
			}
			continue
		}
		if strings.Contains(src, theRenderedField) {
			t.Errorf("%s mentions %s; renderIn names one verb's response and nothing else",
				path, theRenderedField)
		}
	}
}

// requestStructOf returns the Request declaration of a write DTO file, or "".
// The Response of the same file legitimately carries the field, so a whole-file
// Contains would answer the wrong question.
func requestStructOf(src string) string {
	for _, decl := range strings.Split(src, "\ntype ") {
		if i := strings.Index(decl, " struct {"); i > 0 && strings.HasSuffix(decl[:i], "Request") {
			return decl
		}
	}
	return ""
}
