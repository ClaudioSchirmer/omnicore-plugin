# PostgreSQL — dialect knowledge

Authority for every exact type/column shape below: the pinned `table-schema.html`
("Supported column shapes"). This sheet orients and flags the traps; the doc wins on any
disagreement.

## Identity / id columns

A `domain.ID` field (and every cross-aggregate reference) maps to Postgres's native `UUID`
column — the same type for the required and the nullable (`*domain.ID`) case. A plain
`string` field is text, always. Postgres is the least surprising engine on identity: one
codec covers both nullability paths.

## Unique / PK violation — the ConstraintBinding key

Postgres surfaces a unique/PK violation (SQLSTATE 23505) carrying the **constraint NAME**,
and that name IS the key the repo's `Constraints` map binds. So the migration names its
constraints deterministically and the repo binds those names:

- unique constraint → `<table>_<col>_key`
- primary key → `<table>_pkey`

The match is table-agnostic: a role repo over a shared base binds the BASE table's
constraint names too (e.g. the base identity's natural-key `_key`). Name a constraint on
its OWNING table.

## Active-only uniqueness (archived remnants must not block)

When a natural key must be unique only among ACTIVE rows (so an archived remnant does not
block a new active row), Postgres expresses it as a PARTIAL unique index (a `WHERE`
predicate on the archive column). The exact predicate form is in `table-schema.html` —
route there; do not hand-roll it.

## Types & semantics

Native `BOOLEAN` and native `JSON`/`JSONB`; money is stored as an integer of minor units
(never a float); timestamps are application-stamped. No decimal-precision trap.

## Column & table descriptions — they go in the catalogue

`COMMENT ON TABLE <t> IS '…';` and `COMMENT ON COLUMN <t>.<c> IS '…';`, as statements after
the CREATE. Double the apostrophes inside the text. A description left as a `--` line in the
migration is invisible to `\d+`, to `pg_description` and to every client — which is the whole
audience it was written for.

## Read side

Full distributed CQRS is available: entity views project to MongoDB through the CDC
pipeline. (A view may still opt into the relational read-your-writes posture via
`.RelationalSource(...)` — that is a per-view choice, not a Postgres constraint.)
