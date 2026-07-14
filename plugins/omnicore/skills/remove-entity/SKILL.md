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

## Core principles

- **Nothing is deleted before the plan is approved.** The full footprint is inventoried
  into `remove-entity/<entity>/plan.md`, the dev approves it, and Phase 2 deletes
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
  Converse in the user's language.

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
  topics/subjects and integration events it publishes.

## Phase 1 — The gate: `remove-entity/<entity>/plan.md`

`Status: DRAFT`, hard STOP until the dev approves; `⚠️ OPEN` slots must be answered,
never defaulted. Sections (structural — `N/A — <why>`, never deleted):

1. **Inventory** — every file to delete and every file to EDIT (unwiring), per layer.
   This list is the contract; Phase 2 touches nothing outside it.
2. **Dependents** [blocking]: every shared surface found in Phase 0 — shared base with
   other roles (the base STAYS; only this role's slice goes), composed/embedding views
   of other entities (they need their own edit + `Version` decision, listed here),
   integration events consumed elsewhere (consumers break — the dev decides the
   sequencing). Each one `⚠️ OPEN` until decided.
3. **Data strategy** [high-risk]: drop the tables (new migration, down recreates
   structure only — data is GONE) vs retire code and keep the data. Same decision for
   the Mongo view collections. Recommend, show the trade-off, confirm.
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

1. **No corpses:** sweep the repo for the entity's names (type, tables, views, keys,
   routes) → the only remaining hits are the ones the plan explicitly kept (shared
   base, kept data migration history — historical migration files are NEVER rewritten).
2. **`gofmt -l` + `go vet` + `go build`** (engine + transport tags) — clean.
3. **Boot** — the service comes up with the entity gone; probes green; surviving
   surfaces still answer.
4. **Regression** — the project's existing suite if it has one; report honestly if the
   suite itself referenced the removed entity (those tests were in the plan's inventory).
5. **Report the leftovers the dev owns:** data kept (tables/collections) per the
   approved strategy, broker topics/subjects that linger, consumers of removed events.
6. **Offer to run.** ONE question: boot the app to click through what remains? Yes →
   delegate to `/omnicore:run`. No → done.

Leave `remove-entity/<entity>/` in place for review.

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
| tables / drop DDL / down twins | migrations · table-schema |
| shared base / role slices | table-schema · query-side |
| views / surviving embeds / `Version` | auto-query-handlers · mongo-schema-evolution · query-side |
| routes / OpenAPI cleanup | openapi · reference |
| GraphQL / gRPC surface cleanup | graphql · grpc |
| outbox / topics / integration events | transport |
| file layout (what else lives in a shared file) | service-layout |

## What this skill never does

No deletion before the approved plan, no framework edits, no git, no rewriting of
historical migration files, no dropping of shared bases/views other roles still use, no
data destruction beyond what the plan names — and the dev approved.
