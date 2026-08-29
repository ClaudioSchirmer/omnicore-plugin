---
name: implement
description: >-
  omnicore: wire a framework capability into an existing omnicore-based service — expose
  entities on another surface (gRPC, GraphQL), call an external API from a handler
  (httpclient + middleware), cache a query, publish/consume integration events, lifecycle
  hooks, authz, tracing/audit, resilience — anything the PINNED framework offers that no
  dedicated skill owns. Docs-first: it routes the request against the pin's Documentation
  Map; if the framework doesn't offer it, it says so honestly (and checks newer releases).
  Use when the user asks to add/integrate/enable a capability or an external integration
  on an existing service. Only for projects that import
  github.com/ClaudioSchirmer/omnicore.
---

# implement

The dedicated skills own the entity/view/service lifecycles; real requests are just as
often a **capability**: "expose customers over gRPC", "validate the tax-id against the
bank's API inside the create handler", "cache this query", "emit an event when the
contract is signed". What varies per capability is NOT in any skill — it is in the
pinned `/docs`. So this skill knows only two things: the **standard rituals** (preflight
→ discovery → plan gate → doc-read-before-artifact → verify → run offer) and **how to
discover what the framework offers** — dynamic routing against the pin's documentation
instead of a static change→section table. That is why one generic skill works and why it
grows with the framework for free: a new release documents a new capability, and this
skill can already plan it.

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

