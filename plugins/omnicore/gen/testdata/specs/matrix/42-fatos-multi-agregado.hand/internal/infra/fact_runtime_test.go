//go:build sqlite

package infra

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClaudioSchirmer/omnicore/domain"
	"github.com/ClaudioSchirmer/omnicore/infra/db/core"
	_ "github.com/ClaudioSchirmer/omnicore/infra/db/engine/sqlite"
)

// THIS FILE IS PART OF THE FIXTURE, not of the generated output. The matrix
// lane lays a case's `.hand/` tree over the staged host before generating, and
// then runs `go test ./internal/...` — which is what makes a RUNTIME assertion
// possible at all in a gate whose other lanes stop at "it compiles".
//
// The engine's own package registers the "sqlite" driver name, so opening a
// plain *sql.DB against the same file needs no second driver import.
//
// This is the half generating and compiling cannot prove: that the queries the
// emitter writes actually RUN, and answer what the spec said, against a real
// engine with real NULLs.
//
// Three things are being measured, and each of them was a guess until now:
//   - one Aggregate/AggregateBy call carrying SEVERAL specs comes back with
//     every one of them filled;
//   - a group whose aggregated column is NULL in every row reports Found=false
//     rather than a zero that reads as an answer;
//   - a comparison against a framework-stamped column (CreatedAt, DeletedAt)
//     resolves to the right column AND binds a time.Time the engine compares
//     the way the column is stored.

const cliente = "6f0f6f2a-6f9f-4a6a-9f9f-2b7a9c5f21d4"

