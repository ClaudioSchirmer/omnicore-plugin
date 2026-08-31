package emit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/discover"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/ir"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/spec"
)

// A stamped column is declared by a DIFFERENT schema builder and is absent from
// every write surface. Both halves are invisible to a compiler: emitting
// Field(...) instead of StampedTimeField(...) builds fine and produces a column
// the framework writes from the struct — which is to say, never — and putting
// the field on a request DTO builds fine and accepts a value nothing stores.

func stampedModel(t *testing.T) *ir.Model {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", "specs", "matrix", "43-carimbos.yaml")
	s, err := spec.Load(p)
	if err != nil {
		t.Fatalf("loading the stamped matrix case: %v", err)
	}
	m, err := ir.Resolve(s, &discover.Project{
		ModulePath: "example.test/svc",
		Dialects:   []string{"sqlite"},
		Root:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("resolving the stamped matrix case: %v", err)
	}
	return m
}

// TestTheSchemaDeclaresTheStampedBuilders is the whole point of the key: the
// schema is what tells the framework the column is never written from the struct
// and what filling it means.
func TestTheSchemaDeclaresTheStampedBuilders(t *testing.T) {
	src := goSources(emitAll(t, stampedModel(t)))
	var schema string
	for path, body := range src {
		if strings.HasSuffix(path, "_schema.go") {
			schema = body
		}
	}
	if schema == "" {
		t.Fatal("no schema file was emitted")
	}
	for _, want := range []string{
		`StampedTimeField("CanceladaEm", "cancelada_em")`,
		`StampedCounterField("TotalDeCobrancas", "total_de_cobrancas")`,
		`StampedCounterField("FalhasSeguidas", "falhas_seguidas")`,
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("the schema does not declare %s:\n%s", want, schema)
		}
	}
	// The needle carries the leading tabs on purpose: `Field("CanceladaEm"` is a
	// substring of `StampedTimeField("CanceladaEm"`, so the naive check passes
	// on the correct output and proves nothing.
	for _, unwanted := range []string{
		"\t\tField(\"CanceladaEm\"",
		"\t\tField(\"TotalDeCobrancas\"",
	} {
		if strings.Contains(schema, unwanted) {
			t.Errorf("a stamped column was declared as an ordinary field (%s) — the "+
				"framework would then expect a value from the struct, and there is never "+
				"one:\n%s", unwanted, schema)
		}
	}
}

// TestNoWriteSurfaceCarriesAStampedField. The Result and the Response DO carry
// it — the value is the row's and the caller receives it — so this asserts on
// the input half by name rather than on the files as a whole.
func TestNoWriteSurfaceCarriesAStampedField(t *testing.T) {
	m := stampedModel(t)
	for _, f := range m.WritableFields() {
		if f.Stamped != "" {
			t.Errorf("%s is stamped and reached the write surface — a request DTO, a "+
				"command and a mapper would be generated for a value the framework "+
				"overwrites", f.Name)
		}
	}
	for _, f := range ir.Mappable(m.WritableFields()) {
		if f.Stamped != "" {
			t.Errorf("%s is stamped and the mapper assigns it; assigning the Go field "+
				"does nothing, so the generated line would be a silent no-op", f.Name)
		}
	}
	if got := len(m.StampedFields()); got != 3 {
		t.Fatalf("the matrix case declares three stamped columns, StampedFields returned %d", got)
	}
}

// TestTheStampedColumnsAreStillOrdinaryEverywhereElse. The failure this guards
// is over-correction: a column the read side stops projecting because the write
// side does not touch it. Only the write is special.
func TestTheStampedColumnsAreStillOrdinaryEverywhereElse(t *testing.T) {
	m := stampedModel(t)
	files := emitAll(t, m)
	var migration string
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".up.sql") {
			migration = string(f.Content)
		}
	}
	for _, want := range []string{"cancelada_em", "total_de_cobrancas", "falhas_seguidas"} {
		if !strings.Contains(migration, want) {
			t.Errorf("the migration does not create %q — a stamped column is an ordinary "+
				"column of the table:\n%s", want, migration)
		}
	}
	// NOT NULL on the counter and NULL on the time is the shape the framework
	// declares, and the one the spec validator holds the author to.
	if !strings.Contains(migration, `"total_de_cobrancas" INTEGER NOT NULL`) {
		t.Errorf("the counter column is not NOT NULL:\n%s", migration)
	}
	if !strings.Contains(migration, `"cancelada_em" TEXT NULL`) {
		t.Errorf("the stamped time column is not nullable:\n%s", migration)
	}
}

// TestTheAggregateFieldSaysItIsNotAssignable. The struct is where somebody about
// to write `e.CanceladaEm = time.Now()` is looking, and that assignment is the
// one mistake here that neither the compiler nor the framework reports.
func TestTheAggregateFieldSaysItIsNotAssignable(t *testing.T) {
	src := goSources(emitAll(t, stampedModel(t)))
	var aggregate string
	for path, body := range src {
		if strings.HasSuffix(path, "internal/domain/assinatura.go") {
			aggregate = body
		}
	}
	if aggregate == "" {
		t.Fatal("the aggregate file was not emitted")
	}
	// The verb list is part of the warning and not decoration: StampNull is
	// listed on the two fields that can hold an absence and left off the plain
	// int64 counter, which has none — asking it there is refused by the write.
	for _, want := range []string{
		`Stamp/StampNull/StampEmpty("CanceladaEm"), never assign`,
		`Stamp/StampEmpty("TotalDeCobrancas"), never assign`,
		`Stamp/StampNull/StampEmpty("FalhasSeguidas"), never assign`,
	} {
		if !strings.Contains(aggregate, want) {
			t.Errorf("the aggregate does not warn about %s:\n%s", want, aggregate)
		}
	}
	if strings.Contains(aggregate, `StampNull/StampEmpty("TotalDeCobrancas")`) {
		t.Errorf("the plain int64 counter offers StampNull, which the write refuses "+
			"— it has no absence to hold:\n%s", aggregate)
	}
}
