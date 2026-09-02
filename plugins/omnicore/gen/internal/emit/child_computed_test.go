package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/fsplan"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A collection with a derived field, on an entity whose listing serves
// `?fields=` and whose by-id read does not.
//
// That asymmetry is the fixture's whole point. ONE <Child>RowResult serves both
// reads — the mapper is name-based, and two shapes for one collection is the
// thing the shared type exists to prevent — so the entry is sparse whenever the
// LISTING is, including inside the by-id Result, whose own fields are plain
// values. A per-entry seat that read the ROOT's shape unwrapped a pointer that
// was still a pointer and assigned a value where one was expected: two compile
// errors, in the tree the author was handed.
const childComputedSpec = `
specVersion: 1
entity: CestaE
plural: CestasE
language: pt-BR
storage:
  kind: flat
  table: cestas_e
  description: Cestas.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Codigo, type: string, column: codigo, length: 20, livesOn: root, example: "CST-1", description: O código.}
children:
  - name: LinhaE
    plural: LinhasE
    table: cesta_e_linhas
    parentColumn: cesta_e_id
    description: Linhas.
    ownedBy: root
    editStrategy: atomic-replace
    businessIdentity: [Sku]
    fields:
      - {name: Sku, type: string, column: sku, length: 20, example: "SKU-9", description: O item.}
      - {name: Observacao, type: string, column: observacao, length: 60, nullable: true, example: "sem gelo", description: "A observação, quando há."}
      - {name: ProdutoID, type: id, column: produto_id, example: 3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31, description: O produto.}
      - {name: RegistradoEm, type: time, column: registrado_em, example: "2026-02-01T09:00:00Z", description: Quando entrou.}
    computed:
      - name: Rotulo
        type: string
        from: [Sku, Observacao, ProdutoID, RegistradoEm]
        example: "SKU-9 (sem gelo)"
        description: O rótulo da linha.
modes: [display, insert, update, archive]
update: {shape: both}
delete: {root: soft}
read:
  backing: relational
  view: {name: cestas_e}
  byId: true
  byParams:
    filters:
      - {field: Codigo, ops: [eq]}
    controls: {pagination: true, fields: true}
surfaces: {rest: true}
authz:
  resource: cestae
  dataAccess: anyone-with-permission
  permissions: {insert: "cestae:escrever", update: "cestae:escrever", patch: "cestae:escrever", archive: "cestae:arquivar", read: "cestae:ler"}
`

func childComputedModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(childComputedSpec), "cesta.omnicore.yaml")
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

// emittedBody returns one emitted file's source, by path suffix.
func emittedBody(t *testing.T, files []fsplan.File, suffix string) string {
	t.Helper()
	for _, f := range files {
		if strings.HasSuffix(f.Path, suffix) {
			return string(f.Content)
		}
	}
	t.Fatalf("%s was not emitted", suffix)
	return ""
}

// The regression: the by-id seat must unwrap the ENTRY's pointers even though
// the by-id Result's own fields are values.
func TestThePerEntrySeatFollowsTheEntrysPointerDiscipline(t *testing.T) {
	files := emitAll(t, childComputedModel(t))
	byID := emittedBody(t, files, "internal/application/queries/find_cesta_e_by_id_query.go")

	if !strings.Contains(byID, "for i := range r.LinhasE {") {
		t.Fatalf("the by-id read does not derive per entry:\n%s", byID)
	}
	// The root's own derivation is absent here (this entity declares none), so
	// everything asserted below is the entry's.
	if !strings.Contains(byID, "if r.LinhasE[i].Sku != nil &&") {
		t.Error("a non-nullable source of a SPARSE entry must be guarded and dereferenced — " +
			"the entry is a pointer shape even inside the value-shaped by-id Result")
	}
	// The guard covers the sources that are pointers ONLY because the shape is
	// sparse, and no others: a nullable source is a pointer in the signature
	// too, so guarding it would refuse to derive for a legitimately absent value.
	if strings.Contains(byID, "r.LinhasE[i].Observacao != nil") {
		t.Error("a nullable source was guarded — its absence is a value the derivation decides about")
	}
	if !strings.Contains(byID, "*r.LinhasE[i].Sku") {
		t.Error("the source was passed as a pointer to a function that declares a value")
	}
	if !strings.Contains(byID, "r.LinhasE[i].Rotulo = &v") {
		t.Error("a derived value was assigned to a pointer field without taking its address")
	}
	// A NULLABLE source is a pointer on both sides, so it travels untouched.
	if strings.Contains(byID, "*r.LinhasE[i].Observacao") {
		t.Error("a nullable source is a pointer in the signature too — unwrapping it is a type error")
	}
}

