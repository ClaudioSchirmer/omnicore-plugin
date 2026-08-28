# conventions/migrations.md — relational migrations (flat / base case)

> NO code here, by design. Naming/granularity: `service-layout.html`. Numbering, `.down`
> mandate, autoRun, and the per-dialect Go↔column type tables: the routed `/docs` —
> reading them before generating this layer is MANDATORY. This file carries only
> skill-level process, decisions and traps.

Shape requirements for the partition tools are deltas: children → `aggregate-children.md`
· sibling → `siblings.md` · SharedBase → `sharedbase.md`.

Docs for this layer: `migrations.html` (numbering, tracking, `.down`, autoRun) ·
`yaml-reference.html` (dialect selection) · `table-schema.html` (the "Go ↔ <dialect>"
type tables — the sole authority; never type columns from memory).

## Files & granularity

Per `service-layout.html`: the service's own sequence starts at `0001` (independent of the
framework's — separate tracking tables, no collision; never write the framework SQL);
**ONE migration pair per scaffolded entity** — every table (base, role, children,
siblings) in a single `000N_<entity>.up.sql` in FK-dependency order, `.down.sql` dropping
in reverse; child/sibling tables owner-prefixed; a `{version}_{name}` filename that
doesn't parse is silently ignored, then rejected at boot; **every `.up` needs its
`.down`** (may be a no-op) or boot aborts.

## Dialect discovery — generate for the TARGET set

Targets = `relational.dialect` across every `microservice.*.yaml` (resolving
`${VAR:default}`) UNION migration folders that actually CONTAIN `.sql` files. **An empty
`migrations/<dialect>/` folder is NOT a target** — the yaml is the authority, not folder
presence. Emit the pair in every target dialect's folder and bind the repo `Constraints`
per dialect (infra.md).

## Column-type decisions (dialect-neutral — the per-dialect mapping stays in the docs)

Nullable Go pointer → nullable column; money = `int64` minor units, never float; exact
decimals → `string` (float64 rounds); binary floats fine for non-money numerics. Managed
columns when declared: `deleted_at` nullable **and sub-second, in the same type on a root
and its children** (Traps), `created_at`/`updated_at` NOT NULL with a DB default as
belt-and-suspenders (the framework stamps actively). If the entity has no Archive mode,
there is no archive column — keep `Modes()` ⟺ the schema declaration ⟺ the migration in
lockstep.

**A COMPOSITE value object is N columns, and its nullability is TWO questions.** Each part
gets its own column, typed by the part's own Go type. Whether that column may be NULL is
decided by the part's shape AND by the value object's: a part declared as a pointer inside
the VO is nullable, and **an OPTIONAL composite (the entity holds it as `*Money`) makes
EVERY one of its part columns nullable regardless** — "absent" is written as all-NULL and
read back from all-NULL, so a single NOT NULL among them makes absence impossible to store.
Under a mandatory composite each part follows its own type, exactly like a scalar VO field.
The column COMMENT comes from the PART's description, declared on the value object.

## Self-documenting DDL — the description goes INTO the database

Every generated table and column carries its description, sourced from the spec (§2
`Description` per column; the §1 one-liner per table) — and it is **stored in the database
itself, not left as a `-- ` line in the file.** The point of writing a description is that
someone holding a CONNECTION and not this repository can read it: the DBA on the
catalogue, the BI tool listing columns, the next developer opening the table in a client.
A `--` line reaches none of them.

So, per dialect — and this is the framework's own control-plane DDL, mirrored:

| Dialect | Where the description goes |
|---|---|
| postgres · oracle | `COMMENT ON TABLE <t> IS '…';` / `COMMENT ON COLUMN <t>.<c> IS '…';` after the CREATE |
| mysql | inline on the column (`… NOT NULL COMMENT '…'`) + `ALTER TABLE <t> COMMENT = '…';` |
| sqlserver | `EXEC sp_addextendedproperty @name = N'MS_Description', …` at TABLE and COLUMN level (what SSMS and `sys.extended_properties` read) |
| **sqlite** | **`--` line comments — the ONLY dialect that has nowhere to store one** |

Escape the apostrophes (`''`) — an ordinary "the person's document" otherwise ends the
literal and turns the rest of the sentence into syntax. On sqlserver, take the schema from
`SCHEMA_NAME()` through a `DECLARE @schema sysname` at the top of the file rather than
hardcoding `dbo`, or a service whose tables live in its own schema fails every one of
them. Applies to EVERY table the entity emits (base, role, children, siblings), and to the
managed columns too (`revision`, the archive stamp, the timestamps, the FK) — a catalogue
that documents half a table is a catalogue nobody trusts. Column types still come only
from `table-schema.html`; a description never changes a type.

