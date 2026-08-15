package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/compat"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/emit"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/gofile"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/report"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// GenerateOptions carries the caller's choices.
type GenerateOptions struct {
	SpecPath         string
	ProjectDir       string
	LangFallback     bool
	ForceUnsupported bool
	DryRun           bool
	Force            map[string]bool
	// Clock is injectable so the golden gate can pin the date and still assert
	// that a regeneration changes nothing.
	Clock func() time.Time
}

// Now is the clock, defaulting to the real one.
func (o GenerateOptions) Now() time.Time {
	if o.Clock != nil {
		return o.Clock()
	}
	return time.Now()
}

// Generate validates, resolves, emits and writes.
//
// Validation is repeated here rather than trusted from a previous `check`: the
// spec may have changed in between, and a generate that ran on a spec nobody
// validated is exactly the failure the check exists to prevent.
func Generate(w io.Writer, opt GenerateOptions) error {
	proj, err := discover.Find(opt.ProjectDir)
	if err != nil {
		return err
	}
	// The same refusal check makes: without a dialect this used to proceed and
	// emit ZERO migration files, with the report claiming nothing was skipped.
	if len(proj.Dialects) == 0 {
		return fmt.Errorf("no relational dialect could be discovered — the generator " +
			"reads relational.dialect from the microservice*.yaml profiles and the " +
			"migrations/<dialect>/ folders that hold SQL; without a target it cannot " +
			"write migrations")
	}

	s, err := spec.Load(opt.SpecPath)
	if err != nil {
		return err
	}
	problems := spec.Validate(s, spec.Options{LangFallback: opt.LangFallback, ExistingVOs: proj.ExistingVOs, VOOwner: proj.VOOwner, Neighbours: neighboursOf(proj)})
	if problems.HasBlockers() {
		return problems.Error()
	}
	if cov := spec.CheckCoverage(s); cov.HasBlockers() {
		return cov.Error()
	}

	verdict := compat.Evaluate(proj.FrameworkVersion, proj.LocalCheckout)
	if verdict.Blocks && !opt.ForceUnsupported {
		return fmt.Errorf("%s", verdict.Message)
	}

	lock, err := fsplan.LoadLock(proj.Root)
	if err != nil {
		return err
	}

	model, err := ir.Resolve(s, proj)
	if err != nil {
		return err
	}
	// An entity that was generated before keeps the migration number it already
	// owns; only a new one allocates.
	for dialect, n := range lock.OrdinalsOf(model.Entity.Pascal) {
		model.Ordinal[dialect] = n
	}

	// A projected shape that changed while its version did not is a boot abort,
	// and INV-3 says a boot trap is a static error. The generator is the only
	// party that can see BOTH shapes — the one it just resolved and the one it
	// wrote last time — so it is the only one that can say this before the
	// service is started.
	view := fsplan.ViewState{
		Shape:   fsplan.Hash([]byte(emit.ViewShape(model))),
		Version: model.Read.Version,
	}
	if was, changed := lock.ViewShapeChangedWithoutBump(model.Entity.Pascal, view); changed {
		return fmt.Errorf(
			"the read view %q projects a different shape than the last generation, and "+
				"read.view.version is still %d.\n\n"+
				"The framework compares the declared version against what is stored and "+
				"REFUSES TO BOOT rather than serve a projection built to an older shape, so "+
				"generating this would produce a tree that does not start.\n\n"+
				"  → bump read.view.version to %d\n\n"+
				"On a Mongo backing that bump is also what triggers the rebuild; on a "+
				"relational one it is what tells a reader the shape moved.",
			model.Read.ViewName, was, was+1)
	}

	specRel, _ := filepath.Rel(proj.Root, opt.SpecPath)
	if specRel == "" {
		specRel = opt.SpecPath
	}

	result, err := emit.All(model, proj.Root, emit.FileMeta{
		Spec:   specRel,
		Entity: model.Entity.Pascal,
		Date:   opt.Now().Format("2006-01-02"),
	})
	if err != nil {
		return err
	}

	decisions, err := fsplan.Plan(proj.Root, model.Entity.Pascal, result.Files, lock, opt.Force)
	if err != nil {
		return err
	}
	orphans := fsplan.Orphans(model.Entity.Pascal, result.Files, lock)
	migrationsKept := keptMigrations(decisions)

	if opt.DryRun {
		fmt.Fprintf(w, "Dry run — nothing was written.\n\n")
		for _, d := range decisions {
			fmt.Fprintf(w, "  %-16s %s\n", d.Action, d.File.Path)
		}
		return nil
	}

	specBytes, _ := os.ReadFile(opt.SpecPath)
	if err := fsplan.Apply(proj.Root, model.Entity.Pascal, specRel,
		fsplan.Hash(specBytes), proj.FrameworkVersion, model.Ordinal, view, decisions, lock); err != nil {
		return err
	}

	var warnings []string
	for _, p := range problems.Warnings() {
		warnings = append(warnings, p.Where+": "+p.Message)
	}

	md := report.Render(report.Input{
		Model: model, SpecPath: specRel, Decisions: decisions,
		MissingTranslations: result.MissingTranslations,
		MigrationsKept:      migrationsKept,
		TargetTables:        result.TargetTables,
		Orphans:             orphans,
		CompatLevel:         string(verdict.Level),
		CompatMessage:       verdict.Message,
		FrameworkPinned:     verdict.Pinned,
		Warnings:            warnings,
	})
	reportPath := filepath.Join(filepath.Dir(opt.SpecPath),
		model.Entity.Snake+".gen-report.md")
	if err := os.WriteFile(reportPath, []byte(md), 0o644); err != nil {
		return fmt.Errorf("writing the report: %w", err)
	}

	summarise(w, model.Entity.Pascal, decisions, reportPath)
	return nil
}

