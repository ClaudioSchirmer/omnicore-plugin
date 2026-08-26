package emit

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// Redaction, emitted.
//
// There is exactly one place a redaction changes what is written: the schema
// chain, where Field(go, column) becomes RedactedField(go, column, InSync,
// InAudit). Nothing above the schema knows — not the aggregate, not the
// commands, not the DTOs, not the migration, not the view — because the real
// value is still what the column holds and what the entity hydrates with. The
// mask exists only in the copies the framework makes of the row.
//
// The second place is a FILE rather than a call: a `hook` axis names a function
// the generator cannot write, in the same package as the schemas that call it.

// redactedFieldCall renders the schema call for one redacted field. indent is
// the leading tabs of a CONTINUATION line, the same contract compositeCall
// follows — the options go one per line, because a redaction is the kind of
// declaration a reviewer reads twice and a single 120-column line is read once.
func redactedFieldCall(f ir.Field, indent string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "RedactedField(%s, %s,", quote(f.Name), quote(f.Column))
	fmt.Fprintf(&b, "\n%s\tcore.InSync(%s),", indent, redactorExpr(f.Redaction.InSync, f))
	fmt.Fprintf(&b, "\n%s\tcore.InAudit(%s))", indent, redactorExpr(f.Redaction.InAudit, f))
	return b.String()
}

// redactorExpr renders one axis as the framework constructor it is.
func redactorExpr(r ir.Redactor, f ir.Field) string {
	switch r.Kind {
	case "fixed":
		return fmt.Sprintf("core.RedactWith(%s)", fixedLiteral(r.Value, f.SpecType))
	case "keep-last":
		return fmt.Sprintf("core.RedactKeepLast(%d)", r.Keep)
	case "hook":
		return fmt.Sprintf("core.RedactUsing(%s)", r.HookFunc)
	default:
		return "core.Plain()"
	}
}

// fixedLiteral renders the replacement in the COLUMN's own Go type.
//
// The type is not decoration: the framework compares it against the field's
// effective scalar at construction and panics on a mismatch, because a payload
// whose column changed type breaks the map the read side decodes through and
// the view's $jsonSchema with it. An untyped constant would be int and float64,
// which is wrong for exactly the two columns most likely to be masked with a
// zero — int64 money and a float score — so every literal is written typed.
func fixedLiteral(value, specType string) string {
	switch specType {
	case "int":
		return value
	case "int64":
		return fmt.Sprintf("int64(%s)", value)
	case "float64":
		return fmt.Sprintf("float64(%s)", value)
	case "bool":
		return value
	case "time":
		// Parsed here and written as a literal Date, so the schema carries no
		// parse that can fail at boot. The validator already refused anything
		// that is not RFC 3339, so the error branch is unreachable — and if it
		// ever is reached, a zero time is a worse lie than a visible fallback.
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return fmt.Sprintf("time.Time{} /* unparseable: %s */", value)
		}
		return fmt.Sprintf("time.Date(%d, %d, %d, %d, %d, %d, %d, time.UTC)",
			t.UTC().Year(), int(t.UTC().Month()), t.UTC().Day(),
			t.UTC().Hour(), t.UTC().Minute(), t.UTC().Second(), t.UTC().Nanosecond())
	default:
		return strconv.Quote(value)
	}
}

// schemaNeedsTime reports whether a schema file has to import time — true only
// when a fixed redactor replaces a timestamp, which is the one thing in the
// whole chain that is not a string, a number or a column name.
func schemaNeedsTime(fields ...[]ir.Field) bool {
	for _, run := range fields {
		for _, f := range run {
			if f.Redaction == nil {
				continue
			}
			for _, r := range []ir.Redactor{f.Redaction.InSync, f.Redaction.InAudit} {
				if r.Kind == "fixed" && f.SpecType == "time" {
					return true
				}
			}
		}
	}
	return false
}

// facetFields flattens the fields of every 1:1 facet attached to one node, so a
// question about "everything this schema file writes" can be asked once. The
// facets are declared INSIDE the owner's chain, so their fields reach the same
// file and the same import block.
func facetFields(m *ir.Model, node string) []ir.Field {
	var out []ir.Field
	for _, sib := range m.SiblingsOn(node) {
		out = append(out, sib.Fields...)
	}
	return out
}

