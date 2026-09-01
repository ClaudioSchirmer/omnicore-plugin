package emit

import (
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
)

// TestAJoinTargetIsReducedToItsOwnTable is the framework's own refusal, moved
// to where it costs a test run instead of a boot.
//
// A traversal puts the target in the FROM as ONE table under one alias, so the
// framework takes a DIRECT schema there and refuses a node it could only read
// in part — a target's children, facets and shared base live in tables the join
// never enters. Emitting the target as declared compiles perfectly and then
// panics at repository construction, which is the one failure this generator
// exists to make impossible.
//
// The CHILD of an InChild join is asserted the other way round on purpose. It
// is the JOINING side, not the target: its own facets are resolved exactly as
// the root's are on a root join, so reducing it would narrow what the foreign
// key is allowed to be.
func TestAJoinTargetIsReducedToItsOwnTable(t *testing.T) {
	models := matrixModels(t)
	models["child-join-time"] = childJoinTimeModel(t)

	declared := 0
	for name, m := range models {
		if len(m.Joins) == 0 {
			continue
		}
		t.Run(name, func(t *testing.T) {
			for _, head := range joinHeads(t, m) {
				declared++
				child, target, isInChild := splitJoinHead(head)
				if isInChild && strings.Contains(child, "AsDirectSchema") {
					t.Errorf("the CHILD of an InChild join is the joining side and must NOT be reduced:\n\t%s", head)
				}
				if !strings.Contains(target, ".AsDirectSchema()") {
					t.Errorf("a join target is one table and must be reduced with AsDirectSchema():\n\t%s", head)
				}
			}
		})
	}
	if declared == 0 {
		t.Fatal("no read join reached the emitted repositories, so this test proves nothing")
	}
}

// joinHeads returns the emitted `read.…Join(…)` line of every traversal the
// model declares, from the repository file that declares them.
func joinHeads(t *testing.T, m *ir.Model) []string {
	t.Helper()
	src := goSources(emitAll(t, m))["internal/infra/"+m.Entity.Snake+"_repository.go"]
	if src == "" {
		t.Fatalf("%s: no repository file was emitted at all", m.Entity.Pascal)
	}
	// An InChild head is emitted over two lines — the verb names the child, .To
	// names the target — so the second one is folded back in before the head is
	// judged as a whole.
	var out []string
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "read.InnerJoin") && !strings.Contains(line, "read.LeftJoin") {
			continue
		}
		head := strings.TrimSpace(line)
		if strings.Contains(head, "InChild") && !strings.Contains(head, ".To(") && i+1 < len(lines) {
			head += strings.TrimSpace(lines[i+1])
		}
		out = append(out, head)
	}
	// Every HOP is a call of its own — a chain renders as nested Then blocks — so
	// the count to match is the flattened one.
	hops := 0
	for _, j := range m.Joins {
		hops += len(j.Walk())
	}
	if len(out) != hops {
		t.Fatalf("%s: %d hops declared, %d emitted:\n%s",
			m.Entity.Pascal, hops, len(out), src)
	}
	return out
}

// splitJoinHead cuts one emitted head into the two schema arguments: the child
// (empty on a root join) and the target.
func splitJoinHead(head string) (child, target string, isInChild bool) {
	if i := strings.Index(head, ".To("); i >= 0 {
		return head[:i], head[i:], true
	}
	return "", head, false
}
