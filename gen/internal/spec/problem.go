package spec

import (
	"fmt"
	"sort"
	"strings"
)

// Severity separates "the generator refuses" from "the generator proceeds and
// says so". There is no third level: a thing worth mentioning is either a
// blocker or a warning, never a silent note.
type Severity string

const (
	Blocker Severity = "blocker"
	Warning Severity = "warning"
)

// Problem is one finding, addressed to the person who wrote the spec. Where
// points at the spec location in the author's own vocabulary (`fields[2].type`),
// never at a Go struct field.
type Problem struct {
	Severity Severity
	Where    string
	Message  string
	// Fix is the concrete action, when there is exactly one. Omitted when the
	// author has a real choice to make.
	Fix string
}

func (p Problem) String() string {
	s := fmt.Sprintf("%s: %s", p.Where, p.Message)
	if p.Fix != "" {
		s += " → " + p.Fix
	}
	return s
}

// Problems accumulates findings during validation.
type Problems struct {
	items []Problem
}

func (ps *Problems) Blockerf(where, format string, args ...any) {
	ps.items = append(ps.items, Problem{Severity: Blocker, Where: where, Message: fmt.Sprintf(format, args...)})
}

func (ps *Problems) BlockerFix(where, msg, fix string) {
	ps.items = append(ps.items, Problem{Severity: Blocker, Where: where, Message: msg, Fix: fix})
}

func (ps *Problems) Warnf(where, format string, args ...any) {
	ps.items = append(ps.items, Problem{Severity: Warning, Where: where, Message: fmt.Sprintf(format, args...)})
}

func (ps *Problems) WarnFix(where, msg, fix string) {
	ps.items = append(ps.items, Problem{Severity: Warning, Where: where, Message: msg, Fix: fix})
}

func (ps *Problems) Items() []Problem { return ps.items }

func (ps *Problems) Blockers() []Problem { return ps.filter(Blocker) }

func (ps *Problems) Warnings() []Problem { return ps.filter(Warning) }

func (ps *Problems) filter(s Severity) []Problem {
	var out []Problem
	for _, p := range ps.items {
		if p.Severity == s {
			out = append(out, p)
		}
	}
	return out
}

func (ps *Problems) HasBlockers() bool { return len(ps.Blockers()) > 0 }

// Sort orders findings by location so a re-run reports them the same way —
// unstable ordering makes diffs of the report meaningless.
func (ps *Problems) Sort() {
	sort.SliceStable(ps.items, func(i, j int) bool {
		if ps.items[i].Severity != ps.items[j].Severity {
			return ps.items[i].Severity == Blocker
		}
		return ps.items[i].Where < ps.items[j].Where
	})
}

// Error renders the blockers as a single error, or nil when there are none.
func (ps *Problems) Error() error {
	b := ps.Blockers()
	if len(b) == 0 {
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "the spec has %d problem(s):", len(b))
	for _, p := range b {
		sb.WriteString("\n  • " + p.String())
	}
	return fmt.Errorf("%s", sb.String())
}
