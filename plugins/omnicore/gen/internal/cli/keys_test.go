package cli

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// TestExplainKeysShowsEachKeysOwnSet proves the key reference never advertises a
// value the validator refuses by name.
//
// Sets are matched to key paths by SUFFIX, and that is deliberate: it is what
// lets `children[].fields[].unique.scope` answer with the same values as the
// top-level key of the same name, instead of reading as a different, dumber key.
// The hazard is the nested key whose set genuinely DIFFERS from its outer
// namesake — `joins[].fields[].type` is `fields[].type` minus `id`, because a
// join field carries no domain type. Under a first-match-wins suffix rule it
// inherited the outer set and offered `id`, which `joins[].fields[].type` blocks
// with a blocker. So every path the vocabulary states EXACTLY must render ITS
// OWN set here.
func TestExplainKeysShowsEachKeysOwnSet(t *testing.T) {
	out := explainKeys()
	checked := 0
	for _, v := range spec.Vocabularies() {
		line, ok := keyLineOf(out, v.Path)
		switch {
		case !ok:
			// A vocabulary path that is not a key of the language on its own
			// (it is only reachable nested) has no line to check.
			continue
		case strings.Contains(line, "REFUSED by this build"):
			// A refused key prints the reason instead of the set, on purpose.
			continue
		}
		checked++
		if got := strings.TrimRight(line, " "); !strings.HasSuffix(got, v.Set.String()) {
			t.Errorf("`explain keys` renders %s as\n  %s\nbut its own set is %s",
				v.Path, got, v.Set.String())
		}
	}
	if checked == 0 {
		t.Fatal("no vocabulary path matched a key line — the reference or the paths moved")
	}
}

// keyLineOf finds the reference line for one key path. The line starts with the
// path, padded, so the first whitespace-separated token is the path itself.
func keyLineOf(out, path string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == path {
			return line, true
		}
	}
	return "", false
}
