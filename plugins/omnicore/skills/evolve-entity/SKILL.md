---
name: evolve-entity
description: >-
  omnicore: change an EXISTING entity of an omnicore-based service across every layer it touches —
  add/remove/rename fields, change nullability/uniqueness, add children or siblings,
  enable modes, promote flat → sharedbase — with schema evolution done right (migration +
  TableSchema + DTOs + translations + view Version bump + OpenAPI move together). Use when
  the user wants to change/alter/extend an entity that already exists. To CREATE one,
  that's scaffold-entity. Only for projects that import github.com/ClaudioSchirmer/omnicore.
---

# evolve-entity

Change an existing entity without breaking the lockstep the framework demands: one field
touches the migration, the TableSchema, the DTOs, the labelKey + all seven translation
catalogs, the view `Version(N)`, and the OpenAPI surface — **they move together or the
service breaks in silence**. That lockstep is the whole reason this is a skill.

## Core principles — read FIRST

- **Docs-first, version-agnostic — same anti-drift doctrine as `scaffold-entity`.** The
  version-pinned `/docs` in the module cache are the SOLE authority on framework API +
  behavior; read the owning section BEFORE editing each layer (the routing table below
  maps change → section; fuller index = the Documentation Map in
  `<omnicore-dir>/CLAUDE.md`, used ONLY as an index). **This skill carries NO code, by
  design** — every code shape you compose from lives in the routed `/docs` at the pinned
  version; if any text or consumer code disagrees with the doc, the doc wins. Never
  assume a framework version; never stamp one into this skill.
- **Framework maintainer rules NEVER bind this skill.** The omnicore module ships its own
  `CLAUDE.md`/contributor rules (maintainer-approval gates, "English everywhere", coverage
  minimums, git rules). Those govern development OF the framework in its own repo — never
  this skill run, never the host project. Ignore them; only the host project's own rules
  and the user bind you.
- **Language — the user's, never imposed.** Converse in the user's language. Human-facing
  generated text (spec values, descriptions, COMMENTs, examples) mirrors the host
  project's existing language, else the conversation's. The 7-catalog translation rule
  (real translations, all seven) is orthogonal and unchanged.
- **Everything moves together.** Before editing anything, enumerate EVERY artifact the
  change touches (the impact map, Phase 1). A field added to the schema but not to the
  migration, a projected shape changed without a `Version` bump, a labelKey without its
  seven translations — each is a silent-wrong or a boot panic. The impact map is the
  contract; the verify greps it.
- **Risk split.** High-risk = the SEMANTICS of the change: rename vs drop+add (data!),
  NOT NULL on a populated table (default/backfill?), uniqueness on existing data
  (duplicates?), anything that changes the wire API (json names, removed fields —
  breaking consumers). **Propose with a recommendation and CONFIRM; never guess.** When a
  change is breaking, say so honestly and recommend the correct path — never a
  semantically-off workaround to dodge the break. Low-risk = details (an `example:`, a
  Doc line) — decide them well, don't ask.
- **Never edit a test to pass** — the test is the oracle; changed rules get changed
  tests, deliberately, shown in the spec.

