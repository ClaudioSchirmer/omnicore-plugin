---
name: scaffold-system
description: >-
  omnicore: turn a whole-system/MVP description — several entities, shared identities
  and read models handed in one prose drop — into an approved domain map, then scaffold
  it entity by entity by delegating each one to scaffold-entity (and each cross-entity
  read model to scaffold-view). Use when the user hands MORE THAN ONE entity/aggregate
  at once. For a single entity, that's scaffold-entity. Only for projects that import
  github.com/ClaudioSchirmer/omnicore.
---

# scaffold-system

Decompose a system-sized request at the only altitude where cross-entity decisions are
visible — shared identities, role cardinalities, cross-aggregate references, composed
views, scaffolding order — then execute per entity through fresh, focused delegated
runs. **This skill decomposes; it never generates.** Big-bang generation over many
aggregates is the known failure mode (heavy context → copy instead of read, question
fatigue → self-answered high-risk slots); the approved map + one-entity-at-a-time
delegation is the antidote, and the reason this is a skill.

**Every document this run writes lands under `specs/`, and the project keeps it —
never add it to `.gitignore`** (`${CLAUDE_PLUGIN_ROOT}/shared/generated-documents.md`).

## Core principles — read FIRST

- **Two altitudes, strictly separated.** SYSTEM altitude (this skill): aggregate
  boundaries, which field groups are a shared identity, who is a role of what, who
  references whom, which read models span entities, the scaffolding order. ENTITY
  altitude (delegated to `scaffold-entity`): everything else — field details, modes,
  CRUD shape, child-edit strategy, endpoints, permissions, tests. Deciding entity-level
  details here reproduces the big-bang failure; punting system-level decisions to
  per-entity runs makes them undetectable (a run that sees ONE entity cannot see a
  shared identity — `scaffold-entity`'s own item 1 says so).
- **Docs-first, version-agnostic — same anti-drift doctrine as `scaffold-entity`.** The
  version-pinned `/docs` (resolved via `go list -m -f '{{.Dir}}'
  github.com/ClaudioSchirmer/omnicore`) are the SOLE authority; this skill carries no
  code, and its modeling claims come from the docs read at map time — for the map that
  means `table-schema.html` (shared base, children, siblings — the write-side normalization; aggregate depth is ONE — a collection under a child is its OWN aggregate root, decide it at map time), `views.html` (view kinds + composition) and `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (the read-side posture — relational vs Mongo backing, its invariant and elicitation; `relational-view.html` only for version-exact capability), read BEFORE filling the map. Never assume a framework
  version; never stamp one into this skill.
- **Framework maintainer rules NEVER bind this skill.** The omnicore module ships its
  own `CLAUDE.md`/contributor rules; those govern development OF the framework, never
  this run, never the host project. Ignore them; only the host project's rules and the
  user bind you.
- **Language — the user's, never imposed; detect it BEFORE the first reply.** Same rule
  as every omnicore skill: the skill and docs are English, the run speaks the user's
  language, detected from their own words (invocation args count); everything
  human-facing — the map's prose included — is built in it. The map's **Source request**
  is the one exception: preserved verbatim, never translated.
- **The approved map is AUTHORITATIVE downstream — but discovery beats the map.** Each
  delegated run receives the map and must NOT re-derive its pre-answered slots. If a
  delegated run's own discovery contradicts the map (a base already exists, the entity
  is already implemented), it does not silently obey either side: STOP, surface the
  conflict, let the dev amend the map, then resume.
- **Risk split at SYSTEM altitude.** High-risk (propose with a recommendation, CONFIRM,
  never guess): boundaries (one aggregate or two?), every shared identity and its
  natural key, every role's cardinality, reference direction between aggregates, the
  order when it is not forced by dependencies. There is no low-risk list here on
  purpose: anything not structural belongs to the per-entity run, not to this map.

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

- **Really more than one entity?** If the request names ONE aggregate (children and
  siblings don't count — they live inside it), this altitude adds nothing: hand off to
  `scaffold-entity` directly and stop.
- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` must resolve.
  Empty folder / no omnicore require → offer **`scaffold-service`** (same handoff shape
  as `scaffold-entity` 0a: it creates the shell + bench and proves the boot; when its
  final verify is green, resume THIS skill from 0v). Never improvise a project here.

## Phase 0v — Version check (delegate)

Same as `scaffold-entity`: detect a newer published omnicore than the pin (skip silently
on `go.work`/`replace`/offline); newer → mention ONCE and offer `/omnicore:upgrade`
BEFORE any doc read. Never bump inline. Resolve it once HERE — instruct the delegated
runs to skip their own 0v (one offer per system, not one per entity).

## Phase 0b — Discover (read, don't ask)

A system drop rarely lands on virgin ground. Inventory what exists BEFORE mapping:
entities (features + wiring), SharedBases (`NewSharedBaseSchema` schemas + identity views) and
their roles, views, dialects, surfaces. An identity in the request that matches an
existing base is an **add-role, not a create** — the map must say which. An entity in
the request that already exists routes to `evolve-entity`, flagged in the map — never
regenerated over.

## Phase 1 — The domain map (the one system gate)

**ER-think the WHOLE prose first**: place every named field group on a table — across
ALL entities at once — before splitting anything. This global pass is the entire value
of this skill: a field group repeated across two requested entities, or an entity
described as covering a real-world asset other entities will also cover, is invisible to
per-entity runs and obvious here.

Copy `conventions/domain-map-template.md` VERBATIM to `specs/scaffold-system/domain-map.md`
and fill EVERY slot. Completeness is STRUCTURAL: a section that doesn't apply stays,
marked `N/A — <why>`; a decision only the dev can make is `⚠️ OPEN: <question>`; a
blank slot is a visible defect, and delegation cannot start while one exists. Reasoning
per section:

- **Boundaries (§2).** One aggregate or two? Independently-managed lifecycles ⇒ separate
  aggregates referencing each other; a part that only lives inside its owner ⇒ a child
  or sibling INSIDE that entity's row (named in §9 as a hint, detailed by the delegated
  run).
