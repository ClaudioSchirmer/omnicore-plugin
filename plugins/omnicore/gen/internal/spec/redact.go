package spec

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Redaction, validated.
//
// The framework refuses a bad declaration too — every rule below has a
// construction panic behind it. That is not a reason to skip it here: a panic
// arrives at BOOT, after a whole tree has been generated, compiled and wired,
// and it names a Go call site rather than the line of the spec that asked for
// it. The spec is where the author is, so the spec is where the answer belongs.
//
// What this file does NOT do is decide policy. It refuses declarations the
// framework cannot honour and warns where a redaction promises less than it
// looks like it promises; which fields are sensitive is the author's call, and
// no default is invented for them.

// redactSeat describes where a redaction was declared, in the author's words.
// It is carried rather than derived because the same Field struct is reused at
// five seats and the message has to name the right one.
type redactSeat struct {
	// Root is true only for a field of the entity's own fields[] list — the one
	// seat where the shared base's natural key can be sitting.
	Root bool
	// Scalar is the field's effective persisted type, from the closed set: the
	// type behind a value object, which is what the redactor actually sees.
	Scalar string
	// Hidden reports whether the field is already out of every response body.
	Hidden bool
	// FieldName is the name the author wrote, for the read-side warning.
	FieldName string
	// LivesOn is the table the field is stored in, as the author declared it.
	// The only value that changes an answer here is "base": a role that REUSES
	// an identity declared elsewhere does not write that identity's schema, so a
	// redaction declared on one of its columns would be dropped on the floor.
	LivesOn string
}

// validateRedact checks one redaction declaration. where is the spec path of
// the FIELD (or part); the messages append the axis themselves.
func validateRedact(s *Spec, r *Redact, where string, seat redactSeat, ps *Problems) {
	if r == nil {
		return
	}
	if seat.Root && s.Storage.Base != nil && s.Storage.Base.NaturalKey != "" &&
		s.Storage.Base.NaturalKey == seat.FieldName {
		ps.BlockerFix(where+".redact",
			fmt.Sprintf("%q is the shared identity's natural key and cannot be redacted",
				seat.FieldName),
			"the identity's id is derived from this value IN THE CLEAR (UUIDv5 over a "+
				"fixed, public namespace) and travels in every payload as the document's "+
				"_id, so masking the column would hide nothing. If the value is sensitive "+
				"it is not the identity: deduplicate on a non-sensitive column and declare "+
				"the sensitive one as an ordinary redacted field beside it")
		return
	}
	if seat.LivesOn == "base" && s.Storage.Base != nil && s.Storage.Base.Reuse {
		ps.BlockerFix(where+".redact",
			"this spec REUSES the shared identity, so it does not write the identity's "+
				"schema — a redaction declared on one of its columns would be generated "+
				"nowhere",
			"declare it on the spec that owns the base (storage.base.reuse: false), where "+
				"the column is declared; every role over the identity then carries it, "+
				"because the base schema is one declaration shared by all of them")
		return
	}
	if seat.Scalar == "id" {
		ps.BlockerFix(where+".redact",
			"an `id` field cannot be redacted",
			"an identifier is what the projection and every consumer link rows BY — a "+
				"masked one points at nothing, and the row it identifies is disclosed by "+
				"whatever it is joined to anyway. Redact the sensitive value, not the key "+
				"that addresses it")
		return
	}

	validateRedactAxis(r.InSync, where, "inSync",
		"the outbox payload — and with it the topic, every consuming service, the two "+
			"failure ledgers and the projected document",
		seat, ps)
	validateRedactAxis(r.InAudit, where, "inAudit",
		"the audit event — the audit_events row, the slog echo and the /audit endpoint",
		seat, ps)

	if plainAxis(r.InSync) && plainAxis(r.InAudit) {
		ps.WarnFix(where+".redact",
			"both axes are plain, so this declaration masks nothing",
			"it is a legal way to say \"reviewed, and the real value belongs in both\" — "+
				"if that is not what you meant, one of the two axes wants a mask; if it is, "+
				"dropping the block entirely says the same thing with less to read")
	}

	// The asymmetry that surprises people, and the one thing the framework
	// genuinely does not do. Redaction governs the copies the framework MAKES;
	// a relational read model makes none — it selects the columns, which hold
	// the real value. So the same declaration that masks the topic and the audit
	// trail serves the value verbatim on this project's own API.
	// Only when inSync actually MASKS. An author who wrote inSync: {kind: plain}
	// has already said the payload carries the real value, so a read model that
	// serves it too is exactly what they asked for — warning there would be
	// crying wolf at the one declaration that cannot be surprised by this.
	if s.Read.Backing == "relational" && r.InSync != nil && r.InSync.Kind != "" &&
		!plainAxis(r.InSync) && !seat.Hidden && !restricted(s, seat.FieldName) {
		ps.WarnFix(where+".redact",
			"the read model is relational, so this field is still served IN THE CLEAR by "+
				"this service's own API",
			"redaction governs the copies the FRAMEWORK makes — the payload, the topic, "+
				"the consumers, the audit event — and a relational read model makes none: "+
				"it selects the column. Pair it with `hidden: true` (nobody receives the "+
				"field) or read.fieldRestrict (only callers holding a permission do), or "+
				"project through mongo, where the document IS the redacted payload")
	}
}

