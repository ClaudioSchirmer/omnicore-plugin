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
- **Language — the user's, never imposed; detect it BEFORE the first reply.** This
  skill, the framework docs and every `CLAUDE.md` you read are written in English —
  that NEVER sets the language of the run. Read the user's language from their own
  words (invocation args count, even a single word); switch the moment it becomes
  clear, even mid-run. Everything human-facing is BUILT in that language, not just the
  replies — spec values, descriptions, COMMENTs, examples — mirroring the host
  project's existing language, else the conversation's. The 7-catalog translation rule
  (real translations, all seven) is orthogonal and unchanged — those belong to the
  dev's END USERS, dynamic, never collapsed to the conversation language.
- **Everything moves together.** Before editing anything, enumerate EVERY artifact the
  change touches (the impact map, Phase 1). Know WHICH failure each miss buys, because
  triage differs: a missed `Version` bump is an UNCONDITIONAL boot abort
  (forgot-to-bump drift — no autoRun escape); a schema/migration mismatch is a boot
  abort or a first-INSERT 500; a labelKey missing its translations has NO guard at all
  — it renders blank/raw at runtime, invisible to every gate but a read of the
  response. The impact map is the contract; the verify greps it.
- **Risk split.** High-risk = the SEMANTICS of the change: rename vs drop+add (data!),
  NOT NULL on a populated table (default/backfill?), uniqueness on existing data
  (duplicates?), anything that changes the wire API (json names, removed fields —
  breaking consumers). **Propose with a recommendation and CONFIRM; never guess.** When a
  change is breaking, say so honestly and recommend the correct path — never a
  semantically-off workaround to dodge the break. Low-risk = details (an `example:`, a
  Doc line) — decide them well, don't ask.
- **Never edit a test to pass** — the test is the oracle; changed rules get changed
  tests, deliberately, shown in the spec.

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

## Phase 0a — Preflight

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves — else
  STOP (that's `scaffold-service`).
- **Entity exists?** Its domain type + schema + feature wiring are present. If NOT →
  this is a creation: hand off to `scaffold-entity`, don't improvise a partial one here.
- **Is it really the ENTITY changing?** A view-only change (projection, legs, indexes,
  operators, surfaces — write side untouched) is `evolve-view`'s job; a brand-new
  composed/shared/upstream read model is `scaffold-view`'s. This skill is for changes
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
2. **Impact map** — every file/layer touched, per artifact (migration — in EVERY target
   dialect's folder, same number, schema, domain,
   DTOs, routes, views, translations, OpenAPI, tests, **and the classes an evolution
   forgets**: feature/bootstrap wiring · `microservice.*.yaml` — `auth.publicRoutes`
   when a route path changes, `integration:` when the entity starts publishing · the
   proto contract + regenerated stubs when the entity is gRPC-exposed · the GraphQL
   schema when exposed there). This list is what Phase 2 executes
   and what the verify greps — nothing outside it gets edited, so a class missing HERE
   is out of scope by construction: enumerate against Phase 0b's surface map, not from
   memory.
3. **Migration strategy** [high-risk]: additive ALTER vs rename (a "rename" is really
   rename-in-place vs drop+add+backfill — data decides) vs drop. NOT NULL on existing
   rows needs a default or a backfill step — say which. Every `up` has its `down` twin;
   the down of a destructive step recreates STRUCTURE, not data — say so honestly.
4. **View evolution** [high-risk]: does the projected shape change? Then every affected
   MATERIALIZED view bumps `Version(N)` — and the root schema's COLUMNS are part of the
   rebuild hash, so a plain field add on the aggregate moves it: the full hash list
   lives in `views` at the pin (read it, not just `mongo-schema-evolution`, whose bump
   list is options-scoped); a missed bump is an unconditional boot abort.
   Views of other entities embedding this one count — **per kind, differently**: a
   `JoinView` embedder is Version-COUPLED (bump it in the same change — the guard fires
   on it otherwise); a `JoinView` leg's `.Fields()` allowlist does NOT pick up a new
   field (silent omission — extending the list is itself a shape change on the
   embedder), while a renamed/removed field a leg still lists is a BOOT REJECT; a
   ComposedView / `Link*` leg has NO `Version` at all — its lever is the leg/schema
   declaration itself. **Archive newly enabled ⇒ settle
   the view's archive regime in this spec** — gated on the view's backing, per
   `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`'s elicitation contract (relational serves
   no `DeleteOnArchive()`); never a silent default — the scaffold side asks this at
   birth, adding the mode later doesn't get to skip it.
5. **API impact** [high-risk]: wire-visible changes (json names, removed/renamed fields,
   validation tightening) are BREAKING for consumers — list them, flag them, let the dev
   decide; never smuggle a break in silently.
6. **Translations** — every new/renamed labelKey and notification: all seven catalogs,
   real translations; removed keys leave no orphans.
7. **Tests** — which existing tests change and why (rule changed ⇒ test changes with it,
   shown here, not discovered later), which new branches need coverage. **The project's
   generated contract QA (`qa/*.sh`, runner, fixtures) is an entity-shaped artifact,
   not a frozen oracle**: a deliberate contract change breaks it BY DESIGN — plan its
   update (or a regeneration via `/omnicore:qa`) here, explicitly; that planned update
   is not the "edit a test to pass" sin, silence about it is.
