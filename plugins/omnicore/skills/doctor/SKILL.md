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
   READ it before theorizing (`shared/boot-contract.md` owns the four, ordered):
   `draining` = shutdown in progress; `initializing: rebuilding view "X" (n/m)` — and
   its no-progress-yet sibling `initializing: view rebuild in progress` — = the
   service IS up and serving, wait — not a hang, not a store problem; only the last
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

- **`import cycle not allowed` through `internal/domain`, or a build error from the domain
  package naming an infra/third-party symbol** → something that is not domain was placed in
  the domain package, and the cycle is the architecture reporting it. The fix is never a
  new shared package to break the cycle: identify what does not belong (a mechanism
  contract, protocol constants, a helper parked there for import convenience), and move it
  to its consumer — `${CLAUDE_PLUGIN_ROOT}/shared/domain-membership.md` owns the decision
  and carries the destination table. Prescribe the move; the dev applies it. Worth stating
  in the report even when the build is green and the misplacement was merely noticed:
  nothing fails at compile time in the common case, which is exactly why it accumulates.
- **Boot abort naming a yaml key the profile does not have, right after a framework
  bump** → a key that ARRIVED MANDATORY with no default; the build was green because
  nothing in Go references it. Read the abort's own text for the key, then check the
  pin's `yaml-reference` for the whole mandatory set inside that block rather than
  fixing the one key the abort happened to reach first — the guard stops at the first
  miss, so a second one is waiting behind it. `relational.clock` (`db` | `app`) is the
  recent instance: no default on purpose, because which clock the service's history is
  stamped against is the operator's declaration. **Prescribe the same value for EVERY
  profile** — a service stamped by the backend's clock in dev and the pod's clock in
  prd has timestamps that mean two different things. Owner:
  `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md`.
- **Boot abort with a migration version/dirty error after a bench "reset"** → the
  compose down kept the named volumes and the old DB is still there. Prescribe the full
  reset (volumes included) with a loud data-loss warning — the dev runs it.
- **Boot abort "no relational engine registered"** → missing engine build tag; the
  yaml's `relational.dialect` names which. (The transport twin of this failure is NOT
  a boot abort — it surfaces at the point of use; see the dead-reactions signature
  below.)
- **Document-store registry guard** (foreign collections in the view database) — the
  presentation is PROFILE-SPLIT: under `dev` it is a `slog.Warn` naming each foreign
  collection and boot CONTINUES (foreign docs can then leak into reads — grep the boot
  log for the warning when reads return strangers); under every other profile it is a
  boot abort. **Read the abort's own text before prescribing** — it names the database,
  lists each unclaimed collection and leads with the CAUSE, and there are three quite
  different ones:
  - the read model's declaration is no longer in the build — its view was **deleted,
    renamed, or CONVERTED to a relational read model**, which materializes nothing and
    therefore claims no collection. The conversion is the one that surprises people: it
    leaves both the collection AND its `omnicore_mongo_views` row behind, nothing drops
    them for you, and **deleting only the collection aborts the next boot for a different
    reason**. Prescribe BOTH statements the abort hands over — the collection drop and the
    registry-row delete, keyed by the VIEW name, not by the physical slot.
  - a collection materialized by an upstream SUBSCRIPTION (pin ≥ v0.57.0 claims those
    under their bare name; an older pin reported them as foreign, which made a service
    with a subscription unbootable outside `dev` — if the pin is older, that IS the
    diagnosis and the answer is `/omnicore:upgrade`).
  - genuinely shared database → prescribe isolating this service in its own, per the
    bootstrap section.
