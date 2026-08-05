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
columns when declared: `deleted_at` nullable, `created_at`/`updated_at` NOT NULL with a DB
default as belt-and-suspenders (the framework stamps actively). If the entity has no
Archive mode, there is no archive column — keep `Modes()` ⟺ the schema declaration ⟺ the migration in
lockstep.

## Self-documenting DDL — table & column descriptions

Every generated table and column carries its description as a SQL COMMENT, sourced from
the spec (§2 `Description` per column; the §1 one-liner per table) — the schema documents
itself. The mechanism is standard DDL, per dialect: **Postgres & Oracle** = `COMMENT ON
TABLE …` / `COMMENT ON COLUMN <table>.<col> IS '…'` statements after the `CREATE TABLE`
(single statements — compatible with the Oracle runner's plain-SQL split); **MySQL** =
inline `COMMENT '…'` on each column + a trailing `COMMENT='…'` table option. For any other
dialect the pinned docs define the mechanism — and when the pin documents NONE (today:
SQL Server), emit no comment DDL rather than inventing one; the descriptions live in the
spec regardless. The `.down` needs nothing extra — dropping the table drops its comments. Applies to EVERY table the
entity emits (base, role, children, siblings). Column types still come only from
`table-schema.html`; a description never changes a type.

## Traps

- **⚠️ Id column types follow the pin's identity contract — pair DDL with the field's Go
  type per the PINNED `table-schema.html` ("Supported column shapes"; detection + full
  rule: SKILL.md boot-traps, "Id typing").** Managed slots (entity id, every FK) are
  always native: Postgres `UUID`, MySQL and SQL Server `BINARY(16)` (never
  `UNIQUEIDENTIFIER` — its GUID sort order destroys the v7 time locality; per the
  pinned `table-schema.html`), Oracle `RAW(16)`. On a TYPED-IDENTITY pin, a
  reference column follows the field type — `domain.ID`/`*domain.ID` ⇒
  `UUID`/`BINARY(16)`/`RAW(16)` (NULL-able for the pointer), `string`/`*string` ⇒
  `CHAR(36)`/`VARCHAR(36)` (`VARCHAR2(36)` on oracle); a
  mismatched pair throws `Error 1366/1406` at the FIRST INSERT — a runtime 500 `go build`
  cannot catch. On older pins (≤ v0.29.0): required reference ⇒ `BINARY(16)`, nullable
  (`*string`) ⇒ `VARCHAR(36)`; Postgres `UUID` for both. (The framework's own
  control-plane tables use `CHAR(36)` via a different write path — do NOT mirror them.)
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
