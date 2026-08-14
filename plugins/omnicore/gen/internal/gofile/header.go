package gofile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// Every generated file carries a header that says where it came from and what
// it is, and carries its own checksum.
//
// The checksum is what makes "did a human change this?" answerable from the
// FILE, not from a side-car. That matters more than it sounds: with the answer
// living only in a lock file, deleting or losing that file makes the entire
// tree look hand-written, and the generator has to refuse everything or trust
// everything. With the answer in the file, each one speaks for itself — it
// survives being copied, moved, or arriving in a patch.
//
// Two details are not obvious and both would break something if done naively.
//
// A checksum cannot cover itself: writing it changes the bytes it was computed
// over. So the digest is taken with the checksum's own value blanked out, and
// verification blanks it again before recomputing.
//
// And the date cannot be "now" on every run. A file whose bytes change each
// time is a file that regenerates into a diff forever — the no-op regeneration
// stops being a no-op, and the guarantee that a spec and its output are in step
// becomes unverifiable. So the date advances only when the CONTENT changed.
const generatedBy = "omnicore-plugin-gen"

// Meta is what the header states.
type Meta struct {
	// Describes is the one-line purpose, in the words of the spec that asked for it.
	Describes string
	// Spec is the path of the spec this came from.
	Spec string
	// Entity names the aggregate, so a file found on its own is traceable.
	Entity string
	// Date is when the CONTENT was last generated (yyyy-mm-dd).
	Date string
	// Hook marks a file the generator writes once and never rewrites. Its header
	// says so, and it carries no checksum: hashing a file whose whole point is
	// to be edited would report drift on every edit, which is noise, not safety.
	Hook bool
	// Consequence states what happens while a hook file is still unwritten. The
	// two kinds differ sharply and must not be described alike: unwritten rules
	// leave invariants unenforced, which is quiet, while unwritten service facts
	// panic on first use, which is not. A reader deciding what to do next needs
	// to know which one they are holding.
	Consequence string
}

const blankChecksum = "0000000000000000000000000000000000000000000000000000000000000000"

// The header is not Go-specific. A migration is generated too, and a checksum
// that only covered the Go half would leave the SQL — the part that touches
// live data — as the one place a silent edit goes unnoticed.
const (
	GoComment  = "//"
	SQLComment = "--"
)

func checksumPrefixFor(c string) string { return c + " checksum:   sha256:" }

func checksumLineRe(c string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(c) + ` checksum:   sha256:[0-9a-f]{64}$`)
}

func dateLineRe(c string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(c) + ` generated:  (\d{4}-\d{2}-\d{2})$`)
}

// ApplyHeader prepends the header and seals the file with its own checksum.
//
// previous is the file currently on disk, or nil. It is read for one reason: to
// keep the recorded date when nothing of substance changed, so that
// regenerating an unchanged spec really does change nothing.
func ApplyHeader(content []byte, meta Meta, previous []byte) []byte {
	return ApplyHeaderWith(GoComment, content, meta, previous)
}

// ApplyHeaderWith is the same for a file whose comments are not Go's.
func ApplyHeaderWith(comment string, content []byte, meta Meta, previous []byte) []byte {
	if meta.Hook {
		return append([]byte(hookHeader(comment, meta)), content...)
	}

	body := string(content)
	head := ownedHeader(comment, meta, blankChecksum)
	candidate := head + body

	// Keep the previous date when the file is otherwise identical: a date that
	// moves on every run turns every regeneration into a diff.
	if len(previous) > 0 {
		if prevDate := recordedDate(comment, string(previous)); prevDate != "" {
			withPrevDate := ownedHeader(comment, withDate(meta, prevDate), blankChecksum) + body
			if normalize(stripChecksum(comment, withPrevDate)) ==
				normalize(stripChecksum(comment, candidate)) {
				candidate = withPrevDate
			}
		}
	}

	prefix := checksumPrefixFor(comment)
	sum := digest(comment, candidate)
	return []byte(strings.Replace(candidate, prefix+blankChecksum, prefix+sum, 1))
}