- **Writes 2xx forever, views never arrive** → before the boot log, settle ONE thing about
  the write itself: **was it an aggregate write at all?** A **Direct** repository write (one
  table, no aggregate — `${CLAUDE_PLUGIN_ROOT}/shared/direct-schema.md`) emits no outbox
  row BY DESIGN, so there is no CDC record, nothing for the relay to stream and nothing for
  the projection to apply — on every posture, Mongo present or not. The signature is a
  clean split: the row is in the table, the outbox has nothing for it, and the boot log is
  innocent. It is a design mismatch, not a broken pipeline, and the answer is the door
  (promote the resource to an aggregate), never a relay fix. The same read explains a
  missing AUDIT trail and absent domain/integration events on that write. Then, for a
  genuine aggregate write, read the BOOT LOG: the INFO
  anchor `projection consumer not started: no transport configured` is direct
  evidence — with no `transport:` block the sync consumer is skipped BY DESIGN
  (registry, spec application and drift detection still run, so collections exist
  and boot is green) and a Mongo-backed view will NEVER materialize on this posture;
  relational views are unaffected, and the answer is a `/omnicore:configure`
  conversion, not a fix. Do not fuse it with its twin: a `transport:` block PRESENT
  but the binary built WITHOUT the transport tag logs no such line and fails at the
  point of use (`no transport registered` — the dead-reactions signature below). Only
  with the block present AND the tag linked, walk relay → broker → sync in that
  order: a relay that never reaches "streaming", an unreachable broker, or a sync
  group that isn't consuming. A relay crash-looping BEFORE the app's first boot is
  expected (the outbox doesn't exist yet) — not a failure. In an infra-free / SQLite
  project none of the three exists and views are served relational
  (read-your-writes) — same INFO anchor, same `/omnicore:configure` answer.
- **A read returns 400 on a relational read model** → not a bug, a capability boundary,
  and the notification tells you WHICH: `UnsupportedCapabilityNotification` (pin ≥
  v0.57.0 — every read engine raises this same one; an older pin had a
  relational-specific name) means the request asked for something that backing cannot
  serve — `?search=`, or filter/sort on a 1:N child field. `SchemaViolationNotification`
  means something else entirely: the `?fields=` path or the cursor is not a thing this
  read model has, which is the SAME answer the Mongo reader gives the same token. Do not
  prescribe a backing conversion for the second one — it is a client-side typo or a stale
  consumer. For the first, the honest options are the Mongo projection or, when the field
  belongs to another aggregate reached by a foreign key, a read join
  (`${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md`) — the capability rule and the split are
  owned by `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`.
- **A read join field is blank / the aggregate vanished from every read** → the two
  failure modes of a declared traversal, and they look nothing alike. Blank on a LEFT
  join = there is genuinely no counterpart (nil is an ABSENCE, and the framework refuses
  a non-pointer field precisely so it cannot be confused with the zero value). An
  aggregate missing from `FindByID` as well as from the listing = an INNER join whose
  foreign key points at nothing — the declaration lives on the repository, so it drops the
  row from every read, and a legitimate write then 404s on its own record. The framework
  refuses `inner` over a NULLABLE key at construction; over a non-nullable one with broken
  referential integrity, the data is the bug. Never diagnose either from the view — the
  join is on the repository (`${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md`).
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
- **Reactions/subscriptions dead on a GREEN service** (`transport: no transport
  registered for "<name>" (build with the transport's build tag?)` at the
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
| a write REFUSED where it used to be accepted — a 409 on an update, a 404 on a lifecycle verb: read what the pin guards a write on before treating it as a regression | lifecycle-map · status-mapping |
| log anatomy (`threadId`/`traceId`/`msg` anchors) / ELK bench profile | logs |
| authz boot panic (missing RequirePermission sweep) / blank `/docs` (CDN, air-gapped) | authz-seams · openapi |
| boot order / guards / feature wiring | bootstrap |
| yaml keys, defaults, profiles | yaml-reference |
| migrations state / numbering / dirty | migrations |
| outbox / relay / broker contract | transport |
| infra-free / relational-view posture (views by design, no relay) | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) · relational-view for version-exact capability |
| reaching ANOTHER aggregate from a query — read joins (repository-declared), and the rule-vs-wire split · a repository that COMPILED and now panics at construction over a join target (the traversal takes one table, so the argument is reduced — the classic first symptom of a framework upgrade, not of a code change) | `${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md` (owner) · read-joins for version-exact contract |
| a write that got slower as the table grew · a hand-written Service/finder that lists rows to answer a scalar (including one that runs a query only to collect ids and a second filtered by them — that is a subquery) · `Old()` empty on a write loaded by hand (a list load does not snapshot) | `${CLAUDE_PLUGIN_ROOT}/shared/query-primitives.md` (owner) · `criteria` where the pin has it, else custom-command-handler · old-state |
| a row that landed but produced NO outbox record, NO audit line and NO event · a declaration panic naming a child/sibling/shared base, or an entity schema refused as an anchor · a write rejected before any statement (empty predicate, id or stamped timestamp in the values) | `${CLAUDE_PLUGIN_ROOT}/shared/direct-schema.md` (owner) · direct-schema for version-exact contract |
| **a read that is slow because the code WORKED AROUND a query it believed impossible** — an N+1 loop, aggregates loaded to fold the answer in Go, a denormalized column added to the write path so a read would not have to join, a view projected for a one-off report, an aggregate repository bent into a report shape, or a comment/commit saying "the framework cannot do that join". **The diagnosis is the DOOR, not the query: a `DirectRepository` over a Direct schema anchors on ANY table and joins ANY table with no foreign key and no relationship required, at any depth, with the whole criteria surface over what it reached — and a Direct READ costs the aggregate nothing.** Prescribe the Direct anchor and name the workaround that comes out | `${CLAUDE_PLUGIN_ROOT}/shared/direct-schema.md` (owner — *THE READ IS UNRESTRICTED*) · `${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md` · `${CLAUDE_PLUGIN_ROOT}/shared/query-primitives.md` |
| projection / sync / view versioning | auto-query-handlers · mongo-schema-evolution |
| parked events / failed ripples / `omnicore_projection_failures` / `ProjectionHealth` / `parkedRetry`·`reconcile` knobs | views · auto-query-handlers · yaml-reference |
| declaration boot panics (undeclared field, reserved names, depth, `Modes()`⟺archive column, index guards — the write/read schema guard families) | table-schema · views |
| an unarchive that restored NO child, or revived one removed long before the root's archive — the restore is scoped by the archive STAMP, so the suspects are a `deleted_at` coarser on the child than on the root (it truncates the stamp and matches nothing) and a second-resolution column (a bare MySQL `DATETIME`, which folds two operations of the same second into one). Neither raises an error | aggregate-persistence · table-schema |
| a boot panic naming a value object: a multi-field one mapped with `Field(...)` (decompose it with `Composite(...)`), a scalar/enum one passed to `NewCompositeValueObject` (it occupies ONE column — `Field(...)`), a struct with no `IsValid` (not a value object at all), two fields of one composite type on an entity (resolution is BY TYPE), one composite split across root and a sibling (the once rule), a `json:"-"` part or a value object with a custom (un)marshaler (both poison the `Old()` ghost) | table-schema · value-objects · old-state |
| 401/403 that are NOT probes (JWKS unreachable, expired vs invalid, revocation) | auth-middleware |
| HTTP-layer statuses with no handler involved (413 body limit, 408 read timeout, 504 request deadline, idle-close) | app-context · yaml-reference |
| outbound call hangs/failures (breaker open, retry budget, TLS/HMAC — httpclient AND grpcclient) | httpclient · grpc |
| cache silently degraded (failMode, lazy connect, log anchors) | cache-subsystem |
| probes / liveness / readiness semantics | bootstrap · reference |
| tracing / shutdown behavior | tracing |
| **a 500 on a by-id route, or on an ordinary filter** (`?age=abc`, `?someId=lixo`), where the SAME request answers 404/400 against a Mongo-backed view or a newer pin. Since **v0.70.0** the refusal happens at the wire, before the handler: a malformed `:id` is 404 `UnknownIDAddressNotification` on a read and 400 `MalformedIDNotification` on a write, and a filter value outside the leaf's declared kind is 400 `InvalidFilterValueNotification`. So the suspects are a pin below v0.70.0 (a relational backing reached the driver — SQLSTATE 22P02 on Postgres, a uuid-codec failure elsewhere), or a HAND-WRITTEN handler still on the two-value `fwweb.BindPath` — the auto wrappers got the guard for free, a manual one has to return `fwweb.RespondViolation` | status-mapping · auto-handlers · auto-query-handlers |
| error envelopes / status codes seen by clients | status-mapping |
| gRPC surface trouble | grpc |
| GraphQL surface trouble | graphql |

## What this skill never does

No file edits, no scaffolding, no migrations, no git, no destructive runtime action —
resets that lose data are prescribed with their consequence and run by the dev, never by
this skill. It never reports a cause without the evidence that proves it.
