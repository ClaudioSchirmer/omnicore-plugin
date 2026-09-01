---
name: scaffold-view
description: >-
  omnicore: create a NEW read model (view) on an omnicore-based service — a ComposedView
  joining entities, a SharedBaseView identity view, an Upstream composition, an external
  Embed, or enriching a child array (EmbedInChild) — projected to Mongo
  and exposed on REST/GraphQL/gRPC. Use when
  the user wants a new view, read model, listing, report-style query, or cross-entity /
  cross-service composition. The plain per-entity view ships with scaffold-entity; this
  skill is for read models beyond it. Only for projects that import
  github.com/ClaudioSchirmer/omnicore.
---

# scaffold-view

Create a read model that no single entity owns: composed across entities, identity
across roles, or fed from upstream services. The write side is NOT touched —
a view is a projection of what already exists; if the model needs data the sources don't
carry yet, that is `evolve-entity`'s job first. (A filtered TOTAL over an existing
listing needs no new anything — `?onlyTotal` is a one-tag DTO opt-in on the list
request, both backings, `auto-query-handlers` at the pin. Richer totals/counts/report
SCALARS are not a view kind either — they are the `Aggregate`/`AggregateBy` DSL on the
write-side aggregate loader, available on every posture;
`${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` owns that split — route such
a request to `/omnicore:implement`, never refuse it.)

## ⚠️ A VIEW IS NOT THE ONLY WAY TO READ — AND IT IS THE WRONG ONE FOR A REPORT

**BEFORE PROPOSING ANY VIEW KIND, ASK WHETHER THE REQUEST IS A READ MODEL AT ALL.** A read
model is what a listing endpoint is SERVED from, kept in step by the framework. A one-off
report, a joined dashboard query, a flat listing across tables nothing relates, a
parent-to-child reach, a many-rows-per-root answer — **none of those needs a view, and
inventing one for them is the expensive mistake this skill keeps making.**