8. **Kind promotion (flat → sharedbase)** [high-risk — only when the change IS this;
   `N/A` otherwise]: the hardest migration in the catalog — never improvise it. The
   pieces, each explicit in the spec: the new base table + the natural-key choice (the
   base PK derives from it — UUIDv5; wrong key = merged/split identities, data
   corruption not a bug) · the FK model (shared-PK vs separate-FK — re-enrollment
   semantics, dev's call) · the data move as a real migration + backfill (up creates
   base + backfills from the flat table, down recreates STRUCTURE only — say so) · the
   role table slims to role-only fields · the identity becomes its OWN bounded context
   (routes file, feature, `<identity>:read` permission — named after the BASE) · views
   and translations follow. Mechanics: `table-schema` at the pin + scaffold-entity's
   `conventions/sharedbase.md` (the write/read contracts are THE reference — this
   skill adds only the migration path). The REVERSE (sharedbase→flat demotion, or
   moving a role to a different base) is the same catalog walked backwards — equally
   spec-gated here, never improvised and never refused as unsupported.

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
| value objects: add/rename a VO field, a new enum, place in vos/ or aggregatevos/ | value-objects · service-layout |
| unique field add/remove → `Constraints` binding · per-engine id/decimal/boolean columns | `${CLAUDE_PLUGIN_ROOT}/shared/dialects/<dialect>.md` (read every TARGET dialect's — targets = `relational.dialect` across ALL `microservice.*.yaml`; the ALTER pair lands in EVERY target's `migrations/<dialect>/` at the same number) |
| rules / notifications / actionName | rules-dsl · old-state · status-mapping |
| children / siblings / cascade — AND any NEW table's shape rules (child FK + covering index; sibling shares the owner's PK, no lifecycle columns; role UNIQUE FK; revision column on entity/base tables only) | aggregate-persistence · table-schema · migrations · scaffold-entity's `conventions/{aggregate-children,siblings,migrations}.md` |
| write handlers / in-TX hooks | auto-handlers · lifecycle-hooks |
| full write-path ripple of a change (SQL ↔ outbox ↔ Mongo op ↔ audit verb) | lifecycle-map |
| read-side impact of a write change (backing, kinds, archive regime) | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) |
| view shape / indexes / `Version` bump (the FULL rebuild-hash list — root columns, embeds, embedder coupling — is in `views`) | views · auto-query-handlers · mongo-schema-evolution |
| SharedBaseView / ComposedView impact | views · query-side |
| REST routes / OpenAPI surface | openapi · reference |
| GraphQL surface | graphql |
| gRPC surface / proto contract + stub regeneration | grpc |
| migration numbering / down twins / dialect | migrations · yaml-reference |
| authz (permission / owner-check / tenant) | authz-seams |
| feature wiring / bootstrap | bootstrap |
| file layout / naming (ANY layer) | service-layout |

## Final verify (the gate)

0. **Reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`) — walk the
   plan's own promises item by item with evidence; an unmet target is RED or an explicit
   dev-accepted deviation.
1. **Mechanical checks** (pre-boot, all that apply — run to a CLEAN pass before
   anything boots): every changed `up` has its `down`
   twin, in EVERY target dialect's folder at the same number · projected-shape change ⇒
   `Version(N)` bumped on EVERY affected materialized view (JoinView embedders
   included — spec item 4) · grep the
   OLD field/key name across the repo → no stale references (code, translations, docs,
   tests, view legs/`Fields()` lists) · new labelKeys present in ALL SEVEN catalogs ·
   NOT NULL additions carry their
   default/backfill · id/FK column shapes match the pin's identity contract
   (`table-schema` — the authority, never memory) · a new Response field beside a
   request declaring `Fields` is `*T`/slice + `,omitempty` (the recursive sparse-render
   guard PANICS at boot otherwise) · Archive newly enabled ⇒ `Modes()` ⟺ schema
   archive-column ⟺ migration in lockstep · a value-typed `query:`-tagged scalar
   renders REQUIRED in OpenAPI (pointer unless the spec says required) · any NEW table
   follows the shape rules (revision column on entity/base only, no leading-`_`
   physical column, unique constraints named on the OWNING table).
2. **`gofmt -l` + `go vet` + `go build`** (engine + transport tags) — clean.
3. **Unit tests** — updated branches green; never weakened to pass.
4. **BOOT the service** (a verification boot: dev profile, target tags, log to a file —
   never through a pipe — then SIGTERM; the same in-gate boot the sibling skills do —
   distinct from the interactive `/omnicore:run` offer below). Every characteristic
   failure of an evolution is boot-time, not compile-time: migration apply/dirty,
   schema binding, the sparse-render guard, `publicRoutes` exact-match, view drift.
   Know the healthy shapes: under non-dev profiles pending migrations
   (`autoRun: check`) and a pending view rebuild ABORT on purpose; `/readyz` 503
   `rebuilding view` = wait, not a failure (`shared/boot-contract.md`). A green build
   that was never booted is NOT a verified evolution.
5. **Regression** — the project's existing suite if it has one (proves no breakage);
   contract changes hit the planned qa updates from spec item 7, nothing unplanned.
6. **Offer to run.** Ask ONE question: boot the app to click through the changed
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
