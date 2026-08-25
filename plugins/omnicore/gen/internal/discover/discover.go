// Package discover reads the host project so the spec never has to repeat what
// the project already states. Module path, target dialects, the pinned framework
// version, the free migration ordinal and the existing value objects are FACTS
// of the project — asking an author to restate them invites them to disagree
// with reality.
package discover

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/layout"
)

// Project is everything the generator learned by reading, not by asking.
type Project struct {
	Root       string
	ModulePath string

	// Dialects is the union of the dialects the configuration selects and the
	// migration folders that actually contain SQL. An EMPTY folder is not a
	// target: the yaml is the authority, folder presence alone is not.
	Dialects []string

	// FrameworkVersion is the pin as Go resolves it. Empty when it cannot be
	// resolved (a local checkout, an offline proxy) — never fatal.
	FrameworkVersion string
	FrameworkDir     string
	LocalCheckout    bool

	// NextOrdinal is per dialect: the same number may be free in one folder and
	// taken in another, and a single counter would mask the collision.
	NextOrdinal map[string]int

	// ExistingVOs lists EVERY value object the package declares — the ones this
	// generator wrote for other entities included, because that is precisely
	// what a second role over the same shared base needs to reuse.
	//
	// It once listed only hand-written ones, to stop a re-run refusing the VOs
	// its own first run had created. That confused two questions that are not
	// the same: who may REWRITE the file, and who may REFERENCE the type. The
	// answer to the first is VOOwner; the answer to the second is everyone.
	ExistingVOs []string

	// VOOwner maps a value object to the entity whose spec generated it, read
	// from the file's banner. A hand-written one maps to "" — nobody's to
	// rewrite, everybody's to reference.
	VOOwner map[string]string

	// VOKind maps a value object to WHAT IT IS — "raw" (it writes its own
	// IsValid; a composite is one of these) or "enum" (membership, checked by
	// the framework). A type the reader could not classify is absent.
	//
	// It exists for the one question a NAME cannot answer: a field declared
	// `vo.kind: reuse` names a type this spec never described, and validating
	// that value in place is `x.IsValid(...)` for one kind and
	// `domain.ValidateEnum(x, ...)` for the other. Guessing wrong does not
	// compile, so the generator asks the package instead.
	VOKind map[string]string

	HasMongo bool

	// SiblingSpecs is what the OTHER specs of this project already claim: the
	// view name and the route of each entity, keyed by the entity's name.
	//
	// The framework refuses two features declaring the same view name, and it
	// refuses them at BOOT — which means the collision is found by running the
	// service, long after the spec that caused it was written. The generator can
	// see every spec in the project, so it can say it while the author is still
	// looking at the file.
	SiblingSpecs []SpecClaim
}

// SpecClaim is what one spec in this project has already taken.
type SpecClaim struct {
	Path     string
	Entity   string
	ViewName string
	Route    string
	// Table and Fields are the ROOT of that spec: what a READ JOIN declared by
	// another entity traverses INTO. A join names a column of the target, and
	// the only way to say whether that column exists — and what Go type the
	// field it lands on must have — is to read the target's own declaration.
	Table  string
	Fields []FieldClaim
	// Revision is that spec's optimistic-concurrency column, and it is carried
	// here to be REFUSED BY NAME. It is the one managed column the framework's
	// read path does not resolve — goNameForRead answers the timestamps and
	// stops — so a traversal onto it fails at repository construction. Knowing
	// which column it is turns a generic "not a column of X" into the reason.
	Revision string
	// Children is what this spec declares its collections to be. A role that
	// MOUNTS one of them restates its shape, and the two statements have to
	// agree: a column named on one side and not the other compiles on both and
	// reads a document that does not exist.
	Children []ChildClaim
}

// ChildClaim is one collection as a spec declares it.
type ChildClaim struct {
	Name    string
	Table   string
	OwnedBy string
	Fields  []FieldClaim
}

// FieldClaim is one field of a collection, in the three spellings that have to
// match for two specs to mean the same table.
type FieldClaim struct {
	Name   string
	Column string
	Type   string
	// Nullable and LivesOn matter to a read join: an InnerJoin is legal only
	// over a NON-NULLABLE foreign key, and a join reaches the target's own
	// table — so a column that lives on a shared base or a sibling is not
	// something the traversal's single predicate can reach.
	Nullable bool
	LivesOn  string
}