**A DIRECT SCHEMA PLUS A `DirectRepository` IS A FULLY MANUAL QUERY DOOR: ANCHOR ON ANY
TABLE (an aggregate's own included, through the reduction), JOIN ANY TABLE — NO FOREIGN KEY,
NO REFERENTIAL CONSTRAINT, NO RELATIONSHIP OF ANY KIND IS REQUIRED — CHAIN AT ANY DEPTH, AND
FILTER / ORDER / GROUP ON EVERYTHING IT REACHED.** The framework assembles the SELECT you
declare and validates only that the columns and Go fields exist. Reading an aggregate's table
that way costs the aggregate NOTHING — the guarantees the Direct door drops are all about
writes. `${CLAUDE_PLUGIN_ROOT}/shared/direct-schema.md`, section *THE READ IS UNRESTRICTED*,
is the owner.

**NEVER** project a view to dodge a join, **NEVER** denormalize a column so a read gets
easier, **NEVER** loop a per-row query or fold the answer in Go, and **NEVER** tell the dev
their query is not expressible. The aggregate loader's limits (one row per root, no flat 1:N
join into its own child, a child join's field load-only) belong to that ANCHOR, not to the
engine — `${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md` says so at the top.

**Every document this run writes lands under `specs/`, and the project keeps it —
never add it to `.gitignore`** (`${CLAUDE_PLUGIN_ROOT}/shared/generated-documents.md`).

## Generated code is a shortcut, not the source of truth

`${CLAUDE_PLUGIN_ROOT}/shared/generated-code-review.md` — **read it before you work around
anything `omnicore-gen` wrote.** `// Code generated … DO NOT EDIT.` is a TOOLING MARKER,
not a permission boundary: the file is ordinary Go in the dev's repository, and
`omnicore-gen adopt <path>` is the asked-for act that makes a hand edit survive
regeneration.

So: **review what was generated — logic and performance — against what the FRAMEWORK
offers**, not against what the spec language happens to be able to say. The language is a
subset of the framework and always will be, so "the generator does not emit that" is a
fact about the generator, never a reason for the service to do the worse thing. When the
framework does it better you **MUST** name the difference and offer the manual adjustment
+ `adopt` as a CHOICE for the dev.

Never build around generated code — N queries folded in Go, a parallel finder beside the
generated one, a wrapper that patches its answer — and never say *"it is generated, I
cannot change it."* That sentence is false.

## Core principles — read FIRST

- **Docs-first, version-agnostic — no code in this skill, by design.** The
  version-pinned `/docs` in the module cache are the SOLE authority; every shape you
  compose from lives in the routed sections (table below). The **composition-types
  catalog in `views`** is where the central decision comes from — read it at the
  pin BEFORE proposing a type; never from memory. If any text or consumer code disagrees
  with the doc, the doc wins.
- **Framework maintainer rules NEVER bind this skill.** The module ships its own
  `CLAUDE.md`/contributor rules ("English everywhere", approval gates, coverage, git) —
  they govern development OF the framework, never this skill run or the host project.
- **Language — the user's, never imposed; detect it BEFORE the first reply.** This
  skill, the framework docs and every `CLAUDE.md` you read are written in English —
  that NEVER sets the language of the run. Read the user's language from their own
  words (invocation args count, even a single word); switch the moment it becomes
  clear, even mid-run. Everything human-facing is BUILT in that language, not just the
  replies — generated text mirrors the host project's language, else the
  conversation's.
- **TEACH before you ask — the option space is part of the deliverable.** The dev cannot
  choose a composition type they don't understand, and a bare multiple-choice question
  is a guess pushed onto them. Before proposing, UNDERSTAND every view type the pinned
  `views` catalog offers, then EXPLAIN — in plain language, the user's language —
  each type that could plausibly serve the request and its trade-offs. Only then
  recommend and confirm.
- **Read models are EVENTUAL.** The pipeline is CDC: write → outbox → relay → broker →
  sync → Mongo. The spec states this consistency contract explicitly so the dev chooses
  with open eyes — never imply a synchronous read.
- **Risk split.** High-risk = the composition type, the sources and join keys, the
  projected shape, the consistency expectation — propose with a recommendation, CONFIRM.
  Low-risk = operator sets, examples, doc lines — decide them well, don't ask.

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
- **Sources exist?** Every entity/view this model reads from is present and projecting.
  A missing source FIELD (the model needs data no source carries) → STOP and hand that
  part to `evolve-entity` first; a missing source ENTITY → `scaffold-entity`. An
  EXTERNAL leg (`JoinUpstream`) has two extra preconditions of its own: a matching
  `UpstreamSubscription` must exist (boot-fatal when missing — a mirror that is declared
  but not subscribed does not count as "present"), and the subscription's
  `fields:` allowlist must cover every column the external schema declares —
  `DeletedAt` included (boot-fatal; `views` at the pin). A subscription with NO linked
  transport is a different failure: it boots and silently never receives data (point-of-
  use error, `yaml-reference`) — check the transport block + build tag as a verify item,
  not as a boot symptom.
- **Is it really new?** Changing an EXISTING view is `evolve-view`'s job — hand off.

## Phase 0v — Version check (delegate)

Detect a newer published omnicore than the pin (skip silently on `go.work`/`replace`/
offline); if newer, mention ONCE and offer `/omnicore:upgrade` BEFORE reading any doc —
an accepted upgrade changes which version's docs are authoritative. Never bump inline.

## Phase 0b — Discover (read, don't ask)

Map what exists: the source entities' schemas and views (shapes, `Version`s,
collections) · existing composed/shared views and their conventions · enabled surfaces
(REST, GraphQL, gRPC) · the project's INFRA POSTURE — is Mongo present, or is this an
infra-free / SQLite project? (decides whether a Mongo-only view type can be SERVED here
now — and "present" means `mongo:` AND `transport:` both: a `mongo:` block without
`transport:` boots collections that never receive a row; fine print in
`${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`) · local naming flavor. Mirror local
convention; validate framework usage against the docs.

