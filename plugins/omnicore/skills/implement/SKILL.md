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
- **Docs-first, version-agnostic — same anti-drift doctrine as every omnicore skill.**
  This skill carries NO code; every code shape composes from the routed sections at the
  pin. Never assume or stamp a framework version.
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
   with no section behind it is a defect.
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
