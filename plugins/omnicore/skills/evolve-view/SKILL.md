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

**Every document this run writes lands under `specs/`, and the project keeps it —
never add it to `.gitignore`** (`${CLAUDE_PLUGIN_ROOT}/shared/generated-documents.md`).

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
EmbedMany | EmbedInChild | Link | LinkMany | LinkInChild — the full set is the PIN's
`views` catalog, never this list) · legs/roles and join keys · projected shape + current `Version` · indexes
and options · collection name · **whether it is declared as a RELATIONAL read model**
(SoR-served — its own declaration TYPE, contributed through the relational feature seam,
carrying no `Version`, no indexes and no collection) · **whether its loader declares READ
JOINS** (a relational view inherits them and declares none itself, so a filterable field
may come from a traversal rather than from the schema — `shared/read-joins.md`) · **is
Mongo present in this project** (infra-free ⇒ a flip TO Mongo needs
it enabled first) · surfaces exposing it (REST, GraphQL) · known consumers in-repo · the
project's local flavor. For any change touching a segment's projected
fields or lifecycle, check whether it FLIPS the segment's ARCHIVE regime
(follow-the-source vs retain-regardless): that is a shape change like any other
(`Version` bump ⇒ rebuild) AND it changes what consumers SEE on default reads — call
it out explicitly in the impact map, per the pin's `views` archived rule.

## Phase 1 — Spec gate: `specs/evolve-view/<view>/spec.md`

`Status: DRAFT`, hard STOP until approved; `⚠️ OPEN` answered, never defaulted; sections
structural (`N/A — <why>`):

0. **Is this a view change at all?** If what the dev wants is "the listing should also
   show / filter by a field of ANOTHER aggregate", stop and check `shared/read-joins.md`
   first. On a relational read model that is a READ JOIN on the repository — no view
   edit, no `Version`, no rebuild — and proposing an Embed or a ComposedView for it is
   over-engineering a solved problem. On a Mongo-projected view it genuinely is a view
   change (Embed/Link/Composed) and this skill owns it.
1. **The change, in one paragraph** — restated.
2. **Impact map** — every artifact touched (view declaration, `Version`, indexes,
   surfaces, tests). The contract: Phase 2 edits nothing outside it.
