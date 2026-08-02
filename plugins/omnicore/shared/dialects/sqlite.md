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

SQLite has no partial index in the Postgres sense; the "unique among active rows only"
mechanism is the one `table-schema.html` prescribes for SQLite — route there for the exact
shape.

## Read side — no Mongo, so no CDC-projected views

SQLite has NO CDC source (Debezium cannot tail a SQLite file), so there is NO MongoDB
projection. **All entity views are served RELATIONAL, straight from the SoR**
(read-your-writes, no CDC lag) — root-only reads: no embeds/links/composed/shared views, no
free-text search, no child/sibling filter+sort (those return 400
`RelationalCapabilityNotification`, not 500). Any "filter on the Mongo view" fallback that
applies to the SQL engines does NOT apply here. Gaining Mongo (and integration events, and
composed/shared views) later is an engine swap via `/omnicore:configure` — fully reversible,
no application code lost.