// VerifyHeader reports whether a file still matches the checksum it carries.
//
// A file with no checksum line answers false with tracked=false: it is not
// something this generator sealed, which is a different situation from one it
// sealed and someone changed.
func VerifyHeader(content []byte) (intact, tracked bool) {
	// Try both comment styles: the caller usually knows, but a verifier that has
	// to be told is one more place to pass the wrong thing.
	for _, c := range []string{GoComment, SQLComment} {
		if intact, tracked := verifyWith(c, content); tracked {
			return intact, true
		}
	}
	return false, false
}

func verifyWith(comment string, content []byte) (intact, tracked bool) {
	// Normalise FIRST: with CRLF endings the carriage return sits inside the
	// line, so an end-of-line anchor never matches and a Windows checkout would
	// look like a file this generator never wrote.
	s := normalize(string(content))
	re := checksumLineRe(comment)
	if !re.MatchString(s) {
		return false, false
	}
	recorded := strings.TrimPrefix(re.FindString(s), checksumPrefixFor(comment))
	return digest(comment, stripChecksum(comment, s)) == recorded, true
}

// digest hashes the file with the checksum field blanked and line endings
// normalised, so a checkout that rewrote CRLF does not read as an edit.
func digest(comment, s string) string {
	sum := sha256.Sum256([]byte(normalize(stripChecksum(comment, s))))
	return hex.EncodeToString(sum[:])
}

func stripChecksum(comment, s string) string {
	return checksumLineRe(comment).ReplaceAllString(s, checksumPrefixFor(comment)+blankChecksum)
}

func normalize(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

func recordedDate(comment, s string) string {
	m := dateLineRe(comment).FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}

func withDate(m Meta, date string) Meta {
	m.Date = date
	return m
}

func ownedHeader(c string, m Meta, checksum string) string {
	var b strings.Builder
	b.WriteString(c + " Code generated by " + generatedBy + ". DO NOT EDIT.\n")
	b.WriteString(c + "\n")
	for _, line := range wrapComment(m.Describes, 74) {
		b.WriteString(c + " " + line + "\n")
	}
	b.WriteString(c + "\n")
	fmt.Fprintf(&b, c+" entity:     %s\n", m.Entity)
	fmt.Fprintf(&b, c+" spec:       %s\n", m.Spec)
	fmt.Fprintf(&b, c+" generator:  %s\n", generatedBy)
	fmt.Fprintf(&b, c+" generated:  %s\n", m.Date)
	fmt.Fprintf(&b, "%s%s\n", checksumPrefixFor(c), checksum)
	b.WriteString(c + "\n")
	for _, line := range wrapComment(
		"The checksum covers this file with the checksum line itself blanked. The "+
			"generator recomputes it before every write: if it does not match, the file "+
			"was changed by hand and the run REFUSES it — your change is neither "+
			"overwritten nor updated. Change the spec and regenerate instead; to keep a "+
			"deliberate edit, run omnicore-gen adopt on this path.", 74) {
		b.WriteString(c + " " + line + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

func hookHeader(c string, m Meta) string {
	var b strings.Builder
	b.WriteString(c + " THIS FILE IS YOURS. Written once by " + generatedBy + ", never again.\n")
	b.WriteString(c + "\n")
	for _, line := range wrapComment(m.Describes, 74) {
		b.WriteString(c + " " + line + "\n")
	}
	b.WriteString(c + "\n")
	fmt.Fprintf(&b, c+" entity:     %s\n", m.Entity)
	fmt.Fprintf(&b, c+" spec:       %s\n", m.Spec)
	fmt.Fprintf(&b, c+" generator:  %s (created this file, does not maintain it)\n", generatedBy)
	fmt.Fprintf(&b, c+" created:    %s\n", m.Date)
	b.WriteString(c + "\n")
	if m.Consequence != "" {
		for _, line := range wrapComment(m.Consequence, 74) {
			b.WriteString(c + " " + line + "\n")
		}
		b.WriteString(c + "\n")
	}
	for _, line := range wrapComment(
		"There is no checksum here on purpose: this file exists to be edited, so "+
			"hashing it would report drift every time you did the thing it is for.", 74) {
		b.WriteString(c + " " + line + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

func wrapComment(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			out = append(out, line)
			line = w
			continue
		}
		line += " " + w
	}
	return append(out, line)
}
