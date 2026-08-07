# shared/boot-contract.md — boot, probes, config & shutdown, one owner

The single home for the framework's **operational contract**: what boots, what aborts,
what the probes really say, and how a service drains. Every skill that writes yaml,
boots a service, or diagnoses one routes HERE instead of restating — one owner, no
drift. No code, by design. The version-exact key/default tables are `yaml-reference` /
`bootstrap` / `migrations` at the PIN — this file orients and names the traps; if it
ever disagrees with the pinned docs, the docs win.

## Build tags — the boot's precondition

- Every build must link exactly ONE relational engine tag —
  `postgres | mysql | sqlserver | oracle | sqlite`. A build with no engine tag
  COMPILES clean and then **aborts at boot** ("no relational engine registered …
  build with the engine's build tag?") — the classic green-build/dead-boot trap.
- The transport tag (`kafka | nats`) follows the **yaml, not the engine**: a config
  with a `transport:` block needs the matching tag; without it the build compiles,
  boots and probes GREEN, and every consumer / upstream subscription / Mongo-projected
  refresh fails later **at the point of use** ("no transport linked — build with
  -tags kafka or -tags nats"). No `transport:` block (opt-out by absence — legal on
  ANY engine, not just SQLite) → build with no transport tag, no messaging — and the
  projection sync consumer is SKIPPED at boot with an INFO line ("projection consumer
  not started: no transport configured"); view registry, spec application and drift
  detection still run, so a `mongo:` block WITHOUT `transport:` boots green with
  collections that exist but never receive a row (useful only to let Mongo-declared
  views boot — a bench/QA shape, not a serving posture).
- The zero-infra SQLite MVP is engine-tag-only and pure Go:
  `CGO_ENABLED=0 go build -tags sqlite`. SQLite still NEEDS its engine tag — "SQLite
  is tagless" is a misreading; only the transport tag is absent, and only because the
  posture has no `transport:` block.

## Probes — registered for free, public NOT for free

- `/livez` (process up) and `/readyz` (fit for traffic) are framework-registered on
  every service. Under `auth.mode: jwt` they still require a token — a tokenless
  kubelet gets **401** and the orchestrator kills/never-readies a healthy pod. They
  must be LISTED in `auth.publicRoutes`.
- **Entry format is `METHOD /path` — mandatory**: `"GET /livez"`, `"GET /readyz"`. A
  bare path without the method (`"/livez"`) fails `parsePublicRoutes` and ABORTS boot.
- **`auth.publicRoutes` is validated at boot against the registered route set,
  exact-match**: a typo, wrong method, or trailing slash (no matching route) ABORTS
  boot with the offender named; a path-param/wildcard entry can never match — mark
  that route `Doc.Public` instead. So the failure ladder is: missing entry → 401 on
  probes in prd; hand-fixed loosely → boot abort. One fix: the exact
  `METHOD /path` probe entries in `publicRoutes`.
- **`/readyz` 503 carries a REASON — read it, never just count retries.** Three,
  ordered: (1) `draining` — shutdown in progress; (2) `initializing: rebuilding view
  "X" (n/m)` — a background boot-time view rebuild: the service IS up (`/livez` 200)
  and serving, a follower pod adopting another pod's rebuild lock is normal, wait —
  this is not a hang; (3) store unreachable — a real dependency failure, stop and
  diagnose. **The transport is EXCLUDED from readiness by design** — a broker outage
  never flips `/readyz`.

## Dev-only gates (other profiles reject at boot)

- `auth.mode: disabled` — accepted ONLY under the dev profile; a prd/other profile
  without a real `auth` block aborts boot.
- The featureless shell — accepted only when `Features == nil && BeforeServe == nil`
  (`Wiring.OpenAPI` still qualifies as featureless); the moment a feature exists,
  `Translations` are required. Under non-dev profiles a featureless wiring is a boot
  reject, not a warning.

## Migrations `autoRun` — a profile-aware three-mode enum

`check | true | false`. Dev defaults to `true` (apply on boot); every other profile
defaults to `check` — **strict mode: pending or dirty migrations ABORT boot on
purpose** (the recovery options are in `migrations` at the pin; `Force(version)`
resets the tracking pointer ONLY, it runs nothing). A badly-named file
(`v2_init.up.sql`) is caught by validation, not silently skipped. Every `.up.sql`
needs its `.down.sql` (may be a no-op) or boot aborts.

## Config — interpolation & strictness

- `${VAR}` / `${VAR:default}` env forms: an unset var with no default is SILENT
  (empty) — the classic "boots wrong" input. `${file:...}` and `${vault:...}` are
  STRICT: unreadable path / missing secret ABORTS boot naming it. No recursion; a
  literal `}` needs care. Prd endpoints ship as pure `${VARS}` — no localhost
  defaults.
- Several yaml blocks are STRICT-DECODED (unknown key = boot abort), among them
  `mongo.rebuild`, `auth.authorization`, each `upstreamSubscriptions` entry; the
  `audit.destinations` list aborts on an unknown token or duplicate — and an ABSENT
  audit block means BOTH destinations active, `[]` disables. Exact block list: the
  pin's `yaml-reference`.
- **The framework env vars are a CLOSED set of four**: `APP_PROFILE`,
  `OMNICORE_CONFIG_PATH`, `OMNICORE_MONGO_FORCE_REBUILD` (exact string `"true"`;
  narrow scope — index conflicts only, never drops collections or data),
  `OMNICORE_CODE_VERSION`. Everything else is configuration and belongs in the yaml —
  one-shot operational overrides must NOT ship in git; that is WHY they are env vars.

## Shutdown — an ordered drain, narrated

SIGTERM starts a dependency-ordered drain with budgets
(`shutdown.drainTimeoutSeconds` default 30s · `tracingDrainSeconds` 5s ·
`hardGraceSeconds` watchdog, negative opts out); a SECOND SIGTERM means exit now.
The log narrates each stage `draining…/drained` and NAMES the laggard — a slow
shutdown is diagnosed by READING that narration, not by guessing. Never observe boot
or drain through a pipe (`| tee`, `| grep`): a broken pipe can swallow the narration —
log to a file. (And per the QA discipline: never `kill -9` a serving process — orphans
keep the ports.)

## Diagnosis quick-map (doctor)

Boot abort "no relational engine registered" → missing engine build tag · reactions/
subscriptions dead on a green service, "no transport linked" at the point of use →
`transport:` block present but built without the transport tag · Mongo-backed views
never materialize, boot INFO "projection consumer not started" → no `transport:`
block at all (posture by design, not a fault) ·
401 on probes under jwt → `publicRoutes` missing the probe paths · boot abort naming a
publicRoutes entry → exact-match validation, fix the literal · boot abort on pending
migrations under prd → `autoRun: check` working as designed · `/readyz` 503 with
`rebuilding view` → wait, not down · Mongo index-conflict boot abort →
`OMNICORE_MONGO_FORCE_REBUILD=true` one-shot · featureless/empty-Translations reject
→ dev-only gate · boot abort naming a file path/secret → strict `${file:}`/`${vault:}`
interpolation.
