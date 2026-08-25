package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/emit"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/naming"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// Prune removes what an EARLIER shape of a spec left behind.
//
// It exists because a spec that shrinks leaves two kinds of residue, and
// `generate` cleans up neither — on purpose. A run that writes files is the
// wrong moment to delete other ones, and a shared registration file carries
// other entities' content, so the write path reports and moves on. The result
// was real work handed to a human with no tool: orphaned Go files that still
// compile and mean nothing, and notification declarations plus translation keys
// that no gate ever mentions again. A dead translation key is invisible to
// `check`, to `generate`, to the compiler and to the tests; it ships forever.
//
// Two rules keep this safe enough to be routine:
//
//   - It removes ONLY what the generator itself wrote and the lock still
//     recognises, byte for byte. Anything edited by hand, adopted, or of unknown
//     origin is reported and left exactly as it is.
//   - It says what it would do and does nothing, until -apply. Deleting a file
//     and a translation key is not a step to discover after the fact.
type PruneOptions struct {
	SpecPath   string
	ProjectDir string
	Apply      bool
}

func Prune(w io.Writer, opt PruneOptions) error {
	proj, err := discover.Find(opt.ProjectDir)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(opt.SpecPath)
	if err != nil {
		return fmt.Errorf("reading the spec: %w", err)
	}
	s, err := spec.Parse(raw, opt.SpecPath)
	if err != nil {
		return err
	}
	problems := spec.Validate(s, spec.Options{
		LangFallback: true, ExistingVOs: proj.ExistingVOs, ExistingVOKinds: proj.VOKind,
		VOOwner: proj.VOOwner, Neighbours: neighboursOf(proj),
	})
	if problems.HasBlockers() {
		return fmt.Errorf("the spec does not validate, so what it PRODUCES is unknown and "+
			"nothing can be called leftover:\n\n%s", problems.Error())
	}
	model, err := ir.Resolve(s, proj)
	if err != nil {
		return err
	}

	lock, err := fsplan.LoadLock(proj.Root)
	if err != nil {
		return err
	}
	entity := model.Entity.Pascal
	if _, ok := lock.Entities[entity]; !ok {
		return fmt.Errorf("%s is not recorded in %s — there is nothing this generator "+
			"wrote that could be left over", entity, fsplan.LockName)
	}

	specRel, _ := filepath.Rel(proj.Root, opt.SpecPath)
	if specRel == "" {
		specRel = opt.SpecPath
	}
	result, err := emit.All(model, proj.Root, emit.FileMeta{
		Spec:                 specRel,
		Entity:               entity,
		Date:                 "",
		PriorRegistrations:   lock.RegistrationsOf(entity),
		ForeignRegistrations: lock.RegistrationsExcept(entity),
	})
	if err != nil {
		return err
	}

	files := fsplan.PlanPrune(proj.Root, entity, result.Files, lock)
	files = keepWhatNeighboursStillNeed(proj, opt.SpecPath, files)
	regs, err := planRegistrationPrune(proj.Root, entity, result.Registrations, lock)
	if err != nil {
		return err
	}

	printPrunePlan(w, files, regs)
	if countable(files, regs) == 0 {
		return nil
	}
	if !opt.Apply {
		fmt.Fprintf(w, "\nNothing was changed. Re-run with -apply to remove them.\n")
		return nil
	}

	if err := applyRegistrationPrune(proj.Root, entity, regs, lock); err != nil {
		return err
	}
	if err := fsplan.ApplyPrune(proj.Root, entity, files, lock); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nPruned. Build and run the tests: removing a notification type the "+
		"code still references is a compile error, which is the good kind.\n")
	return nil
}

