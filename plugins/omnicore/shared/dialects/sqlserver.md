# SQL Server — dialect knowledge

Authority for every exact type/column shape below: the pinned `table-schema.html`
("Supported column shapes"). This sheet orients and flags the traps; the doc wins on any
disagreement.

## Identity / id columns

A `domain.ID` field maps to `BINARY(16)` (the compact 16-byte UUID codec) — **never
`UNIQUEIDENTIFIER`** (its byte-order differs from the canonical UUID layout and breaks the
codec). This is the single most common SQL Server drift. A plain `string` field is text,
always.

## Unique / PK violation — the ConstraintBinding key

SQL Server surfaces a unique/PK violation naming the object, and that name is the key the
repo's `Constraints` map binds — two error numbers, two markers:

- 2627 (constraint) → the CONSTRAINT name
- 2601 (unique index) → the unique INDEX name

Name them deterministically in the migration so the repo can bind them:

- unique constraint → `<table>_<col>_key`
- primary key → `<table>_pkey` (named explicitly on the `CONSTRAINT` — SQL Server does not
  auto-name it usefully)

The match is table-agnostic: a role repo over a shared base binds the BASE table's names
too.

## Active-only uniqueness (archived remnants must not block)

SQL Server expresses "unique among active rows only" as a FILTERED unique index (a `WHERE`
predicate on the archive column). The exact predicate is in `table-schema.html` — route
there.

## Types & semantics

Boolean is `BIT`; money is an integer of minor units. Per-relay note: CDC depends on the
SQL Server Agent plus per-database/per-table CDC enablement done AFTER the first app boot
creates the outbox — that lives in `transport.html` / the bench templates, not here.

## Read side

Full distributed CQRS is available: entity views project to MongoDB through the CDC
pipeline.
