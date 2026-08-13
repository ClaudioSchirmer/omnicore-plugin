package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/compat"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// CheckResult is the machine contract.
//
// It exists because `go run` collapses every exit code to 1, so a caller cannot
// learn the outcome from the process status. This JSON is therefore the ONLY
// authority on whether generation can proceed — and it is emitted on every
// path, including an error so early that nothing else ran.
type CheckResult struct {
	CanGenerate bool         `json:"canGenerate"`
	Entity      string       `json:"entity,omitempty"`
	Blockers    []Finding    `json:"blockers"`
	Warnings    []Finding    `json:"warnings"`
	Project     *ProjectInfo `json:"project,omitempty"`
	Compat      *CompatInfo  `json:"compat,omitempty"`
}

type Finding struct {
	Where   string `json:"where"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

type ProjectInfo struct {
	Root        string         `json:"root"`
	Module      string         `json:"module"`
	Dialects    []string       `json:"dialects"`
	HasMongo    bool           `json:"hasMongo"`
	NextOrdinal map[string]int `json:"nextOrdinal"`
	ExistingVOs []string       `json:"existingValueObjects"`
}

type CompatInfo struct {
	Level     string `json:"level"`
	Pinned    string `json:"pinned"`
	Supported string `json:"supported"`
	Message   string `json:"message"`
}

// CheckOptions carries what the caller chose.
type CheckOptions struct {
	SpecPath         string
	ProjectDir       string
	LangFallback     bool
	ForceUnsupported bool
	JSON             bool
}

// Check validates without writing anything. It always produces a result — an
// early failure becomes a blocker in the JSON rather than an empty stdout.
func Check(w io.Writer, opt CheckOptions) (CheckResult, error) {
	res := CheckResult{Blockers: []Finding{}, Warnings: []Finding{}}

	// The project must resolve BEFORE anything is judged: without it there is
	// no module path, no dialects and no framework pin, and a check that
	// approved a spec the real run would reject is the worst outcome there is.
	proj, err := discover.Find(opt.ProjectDir)
	if err != nil {
		res.Blockers = append(res.Blockers, Finding{Where: "project", Message: err.Error()})
		return finish(w, res, opt)
	}
	res.Project = &ProjectInfo{
		Root: proj.Root, Module: proj.ModulePath, Dialects: proj.Dialects,
		HasMongo: proj.HasMongo, NextOrdinal: proj.NextOrdinal, ExistingVOs: proj.ExistingVOs,
	}
	if len(proj.Dialects) == 0 {
		res.Blockers = append(res.Blockers, Finding{
			Where:   "project",
			Message: "no relational dialect could be discovered",
			Fix: "the generator reads relational.dialect from the microservice*.yaml profiles " +
				"and the migrations/<dialect>/ folders that hold SQL; without a target it " +
				"cannot write migrations",
		})
	}

	v := compat.Evaluate(proj.FrameworkVersion, proj.LocalCheckout)
	res.Compat = &CompatInfo{
		Level: string(v.Level), Pinned: v.Pinned, Supported: v.Supported, Message: v.Message,
	}
	switch {
	case v.Blocks && !opt.ForceUnsupported:
		res.Blockers = append(res.Blockers, Finding{Where: "framework", Message: v.Message,
			Fix: "upgrade the framework, or pass --force-unsupported"})
	case v.Level != compat.Exact:
		res.Warnings = append(res.Warnings, Finding{Where: "framework", Message: v.Message})
	}

	s, err := spec.Load(opt.SpecPath)
	if err != nil {
		res.Blockers = append(res.Blockers, Finding{Where: "spec", Message: err.Error()})
		return finish(w, res, opt)
	}
	res.Entity = s.Entity

	problems := spec.Validate(s, spec.Options{LangFallback: opt.LangFallback, ExistingVOs: proj.ExistingVOs})
	appendFindings(&res, problems)

	// Coverage runs only once the spec is otherwise sound: meeting "not in this
	// build yet" while the spec still has real errors buries the real errors.
	if !problems.HasBlockers() {
		appendFindings(&res, spec.CheckCoverage(s))
	}

	res.CanGenerate = len(res.Blockers) == 0
	return finish(w, res, opt)
}

func appendFindings(res *CheckResult, ps *spec.Problems) {
	for _, p := range ps.Blockers() {
		res.Blockers = append(res.Blockers, Finding{Where: p.Where, Message: p.Message, Fix: p.Fix})
	}
	for _, p := range ps.Warnings() {
		res.Warnings = append(res.Warnings, Finding{Where: p.Where, Message: p.Message, Fix: p.Fix})
	}
}

func finish(w io.Writer, res CheckResult, opt CheckOptions) (CheckResult, error) {
	res.CanGenerate = len(res.Blockers) == 0
	if opt.JSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return res, err
		}
		return res, nil
	}
	renderHuman(w, res)
	return res, nil
}

func renderHuman(w io.Writer, res CheckResult) {
	if res.Entity != "" {
		fmt.Fprintf(w, "Entity: %s\n", res.Entity)
	}
	if res.Project != nil {
		fmt.Fprintf(w, "Project: %s (%s)\n", res.Project.Module, res.Project.Root)
		fmt.Fprintf(w, "Dialects: %v\n", res.Project.Dialects)
	}
	if res.Compat != nil {
		fmt.Fprintf(w, "Framework: %s (supported %s.x) — %s\n",
			orNone(res.Compat.Pinned), res.Compat.Supported, res.Compat.Level)
	}
	fmt.Fprintln(w)

	if len(res.Blockers) > 0 {
		fmt.Fprintf(w, "%d blocker(s):\n", len(res.Blockers))
		for _, f := range res.Blockers {
			fmt.Fprintf(w, "  ✗ %s: %s\n", f.Where, f.Message)
			if f.Fix != "" {
				fmt.Fprintf(w, "      → %s\n", f.Fix)
			}
		}
		fmt.Fprintln(w)
	}
	if len(res.Warnings) > 0 {
		fmt.Fprintf(w, "%d warning(s):\n", len(res.Warnings))
		for _, f := range res.Warnings {
			fmt.Fprintf(w, "  ! %s: %s\n", f.Where, f.Message)
			if f.Fix != "" {
				fmt.Fprintf(w, "      → %s\n", f.Fix)
			}
		}
		fmt.Fprintln(w)
	}
	if res.CanGenerate {
		fmt.Fprintln(w, "✓ this spec can be generated")
	} else {
		fmt.Fprintln(w, "✗ generation is blocked — fix the blockers above")
	}
}

func orNone(s string) string {
	if s == "" {
		return "unresolved"
	}
	return s
}

// Fatal writes a last-resort JSON blocker. Used when the caller asked for JSON
// and something failed before Check could run, so stdout is never empty.
func Fatal(where, msg string, asJSON bool) {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(CheckResult{
			CanGenerate: false,
			Blockers:    []Finding{{Where: where, Message: msg}},
			Warnings:    []Finding{},
		})
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", where, msg)
}
