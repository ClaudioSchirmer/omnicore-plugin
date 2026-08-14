package emit

import (
	"fmt"
	"regexp"
	"strings"
)

// Registration files are shared by every entity: the notification declarations
// and the seven translation catalogs. The generator INSERTS into them and
// removes from them; it never rewrites them, because they carry other entities'
// content and, often, hand-written notes.
//
// Every merge here is idempotent by construction — it checks for what it is
// about to add. A merge that appended blindly would duplicate declarations on
// the second run, and the second run is the normal case.

// MergeTypeDecls appends the declarations that are missing, preserving whatever
// is already there.
func MergeTypeDecls(existing string, decls []TypeDecl) (string, bool) {
	changed := false
	out := existing
	for _, d := range decls {
		if declaresType(out, d.Name) {
			continue
		}
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += "\n"
		for _, line := range wrap(d.Doc, 76) {
			if line != "" {
				out += "// " + line + "\n"
			}
		}
		out += d.Body + "\n"
		changed = true
	}
	return out, changed
}

// TypeDecl is one declaration to register.
type TypeDecl struct {
	Name string
	Doc  string
	Body string
}

func declaresType(src, name string) bool {
	re := regexp.MustCompile(`(?m)^type\s+` + regexp.QuoteMeta(name) + `\s`)
	return re.MatchString(src)
}

// MergeMapEntries inserts key/value pairs into the LAST map literal of a
// catalog file, which is where a translation module returns its table.
//
// Keys already present are left alone rather than overwritten: a translator may
// well have improved the generated wording, and silently reverting that would
// be the worst kind of "helpful".
func MergeMapEntries(existing string, entries []MapEntry) (string, bool, []string) {
	closing := findMapClose(existing)
	if closing < 0 {
		return existing, false, nil
	}
	var added []string
	var block strings.Builder
	for _, e := range entries {
		if hasMapKey(existing, e.Key) {
			continue
		}
		block.WriteString(fmt.Sprintf("\t\t%q: %q,\n", e.Key, e.Value))
		added = append(added, e.Key)
	}
	if block.Len() == 0 {
		return existing, false, nil
	}
	// An EMPTY catalog writes its literal on one line (`return map[string]string{}`),
	// so the closing brace shares the line with the return. Inserting at the start
	// of that line would put the entries BEFORE the return and produce a file that
	// does not parse — which is exactly what a project with no catalogs yet gets.
	if isSameLineClose(existing, closing) {
		return existing[:closing] + "\n" + block.String() + "\t" + existing[closing:], true, added
	}
	return existing[:closing] + block.String() + existing[closing:], true, added
}

// isSameLineClose reports whether the insertion point sits mid-line, which is
// the single-line-literal case.
func isSameLineClose(src string, at int) bool {
	return at > 0 && src[at-1] != '\n'
}

// MapEntry is one translation key and its text.
type MapEntry struct {
	Key   string
	Value string
}

func hasMapKey(src, key string) bool {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(fmt.Sprintf("%q", key)) + `\s*:`)
	return re.MatchString(src)
}

// findMapClose locates the closing brace of the map literal a catalog returns.
// It scans for `map[string]string{` and tracks depth, so a value containing a
// brace cannot fool it the way a naive last-brace search would.
func findMapClose(src string) int {
	start := strings.Index(src, "map[string]string{")
	if start < 0 {
		return -1
	}
	depth := 0
	inString := false
	var quoteChar byte
	for i := start + len("map[string]string{") - 1; i < len(src); i++ {
		c := src[i]
		if inString {
			if c == '\\' {
				i++
				continue
			}
			if c == quoteChar {
				inString = false
			}
			continue
		}
		switch c {
		case '"', '`':
			inString, quoteChar = true, c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				// A multi-line literal closes on a line of its own, so the entries
				// go at the start of that line. An EMPTY catalog writes the whole
				// literal inline (`return map[string]string{}`) and has no such
				// line — returning the brace itself lets the caller open one.
				lineStart := strings.LastIndexByte(src[:i], '\n') + 1
				if strings.TrimSpace(src[lineStart:i]) != "" {
					return i
				}
				return lineStart
			}
		}
	}
	return -1
}

// catalogSkeleton is the file a project gets when it has no catalog yet.
func catalogSkeleton(lang, typeName, ctor, constName string) string {
	var s src
	s.Doc(
		fmt.Sprintf("%s is the %s translation catalog.", ctor, strings.ToUpper(lang)),
		"",
		"This file is a registration site: omnicore-gen inserts the keys an entity "+
			"needs and never rewrites what is already here. Improving a generated "+
			"wording is safe — the generator will not revert it.",
	)
	s.Blank()
	s.L("package translations")
	s.Blank()
	s.L("import (")
	s.L("\t%s", quote(fwImport("application/configuration")))
	s.L("\t%s", quote(fwImport("application/translation")))
	s.L(")")
	s.Blank()
	s.L("type %s struct{}", typeName)
	s.Blank()
	s.L("func %s() translation.Module { return %s{} }", ctor, typeName)
	s.Blank()
	s.L("func (%s) Language() configuration.Language { return configuration.%s }", typeName, constName)
	s.Blank()
	s.L("func (%s) Translations() map[string]string {", typeName)
	s.L("\treturn map[string]string{")
	s.L("\t}")
	s.L("}")
	return s.String()
}

// catalogs pairs each language with the identifiers the framework expects.
//
// The constants are NOT uniform with the file names, and that asymmetry is the
// trap: the catalogs are ptbr/eng/esp/fra/deu/ita/nld, but the framework's
// constants are LangPTBR, LangENG, LangES, LangFR, LangDE, LangIT, LangNL.
// Deriving one from the other by rule produces four identifiers that do not
// exist, so the pairing is written out.
var catalogs = []struct {
	Code      string
	Type      string
	Ctor      string
	LangConst string
}{
	{"ptbr", "ptbr", "PTBR", "LangPTBR"},
	{"eng", "eng", "ENG", "LangENG"},
	{"esp", "esp", "ESP", "LangES"},
	{"fra", "fra", "FRA", "LangFR"},
	{"deu", "deu", "DEU", "LangDE"},
	{"ita", "ita", "ITA", "LangIT"},
	{"nld", "nld", "NLD", "LangNL"},
}
