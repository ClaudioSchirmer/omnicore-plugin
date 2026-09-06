// Package compat decides how this generator relates to the framework version a
// project pins.
//
// The posture is deliberate: floor == ceiling, one supported version, ONE shape
// of emitted code. Branching templates per framework version is the largest
// drift source a generator can have, so it is not done. But an unsupported
// version is a WARNING TO JUDGE, never a wall — the compiler is the real
// oracle, and most releases keep emitting code that builds.
package compat

import (
	"fmt"
	"strconv"
	"strings"
)

// Supported is the framework version this build targets and is proven against
// by the golden gate. Moving it is a deliberate act paired with reviewing the
// emitters — never a silent bump.
const Supported = "v0.73.0"

// SupportedIsPublished says whether Supported carries a tag anybody can pin
// yet. It is false while the emitters are written against the framework's
// unreleased line, and that case is NOT a detail of wording: no released
// version can satisfy the target, so every pin a project can actually declare
// is Behind, and the standing advice — upgrade the framework — names a version
// that does not exist. The only thing that works meanwhile is a checkout
// (go.mod replace, or the golden gate's OMNICORE_LOCAL), so that is what the
// refusal says instead.
//
// Flip it to true in the same commit that bumps Supported to a published tag,
// alongside testdata/host/go.mod.
const SupportedIsPublished = false

// Level is the verdict.
type Level string

const (
	// Exact: the project pins the supported line.
	Exact Level = "exact"
	// Ahead: the framework moved past this generator. Generation proceeds; the
	// author judges the changelog and the compiler decides.
	Ahead Level = "ahead"
	// Behind: the project pins an older line. Refused by default, because the
	// gap is usually an API that does not exist yet rather than a signature
	// that moved — but overridable.
	Behind Level = "behind"
	// Unknown: the pin could not be resolved (local checkout, offline proxy).
	// Never fatal.
	Unknown Level = "unknown"
)

// Verdict is the full compatibility answer, including the words to show.
type Verdict struct {
	Level     Level
	Pinned    string
	Supported string
	Message   string
	// Fix is the one action that resolves the verdict, for a caller rendering a
	// finding rather than the prose. Empty when there is nothing to do.
	Fix string
	// Blocks reports whether generation should stop absent an override.
	Blocks bool
}

// Evaluate compares the pinned version against the supported one.
func Evaluate(pinned string, localCheckout bool) Verdict {
	v := Verdict{Pinned: pinned, Supported: Supported}

	if localCheckout || pinned == "" || pinned == "(devel)" {
		v.Level = Unknown
		v.Message = "the framework resolves to a local checkout, so its released version is " +
			"unknown; generating anyway — the compiler is the oracle"
		if !SupportedIsPublished {
			// The checkout is the ONLY thing that can satisfy an unpublished
			// target, so it is worth saying which checkout: one parked at the
			// last tag builds nothing the emitters write, and the wall of red
			// that produces reads as a generator defect.
			v.Message += fmt.Sprintf(". This build targets the unpublished %s, so that "+
				"checkout has to be on the line carrying it — an older tag will not compile",
				Supported)
		}
		return v
	}

	pMaj, pMin, pPatch, okP := parseLine(pinned)
	sMaj, sMin, sPatch, okS := parseLine(Supported)
	if !okP || !okS {
		v.Level = Unknown
		v.Message = fmt.Sprintf("could not read the framework version %q; generating anyway", pinned)
		return v
	}

	switch {
	case pMaj == sMaj && pMin == sMin && pPatch >= sPatch:
		v.Level = Exact
		v.Message = fmt.Sprintf("framework %s meets the required %s", pinned, Supported)
	case pMaj > sMaj || (pMaj == sMaj && pMin > sMin):
		v.Level = Ahead
		v.Message = fmt.Sprintf(
			"the project pins framework %s, ahead of the %s this generator targets. "+
				"Generating anyway. Read the changelog of the pinned version and ask two "+
				"questions: was there a breaking change, and does it touch what the generator "+
				"emits? Then build — go vet and go build settle it faster than reading can. "+
				"A small fix is fine (adopt it with `omnicore-gen adopt <path>` so the next "+
				"run keeps it); a capability that changed shape entirely is a generator bump, "+
				"not a patch",
			pinned, Supported)
	case !SupportedIsPublished:
		// Behind, but not because the project fell behind: the target has no tag
		// yet, so this is where EVERY real pin lands. Sending the author to
		// /omnicore:upgrade would name a version nobody can fetch.
		v.Level = Behind
		v.Blocks = true
		v.Message = fmt.Sprintf(
			"this generator targets framework %s, which is NOT PUBLISHED yet — the project "+
				"pins %s, and no released version carries the API the emitters call. There is "+
				"nothing to upgrade to: until %s ships, the only tree that builds is one whose "+
				"go.mod replaces the framework with a checkout. Pass --force-unsupported to "+
				"generate anyway and judge the result yourself",
			Supported, pinned, Supported)
		v.Fix = fmt.Sprintf("wait for framework %s, or point go.mod at a framework checkout; "+
			"or pass --force-unsupported", Supported)
	default:
		v.Level = Behind
		v.Blocks = true
		v.Message = fmt.Sprintf(
			"the project pins framework %s, older than the %s this generator requires. "+
				"Going backwards the gap is usually an API that does not exist yet rather "+
				"than one that moved, so the reliable fix is to upgrade the framework "+
				"(/omnicore:upgrade). Pass --force-unsupported to generate anyway and judge "+
				"the result yourself",
			pinned, Supported)
		v.Fix = "upgrade the framework, or pass --force-unsupported"
	}
	return v
}

// parseLine reads the major.minor.patch of a vX.Y.Z tag. A missing patch reads
// as 0.
func parseLine(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, false
	}
	if len(parts) > 2 {
		if patch, err = strconv.Atoi(parts[2]); err != nil {
			return 0, 0, 0, false
		}
	}
	return major, minor, patch, true
}
