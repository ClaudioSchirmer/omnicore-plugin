package compat

import "testing"

func TestVerdicts(t *testing.T) {
	// The fixtures below are written AGAINST a specific Supported value: which
	// pin counts as behind, exact or ahead only means anything relative to it.
	// So the value is asserted first — a bump that leaves this table behind
	// would otherwise keep passing while testing the wrong three relations.
	if Supported != "v0.53.0" {
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
		{"the supported line", "v0.53.0", false, Exact, false},
		{"same line, later patch", "v0.53.9", false, Exact, false},
		{"framework moved ahead", "v0.54.0", false, Ahead, false},
		{"project is one line older", "v0.52.9", false, Behind, true},
		{"project is older", "v0.48.0", false, Behind, true},
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
