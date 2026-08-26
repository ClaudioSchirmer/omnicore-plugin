package ir

import (
	"fmt"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// Redaction is what a field's declared redaction becomes once resolved: the two
// mandatory axes, plus the name of the function a `hook` axis calls.
//
// It is resolved HERE rather than in the emitter because two consumers read it
// and they must agree to the letter: the schema emitter writes the call, and the
// report asks the author for the signature. Those were separate strings once,
// for the derivations, and a reviewer who copied the report's signature wrote a
// function nothing called.
type Redaction struct {
	InSync  Redactor
	InAudit Redactor
}

// Redactor is one axis. Kind is the spec's own word for it — plain, fixed,
// keep-last or hook — and the emitter maps that to the framework constructor.
type Redactor struct {
	Kind  string
	Value string
	Keep  int
	// HookFunc names the function a `hook` axis calls. Empty for every other
	// kind. It is DERIVED, not declared: a name in the spec would be a second
	// place to keep in step with the file the generator writes, and renaming it
	// there would leave the hook orphaned in a file nothing rewrites.
	HookFunc string
	// Owner and Axis are what the hook file's TODO says the function is for.
	Owner string
	Axis  string
}

// Active reports whether this axis actually transforms the value. `plain` does
// not, which is what makes "declared and deliberately in the clear" expressible
// without costing anything at runtime.
func (r Redactor) Active() bool { return r.Kind != "" && r.Kind != "plain" }

// IsHook reports whether this axis is answered by a hand-written function.
func (r Redactor) IsHook() bool { return r.Kind == "hook" }

// Declared reports whether the field carries a redaction at all.
func (r Redaction) Declared() bool { return r.InSync.Kind != "" || r.InAudit.Kind != "" }

// Hooks lists the axes of one redaction that need a hand-written function.
func (r Redaction) Hooks() []Redactor {
	var out []Redactor
	for _, a := range []Redactor{r.InSync, r.InAudit} {
		if a.IsHook() {
			out = append(out, a)
		}
	}
	return out
}

// resolveRedaction lowers the spec's block. owner is what the derived hook name
// is qualified by — the entity for a root, sibling or base field, the
// collection's own name for an entry's — and field is the LOGICAL name, which
// for a composite part is the name it is exposed under.
func resolveRedaction(r *spec.Redact, owner, field string) *Redaction {
	if r == nil {
		return nil
	}
	return &Redaction{
		InSync:  resolveRedactor(r.InSync, owner, field, "InSync"),
		InAudit: resolveRedactor(r.InAudit, owner, field, "InAudit"),
	}
}

func resolveRedactor(r *spec.Redactor, owner, field, axis string) Redactor {
	if r == nil {
		return Redactor{}
	}
	out := Redactor{Kind: r.Kind, Value: r.Value, Keep: r.Keep, Owner: owner + "." + field, Axis: axis}
	if out.IsHook() {
		out.HookFunc = fmt.Sprintf("redact%s%s%s", owner, field, axis)
	}
	return out
}

// RedactedField is one declaration, with the seat it was declared at spelled
// out — the report reads it, and "Phone" alone does not say whether it is the
// entity's, a facet's or one entry of a collection's.
type RedactedField struct {
	Seat  string
	Field Field
}

// RedactedFields lists every redaction the model carries, in a stable order:
// the root and its base, then the facets, then each collection.
func RedactedFields(m *Model) []RedactedField {
	var out []RedactedField
	add := func(seat string, fs []Field) {
		for _, f := range fs {
			if f.Redaction != nil {
				out = append(out, RedactedField{Seat: seat, Field: f})
			}
		}
	}
	add(m.Entity.Pascal, m.Fields)
	for _, s := range m.Siblings {
		add(m.Entity.Pascal+"."+s.Name+" (facet)", s.Fields)
	}
	for _, c := range m.Children {
		add(c.Name+" (collection entry)", c.Fields)
	}
	return out
}

// RedactionHooks are every hand-written redactor one model owes, across every
// seat a field can occupy: the root and its base, the facets of both, each
// collection entry and the facets of those.
//
// It is collected from the RESOLVED fields rather than from the spec so a part
// of a composite is counted under the name it is exposed by — the same name the
// emitted call and the hook file's function use.
func RedactionHooks(m *Model) []Redactor {
	var out []Redactor
	seen := map[string]bool{}
	add := func(fs []Field) {
		for _, f := range fs {
			if f.Redaction == nil {
				continue
			}
			for _, h := range f.Redaction.Hooks() {
				if seen[h.HookFunc] {
					continue
				}
				seen[h.HookFunc] = true
				out = append(out, h)
			}
		}
	}
	add(m.Fields)
	for _, s := range m.Siblings {
		add(s.Fields)
	}
	for _, c := range m.Children {
		add(c.Fields)
	}
	return out
}