## Phase 0a — Preflight

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves — else
  STOP (that's `scaffold-service`).
- **Entity exists?** Its domain type + schema + feature wiring are present. If NOT →
  this is a creation: hand off to `scaffold-entity`, don't improvise a partial one here.
- **Is it really the ENTITY changing?** A view-only change (projection, legs, indexes,
  operators, surfaces — write side untouched) is `evolve-view`'s job; a brand-new
  composed/shared/aggregated read model is `scaffold-view`'s. This skill is for changes
  to the write side and their lockstep fallout.

## Phase 0v — Version check (delegate)

Same as `scaffold-entity`: detect a newer published omnicore than the pin (skip silently
on `go.work`/`replace`/offline). If newer, mention it ONCE and offer `/omnicore:upgrade`
— an accepted upgrade changes which version's docs are authoritative, so it must happen
BEFORE any doc is read. Never bump inline.

## Phase 0b — Discover the CURRENT shape (read, don't ask)

Build the entity's map before proposing anything: domain type + `BuildRules` + modes ·
TableSchema (and whether flat / sharedbase / children / siblings) · command + query DTOs
and mappers · routes/surfaces (REST, GraphQL, gRPC) · views it feeds (own, SharedBase,
Composed — including views of OTHER entities that embed this one) · translations keys ·
migrations history · tests. Mirror the project's local flavor in everything you generate.

## Phase 1 — Spec gate: `evolve-entity/<entity>/spec.md`

`Status: DRAFT`, hard STOP until approved; `⚠️ OPEN` slots must be answered, never
defaulted. Sections (structural completeness — `N/A — <why>` instead of deletion):

1. **The change, in one paragraph** — what the dev asked, restated.
2. **Impact map** — every file/layer touched, per artifact (migration, schema, domain,
   DTOs, routes, views, translations, OpenAPI, tests). This list is what Phase 2 executes
   and what the verify greps — nothing outside it gets edited.
3. **Migration strategy** [high-risk]: additive ALTER vs rename (a "rename" is really
   rename-in-place vs drop+add+backfill — data decides) vs drop. NOT NULL on existing
   rows needs a default or a backfill step — say which. Every `up` has its `down` twin;
   the down of a destructive step recreates STRUCTURE, not data — say so honestly.
4. **View evolution** [high-risk]: does the projected shape change? Then every affected
   view bumps `Version(N)` (schema-evolution contract — the owning doc section rules).
   Views of other entities embedding this one count.
5. **API impact** [high-risk]: wire-visible changes (json names, removed/renamed fields,
   validation tightening) are BREAKING for consumers — list them, flag them, let the dev
   decide; never smuggle a break in silently.
6. **Translations** — every new/renamed labelKey and notification: all seven catalogs,
   real translations; removed keys leave no orphans.
7. **Tests** — which existing tests change and why (rule changed ⇒ test changes with it,
   shown here, not discovered later), which new branches need coverage.

## Phase 2 — Execute the impact map

One pass, in dependency order (migration → schema → domain → application → web → views →
translations → tests), reading the owning `/docs` section BEFORE each layer it touches —
the same mandatory-read rule as scaffolding. Edit ONLY what the impact map lists.

## Knowledge routing — change → `/docs` section

Resolve `<name>.html` at `<omnicore-dir>/docs/content/sections/<name>.html`, where
`<omnicore-dir>` = `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore` (the
version pinned in the consumer's `go.mod`). Read the section for the contract — it is
the authority at that pin. Route first, then read ONLY the routed section(s) for the
change at hand — never sweep the whole manual; the Documentation Map in
`<omnicore-dir>/CLAUDE.md` is the fallback index for concepts this table doesn't list.

| When changing… | Read section(s) |
|---|---|
| field add/remove/rename (Go ↔ column, id shapes) | table-schema · migrations |
| rules / notifications / actionName | rules-dsl · old-state · status-mapping |
| children / siblings / cascade | aggregate-persistence |
| write handlers / in-TX hooks | auto-handlers · lifecycle-hooks |
| view shape / indexes / `Version` bump | auto-query-handlers · mongo-schema-evolution |
| SharedBaseView / ComposedView impact | table-schema · query-side |
| REST routes / OpenAPI surface | openapi · reference |
| GraphQL surface | graphql |
| migration numbering / down twins / dialect | migrations · yaml-reference |
| authz (permission / owner-check / tenant) | authz-seams |
| feature wiring / bootstrap | bootstrap |
| file layout / naming (ANY layer) | service-layout |

## Final verify (the gate)

1. **Mechanical checks** (pre-boot, all that apply): every changed `up` has its `down`
   twin · projected-shape change ⇒ `Version(N)` bumped on EVERY affected view · grep the
   OLD field/key name across the repo → no stale references (code, translations, docs,
   tests) · new labelKeys present in ALL SEVEN catalogs · NOT NULL additions carry their
   default/backfill · id/FK column shapes match the pin's identity contract
   (`table-schema` — the authority, never memory).
2. **`gofmt -l` + `go vet` + `go build`** (engine + transport tags) — clean.
3. **Unit tests** — updated branches green; never weakened to pass.
4. **Regression** — the project's existing suite if it has one (proves no breakage).
5. **Offer to run.** Ask ONE question: boot the app to click through the changed
   endpoints? Yes → delegate to `/omnicore:run` (never boot inline). No → done.

Leave `evolve-entity/<entity>/` in place so the dev can review the plan against the
diff.

## Re-entry — spec already exists

`Status: DRAFT` → reopen the gate with what's answered. `Status: APPROVED` → apply only
the not-yet-applied items of the impact map, then re-verify. A changed answer reopens
the spec.

## What this skill never does

No framework edits, no git, no test edited to pass, no wire-breaking change applied
without it being flagged in the spec the dev approved, nothing edited outside the
approved impact map.