**A reworded description is a MIGRATION, not an edit.** The comment lives in the database,
so changing the text in the spec or the schema changes nothing until a new numbered pair
carries the `COMMENT ON` / `sp_addextendedproperty` statement — the same rule as any other
shape change, and `evolve-entity`'s impact map is where it belongs.

## Traps

- **⚠️ Id column types follow the pin's identity contract — pair DDL with the field's Go
  type per the PINNED `table-schema.html` ("Supported column shapes"; detection + full
  rule: SKILL.md boot-traps, "Id typing").** Managed slots (entity id, every FK) are
  always native: Postgres `UUID`, MySQL and SQL Server `BINARY(16)` (never
  `UNIQUEIDENTIFIER` — its GUID sort order destroys the v7 time locality; per the
  pinned `table-schema.html`), Oracle `RAW(16)`, SQLite `TEXT` (ids are text there —
  the dialect sheet owns it; this five-engine list is NOT complete-by-copy, the
  target's `shared/dialects/<dialect>.md` always wins). On a TYPED-IDENTITY pin, a
  reference column follows the field type — `domain.ID`/`*domain.ID` ⇒
  `UUID`/`BINARY(16)`/`RAW(16)`/`TEXT` (NULL-able for the pointer), `string`/`*string` ⇒
  `CHAR(36)`/`VARCHAR(36)` (`VARCHAR2(36)` on oracle); a
  mismatched pair throws `Error 1366/1406` at the FIRST INSERT — a runtime 500 `go build`
  cannot catch. On older pins (≤ v0.29.0): required reference ⇒ `BINARY(16)`, nullable
  (`*string`) ⇒ `VARCHAR(36)`; Postgres `UUID` for both. (The framework's own
  control-plane tables use `CHAR(36)` via a different write path — do NOT mirror them.)
- **⚠️ The archive stamp column (`deleted_at`) needs SUB-SECOND precision, and a root and
  its children must declare the SAME type — take it from the pinned `table-schema.html`,
  never from the engine's friendliest default.** The stamp is not only a flag: it
  identifies the archive OPERATION. One archive binds a single instant on the root row and
  on every child row its cascade reaches, and the unarchive restores exactly the children
  carrying the ROOT's instant — so a child archived on its own months earlier stays down.
  That discriminator is an equality between two stored timestamps, so it is only as sharp
  as the column. A second-resolution column (a bare MySQL `DATETIME` — the default that
  looks right) folds two operations of the same second into one stamp and revives a child
  the root never touched; a child column COARSER than the root's truncates the instant it
  was given and matches nothing, so the restore reaches nothing. Neither raises an error:
  the rows are simply wrong, and only a rebuild shows it.
- **Unique constraints are named `<table>_<col>_key` on the four SQL engines** so the repo
  can bind that name; name them on the OWNING table (a base field's constraint lives on the
  base table — e.g. `persons_email_key`, not `students_email_key`). The PK name and, on
  SQLite, the WHOLE bind-key form diverge — **`${CLAUDE_PLUGIN_ROOT}/shared/dialects/<dialect>.md`
  is the source of truth for what the repo binds** (SQLite reports the `<table>.<column>`
  column list on a violation, not any index name, so naming the index is fine but does NOT
  drive the bind — the repo key is dotted there).
- **Every entity/base table carries the revision column** (`revision BIGINT NOT NULL
  DEFAULT 0` — exact shape per the pin's `table-schema.html`); child/sibling tables do
  NOT (the schema forbids `Revision` there — `conventions/infra.md`, managed-slot
  contract). **And no physical column may start with `_`** — the underscore namespace
  is reserved; declaring one is a boot failure (`lifecycle-map.html`).
- Children get an FK + covering index; siblings share the owner's PK with
  `ON DELETE CASCADE` and NO lifecycle columns — see the deltas.
- **A CROSS-AGGREGATE reference column (`course_id`, `buyer_id` — a `domain.ID` field
  pointing at ANOTHER aggregate's table) is a BARE column, no DB foreign key, by
  default.** DB-level FKs live INSIDE the aggregate boundary (child → root, sibling →
  owner, role → base — the shapes above); across aggregates the referenced row
  archives rather than deletes, so referential existence is a DOMAIN rule (the
  ctx-bound Service probe — SKILL.md's "rules needing existence" row), never an
  `ON DELETE` action bridging two consistency boundaries. The shape is a plain nullable
  column, no DB-level FK constraint. Add a plain
  INDEX on the column when reads filter/join by it; a real FK (`ON DELETE RESTRICT`
  only, never CASCADE) is a dev-signed exception, not the default.
