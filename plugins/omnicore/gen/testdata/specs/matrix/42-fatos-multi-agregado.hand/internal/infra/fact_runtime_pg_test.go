//go:build postgres

package infra

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClaudioSchirmer/omnicore/domain"
	"github.com/ClaudioSchirmer/omnicore/infra/db/core"
	_ "github.com/ClaudioSchirmer/omnicore/infra/db/engine/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// The same assertions as the sqlite runtime test, against PostgreSQL — because
// one of the things this release added is compared on a column type SQLite does
// not have.
//
// SQLite stores a timestamp as TEXT and compares it lexicographically, which is
// the easy case: any ISO-8601 string sorts correctly. PostgreSQL stores
// TIMESTAMPTZ and an identity as a native UUID, so `criteria.Gte("CreatedAt",
// t)` here proves the driver binds a time.Time the server actually compares —
// and the framework's own per-dialect tests assert the RENDERED SQL for ordinary
// fields, never for the three columns it stamps itself.
//
// Skipped unless PROBE_PG_DSN names a reachable database; the DDL comes from the
// generated migration rather than being hand-written, so the columns the query
// names and the columns the table has cannot drift apart.

// Declared here too: the sqlite file that also names it is not built under
// this tag, and a build carrying BOTH tags must not see one name twice.
const clientePG = "6f0f6f2a-6f9f-4a6a-9f9f-2b7a9c5f21d4"

func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("PROBE_PG_DSN")
	if dsn == "" {
		t.Skip("PROBE_PG_DSN not set — the postgres runtime lane needs a reachable server")
	}
	return dsn
}

func applyMigrations(t *testing.T, db *sql.DB, suffix string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "postgres", "*"+suffix))
	if err != nil || len(files) == 0 {
		t.Fatalf("no %s migration found: %v", suffix, err)
	}
	for _, f := range files {
		sqlText, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if _, err := db.Exec(string(sqlText)); err != nil && suffix == ".up.sql" {
			t.Fatalf("applying %s: %v", f, err)
		}
	}
}

func pgService(t *testing.T) *AtendimentoServiceImpl {
	t.Helper()
	dsn := pgDSN(t)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("postgres unreachable at PROBE_PG_DSN: %v", err)
	}

	// Down first so a rerun does not trip over what the last one left.
	applyMigrations(t, db, ".down.sql")
	applyMigrations(t, db, ".up.sql")

	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("bad fixture time %q: %v", s, err)
		}
		return v
	}
	rows := []struct {
		id, codigo, setor string
		duracao           int
		nota              any
		created           time.Time
		deleted           any
	}{
		{"11111111-1111-4111-8111-111111111111", "AT-1", "suporte", 30, 8.0, at("2026-01-01T00:00:00Z"), nil},
		{"22222222-2222-4222-8222-222222222222", "AT-2", "suporte", 50, nil, at("2026-02-01T00:00:00Z"), nil},
		{"33333333-3333-4333-8333-333333333333", "AT-3", "vendas", 20, nil, at("2026-03-01T00:00:00Z"), nil},
		{"44444444-4444-4444-8444-444444444444", "AT-4", "vendas", 70, 6.0, at("2026-04-01T00:00:00Z"), at("2026-05-01T00:00:00Z")},
	}
	for _, r := range rows {
		_, err := db.Exec(
			`INSERT INTO "atendimentos" (id, codigo, setor, duracao, nota, cliente_id, revision, created_at, updated_at, deleted_at)
			 VALUES ($1, $2, $3, $4, $5, $6, 0, $7, $7, $8)`,
			r.id, r.codigo, r.setor, r.duracao, r.nota, clientePG, r.created, r.deleted)
		if err != nil {
			t.Fatalf("seeding %s: %v", r.codigo, err)
		}
	}

	eng, err := core.NewEngine("postgres", context.Background(), core.EngineConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return NewAtendimentoServiceImpl(NewAtendimentoRepository(eng))
}

func TestPGMultiAggregateAnswersEveryNumber(t *testing.T) {
	got := pgService(t).CargaDoCliente(domain.NewID(clientePG))
	if got.Atendimentos != 3 || got.MinutosTotais != 100 {
		t.Errorf("got %d atendimentos / %d minutos, want 3 / 100", got.Atendimentos, got.MinutosTotais)
	}
	if !got.NotaMediaFound || got.NotaMedia != 8 {
		t.Errorf("NotaMedia = %v (found %v), want 8 found", got.NotaMedia, got.NotaMediaFound)
	}
	if !got.MelhorNotaFound || got.MelhorNota != 8 {
		t.Errorf("MelhorNota = %v (found %v), want 8 found", got.MelhorNota, got.MelhorNotaFound)
	}
}

func TestPGAGroupWithNothingToAverageSaysSo(t *testing.T) {
	groups := pgService(t).PorSetorDoCliente(domain.NewID(clientePG))
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	for _, g := range groups {
		switch g.Setor {
		case "suporte":
			if g.Atendimentos != 2 || g.MinutosTotais != 80 || g.MaisCurto != 30 {
				t.Errorf("suporte = %+v", g)
			}
			if !g.NotaMediaFound || g.NotaMedia != 8 {
				t.Errorf("suporte NotaMedia = %v (found %v), want 8 found", g.NotaMedia, g.NotaMediaFound)
			}
		case "vendas":
			if g.Atendimentos != 1 || g.MaisCurto != 20 {
				t.Errorf("vendas = %+v", g)
			}
			if g.NotaMediaFound {
				t.Errorf("vendas NotaMediaFound = true (value %v) — nothing there is scored", g.NotaMedia)
			}
		}
	}
}

// The one this file exists for: a real TIMESTAMPTZ, compared against a
// time.Time the driver binds, on a column no fields[] entry declares.
func TestPGStampedColumnsAreComparable(t *testing.T) {
	svc := pgService(t)

	cut := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	if got := svc.AbertosDesde(cut); got != 1 {
		t.Errorf("AbertosDesde(2026-02-15) = %d, want 1 — only AT-3 is later and unarchived", got)
	}
	early := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := svc.AbertosDesde(early); got != 3 {
		t.Errorf("AbertosDesde(2025-01-01) = %d, want 3", got)
	}
	late := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := svc.AbertosDesde(late); got != 0 {
		t.Errorf("AbertosDesde(2027-01-01) = %d, want 0 — nothing is that late", got)
	}
	if got := svc.ArquivadosDoCliente(domain.NewID(clientePG)); got != 1 {
		t.Errorf("ArquivadosDoCliente = %d, want 1", got)
	}
	de := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	ate := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	if got := svc.TocadosEntre(de, ate); got != 2 {
		t.Errorf("TocadosEntre = %d, want 2", got)
	}
}
