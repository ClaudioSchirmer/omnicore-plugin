---
name: qa
description: >-
  omnicore: generate and run a CONTRACT QA SUITE for an omnicore-based service — read the
  project's entities, views, surfaces and infra posture, derive the framework's promised
  behaviors from the pinned docs (verb set per mode, status codes, archive semantics,
  filter vocabulary, typed 400s), and produce an executable e2e suite (qa/*.sh + a
  runner) that PROVES them against the running service. Use when the dev wants e2e tests,
  contract tests, a QA suite, smoke tests, or to "prove the service works". Only for
  projects that import github.com/ClaudioSchirmer/omnicore.
---

# qa

Close the loop the other skills open: scaffold builds it, run boots it, **qa proves
it**. This skill reads what the service declares (entities, modes, views, backings,
surfaces), derives what the PINNED framework therefore promises, and generates an
executable contract suite that exercises those promises against the real running
service — then runs it and reports GREEN/RED honestly.

**Everything this run writes into the project is a document the project keeps —
never add it to `.gitignore`** (`${CLAUDE_PLUGIN_ROOT}/shared/generated-documents.md`).

## Core principles — read FIRST

- **The contract comes from the PIN's docs, never from this skill.** Which verbs an
  entity serves, which status code each failure maps to, what `?includeArchived`
  reveals, which reads a relational view rejects — all of it is derived per entity from
  the docs at the project's pinned omnicore version (`go list -m -f '{{.Dir}}'
  github.com/ClaudioSchirmer/omnicore` → `/docs/content/sections/<name>.html`). This
  file names the derivation, never the answers. Never stamp a framework version here.
- **Prove behaviors, not implementations.** A case asserts what the wire promises
  (code + envelope + effect visible on a read-back), never internals. The generated
  suite must fail if the service regresses and must NOT fail when internals are
  refactored behind the same contract.
- **Posture-aware expectations** (`${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`, the
  owner): on a Mongo-projected backing, a write→read-back case POLLS the view for the
  NEWEST write (CDC lag is legitimate; a bounded poll, never a blind sleep); on a
  relational backing, read-your-writes is the promise — the read-back
  is IMMEDIATE and a needed poll is itself a failure. On SQLite the plain views are
  relational (read-your-writes), but a Mongo-backed shape the service kept
  (SharedBaseView) NEVER materializes there — no CDC source — so its honest assertion
  is "declared, boots, serves empty", never a read-back. The suite encodes the right
  expectation per view, not the loosest one.
- **Framework maintainer rules NEVER bind this skill** — the omnicore module ships its
  own `CLAUDE.md`; ignore it beyond the Documentation Map index. Only the host
  project's rules apply.
- **User language for talk, English for artifacts** — converse in the dev's language;
  generated scripts, case names and comments in English like the rest of the codebase.
- **This skill writes ONLY under `qa/`** — never application code, never yaml, never
  migrations. A case that can't pass because the SERVICE is wrong is a finding to
  report (route `doctor` / the owning skill), never something to "fix" by weakening
  the case. **Never edit a case to pass — the suite is the oracle.**

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

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves —
  else STOP (this is not a generic test generator). At least one entity wired — else
  there is nothing to prove; say so and point at `scaffold-entity`.
- **No version check here** (like `remove-entity`): the suite proves the CURRENT pin's
  contract; upgrading first is `/omnicore:upgrade`, separately.

## Phase 0b — Discover (read, don't ask)

Map what the service declares — this inventory IS the test surface:
- **Entities**: schemas (fields, VOs — including any COMPOSITE value object and the
  names its parts are EXPOSED under, which are the only names any surface speaks; the
  composite's own field name appears NOWHERE on the wire), uniqueness incl. active-only, children,
  siblings, SharedBase roles), `Modes()` per aggregate (the verb set each entity
  actually serves — an absent verb still gets a case, never a skip, but ASSERT THE
  RIGHT CODE, three distinct shapes per `status-mapping`: mode missing from `Modes()`
  with the route mounted → the mode's `…NotAllowedNotification`, **403**; no route
  matches the path at all → **404**; the same path registered under another method
  only → **405**), constraint bindings (which duplicate → which 409 key).
  **Cross-check the derived verb inventory against `GET /openapi.json`** — the
  framework auto-registers it whenever `Wiring.OpenAPI` is set and it enumerates the
  routes actually wired (probes included): the cheapest, most reliable oracle for
  "which verbs does this entity really serve"; a source-vs-openapi disagreement is a
  finding, not a guess to resolve silently.
- **Views**: per view its backing (`.RelationalSource` or Mongo) → the read-back
  expectation per the posture rule above; archive regime (kept-but-hidden vs
  `DeleteOnArchive`); filter/sort/search vocabulary per field; which RESERVED controls
  the list Request DTO declares (`query:"…"` — the DTO opt-in gate: declared = served,
  undeclared = typed 400 on presence; `?fields=`/`?onlyTotal`/`?search` included).
- **Surfaces**: REST always; GraphQL/gRPC/exports only when wired — a case per
  enabled surface (handler invariance: the SAME operation answers identically), none
  for absent ones.
- **Infra posture** (`shared/read-side.md` + `shared/capabilities.md`): Mongo+CDC
  present, or relational-only/SQLite. Auth mode per profile.
- **Existing `qa/`**: if the project already has a suite, mirror its conventions and
  EXTEND it — never generate a parallel second style.

## Phase 1 — Plan gate: `qa/plan.md`

`Status: DRAFT`, hard STOP until approved. Sections (structural completeness — `N/A —
<why>`, never deleted):

1. **Coverage matrix** — per entity × surface: the case families derived from Phase 0b
   and the pin's docs — happy path per served verb · validation 422 (a representative
   VO/rule failure per shape, asserting the notification KEY, not prose) · the dual
   409 (duplicate vs wrong-state — only where the entity declares each) · archive
   round-trip (`archive → hidden → ?includeArchived reveals → unarchive → visible`;
   with `DeleteOnArchive`, absence instead) · read vocabulary (one filter/`?orderBy=`
   per declared operator family, `?fields=` when opted in, `?search=` where a text index
   serves it, `?onlyTotal=true` only-total, `?last=` alone serving the TAIL window, and
   the PAGINATION ENVELOPE as a contract: `pagination.totalCount`/`hasNextPage`/
   `hasPreviousPage`/`startCursor`/`endCursor` truthfulness (cursors are WINDOW EDGES —
   walk by echoing them into `?after=`/`?before=`), page-2 disjointness, cursor
   advance) · **golden-record round-trip** (one record exercising EVERY declared
   field — VO fields included, and a COMPOSITE value object as its EXPOSED PARTS, one
   wire field each, never the composite's own name — written then read back field-by-field on each enabled
   surface: the family that catches a field silently dropped from a DTO/projection,
   which every other family passes over) · rejected reads — the WHOLE typed-400 guard
   family, not one: the relational 1:N pushdown · unknown field · operator outside a
   field's allowlist · a RESERVED control the Request DTO does not declare (the DTO
   opt-in gate — PRESENCE trips it, so an undeclared `?onlyTotal=false` rejects too;
   surface idiom differs BY DESIGN: REST 400, GraphQL unknown argument — and `fields`/
   `onlyTotal` are selection-natural there, never gated — gRPC INVALID_ARGUMENT; the
   pin's `graphql`/`grpc` sections own the per-surface rendering, don't assert the
   REST envelope cross-surface) ·
   `?first=`/`?last=` above the view's ceiling · mixed directions (`first`+`last`,
   `first`+`before`, `after`+`before` — backward is `last`+`before`) · `?onlyTotal=true`
   beside a page-shaping control (the only-total conflict matrix; filters/`?search=`/
   `?includeArchived` stay valid — counting a filtered subset is the point) · malformed
   `?after=`/`?before=` · cursor↔`orderBy` mismatch ·
   cursor↔`includeArchived` mismatch · `?search=` on a relational view · segment
   `?orderBy=` on a composed leg (derive the exact set from `status-mapping`'s
   SemanticSchema rows + `auto-query-handlers` at the pin — the enumeration here is
   the FAMILIES, the pin owns the members) · absent verbs
   → the 403/404/405 split above · not-found → 404. Route: `auto-handlers` +
   `status-mapping` + `auto-query-handlers` at the pin for the exact contracts.
2. **Data hygiene** [high-risk — ⚠️ OPEN, the dev decides]: where the suite's records
   live. Options, stated honestly: run against the DEV profile bench with
   uniquely-suffixed records the suite archives at the end (residue: archived rows) ·
   a dedicated throwaway database selected by a SUITE-OWNED config file (generated
   under `qa/`, picked via `OMNICORE_CONFIG_PATH` — that is how a "dedicated profile"
   respects this skill's never-touch-the-project-yaml rule) · SQLite `:memory:`
   (only if the service already runs so). Never silently write into a database the
   dev cares about. **And state HOW state resets between runs/cases** — exact-count
   assertions demand it: on a Mongo-projected backing, wiping the relational rows
   does NOT clear the projection (a separate store — clear the view collections too),
   and a wipe racing in-flight CDC re-materializes documents, so reset = relational
   delete + view clear + a short DRAIN before seeding (the canonical suites treat the
   clean baseline as a precondition, not a hope).
3. **Auth** [⚠️ OPEN when enabled]: dev profile usually ships `auth.mode: disabled` —
   the suite runs tokenless and SAYS the auth layer is untested. If the dev wants
   auth-enabled QA: where does a test token come from (their IdP — never invented)?
   Then 401/403 cases join the matrix.
4. **Out of scope, named plainly**: load/performance, UI. An `⚠️ OPEN` only if the dev
   asks for it. Integration events are IN scope when the service publishes or consumes
   them: assert the in-TX `integration_events` row per publishing write (SQL — always
   provable), and when the bench has a live relay+broker, the consume side too
   (receiver → effect, dedup via `omnicore_integration_processed`); without a live
   relay, mark the delivery half `⚠️ OPEN` honestly instead of silently dropping the
   capability from the contract.
5. **Runner contract**: `qa/run.sh` executes suites **fail-fast by default** (first
   RED stops the run; an explicit flag for the exhaustive sweep), prints per-suite
   GREEN/RED counts and a final matrix line, and each suite EXITS NON-ZERO when any
   of its cases failed (without that, fail-fast can never trip); every temp file is
   namespaced per run
   (PID/timestamp — parallel lanes must never share a hardcoded `/tmp` path), **and
   so is every other shared artifact of a lane: the HTTP/gRPC ports, the compiled
   server binary, its log file** (two lanes sharing any of them corrupt each other);
   cleanup
   runs on exit; the service is stopped with SIGTERM, **never** `kill -9` — and the
   runner WAITS on the server PID until the drain completes (default budget ~30s)
   before the next suite binds the same port.

## Phase 2 — Generate + execute

1. Generate `qa/<entity>.sh` per entity + `qa/run.sh` per the approved plan. Style:
   plain POSIX-friendly bash + `curl` (+ the project's own tooling for GraphQL/gRPC
   when enabled — `grpcurl` only if available, else mark those cases SKIPPED loudly);
   each case prints its name, expectation and verdict; a failed assertion shows the
   REAL response body, not a summary; **every request pins `Accept-Language`** (one
   fixed locale — envelope assertions must be deterministic, not hostage to the
   machine's locale).
2. **Bench first, on the Mongo/full posture**: `docker compose -f
   devops/docker-compose.yml ps` → missing/unhealthy services `up -d` and wait
   healthy — the read side depends on containers this skill never assumes are up.
3. **Build + boot the service under test — with the right tags and the right config,
   explicitly** (`shared/boot-contract.md`, Build tags, owns the law): build
   `-tags '<engine> <transport>'` from the yaml's `relational.dialect` + `transport:`
   block (no `transport:` block → no transport tag; SQLite → `CGO_ENABLED=0 -tags
   sqlite`); boot in background with `APP_PROFILE` and — when the plan's hygiene
   picked a suite-owned config — `OMNICORE_CONFIG_PATH`, log to a per-lane file, poll
   `/readyz` READING the 503 reason. **Resolve the effective port from the yaml's
   `${VAR:default}` interpolation and probe it FIRST**: something already listening
   there means you may be about to test a binary you didn't build — kill/free the
   port (SIGTERM) before booting, never assume the listener is yours. Full-bench:
   confirm the relay
   reached streaming BEFORE any CDC-dependent case, else those cases report a bench
   problem, not a service failure.
4. Execute `qa/run.sh`. Report the real counts.

## Final verify (the gate)

0. **Reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`) — walk
   the plan's coverage matrix item by item: every promised case family exists in the
   generated suite and RAN; an unimplemented family is RED or an explicit dev-accepted
   deviation.
1. **The suite is honest**: pick one generated case, break its expectation manually
   (e.g. assert 200 where the service returns 409), run it, watch it FAIL, restore it.
   A suite that cannot fail proves nothing — this meta-case is mandatory.
2. **Full run green** (or the RED list reported verbatim with bodies — a RED here is a
   FINDING about the service, routed to `doctor`/the owning skill, never patched away
   in the suite).
3. **Residue as planned**: whatever §2 of the plan promised about data is what
   actually remains; say what was left and where.
4. Leave `qa/plan.md` in place; offer `/omnicore:run` if the dev wants the service
   kept up for manual poking.

## Knowledge routing — question → source

| When deriving… | Read |
|---|---|
| verb set / handler contracts / bodyless results | auto-handlers |
| status codes / envelopes / notification keys / dual 409 | status-mapping |
| filter operators / `?fields=` / pagination / exports | auto-query-handlers · query-side |
| read-back expectation per backing (poll vs immediate) / archive regime | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) · relational-view for version-exact |
| probes / readyz reasons / boot & drain discipline | `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md` (owner) |
| what's testable under this posture / event semantics | `${CLAUDE_PLUGIN_ROOT}/shared/capabilities.md` (owner) · integration-events · transport |
| GraphQL parity / gRPC procedures | graphql · grpc |
| auth / permissions / public routes | auth-middleware · authz-seams |

## What this skill never does

- Touch anything outside `qa/` — no application code, no yaml, no migrations.
- Weaken or delete a case to make a run green.
- Invent tokens, credentials or endpoints.
- Claim coverage it didn't run — SKIPPED is printed as SKIPPED, never folded into
  GREEN.
