# shared/capabilities.md — what the framework offers, when, and how to choose

The single home for **capability availability and the choice rules between them** —
what works under which infra posture, which integration style fits which need, what is
already automatic. Skills that wire, offer, or diagnose capabilities route HERE — one
owner, no drift. No code, by design; exact APIs, middleware lists and option names are
the PIN's docs — if this file ever disagrees with them, the docs win.

## Availability under the infra posture — BOTH directions

The posture (Mongo/CDC present or not — `../read-side.md` owns the view side) gates a
SHORT list. State it both ways; never refuse what the posture doesn't gate:

- **Needs Mongo + CDC relay** (absent on infra-free / SQLite): Mongo-projected views
  and every multi-source view KIND, integration events (publish AND subscribe — both
  ride the relay), UpstreamSubscription.
- **Works EVERYWHERE, SQLite included** — offer freely: httpclient (+ its middleware),
  cache (both slots), gRPC and GraphQL surfaces, exports, auth/authz, audit, tracing,
  lifecycle hooks, domain events (in-process), the whole write side. The mirror-image
  failure — refusing an available capability "because it's an MVP" — is as wrong as
  offering an unavailable one.

## Service-to-service — the decision matrix

- **Sync call, internal service → gRPC** (the default for east-west traffic).
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
zero cost no-op. "Cache this" therefore always carries one elicitation: private or
shared, and which backend — never silently pick.

## Already automatic — check BEFORE wiring

Things `implement` must NOT "wire" because they're already on:
- **Audit** — zero handler code; it travels with the persister. An ABSENT `audit`
  yaml block = BOTH destinations active; `[]` disables; unknown token aborts boot.
- **Domain events** — `RegisterEvent` auto-publishes post-commit on both Auto and
  manual paths (Slog publisher by default; swap the publisher, don't re-plumb).
- The honest answer to "add X" is sometimes "X is already on — here's where to see
  it". Check this list first.
