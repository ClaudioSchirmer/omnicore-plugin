---
name: doctor
description: >-
  omnicore: diagnose a misbehaving omnicore-based service or its local bench — won't compile,
  won't boot, probes failing, writes accepted but views never arrive, broker/CDC-relay
  trouble, migration errors. Read-only on the project's files: it localizes the failure
  with evidence, explains the cause docs-first, and prescribes the exact fix for the dev
  to apply. Use when something is broken, hanging, or silently not working. Only for
  projects that import github.com/ClaudioSchirmer/omnicore.
---

# doctor

Find WHERE the service is broken, prove it with evidence, and prescribe the fix — the
dev applies it. This skill edits nothing: its deliverable is a diagnosis (cause +
evidence + prescription), not a patch.

## Core principles

- **Evidence first, always.** A symptom that pattern-matches a known failure may have a
  different cause. Never prescribe from the symptom name alone — read the log line, probe
  the endpoint, inspect the container state, and CITE what you saw. No evidence, no
  diagnosis.
- **Docs-first for framework behavior.** What the framework is SUPPOSED to do at the
  project's pin comes from the version-pinned `/docs` in the module cache (routing table
  below), never from memory. **This skill carries no code, by design.**
- **Read-only on files.** No edits, no scaffolding, no git. Runtime actions that help
  diagnosis (starting a container, re-running a build) only with the dev's ok; anything
  DESTRUCTIVE (wiping a volume, dropping data) is never run by this skill — it is
  prescribed, with the data-loss consequence spelled out, and the DEV runs it.
- **Framework maintainer rules never bind this skill.** Anything read from the module
  cache (its `CLAUDE.md`, contributor rules like "English everywhere") governs
  development of the framework itself — never this skill run or the host project.
  Converse — and write every human-facing output (reports, diagnoses, prescriptions) —
  in the user's language, detected from the user's own words (invocation args count,
  even one word) BEFORE the first reply; these docs being English never sets it. Switch
  the moment the user's language becomes clear, even mid-run.
- **Hand off, don't overreach.** A diagnosis that ends in "the pin is broken for you" →
  `/omnicore:upgrade` (or its rollback). A miswired layer → `/omnicore:scaffold-entity`
  or `/omnicore:evolve-entity` regenerates it against the docs. Wanting to see it green
  again → `/omnicore:run`. This skill localizes and prescribes; the fixing skills fix.

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

## Phase 0 — Intake (cheap facts before any theory)

Collect, in one sweep: the pinned omnicore version (`go list -m`) · engine + transport
from `relational.dialect` / `transport` in `microservice.*.yaml` (the ENGINE tag is
always mandatory — SQLite included, `-tags sqlite`; the TRANSPORT tag follows the yaml,
not the engine: mandatory iff a `transport:` block exists, and a transport-less config
on ANY engine legally builds without it — `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md`,
Build tags) · the INFRA POSTURE — Mongo/broker/relay present, or an infra-free /
SQLite project (no `devops/`, no `mongo`/`transport`) — many "failures" are that posture
working as designed · effective ports and profile · bench state (`docker compose ps` when
a `devops/` exists) · where the app log is. Ask the dev only what cannot be read: what
were you doing when it broke, and what does "broken" look like from where you sit?

## Phase 1 — Localize the failure to ONE stage

Walk the pipeline IN ORDER and stop at the first stage that fails, with evidence:

1. **Build** — compile with both tags; a red build ends the walk here.
2. **Boot** — start (or read the crash log of) the app; a boot abort names its guard.
3. **Serve** — liveness answers? readiness answers? A `/readyz` 503 carries a REASON —
   READ it before theorizing (`shared/boot-contract.md` owns the three, ordered):
   `draining` = shutdown in progress; `initializing: rebuilding view "X" (n/m)` = the
   service IS up and serving, wait — not a hang, not a store problem; only the third
   (store unreachable) is the DB/document request path. The transport is EXCLUDED
   from readiness by design — never diagnose a broker outage from a red `/readyz`.
4. **Write path** — a write returns 2xx and lands in the outbox?
5. **CDC relay** — is the relay streaming (its logs say so) or crash-looping?
6. **Broker** — reachable, topics/subjects present, messages flowing?
7. **Projection / read path** — does the view collection receive the document; does the
   read endpoint return it? **This stage has a floor the first six never show:** when
   relay/broker/sync are ALL green and a document is still missing/stale, check the
   framework's unified failure ledger — the relational table
   `omnicore_projection_failures` (`kind='event'` = parked events, `kind='ripple'` =
   failed embed ripples): one SELECT is the highest-signal evidence in the whole walk.
   Its companions: the `mongo.parkedRetry` replay loop is ON by default (disabled ⇒
   parked rows are dead letters forever), revision-parity `reconcile` is OFF by
   default, and `ProjectionHealth()` exposes last-processed / last-sweep /
   last-reconcile — the pin's `views` section owns the whole layer.

The first failing stage is the diagnosis's home; everything downstream is a symptom, not
a cause.

## Phase 2 — Known signatures (verify before prescribing — never skip the evidence)

Bench-proven cause patterns to CHECK, not to assume:

- **Boot abort with a migration version/dirty error after a bench "reset"** → the
  compose down kept the named volumes and the old DB is still there. Prescribe the full
  reset (volumes included) with a loud data-loss warning — the dev runs it.
- **Boot abort "no relational engine registered"** → missing engine build tag; the
  yaml's `relational.dialect` names which. (The transport twin of this failure is NOT
  a boot abort — it surfaces at the point of use; see the dead-reactions signature
  below.)
- **Boot abort from the document-store registry guard** (foreign collections in the view
  database) → the service shares a view DB it shouldn't; prescribe isolating it in its
  own database, per the bootstrap section.