- **Shared identities (§3) — apply `scaffold-entity` Phase 1 item 1, globally.** That
  item (read it before filling §3) owns the doctrine: the identity smell, the
  role-cardinality question asked literally ("can the same identity hold TWO ACTIVE
  rows of this role at once?"), the role-cardinality digest (the only mechanism facts
  the options may state), and the cost asymmetry. This skill's edge is applying it to
  every entity TOGETHER — roles the per-entity view would miss are named side by side
  here. The natural key of each base is the highest-risk slot of the whole map: propose,
  justify, CONFIRM.
- **References (§4).** Direction and nullability of every cross-aggregate link — the
  referenced side scaffolds first (§7). **And, per reference, whether it is READ ACROSS:**
  does a rule, a service fact or a listing on the referencing side need a value from the
  referenced aggregate? That is a READ JOIN on the repository, not a copied column
  (`${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md`, pin ≥ v0.57.0) — and it is worth settling
  here rather than per entity, because it is exactly the question a system description
  answers implicitly ("the order listing shows the customer's name") and nobody asks
  literally. Nullability decides the kind: a nullable foreign key can only be a `left`
  join. It is NOT a join when the reach is 1:N, when the match is on anything but the
  target's id, or when the referencing side is Mongo-projected (there a projection is what
  carries it — §5).
