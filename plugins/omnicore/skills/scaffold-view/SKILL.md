---
name: scaffold-view
description: >-
  omnicore: create a NEW read model (view) on an omnicore-based service — a ComposedView
  joining entities, a SharedBaseView identity view, an Upstream composition, an external
  Embed, enriching a child array (EmbedInChild), or an aggregated view — projected to Mongo
  and exposed on REST/GraphQL. Use when
  the user wants a new view, read model, listing, report-style query, or cross-entity /
  cross-service composition. The plain per-entity view ships with scaffold-entity; this
  skill is for read models beyond it. Only for projects that import
  github.com/ClaudioSchirmer/omnicore.
---

# scaffold-view

Create a read model that no single entity owns: composed across entities, identity
across roles, fed from upstream services, or aggregated. The write side is NOT touched —
a view is a projection of what already exists; if the model needs data the sources don't
carry yet, that is `evolve-entity`'s job first.

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
  part to `evolve-entity` first; a missing source ENTITY → `scaffold-entity`.
- **Is it really new?** Changing an EXISTING view is `evolve-view`'s job — hand off.

## Phase 0v — Version check (delegate)

Detect a newer published omnicore than the pin (skip silently on `go.work`/`replace`/
offline); if newer, mention ONCE and offer `/omnicore:upgrade` BEFORE reading any doc —
an accepted upgrade changes which version's docs are authoritative. Never bump inline.

## Phase 0b — Discover (read, don't ask)

Map what exists: the source entities' schemas and views (shapes, `Version`s,
collections) · existing composed/shared views and their conventions · enabled surfaces
(REST, GraphQL, gRPC) · the project's INFRA POSTURE — is Mongo present, or is this an
infra-free / SQLite project? (decides whether a Mongo-only view type can be served here
now) · local naming flavor. Mirror local convention; validate framework usage against
the docs.

## Phase 1 — Spec gate: `scaffold-view/<view>/spec.md`

`Status: DRAFT`, hard STOP until approved; `⚠️ OPEN` slots answered, never defaulted;
sections structural (`N/A — <why>`, never deleted):

1. **Composition type** [high-risk — THE decision, taught before asked]: read the FULL
   view catalog + composition contracts in `views` at the pin (the type set is the PIN's, not
   this skill's — e.g. a local cross-entity join like ComposedView vs the external
   family sourcing another service's data vs a shared-base identity view vs an
   aggregated view). For EVERY type that could plausibly serve the request, give the
   dev a short plain-language explanation + its trade-offs on the axes that decide:
   **where the truth lives** (local entities vs another service) · **how it refreshes**
   (CDC join vs subscription/mirror vs fetched on request) · **coupling + failure mode**
   (what the reader sees when a source is down or lagging) · **cost** (storage,
   rebuild, latency). Then recommend ONE with the why, name the runner-up, and CONFIRM.
   Never self-answer; never ask without the explanation.
   **Relational backing is NOT on this menu.** Every type this skill creates
   (ComposedView, SharedBaseView, the Embed/Link family, Upstream, aggregated) is
   relational-INELIGIBLE — a boot fail, a 400, or a different declaration type carrying
   no `.RelationalSource()` at all (parity table in `relational-view`). A relational
   (SoR-served) read model exists only for a PLAIN single-aggregate listing — that is
   `scaffold-entity`'s per-entity view, not this skill's. If the request turns out to be
   exactly that, say so and route it there (honoring the project read-side posture).
   **In an infra-free / SQLite project (no Mongo) — never refuse, offer the upgrade.** A
   ComposedView / SharedBaseView / Embed / Upstream / aggregated view needs Mongo (it
   projects a document; SQLite has no projection). Do NOT say "can't": say it runs on
   Mongo, and offer to enable it — *"want me to stand up Mongo + CDC + Docker now? I'll
   delegate `/omnicore:configure` (it swaps to a Debezium-tailable engine and re-asks the
   infra questions), then come back and build the view — or you can run
   `/omnicore:configure` yourself first."* All reversible, no code lost. A plain
   single-aggregate listing still works today as a relational per-entity view
   (`scaffold-entity`).
2. **Sources + join keys** [high-risk]: which entity/view/service feeds each leg, joined
   by which key. Every 1:N leg's FK is INDEXED (boot-fatal otherwise — verify item).
3. **Projected shape** — every projected field, its source, `Version(1)`; the evolution
   rule stated (shape change later ⇒ `Version` bump ⇒ rebuild — `mongo-schema-evolution`).
4. **Consistency contract** [high-risk]: eventual via CDC; expected lag tolerance; what
   the consumer must NOT assume. For Upstream/Embed: what happens when the remote is
   down (per the pinned contract in `views`). **AND the ARCHIVE regime — decide it
   explicitly, never by silent default** [high-risk]: (a) per embedded/linked SEGMENT:
   when the source row archives, does the segment FOLLOW it (hidden on default reads,
   `?includeArchived` reveals) or RETAIN its data regardless (e.g. a sale keeping the
   archived customer's name forever, renames still flowing in)? (b) the view's OWN root:
   archived rows kept-but-hidden (default) or dropped (`DeleteOnArchive()` — hot tier)?
   The pin's `views` section (the archived rule / the segment cut) names the exact
   lever for each source kind — read it there, then CONFIRM per leg with the dev.
5. **Surfaces** — REST endpoints, GraphQL exposure, filter operators per field
   (operators are low-risk — decide well), pagination/options.
6. **Naming** — collection, view type, routes; owner-prefixed and consistent with the
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
| relational backing — eligibility / limits | relational-view |
| view declaration / options / indexes / aggregations | views · auto-query-handlers |
| custom projection / response shaping | custom-query-handler |
| `Version` / rebuild / evolution | mongo-schema-evolution |
| SharedBaseView / ComposedView shapes | views |
| REST routes / OpenAPI | openapi · reference |
| GraphQL exposure | graphql |
| registration / wiring | bootstrap |
| file layout / naming | service-layout |

## Final verify (the gate)

1. **Mechanical, pre-boot:** every 1:N leg FK indexed · `Version` declared on the new
   view · ONE view per file (`service-layout`) · no write-side file touched.
2. **`gofmt -l` + `go vet` + `go build`** (engine + transport tags) — clean.
3. **Boot** — the view registers; probes green.
4. **Functional honesty:** the projection only proves itself after source writes flow
   through CDC — say plainly what was verified (registration, boot) vs what needs a
   write-and-wait round-trip.
5. **Regression** — the project's suite if it has one.
6. **Offer to run.** ONE question: boot the app to click through the new read endpoints?
   Yes → delegate to `/omnicore:run`. No → done.

Leave `scaffold-view/<view>/` in place for review.

## Re-entry — spec already exists

`Status: DRAFT` → reopen the gate with what's answered (never re-ask answered slots).
`Status: APPROVED` → generate only the missing/failed artifacts, then re-verify. A
changed answer (composition type, sources, shape) reopens the spec and invalidates the
derived artifacts — say which.

## What this skill never does

No write-side edits (missing source data → `evolve-entity`), no framework edits, no git,
nothing generated outside the approved spec, no synchronous-read promises about an
eventual pipeline.
