package spec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load reads and strictly decodes a spec file. Unknown keys are errors, not
// warnings: a key the generator does not read is a key whose intent is silently
// dropped, which is exactly the failure INV-1 exists to prevent.
//
// It does NOT validate semantics — that is Validate, so that callers can report
// decode problems and semantic problems in distinct phases.
func Load(path string) (*Spec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}
	return Parse(raw, path)
}

// Parse decodes spec bytes. Exposed separately so tests and fixtures need no files.
func Parse(raw []byte, path string) (*Spec, error) {
	var s Spec
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		// An empty document decodes as io.EOF; say so plainly instead of
		// leaking the reader's error.
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: the spec file is empty", path)
		}
		return nil, translateDecodeError(err, path)
	}
	s.SourcePath = path
	return &s, nil
}

// unknownFieldRe matches yaml.v3's KnownFields complaint so it can be restated
// in the language of the spec instead of the language of Go structs.
var unknownFieldRe = regexp.MustCompile(`line (\d+): field (\S+) not found in type (\S+)`)

func translateDecodeError(err error, path string) error {
	msg := err.Error()
	var out []string
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "yaml: unmarshal errors:") {
			continue
		}
		if m := unknownFieldRe.FindStringSubmatch(line); m != nil {
			lineNo, key, goType := m[1], m[2], m[3]
			hint := ""
			if sugg := suggestKey(goType, key); sugg != "" {
				hint = fmt.Sprintf(" — did you mean %q?", sugg)
			}
			out = append(out, fmt.Sprintf(
				"%s:%s: unknown key %q in %s%s", path, lineNo, key, specSectionName(goType), hint))
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s", path, line))
	}
	if len(out) == 0 {
		return fmt.Errorf("%s: %s", path, msg)
	}
	return errors.New(strings.Join(out, "\n"))
}

// specSectionName renders a Go type name as the spec section a spec author
// recognises ("spec.Storage" → "the storage block").
func specSectionName(goType string) string {
	name := goType
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	switch name {
	case "Spec":
		return "the top level of the spec"
	default:
		return "the " + strings.ToLower(camelToWords(name)) + " block"
	}
}

func camelToWords(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// knownKeysOf reflects the yaml tags declared on a spec type so the suggestion
// list is derived from the types themselves and can never go stale.
func knownKeysOf(goType string) []string {
	name := goType
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	t, ok := specTypes[name]
	if !ok {
		return nil
	}
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("yaml")
		tag = strings.Split(tag, ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		keys = append(keys, tag)
	}
	sort.Strings(keys)
	return keys
}

func suggestKey(goType, given string) string {
	best, bestDist := "", 1<<30
	for _, k := range knownKeysOf(goType) {
		d := levenshtein(strings.ToLower(given), strings.ToLower(k))
		if d < bestDist {
			best, bestDist = k, d
		}
	}
	// Only suggest when the typo is plausibly a typo, not a different word.
	if best == "" || bestDist > 3 || bestDist >= len(best) {
		return ""
	}
	return best
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// specTypes is the reflection index used for key suggestions. Every struct in
// the spec language is registered here; the manifest test asserts none is missing.
var specTypes = map[string]reflect.Type{
	"Spec":          reflect.TypeOf(Spec{}),
	"Storage":       reflect.TypeOf(Storage{}),
	"Base":          reflect.TypeOf(Base{}),
	"Managed":       reflect.TypeOf(Managed{}),
	"Field":         reflect.TypeOf(Field{}),
	"FieldVO":       reflect.TypeOf(FieldVO{}),
	"Unique":        reflect.TypeOf(Unique{}),
	"ValueObject":   reflect.TypeOf(ValueObject{}),
	"EnumMember":    reflect.TypeOf(EnumMember{}),
	"Child":         reflect.TypeOf(Child{}),
	"Sibling":       reflect.TypeOf(Sibling{}),
	"Update":        reflect.TypeOf(Update{}),
	"Delete":        reflect.TypeOf(Delete{}),
	"Rules":         reflect.TypeOf(Rules{}),
	"Rule":          reflect.TypeOf(Rule{}),
	"ManualRule":    reflect.TypeOf(ManualRule{}),
	"Notification":  reflect.TypeOf(Notification{}),
	"Texts":         reflect.TypeOf(Texts{}),
	"Service":       reflect.TypeOf(Service{}),
	"Fact":          reflect.TypeOf(Fact{}),
	"Read":          reflect.TypeOf(Read{}),
	"View":          reflect.TypeOf(View{}),
	"Index":         reflect.TypeOf(Index{}),
	"ByParams":      reflect.TypeOf(ByParams{}),
	"Filter":        reflect.TypeOf(Filter{}),
	"Controls":      reflect.TypeOf(Controls{}),
	"FieldRestrict": reflect.TypeOf(FieldRestrict{}),
	"Surfaces":      reflect.TypeOf(Surfaces{}),
	"GraphQL":       reflect.TypeOf(GraphQL{}),
	"Exports":       reflect.TypeOf(Exports{}),
	"CSVExport":     reflect.TypeOf(CSVExport{}),
	"XLSXExport":    reflect.TypeOf(XLSXExport{}),
	"Authz":         reflect.TypeOf(Authz{}),
}
