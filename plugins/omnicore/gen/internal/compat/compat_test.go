package compat

import "testing"

func TestVerdicts(t *testing.T) {
	// The fixtures below are written AGAINST a specific Supported value: which
	// pin counts as behind, exact or ahead only means anything relative to it.
	// So the value is asserted first — a bump that leaves this table behind
	// would otherwise keep passing while testing the wrong three relations.
	if Supported != "v0.68.0" {
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
		{"the supported line", "v0.68.0", false, Exact, false},
		{"same line, later patch", "v0.68.9", false, Exact, false},
		{"framework moved ahead", "v0.69.0", false, Ahead, false},
		// One line older, and on THIS bump the block is no longer a posture: a
		// generated repository calls schemas.X().AsDirectSchema() on every read
		// join's target, and that method does not exist before v0.68.0 — the
		// tree does not compile, which is the honest failure the refusal
		// forecasts. (Before it, the one-line-back gap was only the Direct
		// write's upsert scoping, which the generator does not emit.) Older
		// lines break for their own reasons: v0.65.0's TableSchema panics at
		// boot on the StampedCounterField over *int64 a nullable
		// `stamped: counter` emits, and v0.64.0 has no StampedTimeField /
		// StampedCounterField at all and no `relational.clock` key. There is no
		// "same line, earlier patch" case while the supported line is a .0 — the
		// patch comparison is exercised by the later-patch row.
		{"project is one line older", "v0.67.0", false, Behind, true},
		{"project is one line older, later patch", "v0.67.1", false, Behind, true},
		{"project is two lines older", "v0.66.0", false, Behind, true},
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
