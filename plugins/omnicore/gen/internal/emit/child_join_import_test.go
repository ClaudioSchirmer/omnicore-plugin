package emit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// childJoinTimeSpec is a collection that gains a TIMESTAMP through a read join.
//
// The two timestamp fields are the same defect wearing two hats: one lands on
// the target's framework-stamped archive slot, the other on an ordinary column
// the target declares itself. Neither is a field of the collection, so an
// import decided from the collection's own fields misses both — which is why
// the stamped one only LOOKED like a consequence of joins learning to traverse
// onto a managed slot.
const childJoinTimeSpec = `
specVersion: 1
entity: Papel
plural: Papeis
language: pt-BR
storage:
  kind: flat
  table: papeis
  description: Papéis do sistema.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 120, livesOn: root, example: Admin, description: O nome do papel.}
children:
  - name: PapelPermissao
    plural: PapelPermissoes
    table: papel_permissoes
    parentColumn: papel_id
    description: As permissões concedidas ao papel.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [PermissaoID]
    fields:
      - {name: PermissaoID, type: id, column: permissao_id, example: 1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4, description: A permissão concedida.}
joins:
  - to: Permissao
    kind: inner
    on: permissao_id
    inChild: PapelPermissao
    fields:
      - {name: PermissaoNome, type: string, column: nome, example: "papel:ler", description: Nome da permissão.}
      - {name: PermissaoArquivadaEm, type: time, column: deleted_at, nullable: true, example: "2026-01-01T00:00:00Z", description: Quando a permissão foi arquivada.}
      - {name: PermissaoVigenteEm, type: time, column: vigente_em, example: "2026-01-01T00:00:00Z", description: Desde quando a permissão vale.}
modes: [display, insert]
read:
  backing: relational
  view: {name: papeis}
  byId: true
  byParams:
    filters: [{field: Nome, ops: [eq]}]
    sort: [Nome]
    controls: {pagination: true, orderBy: true, fields: true}
surfaces: {rest: true}
authz:
  resource: papel
  dataAccess: anyone-with-permission
  permissions: {insert: "papel:escrever", read: "papel:ler"}
`

func childJoinTimeModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(childJoinTimeSpec), "papel.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(s, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("the fixture does not validate:\n%v", ps.Error())
	}
	m, err := ir.Resolve(s, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	return m
}

// TestChildRowResultImportsTheTimeItNames is the regression.
//
// The collection's read shape carries the join's timestamps, and the import
// block is decided from the types the file writes. Deciding it from the
// collection's PERSISTED fields alone emitted `time.Time` under no `time`
// import — a file `check` calls generatable and `generate` writes happily,
// which then fails at `go build` in the consumer's tree. Both columns are
// asserted because both are the same defect, and only one of them is new.
func TestChildRowResultImportsTheTimeItNames(t *testing.T) {
	files := emitAll(t, childJoinTimeModel(t))
	got := ""
	for _, f := range files {
		if strings.HasSuffix(f.Path, "internal/application/queries/papel_row_results.go") {
			got = string(f.Content)
		}
	}
	if got == "" {
		t.Fatal("no child row-result file was emitted at all")
	}

	for _, field := range []string{"PermissaoArquivadaEm", "PermissaoVigenteEm"} {
		if !strings.Contains(got, field) {
			t.Fatalf("%s never reached the child row result, so this test proves nothing:\n%s",
				field, got)
		}
	}
	if !strings.Contains(got, "time.Time") {
		t.Fatalf("no timestamp reached the child row result, so this test proves nothing:\n%s", got)
	}
	if !strings.Contains(got, `"time"`) {
		t.Errorf("the child row result names time.Time and imports no time:\n%s", got)
	}
	assertEmittedImportsResolve(t, files)
}

// TestEveryEmittedFileImportsWhatItQualifies makes that assertion structural,
// over every Go file every matrix spec emits.
//
// internal/gofile prunes the OPPOSITE class — an import nothing uses — once,
// for every emitter at once. Nothing covered this one: a qualifier used with no
// import is invisible to `check`, survives `generate`, and lands as a build
// error in the consumer's tree. Asserting the property over the whole corpus
// means an emitter that grows a new type cannot forget its import quietly, and
// a spec added to the matrix widens the guard for free.
func TestEveryEmittedFileImportsWhatItQualifies(t *testing.T) {
	for name, m := range matrixModels(t) {
		t.Run(name, func(t *testing.T) {
			assertEmittedImportsResolve(t, emitAll(t, m))
		})
	}
}

// assertEmittedImportsResolve reports every `pkg.Name` qualifier an emitted file
// uses that nothing provides.
//
// A qualifier is unexplained only when it is neither an import of the file nor
// a package-level name declared in the PACKAGE — sibling files of one directory
// share a namespace, and a const declared in one of them is reached by bare
// name from another.
func assertEmittedImportsResolve(t *testing.T, files []fsplan.File) {
	t.Helper()

	type parsed struct {
		path string
		file *ast.File
	}
	fset := token.NewFileSet()
	byDir := map[string][]parsed{}
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		af, err := parser.ParseFile(fset, f.Path, f.Content, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: the emitted file does not parse: %v", f.Path, err)
		}
		dir := path.Dir(f.Path)
		byDir[dir] = append(byDir[dir], parsed{f.Path, af})
	}

	for dir, group := range byDir {
		declared := map[string]bool{}
		for _, p := range group {
			collectPackageLevelNames(p.file, declared)
		}
		for _, p := range group {
			assertOneFilesImports(t, dir, p.path, p.file, declared)
		}
	}
}

func assertOneFilesImports(t *testing.T, dir, filePath string, f *ast.File, declared map[string]bool) {
	t.Helper()

	provided := map[string]bool{}
	for _, imp := range f.Imports {
		provided[importQualifier(imp)] = true
	}

	missing := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		// Obj is set for a name the FILE declares or binds; declared covers the
		// rest of the package. Either way the selector is a field or a method,
		// not a package qualifier.
		if ident.Obj != nil || declared[ident.Name] || provided[ident.Name] {
			return true
		}
		missing[ident.Name] = fmt.Sprintf("%s.%s", ident.Name, sel.Sel.Name)
		return true
	})
	if len(missing) == 0 {
		return
	}
	var uses []string
	for _, u := range missing {
		uses = append(uses, u)
	}
	sort.Strings(uses)
	t.Errorf("%s (package %s) uses %s and imports nothing that provides it — the file cannot compile",
		filePath, dir, strings.Join(uses, ", "))
}

// majorVersionSuffix is the module-path tail that is not a package name:
// github.com/gofiber/fiber/v2 is imported as `fiber`.
var majorVersionSuffix = regexp.MustCompile(`^v[0-9]+$`)

func importQualifier(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	p := strings.Trim(imp.Path.Value, `"`)
	segs := strings.Split(p, "/")
	last := segs[len(segs)-1]
	if majorVersionSuffix.MatchString(last) && len(segs) > 1 {
		last = segs[len(segs)-2]
	}
	return last
}

// collectPackageLevelNames gathers what the file contributes to its package's
// namespace: types, functions, and package-level consts and vars.
func collectPackageLevelNames(f *ast.File, into map[string]bool) {
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Recv == nil {
				into[decl.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, s := range decl.Specs {
				switch sp := s.(type) {
				case *ast.TypeSpec:
					into[sp.Name.Name] = true
				case *ast.ValueSpec:
					for _, id := range sp.Names {
						into[id.Name] = true
					}
				}
			}
		}
	}
}
