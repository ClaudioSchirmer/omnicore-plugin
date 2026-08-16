# Oracle (23ai+) — dialect knowledge

Authority for every exact type/column shape below: the pinned `table-schema.html`
("Supported column shapes"). This sheet orients and flags the traps; the doc wins on any
disagreement. Oracle floor is Database 23ai — older releases are out of scope.

## Identity / id columns

A `domain.ID` field maps to `RAW(16)` (the compact 16-byte UUID codec). A plain `string`
field is text, always. Identifiers are UNQUOTED (Oracle folds them to uppercase in the
catalog) — the framework accounts for this; do not quote them into a fixed case.

## Unique / PK violation — the ConstraintBinding key

Oracle surfaces a unique/PK violation (ORA-00001) naming the violated constraint as
`SCHEMA.NAME`; the framework extracts the bare NAME (lowercased) and that is the key the
repo's `Constraints` map binds. Name them deterministically:

- unique constraint → `<table>_<col>_key`
- primary key → `<table>_pkey` (named explicitly)

The match is table-agnostic: a role repo over a shared base binds the BASE table's names
too.

## Active-only uniqueness (archived remnants must not block)

Oracle has no partial index; "unique among active rows only" uses the function-based /
expression-index workaround the framework prescribes in `table-schema.html` — route there.

## Types & semantics

23ai has native `BOOLEAN` and native `JSON`; money is an integer of minor units; decimal
numerics are `NUMBER`. **CDC caveat baked into the column design:** the CDC-tailed payload
columns are CLOB by framework decision (LogMiner cannot decode native-JSON redo) — this is
a `table-schema.html` fact, stated here only so it is not "corrected" away.

## Column & table descriptions — they go in the catalogue

`COMMENT ON TABLE "<t>" IS '…';` and `COMMENT ON COLUMN "<t>"."<c>" IS '…';`, as statements
after the CREATE — `user_tab_comments` / `user_col_comments` are what a client reads. Double
the apostrophes inside the text. Plain statements, never a PL/SQL block: the migration runner
splits on top-level semicolons and does not support `BEGIN … END`.

## Read side

Full distributed CQRS is available: entity views project to MongoDB through the CDC
pipeline (LogMiner → relay → broker → SyncEngine). Expect a small mining-cadence floor on
write→document latency — a bench characteristic, not a modeling concern.
