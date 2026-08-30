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
  refresh fails later **at the point of use** (`transport: no transport registered for
  "<name>" (build with the transport's build tag?)`). No `transport:` block (opt-out by absence — legal on
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
  bare path without the method (`"/livez"`) fails the boot-time public-routes scan and
  ABORTS boot (`must be "METHOD /path"`).
- **`auth.publicRoutes` is validated at boot against the registered route set,
  exact-match**: a typo, wrong method, or trailing slash (no matching route) ABORTS
  boot with the offender named; a path-param/wildcard entry can never match — mark
  that route `Doc.Public` instead. So the failure ladder is: missing entry → 401 on
  probes in prd; hand-fixed loosely → boot abort. One fix: the exact
  `METHOD /path` probe entries in `publicRoutes`.
- **`/readyz` 503 carries a REASON — read it, never just count retries.** Four,
  ordered: (1) `draining` — shutdown in progress; (2) `initializing: rebuilding view
  "X" (n/m)` — a background boot-time view rebuild: the service IS up (`/livez` 200)
  and serving, a follower pod adopting another pod's rebuild lock is normal, wait —
  this is not a hang; (3) `initializing: view rebuild in progress` — same class as
  (2), the window before the first progress record (drift reconcile) exists; (4)
  store unreachable — a real dependency failure, stop and
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
- **A MANDATORY key with no default aborts boot by its ABSENCE**, which is the mirror
  image of the strict decoding below and fails in a way nothing in Go reports: the
  service compiles, vets and tests green, and dies on the first boot after the bump.
  `relational.dialect` and `relational.dsn` have always been that shape; on a pin that
  carries it, `relational.clock` (`db` | `app`) joined them — deliberately undefaulted,
  because which clock a service's history is written against is an operator's
  declaration and a framework that picked one silently would be choosing whose
  timestamps to trust.

  **When you ASK an operator for that value, explain why the instant is read BEFORE the
  write — otherwise `db` reads as a gratuitous round-trip and the framework reads as
  badly built.** The obvious-looking design, `NOW()` inside the DML, is the one the
  framework deliberately does not use on EITHER setting: the instant is minted once per
  operation and bound as an ordinary argument, because one write is several statements
  (root, children, siblings, the base cascade) that must all carry the same instant, and
  because the value has to be known in Go before `COMMIT` — the outbox payload, the audit
  event, the lifecycle hooks and the response are built from it, and the unarchive cascade
  tells *this* archive's children from the ones already archived by comparing exactly that
  stamp. So the setting decides only WHOSE clock that one reading comes from: the backend
  (one clock for the whole fleet, one extra round-trip per write TX) or the writing
  process (no round-trip, the pod's own drift). Neither makes `updated_at` an ordering
  token — that is `Revision`, the commit-order counter. **Which keys are mandatory is the
  PIN's `yaml-reference`'s answer, never this file's** — read it against the project's own profiles, and check
  EVERY profile, since the one that boots in dev is not the one that boots in prd.
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

**Signal the APP, never a `go run`.** `go run` compiles to a temp binary and runs it as a
CHILD; it does not forward SIGTERM. So a verification boot launched as `go run … &` and
then sent `kill -TERM $!` exits with no drain at all — no `draining…`, no `drained`, and
often a listener still holding the port — which reads as "this service has no graceful
shutdown" when nothing was ever asked to shut one down. Any boot that will be signalled
runs the BUILT binary (`go build -tags '<engine> <transport>' -o ./bin/<svc> ./bootstrap`,
then run that; the start wrappers do exactly this and `exec` it, so the wrapper's pid IS
the app's). If you are stuck with a `go run`, signal the LISTENER — the pid holding the
port — or the whole process group, never the parent alone.

## Diagnosis quick-map (doctor)

Boot abort naming a MISSING mandatory yaml key (the pin's `yaml-reference` says which
are mandatory; `relational.clock` is the one that arrived most recently and the one an
upgraded service trips on) → the key has no default ON PURPOSE — add it to EVERY
profile with the same value, do not guess one per profile ·
Boot abort "no relational engine registered" → missing engine build tag · reactions/
subscriptions dead on a green service, `no transport registered` at the point of use →
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
