# SQLite — dialect knowledge (the zero-infra MVP engine)

Authority for every exact type/column shape below: the pinned `table-schema.html`
("Supported column shapes", SQLite table). This sheet orients and flags the traps; the doc
wins on any disagreement. SQLite is the pure-Go, single-node, MVP / self-executable backend
— it is NOT like the four SQL engines, and this file exists because treating it as if it
were is exactly how services break.

## Unique / PK violation — the ConstraintBinding key (THE trap)

**SQLite reports NO constraint or index name in a violation. It reports the violated
COLUMN LIST — `table.column`, with a DOT.** So the key the repo's `Constraints` map binds
on SQLite is `<table>.<column>`, never the `<table>_<col>_key` index name the SQL engines
use:

- single-column unique / PK → `<table>.<column>` (e.g. the natural key `people.email`, the
  id `people.id`)
- composite unique → the columns joined `, ` in SCHEMA-DECLARED order (e.g. `t.a, t.b`)

Why this bites: you CAN name a SQLite index `<table>_<col>_key` in the migration, and it
looks right — but SQLite still never emits that name on a violation, so a repo that binds
the index name silently misses. The lookup fails, the raw DB error escapes unmapped, and
the intended custom 409 becomes a generic 500. It compiles and boots; only the duplicate
INSERT reveals it. When a service's dialect is SQLite, EVERY `ConstraintBinding` key must
be the dotted `table.column` form — a key ending in `_key`, `_pkey`, or the literal
`PRIMARY` is the SQL-engine reflex leaking in and is always wrong here.

(A foreign-key violation carries no name or column list at all — the framework classifies
it as a boolean only; nothing to bind.)

## Identity / id columns

A `domain.ID` field maps to `TEXT`. A plain `string` field is `TEXT`. There is no compact
binary UUID codec here — SQLite stores the id as text.

## Types & semantics

- **Decimal-as-`string` → `TEXT`, never `NUMERIC`.** SQLite's `NUMERIC` affinity coerces a
  decimal to a float64 and silently loses precision; money/decimals ride as `TEXT`.
- Case folding is **ASCII-only** — non-ASCII case-insensitive matching does not fold. An
  MVP-not-production trait; state it to the dev honestly.
- The engine factory FORCES the correctness pragmas (foreign keys on, case-sensitive like)
  — the service does not set them.

## Active-only uniqueness (archived remnants must not block)

SQLite supports Postgres-style partial indexes (since 3.8.0; the framework's own embedded
SQLite migrations use them): `CREATE UNIQUE INDEX ... ON <role>(<fk>) WHERE deleted_at IS
NULL` — the same statement as Postgres. `table-schema.html` owns the per-dialect shapes.

## Column & table descriptions — the ONE engine that cannot store them

SQLite has no `COMMENT ON` and no catalogue slot for a description, so here — and only here —
the description stays in the migration file as a `--` line above the column. Every other
engine stores it in the database; do not carry this exception over to them.

## Read side — no Mongo, so no CDC-projected views

SQLite has NO CDC source (Debezium cannot tail a SQLite file), so the read-side posture
is FORCED relational — all entity views served straight from the SoR. What that serves
vs rejects, what it never constrains (write-side modeling), and the upgrade path are
owned by `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` — route there, do not restate.
