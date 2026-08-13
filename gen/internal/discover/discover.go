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

	// ExistingVOs lists only the HAND-WRITTEN value objects. One this generator
	// wrote is ours to rewrite, so counting it as foreign would make the second
	// run refuse what the first run created.
	ExistingVOs []string
	HasMongo    bool
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
	p.ExistingVOs = discoverVOs(root)
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
const generatedBanner = "Code generated by omnicore-gen"

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

func discoverVOs(root string) []string {
	dir := filepath.Join(root, "internal", "domain", "vos")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	typeRe := regexp.MustCompile(`(?m)^type\s+([A-Z][A-Za-z0-9]*)\s`)
	var out []string
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if bytes.Contains(b, []byte(generatedBanner)) {
			continue
		}
		for _, m := range typeRe.FindAllSubmatch(b, -1) {
			name := string(m[1])
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
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
