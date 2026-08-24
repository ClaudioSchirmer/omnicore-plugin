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
	//
	// It means one thing per class, and the difference decides what may be done
	// with the file. For an OWNED file it is what the last run wrote, and a
	// mismatch is a hand edit to refuse. For a HOOK it is what the generator
	// wrote when it CREATED the file and never anything since — a mismatch is
	// not drift, it is the file being used for what it exists for, and the only
	// thing it authorises is telling the author the file is theirs.
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

// RegistrationsExcept is the union of what every OTHER entity recorded, so a
// merge can recognise a shared declaration as the generator's own even when the
// entity that wrote it is not the one running.
//
// A key claimed by two entities keeps the first hash seen; they are compared
// against one text on disk, so at most one of them can match anyway.
func (l *Lock) RegistrationsExcept(entity string) map[string]map[string]string {
	if l == nil {
		return nil
	}
	out := map[string]map[string]string{}
	for name, e := range l.Entities {
		if name == entity {
			continue
		}
		for path, decls := range e.Registrations {
			if out[path] == nil {
				out[path] = map[string]string{}
			}
			for k, v := range decls {
				if _, taken := out[path][k]; !taken {
					out[path][k] = v
				}
			}
		}
	}
	return out
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
		// MERGED, never replaced. A key this run no longer writes is still a key
		// this entity PUT in a shared file, and the record is the only evidence
		// of that — the text stays on disk either way, so dropping the record
		// here would lose the one thing that can later prove the leftover is the
		// generator's own and safe to remove. Prune is what forgets it, at the
		// same moment it takes the text out.
		if entry.Registrations == nil {
			entry.Registrations = map[string]map[string]string{}
		}
		for path, decls := range registrations {
			if entry.Registrations[path] == nil {
				entry.Registrations[path] = map[string]string{}
			}
			for name, hash := range decls {
				entry.Registrations[path][name] = hash
			}
		}
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
			switch {
			case d.File.Class == Owned:
				entry.Files[d.File.Path] = LockFile{Class: Owned, Hash: Hash(d.File.Content)}
			case tracksAsHook(d.File.Path, d.File.Class):
				// The CREATION hash, and the only one this record will ever
				// hold: a hook is written once, so what the generator put there
				// is a fixed fact. Everything a hook needs from the lock follows
				// from that one comparison — whether the author has written in
				// it yet, and therefore whether an orphaned one may be removed.
				entry.Files[d.File.Path] = LockFile{Class: Hook, Hash: Hash(d.File.Content)}
			default:
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
			// Never RE-hashed: the author owns the contents outright, so the
			// record keeps the hash of what the generator first wrote and
			// nothing else. That is what tells an untouched hook apart from one
			// somebody has written in, which is the whole question prune asks.
			//
			// The delete below is what lets a file CHANGE class between builds
			// without leaving a lie behind. Migrations became hooks after
			// projects had been generated with them as owned, and a stale owned
			// record would have kept `doctor` verifying a checksum on a file the
			// author is now invited to edit — reporting a hand edit as drift, on
			// the one command whose whole job is telling the truth about drift.
			//
			// An ADOPTED owned file also lands here, and its record must stay:
			// that record IS the adoption.
			if d.File.Class == Owned {
				break
			}
			if !tracksAsHook(d.File.Path, d.File.Class) {
				delete(entry.Files, d.File.Path)
				break
			}
			if rec, ok := entry.Files[d.File.Path]; ok && rec.Class == Hook {
				break
			}
			// A hook this project generated BEFORE hooks were recorded at all.
			// Without this line the fix would only reach trees generated from
			// here on: every hook already on disk would stay invisible to
			// `prune` forever, which is precisely the state being fixed.
			//
			// What is recorded is what the generator WOULD write for it now,
			// which is the closest thing to a creation record that still exists.
			// It is deliberately not the file's own bytes: recording those would
			// make a hook somebody had already filled in look untouched, and the
			// one thing this record must never do is authorise deleting a body.
			// The comparison is therefore conservative rather than exact — a
			// retrofitted hook is reported and left, not removed — and that is
			// the right side to be wrong on.
			entry.Files[d.File.Path] = LockFile{Class: Hook, Hash: Hash(d.File.Content)}
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
	if rec.Class == Hook {
		return fmt.Errorf("%s is a hook: it was written once and is already yours, so "+
			"there is no refusal here for an adoption to lift — regeneration never "+
			"touches it. The record exists only so `prune` can tell you when the spec "+
			"stops producing it", path)
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

// UnwrittenHookReason says what an UNTOUCHED hook is costing, by kind.
//
// The three are silent in three different ways and only one of them is safe to
// defer, so one sentence for all three is the sentence that gets somebody
// paged. It is told from the PATH for the same reason keptHookReason is: the
// reader of `doctor` has no model in hand, and a reason written for the other
// kind is worse than none.
func UnwrittenHookReason(path string) string {
	switch {
	case strings.HasSuffix(path, "_service_manual.go"):
		return "still exactly as it was created — the facts it declares PANIC the first " +
			"time a rule asks one, which is a running service falling over rather than a " +
			"field arriving empty"
	case strings.HasSuffix(path, "_computed_manual.go"):
		return "still exactly as it was created — every derived field it backs renders " +
			"ABSENT, quietly, on REST, on GraphQL and in the exports at once"
	case strings.HasSuffix(path, "_rules_manual.go"):
		return "still exactly as it was created — the invariants it holds are declared and " +
			"NOT enforced, so a write the spec calls invalid is accepted"
	}
	return "still exactly as it was created — nothing has been written in it, and what it " +
		"was created for is not happening"
}

// tracksAsHook reports whether a hook is one the lock should remember.
//
// Every hook except a migration. A hook used to be recorded nowhere at all,
// which made an ORPHANED one invisible to every command this generator has:
// take the last computed field out of a spec, regenerate, and the derivation
// file stays on disk declaring a function nothing calls — `prune` iterates the
// lock, so it never saw the file, and the author was left with a tree only a
// manual `rm` could finish. Recording the hook's creation hash is what gives
// them a tool path back, and it costs the hook nothing: the file is still
// written once, still never rewritten, still carries no checksum of its own.
//
// A MIGRATION stays out, and not by omission. Its effect outlives the file — it
// ran, and a tracking table in every environment says so — which is exactly why
// prune already refuses to consider one. Recording it would only teach the
// other reader of this map, `generate`'s orphan report, to offer a cleanup that
// must never happen.
func tracksAsHook(path string, class Class) bool {
	return class == Hook && !IsMigration(path)
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

// ---------------------------------------------------------------- prune

// PruneKind is what pruning decided about one artefact the lock still records.
type PruneKind string

const (
	// PruneDelete: it is on disk, unchanged since the generator wrote it, and
	// this spec no longer produces it. Safe to remove.
	PruneDelete PruneKind = "delete"
	// PruneForget: already deleted by hand. Only the lock still remembers it,
	// which is what makes `doctor` report "is gone" forever.
	PruneForget PruneKind = "forget"
	// PruneKeep: it diverged from what the generator wrote, or it carries an
	// adopted edit. Reported and left alone — the same rule that governs every
	// other write here.
	PruneKeep PruneKind = "keep"
)

// PruneFile is one recorded file and what pruning would do with it.
type PruneFile struct {
	Path   string
	Kind   PruneKind
	Reason string
}

// PlanPrune decides what is left over from an EARLIER shape of this spec.
//
// It answers a question `generate` deliberately does not: a run that writes
// files is the wrong moment to delete other ones, so the write path only ever
// reports orphans. Pruning is the asked-for act, and it obeys the same rule as
// every write here — anything that no longer matches what the generator itself
// wrote is reported, never removed.
//
// Migrations are never candidates. Their effect outlives the file, so a
// migration nobody generates anymore is still one that RAN.
func PlanPrune(root, entity string, current []File, lock *Lock) []PruneFile {
	prev, ok := lock.Entities[entity]
	if !ok {
		return nil
	}
	now := map[string]bool{}
	for _, f := range current {
		now[f.Path] = true
	}

	var out []PruneFile
	for path, rec := range prev.Files {
		if now[path] || IsMigration(path) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, path))
		switch {
		case err != nil:
			out = append(out, PruneFile{path, PruneForget,
				"already deleted — only the lock still remembers it"})
		case rec.AdjustedFor != "":
			out = append(out, PruneFile{path, PruneKeep,
				"carries a hand edit adopted at framework " + rec.AdjustedFor})
		case Hash(content) != rec.Hash && rec.Class == Hook:
			// An orphaned hook whose contents are not the generator's. Saying
			// WHICH kind of leftover it is matters more here than anywhere else
			// in this list: the file compiles, declares functions nothing calls
			// anymore, and no other command will ever mention it again.
			//
			// The sentence deliberately does not claim "you wrote in it". For a
			// hook recorded at creation that is what a mismatch means; for one
			// recorded retroactively — a project generated before hooks were
			// recorded at all — the record is only what the generator WOULD
			// write, so a mismatch means "not mine" and no more than that.
			out = append(out, PruneFile{path, PruneKeep,
				"the spec no longer produces it, and its contents are not what the " +
					"generator writes for it — it is yours, and removing it is a hand " +
					"edit only you can make"})
		case Hash(content) != rec.Hash:
			out = append(out, PruneFile{path, PruneKeep,
				"it was edited by hand since it was generated"})
		default:
			out = append(out, PruneFile{path, PruneDelete,
				"the spec no longer produces it, and it is unchanged since it was written"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ApplyPrune removes what PlanPrune decided and forgets it in the lock. A
// PruneKeep is skipped: it is the author's.
func ApplyPrune(root, entity string, files []PruneFile, lock *Lock) error {
	entry, ok := lock.Entities[entity]
	if !ok {
		return fmt.Errorf("no generated entity named %q is recorded in %s", entity, LockName)
	}
	for _, f := range files {
		switch f.Kind {
		case PruneDelete:
			if err := os.Remove(filepath.Join(root, f.Path)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("removing %s: %w", f.Path, err)
			}
			delete(entry.Files, f.Path)
		case PruneForget:
			delete(entry.Files, f.Path)
		}
	}
	lock.Entities[entity] = entry
	return lock.Save(root)
}

// ForgetRegistration drops one declaration from what this entity records for a
// shared file, after the caller has removed the text itself.
func (l *Lock) ForgetRegistration(entity, path, decl string) {
	entry, ok := l.Entities[entity]
	if !ok || entry.Registrations == nil {
		return
	}
	if decls, ok := entry.Registrations[path]; ok {
		delete(decls, decl)
		if len(decls) == 0 {
			delete(entry.Registrations, path)
		}
	}
	l.Entities[entity] = entry
}