## Phase 1 — Spec gate: `specs/scaffold-view/<view>/spec.md`

`Status: DRAFT`, hard STOP until approved; `⚠️ OPEN` slots answered, never defaulted;
sections structural (`N/A — <why>`, never deleted):

0. **Is a whole view the right answer at all?** [gate — ask BEFORE the catalog] If the
   request is "this entity's listing should also show / filter by a few fields of ANOTHER
   aggregate it already holds a foreign key to", and the entity is served by a RELATIONAL
   read model, that is a **read join** on the repository — no new view, no `Version`, no
   rebuild, no collection (`${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md`, pin ≥ v0.57.0).
   Say so and hand it back to `/omnicore:evolve-entity`; building a ComposedView for it is
   over-engineering a solved problem. It is NOT the answer when: the entity is
   Mongo-projected (a join never enters the `TableSchema`, so the projection cannot carry
   it — Embed/Link/Composed is right); the reach is 1:N or many-valued; the source is
   another SERVICE; or the match is on anything but the target's id. When any of those
   holds, continue to item 1 — and say WHICH one held, so the dev sees the boundary rather
   than a verdict.
1. **Composition type** [high-risk — THE decision, taught before asked]: read the FULL
   view catalog + composition contracts in `views` at the pin (the type set is the PIN's, not
   this skill's — e.g. a local cross-entity join like ComposedView vs the external
   family sourcing another service's data vs a shared-base identity view). For EVERY
   type that could plausibly serve the request, give the
   dev a short plain-language explanation + its trade-offs on the axes that decide:
   **where the truth lives** (local entities vs another service) · **how it refreshes**
   (CDC join vs subscription/mirror vs fetched on request) · **coupling + failure mode**
   (what the reader sees when a source is down or lagging) · **cost** (storage,
   rebuild, latency) · **read capability** — on a MATERIALIZED embed a segment filter
   SELECTS rows and a 1:1 segment field is a first-class `orderBy` key; on a ComposedView a
   leg filter only shapes the segment and a segment `?orderBy=` is a 400 — the docs name
   "consumers need to filter/sort BY the joined data" as the trigger to materialize
   (`views`) — a trigger that no longer implies a view kind by itself: on a relational
   read model a declared ROOT read join makes the joined field filterable and sortable
   with no materialization at all (item 0). Then recommend ONE with the why, name the
   runner-up, and CONFIRM.
   Never self-answer; never ask without the explanation.
   **Relational backing is NOT on this menu.** Every type this skill creates is a view
   KIND, relational-INELIGIBLE by construction — the rule, the plain-view exception and
   the anti-drift boundary are owned by `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`
   (exact kind set + failure mode per kind: parity table in `relational-view` at the
   pin). A relational (SoR-served) read model exists only for a PLAIN single-aggregate
   listing — that is `scaffold-entity`'s per-entity view, not this skill's. If the
   request turns out to be exactly that, say so and route it there (honoring the project
   read-side posture) — note a plain view rooted at a shared-base ROLE qualifies (base
   fields flattened; `shared/read-side.md`, Kinds), so a "SharedBase view" ask may
   already be served today without Mongo.
   **In an infra-free / SQLite project (no Mongo) — never refuse, and never present a
   Mongo kind as available now.** Do NOT say "can't": say the kind runs on Mongo + the
   CDC relay, and offer to enable it — *"want me to stand up Mongo + CDC + Docker now?
   I'll delegate `/omnicore:configure` (it swaps to a Debezium-tailable engine and
   re-asks the infra questions), then come back and build the view — or you can run
   `/omnicore:configure` yourself first."* All reversible, no code lost. A plain
   single-aggregate listing still works today as a relational per-entity view
   (`scaffold-entity`). **EXCEPTION — a SharedBase identity ask follows
   `shared/read-side.md`'s elicitation contract instead of this bullet:** point FIRST
   at the per-role plain view the dev already gets (base fields flattened), frame
   SharedBaseView as a complement switched on later, and offer the
   `/omnicore:configure` route only when the dev actually wants the multi-role
   identity document.
