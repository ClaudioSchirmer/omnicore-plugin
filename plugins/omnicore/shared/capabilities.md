# shared/capabilities.md — what the framework offers, when, and how to choose

The single home for **capability availability and the choice rules between them** —
what works under which infra posture, which integration style fits which need, what is
already automatic. Skills that wire, offer, or diagnose capabilities route HERE — one
owner, no drift. No code, by design; exact APIs, middleware lists and option names are
the PIN's docs — if this file ever disagrees with them, the docs win.

## Availability under the infra posture — three independent axes, BOTH directions

The posture is not one switch but THREE independent opt-outs (`read-side.md` owns
the view side): **Mongo** (document read side), **broker + transport build tag**
(messaging), **CDC relay over a tailable engine** (change capture). Legal postures
include Mongo-with-no-broker and broker-with-no-Mongo — gate by the axis the
capability actually needs; never refuse what the posture doesn't gate:

- **Needs Mongo + CDC relay** (absent on infra-free / SQLite): Mongo-projected views
  and every multi-source view KIND.
- **Integration events — the two halves gate DIFFERENTLY.** PUBLISH rides the outbox
  + CDC relay, so it needs a tailable engine (absent on SQLite). SUBSCRIBE needs only
  a broker + the transport build tag — **no relay, no Mongo, no tailable engine**: a
  service can react to another service's events on an otherwise infra-free posture
  the moment a broker and the tag exist. Refusing "subscribe" on a Mongo-less
  project is a wrong refusal.
- **UpstreamSubscription** — broker + transport tag + Mongo (the mirror is a
  projected collection); the LOCAL relay plays no part in it.
- **Works EVERYWHERE, SQLite included** — offer freely: httpclient (+ its middleware),
  cache (both slots), gRPC and GraphQL surfaces, exports, auth/authz, audit, tracing,
  lifecycle hooks, domain events (in-process), the whole write side, and the **Direct
  schema/repository** door (one table, no aggregate — gated by the PIN and by nothing in
  the posture; `direct-schema.md` owns it). The mirror-image failure — refusing an
  available capability "because it's an MVP" — is as wrong as offering an unavailable one.
  - The one posture-shaped consequence to state whenever Direct is offered: a Direct write
    emits **no outbox row**, so it never feeds a Mongo-projected view on ANY posture. That
    is a property of the door, not of the infra — adding Mongo does not change it.
- **The transport BUILD TAG is part of enabling messaging.** A consumer or upstream
  subscription wired into a service that builds without the tag passes every
  compile-and-boot gate and dies at the point of use — an enable plan must carry the
  new build/run commands, not just yaml (`boot-contract.md`, Build tags).

## Service-to-service — the decision matrix

- **Sync call, internal service → gRPC** (the default for east-west traffic). Two
  distinct halves, both available everywhere: the inbound **gRPC surface** (the
  service serves) and the outbound **`grpcclient` toolbox** (`Deps.GRPCClient`,
  retry/breaker/backoff cores shared with httpclient) — "call service X over gRPC"
  is the second, not the first.
- **Sync call, external/third-party API → httpclient** (with its middleware chain).
- **Facts & reactions, async → integration events** (broker-carried, at-least-once).
- **"Read another service's state inside MY queries" → UpstreamSubscription** — NOT a
  call: the data becomes local, projected and queryable (needs Mongo/CDC, see above).
- **There is NO cross-service COMMAND, by design.** A service never tells another what
  to do over a command endpoint — it publishes the FACT (event); the imperative verb
  is the consumer's internal detail. An agent asked to "make service A trigger B"
  designs an event + a receiver, not an RPC command.

## Integration events — the contracts that bite

- Delivery is **at-least-once**: every Receiver handler MUST be designed idempotent —
  the double-invoke window is documented, the named strategies are in
  `integration-events` at the pin. Eliciting the idempotency strategy is part of the
  design question, not an afterthought.
- **Consumer groups**: same group → ONE reaction total; distinct groups → one EACH;
  replicas of one group SPLIT, never duplicate. `startFrom` (enum-validated) takes
  effect when the group has no committed offset — an existing group keeps its position
  (platform semantics; the pin's docs state the enum + defaults). "Fires twice / never
  fires" triage starts here.
- A declared subscribe with no registered receiver ABORTS boot — and the inverse too.

## Cache — two slots, one mandatory question

`Deps.Cache` (per-service, private) vs `Deps.SharedCache` (cross-service, shared) —
two axes: slot × backend. `cache.shared.store: memory` is a BOOT REJECT (a shared
cache can't be process-local). Omitting the cache block entirely = nil degradation,
zero cost no-op. "Cache this" (a query, a computed result) therefore carries one
elicitation: private or shared, and which backend — never silently pick. One carve-out:
the **httpclient response cache** uses `Deps.Cache` (private) unconditionally — there
is no slot to elicit for it, only "declare the `cache:` block or the layer bypasses".

## Already automatic — check BEFORE wiring

Things `implement` must NOT "wire" because they're already on:
- **Audit** — zero handler code; it travels with the persister. An ABSENT `audit`
  yaml block = BOTH destinations active; `[]` disables; unknown token aborts boot.
- **Domain events** — `RegisterEvent` auto-publishes post-commit on both Auto and
  manual paths (Slog publisher by default; swap the publisher, don't re-plumb).
- **Structured JSON logs** — the always-on stdout channel, zero framework config
  ("add JSON logging" is already done).
- **Probes** — `/livez` and `/readyz` are framework-registered on every service
  (`boot-contract.md` owns their semantics).
- **Per-request correlation** — the `AppContext` UUID rides every request already;
  "add request ids" is a where-to-see-it answer.
- The honest answer to "add X" is sometimes "X is already on — here's where to see
  it". Check this list first.

## Owning docs sections — exact names, never guess

The pin's section filenames are deliberately asymmetric; a fetch that 404s means the
name was a guess. The owners for this sheet's areas: integration events →
`integration-events` · broker/relay/consumer mechanics → `transport` · s2s decision
detail → `service-to-service` · outbound HTTP → `httpclient` (its middleware chain is
a heading inside it, not a separate section) · outbound gRPC → `grpc` (client half) ·
inbound surfaces → `grpc` / `graphql` · cache → `cache-subsystem` · permissions →
`authz-seams` (there is no section named `authz`) · inbound auth → `auth-middleware` ·
audit → `audit` · tracing → `tracing` · logs → `logs` · hooks → `lifecycle-hooks` ·
exports/OpenAPI → `openapi` · config keys → `yaml-reference` · one table with no aggregate
→ `direct-schema` (under Infrastructure, beside `table-schema`; its ABSENCE from the pin's
nav is the availability test — see `direct-schema.md`) · a column dating a business FACT
or counting one, filled by the framework and settable by no caller → `table-schema` (the
stamped family; the decision is an ENTITY change, so it belongs to `scaffold-entity` /
`evolve-entity`, whose `conventions/infra.md` owns it) · WHICH clock stamps the managed
timestamp columns → `yaml-reference` (`relational.clock`; on a pin that carries it the key
is mandatory with no default, so its absence is a boot abort — `boot-contract.md`) · the
write-side query DSL — operators, the envelope, and SUBQUERIES (`Sub`/`Exists`/`Outer`) →
`criteria` on a pin that carries the section, and inside `custom-command-handler` ("Loading
by criteria") on one that does not; its presence is the availability test for subqueries,
and `query-primitives.md` owns which question they answer · reaching another aggregate's
column across a foreign key → `read-joins` (`read-joins.md` owns the decision, including
the reduced target and chains).
