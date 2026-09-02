package compat

import "testing"

func TestVerdicts(t *testing.T) {
	// The fixtures below are written AGAINST a specific Supported value: which
	// pin counts as behind, exact or ahead only means anything relative to it.
	// So the value is asserted first — a bump that leaves this table behind
	// would otherwise keep passing while testing the wrong three relations.
	if Supported != "v0.70.0" {
		t.Fatalf("Supported moved to %s — move the fixtures below with it, then update "+
			"this guard; they only mean something relative to the supported line", Supported)
	}
	cases := []struct {
		name   string
		pin    string
		local  bool
		want   Level
		blocks bool
	}{
		{"the supported line", "v0.70.0", false, Exact, false},
		{"same line, later patch", "v0.70.9", false, Exact, false},
		{"framework moved ahead", "v0.71.0", false, Ahead, false},
		// One line older, and on THIS bump the block is a POSTURE rather than a
		// compile break — worth saying, because the two previous bumps were the
		// opposite and the refusal reads the same either way. v0.70.0 moved the
		// by-id and filter-value refusals INTO the fwweb wrappers the emitters
		// already call, so the emitted tree still builds on v0.69.0; what it
		// loses is the CONTRACT the generated suite asserts — a malformed `:id`
		// answers 500 on a relational backing instead of 404/400, and a filter
		// value outside the leaf's kind answers 500 instead of 400. The lines
		// below it are still hard failures: v0.68.0 has no client-ip on
		// AppContext, which `assignedFrom: client-ip` emits; v0.67.0 has no
		// AsDirectSchema(), which every read join target goes through; v0.65.0
		// TableSchema panics at boot on the StampedCounterField over *int64 a
		// nullable `stamped: counter` emits; and v0.64.0 has no
		// StampedTimeField / StampedCounterField at all and no
		// `relational.clock` key. There is no "same line, earlier patch" case
		// while the supported line is a .0 — the patch comparison is exercised
		// by the later-patch row.
		{"project is one line older", "v0.69.0", false, Behind, true},
		{"project is one line older, later patch", "v0.69.9", false, Behind, true},
		{"project is two lines older", "v0.68.0", false, Behind, true},
		{"project is older", "v0.49.0", false, Behind, true},
		{"local checkout", "", true, Unknown, false},
		{"devel", "(devel)", false, Unknown, false},
		{"garbage", "not-a-version", false, Unknown, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Evaluate(c.pin, c.local)
			if v.Level != c.want {
				t.Errorf("Evaluate(%q, %v).Level = %s, want %s", c.pin, c.local, v.Level, c.want)
			}
			if v.Blocks != c.blocks {
				t.Errorf("Evaluate(%q).Blocks = %v, want %v", c.pin, v.Blocks, c.blocks)
			}
			if v.Message == "" {
				t.Error("every verdict must carry a message the author can act on")
			}
		})
	}
}

// TestAheadNeverBlocks is the rule the maintainer set explicitly: a framework
// newer than the generator targets is a judgement call, not a wall.
func TestAheadNeverBlocks(t *testing.T) {
	if Evaluate("v9.99.0", false).Blocks {
		t.Error("a framework ahead of the supported line must never block generation")
	}
}

// TestUnknownNeverBlocks guards the offline / local-checkout path: an
// unresolvable pin must degrade, never abort.
func TestUnknownNeverBlocks(t *testing.T) {
	if Evaluate("", true).Blocks {
		t.Error("an unresolvable pin must not block generation")
	}
}
