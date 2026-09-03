package compat

import "testing"

func TestVerdicts(t *testing.T) {
	// The fixtures below are written AGAINST a specific Supported value: which
	// pin counts as behind, exact or ahead only means anything relative to it.
	// So the value is asserted first — a bump that leaves this table behind
	// would otherwise keep passing while testing the wrong three relations.
	if Supported != "v0.72.1" {
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
		{"the supported line", "v0.72.1", false, Exact, false},
		{"same line, later patch", "v0.72.9", false, Exact, false},
		{"framework moved ahead", "v0.73.0", false, Ahead, false},
		// The refusal reads the same at every distance, so what each distance
		// actually COSTS is written down here — the three nearest lines are a
		// POSTURE and the ones below them are compile breaks, and treating those
		// two as one thing is how a bump gets waved through or panicked over.
		//
		// v0.72.1 is the first PATCH this generator has required, and the reason is
		// not a compile break — v0.72.0 emits and builds identically. What it costs
		// is a boundary a generated service declares and does not get: with
		// surfaces.graphql, a selection carrying __typename (which Apollo, urql and
		// Relay append to every selection set, so: effectively every real client)
		// dropped the projection, and with it ReadCriteria.Restrict's
		// FieldAccessForbiddenNotification — an explicitly selected restricted field
		// stopped answering 403 while the value was still scrubbed. A refusal that
		// only holds for documents no real client sends is not a refusal, so a
		// generated read side is refused below this patch rather than warned about.
		//
		// v0.72.0 is additive for generated code: it made the GraphQL doc surface
		// bypass authentication the way the Swagger one already did, and added
		// web.AuthOptions.PublicWhen and graphql.IsIntrospectionOnlyRequest, which
		// no emitter calls. So a project on v0.71.0 BUILDS — what it loses is that
		// a generated service exposing surfaces.graphql under auth.mode: jwt
		// serves a GraphiQL page that answers 401 and cannot fetch its own schema.
		//
		// v0.71.0 renamed tracing.SubPgx to SubRelational (its BREAKING change) and
		// retyped the filter-value coercion. Neither reaches the emitted tree: this
		// generator writes no tracing configuration and builds no FilterSpec by
		// hand. v0.70.0 moved the by-id and filter-value refusals INTO the fwweb
		// wrappers the emitters already call, so v0.69.0 still builds too; what it
		// loses is the CONTRACT the generated suite asserts — a malformed `:id`
		// answers 500 on a relational backing instead of 404/400, and a filter
		// value outside the leaf's type answers 500 instead of 400.
		//
		// The first HARD failure is v0.68.0: no client-ip on AppContext, which
		// `assignedFrom: client-ip` emits. Below it, v0.67.0 has no
		// AsDirectSchema(), which every read join target goes through; v0.65.0
		// TableSchema panics at boot on the StampedCounterField over *int64 a
		// nullable `stamped: counter` emits; and v0.64.0 has no
		// StampedTimeField / StampedCounterField at all and no
		// `relational.clock` key.
		{"same line, earlier patch", "v0.72.0", false, Behind, true},
		{"project is one line older", "v0.71.0", false, Behind, true},
		{"project is one line older, later patch", "v0.71.9", false, Behind, true},
		{"project is two lines older", "v0.70.0", false, Behind, true},
		{"project is at the first hard break", "v0.68.0", false, Behind, true},
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