// TestTheDerivationsLiveInQueriesUtils is about the SEAT, and the seat is the
// whole reason this file is not in the queries package.
//
// A derivation is called by the reads and by every write that answers with the
// record — that is what makes one surface unable to render a different value
// than another. While the hook sat in `queries`, the way the write side reached
// it was to import the entire queries package of the entity: a command
// depending on every read, for one function. `utils/` is the seat for exactly
// that shape, and both sides now import a leaf.
func TestTheDerivationsLiveInQueriesUtils(t *testing.T) {
	m := childComputedModel(t)
	files := emitAll(t, m)

	hook := emittedBody(t, files, computedHookFile(m))
	if !strings.HasPrefix(computedHookFile(m), "internal/application/queries/utils/") {
		t.Fatalf("the hook is not under queries/utils: %s", computedHookFile(m))
	}
	if !strings.Contains(hook, "\npackage utils\n") {
		t.Errorf("the hook must declare the package of the directory it is in:\n%s", hook)
	}

	// The reads call it QUALIFIED now: same package no longer.
	read := emittedBody(t, files, "internal/application/queries/find_cesta_e_by_id_query.go")
	if !strings.Contains(read, `qryutils "example.test/svc/internal/application/queries/utils"`) {
		t.Errorf("the read does not import the derivations it calls:\n%s", read)
	}
	if !strings.Contains(read, "qryutils.ComputeCestaELinhaERotulo(ctx,") {
		t.Errorf("the read calls the derivation unqualified, which no longer resolves:\n%s", read)
	}

	// And the write side reaches the leaf instead of the whole queries package.
	for _, f := range files {
		if !strings.HasPrefix(f.Path, "internal/application/commands/") ||
			!strings.HasSuffix(f.Path, ".go") {
			continue
		}
		body := string(f.Content)
		if strings.Contains(body, `"example.test/svc/internal/application/queries"`) {
			t.Errorf("%s imports the whole queries package — that dependency is what "+
				"moving the derivations was for", f.Path)
		}
	}
}

// rootComputedSpec derives at the ROOT, which is the half a per-entry fixture
// cannot show: only a root derivation reaches the WRITE side, where the same
// function fills the Result of a POST that answers with the record.
const rootComputedSpec = `
specVersion: 1
entity: Ficha
plural: Fichas
language: pt-BR
storage:
  kind: flat
  table: fichas
  description: Fichas.
  managed: {revision: revision, createdAt: created_at, updatedAt: updated_at, archivedAt: deleted_at}
fields:
  - {name: Nome, type: string, column: nome, length: 40, livesOn: root, example: "Ana", description: O nome.}
  - {name: Apelido, type: string, column: apelido, length: 40, livesOn: root, example: "Aninha", description: O apelido.}
modes: [display, insert, update]
update: {shape: both}
read:
  backing: relational
  view: {name: fichas}
  byId: true
  byParams:
    filters:
      - {field: Nome, ops: [eq]}
    controls: {pagination: true}
  computed:
    - name: Rotulo
      type: string
      from: [Nome, Apelido]
      example: "Ana (Aninha)"
      description: O rótulo.
surfaces: {rest: true}
authz:
  resource: ficha
  dataAccess: anyone-with-permission
  permissions: {insert: "ficha:escrever", update: "ficha:escrever", patch: "ficha:escrever", read: "ficha:ler"}
`