// Find walks up from dir looking for the go.mod that roots the service.
func Find(dir string) (*Project, error) {
	root, err := findModuleRoot(dir)
	if err != nil {
		return nil, err
	}
	p := &Project{Root: root, NextOrdinal: map[string]int{}}
	if p.ModulePath, err = moduleOf(root); err != nil {
		return nil, err
	}
	p.Dialects = discoverDialects(root)
	p.HasMongo = discoverMongo(root)
	p.FrameworkVersion, p.FrameworkDir, p.LocalCheckout = discoverFramework(root)
	p.ExistingVOs, p.VOOwner, p.VOKind = discoverVOs(root)
	p.SiblingSpecs = discoverSpecs(root)
	for _, d := range p.Dialects {
		p.NextOrdinal[d] = nextOrdinal(filepath.Join(root, "migrations", d))
	}
	return p, nil
}

func findModuleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no go.mod found at or above %s — "+
				"omnicore-gen adds an entity to an existing service, it does not create one", dir)
		}
		abs = parent
	}
}

var moduleRe = regexp.MustCompile(`(?m)^module\s+(\S+)`)

func moduleOf(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}
	m := moduleRe.FindSubmatch(b)
	if m == nil {
		return "", fmt.Errorf("go.mod at %s declares no module path", root)
	}
	return string(m[1]), nil
}

// dialectRe pulls relational.dialect out of the yaml profiles without a yaml
// dependency on the consumer's schema, resolving the ${VAR:default} form.
var dialectRe = regexp.MustCompile(`(?m)^\s*dialect:\s*["']?([^"'\s#]+)`)
var envDefaultRe = regexp.MustCompile(`^\$\{[^:}]+:([^}]*)\}$`)
var envBareRe = regexp.MustCompile(`^\$\{[^}]+\}$`)