// summarise prints a short outcome. The detail lives in the report; repeating
// it here would only make the terminal the place people read instead.
func summarise(w io.Writer, entity string, decisions []fsplan.Decision, reportPath string) {
	counts := map[fsplan.Action]int{}
	var refused, kept []fsplan.Decision
	for _, d := range decisions {
		counts[d.Action]++
		switch d.Action {
		case fsplan.RefusedEdited:
			refused = append(refused, d)
		case fsplan.KeptHook:
			kept = append(kept, d)
		}
	}

	fmt.Fprintf(w, "%s generated.\n\n", entity)
	fmt.Fprintf(w, "  created    %d\n", counts[fsplan.Create])
	fmt.Fprintf(w, "  updated    %d\n", counts[fsplan.Update])
	fmt.Fprintf(w, "  unchanged  %d\n", counts[fsplan.Unchanged])

	if len(kept) > 0 {
		fmt.Fprintf(w, "  kept as-is %d (yours, by design)\n", len(kept))
	}
	if len(refused) > 0 {
		fmt.Fprintf(w, "\n%d file(s) were REFUSED because they changed since they were generated.\n",
			len(refused))
		fmt.Fprintf(w, "Nothing was overwritten:\n")
		for _, d := range refused {
			fmt.Fprintf(w, "  · %s\n", d.File.Path)
		}
	}
	fmt.Fprintf(w, "\nRead %s — it lists what still needs implementing.\n", reportPath)
}

// Adopt records a hand fix so regeneration preserves it.
func Adopt(w io.Writer, projectDir, path, why string) error {
	proj, err := discover.Find(projectDir)
	if err != nil {
		return err
	}
	lock, err := fsplan.LoadLock(proj.Root)
	if err != nil {
		return err
	}
	rel := path
	if filepath.IsAbs(path) {
		if r, err := filepath.Rel(proj.Root, path); err == nil {
			rel = r
		}
	}
	// The owning entity is found by looking for the file, so the caller does
	// not have to name it. Names are walked in order so a file two entities
	// share (a registration) is attributed the same way on every run, instead
	// of by map luck.
	entity := ""
	for _, name := range sortedEntityNames(lock) {
		if _, ok := lock.Entities[name].Files[rel]; ok {
			entity = name
			break
		}
	}
	if entity == "" {
		return fmt.Errorf("%s is not a file this generator wrote — "+
			"only a generated file can carry an adopted fix", rel)
	}
	fw := proj.FrameworkVersion
	if fw == "" {
		fw = "unresolved"
	}
	if err := fsplan.Adopt(proj.Root, entity, rel, fw, why, lock); err != nil {
		return err
	}
	fmt.Fprintf(w, "Adopted %s against framework %s.\n", rel, fw)
	fmt.Fprintf(w, "Regeneration will now preserve it instead of refusing it.\n")
	return nil
}