func rootComputedModel(t *testing.T) *ir.Model {
	t.Helper()
	s, err := spec.Parse([]byte(rootComputedSpec), "ficha.omnicore.yaml")
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

// TestTheWriteSideDerivesThroughTheSharedSeat is the other half of the reason
// the derivations moved: a POST that answers with the record renders the SAME
// derived value the GET does, because both call one function. The write used to
// reach it by importing the entity's whole queries package.
func TestTheWriteSideDerivesThroughTheSharedSeat(t *testing.T) {
	m := rootComputedModel(t)
	files := emitAll(t, m)

	insert := emittedBody(t, files, "internal/application/commands/insert_ficha_command.go")
	if !strings.Contains(insert, `qryutils "example.test/svc/internal/application/queries/utils"`) {
		t.Errorf("the write does not import the seat it derives through:\n%s", insert)
	}
	if !strings.Contains(insert, "qryutils.ComputeFichaRotulo(ctx,") {
		t.Errorf("the write does not call the same derivation the reads call:\n%s", insert)
	}
	if strings.Contains(insert, `"example.test/svc/internal/application/queries"`) {
		t.Errorf("a command must not depend on every read of the entity to reach one "+
			"derivation:\n%s", insert)
	}
	// And the seat itself declares it once, for both callers.
	hook := emittedBody(t, files, computedHookFile(m))
	if !strings.Contains(hook, "func ComputeFichaRotulo(") {
		t.Errorf("the derivation is not declared where both sides import it:\n%s", hook)
	}
}

// The derivation is named for BOTH owners. Every entity of a project writes into
// one queries package, and two collections of one entity may each want a Rotulo.
func TestAPerEntryDerivationIsNamedForItsEntityAndItsCollection(t *testing.T) {
	m := childComputedModel(t)
	hook := emittedBody(t, emitAll(t, m), computedHookFile(m))
	if !strings.Contains(hook, "func ComputeCestaELinhaERotulo(") {
		t.Errorf("the derivation is not qualified by entity and collection:\n%s", hook)
	}
	if !strings.Contains(hook, "ONCE PER ENTRY of LinhasE") {
		t.Error("the stub does not tell the implementer how often their body runs")
	}
}

// The wire tag is what makes the field work rather than merely appear, and at a
// nested level it must name the sources BARE: the framework records them under
// the same segment prefix as the field itself.
func TestThePerEntryComputedTagNamesItsSourcesBare(t *testing.T) {
	rows := emittedBody(t, emitAll(t, childComputedModel(t)), "internal/web/requests/dtos/linha_e.go")
	if !strings.Contains(rows, `computed:"Sku,Observacao,ProdutoID,RegistradoEm"`) {
		t.Errorf("the entry's computed tag is missing or prefixed:\n%s", rows)
	}
}

// The hook file's imports are decided from the types it writes, not assumed.
//
// It carried `application/configuration` and nothing else, on the assumption
// that a derivation only ever sees builtins. It does not: `type: id` is
// domain.ID and `type: time` is time.Time, in a source or in the derived value.
// One such declaration — at the ROOT as much as per entry — emitted a hook that
// did not compile, in a write-once file the author is then left to repair by
// hand. It went unseen because no fixture that BUILDS had ever derived from an
// id.
func TestTheDerivationHookImportsTheTypesItNames(t *testing.T) {
	m := childComputedModel(t)
	hook := emittedBody(t, emitAll(t, m), computedHookFile(m))
	for _, want := range []string{`"time"`, `omnicore/domain"`} {
		if !strings.Contains(hook, want) {
			t.Errorf("the hook does not import %s, and it declares a parameter of that "+
				"package:\n%s", want, hook)
		}
	}
}

// And the generated test file alongside it, which BUILDS one of each: a
// derivation case fills every source, and literalFor spells an id as
// domain.NewID and a timestamp as time.Date.
func TestTheQueryTestImportsTheTypesItBuilds(t *testing.T) {
	body := emittedBody(t, emitAll(t, childComputedModel(t)),
		"internal/application/queries/find_cestas_e_by_params_query_test.go")
	for _, want := range []string{`"time"`, `omnicore/domain"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the generated test does not import %s, and it constructs one:\n%s", want, body)
		}
	}
}

// A derived field is NOT part of what the store holds, at either level.
//
// The consequence is a promise to the author, not an internal detail: nothing
// projects it, so adding one to a materialised view changes no stored shape and
// costs no read.view.version bump and no rebuild. If it ever leaked into the
// TableSchema or the view definition, the framework would be asked to project a
// column that does not exist — and the author would be told to bump a version
// for a change that stored nothing.
//
// It is asserted on a MONGO backing on purpose. A relational read model
// materialises nothing, so ViewShape answers "" for it and every assertion here
// would pass without ever reaching the question — a test that proves nothing
// while looking like it does. The two guards below say so out loud rather than
// leaving it to be discovered.
func TestADerivedFieldIsNeverProjected(t *testing.T) {
	src := strings.Replace(childComputedSpec,
		"  backing: relational\n  view: {name: cestas_e}",
		"  backing: mongo\n  view: {name: cestas_e, version: 1}", 1)
	sp, err := spec.Parse([]byte(src), "cesta.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(sp, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("the mongo fixture does not validate:\n%v", ps.Error())
	}
	m, err := ir.Resolve(sp, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	shape := ViewShape(m)
	if shape == "" {
		t.Fatal("the fixture materialises nothing, so this test would prove nothing")
	}
	if strings.Contains(shape, "Rotulo") {
		t.Errorf("the view-shape fingerprint counts a derived field, so adding one would "+
			"demand a version bump for a change that stores nothing:\n%s", shape)
	}

	saw := 0
	for _, f := range emitAll(t, m) {
		if !strings.Contains(f.Path, "internal/infra/") {
			continue
		}
		saw++
		if strings.Contains(string(f.Content), "Rotulo") {
			t.Errorf("%s projects a derived field — there is no column behind it", f.Path)
		}
	}
	if saw == 0 {
		t.Fatal("no infra file was inspected, so nothing was actually asserted")
	}
}

// A hook written before the derivations were qualified by entity is REFUSED,
// not renamed around.
//
// On its own the rename is a loud, harmless failure — the call sites move and
// the linker names the missing symbol. What is not harmless is the file: a hook
// is written once and never rewritten, so by the time this lands the body is the
// author's work. A run that says nothing leaves them with a function nobody
// calls beside call sites for a function nobody wrote, and no statement anywhere
// that the two are the same derivation.
func TestAHookCarryingTheOldUnqualifiedNameIsRefused(t *testing.T) {
	// A ROOT derivation, because that is the only kind a project can carry the
	// old name for: the per-entry seat is new, so no tree predates its naming.
	src := strings.Replace(childComputedSpec,
		"  backing: relational",
		`  computed:
    - {name: Resumo, type: string, from: [Codigo], example: x, description: Um resumo.}
  backing: relational`, 1)
	sp, err := spec.Parse([]byte(src), "cesta.omnicore.yaml")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if ps := spec.Validate(sp, spec.Options{}); ps.HasBlockers() {
		t.Fatalf("the fixture does not validate:\n%v", ps.Error())
	}
	m, err := ir.Resolve(sp, &discover.Project{
		ModulePath: "example.test/svc", Dialects: []string{"sqlite"}, Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "internal/application/queries/utils")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(dir, "cesta_e_computed_manual.go")

	// Nothing on disk yet: there is no rename to report.
	if got := StaleDerivationNames(root, m); len(got) != 0 {
		t.Fatalf("a project with no hook was reported as stale: %v", got)
	}

	// The shape an older generation left behind.
	old := "package queries\n\nfunc ComputeResumo(ctx int) (string, error) { return \"mine\", nil }\n"
	if err := os.WriteFile(hook, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	got := StaleDerivationNames(root, m)
	if len(got) != 1 {
		t.Fatalf("the stale name was not reported: %v", got)
	}
	for _, want := range []string{"cesta_e_computed_manual.go", "ComputeResumo", "ComputeCestaEResumo"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("the refusal does not name %q — the author cannot act on it: %s", want, got[0])
		}
	}

	// Once renamed, it is silent again: the check must not survive the fix.
	renamed := strings.Replace(old, "ComputeResumo", "ComputeCestaEResumo", 1)
	if err := os.WriteFile(hook, []byte(renamed), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := StaleDerivationNames(root, m); len(got) != 0 {
		t.Fatalf("the refusal persists after the rename: %v", got)
	}
}

// The report is the hand-off, so the signature it prints has to be the one on
// disk — byte for byte, both scopes.
//
// They were two format strings that happened to agree, which is a pair that
// agrees until somebody edits one. A reviewer copying a stale signature writes a
// function nothing calls, in the one file the generator will never correct.
func TestTheReportsSignatureIsTheEmittedOne(t *testing.T) {
	m := childComputedModel(t)
	hook := emittedBody(t, emitAll(t, m),
		computedHookFile(m))

	seen := 0
	for _, c := range m.Read.Computed {
		seen++
		if !strings.Contains(hook, ComputedSignature(m, c)+" {") {
			t.Errorf("the report prints a root signature the hook does not carry:\n  %s",
				ComputedSignature(m, c))
		}
	}
	for _, ch := range m.Children {
		for _, c := range ch.Computed {
			seen++
			if !strings.Contains(hook, ChildComputedSignature(m, ch, c)+" {") {
				t.Errorf("the report prints a per-entry signature the hook does not carry:\n  %s",
					ChildComputedSignature(m, ch, c))
			}
		}
	}
	if seen == 0 {
		t.Fatal("the fixture declares no derivation, so nothing was compared")
	}
}
