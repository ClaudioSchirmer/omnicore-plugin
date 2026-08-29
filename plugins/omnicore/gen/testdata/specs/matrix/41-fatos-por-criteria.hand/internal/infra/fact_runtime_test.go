//go:build sqlite

package infra

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ClaudioSchirmer/omnicore/domain"
	"github.com/ClaudioSchirmer/omnicore/infra/db/core"
	_ "github.com/ClaudioSchirmer/omnicore/infra/db/engine/sqlite"
)

// THIS FILE IS PART OF THE FIXTURE, not of the generated output — see the
// matrix lane's `.hand/` mechanism.
//
// The operator vocabulary is a translation: every op of the spec maps to one
// builder of the framework's criteria package. An emit test proves the mapping
// is WRITTEN; only running the query proves it is RIGHT. The two failures that
// survive a compile are the ones this file exists for: an operator that
// resolves to the wrong column, and one whose value binds in a form the engine
// compares against nothing (a set spread into ...any, a pinned enum written as
// the member's name instead of its stored value).

const fila = "6f0f6f2a-6f9f-4a6a-9f9f-2b7a9c5f21d4"
const outraFila = "7a1a7a3b-7a0a-4b7b-8a0a-3c8b0d6a32e5"

func seed(t *testing.T) *ChamadoServiceImpl {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer db.Close()

	ddl := `CREATE TABLE "chamados" (
	  "id" TEXT NOT NULL, "codigo" TEXT NOT NULL, "titulo" TEXT NOT NULL,
	  "situacao" TEXT NOT NULL, "prioridade" INTEGER NOT NULL, "nota" REAL NOT NULL,
	  "fila_id" TEXT NOT NULL, "fechado_em" TEXT NULL,
	  "revision" INTEGER NOT NULL DEFAULT 0,
	  "created_at" TEXT NOT NULL DEFAULT '2026-01-01T00:00:00.000Z',
	  "updated_at" TEXT NOT NULL DEFAULT '2026-01-01T00:00:00.000Z',
	  "deleted_at" TEXT NULL,
	  CONSTRAINT "chamados_pkey" PRIMARY KEY ("id"))`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}

	rows := []struct {
		id, codigo, titulo, situacao string
		prioridade                   int
		nota                         float64
		filaID                       string
		fechado                      any
	}{
		// The stored enum values are the lowercase ones the spec declares; the
		// spec pins MEMBER NAMES, so a fact matching here proves the generator
		// wrote the stored value and not the name.
		{"11111111-1111-4111-8111-111111111111", "CH-2026-001", "Impressora sem tinta", "aberto", 3, 7.0, fila, nil},
		{"22222222-2222-4222-8222-222222222222", "CH-2026-002", "Impressora atolada", "em-analise", 5, 9.0, fila, nil},
		{"33333333-3333-4333-8333-333333333333", "CH-2026-003", "Monitor piscando", "resolvido", 1, 4.0, fila, "2026-03-01T00:00:00.000Z"},
		{"44444444-4444-4444-8444-444444444444", "CH-2026-004", "Teclado quebrado", "cancelado", 2, 5.0, fila, "2026-04-01T00:00:00.000Z"},
		// Another queue entirely: every fact narrowed by FilaID must miss it.
		{"55555555-5555-4555-8555-555555555555", "XX-2026-005", "Impressora sem tinta", "aberto", 5, 9.0, outraFila, nil},
	}
	for _, r := range rows {
		_, err := db.Exec(
			`INSERT INTO "chamados" (id, codigo, titulo, situacao, prioridade, nota, fila_id, fechado_em)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r.id, r.codigo, r.titulo, r.situacao, r.prioridade, r.nota, r.filaID, r.fechado)
		if err != nil {
			t.Fatalf("seeding %s: %v", r.codigo, err)
		}
	}

	eng, err := core.NewEngine("sqlite", context.Background(), core.EngineConfig{DSN: path})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return NewChamadoServiceImpl(NewChamadoRepository(eng))
}

// A set PINNED in the spec, over an enum: the query must carry the members'
// STORED values, and the `notnull` beside it must read the right column.
func TestPinnedSetOverAnEnumMatchesStoredValues(t *testing.T) {
	// resolvido + cancelado, both closed, both in this queue → 2.
	if got := seed(t).EncerradosNaFila(domain.NewID(fila)); got != 2 {
		t.Errorf("EncerradosNaFila = %d, want 2 — CH-003 and CH-004 are in the pinned "+
			"set AND have a closing date", got)
	}
}

// The set as a PARAMETER, spread into criteria's ...any through the generated
// widener — and an empty one, which the framework defines as matching nothing.
func TestSetParameterIsComparedAsASet(t *testing.T) {
	svc := seed(t)

	if got := svc.AbertosNasSituacoes(domain.NewID(fila), []string{"aberto", "em-analise"}); got != 2 {
		t.Errorf("AbertosNasSituacoes([aberto em-analise]) = %d, want 2", got)
	}
	if got := svc.AbertosNasSituacoes(domain.NewID(fila), []string{"aberto"}); got != 1 {
		t.Errorf("AbertosNasSituacoes([aberto]) = %d, want 1", got)
	}
	// An empty set is a legal predicate and not an error: `IN ()` matches
	// nothing on both of the framework's stores.
	if got := svc.AbertosNasSituacoes(domain.NewID(fila), nil); got != 0 {
		t.Errorf("AbertosNasSituacoes(nil) = %d, want 0 — an empty IN matches nothing", got)
	}
}

// The connectives, and the negation around one of them.
func TestConnectivesNarrowTheWayTheyRead(t *testing.T) {
	svc := seed(t)
	var noSelf domain.ID

	// any: title EXACTLY "Impressora sem tinta", or (title contains "Impressora"
	// AND prioridade >= 5). not(any(situacao = cancelado, fechado_em notnull))
	// drops CH-003 and CH-004.
	//   CH-001 matches the exact title, is open        → in
	//   CH-002 matches contains+prioridade 5, is open  → in
	//   CH-005 matches the title but is in ANOTHER queue → out
	if got := svc.ParecidosNaFila(domain.NewID(fila), "Impressora sem tinta", "Impressora", 5, noSelf); got != 2 {
		t.Errorf("ParecidosNaFila = %d, want 2 (CH-001 by exact title, CH-002 by "+
			"contains + priority)", got)
	}

	// excludeSelf really excludes: the same question, minus CH-001 itself.
	self := domain.NewID("11111111-1111-4111-8111-111111111111")
	if got := svc.ParecidosNaFila(domain.NewID(fila), "Impressora sem tinta", "Impressora", 5, self); got != 1 {
		t.Errorf("ParecidosNaFila excluding CH-001 = %d, want 1", got)
	}
}

// The rest of the vocabulary in one query: startswith, endswith, ne, gt, lt,
// lte and a pinned nin.
func TestTheRemainingOperatorsRun(t *testing.T) {
	svc := seed(t)

	// codigo LIKE 'CH-2026-%' AND codigo LIKE '%2' AND codigo <> 'CH-2026-003'
	// AND prioridade > 1 AND prioridade < 6 AND nota <= 9 AND situacao NOT IN ('cancelado')
	//   CH-002 is the only code ending in "2" → 1.
	if got := svc.VizinhosDoCodigo("CH-2026-", "2", "CH-2026-003", 1, 6, 9); got != 1 {
		t.Errorf("VizinhosDoCodigo = %d, want 1 (CH-002 alone ends in 2)", got)
	}
	// Move the ceiling below CH-002's score and it drops out.
	if got := svc.VizinhosDoCodigo("CH-2026-", "2", "CH-2026-003", 1, 6, 8); got != 0 {
		t.Errorf("VizinhosDoCodigo with nota <= 8 = %d, want 0 — CH-002 scores 9", got)
	}
	// startswith really anchors: the other queue's code starts with XX.
	if got := svc.VizinhosDoCodigo("XX-", "5", "nada", 1, 6, 9); got != 1 {
		t.Errorf("VizinhosDoCodigo on the XX prefix = %d, want 1", got)
	}
}

// `not` over SEVERAL nodes is NOT "neither of these". They are ANDed before the
// negation, so this reads "not (open AND high priority)" — and both readings
// produce a query that runs, which is why the difference is asserted here and
// not left to the doc comment.
func TestNotOverSeveralNodesNegatesTheirConjunction(t *testing.T) {
	svc := seed(t)

	// CH-001 is the only open ticket in this queue at priority >= 3, so it is
	// the only row the negation drops: 4 active in the queue, 3 survive.
	if got := svc.ForaDoFocoNaFila(domain.NewID(fila), 3); got != 3 {
		t.Errorf("ForaDoFocoNaFila(>=3) = %d, want 3 — only CH-001 is both open and "+
			"at or above 3", got)
	}
	// Raise the bar past CH-001's priority and NOTHING is both: every row
	// survives. "Neither open nor prioritised" would have answered 2 here.
	if got := svc.ForaDoFocoNaFila(domain.NewID(fila), 4); got != 4 {
		t.Errorf("ForaDoFocoNaFila(>=4) = %d, want 4 — no row satisfies both halves, "+
			"so the negation drops none", got)
	}
}

// `nin` as a PARAMETER, and the empty set at the other end of the mirror: the
// framework reads NOT IN () as "matches everything", where IN () matches
// nothing.
func TestExcludedSetIsComparedAsASet(t *testing.T) {
	svc := seed(t)

	if got := svc.ForaDasSituacoes(domain.NewID(fila), []string{"aberto", "cancelado"}); got != 2 {
		t.Errorf("ForaDasSituacoes([aberto cancelado]) = %d, want 2 (CH-002, CH-003)", got)
	}
	if got := svc.ForaDasSituacoes(domain.NewID(fila), nil); got != 4 {
		t.Errorf("ForaDasSituacoes(nil) = %d, want 4 — an empty NOT IN excludes nothing", got)
	}
}