- **The pin's docs ARE the capability catalog.** Resolve `<omnicore-dir>` = `go list -m
  -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`. Route the request dynamically: the
  Documentation Map in `<omnicore-dir>/CLAUDE.md` is the index; `features.html` /
  `reference.html` are the existence check; the owning section(s) are the authority the
  plan is built FROM. **A capability claim with no doc section behind it does not enter
  the plan** — never assert from memory what the framework offers, in either direction
  (this rule exists because parametric answers about framework mechanics have been wrong
  before; the doc read is what fixes them).
- **Honest no.** If the pinned docs don't offer it: say so plainly, then (a) check the
  newest release's `changelog.html` — offered there → the official path is
  `/omnicore:upgrade` first, offered as an option, never forced; (b) otherwise name the
  closest legitimate path (plain code in the service, an external component, a framework
  feature request) — never a semantically-off workaround, and never reimplement inside
  the service something the framework DOES provide (the doc wins over the urge to
  hand-roll). Distinguish TWO nos: (a) the framework doesn't offer it → the honest no
  above; (b) the framework DOES offer it but the current infra posture lacks what it
  needs (e.g. integration-event publish with no broker/relay, or anything Mongo on an
  infra-free / SQLite project) → NOT a no: name the infra it needs and OFFER to enable it
  via `/omnicore:configure` (delegate, or point the dev at it), then continue. Never
  refuse a capability the framework has — offer the conversion. Reversible, no code lost.
- **Fallback router — dedicated skills own their turf; detect and hand off FIRST.** New
  entity → `scaffold-entity` · several entities/an MVP → `scaffold-system` · change to
  an existing entity's write side → `evolve-entity` · new/changed read model →
  `scaffold-view`/`evolve-view` · removal → `remove-entity` · no service yet →
  `scaffold-service` · version bump → `upgrade` · infra/posture change (add Mongo/broker,
  swap engine, enable the infra a capability needs) → `configure` · "prove the service
  works" / contract tests / e2e suite → `qa` · "it should already
  work but doesn't" →
  `doctor` (diagnose before implementing — a missing capability and a broken one look
  alike) · "how does it work?" → `help`. This skill takes what none of them claims. A
  mixed request (an entity AND a capability) is sequenced: the owner skill first, this
  skill after — say the sequence, don't interleave.
  **The row that is easy to miss: a NEW PERSISTED RESOURCE whose rules are not the
  aggregate's** — "a couple of endpoints with a table behind them, the logic is in the
  handler". **Ask which DOOR first, because there are now two and the wrong one is
  invisible until much later:**
  `${CLAUDE_PLUGIN_ROOT}/shared/direct-schema.md` is the owner. A table that is LISTED
  through the read side, audited, projected, event-raising or lifecycle-driven goes through
  an aggregate — the write side of it is an entity (`scaffold-entity` owns the domain
  struct, the `TableSchema`, the migration, the repository, the bootstrap feature), this
  skill takes the handler-side logic AFTER it, and skipping the domain artifact is not an
  option: the write-backed schema is type-anchored, so the shape is a domain struct with an
  EMPTY `BuildRules`, whose rejections are application notifications
  (`${CLAUDE_PLUGIN_ROOT}/shared/notification-bases.md`). A table with **no aggregate behind
  it at all** — a control table, a job queue, a lookup, an idempotency ledger, something
  nothing lists or audits — is a **Direct schema** on a pin that carries one, and then there
  is no entity, no `scaffold-entity` leg and no domain struct: the row is a storage shape in
  `internal/infra/` and this skill owns the whole thing. Say which door and WHY, name the
  guarantees the Direct one does not carry (no outbox → no view, ever; no audit; no revision
  guard), and let the dev confirm the scope before anything runs.
- **Docs-first, version-agnostic — same anti-drift doctrine as every omnicore skill.**
  This skill carries NO code; every code shape composes from the routed sections at the
  pin. Never assume or stamp a framework version.
- **A scalar question is not a list load.** Report totals, counts, existence checks,
  per-key breakdowns and "is this taken" land in this skill by design (`scaffold-view`
  and `scaffold-system` route them here — they are the write-side aggregate DSL, not a
  view kind). Before writing any of it, read
  `${CLAUDE_PLUGIN_ROOT}/shared/query-primitives.md` — the owner of WHICH primitive
  answers which question. The recurring failure is reaching for the list load and folding
  the answer in Go; the framework has a hydration-free primitive for every one of these,
  on the same criteria surface, and one of them is a correctness trap rather than merely
  a slow one. The second question, when the truth is in a table this service has no
  aggregate for — another aggregate's CHILD table, a control table, a lookup — is which
  ANCHOR the primitive hangs off: `${CLAUDE_PLUGIN_ROOT}/shared/direct-schema.md`. Neither
  "write the SQL by hand" nor "declare a whole aggregate so the question can be asked" is
  the answer on a pin that carries the Direct door.
- **"This rule/service/handler needs a field from ANOTHER aggregate" is not an
  integration.** Before proposing a second `FindOne` inside a rule, a denormalized column
  kept in step by the write path, or a call out to another service, check
  `${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md` (pin ≥ v0.57.0): when this entity already
  holds a foreign key to that aggregate, a READ JOIN declared on the repository makes the
  value an ordinary field of the entity, filled on every load, with no copy to synchronize
  and no extra round trip. Two decisions follow, and they are separate: which columns it
  brings across, and whether the caller RECEIVES them or the field exists for the rules
  alone. The boundaries — 1:1 only, the target's id only, one hop, no collections — are in
  that file; where one is crossed, say which, and route to the honest alternative rather
  than approximating it with a join.
- **Where the artifacts LAND is not improvised — and a rejection belongs to the layer that
  RAISES it.** This skill writes into an existing service, so its files follow the pin's
  `service-layout.html` exactly like every other skill's — that page is NORMATIVE for a
  generator, and it covers this skill's output too (a hand-written `pipeline.Handler` goes
  under `application/commands/handlers/` or `queries/handlers/`, never at the
  `application/` root; an outbound adapter under `infra/external/`). The placement this
  skill gets wrong on its own is the NOTIFICATION: when the handler is what validates,
  the rejection is an APPLICATION notification and is declared in `application/` beside
  it — not appended to `internal/domain/notifications.go` because that is where the
  entity's notifications already live. **Read
  `${CLAUDE_PLUGIN_ROOT}/shared/notification-bases.md` (the owner) before declaring any
  notification type**; it also settles the question that rides along with it — a persisted
  table still needs its domain struct even when that struct's `BuildRules` is empty, so
  "this endpoint has no domain layer" is never the conclusion.
- **A capability's SHARED CONTRACT does not land in `internal/domain` — read
  `${CLAUDE_PLUGIN_ROOT}/shared/domain-membership.md` (the owner) before declaring any type,
  port, interface or constant there.** This skill is where the pressure is highest, because
  a capability arrives as a mechanism that several layers touch — a hasher, a token issuer,
  a clock, an outbound gateway, a protocol's claim/header names — and the domain package is
  the one every layer may import. That property is exactly what makes it the wrong home: the
  pin's rule is that **the interface stays with its CONSUMER, never with its
  implementation**, so a contract the handler calls is declared in `internal/application/`
  beside that handler and implemented under `internal/infra/`. Three reasons that feel like
  reasons and are not — "it is the only package everyone can import without a cycle",
  "there is already something similar in there", "a future endpoint will need it" — are
  refuted by name in that file; do not re-derive them. The plugin's write-time guard refuses
  the two decidable cases outright, so a blocked edit here is the rule firing, not a bug to
  route around.
- **Framework maintainer rules NEVER bind this skill.** The module's own
  `CLAUDE.md`/contributor rules govern development OF the framework — ignore them; only
  the host project's rules and the user bind you.
- **Language — the user's, never imposed; detect it BEFORE the first reply.** Same rule
  as every omnicore skill; everything human-facing is built in it.
- **Risk split.** High-risk = the INTEGRATION SEMANTICS: which seam (in-transaction
  lifecycle hook vs after-commit event vs middleware), sync-in-handler vs async, the
  failure policy (external API down → reject the command? degrade? queue?), idempotency,
  timeouts/retries, anything wire-visible (a new surface is a new public contract),
  secrets and config. **Propose with a recommendation and CONFIRM; never guess.**
  Low-risk = details (config key names, sensible timeouts proposed from the docs'
  defaults) — decide them well, don't ask.
- **External systems get no invented contracts.** The externa API's shape (routes,
  payloads, auth) comes from the dev or their spec — ask for it; never fabricate it.
  Credentials/URLs go through the config profiles per the docs' configuration reference,
  NEVER hardcoded.

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

## Phase 0a — Preflight + routing gate

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves — else
  offer `scaffold-service` (standard handoff; resume here when green).
- **Really this skill's job?** Walk the fallback-router table above BEFORE anything
  else; on a match, hand off to the owner. When in doubt between "capability" and
  "entity change", the question is what the diff touches: schema/DTO/translation
  lockstep → `evolve-entity`; wiring, config, surfaces, middleware, events → here.

## Phase 0v — Version check (delegate)

Same as `scaffold-entity`: newer published pin → mention ONCE and offer
`/omnicore:upgrade` BEFORE any doc read; skip silently on `go.work`/`replace`/offline.
Never bump inline. (Phase 1's routing may ALSO surface upgrade as the path to a
capability the pin lacks — that offer goes through the same skill, same rules.)

## Phase 0b — Discover (read, don't ask)

- The **target** of the request: which entities/handlers/queries/views it names, and
  their current wiring (features, bootstrap, routes/surfaces).
- What is **already enabled**: surfaces wired (REST/GraphQL/gRPC), cache, tracing,
  audit, broker/transport, existing httpclient integrations and middleware — an
  already-wired capability turns "add X" into "extend X" (mirror its local flavor).
- The **config profiles** (`microservice.*.yaml`): dialects, transport, existing keys —
  new config must land in EVERY profile that boots, not just one.

## Phase 1 — Route → read → plan (the gate)

1. **Route.** Map the request onto the pin's docs: Documentation Map → candidate
   section(s); `features.html`/`reference.html` confirm the capability exists at this
   pin. Three outcomes: **offered** → continue · **offered in a newer release only**
   (its `changelog.html` says so) → present honestly, offer `/omnicore:upgrade`, and
   only continue on the new pin if accepted · **offered by the framework but the current
   posture lacks its infra** (broker/relay/Mongo absent, e.g. an infra-free / SQLite
   project) → offer `/omnicore:configure` to enable it, then continue · **not offered** →
   honest no + closest legitimate path, and STOP unless the dev redirects.
   **Before any of that, consult `${CLAUDE_PLUGIN_ROOT}/shared/capabilities.md` (the
   owner) for three gates:** (1) is it ALREADY AUTOMATIC (audit, domain events) — then
   the answer is "already on", not a wiring plan; (2) the availability matrix BOTH ways
   — most capabilities (httpclient, cache, gRPC/GraphQL, authz, tracing…) work on every
   posture, SQLite included: never route to `configure` what isn't actually gated;
   (3) for cross-service asks, the integration-style decision matrix — including the
   doctrine that there is NO cross-service command (design the event + receiver, never
   an imperative RPC between services). Examples of routings (examples
   only — the map at the pin is always the authority, sections may differ per version):
   another transport surface → `grpc` / `graphql` · outbound HTTP + resilience →
   `httpclient` (its middleware chain is a heading INSIDE it, not a separate section) ·
   cross-service events → `integration-events` · in-flow custom logic →
   `lifecycle-hooks` · read-side caching → `cache-subsystem` · permissions/tenancy →
   `authz-seams` (there is no section named `authz`) · observability →
   `tracing`/`audit` — exact names, never derived from the concept's wording.
2. **Read the owning section(s) BEFORE planning.** The plan cites them; a plan line
   with no section behind it is a defect. **When the capability creates FILES, read
   `service-layout.html` at the pin too** — it is the authority for where each one lands
   and what it is named, and §5's placement row is filled from it. If the plan declares a
   notification type, `${CLAUDE_PLUGIN_ROOT}/shared/notification-bases.md` decides its
   layer before the section decides its shape.
3. **Fill the plan.** Copy `conventions/plan-template.md` VERBATIM to
   `specs/implement/<slug>/plan.md` and fill every slot (structural completeness: `N/A —
   <why>` stays, `⚠️ OPEN: <question>` for what only the dev knows — the failure
   policy of an external call is ALWAYS the dev's call unless they already said it).
   High-risk slots carry `(proposed)` + alternatives, visible, never buried.

**Gate:** `Status: DRAFT` → present the plan → hard STOP until approved → `APPROVED`.

## Phase 2 — Execute the impact map

Dependency order (config → wiring/bootstrap → the capability's artifacts → tests),
re-reading the owning section before each artifact it governs. Edit ONLY what the
plan's impact map lists. Config lands in every boot profile; secrets via env
placeholders per the configuration reference, never literal. **New yaml blocks and
routes land on the boot contract's failure surface** — several blocks are
strict-decoded (unknown key = boot abort) and `auth.publicRoutes` is validated
exact-match (`${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md` — read it when the plan
touches either). **And enabling messaging changes the BUILD:** a new integration
consumer / upstream subscription needs the transport build tag or it dies at the
point of use on a green service — the impact map's build/run-commands row (plan §5)
is executed here like any other artifact, never assumed.

## Final verify (the gate)

0. **Reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`) — item 3
   below IS this contract for capabilities; the contract adds: any other stated target
   unmet is RED or an explicit dev-accepted deviation.
1. **Build + vet** — clean, with the plan's TARGET tag set: when the capability
   changed the required tags (a first consumer on a previously transport-less build),
   verifying with the OLD tags proves nothing — the §5 build/run row names the set.
2. **Tests** — the plan's new branches covered; never weaken an existing test to pass.
3. **Capability proof — the plan's own verify step, executed.** Each capability states
   in the plan how it will be PROVEN, not assumed: a surface answers a real call, a
   cache shows a hit on repeat, an event lands on the broker, an external call is
   exercised against the sandbox/mock the dev named. What cannot be proven locally (no
   sandbox for the external API) is reported honestly as unverified — with the exact
   step the dev must run to close it.
4. **Offer to run**: one question; yes → delegate to `/omnicore:run` (never boot
   inline).

Leave `specs/implement/<slug>/` in place — the plan is the review trail.

## Re-entry — a plan already exists

`Status: DRAFT` → reopen the gate with what's answered. `APPROVED` → apply only the
not-yet-applied impact-map items, then re-verify. A changed answer reopens the plan.

## What this skill never does

No framework edits, no git, no capability claim without a doc section behind it, no
invented external-API contract, no hardcoded secret, no reimplementation of what the
framework offers, no semantically-off workaround to dodge an upgrade or a breaking
change (cost it honestly instead), no turf-grab from a dedicated skill, no test edited
to pass, nothing outside the approved impact map.

## conventions/ index

| File | Covers | Load when |
|---|---|---|
| `plan-template.md` | the Phase 1 plan skeleton — routing evidence, integration semantics, impact map, config/secrets, verify step | always — Phase 1 |
