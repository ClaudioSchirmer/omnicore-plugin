package emit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/gofile"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// wire.go is the one place an entity becomes REAL.
//
// Everything else can be generated perfectly and the service still serves
// nothing: the routes exist, the schema binds, the migration applies, and not
// one request reaches any of it — because the feature was never registered.
// Nothing in the compiler notices, and the boot is green.
//
// It is a registration site, so it is merged rather than rewritten: it belongs
// to the whole service and usually carries hand-written notes.
func mergeWire(m *ir.Model, root string) (*fsplan.File, error) {
	rel := findWireFile(root)
	if rel == "" {
		// No composition root to insert into. Not an error the generator can fix
		// — creating one would be guessing at a shape the service already has.
		return nil, nil
	}
	existing, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return nil, err
	}

	feature := m.Entity.PluralPascal + "Feature"
	ctor := "New" + feature
	src := string(existing)

	if registersFeature(src, ctor) {
		return nil, nil
	}

	merged, err := insertFeature(src, ctor)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	merged = ensureWireImports(merged, m)
	out, ferr := gofile.Finalize([]byte(merged))
	if ferr != nil {
		return nil, fmt.Errorf("%s: %w", rel, ferr)
	}
	return &fsplan.File{
		Path: rel, Class: fsplan.Registration, Content: out,
		Describes: fmt.Sprintf("the %s registration in the composition root", feature),
	}, nil
}

// growWiring adds the two fields an entity needs to the returned Wiring.
//
// Translations come along because the framework requires them the moment a
// feature exists: adding the feature alone would turn a working empty shell
// into one that refuses to boot, and the cause would read as unrelated.
func growWiring(src string) (string, error) {
	loc := wiringRe.FindStringIndex(src)
	if loc == nil {
		return "", fmt.Errorf("no bootstrap.Wiring literal found in the composition root — " +
			"an entity is registered by adding it to the Wiring that Wire returns")
	}
	at := loc[1]

	block := "\n\t\tFeatures: []bootstrap.Feature{},\n"
	// Translations come along ONLY when the Wiring does not carry them yet: a
	// hand-refactored root that already lists its catalogs (but has no Features
	// field) would otherwise get the key a second time — a compile error blamed
	// on a file the author wrote.
	if !strings.Contains(src, "Translations:") {
		block = "\n\t\t// The seven catalogs. The framework requires them as soon as a\n" +
			"\t\t// feature exists, so they arrive with the first entity.\n" +
			"\t\tTranslations: []translation.Module{\n" +
			"\t\t\tapptrans.PTBR(), apptrans.ENG(), apptrans.ESP(), apptrans.FRA(),\n" +
			"\t\t\tapptrans.DEU(), apptrans.ITA(), apptrans.NLD(),\n" +
			"\t\t},\n" + block
	}
	return src[:at] + block + src[at:], nil
}

func findWireFile(root string) string {
	for _, candidate := range []string{
		"bootstrap/wire.go", "wire.go", "cmd/wire.go", "internal/bootstrap/wire.go",
	} {
		if _, err := os.Stat(filepath.Join(root, candidate)); err == nil {
			return candidate
		}
	}
	return ""
}

// registersFeature detects an existing registration in EITHER form: constructed
// inline in the slice, or built into a local variable first.
//
// Only matching the inline form would make a re-run against a refactored file
// insert the feature a second time, and the service would mount every route
// twice.
func registersFeature(src, ctor string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(ctor) + `\s*\(`).MatchString(src)
}

// featuresRe finds the Features slice literal in the returned Wiring.
var featuresRe = regexp.MustCompile(`(?s)Features:\s*(\[\]bootstrap\.Feature)?\s*\{`)

// wiringRe finds the returned Wiring literal, so a shell that has no Features
// list yet can be given one.
var wiringRe = regexp.MustCompile(`(?s)bootstrap\.Wiring\{`)

func insertFeature(src, ctor string) (string, error) {
	loc := featuresRe.FindStringIndex(src)
	if loc == nil {
		// A freshly scaffolded service has no Features list at all — it is the
		// most common starting point, not an edge case. Refusing here would mean
		// the generator works on every service except a brand-new one.
		grown, err := growWiring(src)
		if err != nil {
			return "", err
		}
		src = grown
		loc = featuresRe.FindStringIndex(src)
		if loc == nil {
			return "", fmt.Errorf("could not add a Features list to the composition root")
		}
	}
	open := loc[1] - 1 // the '{' itself
	close := matchBrace(src, open)
	if close < 0 {
		return "", fmt.Errorf("the Features list is not closed")
	}

	entry := fmt.Sprintf("\t\t\t%s(d),\n", ctor)
	inner := strings.TrimSpace(src[open+1 : close])
	if inner == "" {
		// An empty list is written inline; open it out so the entry has a line.
		return src[:open+1] + "\n" + entry + "\t\t" + src[close:], nil
	}
	lineStart := strings.LastIndexByte(src[:close], '\n') + 1
	if strings.TrimSpace(src[lineStart:close]) != "" {
		return src[:close] + "\n" + entry + "\t\t" + src[close:], nil
	}
	return src[:lineStart] + entry + src[lineStart:], nil
}

// matchBrace returns the index of the brace closing the one at open.
func matchBrace(src string, open int) int {
	depth := 0
	inString := false
	var quote byte
	for i := open; i < len(src); i++ {
		c := src[i]
		if inString {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				inString = false
			}
			continue
		}
		// Same rule as findMapClose: a // comment's braces are not structure.
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			if nl := strings.IndexByte(src[i:], '\n'); nl >= 0 {
				i += nl
				continue
			}
			break
		}
		switch c {
		case '"', '`':
			inString, quote = true, c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// ensureWireImports adds what the registration needs, and only what is missing.
func ensureWireImports(src string, m *ir.Model) string {
	needed := []struct{ alias, path string }{
		{"", "github.com/ClaudioSchirmer/omnicore/application/translation"},
		{"apptrans", m.ImportPath("internal/application/translations")},
	}
	var missing []string
	for _, imp := range needed {
		if strings.Contains(src, `"`+imp.path+`"`) {
			continue
		}
		if imp.alias != "" {
			missing = append(missing, "\t"+imp.alias+" \""+imp.path+"\"\n")
		} else {
			missing = append(missing, "\t\""+imp.path+"\"\n")
		}
	}
	if len(missing) == 0 {
		return src
	}
	if i := strings.Index(src, "import (\n"); i >= 0 {
		at := i + len("import (\n")
		return src[:at] + strings.Join(missing, "") + src[at:]
	}
	// No parenthesised block — a root with a single `import "x"` statement, or
	// none at all. Open a block after the package clause; silently doing
	// nothing here handed Finalize a file referencing packages it never
	// imported, with the error pointing at the merge rather than the cause.
	if loc := packageClauseRe.FindStringIndex(src); loc != nil {
		return src[:loc[1]] + "\nimport (\n" + strings.Join(missing, "") + ")\n" + src[loc[1]:]
	}
	return src
}

var packageClauseRe = regexp.MustCompile(`(?m)^package\s+\w+\r?\n`)