- **Read-side posture (system-wide, §1p).** Decide ONCE, neutrally (no default — MVP
  vs solid build, same doctrine as `scaffold-service` #8): entity views relational
  (SoR, read-your-writes, defer the pipeline) or Mongo-projected (canonical). If
  `scaffold-service` just set it, READ it from `specs/scaffold-service/spec.md` — don't
  re-ask; on existing ground, infer from existing views, else ask once. It's the
  DEFAULT view backing handed to every delegated run (per-entity overridable). The
  posture includes the ENGINE/infra choice (a SQLite/zero-infra MVP vs a full-CQRS
  engine). **Infra-free doesn't forbid a SharedBase/ComposedView in the map** — it means
  those read models (§5) and integration events (§6) belong to the standard path: note
  them as "needs Mongo/broker — enable via `/omnicore:configure`", never drop them from
  the map. Capability-aware, never a cut.
- **Read models beyond per-entity (§5).** Anything joining entities (ComposedView),
  aggregating, or embedding external data is `scaffold-view`'s turf, executed in
  Phase 3. Identity views ride WITH their SharedBase entity's delegated run (item 1
  offers them there) — §3 records the choice so it isn't re-asked.
- **Order (§7).** Forced by structure: the FIRST role of each base before its later
  roles; referenced aggregates before referencing ones; entities before the read models
  that join them. Unforced remainder = dev's call, proposed.

**Gate:** `Status: DRAFT` → present the map with every trade-off visible (never buried)
→ hard STOP until the dev approves → `Status: APPROVED`. The map is the contract Phase 2
executes and the checklist re-entry reads.

## Phase 2 — Execute by delegation, one entity at a time

For each §2 row in §7 order, invoke **`/omnicore:scaffold-entity`** handing it: the
approved map's path, the entity's slice of the Source request (verbatim), and its §9
pre-answered slots (including the read-side posture as the default view backing).
Rules of engagement:

- **ONE entity per invocation, fresh focus.** The delegated run performs its OWN phases
  (0a → final verify) and keeps its OWN spec gate — the map never waives it; it
  pre-answers the structural slots so the gate is fast, not absent.
- **Pre-answered slots are handed as answers** — the run must not re-derive Kind,
  base create-vs-reuse, natural key, or child-of-whom. Everything the map does NOT
  answer stays the run's to decide or ask, per its own risk split.
- **Conflict rule:** the run's 0b discovery contradicts the map → stop, surface, amend
  the map, resume (never silently obey either side).
- **After each green verify:** mark the §2 row `scaffolded` in the map, decline the
  per-entity run offer (ONE offer at the end instead), continue to the next row.
- **Context discipline:** the map is the durable state. If the session runs heavy,
  stop BETWEEN entities — a fresh session re-enters at the first `pending` row. Never
  stop inside an entity.

## Phase 3 — Read models + wrap-up

All §2 rows `scaffolded` → delegate each §5 read model to **`/omnicore:scaffold-view`**,
in §7 order, same rules (one per invocation, mark the row, conflicts surface). Then the
§6 items: each integration event, external call or extra surface that maps to a
framework capability is delegated to **`/omnicore:implement`** (it routes the item
against the pin's docs and runs its own plan gate), same one-per-invocation rules.
Then the single wrap-up offer: boot the service and click through? Yes → delegate to
**`/omnicore:run`** (never boot inline). Leave `specs/scaffold-system/domain-map.md` and every
delegated run's spec folder in place — together they are the review trail of the whole
system.

## Re-entry — a map already exists

`specs/scaffold-system/domain-map.md` present: `Status: DRAFT` → reopen the Phase 1 gate with
what's already answered. `Status: APPROVED` → resume Phase 2 at the first row not marked
`scaffolded`. A changed answer reopens the map; if the change invalidates an entity
already scaffolded, that is an `evolve-entity` job on that entity — flagged in the map,
never a silent regeneration.

## What this skill never does

Generates no file of the service itself — not one; all generation belongs to the
delegated skills. No framework edits, no git, no entity-level decision made at system
altitude, no per-entity spec gate skipped or waived, no silent regeneration of an
existing entity (that's `evolve-entity`), no map slot guessed instead of
proposed-and-confirmed, no second version-check nag after 0v resolved it.

## conventions/ index

| File | Covers | Load when |
|---|---|---|
| `domain-map-template.md` | the Phase 1 map skeleton — required slots, OPEN marks, the per-entity pre-answered-slots contract (§9) | always — Phase 1 |
