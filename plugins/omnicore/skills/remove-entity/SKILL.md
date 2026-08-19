---
name: remove-entity
description: >-
  omnicore: surgically remove an entity from an omnicore-based service — every layer (domain,
  application, web, infra, views, translations, migrations, bootstrap wiring) via a
  visible removal plan the dev approves BEFORE anything is deleted. Use when the user
  wants to delete/remove/retire an entity, aggregate, or CRUD resource. Only for
  projects that import github.com/ClaudioSchirmer/omnicore.
---

# remove-entity

Take an entity out of a service without leaving corpses (dead wiring, orphan
translations, stale views) and without collateral damage (a shared base another role
still uses, a composed view of ANOTHER entity that embeds this one). Inventory first,
approve, then delete — never the reverse.

**Every document this run writes lands under `specs/`, and the project keeps it —
never add it to `.gitignore`** (`${CLAUDE_PLUGIN_ROOT}/shared/generated-documents.md`).

## Core principles

- **Nothing is deleted before the plan is approved.** The full footprint is inventoried
  into `specs/remove-entity/<entity>/plan.md`, the dev approves it, and Phase 2 deletes
  exactly that list — nothing more, nothing less.
- **Docs-first, version-agnostic.** What safely unwires (feature mounting, view
  registration, migration ordering) is defined by the version-pinned `/docs` in the
  module cache (routing table below), never by memory. **This skill carries no code, by
  design.**
- **Data is the dev's, and data loss is a named decision.** Dropping tables and view
  collections destroys data. The migration strategy is a HIGH-RISK slot: drop now vs
  keep the tables (retire only the code). The down twin of a drop recreates STRUCTURE,
  not data — the plan says so honestly.
- **Shared surfaces are the trap.** A SharedBase base under other roles, a
  SharedBaseView listing this role, a ComposedView leg in another entity, integration
  events other services consume — each dependent found becomes `⚠️ OPEN` in the plan and
  BLOCKS approval until the dev decides. Never assume a dependent is safe to cut.
