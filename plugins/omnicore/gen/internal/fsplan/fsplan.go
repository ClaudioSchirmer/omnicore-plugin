// Package fsplan owns what the generator is allowed to write, and what it must
// refuse to touch.
//
// The rule that shapes everything here: the generator NEVER clobbers a file a
// human changed. It refuses, says so, and leaves the file alone. That is what
// makes regeneration safe enough to be routine — and routine regeneration is
// what makes the whole spec-driven model work.
package fsplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/gofile"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/layout"
)

// Class is how the generator relates to a file.
type Class string

const (
	// Owned: written in full by the generator, hashed. A hand edit means the
	// next run refuses it rather than discarding the edit.
	Owned Class = "owned"
	// Hook: written once if absent, then never touched and never hashed. Two
	// very different things live here for the same reason — the file's content
	// is the author's from the moment it exists. Hand-written business rules are
	// one; a MIGRATION is the other, because its effect outlives the file and a
	// rewrite would change what the file claims without changing the database.
	Hook Class = "hook"
	// Registration: a shared file the generator inserts into and removes from,
	// never rewrites. wire.go, the notification files, the translation catalogs.
	Registration Class = "registration"
)

// File is one planned output.
type File struct {
	Path    string // relative to the project root
	Class   Class
	Content []byte
	// What names, in the author's terms, this file carries. Feeds the report,
	// which describes WHAT was generated before it lists paths.
	Describes string
	// Consequence states what happens while a hook file is still unwritten. The
	// kinds differ: unenforced rules are quiet, an unimplemented service fact
	// panics on first use. Saying which is the difference between a reader
	// deferring it safely and deferring it into an outage.
	Consequence string
}

// Action is what the plan decided for one file.
type Action string

const (
	Create    Action = "create"
	Update    Action = "update"
	Unchanged Action = "unchanged"
	// RefusedEdited: an owned file diverged from its recorded hash.
	RefusedEdited Action = "refused-edited"
	// KeptHook: a hook file already exists, so its contents are the author's.
	KeptHook Action = "kept-hook"
)

// Decision pairs a file with what will happen to it and why.
type Decision struct {
	File   File
	Action Action
	Reason string
}

// Expected reports whether a decision is a refusal the author should NOT be
// alarmed by. A hook file that already holds hand-written rules is working
// exactly as designed, so it must not be reported as a failure or invite the
// destructive --force.
func (d Decision) Expected() bool { return d.Action == KeptHook }

// The two hook kinds are told apart by their path, so a reader is never handed
// a reason written for the other one. A migration that already exists is not
// "where your rules live"; a rules file that does not exist yet is not "the
// tables were created".
func keptHookReason(path string) string {
	if IsMigration(path) {
		return "created once and never rewritten — a migration that ran cannot be taken back by editing it"
	}
	return "hand-written rules live here, by design"
}

func createHookReason(path string) string {
	if IsMigration(path) {
		return "the tables, written once; from now on it is yours and a change is a NEW numbered pair"
	}
	return "created empty for the rules the spec could not express"
}

// ---------------------------------------------------------------- lock

// LockName is the lock as a reader sees it in a message: the path inside the
// service, not the bare file name, because "lock.json" on its own does not say
// which of a project's tools it belongs to.
const LockName = layout.Dir + "/" + layout.LockName

type Lock struct {
	Version  int                   `json:"version"`
	Entities map[string]LockEntity `json:"entities"`
}