// plainAxis reports whether an axis is declared and transforms nothing.
func plainAxis(r *Redactor) bool { return r != nil && r.Kind == "plain" }

// restricted reports whether read.fieldRestrict already gates the field behind
// a permission.
func restricted(s *Spec, name string) bool {
	for _, fr := range s.Read.FieldRestrict {
		if fr.Field == name {
			return true
		}
	}
	return false
}

// validateRedactAxis checks ONE axis. governs is what that axis reaches, quoted
// into the missing-axis message so the fix is obvious rather than obedient.
func validateRedactAxis(r *Redactor, where, axis, governs string, seat redactSeat, ps *Problems) {
	w := where + ".redact." + axis
	if r == nil {
		ps.BlockerFix(where+".redact",
			fmt.Sprintf("%s is not declared, and both axes are mandatory", axis),
			fmt.Sprintf("%s carries %s. Nothing is defaulted here: the two answers a "+
				"default could pick are \"leak\" and \"guess\". Write %s: {kind: plain} to "+
				"keep the real value there, out loud", axis, governs, axis))
		return
	}
	if !RedactKinds.Has(r.Kind) {
		ps.BlockerFix(w+".kind",
			fmt.Sprintf("%q is not a redactor", r.Kind),
			"one of: "+RedactKinds.String())
		return
	}

	if r.Kind == "fixed" {
		if strings.TrimSpace(r.Value) == "" {
			ps.BlockerFix(w+".value",
				"a fixed redactor needs the value it writes",
				"declare value: \"***\" for a string, \"0\" for a number, \"false\" for a "+
					"bool. It is never empty and never null — an all-null column group reads "+
					"as a REMOVED facet row in the payload contract, and an empty string in "+
					"one reads the same way")
		} else {
			checkFixedValue(r.Value, seat.Scalar, w+".value", ps)
		}
	} else if r.Value != "" {
		ps.BlockerFix(w+".value",
			fmt.Sprintf("value is only read by kind: fixed, and this axis is %q", r.Kind),
			"drop it, or change the kind to fixed — a key the build ignores is a "+
				"redaction the author believes is in force")
	}

	if r.Kind == "keep-last" {
		if r.Keep <= 0 {
			ps.BlockerFix(w+".keep",
				"keep-last needs how many trailing runes stay visible",
				"declare keep: 4 — to mask the whole value use {kind: fixed, value: \"***\"}")
		}
	} else if r.Keep != 0 {
		ps.BlockerFix(w+".keep",
			fmt.Sprintf("keep is only read by kind: keep-last, and this axis is %q", r.Kind),
			"drop it, or change the kind to keep-last")
	}

	// keep-last and hook both work on TEXT: one counts runes, the other is
	// handed a string. A mask that changes the column's type breaks the type map
	// the read side decodes through and the view's $jsonSchema with it.
	if (r.Kind == "keep-last" || r.Kind == "hook") && seat.Scalar != "" && seat.Scalar != "string" {
		ps.BlockerFix(w+".kind",
			fmt.Sprintf("%s is string-only, and this field is persisted as %s", r.Kind, seat.Scalar),
			"a mask has to carry the column's own type, or the payload stops matching "+
				"what the read side decodes through and the view's $jsonSchema rejects the "+
				"document. Use {kind: fixed, value: …} with a value of that type")
	}
}

// checkFixedValue proves the replacement carries the column's own type. The
// framework checks the same thing at construction, against the Go scalar; doing
// it here is what turns "boot panic naming a builder call" into "line 41 of your
// spec".
func checkFixedValue(value, scalar, where string, ps *Problems) {
	var err error
	switch scalar {
	case "", "string":
		return
	case "int":
		_, err = strconv.Atoi(value)
	case "int64":
		_, err = strconv.ParseInt(value, 10, 64)
	case "float64":
		_, err = strconv.ParseFloat(value, 64)
	case "bool":
		_, err = strconv.ParseBool(value)
	case "time":
		_, err = time.Parse(time.RFC3339, value)
	default:
		return
	}
	if err == nil {
		return
	}
	hint := map[string]string{
		"int":     "a whole number, e.g. \"0\"",
		"int64":   "a whole number, e.g. \"0\"",
		"float64": "a number, e.g. \"0\"",
		"bool":    "true or false",
		"time":    "an RFC 3339 instant, e.g. \"1970-01-01T00:00:00Z\"",
	}[scalar]
	ps.BlockerFix(where,
		fmt.Sprintf("%q is not a %s, which is what this column holds", value, scalar),
		"the replacement must carry the column's own type or the payload breaks the "+
			"type map the read side decodes through — write "+hint)
}
