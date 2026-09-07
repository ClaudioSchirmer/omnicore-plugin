package compat

import (
	"strings"
	"testing"
)

func TestVerdicts(t *testing.T) {
	// The fixtures below are written AGAINST a specific Supported value: which
	// pin counts as behind, exact or ahead only means anything relative to it.
	// So the value is asserted first — a bump that leaves this table behind
	// would otherwise keep passing while testing the wrong three relations.
	if Supported != "v0.74.0" {
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
		{"the supported line", "v0.74.0", false, Exact, false},
		{"same line, later patch", "v0.74.9", false, Exact, false},
		{"framework moved ahead", "v0.75.0", false, Ahead, false},
		// The refusal reads the same at every distance, so what each distance
		// actually COSTS is written down here — the nearest lines are a POSTURE
		// and the ones below them are compile breaks, and treating those two as
		// one thing is how a bump gets waved through or panicked over.
		//
		// v0.73.0 is the nearest published line below the target, and it is a
		// compile break at zero distance: v0.74.0 renamed the managed archive
		// slot, so TableSchema.DeletedAt is gone and every generated schema that
		// declares storage.managed.archivedAt calls ArchivedAt instead. One call
		// is enough, and almost every entity makes it — which is why the refusal
		// at this distance is a compile break stated in advance rather than a
		// posture, and why it blocks by default.
		//
		// v0.72.1 and below add a SECOND compile break on top of that one:
		// v0.73.0's notification redesign removed the string-named
		// Rules.AddNotification, retyped ValidateEnum to take a field reference,
		// and moved an AggregateValueObject's BuildRules onto the pointer
		// receiver. Every generated entity, child, value object and composite
		// calls at least one of those.
		//
		// v0.72.1 was the first PATCH this generator required, and the reason is
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
		{"the last published line", "v0.73.0", false, Behind, true},
		{"one line older", "v0.72.1", false, Behind, true},
		{"same line, earlier patch", "v0.72.0", false, Behind, true},
		{"project is two lines older", "v0.71.0", false, Behind, true},
		{"two lines older, later patch", "v0.71.9", false, Behind, true},
		{"project is three lines older", "v0.70.0", false, Behind, true},
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

// TestUnpublishedTargetDoesNotSendTheAuthorShopping guards the one thing the
// SupportedIsPublished flag exists for. While the target has no tag, EVERY pin
// a project can declare is Behind — and the standing advice for Behind is
// "upgrade the framework", which would name a version nobody can fetch. The
// refusal has to say what actually works instead: a checkout.
func TestUnpublishedTargetDoesNotSendTheAuthorShopping(t *testing.T) {
	if SupportedIsPublished {
		t.Skip("Supported is published — the ordinary Behind advice is the correct one")
	}
	v := Evaluate("v0.72.1", false)
	if !v.Blocks {
		t.Fatal("a pin that cannot carry the emitted API must block")
	}
	if strings.Contains(v.Message, "/omnicore:upgrade") {
		t.Errorf("the refusal points at an upgrade that does not exist yet: %s", v.Message)
	}
	for _, want := range []string{"NOT PUBLISHED", "checkout"} {
		if !strings.Contains(v.Message, want) {
			t.Errorf("the refusal never says %q, so it does not say what to do: %s", want, v.Message)
		}
	}
	if v.Fix == "" {
		t.Error("a blocking verdict owes the caller a Fix line to render")
	}
}

// TestALocalCheckoutIsToldWhichCheckout: the checkout is the only thing that
// satisfies an unpublished target, and one parked at the last tag builds
// nothing — a wall of red that reads as a generator defect.
func TestALocalCheckoutIsToldWhichCheckout(t *testing.T) {
	if SupportedIsPublished {
		t.Skip("Supported is published — any checkout on the line will do")
	}
	if v := Evaluate("", true); !strings.Contains(v.Message, Supported) {
		t.Errorf("the local-checkout path never names the line it needs: %s", v.Message)
	}
}