type LockEntity struct {
	Spec      string              `json:"spec"`
	SpecHash  string              `json:"specHash"`
	Framework string              `json:"framework"`
	Files     map[string]LockFile `json:"files"`
	// Ordinals records the migration number this entity already owns, per
	// dialect. Without it every regeneration would allocate a FRESH number —
	// the previous run's own files push the counter along — and the project
	// would accumulate one duplicate migration per run.
	Ordinals map[string]int `json:"ordinals,omitempty"`
	// ViewShape is a hash of what the read side PROJECTS, and ViewVersion the
	// number the spec declared alongside it. Kept together because the pair is
	// the only way to catch the mistake they exist for: a shape that changed
	// while the version did not. The framework answers that by refusing to boot,
	// so it is a failed start rather than a wrong answer — but it is a failed
	// start found minutes later, by whoever runs the service, instead of by the
	// tool that just wrote the change.
	ViewShape   string `json:"viewShape,omitempty"`
	ViewVersion int    `json:"viewVersion,omitempty"`
	// Registrations records what this entity last wrote INTO a shared file,
	// keyed by path and then by declaration name, as a hash of the text.
	//
	// A registration file has no header and no checksum of its own — it belongs
	// to every entity in the project, so there is no single run that could seal
	// it. This is the same answer applied one declaration at a time: matching
	// what was recorded means the text on disk is still the generator's own and
	// may be replaced; differing means somebody edited it, and it is theirs.
	// Absent means the declaration predates this record and is left alone,
	// because "I do not know who wrote this" is not a licence to overwrite it.
	Registrations map[string]map[string]string `json:"registrations,omitempty"`
}

type LockFile struct {
	Class Class `json:"class"`
	// Hash is the hash of what the generator ITSELF last wrote — never of what
	// it merely found on disk. Recording a foreign hash here would make a
	// refusal last exactly one run: the next comparison would match, and the
	// file the generator had just declined to touch would be overwritten.
	Hash string `json:"hash"`
	// AdjustedFor records that a hand edit was DELIBERATELY accepted through
	// `adopt`, and the framework version it was accepted at. The version is
	// context — it says what the tree looked like at the time — not the reason:
	// an adoption is equally legitimate when the generator simply does not cover
	// something and the author wants the generated base anyway.
	//
	// What it is NOT is a way to keep an edit the spec could have expressed. An
	// adopted file stops tracking the spec: every later improvement to the
	// emitters lands everywhere except here, silently, forever.
	AdjustedFor string `json:"adjustedFor,omitempty"`
	// Why is the one line the author gave when adopting. Optional, and worth
	// giving: the next person to meet this file is usually not the one who
	// edited it, and "adopted" alone does not say whether the reason still holds.
	Why string `json:"why,omitempty"`
}

// RegistrationsOf is what the named entity last wrote into the shared files.
func (l *Lock) RegistrationsOf(entity string) map[string]map[string]string {
	if l == nil {
		return nil
	}
	return l.Entities[entity].Registrations
}

func LoadLock(root string) (*Lock, error) {
	b, err := os.ReadFile(layout.LockIn(root))
	if os.IsNotExist(err) {
		return &Lock{Version: 1, Entities: map[string]LockEntity{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", LockName, err)
	}
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("%s is not readable as JSON (%w) — "+
			"restore it from version control rather than deleting it, or the next run "+
			"will treat every generated file as hand-written", LockName, err)
	}
	if l.Entities == nil {
		l.Entities = map[string]LockEntity{}
	}
	return &l, nil
}

func (l *Lock) Save(root string) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(layout.DirIn(root), 0o755); err != nil {
		return err
	}
	return os.WriteFile(layout.LockIn(root), append(b, '\n'), 0o644)
}