func seed(t *testing.T) core.RelationalEngine {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer db.Close()

	ddl := `CREATE TABLE "atendimentos" (
	  "id" TEXT NOT NULL, "codigo" TEXT NOT NULL, "setor" TEXT NOT NULL,
	  "duracao" INTEGER NOT NULL, "nota" REAL NULL, "cliente_id" TEXT NOT NULL,
	  "revision" INTEGER NOT NULL DEFAULT 0,
	  "created_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL, "deleted_at" TEXT NULL,
	  CONSTRAINT "atendimentos_pkey" PRIMARY KEY ("id"))`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}

	// suporte: one scored, one NOT scored     → avg over 8.0
	// vendas:  one NOT scored (active)        → avg over NOTHING  ← the case
	//          one scored but ARCHIVED        → out of every activeOnly answer
	rows := []struct {
		id, codigo, setor string
		duracao           int
		nota              any
		created, deleted  any
	}{
		{"11111111-1111-4111-8111-111111111111", "AT-1", "suporte", 30, 8.0, "2026-01-01T00:00:00.000Z", nil},
		{"22222222-2222-4222-8222-222222222222", "AT-2", "suporte", 50, nil, "2026-02-01T00:00:00.000Z", nil},
		{"33333333-3333-4333-8333-333333333333", "AT-3", "vendas", 20, nil, "2026-03-01T00:00:00.000Z", nil},
		{"44444444-4444-4444-8444-444444444444", "AT-4", "vendas", 70, 6.0, "2026-04-01T00:00:00.000Z", "2026-05-01T00:00:00.000Z"},
	}
	for _, r := range rows {
		_, err := db.Exec(
			`INSERT INTO "atendimentos" (id, codigo, setor, duracao, nota, cliente_id, revision, created_at, updated_at, deleted_at)
			 VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
			r.id, r.codigo, r.setor, r.duracao, r.nota, cliente, r.created, r.created, r.deleted)
		if err != nil {
			t.Fatalf("seeding %s: %v", r.codigo, err)
		}
	}

	eng, err := core.NewEngine("sqlite", context.Background(), core.EngineConfig{DSN: path})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return eng
}

func service(t *testing.T) *AtendimentoServiceImpl {
	t.Helper()
	return NewAtendimentoServiceImpl(NewAtendimentoRepository(seed(t)))
}

// Several specs, one query, every one of them filled.
func TestMultiAggregateAnswersEveryNumber(t *testing.T) {
	got := service(t).CargaDoCliente(domain.NewID(cliente))

	if got.Atendimentos != 3 {
		t.Errorf("Atendimentos = %d, want 3 (the archived row is out under activeOnly)", got.Atendimentos)
	}
	if got.MinutosTotais != 100 {
		t.Errorf("MinutosTotais = %d, want 100 (30+50+20)", got.MinutosTotais)
	}
	if !got.NotaMediaFound || got.NotaMedia != 8 {
		t.Errorf("NotaMedia = %v (found %v), want 8 found — one row is scored", got.NotaMedia, got.NotaMediaFound)
	}
	if !got.MelhorNotaFound || got.MelhorNota != 8 {
		t.Errorf("MelhorNota = %v (found %v), want 8 found", got.MelhorNota, got.MelhorNotaFound)
	}
}

// The defect this release fixes, measured against real NULLs: a group that
// exists and has nothing to average.
func TestAGroupWithNothingToAverageSaysSo(t *testing.T) {
	groups := service(t).PorSetorDoCliente(domain.NewID(cliente))
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (suporte, vendas)", len(groups))
	}
	by := map[string]int{}
	for i, g := range groups {
		by[g.Setor] = i
	}

	sup := groups[by["suporte"]]
	if sup.Atendimentos != 2 || sup.MinutosTotais != 80 || sup.MaisCurto != 30 {
		t.Errorf("suporte = %+v, want 2 atendimentos / 80 minutos / 30 mais curto", sup)
	}
	if !sup.NotaMediaFound || sup.NotaMedia != 8 {
		t.Errorf("suporte NotaMedia = %v (found %v), want 8 found", sup.NotaMedia, sup.NotaMediaFound)
	}

	ven := groups[by["vendas"]]
	if ven.Atendimentos != 1 || ven.MinutosTotais != 20 || ven.MaisCurto != 20 {
		t.Errorf("vendas = %+v, want 1 atendimento / 20 minutos / 20 mais curto", ven)
	}
	if ven.NotaMediaFound {
		t.Errorf("vendas NotaMediaFound = true with NotaMedia %v — no row in that group is "+
			"scored, so there is nothing to average and the zero is not an answer", ven.NotaMedia)
	}
}

// The same distinction on the SINGLE form, under the name it has always used.
func TestSingleGroupedAverageCarriesFound(t *testing.T) {
	groups := service(t).MediaPorSetor()
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	for _, g := range groups {
		switch g.Setor {
		case "suporte":
			if !g.ValueFound || g.Value != 8 {
				t.Errorf("suporte = %v (found %v), want 8 found", g.Value, g.ValueFound)
			}
		case "vendas":
			if g.ValueFound {
				t.Errorf("vendas ValueFound = true (value %v) — nothing in that group is scored", g.Value)
			}
		default:
			t.Errorf("unexpected group %q", g.Setor)
		}
	}
}

// A comparison against a column the FRAMEWORK stamps: it has to resolve to the
// right column and bind an instant the engine compares the way it stored it.
func TestStampedColumnsAreComparable(t *testing.T) {
	svc := service(t)

	cut := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	if got := svc.AbertosDesde(cut); got != 1 {
		t.Errorf("AbertosDesde(2026-02-15) = %d, want 1 — only AT-3 is both later and unarchived", got)
	}
	early := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := svc.AbertosDesde(early); got != 3 {
		t.Errorf("AbertosDesde(2025-01-01) = %d, want 3 — every unarchived row is later", got)
	}

	if got := svc.ArquivadosDoCliente(domain.NewID(cliente)); got != 1 {
		t.Errorf("ArquivadosDoCliente = %d, want 1 — DeletedAt IS NOT NULL selects the archived row", got)
	}

	de := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	ate := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	if got := svc.TocadosEntre(de, ate); got != 2 {
		t.Errorf("TocadosEntre = %d, want 2 — AT-2 and AT-3 fall inside the window", got)
	}
}

// excludeSelf on a fact that answers SEVERAL numbers, and an ungrouped min over
// a NOT NULL column — which still carries Found, because the matching set as a
// whole can be empty even when the column never is.
func TestExcludeSelfNarrowsAMultiAnswerFact(t *testing.T) {
	svc := service(t)
	var noSelf domain.ID

	all := svc.OutrosDoCliente(domain.NewID(cliente), noSelf)
	if all.Quantos != 3 {
		t.Errorf("OutrosDoCliente(no self).Quantos = %d, want 3", all.Quantos)
	}
	if !all.MaisCurtoFound || all.MaisCurto != 20 {
		t.Errorf("MaisCurto = %d (found %v), want 20 found", all.MaisCurto, all.MaisCurtoFound)
	}

	// AT-3 is the 20-minute one: excluding it moves the minimum.
	self := domain.NewID("33333333-3333-4333-8333-333333333333")
	rest := svc.OutrosDoCliente(domain.NewID(cliente), self)
	if rest.Quantos != 2 {
		t.Errorf("OutrosDoCliente(excluding AT-3).Quantos = %d, want 2", rest.Quantos)
	}
	if !rest.MaisCurtoFound || rest.MaisCurto != 30 {
		t.Errorf("MaisCurto without AT-3 = %d (found %v), want 30 found",
			rest.MaisCurto, rest.MaisCurtoFound)
	}

	// And the empty set: another client has nothing, so there is no minimum —
	// Found says so instead of the zero standing in for one.
	empty := svc.OutrosDoCliente(domain.NewID("99999999-9999-4999-8999-999999999999"), noSelf)
	if empty.Quantos != 0 {
		t.Errorf("a client with nothing answers Quantos = %d, want 0", empty.Quantos)
	}
	if empty.MaisCurtoFound {
		t.Errorf("MaisCurtoFound = true over an empty set (value %d) — there is no "+
			"minimum of nothing", empty.MaisCurto)
	}
}
