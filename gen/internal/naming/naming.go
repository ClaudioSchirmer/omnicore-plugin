// Package naming holds the identifier transforms the emitters share. They live
// in one place because a generator that spells the same concept two ways in two
// layers produces code that compiles and is wrong.
package naming

import "strings"

// Snake converts PascalCase to snake_case. Runs of capitals are treated as one
// word, so "ID" stays "id" and "HTTPServer" becomes "http_server".
func Snake(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if isUpper(r) {
			prevLower := i > 0 && !isUpper(runes[i-1])
			nextLower := i+1 < len(runes) && !isUpper(runes[i+1])
			if i > 0 && (prevLower || nextLower) {
				b.WriteByte('_')
			}
			b.WriteRune(toLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Camel lowercases the leading word: "EnrollmentNumber" → "enrollmentNumber",
// "ID" → "id". This is the JSON name of a field.
func Camel(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	if !isUpper(runes[0]) {
		return s
	}
	// A leading run of capitals lowercases as a unit ("IDNumber" → "idNumber").
	i := 0
	for i < len(runes) && isUpper(runes[i]) {
		i++
	}
	if i > 1 && i < len(runes) {
		i--
	}
	for j := 0; j < i; j++ {
		runes[j] = toLower(runes[j])
	}
	return string(runes)
}

// Pascal uppercases the first rune, leaving the rest alone.
func Pascal(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = toUpper(r[0])
	return string(r)
}

// Plural is deliberately conservative English pluralisation. It exists for
// route and collection names; anything it would get wrong is overridable in the
// spec, because guessing a plural is not worth a wrong URL.
func Plural(s string) string {
	if s == "" {
		return s
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasSuffix(lower, "s"), strings.HasSuffix(lower, "x"),
		strings.HasSuffix(lower, "z"), strings.HasSuffix(lower, "ch"),
		strings.HasSuffix(lower, "sh"):
		return s + "es"
	case strings.HasSuffix(lower, "y") && len(s) > 1 && !isVowel(rune(lower[len(lower)-2])):
		return s[:len(s)-1] + "ies"
	default:
		return s + "s"
	}
}

// Singular strips a trailing plural. It preserves the -us/-is endings that a
// naive rule mangles ("status" must not become "statu").
func Singular(s string) string {
	lower := strings.ToLower(s)
	switch {
	case strings.HasSuffix(lower, "us"), strings.HasSuffix(lower, "is"),
		strings.HasSuffix(lower, "ss"):
		return s
	case strings.HasSuffix(lower, "ies") && len(s) > 3:
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(lower, "es") && len(s) > 2:
		base := lower[:len(lower)-2]
		if strings.HasSuffix(base, "s") || strings.HasSuffix(base, "x") ||
			strings.HasSuffix(base, "z") || strings.HasSuffix(base, "ch") ||
			strings.HasSuffix(base, "sh") {
			return s[:len(s)-2]
		}
		return s[:len(s)-1]
	case strings.HasSuffix(lower, "s") && len(s) > 1:
		return s[:len(s)-1]
	default:
		return s
	}
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
func toLower(r rune) rune {
	if isUpper(r) {
		return r + ('a' - 'A')
	}
	return r
}
func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}
func isVowel(r rune) bool {
	switch r {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}
