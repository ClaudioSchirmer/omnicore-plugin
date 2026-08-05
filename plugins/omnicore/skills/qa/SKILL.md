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
  relational backing (SQLite always), read-your-writes is the promise — the read-back
  is IMMEDIATE and a needed poll is itself a failure. The suite encodes the right
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

As in the sibling skills: confirm the plugin cache is current; on mismatch mention
`/plugin` update once and continue.

## Phase 0a — Preflight

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves —
  else STOP (this is not a generic test generator). At least one entity wired — else
  there is nothing to prove; say so and point at `scaffold-entity`.
- **No version check here** (like `remove-entity`): the suite proves the CURRENT pin's
  contract; upgrading first is `/omnicore:upgrade`, separately.

## Phase 0b — Discover (read, don't ask)

Map what the service declares — this inventory IS the test surface:
- **Entities**: schemas (fields, VOs, uniqueness incl. active-only, children,
  siblings, SharedBase roles), `Modes()` per aggregate (the verb set each entity
  actually serves — an absent mode means the VERB IS ABSENT and its case asserts 405,
  never skips), constraint bindings (which duplicate → which 409 key).
- **Views**: per view its backing (`.RelationalSource` or Mongo) → the read-back
  expectation per the posture rule above; archive regime (kept-but-hidden vs
  `DeleteOnArchive`); filter/sort/search vocabulary per field; `?fields=` opt-in.
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
   with `DeleteOnArchive`, absence instead) · read vocabulary (one filter/sort per
   declared operator family, `?fields=` when opted in, pagination) · rejected reads
   (the typed 400 on a relational view's 1:N pushdown; unknown field) · absent verbs
   → 405 · not-found → 404. Route: `auto-handlers` + `status-mapping` +
   `auto-query-handlers` at the pin for the exact contracts.
2. **Data hygiene** [high-risk — ⚠️ OPEN, the dev decides]: where the suite's records
   live. Options, stated honestly: run against the DEV profile bench with
   uniquely-suffixed records the suite archives at the end (residue: archived rows) ·
   a dedicated throwaway database/profile (clean, more setup) · SQLite `:memory:`
   (only if the service already runs so). Never silently write into a database the
   dev cares about.
3. **Auth** [⚠️ OPEN when enabled]: dev profile usually ships `auth.mode: disabled` —
   the suite runs tokenless and SAYS the auth layer is untested. If the dev wants
   auth-enabled QA: where does a test token come from (their IdP — never invented)?
   Then 401/403 cases join the matrix.
4. **Out of scope, named plainly**: integration-event consumption (needs a consumer
   harness — `implement` wires consumers; proving broker delivery end-to-end is not
   an HTTP contract), load/performance, UI. An `⚠️ OPEN` only if the dev asks for it.
5. **Runner contract**: `qa/run.sh` executes suites **fail-fast by default** (first
   RED stops the run; an explicit flag for the exhaustive sweep), prints per-suite
   GREEN/RED counts and a final matrix line; every temp file is namespaced per run
   (PID/timestamp — parallel lanes must never share a hardcoded `/tmp` path); cleanup
   runs on exit; the service is stopped with SIGTERM, **never** `kill -9`.

## Phase 2 — Generate + execute

1. Generate `qa/<entity>.sh` per entity + `qa/run.sh` per the approved plan. Style:
   plain POSIX-friendly bash + `curl` (+ the project's own tooling for GraphQL/gRPC
   when enabled — `grpcurl` only if available, else mark those cases SKIPPED loudly);
   each case prints its name, expectation and verdict; a failed assertion shows the
   REAL response body, not a summary.
2. **Boot the service the way `run` does** (background, log to a file, poll `/readyz`
   READING the 503 reason — `shared/boot-contract.md`); full-bench: confirm the relay
   reached streaming BEFORE any CDC-dependent case, else those cases report a bench
   problem, not a service failure.
3. Execute `qa/run.sh`. Report the real counts.

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
