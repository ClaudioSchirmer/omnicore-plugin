# shared/dialects — per-engine knowledge, one file per relational dialect

The single home for **the axes where the relational dialects diverge**. Every omnicore
skill that generates or alters dialect-specific artifacts (migration DDL, `TableSchema`
type choices, `ConstraintBinding` keys, active-only uniqueness, engine swaps) routes
HERE instead of restating dialect facts inline — so the knowledge has ONE owner and the
generating agent reads ONLY the dialect(s) it needs.

> Engine-INDEPENDENT read-side knowledge (posture, what a relational view serves, the
> write-side invariant, elicitation) lives in the sibling `../read-side.md` — same
> one-owner contract, different axis.

## The contract

- **Read only what applies.** A service runs ONE dialect per build (occasionally a
  multi-engine build — then read each target's file). Never read the other engines' files
  — that is the noise this split exists to remove.
- **No code here — by design.** These files carry generic KNOWLEDGE (the divergence and
  why it bites), never SQL or Go snippets. The example a maintainer reasons from is the
  constraint-violation key (below), not a copyable statement.
- **The pinned docs are the authority.** The version-pinned `table-schema.html` (types,
  column shapes) and `transport.html` (CDC/relay) in the module cache are the source of
  truth for exact forms; these sheets ORIENT and flag the traps, and route there for the
  authoritative table. If a sheet ever disagrees with the pinned doc, **the doc wins** —
  the sheet has drifted. Never stamp a framework version into a sheet.

## The engines

| File | Engine | The one thing not to miss |
|---|---|---|
| `postgres.md`  | PostgreSQL            | binds by constraint NAME (23505) |
| `mysql.md`     | MySQL                | binds by index NAME (1062); id codec is `BINARY(16)` |
| `sqlserver.md` | SQL Server           | binds by constraint/index NAME (2627/2601); never `UNIQUEIDENTIFIER` |
| `oracle.md`    | Oracle 23ai+         | binds by constraint NAME (ORA-00001); CDC payload is CLOB |
| `sqlite.md`    | SQLite (zero-infra)  | **binds by COLUMN LIST `table.column`, NOT the index name** — the exception |

The cross-engine crux they all share: a unique/PK violation is mapped to a custom
notification through the repo's `ConstraintBinding` map, keyed by whatever the running
engine surfaces. **Four engines surface a NAME; SQLite surfaces the column list.** Bind
every target dialect's key form — the framework matches whichever the running engine
returns.
