package emit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// Registration files are shared by every entity: the notification declarations
// and the seven translation catalogs. The generator INSERTS into them and
// removes from them; it never rewrites one WHOLE, because it carries other
// entities' content and, often, hand-written notes.
//
// Every merge here is idempotent by construction — it checks for what it is
// about to add. A merge that appended blindly would duplicate declarations on
// the second run, and the second run is the normal case.
//
// Replacing one declaration in place is a separate question from rewriting the
// file, and the answer is per declaration: `prior` carries the hash this entity
// recorded for each one last time it wrote it.
//
//	no prior record            → leave it, and report. Not knowing who wrote
//	                             something is not a licence to overwrite it.
//	prior == what is on disk   → the text is still the generator's own, so a
//	                             spec that moved may move it.
//	prior != what is on disk   → somebody edited it. It is theirs; report it.
//
// This exists because the alternative was found the hard way: a notification
// that gains a `tvars` entry needs a field on its struct, the rules emitted for
// it write `N{Max: "50"}`, and a stale declaration stops the package compiling
// with an error pointing at the rule instead of at the struct.

// MergeTypeDecls appends the declarations that are missing, preserving whatever
// is already there.
//
// The third return names the declarations that ARE there and no longer match
// what this spec describes. They are not rewritten — this file belongs to every
// entity in the project, and a declaration someone extended by hand is not the
// generator's to discard. But silence was worse than either option: a
// notification that gained a tvar needs a field on its struct, the emitted rule
// writes `N{Max: "50"}` for it, and the package stops compiling with an error
// that points at the rule rather than at the struct nobody updated. Naming it in
// the report is what turns that into a one-line fix.
func MergeTypeDecls(existing string, decls []TypeDecl, prior map[string]string) (string, bool, []string, map[string]string) {
	changed := false
	var stale []string
	written := map[string]string{}
	out := existing
	for _, d := range decls {
		if declaresType(out, d.Name) {
			onDisk, ok := declText(out, d.Name)
			switch {
			case !ok:
				// The file does not parse. The compiler says that better than a
				// drift note would, and rewriting into a broken file is worse.
			case normaliseDecl(onDisk) == normaliseDecl(d.Body):
				// Already what this spec says: record it, so a LATER change can
				// be recognised as ours even if this run wrote nothing.
				written[d.Name] = HashText(onDisk)
			case prior[d.Name] == HashText(onDisk):
				replaced, ok := replaceDecl(out, d)
				if !ok {
					stale = append(stale, d.Name)
					break
				}
				out = replaced
				written[d.Name] = HashText(d.Body)
				changed = true
			default:
				stale = append(stale, d.Name)
			}
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
		written[d.Name] = HashText(d.Body)
		changed = true
	}
	return out, changed, stale, written
}

// replaceDecl swaps one declaration's text in place, leaving its doc comment and
// everything around it untouched.
//
// The doc is deliberately not replaced: it is regenerated prose, and rewriting
// it would turn every wording change into a diff on a shared file for no gain.
func replaceDecl(existing string, d TypeDecl) (string, bool) {
	start, end, ok := declRange(existing, d.Name)
	if !ok {
		return existing, false
	}
	return existing[:start] + d.Body + existing[end:], true
}

// HashText is the identity of one registered declaration or catalog entry.
// Whitespace is normalised first, so gofmt realigning a struct — which it does
// the moment a longer field name arrives — is not read as somebody's edit.
func HashText(s string) string {
	sum := sha256.Sum256([]byte(normaliseDecl(s)))
	return hex.EncodeToString(sum[:])
}

// declText extracts the source of one type declaration, doc comment excluded.
func declText(src, name string) (string, bool) {
	start, end, ok := declRange(src, name)
	if !ok {
		return "", false
	}
	return src[start:end], true
}

// declRange locates one type declaration's source offsets, doc comment excluded.
func declRange(src, name string) (int, int, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return 0, 0, false
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name == nil || ts.Name.Name != name {
				continue
			}
			start := fset.Position(gd.TokPos).Offset
			end := fset.Position(gd.End()).Offset
			if start < 0 || end > len(src) || start >= end {
				return 0, 0, false
			}
			return start, end, true
		}
	}
	return 0, 0, false
}

