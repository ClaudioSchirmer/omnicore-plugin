# conventions/infra.md — the infra layer (flat / base case)

> NO code here, by design. Layout/naming/granularity: `service-layout.html`. API mechanics
> (schema DSL, boot checks, view options): the routed `/docs` — reading them before
> generating this layer is MANDATORY. This file carries only skill-level process, decisions
> and traps.

Deltas: `.Child` → `aggregate-children.md` · `.Sibling` → `siblings.md` · `.SharedBase` →
`sharedbase.md`.

Docs for this layer: schema + managed columns + boot checks + the per-dialect Go↔column
tables → `table-schema.html` · view options/indexes/`Version` → `auto-query-handlers.html`
+ `mongo-schema-evolution.html` · Service impl channels → `service-to-service.html`.

## Files

Per `service-layout.html`: `schemas/` one schema function per file (base, each role, each
child, each sibling — no bundling); `views/` one view per file; repositories at the infra
root; the domain-service implementation in its own file here.

## Schema — decisions + traps

- `Field("GoName", "column")` — **left = Go, right = DB column**; inverting is a silent
  bug. The schema is type-anchored: a `Field` naming a missing/unexported Go field panics
  at boot; an undeclared exported field is simply never persisted.
- Managed columns are declared **by presence** and actively stamped by the framework —
  never rely on a DB `DEFAULT` for correctness (the DDL default is belt-and-suspenders).
  All three are OPTIONAL: undeclared ⇒ never mentioned in any SQL.
- Boot checks the docs route you past: field-exists, column bijection, PK single-column,
  **`Modes()` ⟺ archive-column** agreement.

## Repository — decisions

- The engine parameter is the neutral relational engine (`Deps.DB`) — the dialect is a
  YAML change, never an edit here.
- **The unique-field chain (5 points — miss one and the violation is an ugly 500 or a
  lonely 409):**
  0. **(recommended primary) a domain Service pre-check in `BuildRules`** — exclude-self
     on update, unarchive included when reactivation can collide — so the duplicate
     reports together with every other validation error; the points below then guard only
     the check-to-commit race window (defense in depth, never presented as the primary).
     **Implement it with the loader's hydration-free `Exists` probe** (the PK is
     criteria-addressable as the fixed field `"ID"` for exclude-self) — never a
     FindAll-and-filter workaround, which pays full aggregate hydration to answer yes/no.
     Confirm the pinned version's loader surface in `custom-command-handler.html`
     (Loading by criteria); a version without the probes falls back to a
     FindOne/FindAll-based check. For scalar facts beyond existence (counts, totals,
     extremes), the same hydration-free family has an aggregate DSL
     (Count/Sum/Avg/Min/Max in one SELECT) — never FindAll-and-count; exact specs at
     the pin;
  1. migration `UNIQUE` constraint (active-only variants per `table-schema.html` when
     archived remnants must not block);
  2. repo `Constraints` binding (violation KEY → notification + field) — the match is
     table-agnostic, so a ROLE repo binds BASE-table constraints too (e.g.
     `persons_email_key`). **The KEY form is PER DIALECT — the four SQL engines bind the
     constraint/index NAME (`<table>_<col>_key`; PK `<table>_pkey`, except mysql `PRIMARY`),
     SQLite binds the `<table>.<column>` column list (dotted, NOT any index name). Get the
     exact form from `${CLAUDE_PLUGIN_ROOT}/shared/dialects/<dialect>.md` and bind EVERY
     target dialect's;**
  3. a custom `<Field>AlreadyExistsNotification` (409, all 7 catalogs);
  4. an immutability rule in the domain when it's the natural key.
  A flat entity with no unique business column needs no `Constraints` map at all.

## View — decisions + traps

- **`Version(N)` bump rule:** bump on any rebuild-relevant change (root, embeds,
  DeleteOnArchive, jsonSchema, collation, capped, time-series); index-only changes do NOT
  bump; forgetting when the hash changed = boot abort (`mongo-schema-evolution.html`).
