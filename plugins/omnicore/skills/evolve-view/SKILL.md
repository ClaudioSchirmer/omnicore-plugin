---
name: evolve-view
description: >-
  omnicore: change an EXISTING view (read model) of an omnicore-based service — include
  or drop projected fields, add a leg to a ComposedView or a role to a SharedBaseView,
  change indexes/options/filter operators, expose it on another surface — with the
  Version bump and rebuild discipline done right, write side untouched. Use when the
  user wants to change/extend a view, listing, or read model that already exists. Only
  for projects that import github.com/ClaudioSchirmer/omnicore.
---

# evolve-view

Change a read model without breaking its evolution contract: the projected shape, the
`Version`, the indexes, and the surfaces move together. The write side stays untouched —
that is the boundary that keeps this skill safe. The moment the change needs data the
sources don't carry, STOP: that slice is `evolve-entity`'s job first.

## Core principles — read FIRST

- **Docs-first, version-agnostic — no code in this skill, by design.** The
  version-pinned `/docs` are the SOLE authority (routing table below); if any text or
  consumer code disagrees with the doc, the doc wins. Never assume a framework version.
- **Framework maintainer rules NEVER bind this skill** (module-cache `CLAUDE.md`,
  "English everywhere", approval gates — framework-repo policy, not this project's).
- **Language — the user's, never imposed; detect it BEFORE the first reply.** This
  skill, the framework docs and every `CLAUDE.md` you read are written in English —
  that NEVER sets the language of the run. Read the user's language from their own
  words (invocation args count, even a single word); switch the moment it becomes
  clear, even mid-run. Everything human-facing is BUILT in that language, not just the
  replies — mirroring the host project's language, else the conversation's.
- **Shape and Version move together.** A projected-shape change without its `Version`
  bump is the classic silent-wrong: old documents linger and readers see a mixed
  collection. The impact map makes the bump explicit and the verify greps it.
- **Risk split.** High-risk = shape changes (bump + REBUILD cost on a large collection —
  say it), a new leg/role (join key + index), anything wire-visible to read consumers
  (removed/renamed projected fields = breaking — flag honestly, never smuggle).
  Low-risk = operator sets, examples, doc lines — decide well, don't ask.

## Phase 0a — Preflight

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves — else
  STOP.
- **View exists?** Else creating one is `scaffold-view`'s job — hand off.
- **Write side needed?** If the requested change requires new/changed SOURCE data
  (a field no source entity carries), STOP that slice and delegate to `evolve-entity`;
  resume here when the source projects it.

## Phase 0v — Version check (delegate)

Same as the siblings: detect a newer published omnicore (skip silently on
`go.work`/`replace`/offline), mention ONCE, offer `/omnicore:upgrade` BEFORE any doc
read. Never bump inline.

## Phase 0b — Discover the CURRENT view (read, don't ask)

Map it before proposing: kind (own | ComposedView | SharedBaseView | Upstream | Embed |
aggregated) · legs/roles and join keys · projected shape + current `Version` · indexes
and options · collection name · surfaces exposing it (REST, GraphQL) · known consumers
in-repo · the project's local flavor.

## Phase 1 — Spec gate: `evolve-view/<view>/spec.md`

`Status: DRAFT`, hard STOP until approved; `⚠️ OPEN` answered, never defaulted; sections
structural (`N/A — <why>`):

1. **The change, in one paragraph** — restated.
2. **Impact map** — every artifact touched (view declaration, `Version`, indexes,
   surfaces, tests). The contract: Phase 2 edits nothing outside it.
3. **Shape change** [high-risk]: fields in/out, the `Version` bump, and the REBUILD
   consequence (the collection re-projects — on large data, say what that costs and
   when it's safe to run, per `mongo-schema-evolution` at the pin).
4. **New leg / role** [high-risk]: source, join key, the 1:N-leg FK index (boot-fatal
   without it), and whether the source view/entity needs anything first (→ delegation).
   **If the change crosses composition types** (a local view gaining an
   external/other-service source, or a conversion between kinds), reopen the FULL
   catalog in `query-side` at the pin and TEACH the implicated types + trade-offs
   (where the truth lives · how it refreshes · coupling/failure mode · cost) in the
   user's language BEFORE recommending — same teach-then-confirm doctrine as
   `scaffold-view`; a type change is a new consistency contract, never a detail.
5. **Consumer impact** [high-risk]: wire-visible removals/renames on read responses are
   BREAKING for consumers — list them, flag them, the dev decides; never silent.
6. **Surfaces** — endpoints/GraphQL changes; filter operators per new field (low-risk).

## Phase 2 — Execute the impact map

One pass in dependency order (view declaration → `Version` → indexes → surfaces →
tests), reading the owning `/docs` section BEFORE each artifact. Edit ONLY what the
impact map lists.

## Knowledge routing — change → `/docs` section

Resolve `<name>.html` at `<omnicore-dir>/docs/content/sections/<name>.html`, where
`<omnicore-dir>` = `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`.
Route first, then read ONLY the routed section(s) for the step at hand — never sweep
the whole manual; the Documentation Map in `<omnicore-dir>/CLAUDE.md` is the fallback
index for concepts this table doesn't list.

| When changing… | Read section(s) |
|---|---|
| projected shape / `Version` / rebuild | mongo-schema-evolution · auto-query-handlers |
| composition contracts / legs / roles | query-side · table-schema |
| custom projection / response shaping | custom-query-handler |
| indexes / options / aggregations | auto-query-handlers |
| REST routes / OpenAPI | openapi · reference |
| GraphQL exposure | graphql |
| registration / wiring | bootstrap |
| file layout / naming | service-layout |

## Final verify (the gate)

1. **Mechanical, pre-boot:** shape changed ⇒ `Version` bumped · every new 1:N leg FK
   indexed · grep the OLD projected-field names → no stale references (code, surfaces,
   tests) · no write-side file touched.
2. **`gofmt -l` + `go vet` + `go build`** (engine + transport tags) — clean.
3. **Boot** — the evolved view registers; probes green.
4. **Functional honesty:** the re-projection proves itself only after CDC flows — state
   what was verified vs what needs a write-and-wait round-trip.
5. **Regression** — the project's suite if it has one.
6. **Offer to run.** Ask ONE question: boot the app to click through the evolved read
   endpoints? Yes → delegate to `/omnicore:run` (never boot inline). No → done.

Leave `evolve-view/<view>/` in place for review.

## Re-entry — spec already exists

`Status: DRAFT` → reopen the gate with what's answered. `Status: APPROVED` → apply only
the not-yet-applied impact-map items, then re-verify. A changed answer reopens the spec.

## What this skill never does

No write-side edits (that's `evolve-entity` — delegated, not improvised), no framework
edits, no git, no shape change without its `Version` bump, no consumer-breaking change
that wasn't flagged in the approved spec.