// keepWhatNeighboursStillNeed protects a file another SPEC of this project
// depends on, even though nothing in this entity's tree produces it anymore.
//
// The case is not hypothetical and it is silent until the compiler speaks: a
// value object is declared by ONE spec and reused by others (`vo: {kind: reuse,
// ref: X}` — the reusing spec deliberately emits no copy, because two copies of
// a rule can disagree). Drop the field that declared it and the file becomes an
// orphan by every measure this generator has — its own lock, its own file set —
// while another entity still has it as a field type. Deleting it is a broken
// build in a package the author was not touching.
//
// So the sibling specs are read for the value objects they NAME, whether they
// declare or reuse them, and any file backing one of those is kept and said so.
// It is a cheap read: the specs are small and prune is an explicit, occasional
// act.
func keepWhatNeighboursStillNeed(proj *discover.Project, selfSpec string, files []fsplan.PruneFile) []fsplan.PruneFile {
	needed := map[string]string{} // vos file path → the spec that still names it
	selfAbs, _ := filepath.Abs(selfSpec)
	for _, sib := range proj.SiblingSpecs {
		// A sibling's path is recorded RELATIVE to the project root, so it is
		// joined before use — reading it as given silently found nothing from any
		// other working directory, and a guard that silently finds nothing is a
		// guard that is not there.
		path := filepath.Join(proj.Root, sib.Path)
		abs, _ := filepath.Abs(path)
		if abs == selfAbs {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		other, err := spec.Parse(raw, sib.Path)
		if err != nil {
			continue // an unreadable neighbour is not a licence to delete its types
		}
		for _, ref := range spec.ValueObjectsNamed(other) {
			needed["internal/domain/vos/"+naming.Snake(ref)+".go"] = filepath.Base(path)
		}
	}
	if len(needed) == 0 {
		return files
	}
	out := make([]fsplan.PruneFile, 0, len(files))
	for _, f := range files {
		if by, ok := needed[filepath.ToSlash(f.Path)]; ok && f.Kind == fsplan.PruneDelete {
			f.Kind = fsplan.PruneKeep
			f.Reason = "another spec of this project still names this value object (" + by + ")"
		}
		out = append(out, f)
	}
	return out
}

// pruneReg is one declaration or catalog key that this entity recorded in a
// SHARED file and that the current spec no longer declares.
type pruneReg struct {
	Path string
	Name string
	Kind fsplan.PruneKind
	// Catalog is true for a translation entry (a map key) rather than a Go
	// declaration. The two are removed by different means and read differently
	// in the report: a dead type is loud once the code stops referencing it, a
	// dead translation key is silent forever.
	Catalog bool
	Reason  string
}

// planRegistrationPrune compares what the lock says this entity WROTE into the
// shared files against what it would write now.
//
// The comparison is per declaration, exactly like the merge that put them
// there: the recorded hash is the proof the text on disk is still the
// generator's own. A declaration someone edited is theirs, and a declaration
// another entity also claims is not this run's to remove — both are reported.
func planRegistrationPrune(root, entity string, now map[string]map[string]string, lock *fsplan.Lock) ([]pruneReg, error) {
	recorded := lock.RegistrationsOf(entity)
	foreign := lock.RegistrationsExcept(entity)
	var out []pruneReg

	paths := make([]string, 0, len(recorded))
	for path := range recorded {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			continue // the file itself is gone; nothing to prune inside it
		}
		src := string(content)
		names := make([]string, 0, len(recorded[path]))
		for name := range recorded[path] {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			if _, still := now[path][name]; still {
				continue
			}
			catalog := emit.IsCatalogPath(path)
			onDisk, found := emit.RegisteredText(src, name, catalog)
			switch {
			case !found:
				out = append(out, pruneReg{path, name, fsplan.PruneForget, catalog,
					"already removed from the file — only the lock still records it"})
			case emit.HashText(onDisk) != recorded[path][name]:
				out = append(out, pruneReg{path, name, fsplan.PruneKeep, catalog,
					"it was edited by hand since it was written"})
			case foreign[path][name] != "":
				out = append(out, pruneReg{path, name, fsplan.PruneKeep, catalog,
					"another entity of this project declares it too"})
			default:
				out = append(out, pruneReg{path, name, fsplan.PruneDelete, catalog,
					"the spec no longer declares it"})
			}
		}
	}
	return out, nil
}

func applyRegistrationPrune(root, entity string, regs []pruneReg, lock *fsplan.Lock) error {
	byPath := map[string][]pruneReg{}
	for _, r := range regs {
		if r.Kind == fsplan.PruneKeep {
			continue
		}
		byPath[r.Path] = append(byPath[r.Path], r)
	}
	for path, items := range byPath {
		abs := filepath.Join(root, path)
		content, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		src := string(content)
		changed := false
		for _, it := range items {
			if it.Kind == fsplan.PruneDelete {
				var ok bool
				if it.Catalog {
					src, ok = emit.RemoveMapEntry(src, it.Name)
				} else {
					src, ok = emit.RemoveTypeDecl(src, it.Name)
				}
				changed = changed || ok
			}
			lock.ForgetRegistration(entity, path, it.Name)
		}
		if changed {
			if err := os.WriteFile(abs, []byte(src), 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", path, err)
			}
		}
	}
	return nil
}

func countable(files []fsplan.PruneFile, regs []pruneReg) int {
	n := 0
	for _, f := range files {
		if f.Kind != fsplan.PruneKeep {
			n++
		}
	}
	for _, r := range regs {
		if r.Kind != fsplan.PruneKeep {
			n++
		}
	}
	return n
}

func printPrunePlan(w io.Writer, files []fsplan.PruneFile, regs []pruneReg) {
	var del, forget, keep []string
	for _, f := range files {
		line := fmt.Sprintf("  %s — %s", f.Path, f.Reason)
		switch f.Kind {
		case fsplan.PruneDelete:
			del = append(del, line)
		case fsplan.PruneForget:
			forget = append(forget, line)
		default:
			keep = append(keep, line)
		}
	}
	for _, r := range regs {
		what := "declaration"
		if r.Catalog {
			what = "translation key"
		}
		line := fmt.Sprintf("  %s: %s (%s) — %s", r.Path, r.Name, what, r.Reason)
		switch r.Kind {
		case fsplan.PruneDelete:
			del = append(del, line)
		case fsplan.PruneForget:
			forget = append(forget, line)
		default:
			keep = append(keep, line)
		}
	}

	if len(del) == 0 && len(forget) == 0 && len(keep) == 0 {
		fmt.Fprintf(w, "Nothing to prune: everything this entity's spec ever produced is "+
			"still something it produces.\n")
		return
	}
	if len(del) > 0 {
		fmt.Fprintf(w, "To remove (%d):\n%s\n", len(del), strings.Join(del, "\n"))
	}
	if len(forget) > 0 {
		fmt.Fprintf(w, "\nTo forget in the lock (%d) — already gone from disk, and the "+
			"reason `doctor` keeps reporting them:\n%s\n", len(forget), strings.Join(forget, "\n"))
	}
	if len(keep) > 0 {
		fmt.Fprintf(w, "\nLeft alone (%d) — not the generator's to remove:\n%s\n",
			len(keep), strings.Join(keep, "\n"))
	}
}