3. **Shape change** [high-risk]: fields in/out, the `Version` bump, and the REBUILD
   consequence (the collection re-projects — on large data, say what that costs and
   when it's safe to run, per `mongo-schema-evolution` at the pin). **Views that EMBED
   this one via a `JoinView` leg are COUPLED by the rebuild hash** — the leg folds this
   view's `Version(n)` into the embedder's identity, so bumping here makes the
   forgot-to-bump guard fire ON THE EMBEDDER: an unconditional boot abort, not an
   option. There is no "bump the embedder later": list each embedding view found in
   Phase 0b, **bump it in the SAME change, deploy both together** (rebuilds
   auto-order source-first). And note an ad-hoc `RebuildView` of this view does NOT
   refresh its dependents — only the deploy-both path converges (`views` +
   `mongo-schema-evolution`). **The bump rule is not "anything changed":** index-only
   changes need NO bump (they sync as artifact-only drift), while options in the
   rebuild hash ($jsonSchema, collation, capped/time-series, `DeleteOnArchive`) DO —
   and collation is IMMUTABLE on an existing collection (divergence aborts boot,
   never auto-drops; Capped ⊕ TimeSeries). Exact lists: `mongo-schema-evolution` ·
   `auto-query-handlers`.
4. **New leg / role** [high-risk]: source, join key, the leg's COVERING index per the
   pin's per-kind law (`views` — read it, don't infer): the parent join column for a
   1:1 Embed (boot-fatal when missing), NO index on the declaring view for an EmbedMany
   (its ripple resolves by the child's own FK), `<childSegment>.<fk>`
   multikey for EmbedInChild — and for an EmbedMany/LinkMany over a `JoinView` leg the
   index belongs on the SOURCE view, not this one (`views` owns the per-kind law);
   whether the source view/entity needs anything first (→ delegation); **and the
   authorization consequence** — a new leg can expose, via join, data the caller's
   identity could not query directly; gating it is the dev's responsibility in
   `ToCriteria` / `crit.Restrict` (`query-side` · `authz-seams`) — raise it, never
   assume the existing gate still covers the widened shape.
   Adding a ROLE to a SharedBaseView: the role set is in the rebuild hash — the
   `Version(N)` bump is MANDATORY, forgetting it aborts boot (scaffold-entity's
   `conventions/sharedbase.md`, Read).
   **If the change crosses composition types** (a local view gaining an
   external/other-service source, or a conversion between kinds), reopen the FULL
   catalog in `views` at the pin and TEACH the implicated types + trade-offs
   (where the truth lives · how it refreshes · coupling/failure mode · cost) in the
   user's language BEFORE recommending — same teach-then-confirm doctrine as
   `scaffold-view`; a type change is a new consistency contract, never a detail.
4b. **Flipping the backing (relational ⇄ Mongo)** [high-risk] — **it is a CONVERSION
   between two different declaration TYPES, not a flag on one** (pin ≥ v0.57.0). The
   view is re-declared on the other seam; there is no marker to add or remove and no
   drift decision to ride on. Confirm the shape at `relational-view` before proposing,
   and put BOTH halves of each transition in the impact map:
   - **Mongo → relational**: declare the relational read model over the aggregate's
     existing loader and contribute it through the relational seam. Then **drop the
     collection and DELETE its `omnicore_mongo_views` row BY HAND, as part of this
     change.** Nothing does it for you any more — a relational read model never reaches
     the sync engine — and the leftovers are then reported as foreign by the
     DB-per-service guard: a warning under `dev`, **a boot abort in every other
     profile**. The registry row is the half that gets forgotten and it aborts the next
     boot for a different reason. Both statements are printed by the guard's own abort
     message; the plan states them anyway, because the plan is read BEFORE the boot
     fails. There is no frozen copy to fall back on: flipping BACK is a full online
     blue-green rebuild, costed like any other on a large collection, not a "resume".
   - **relational → Mongo**: declare the projected view with `Version(1)` and contribute
     it through the Mongo seam; the rebuild provisions the collection from the CURRENT
     SoR — zero-downtime, capturing every write made during the relational phase.
   Only a PLAIN single-aggregate view can flip — a Composed/Shared/Embed view is
   relational-ineligible (different type or boot fail), so this item is `N/A` for them.
   **Read joins survive the flip untouched in either direction** — they live on the
   repository — but their READ-side reach does not: going to Mongo, a joined field stops
   being filterable/sortable/served (a projection is composed from the `TableSchema`,
   which a join never enters), so if the listing filters by one, that filter is a
   consumer-visible LOSS and belongs under item 5. Going to relational, the reverse:
   the reach arrives for free.
   State the read-side consequence so the dev flips with open eyes (per
   `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` + `relational-view`): relational =
   read-your-writes but 1:1-reach filters only (root, sibling, shared base — plus any
   field a declared ROOT read join brought across), `?search=` and 1:N child filter/sort
   become typed 400s (`UnsupportedCapabilityNotification`), and pagination switches from stable keyset to a camouflaged
   OFFSET that can skip/repeat rows under concurrent writes — wire-compatible, so no
   grep will surface it; Mongo = full vocabulary, eventual. If the target is Mongo but
   the project is infra-free (no `mongo.uri`), flipping would abort the boot — don't
   refuse: offer the enablement via `/omnicore:configure`, then flip. **On SQLite that
   means the FULL conversion (Debezium-tailable engine + Mongo + broker + relay —
   `configure` does it in one pass): adding `mongo.uri` alone gives a one-time
   backfill that NOTHING ever updates again (no CDC source) — a silently stale view,
   worse than the refusal.** Reversible, no code lost.
5. **Consumer impact** [high-risk]: wire-visible removals/renames on read responses are
   BREAKING for consumers — list them, flag them, the dev decides; never silent.
6. **Surfaces** — endpoints/GraphQL/gRPC changes; filter operators per new field
   (low-risk; vocabulary + list-DTO allowlist: `query-side` · `auto-query-handlers`).

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
| projected shape / `Version` / rebuild (the FULL rebuild-hash list — incl. embedded-view coupling — is in `views`) | views · mongo-schema-evolution · auto-query-handlers |
| flipping backing: relational ⇄ Mongo (drift, rebuild) | relational-view · mongo-schema-evolution |
| read-side posture / what each backing serves & asks | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) |
| reaching ANOTHER aggregate from a query — read joins (repository-declared), and the rule-vs-wire split | `${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md` (owner) · read-joins for version-exact contract |
| composition contracts / legs / roles | views |
| custom projection / response shaping | custom-query-handler |
| indexes / options (index-only = no bump; hash-moving options = bump; collation immutable) | auto-query-handlers · mongo-schema-evolution |
| read authorization (`ToCriteria` / `Restrict`) | query-side · authz-seams |
| REST routes / OpenAPI | openapi · reference |
| GraphQL exposure | graphql |
| gRPC exposure | grpc |
| registration / wiring (a ComposedView registers via `ComposingFeature.ComposedViews()` — in `views`, not `bootstrap`) | bootstrap · views |
| file layout / naming | service-layout |

## Final verify (the gate)

0. **Reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`) — walk the
   plan's own promises item by item with evidence; an unmet target is RED or an explicit
   dev-accepted deviation.
1. **Mechanical, pre-boot:** shape changed ⇒ `Version` bumped **and every `JoinView`
   embedder of this view bumped in the same change** (spec item 3) · every new leg's
   covering index declared per the per-kind law (1:1 Embed yes, EmbedMany none on the
   declaring view, EmbedInChild multikey; incl. the SOURCE-view index for
   EmbedMany/LinkMany over a JoinView leg) · grep the OLD projected-field names → no
   stale references (code, surfaces, tests) · no write-side file touched.
2. **`gofmt -l` + `go vet` + `go build`** (engine + transport tags) — clean.
3. **Boot** — the evolved view registers; probes green. **Know what a healthy
   post-bump boot looks like:** under any profile but dev `mongo.rebuild.autoRun`
   defaults to `check`, where a pending rebuild ABORTS boot with a diagnostic on
   purpose (run the rebuild per the pin — that is the design, not a bug you fix);
   and during the rebuild `/livez` is 200 while `/readyz` stays 503 naming the
   view — wait, not a failure (`mongo-schema-evolution`).
4. **Functional honesty:** the re-projection proves itself only after CDC flows — state
   what was verified vs what needs a write-and-wait round-trip.
5. **Regression** — the project's suite if it has one.
6. **Offer to run.** Ask ONE question: boot the app to click through the evolved read
   endpoints? Yes → delegate to `/omnicore:run` (never boot inline). No → done.

Leave `specs/evolve-view/<view>/` in place for review.

## Re-entry — spec already exists

`Status: DRAFT` → reopen the gate with what's answered. `Status: APPROVED` → apply only
the not-yet-applied impact-map items, then re-verify. A changed answer reopens the spec.

## What this skill never does

No write-side edits (that's `evolve-entity` — delegated, not improvised), no framework
edits, no git, no shape change without its `Version` bump, no consumer-breaking change
that wasn't flagged in the approved spec.