// Hash normalises line endings before hashing.
//
// Without this, a checkout with CRLF endings — or an editor that formats on
// save — makes every generated file look hand-edited, and the whole tree is
// refused at once with --force as the only apparent way out. That failure is
// worse than the problem the hash solves.
func Hash(content []byte) string {
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- planning

// Plan decides, without writing anything, what would happen to each file.
// `force` names paths whose refusal the author has explicitly overridden.
func Plan(root string, entity string, files []File, lock *Lock, force map[string]bool) ([]Decision, error) {
	prev := lock.Entities[entity]
	var out []Decision

	for _, f := range files {
		abs := filepath.Join(root, f.Path)
		onDisk, readErr := os.ReadFile(abs)
		exists := readErr == nil

		switch f.Class {
		case Hook:
			if exists {
				out = append(out, Decision{File: f, Action: KeptHook, Reason: keptHookReason(f.Path)})
				continue
			}
			out = append(out, Decision{File: f, Action: Create, Reason: createHookReason(f.Path)})

		case Registration:
			// Merging is the caller's job (it needs to know the file's syntax);
			// the plan only reports create-vs-update.
			if exists {
				out = append(out, Decision{File: f, Action: Update, Reason: "registration merged in"})
			} else {
				out = append(out, Decision{File: f, Action: Create, Reason: "registration site created"})
			}

		case Owned:
			if !exists {
				out = append(out, Decision{File: f, Action: Create})
				continue
			}
			// The FILE answers whether it was edited, through the checksum in its
			// own header. That is deliberate: with the answer only in the lock, a
			// lost or deleted lock makes the whole tree look hand-written and the
			// generator has to refuse everything or trust everything. Each file
			// now speaks for itself, and survives being copied or moved.
			intact, sealed := gofile.VerifyHeader(onDisk)
			recorded := prev.Files[f.Path]

			switch {
			case recorded.AdjustedFor != "":
				out = append(out, Decision{File: f, Action: KeptHook,
					Reason: adoptionReason(recorded)})
			case force[f.Path]:
				out = append(out, Decision{File: f, Action: Update, Reason: "overwritten on request"})
			case !sealed:
				out = append(out, Decision{File: f, Action: RefusedEdited,
					Reason: "the file carries no generator checksum, so it was not written by this generator"})
			case !intact:
				out = append(out, Decision{File: f, Action: RefusedEdited,
					Reason: "the checksum in its header no longer matches its contents — it was edited by hand"})
			case Hash(onDisk) == Hash(f.Content):
				out = append(out, Decision{File: f, Action: Unchanged})
			default:
				out = append(out, Decision{File: f, Action: Update})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].File.Path < out[j].File.Path })
	return out, nil
}

// Apply writes what the plan allows and records the result. The lock always
// stores the hash of what is actually ON DISK — recording the hash of content
// that was refused would make the next run believe the file matched.
func Apply(root, entity, specPath, specHash, framework string, ordinals map[string]int, view ViewState, decisions []Decision, lock *Lock) error {
	return ApplyWith(root, entity, specPath, specHash, framework, ordinals, view, decisions, nil, lock)
}

// ApplyWith is Apply plus the per-declaration hashes this run wrote into the
// shared registration files. They are recorded even when nothing else changed:
// the record is what lets the NEXT run tell its own text apart from a hand edit,
// and a run that merely CONFIRMED a declaration still knows it is the author.
func ApplyWith(root, entity, specPath, specHash, framework string, ordinals map[string]int, view ViewState, decisions []Decision, registrations map[string]map[string]string, lock *Lock) error {
	entry, ok := lock.Entities[entity]
	if !ok {
		entry = LockEntity{Files: map[string]LockFile{}}
	}
	if entry.Files == nil {
		entry.Files = map[string]LockFile{}
	}
	entry.Spec, entry.SpecHash, entry.Framework = specPath, specHash, framework
	entry.ViewShape, entry.ViewVersion = view.Shape, view.Version
	if ordinals != nil {
		entry.Ordinals = ordinals
	}
	if registrations != nil {
		entry.Registrations = registrations
	}

	for _, d := range decisions {
		abs := filepath.Join(root, d.File.Path)
		switch d.Action {
		case Create, Update:
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", filepath.Dir(d.File.Path), err)
			}
			if err := os.WriteFile(abs, d.File.Content, 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", d.File.Path, err)
			}
			if d.File.Class == Owned {
				entry.Files[d.File.Path] = LockFile{Class: Owned, Hash: Hash(d.File.Content)}
			} else {
				delete(entry.Files, d.File.Path)
			}
		case Unchanged:
			if d.File.Class == Owned {
				entry.Files[d.File.Path] = LockFile{Class: Owned, Hash: Hash(d.File.Content)}
			} else {
				delete(entry.Files, d.File.Path)
			}
		case RefusedEdited:
			// Deliberately untouched. The recorded hash stays as the last thing
			// this generator wrote, so the file keeps being recognised as edited
			// and keeps being refused until it is adopted or explicitly forced.
		case KeptHook:
			// Never hashed: the author owns it outright.
			//
			// The delete is what lets a file CHANGE class between builds without
			// leaving a lie behind. Migrations became hooks after projects had
			// been generated with them as owned, and a stale owned record would
			// have kept `doctor` verifying a checksum on a file the author is now
			// invited to edit — reporting a hand edit as drift, on the one
			// command whose whole job is telling the truth about drift.
			//
			// An ADOPTED owned file also lands here, and its record must stay:
			// that record IS the adoption.
			if d.File.Class != Owned {
				delete(entry.Files, d.File.Path)
			}
		}
	}

	lock.Entities[entity] = entry
	return lock.Save(root)
}