- **Framework maintainer rules never bind this skill** (module-cache `CLAUDE.md`,
  "English everywhere", etc. govern the framework's own repo, not this project).
  Converse — and write every human-facing output (the removal plan, reports) — in the
  user's language, detected from the user's own words (invocation args count, even one
  word) BEFORE the first reply; these docs being English never sets it. Switch the
  moment the user's language becomes clear, even mid-run.

## Plugin self-check (once, non-blocking)

Once per run, during preflight: compare THIS plugin's installed version — the
`version` field of `${CLAUDE_PLUGIN_ROOT}/.claude-plugin/plugin.json` — with the
published one — the same field at
`https://raw.githubusercontent.com/ClaudioSchirmer/omnicore-plugin/main/plugins/omnicore/.claude-plugin/plugin.json`.
Offline, or either side unreadable → skip silently. Newer published → ONE
non-blocking line riding along with the next reply — "omnicore plugin vX → vY
available — update with `claude plugin update omnicore@omnicore` (marketplace
stale? `/plugin marketplace update omnicore` first); it takes effect next
session." Never a gate: this run continues on the installed skills.

## Phase 0 — Preflight + footprint (read, don't ask)

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves — else
  STOP. **Entity exists?** Else there is nothing to remove — say so.
- **No version check here, deliberately** (unlike the scaffold/evolve siblings' Phase
  0v): a removal must follow the CURRENT pin's docs — upgrading first would only enlarge
  the delta. If the dev wants a newer omnicore, that's `/omnicore:upgrade`, separately.
- **Map the FULL footprint** by sweeping the repo for the entity (type names, table
  names, view names, translation keys, route paths): domain type + rules + service ·
  TableSchema + children/sibling tables · commands/queries + DTOs + mappers · routes on
  every surface (REST, GraphQL, gRPC) · views it owns AND views of others it appears in
  (SharedBaseView roles, ComposedView legs, embeds) · translation keys in the seven
  catalogs · migrations that created its tables · feature/bootstrap wiring · tests ·
  topics/subjects and integration events it publishes · **the `microservice.*.yaml`
  blocks that reference it** (`integration:` publishes AND subscribes,
  `upstreamSubscriptions` entries) — event/topic names rarely contain the entity's Go
  name, so READ those blocks, don't just grep the names: a subscribe left behind with
  its receiver removed is a boot ABORT, and those blocks are strict-decoded
  (`${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md`) · **`auth.publicRoutes` entries
  naming this entity's routes** — the list is validated exact-match against the
  registered route set at boot, so an entry pointing at a removed route PANICS naming
  it (`shared/boot-contract.md`) · **is this the LAST feature?** — removing the only
  remaining feature leaves a featureless wiring; outside dev it is REJECTED at boot
  UNLESS the wiring keeps a `BeforeServe` hook (the reject fires only on zero features
  AND no `BeforeServe` — `bootstrap` at the pin). Flag it in the plan with the three
  honest options: accept a dev-only shell, keep/add a `BeforeServe`, or wait for the
  replacement feature.

## Phase 1 — The gate: `specs/remove-entity/<entity>/plan.md`

`Status: DRAFT`, hard STOP until the dev approves; `⚠️ OPEN` slots must be answered,
never defaulted. Sections (structural — `N/A — <why>`, never deleted):

1. **Inventory** — every file to delete and every file to EDIT (unwiring), per layer —
   **including the `microservice.*.yaml` edits** (integration publishes/subscribes,
   upstreamSubscriptions) from the Phase 0 sweep.
   This list is the contract; Phase 2 touches nothing outside it.
2. **Dependents** [blocking]: every shared surface found in Phase 0 — shared base with
   other roles (the base STAYS; only this role's slice goes), composed/embedding views
   of other entities (they need their own edit + `Version` decision, listed here),
   integration events consumed elsewhere (consumers break — the dev decides the
   sequencing), **and every value object in `internal/domain/vos/` another entity still
   has as a field type** — a VO is declared by one entity and REUSED by others, which
   carry no copy of it, so a file that looks orphaned by every measure can be the one type
   the rest of the project does not compile without. Grep the type name across `internal/`
   before listing any `vos/` file for deletion; a composite VO is the easiest to get wrong,
   because the entity that reuses it names the type once and its parts appear as ordinary
   columns everywhere else. Each one `⚠️ OPEN` until decided. **Two shared-base cases need their
   own verdict, never improvised:** removing the BASE itself while any role lives is
   forbidden by design — but the PHYSICAL guard exists only if the dev's migrations
   declared the role→base FKs `ON DELETE RESTRICT` (the framework asks for it, never
   creates it — `table-schema` at the pin; CHECK the migrations and say which world
   this project is in) — remove/retire every role first, then the base; removing the
   LAST role leaves an orphan base whose fate is the schema's declared `OrphanPolicy`
   (KeepOrphan vs DeleteWhenUnreferenced — read it, state it) AND a SharedBaseView
   with zero roles — which CANNOT boot (a role-less SharedBaseView is a boot reject),
   so the view is RETIRED in the same plan; there is no bump-it-empty option.
3. **Data strategy** [high-risk]: drop the tables (new migration, down recreates
   structure only — data is GONE) vs retire code and keep the data. **The Mongo view
   collections are NOT the same decision:** once the view definition is deleted, a
   kept collection is a FOREIGN collection to the DB-per-service boot guard — dev
   profile logs a warning, **every other profile ABORTS boot naming it**. So "keep the
   data" for a collection honestly means: export/dump it, or move it out of the
   service's view database — never "leave it in place" (that option ships a service
   that boots in dev and is unbootable in prd). Also name the registry hygiene: the
   view's row in the framework's view-registry table (and its projection-state
   entries) lingers after code removal; a later re-add of the same view name meets the
   stale row as forgot-to-bump drift (boot abort) — the pin's `relational-view`
   documents deleting the registry row as the safe operator move when no collection
   remains; put it in the plan. Recommend, show the trade-off, confirm.
4. **Translations** — every key of this entity leaves ALL SEVEN catalogs; keys shared
   with a surviving base/role STAY (say which).
5. **Tests** — which test files go, which shared fixtures need editing.

## Phase 2 — Execute the plan (order is load-bearing)

Unwire first, delete second, migrate last: bootstrap/feature unmounting → routes/surfaces
→ views (own; then the approved edits + `Version` bumps on surviving views that embedded
this entity) → application/domain files → translations → tests → the approved data
migration (if dropping). Read the owning `/docs` section before each layer — same
mandatory-read rule as the scaffold skills.

## Final verify (the gate)

0. **Reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`) — walk
   the approved plan's own promises item by item with evidence; an unmet target is RED
   or an explicit dev-accepted deviation. The most destructive skill does not get to
   skip the discipline every mutating sibling carries.
1. **No corpses:** sweep the repo for the entity's names (type, tables, views, keys,
   routes) → the only remaining hits are the ones the plan explicitly kept (shared
   base, kept data migration history — historical migration files are NEVER rewritten).
2. **`gofmt -l` + `go vet` + `go build`** (engine + transport tags) — clean.
3. **Boot** — the service comes up with the entity gone; probes green; surviving
   surfaces still answer. The boot itself proves no orphan yaml subscribe survived
   (a declared subscribe with no receiver aborts boot — that abort here means the
   Phase 0 yaml sweep missed an entry). **A dev-profile boot does NOT prove prd:** the
   two non-dev-only gates this removal can trip — the foreign-collection guard (a kept
   Mongo collection, plan item 3) and the featureless-wiring reject (last feature
   removed, Phase 0) — WARN or pass under dev and ABORT everywhere else; check both
   explicitly against the plan instead of trusting the green dev boot.
4. **Regression** — the project's existing suite if it has one; report honestly if the
   suite itself referenced the removed entity (those tests were in the plan's inventory).
5. **Report the leftovers the dev owns:** data kept (tables/collections) per the
   approved strategy, the view-registry row / projection-state entries (plan item 3 —
   with the stale-row re-add trap named), broker topics/subjects that linger,
   consumers of removed events.
6. **Offer to run.** ONE question: boot the app to click through what remains? Yes →
   delegate to `/omnicore:run`. No → done.

Leave `specs/remove-entity/<entity>/` in place for review.

## Re-entry — plan already exists

`Status: DRAFT` → reopen the gate with what's already decided (never re-ask answered
slots). `Status: APPROVED` → execute only the not-yet-applied items of the inventory
(check what still exists on disk), then re-run the final verify. A changed answer
(data strategy, a dependent's fate) reopens the plan.

## Knowledge routing — step → `/docs` section

Resolve `<name>.html` at `<omnicore-dir>/docs/content/sections/<name>.html`, where
`<omnicore-dir>` = `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`.
Route first, then read ONLY the routed section(s) for the step at hand — never sweep
the whole manual; the Documentation Map in `<omnicore-dir>/CLAUDE.md` is the fallback
index for concepts this table doesn't list.

| When removing… | Read section(s) |
|---|---|
| feature unmounting / boot wiring | bootstrap |
| tables / drop DDL / down twins (the pair lands in EVERY target dialect's `migrations/<dialect>/`, same number) | migrations · table-schema |
| view-registry row hygiene / stale-row re-add trap | relational-view |
| shared base / role slices | table-schema · views |
| views / surviving embeds / `Version` | views · mongo-schema-evolution · auto-query-handlers |
| routes / OpenAPI cleanup | openapi · reference |
| GraphQL / gRPC surface cleanup | graphql · grpc |
| outbox / topics / integration events | transport |
| file layout (what else lives in a shared file) | service-layout |

## What this skill never does

No deletion before the approved plan, no framework edits, no git, no rewriting of
historical migration files, no dropping of shared bases/views other roles still use, no
data destruction beyond what the plan names — and the dev approved.
