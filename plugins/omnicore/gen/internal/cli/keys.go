package cli

import (
	"fmt"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// explainKeys prints every key of the language.
//
// It answers the question an author has BEFORE they have a question: what can I
// configure at all? The other topics answer "what may this key hold" and "what
// does a spec look like", and both assume you already know the key is there.
// Without this, the way to find `unique.scope` was to guess its name — and what
// people do instead is work around the feature by hand.
func explainKeys() string {
	fields, err := spec.Keys()
	if err != nil {
		return fmt.Sprintf("the key reference could not be built: %v\n", err)
	}

	// Matched by SUFFIX, because the same key repeats inside a nested block:
	// `unique.scope` is as real under children[].fields[] as at the top level,
	// and showing its values in one place and not the other is how an author
	// concludes the nested one is a different, dumber key.
	vocab := spec.Vocabularies()
	setFor := func(path string) (spec.Vocabulary, bool) {
		for _, v := range vocab {
			if v.Path == path || strings.HasSuffix(path, "."+v.Path) {
				return v, true
			}
		}
		return spec.Vocabulary{}, false
	}

	var b strings.Builder
	b.WriteString("Every key of the spec language\n")
	b.WriteString("==============================\n\n")
	b.WriteString("Derived from the language definition itself, so it cannot fall behind what\n")
	b.WriteString("the loader accepts. Read it once before writing a spec: most of what looks\n")
	b.WriteString("like \"the generator cannot express this\" is a key whose name was not\n")
	b.WriteString("guessable.\n\n")
	b.WriteString("A key not listed here does not exist, and writing it is refused by name.\n")
	b.WriteString("A key marked REFUSED exists in the language and this build will not act on\n")
	b.WriteString("it — writing it is blocked, with the reason, before any code is written.\n\n")

	width := 0
	for _, f := range fields {
		if len(f.Path) > width {
			width = len(f.Path)
		}
	}
	if width > 44 {
		width = 44
	}

	refused := spec.RefusedKeys()
	for _, f := range fields {
		line := fmt.Sprintf("  %-*s  %s", width, f.Path, f.Type)
		if why, no := refused[f.Path]; no {
			// Marked, not hidden. Hiding it would make a spec written for a later
			// build look like it contains a typo; listing it unmarked sends an
			// author to write it, run, and get blocked — which is a round trip
			// this line costs nothing to save.
			line += "   REFUSED by this build"
			b.WriteString(line + "\n")
			for _, l := range wrapAt(why+" (see `explain coverage`)", 72) {
				fmt.Fprintf(&b, "  %-*s    %s\n", width, "", l)
			}
			continue
		}
		if v, ok := setFor(f.Path); ok {
			line += "   " + v.Set.String()
		}
		b.WriteString(line + "\n")
		if f.Doc != "" {
			for _, l := range wrapAt(f.Doc, 72) {
				fmt.Fprintf(&b, "  %-*s    %s\n", width, "", l)
			}
		}
	}

	b.WriteString("\nWhat a type means here: `[]X` is a list of blocks, `X` a block, and a bare\n")
	b.WriteString("scalar is one value. A key whose values are a closed set shows the set —\n")
	b.WriteString("`explain vocabulary` adds one line on what each choice decides.\n")
	return b.String()
}

// wrapAt breaks a line so the reference stays readable in a terminal. A key
// list that scrolls sideways is a key list nobody reads to the end.
func wrapAt(s string, width int) []string {
	var out []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && len(line)+1+len(w) > width {
			out = append(out, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