// Adopt accepts a hand fix on an owned file, recording the framework version
// that made it necessary. Without this, the degraded path (a framework newer
// than the generator targets) leaves debt that only surfaces at the next
// regeneration, as a refusal nobody can explain.
func Adopt(root, entity, path, framework, why string, lock *Lock) error {
	entry, ok := lock.Entities[entity]
	if !ok {
		return fmt.Errorf("no generated entity named %q is recorded in %s", entity, LockName)
	}
	rec, ok := entry.Files[path]
	if !ok {
		return fmt.Errorf("%s is not a generated file of %s — "+
			"only a file this generator wrote can carry an adopted fix", path, entity)
	}
	content, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	rec.Hash = Hash(content)
	rec.AdjustedFor = framework
	rec.Why = why
	entry.Files[path] = rec
	lock.Entities[entity] = entry
	return lock.Save(root)
}

// OrdinalsOf returns the migration numbers this entity already owns.
func (l *Lock) OrdinalsOf(entity string) map[string]int {
	return l.Entities[entity].Ordinals
}

// Orphans lists files the previous run generated that this one no longer does.
func Orphans(entity string, files []File, lock *Lock) []string {
	prev, ok := lock.Entities[entity]
	if !ok {
		return nil
	}
	now := map[string]bool{}
	for _, f := range files {
		now[f.Path] = true
	}
	var out []string
	for path := range prev.Files {
		if !now[path] {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// IsMigration reports whether a path is one of the SQL pairs.
//
// A migration that ran in ANY environment is recorded in the framework's
// tracking table, so deleting or rewriting the file does not undo it. That one
// fact is why migrations are hooks, and why an orphaned one is never presented
// as something to clean up.
func IsMigration(path string) bool {
	return strings.HasPrefix(filepath.ToSlash(path), "migrations/") &&
		strings.HasSuffix(path, ".sql")
}

// adoptionReason phrases what an adopted file is, without claiming a cause it
// does not know. "A fix for v0.49.0" was a guess dressed as a fact: adoption
// records a deliberate edit, and the version is only when it happened.
func adoptionReason(rec LockFile) string {
	out := "carries a hand edit adopted at framework " + rec.AdjustedFor
	if rec.Why != "" {
		out += " — " + rec.Why
	}
	return out
}

// ViewState is what the read side projected on this run, and the version the
// spec claimed for it.
type ViewState struct {
	Shape   string
	Version int
}

// ViewShapeChangedWithoutBump reports the one mistake the pair exists to catch.
//
// It answers false on a first generation (there is nothing to compare) and
// false when the version moved, whatever else changed — bumping is the author
// declaring the new shape, which is all that is being asked of them.
func (l *Lock) ViewShapeChangedWithoutBump(entity string, now ViewState) (was int, changed bool) {
	prev, ok := l.Entities[entity]
	if !ok || prev.ViewShape == "" || now.Shape == "" {
		return 0, false
	}
	if prev.ViewShape == now.Shape {
		return 0, false
	}
	if prev.ViewVersion != now.Version {
		return 0, false
	}
	return prev.ViewVersion, true
}