func discoverDialects(root string) []string {
	found := map[string]bool{}

	matches, _ := filepath.Glob(filepath.Join(root, "microservice*.yaml"))
	more, _ := filepath.Glob(filepath.Join(root, "config", "microservice*.yaml"))
	matches = append(matches, more...)
	for _, path := range matches {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, m := range dialectRe.FindAllSubmatch(b, -1) {
			v := strings.TrimSpace(string(m[1]))
			if d := envDefaultRe.FindStringSubmatch(v); d != nil {
				v = d[1]
			} else if envBareRe.MatchString(v) {
				// An unset variable with no default resolves to empty at boot;
				// it names no dialect we can generate for.
				continue
			}
			if v != "" {
				found[v] = true
			}
		}
	}

	// A migration folder counts only when it already carries SQL — an empty
	// folder is a leftover, not a declaration of intent.
	entries, _ := os.ReadDir(filepath.Join(root, "migrations"))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sqls, _ := filepath.Glob(filepath.Join(root, "migrations", e.Name(), "*.sql"))
		if len(sqls) > 0 {
			found[e.Name()] = true
		}
	}

	out := make([]string, 0, len(found))
	for d := range found {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func discoverMongo(root string) bool {
	matches, _ := filepath.Glob(filepath.Join(root, "microservice*.yaml"))
	more, _ := filepath.Glob(filepath.Join(root, "config", "microservice*.yaml"))
	for _, path := range append(matches, more...) {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if regexp.MustCompile(`(?m)^\s*mongo:`).Match(b) {
			return true
		}
	}
	return false
}

const frameworkModule = "github.com/ClaudioSchirmer/omnicore"

// generatedBanner marks a file this generator owns. It is how a regeneration
// tells its own output apart from code someone wrote by hand.
//
// It stops at "omnicore" on purpose. The marker is compared against files this
// generator wrote at any point in its life, and the exact wording of that line
// is not something to depend on — it has already changed once, when the second,
// redundant banner every emitter used to write was removed.
const generatedBanner = "Code generated by omnicore"

// discoverFramework resolves the pin. GOWORK is cleared, NOT set to "off": a
// developer working against a local checkout through go.work must be reported
// as such, and GOWORK=off would hide their checkout and report a version they
// do not actually compile against.
func discoverFramework(root string) (version, dir string, local bool) {
	out, err := runGo(root, "list", "-m", "-f", "{{.Version}}|{{.Dir}}|{{if .Replace}}replaced{{end}}", frameworkModule)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(strings.TrimSpace(out), "|")
	if len(parts) < 2 {
		return "", "", false
	}
	version, dir = parts[0], parts[1]
	replaced := len(parts) > 2 && parts[2] == "replaced"
	// A working copy has no releasable version; "(devel)" and an empty version
	// both mean the same thing here.
	local = replaced || version == "" || version == "(devel)"
	return version, dir, local
}

func runGo(dir string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=")
	b, err := cmd.Output()
	return string(b), err
}

var (
	voTypeRe  = regexp.MustCompile(`(?m)^type\s+([A-Z][A-Za-z0-9]*)\s`)
	voValueRe = regexp.MustCompile(`(?m)^func\s*\(\s*\w+\s+\*?([A-Z][A-Za-z0-9]*)\s*\)\s*Value\s*\(`)
	// A COMPOSITE value object declares no Value() — that absence is precisely
	// what makes it one — so IsValid is the second half of the test. Without it
	// every generated composite was invisible to the inventory: a field trying to
	// reuse one was refused as naming nothing, and `prune` read its file as an
	// orphan and offered to delete a type another spec still depended on.
	voIsValidRe = regexp.MustCompile(`(?m)^func\s*\(\s*\w+\s+\*?([A-Z][A-Za-z0-9]*)\s*\)\s*IsValid\s*\(`)
	// An ENUM value object is the one kind that does NOT write IsValid: it
	// declares its members and the answer for a value outside them, and the
	// framework checks membership itself. That is also the one distinction a
	// caller validating a value object IN PLACE has to make — an enum is
	// validated with domain.ValidateEnum, everything else by calling its own
	// IsValid — so the inventory records which kind each type is, not just that
	// it is one.
	voUnknownRe = regexp.MustCompile(`(?m)^func\s*\(\s*\w+\s+\*?([A-Z][A-Za-z0-9]*)\s*\)\s*UnknownNotification\s*\(`)
	// Hand-written files may group declarations — `type ( CPF string; UF string )`
	// — which the anchored form above never matches, so those types vanished
	// from the inventory and a legitimate reuse was refused as unknown.
	voGroupRe     = regexp.MustCompile(`(?ms)^type\s*\(\n(.*?)^\)`)
	voGroupNameRe = regexp.MustCompile(`(?m)^\s*([A-Z][A-Za-z0-9]*)\s`)
	voEntityRe    = regexp.MustCompile(`(?m)^//\s*entity:\s*([A-Za-z0-9_]+)`)
)

// discoverVOs reads internal/domain/vos and reports what is a value OBJECT,
// with the entity that owns each one.
//
// Two things it must not do, both learned the hard way:
//
// It must not skip generated files. The package's real value objects are
// generated — a shared base whose UF enum came from the first role is exactly
// the type the second role has to reuse — so skipping them left an inventory
// of whatever happened to be hand-written.
//
// It must not call every exported type a value object. Notifications live in
// this package too, in a hand-written file, so the inventory became a list of
// notifications, offered as the reuse candidates and taken as such. A value
// object is a type with a Value() method (the framework's contract); a
// notification has none. That is the test, rather than a suffix rule that a
// type named without one would slip past.
func discoverVOs(root string) ([]string, map[string]string, map[string]string) {
	dir := filepath.Join(root, "internal", "domain", "vos")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, map[string]string{}, map[string]string{}
	}
	var out []string
	owner := map[string]string{}
	kind := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		// A file may declare several types; only those the framework would treat
		// as a value object count — one that yields a scalar (Value) or one that
		// owns a rule over several fields (IsValid). A notification has neither,
		// which is what keeps this an inventory of value objects.
		isVO := map[string]bool{}
		for _, m := range voValueRe.FindAllSubmatch(b, -1) {
			isVO[string(m[1])] = true
		}
		selfValidating := map[string]bool{}
		for _, m := range voIsValidRe.FindAllSubmatch(b, -1) {
			isVO[string(m[1])] = true
			selfValidating[string(m[1])] = true
		}
		enum := map[string]bool{}
		for _, m := range voUnknownRe.FindAllSubmatch(b, -1) {
			enum[string(m[1])] = true
		}
		by := ""
		if bytes.Contains(b, []byte(generatedBanner)) {
			if m := voEntityRe.FindSubmatch(b); m != nil {
				by = string(m[1])
			}
		}
		var declared []string
		for _, m := range voTypeRe.FindAllSubmatch(b, -1) {
			declared = append(declared, string(m[1]))
		}
		for _, g := range voGroupRe.FindAllSubmatch(b, -1) {
			for _, m := range voGroupNameRe.FindAllSubmatch(g[1], -1) {
				declared = append(declared, string(m[1]))
			}
		}
		for _, name := range declared {
			if !isVO[name] {
				continue
			}
			if _, dup := owner[name]; dup {
				continue
			}
			owner[name] = by
			// A type that writes IsValid answers for itself whatever else it
			// declares; only the one that does NOT is an enum, and a type that
			// is neither is left unrecorded rather than guessed at — the caller
			// that needs the distinction refuses on "unknown" and says why.
			switch {
			case selfValidating[name]:
				kind[name] = "raw"
			case enum[name]:
				kind[name] = "enum"
			}
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, owner, kind
}

var ordinalRe = regexp.MustCompile(`^(\d+)_.*\.(up|down)\.sql$`)

func nextOrdinal(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1
	}
	max := 0
	for _, e := range entries {
		m := ordinalRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if n, err := strconv.Atoi(m[1]); err == nil && n > max {
			max = n
		}
	}
	return max + 1
}