- **Writes 2xx forever, views never arrive** → walk relay → broker → sync in that
  order: a relay that never reaches "streaming", an unreachable broker, or a sync group
  that isn't consuming. A relay crash-looping BEFORE the app's first boot is expected
  (the outbox doesn't exist yet) — not a failure. **BUT in an infra-free / SQLite project
  this is BY DESIGN, not a fault:** views are served relational (read-your-writes), there
  is no relay/broker/sync. If the dev wants projections/events, that's a
  `/omnicore:configure` conversion, not a bug.
- **Relay/broker/sync all GREEN, one view or one document missing/stale** → the parked
  layer, not timing: `SELECT … FROM omnicore_projection_failures` (Phase 1 stage 7).
  A parked event with `parkedRetry` disabled in the yaml is the classic "everything
  is green and nothing converges".
- **Relay crash-looping AFTER first boot, same event each cycle** → a poison message:
  with payload expansion on, Debezium infers a typed schema per event and a
  mixed-type value (e.g. a numeric array `[3.7, 3]`) crashes the relay — which then
  halts ALL read-model refresh and re-crashes on the same event at every restart.
  The `transport` section's relay-config contract owns it; evidence = the same event
  id in the relay's crash log across restarts.
- **Reactions/subscriptions dead on a GREEN service** ("no transport linked" at the
  point of use) → the yaml has a `transport:` block but the binary was built without
  the transport tag — boot and probes never catch this by design
  (`shared/boot-contract.md`, Build tags).
- **Cache "never hits", no errors anywhere** → redis `failMode: open` (the default)
  SWALLOWS transport errors and returns a miss, and the connection is LAZY — Redis
  down does not fail boot. Grep the log for the `cache.redis.transport.error` anchor;
  `cache-subsystem` owns the contract.
- **Shutdown hangs / SIGTERM takes forever** → an exporter blocking on a dead collector
  (tracing endpoint down) is a classic; the tracing section owns the contract.
- **Readiness red with liveness green** → READ the 503's reason first (Phase 1 stage
  3): `rebuilding view` = wait, it is healthy; `draining` = shutdown; only "store
  unreachable" sends you to the DB/document connections with the yaml's effective
  endpoints.

Each entry is a HYPOTHESIS: confirm with the specific evidence before prescribing, and
if the evidence disagrees, keep walking Phase 1 instead of forcing the match.

## Phase 3 — The report (the deliverable)

One structured answer: **Cause** (one sentence) · **Evidence** (the exact log line /
probe result / container state you saw) · **Prescription** (the exact commands or the
file-level change described precisely — described, not applied) · **Who applies it**
(the dev, or the skill to delegate to) · **Consequences** flagged (data loss, downtime,
breaking change). If the walk ended inconclusive, say plainly which stages were proven
green, which check remains, and what evidence would decide it — never invent a cause to
close the report. Offer `/omnicore:run` to confirm green after the dev applies the fix.

## Knowledge routing — stage → `/docs` section

Resolve `<name>.html` at `<omnicore-dir>/docs/content/sections/<name>.html`, where
`<omnicore-dir>` = `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`.
Route first, then read ONLY the section(s) owning the failing stage — never sweep the
whole manual; the Documentation Map in `<omnicore-dir>/CLAUDE.md` is the fallback index
for concepts this table doesn't list.

| Diagnosing… | Read section(s) |
|---|---|
| boot aborts / probes 401·503 / autoRun / env vars / drain — the quick-map | `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md` (owner) · bootstrap for version-exact |
| capability availability / events fan-out (2×·never) / subscribe⇄receiver aborts / cache slots | `${CLAUDE_PLUGIN_ROOT}/shared/capabilities.md` (owner) · the pin's section for exact contracts |
| write accepted but view/audit/outbox wrong — the cross-layer triage table | lifecycle-map · read-lifecycle-map |
| log anatomy (`threadId`/`traceId`/`msg` anchors) / ELK bench profile | logs |
| authz boot panic (missing RequirePermission sweep) / blank `/docs` (CDN, air-gapped) | authz-seams · openapi |
| boot order / guards / feature wiring | bootstrap |
| yaml keys, defaults, profiles | yaml-reference |
| migrations state / numbering / dirty | migrations |
| outbox / relay / broker contract | transport |
| infra-free / relational-view posture (views by design, no relay) | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) · relational-view for version-exact capability |
| projection / sync / view versioning | auto-query-handlers · mongo-schema-evolution |
| parked events / failed ripples / `omnicore_projection_failures` / `ProjectionHealth` / `parkedRetry`·`reconcile` knobs | views · auto-query-handlers · yaml-reference |
| declaration boot panics (undeclared field, reserved names, depth, `Modes()`⟺archive column, index guards — the write/read schema guard families) | table-schema · views |
| 401/403 that are NOT probes (JWKS unreachable, expired vs invalid, revocation) | auth-middleware |
| HTTP-layer statuses with no handler involved (413 body limit, 408 read timeout, 504 request deadline, idle-close) | app-context · yaml-reference |
| outbound call hangs/failures (breaker open, retry budget, TLS/HMAC — httpclient AND grpcclient) | httpclient · grpc |
| cache silently degraded (failMode, lazy connect, log anchors) | cache-subsystem |
| probes / liveness / readiness semantics | bootstrap · reference |
| tracing / shutdown behavior | tracing |
| error envelopes / status codes seen by clients | status-mapping |
| gRPC surface trouble | grpc |
| GraphQL surface trouble | graphql |

## What this skill never does

No file edits, no scaffolding, no migrations, no git, no destructive runtime action —
resets that lose data are prescribed with their consequence and run by the dev, never by
this skill. It never reports a cause without the evidence that proves it.