// normaliseDecl collapses whitespace so gofmt's alignment — which changes when a
// LONGER field name arrives beside a short one — is not read as a difference.
func normaliseDecl(s string) string {
	return strings.Join(strings.Fields(s), " ")
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
// The fourth return names the keys that are present with DIFFERENT text from
// what the spec now says. Same reasoning as the declarations above, with the
// balance tipped further towards leaving them alone: a translator improving the
// wording is the expected case, so this is a note in the report and never an
// edit.
func MergeMapEntries(existing string, entries []MapEntry, prior map[string]string) (string, bool, []string, []string, map[string]string) {
	closing := findMapClose(existing)
	if closing < 0 {
		return existing, false, nil, nil, nil
	}
	var added, stale []string
	written := map[string]string{}
	changed := false
	var block strings.Builder
	for _, e := range entries {
		if hasMapKey(existing, e.Key) {
			onDisk, ok := mapValue(existing, e.Key)
			switch {
			case !ok:
				// Unreadable entry: leave it be.
			case onDisk == e.Value:
				written[e.Key] = HashText(onDisk)
			case prior[e.Key] == HashText(onDisk):
				replaced, ok := replaceMapValue(existing, e.Key, e.Value)
				if !ok {
					stale = append(stale, e.Key)
					break
				}
				existing = replaced
				closing = findMapClose(existing)
				written[e.Key] = HashText(e.Value)
				changed = true
			default:
				// Somebody rewrote this text. A translator improving the wording
				// is the EXPECTED case here, far more than it is for a struct, so
				// this branch is the one that matters most in this file.
				stale = append(stale, e.Key)
			}
			continue
		}
		block.WriteString(fmt.Sprintf("\t\t%q: %q,\n", e.Key, e.Value))
		added = append(added, e.Key)
		written[e.Key] = HashText(e.Value)
	}
	if block.Len() == 0 {
		return existing, changed, added, stale, written
	}
	// An EMPTY catalog writes its literal on one line (`return map[string]string{}`),
	// so the closing brace shares the line with the return. Inserting at the start
	// of that line would put the entries BEFORE the return and produce a file that
	// does not parse — which is exactly what a project with no catalogs yet gets.
	if isSameLineClose(existing, closing) {
		return existing[:closing] + "\n" + block.String() + "\t" + existing[closing:], true, added, stale, written
	}
	return existing[:closing] + block.String() + existing[closing:], true, added, stale, written
}

// mapValue reads back the text a catalog currently holds for a key.
func mapValue(src, key string) (string, bool) {
	m := mapEntryRe(key).FindStringSubmatch(src)
	if m == nil {
		return "", false
	}
	v, err := strconv.Unquote(m[1])
	if err != nil {
		return "", false
	}
	return v, true
}

// replaceMapValue swaps one key's text, leaving every other entry untouched.
func replaceMapValue(src, key, value string) (string, bool) {
	loc := mapEntryRe(key).FindStringSubmatchIndex(src)
	if loc == nil {
		return src, false
	}
	return src[:loc[2]] + fmt.Sprintf("%q", value) + src[loc[3]:], true
}

func mapEntryRe(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(fmt.Sprintf("%q", key)) +
		`\s*:\s*(".*")\s*,`)
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
		// A hand-added // comment may carry an unbalanced brace; counting it
		// either never closed the scan (and the merge silently dropped its
		// entries) or shifted the insertion point into the comment.
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			if nl := strings.IndexByte(src[i:], '\n'); nl >= 0 {
				i += nl
				continue
			}
			break
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
			"needs and maintains the text IT wrote, so a spec whose message changed is "+
			"followed here too. Improving a generated wording is still safe: the "+
			"generator records a hash of what it wrote, reads yours as different, and "+
			"leaves it — naming it in the report rather than reverting it.",
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