// discoverSpecs reads the names the project's other specs already claim.
//
// It parses the two lines it needs rather than the whole language: a spec that
// this build could not load in full (written for a newer generator, or simply
// half-finished) still holds a name worth colliding with, and refusing to read
// it would make the check silently weaker exactly when the project is in flux.
func discoverSpecs(root string) []SpecClaim {
	entries, err := os.ReadDir(layout.DirIn(root))
	if err != nil {
		return nil
	}
	var out []SpecClaim
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), layout.SpecSuffix) {
			continue
		}
		path := filepath.Join(layout.DirIn(root), e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc struct {
			Entity  string `yaml:"entity"`
			Plural  string `yaml:"plural"`
			Storage struct {
				Table string `yaml:"table"`
				// The framework-managed columns, declared BY PRESENCE. They are
				// columns of the table like any other and a read join may
				// traverse onto them, so they are read here even though nothing
				// under fields[] ever mentions them.
				Managed struct {
					CreatedAt  string `yaml:"createdAt"`
					UpdatedAt  string `yaml:"updatedAt"`
					ArchivedAt string `yaml:"archivedAt"`
					Revision   string `yaml:"revision"`
				} `yaml:"managed"`
			} `yaml:"storage"`
			Fields []struct {
				Name     string `yaml:"name"`
				Column   string `yaml:"column"`
				Type     string `yaml:"type"`
				Nullable bool   `yaml:"nullable"`
				LivesOn  string `yaml:"livesOn"`
				VO       *struct {
					Kind string `yaml:"kind"`
					Ref  string `yaml:"ref"`
				} `yaml:"vo"`
				// A COMPOSITE field declares no column of its own: its value
				// spans several, one per part. Those columns are ordinary
				// columns of the table — they enter the schema's bijection
				// under the same rules a plain field's does — so a read join
				// may traverse onto one, and this is where they become visible.
				Parts []struct {
					Part     string `yaml:"part"`
					Column   string `yaml:"column"`
					As       string `yaml:"as"`
					Type     string `yaml:"type"`
					Nullable bool   `yaml:"nullable"`
				} `yaml:"parts"`
			} `yaml:"fields"`
			// The value objects THIS spec declares. A composite part states its
			// type only when the value object is reused from elsewhere; when it
			// is declared here, the type lives in the declaration and this is
			// the only place to read it from.
			ValueObjects []struct {
				Name  string `yaml:"name"`
				Parts []struct {
					Name     string `yaml:"name"`
					Type     string `yaml:"type"`
					Nullable bool   `yaml:"nullable"`
				} `yaml:"parts"`
			} `yaml:"valueObjects"`
			Children []struct {
				Name    string `yaml:"name"`
				Table   string `yaml:"table"`
				OwnedBy string `yaml:"ownedBy"`
				Fields  []struct {
					Name   string `yaml:"name"`
					Column string `yaml:"column"`
					Type   string `yaml:"type"`
				} `yaml:"fields"`
			} `yaml:"children"`
			Read struct {
				View struct {
					Name string `yaml:"name"`
				} `yaml:"view"`
			} `yaml:"read"`
		}
		if yaml.Unmarshal(b, &doc) != nil || doc.Entity == "" {
			continue
		}
		claim := SpecClaim{
			Path: filepath.Join(layout.Dir, e.Name()), Entity: doc.Entity,
			ViewName: doc.Read.View.Name, Route: doc.Plural,
			Table: doc.Storage.Table, Revision: doc.Storage.Managed.Revision,
		}
		// The framework-stamped columns are columns of the target's own table
		// like any other, and the READ path resolves them: whatever column the
		// spec puts in each managed slot, TableSchema's goNameForRead answers it
		// under a fixed logical name (CreatedAt, UpdatedAt, DeletedAt), which is
		// exactly what read.WithJoins checks a mapped column against. The NAMES
		// below are the logical ones and are the framework's; the columns are the
		// author's, which is why they are read from this file rather than
		// assumed. Nothing declares them under fields[] — they are declared BY
		// PRESENCE under storage.managed — so this is the only place they can
		// enter the claim, and while they did not, the generator refused a
		// traversal the framework accepts with the one message that is certainly
		// wrong: "not a column of X" — it is one.
		//
		// The archive column is NULLABLE, and the generator is the only side
		// that can say so. The framework deliberately does not police the
		// nullability of a managed slot: domain.Managed keeps those fields
		// unexported, so its reflective check has nothing to point at and
		// answers "not nullable" rather than guessing. A non-pointer Go field
		// therefore survives construction and fails on the first ACTIVE row
		// scanned — "never archived" being the normal state. The column is named
		// right here in the file, so this states what the framework cannot.
		//
		// The revision column is deliberately NOT claimed: the read path does
		// not resolve it, so a join onto it is refused rather than emitted.
		for _, mc := range []struct {
			name, column string
			nullable     bool
		}{
			{"CreatedAt", doc.Storage.Managed.CreatedAt, false},
			{"UpdatedAt", doc.Storage.Managed.UpdatedAt, false},
			{"DeletedAt", doc.Storage.Managed.ArchivedAt, true},
		} {
			if mc.column == "" {
				continue // declared by presence: no name, no column
			}
			claim.Fields = append(claim.Fields, FieldClaim{
				Name: mc.name, Column: mc.column, Type: "time",
				Nullable: mc.nullable, LivesOn: "root",
			})
		}
		for _, f := range doc.Fields {
			// A composite contributes its PARTS instead of itself: it owns no
			// column, and each part owns one.
			if len(f.Parts) > 0 {
				for _, part := range f.Parts {
					pType, pNull := part.Type, part.Nullable
					if pType == "" && f.VO != nil {
						// Declared in this file: the type is in the value
						// object, not restated on the field.
						for _, vo := range doc.ValueObjects {
							if vo.Name != f.VO.Ref {
								continue
							}
							for _, vp := range vo.Parts {
								if vp.Name == part.Part {
									pType = vp.Type
									pNull = pNull || vp.Nullable
								}
							}
						}
					}
					name := part.As
					if name == "" {
						name = part.Part
					}
					claim.Fields = append(claim.Fields, FieldClaim{
						Name: name, Column: part.Column, Type: pType,
						// An OPTIONAL composite stores every part column
						// NULL-able whatever the parts say: "every part NULL"
						// is how the absence of the whole value is written.
						Nullable: pNull || f.Nullable,
						LivesOn:  f.LivesOn,
					})
				}
				continue
			}
			// A field with no column is runtime-only or malformed; either way
			// there is nothing for a join to traverse onto.
			if f.Column == "" {
				continue
			}
			claim.Fields = append(claim.Fields, FieldClaim{
				Name: f.Name, Column: f.Column, Type: f.Type,
				Nullable: f.Nullable, LivesOn: f.LivesOn,
			})
		}
		for _, c := range doc.Children {
			cc := ChildClaim{Name: c.Name, Table: c.Table, OwnedBy: c.OwnedBy}
			for _, f := range c.Fields {
				cc.Fields = append(cc.Fields, FieldClaim{Name: f.Name, Column: f.Column, Type: f.Type})
			}
			claim.Children = append(claim.Children, cc)
		}
		out = append(out, claim)
	}
	return out
}