// redactHookFile is where the hand-written redactors live. It sits beside the
// schemas that call them, in their package, because that is the only consumer:
// nothing above the schema knows a redaction exists.
func redactHookFile(m *ir.Model) string {
	return "internal/infra/schemas/" + m.Entity.Snake + "_redactors_manual.go"
}

// emitRedactHook writes the masks the family does not cover — one function per
// AXIS, because the two axes are independent by design and sharing a function
// between them is a decision the author makes by calling one from the other.
//
// It PANICS until it is written, and that is deliberate. The quiet alternative —
// a stub returning "***" — fails in the safe direction and is still the wrong
// answer, and this is the one place where a wrong answer is expensive to undo:
// the framework CANNOT see that a hook's body changed (a closure has no portable
// identity, so the view hash mixes in only the kind), so documents projected
// through a placeholder mask are not repaired by fixing the function. They are
// repaired by bumping the view's version and rebuilding, by hand, months later.
// A panic costs one failed write; a placeholder costs a rebuild nobody knows to
// run.
func emitRedactHook(m *ir.Model) (fsplan.File, error) {
	hooks := ir.RedactionHooks(m)
	s := &src{}
	s.Doc(
		"The masks this entity's redacted fields wear where the closed family does not "+
			"reach — the local half of an e-mail, a formatted document number, anything "+
			"whose shape is a rule rather than a length.",
		"",
		"Each function is handed the REAL value and returns what the copy carries. It "+
			"runs at two independent points: assembling the outbox payload inside the "+
			"write transaction, and composing the document in the composer — which is "+
			"also the REBUILD path, possibly months from now in a different binary. So "+
			"it must be PURE: no clock, no randomness, no mutable configuration. A "+
			"function that reads any of those makes a rebuilt document disagree with the "+
			"one the sync wrote, and nothing will report it.",
		"",
		"Two rules the framework enforces at runtime. Returning an EMPTY string for a "+
			"non-empty value panics, because an empty scalar reads as a removed facet row "+
			"in the payload contract — return a mask, never nothing. And a panic in here "+
			"is not recovered: it unwinds into the persister's rollback, so the write is "+
			"abandoned rather than completed with an unredacted value.",
		"",
		"Until a body is written the write FAILS, loudly, on the first row that carries "+
			"the field. That is the safe direction on purpose: a stub returning a "+
			"placeholder would mask more than you asked for and quietly project it, and "+
			"the framework cannot detect that the function later changed — fixing it then "+
			"means bumping the view's version and rebuilding by hand.",
	)
	s.Blank()
	s.L("package schemas")

	for _, h := range hooks {
		s.Blank()
		for _, line := range wrap(fmt.Sprintf("%s masks %s in %s.", h.HookFunc, h.Owner, axisReach(h.Axis)), 72) {
			s.L("// %s", line)
		}
		s.L("//")
		for _, line := range wrap(fmt.Sprintf("TODO(%s): implement. Return the MASK for v — never the empty "+
			"string, and never v itself unless this axis was meant to be plain.", h.HookFunc), 72) {
			s.L("// %s", line)
		}
		s.L("func %s(v string) string {", h.HookFunc)
		s.L("\tpanic(%s)", quote(fmt.Sprintf(
			"%s is not implemented yet — see the generation report", h.HookFunc)))
		s.L("}")
	}

	f, err := goFile(redactHookFile(m), fsplan.Hook,
		fmt.Sprintf("the hand-written redactors for %s (%d to implement)", m.Entity.Pascal, len(hooks)), s)
	if err != nil {
		return f, err
	}
	f.Consequence = "each of these PANICS until it is written: the first write carrying " +
		"the field is abandoned and rolled back. That is the safe direction — a stub " +
		"returning a placeholder would project a mask you did not ask for, and the " +
		"framework cannot see a hook's body change, so undoing it later means a view " +
		"version bump and a rebuild."
	return f, nil
}

// axisReach spells out what one axis governs, for the function's own comment.
func axisReach(axis string) string {
	if axis == "InAudit" {
		return "the audit event — the audit_events row, the slog echo and /audit"
	}
	return "the outbox payload — and with it the topic, every consuming service, " +
		"the failure ledgers and the projected document"
}