- Index what the spec's filter/sort list names; `TextIndex` for `?search`.
- Options (`DeleteOnArchive`, `MaxLimit`) — ask; default neither.
- **The managed-slot contract (Revision + ParentID) — cross-layer agreement, like
  `Modes()` ⟺ archive column.** `Revision(col)` is MANDATORY on entity schemas and
  shared bases (omit → boot failure) and FORBIDDEN on children/siblings (declare →
  construction panic). `ParentID` carries three boot-panics: `Field("ID",…)` /
  `Field("ParentID",…)` are reserved names; the ParentID column doubling as a mapped
  `Field` is the silent-overwrite the docs guard against; `ParentID(...)` +
  `SharedBase(...)` together is invalid. And its capability half: ParentID
  auto-projects as the read-only twin of `ID` — filterable/sortable/`?fields=`-able
  with NO hand mapping; never map the FK column yourself. `table-schema.html`.
- **View backing (relational vs Mongo).** Per the project read-side posture / the spec's
  §9 slot. Relational = `.RelationalSource(repo.Loader)` on the plain per-entity view —
  the aggregate's OWN loader (shared with the repo; boot guard `BoundTable()==schema.Table()`),
  never a second one; no collection. Mongo = the plain `View(name).Schema(...)`.
  Only the plain per-entity view is eligible — never a view KIND. What it serves:
  `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`; version-exact: `relational-view.html`.
- **Never `Embed` internal data** — children/siblings auto-project from the schema;
  embedding local data is a fatal boot error. Embeds are for EXTERNAL/upstream data only.

## Service implementation

The domain port's implementation lives here (own file). Channel by
`service-to-service.html`'s matrix: a fact THIS service owns → a direct repo/engine query;
external world → httpclient; another microservice → grpcclient. Injected at the wiring
(see application.md — enforced pairing). For SCALAR facts this service owns — existence,
cardinality, totals, averages, extremes, and their PER-GROUP breakdowns (counts/totals
per key, distinct-key cardinality) — use the loader's hydration-free surface
(`Exists` plus the aggregate facts, whatever shape the pinned version's
`custom-command-handler.html`, "Loading by criteria", ships — newer versions compute
several facts in ONE query, grouped or ungrouped); loading aggregates to answer a scalar
question is the anti-pattern that surface exists to kill.

- **On an infra error the impl FAILS LOUD — it never swallows, and never pushes the error up
  to the domain.** The domain port returns pure values (no `error`), so an unrecoverable query
  failure surfaces HERE as a `panic` — the pipeline's single recover point turns it into a 500
  and the write never happens. Do NOT fold the failure into a plausible-but-wrong answer
  (`if err != nil { return false }`, an empty slice): that silently skips the very invariant
  the rule exists to enforce — a duplicate or a 4th category slips past to the DB backstop (or
  worse, past it). "Cannot answer" is a FAULT, not a "not taken". The panic lives in INFRA,
  where IO happens — NEVER in a domain file, and the port never carries the error back to the
  domain to handle. (The unique index / other backstop is defense in depth, never a licence to
  guess an answer.)
- **Bind the request context — the probe is a read, run it under the request scope.** The
  domain `Service` port is pure and `BuildRules` carries no `context`, so a naive impl runs
  its query on `context.Background()` — outside the request deadline
  (`http.requestTimeoutSeconds`), cancellation, and trace. Don't. Implement the framework's
  `persistence.ScopedServiceProvider` on THIS impl — the service-side counterpart of
  `ScopedReaderProvider`, but mind the asymmetry: the reader mirror comes free from the
  framework's base aggregate repo, whereas a `domain.Service` is your own code, so no framework
  base can provide it — you generate the method here (documented in `auto-handlers.html`,
  "Binding the request context to a Service probe"). The
  impl carries a nil-able `ctx *configuration.AppContext` field and a `ScopedService(ctx)
  domain.Service` method returning a **shallow copy** that closes over the ctx (never mutate
  the receiver — the wired impl is a singleton shared across requests); the probe queries
  under `s.ctx` (an `*AppContext` IS a `context.Context`), falling back to
  `context.Background()` only when `s.ctx == nil` (singleton use — tests, background jobs).
  The Auto handlers bind it for you via `persistence.ScopeService`; in a custom command
  handler wrap the service the same way at the `domain.Get*` call — one line, keeping the
  manual path feature-equivalent. This is the default shape for any Service that queries;
  generate it, don't emit a ctx-less probe.
