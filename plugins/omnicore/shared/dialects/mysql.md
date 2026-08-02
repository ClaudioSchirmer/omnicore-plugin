# MySQL — dialect knowledge

Authority for every exact type/column shape below: the pinned `table-schema.html`
("Supported column shapes"). This sheet orients and flags the traps; the doc wins on any
disagreement.

## Identity / id columns

A `domain.ID` field maps to `BINARY(16)` (the compact 16-byte UUID codec) — NOT a text
column. On a typed-identity pin this covers both the required and the nullable id case.
On older pins the nullable reference (`*string`) had to be `VARCHAR(36)` because the
pointer bypassed the codec — and a relational-side filter on such a column would not match
(you filtered on the Mongo view instead). Confirm which rule the pinned `table-schema.html`
states; pairing the Go field type with the wrong column is a FIRST-INSERT failure (`Error
1366/1406`) that the build never catches. A plain `string` field is text, always.

## Unique / PK violation — the ConstraintBinding key

MySQL surfaces a unique/PK violation (error 1062) naming the violated INDEX, and that index
name is the key the repo's `Constraints` map binds (the framework strips the qualifying
`<table>.` prefix MySQL 8 prints, leaving the bare name). So:

- unique constraint → `<table>_<col>_key`
- primary key → `PRIMARY` (MySQL's fixed name for the clustered PK — bind exactly that)

The match is table-agnostic: a role repo over a shared base binds the BASE table's index
names too.

## Active-only uniqueness (archived remnants must not block)

MySQL has NO partial/filtered index. Uniqueness restricted to active rows is expressed
through the workaround the framework prescribes in `table-schema.html` (e.g. a
nullable-generated-column technique) — route there for the exact shape; do not invent one.

## Types & semantics

No native boolean (stored as a small integer) and JSON is the `JSON` type; money is an
integer of minor units. Per-relay note: a MySQL CDC relay needs a unique `server.id` and
tolerant DDL handling — that is `transport.html` / the bench templates, not this sheet.

## Read side

Full distributed CQRS is available: entity views project to MongoDB through the CDC
pipeline (binlog → relay → broker → SyncEngine).