// Doctor reports drift without changing anything.
func Doctor(w io.Writer, projectDir string) error {
	proj, err := discover.Find(projectDir)
	if err != nil {
		return err
	}
	lock, err := fsplan.LoadLock(proj.Root)
	if err != nil {
		return err
	}
	if len(lock.Entities) == 0 {
		fmt.Fprintln(w, "No generated entity is recorded in this project.")
		return nil
	}

	// Sorted, so two runs of doctor tell the same story in the same order.
	for _, name := range sortedEntityNames(lock) {
		e := lock.Entities[name]
		fmt.Fprintf(w, "%s (spec %s, framework %s)\n", name, e.Spec, orNone(e.Framework))
		specPath := filepath.Join(proj.Root, e.Spec)
		if b, err := os.ReadFile(specPath); err == nil {
			if fsplan.Hash(b) != e.SpecHash {
				fmt.Fprintf(w, "  ! the spec changed since the last generation — "+
					"regenerate to bring the code back in line\n")
			}
		} else {
			fmt.Fprintf(w, "  ! the spec is missing at %s\n", e.Spec)
		}

		var paths []string
		for path := range e.Files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			rec := e.Files[path]
			content, err := os.ReadFile(filepath.Join(proj.Root, path))
			switch {
			case err != nil:
				fmt.Fprintf(w, "  ! %s is gone\n", path)
			case rec.AdjustedFor != "":
				line := fmt.Sprintf("  · %s carries a hand edit adopted at framework %s", path, rec.AdjustedFor)
				if rec.Why != "" {
					line += " — " + rec.Why
				}
				fmt.Fprintln(w, line+"\n      (it no longer tracks the spec: emitter improvements will not reach it)")
			default:
				// The FILE decides, through the checksum in its own header —
				// the same authority `generate` refuses on. The lock's hash is a
				// record of what one entity last wrote, and a file two entities
				// legitimately share (the vos package doc) goes stale in the
				// first one's record the moment the second regenerates. Judging
				// by that reported a hand edit nobody had made, on the one
				// command whose entire job is to tell the truth about drift —
				// and the fix it implies is `adopt`, which would have frozen a
				// perfectly current file out of future improvements.
				if intact, sealed := gofile.VerifyHeader(content); sealed && !intact {
					fmt.Fprintf(w, "  ! %s was edited by hand — regeneration will refuse it\n", path)
				} else if !sealed {
					fmt.Fprintf(w, "  ! %s carries no generator checksum — regeneration will refuse it\n", path)
				}
			}
		}
	}
	return nil
}

func sortedEntityNames(lock *fsplan.Lock) []string {
	out := make([]string, 0, len(lock.Entities))
	for name := range lock.Entities {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// keptMigrations names the SQL pairs this run found already on disk and left
// alone. An empty result means the tables were created by THIS run.
//
// The distinction is what the report needs: a migration just written needs no
// commentary, while one written earlier may have run somewhere since, and the
// reader has to be told that the shape below is what the code now expects —
// not what their database necessarily holds.
func keptMigrations(decisions []fsplan.Decision) []string {
	var out []string
	for _, d := range decisions {
		if d.Action == fsplan.KeptHook && fsplan.IsMigration(d.File.Path) {
			out = append(out, d.File.Path)
		}
	}
	return out
}
