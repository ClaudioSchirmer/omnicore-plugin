// Package naming holds the identifier transforms the emitters share. They live
// in one place because a generator that spells the same concept two ways in two
// layers produces code that compiles and is wrong.
//
// Everything here CHANGES THE CASE of a name the author already gave. Nothing
// here invents a word. There is deliberately no pluraliser and no singulariser:
// a rule that turns Animal into Animals writes Animais nowhere, Person into
// Persons, Analysis into Analysiss — and the names it would produce are route
// paths, document keys and column names, which outlive the guess that made
// them. Every such name is declared in the spec.
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