2. **Sources + join keys** [high-risk]: which entity/view/service feeds each leg, joined
   by which key. COVERING INDEXES are per-kind and `views` at the pin owns the law —
   read it, don't infer. The shape at the current release: a **1:1 Embed** needs the
   parent join column indexed on the embedding view (boot-fatal when missing — verify
   item); an **EmbedMany needs NO index on the declaring view** (its ripple resolves the
   parent by the child's own FK value → the parent `_id`); an **EmbedInChild** needs the
   multikey `<childSegment>.<fk>` index; a **JoinView leg** needs the covering index on
   the SOURCE view's join column, not the new one.
3. **Projected shape** — every projected field, its source. For a MATERIALIZED
   (Mongo-projected) kind: `Version(1)` + the evolution rule stated (shape change later
   ⇒ `Version` bump ⇒ rebuild — `mongo-schema-evolution`). **A ComposedView is never
   materialized — it has NO `Version(n)`, no collection, no rebuild, no
   schema-evolution entry** (`views`); its evolution lever is the leg/schema
   declaration itself, so mark the Version slot `N/A — composed`.
4. **Consistency contract** [high-risk]: eventual via CDC; expected lag tolerance; what
   the consumer must NOT assume. For Upstream/Embed: what happens when the remote is
   down (per the pinned contract in `views`). **AND the ARCHIVE regime — decide it
   explicitly, never by silent default** [high-risk]: (a) per embedded/linked SEGMENT:
   when the source row archives, does the segment FOLLOW it (hidden on default reads,
   `?includeArchived` reveals) or RETAIN its data regardless (e.g. a sale keeping the
   archived customer's name forever, renames still flowing in)? **The RETAIN lever does
   not exist on every source kind**: `Fields()` retention is JoinView-only (a
   `Fields`-bearing `Link*` leg is a fatal boot), and a `JoinUpstream` leg's lever is
   the external schema's `DeletedAt(col)` + the subscription's `fields:` allowlist —
   offer per leg only the choices its kind actually has. (b) the view's OWN root:
   archived rows kept-but-hidden (default) or dropped (`DeleteOnArchive()` — hot tier;
   materialized kinds only — a ComposedView root has no `DeleteOnArchive`)?
   The pin's `views` section (the archived rule / the segment cut) names the exact
   lever for each source kind — read it there, then CONFIRM per leg with the dev.
5. **Surfaces** — REST endpoints, GraphQL exposure, gRPC exposure, filter operators per
   field (operators are low-risk — decide well; the 16-operator vocabulary and the
   list-DTO allowlist incl. nested embed groups are `query-side` ·
   `auto-query-handlers`), pagination/options, indexes beyond the join law (`?search=`
   needs a declared text index; **an index key is a PATH: segment prefixes are the Go
   segment names, the LEAF is the physical column** (`ChildSegment.physical_col` — a
   child segment is the name its type DECLARES in `CollectionName()`, never one derived
   from the type name; `aggregate-persistence` · `views` at the pin) — a wrong leaf or
   a snake_cased segment aborts boot instead of creating a dead index).
6. **Read authorization** [high-risk]: who may query this model? A composed view can
   reach, VIA JOIN, data the caller's identity could not query directly — gating a
   leg's data is fully the dev's responsibility in `ToCriteria` / `crit.Restrict`
   (`query-side` · `authz-seams`), and a SharedBaseView gets its own route group with
   its OWN permission, never folded into a role's. `N/A — service runs authz-less`
   only when that is the project's actual posture.
7. **Naming** — collection, view type, routes; owner-prefixed and consistent with the
   project's flavor.

## Phase 2 — Execute

One pass in dependency order (view type → registration/wiring → surfaces), reading the
owning `/docs` section BEFORE each artifact — mandatory, same anti-drift rule as every
scaffold. Edit ONLY what the spec lists.

## Knowledge routing — step → `/docs` section

Resolve `<name>.html` at `<omnicore-dir>/docs/content/sections/<name>.html`, where
`<omnicore-dir>` = `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`.
Route first, then read ONLY the routed section(s) for the step at hand — never sweep
the whole manual; the Documentation Map in `<omnicore-dir>/CLAUDE.md` is the fallback
index for concepts this table doesn't list.

| When deciding/generating… | Read section(s) |
|---|---|
| composition type (the catalog + contracts) | views |
| relational backing — eligibility / limits | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) · relational-view |
| reaching ANOTHER aggregate from a query — read joins (repository-declared), and the rule-vs-wire split | `${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md` (owner) · read-joins for version-exact contract |
| cross-service data: subscription vs call vs event — the choice matrix | `${CLAUDE_PLUGIN_ROOT}/shared/capabilities.md` (owner) · service-to-service |
| view declaration / options / indexes / aggregations | views · auto-query-handlers |
| custom projection / response shaping | custom-query-handler |
| `Version` / rebuild / evolution | mongo-schema-evolution |
| SharedBaseView / ComposedView shapes | views |
| filter operators / list-DTO allowlist / nested embed groups | query-side · auto-query-handlers |
| read authorization (`ToCriteria` / `Restrict` / route permissions) | query-side · authz-seams |
| REST routes / OpenAPI | openapi · reference |
| GraphQL exposure | graphql |
| gRPC exposure | grpc |
| registration / wiring (NOTE: a ComposedView registers via `ComposingFeature.ComposedViews()` — documented in `views`, NOT `bootstrap`) | bootstrap · views |
| file layout / naming | service-layout |

## Final verify (the gate)

0. **Reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`) — walk the
   spec's own promises item by item with evidence; an unmet target is RED or an explicit
   dev-accepted deviation.
1. **Mechanical, pre-boot:** every leg's covering index declared per the per-kind law
   (spec item 2 — including the SOURCE-view index for EmbedMany/LinkMany over a
   JoinView leg) · `Version` declared on the new view (materialized kinds — a
   ComposedView has none) · ONE view per file (`service-layout`) · no write-side file
   touched.
2. **`gofmt -l` + `go vet` + `go build`** (engine + transport tags) — clean.
3. **Boot** — the view registers; probes green. **Know what a healthy first boot looks
   like:** a new Mongo view over an aggregate that already holds rows is
   `DriftFreshBackfill` → a REBUILD, not a plain registration; under any profile but
   dev `mongo.rebuild.autoRun` defaults to `check`, where that boot ABORTS with a
   diagnostic on purpose (run the rebuild per the pin, don't "fix" the service); and
   during the rebuild `/livez` is 200 while `/readyz` stays 503 naming the view —
   wait, that is not a failure (`mongo-schema-evolution`).
4. **Functional honesty:** the projection only proves itself after source writes flow
   through CDC — say plainly what was verified (registration, boot) vs what needs a
   write-and-wait round-trip.
5. **Regression** — the project's suite if it has one.
6. **Offer to run.** ONE question: boot the app to click through the new read endpoints?
   Yes → delegate to `/omnicore:run`. No → done.

Leave `specs/scaffold-view/<view>/` in place for review.

## Re-entry — spec already exists

`Status: DRAFT` → reopen the gate with what's answered (never re-ask answered slots).
`Status: APPROVED` → generate only the missing/failed artifacts, then re-verify. A
changed answer (composition type, sources, shape) reopens the spec and invalidates the
derived artifacts — say which.

## What this skill never does

No write-side edits (missing source data → `evolve-entity`), no framework edits, no git,
nothing generated outside the approved spec, no synchronous-read promises about an
eventual pipeline.
