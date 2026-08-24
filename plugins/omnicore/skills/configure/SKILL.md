---
name: configure
description: >-
  omnicore: change an existing omnicore service's INFRASTRUCTURE POSTURE and configuration —
  convert a zero-infra/SQLite MVP into full distributed CQRS (add Mongo + broker + CDC relay +
  docker) or the reverse, swap the relational engine, switch transport (kafka ⇄ nats), and tune
  the microservice.*.yaml / devops glue. Every conversion is REVERSIBLE and no application code is
  lost — only infra, config and view backings move. Use when the user wants to add/remove Mongo,
  enable integration events, add or change the broker/CDC bench, switch database engine, go from
  MVP to production infra (or back), or adjust yaml/docker/debezium configuration. Only for
  projects that import github.com/ClaudioSchirmer/omnicore.
---

# configure

Move a service between infrastructure postures without touching a line of its domain, application
or web code. An omnicore service is written the same way whichever backend serves it: the
read-side backing (relational SoR vs Mongo projection), the broker, the CDC relay and the docker
bench are all **config + devops**, chosen per profile. This skill changes that choice — in either
direction — and tunes the surrounding configuration.

**Every document this run writes lands under `specs/`, and the project keeps it —
never add it to `.gitignore`** (`${CLAUDE_PLUGIN_ROOT}/shared/generated-documents.md`).

The two anchor postures:
- **Zero-infra / MVP** — SQLite (`-tags sqlite`, `CGO_ENABLED=0`, one binary + `app.db` or
  `:memory:`), **no Docker**, no Mongo, no broker, no CDC relay. All views served relational from
  the SoR (read-your-writes). No Mongo projections; no integration-event PUBLISHING (publish and
  Mongo both ride the CDC relay, which no relay tails on SQLite — `relational-view`,
  `transport`). SUBSCRIBING needs only a broker + the transport build tag — absent on the pure
  MVP too, but a broker alone (no Mongo, no relay) unlocks it (`shared/capabilities.md`).
- **Full distributed CQRS** — a Debezium-tailable engine (Postgres / MySQL / SQL Server / Oracle)
  + Mongo + broker (kafka | nats) + the CDC relay + a docker bench. Mongo-projected views,
  integration events (publish + subscribe), the whole read-side vocabulary.

…and every point between them: Mongo PROJECTIONS without changing engine are impossible on
SQLite (no relay tails it), but a `mongo:` BLOCK is legal on any posture: without a `transport:`
block the sync consumer is skipped at boot (INFO line "projection consumer not started: no
transport configured") while registry/specs/drift still run — Mongo-declared views BOOT over
collections that never receive a row (the shape a bench/QA profile uses, e.g. to let a
SharedBaseView exist). Every other axis (broker on/off, transport kafka⇄nats, per-view
backing) is independent.

## Core principles — read FIRST

- **Reversible, no code lost — the north star, said out loud every time.** A conversion moves
  config, devops and view backings; it NEVER rewrites domain/application/web code. Going toward
  full CQRS EXPANDS what the service can do (Mongo views, composed/shared/embed, integration
  events); going back to MVP contracts it. State the trade honestly; never frame either direction
  as a lock-in.
- **Capability-aware, never capability-gated.** This skill exists so nothing is ever refused: it
  is the "enable it" path other skills route to. Teach the target posture's consequences, propose,
  CONFIRM — never cut a capability, always offer the conversion that unlocks it.
- **Docs-first, version-agnostic — no framework code in this skill, by design.** The
  version-pinned `/docs` are the SOLE authority (routing table below); YAML keys, opt-out
  semantics, engine type tables, migration layout and the transport/CDC contract all come from the
  pin. The ONE sanctioned exception is the devops glue — reused from `scaffold-service/templates/`
  (compose + Debezium relay + the SQLite zero-infra wrappers), every framework-facing value
  re-validated against `transport.html`. If any text disagrees with the doc, the doc wins.
- **Framework maintainer rules NEVER bind this skill** (module-cache `CLAUDE.md`, "English
  everywhere", approval gates — framework-repo policy, not this project's).
- **Language — the user's, never imposed; detect it BEFORE the first reply.** The skill and docs
  are English; the run speaks the user's language, detected from their own words; everything
  human-facing is built in it.
- **Risk split.** High-risk (propose + CONFIRM, never guess): changing the engine, adding/removing
  Mongo, adding/removing the broker, flipping a view's backing (each carries a rebuild / devops /
  migration cost — say it), and any change that alters what production must run. Low-risk (decide
  well, show filled, don't ask): yaml tuning (timeouts, workers, ports, group names), which
  `${VAR:default}` shapes to use, comment prose.

## Plugin self-check (once, non-blocking)

Once per run, during preflight: compare THIS plugin's installed version — the `version` field of
`${CLAUDE_PLUGIN_ROOT}/.claude-plugin/plugin.json` — with the published one at
`https://raw.githubusercontent.com/ClaudioSchirmer/omnicore-plugin/main/plugins/omnicore/.claude-plugin/plugin.json`.
Offline, or either side unreadable → skip silently. Newer published → ONE non-blocking line riding
the next reply ("omnicore plugin vX → vY available — update with `claude plugin update
omnicore@omnicore` (marketplace stale? `/plugin marketplace update omnicore` first); it takes
effect next session"). Never a gate.

## Phase 0a — Preflight

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves — else STOP
  (an empty dir is `scaffold-service`).
- **What is being asked** — a posture conversion (MVP ⇄ full, engine swap), a single-axis change
  (add broker, swap transport), or pure config tuning. All three are this skill's; scope the run
  to the axes named.
- **Toolchain for the TARGET posture:** when the target gains a docker bench (MVP → full,
  or adding any containerized piece), check `docker` + `docker compose` NOW — discovering
  a Docker-less host at the verify gate wastes the whole run (`scaffold-service` checks
  the same way).

## Phase 0v — Version check (delegate)

Detect a newer published omnicore than the pin (skip silently on `go.work`/`replace`/offline);
if newer, mention ONCE and offer `/omnicore:upgrade` BEFORE reading any doc. Never bump inline.

## Phase 0b — Discover the CURRENT posture (read, don't ask)

Map what exists before proposing — this is the whole safety of the run:
- **Engine + DSN** — `relational.dialect` and the DSN across every `microservice.*.yaml`
  (resolve `${VAR:default}`); the `migrations/<dialect>/` folders present; the build tags in the
  start wrappers.
- **Mongo** — is a `mongo:` block / `mongo.uri` present? (absent ⇒ relational-only posture;
  present WITHOUT `transport:` ⇒ collections boot but the sync consumer is skipped — see the
  anchor-postures note above.)
- **Transport** — a `transport:` block + which build tag (`kafka`/`nats`/tagless)?
- **Views** — which are declared as RELATIONAL read models (SoR-served, their own type,
  contributed through the relational feature seam) vs Mongo-projected; the
  composed/shared/embed ones (Mongo-only by construction).
- **Read joins** — which repositories declare one, and which of their fields a read model
  currently FILTERS, SORTS or SERVES. The DECLARATION is backing-independent and no
  conversion touches it: a join reaches the entity and the rules on either posture, so
  never propose removing one as part of a posture change. Its READ-side reach is NOT
  backing-independent, and that asymmetry is the consequence to carry into item 3 — a
  relational read model inherits the traversal, a Mongo projection cannot see it at all
  (`shared/read-joins.md`).
- **Integration events** — `integration.publishes` / `integration.subscribes` declared?
- **Devops** — is there a `devops/` bench (compose + Debezium)? Which dialect × transport?
- **Surfaces** — REST / GraphQL / gRPC.

## Phase 1 — Plan gate: `specs/configure/plan.md`

`Status: DRAFT`, hard STOP until approved; `⚠️ OPEN` slots answered, never defaulted; sections
structural (`N/A — <why>`):

1. **From → To** — the current posture and the target, in one paragraph each.
2. **Impact map** — every artifact touched: yaml blocks (`mongo`/`transport`/`relational`/
   `migrations` — **on an engine swap `migrations.dir` must repoint to the TARGET
   dialect's folder in EVERY profile** (default `./migrations`; a stale dir silently
   degrades to "empty service sequence": no boot error, no tables) /
   surfaces/tracing/cache/audit/shutdown), `devops/` (compose + Debezium relay), build tags +
   start wrappers, `migrations/<dialect>/`, each view to flip (→ delegated to `evolve-view`),
   `integration:` config. Phase 2 edits NOTHING outside it.
3. **Consequences, taught per axis** [high-risk] — from the pin's docs:
   - **Add Mongo** — requires a Debezium-tailable engine (so on SQLite this implies an engine
     swap) + the broker + the CDC relay; each view flipped relational→Mongo is re-declared as
     a projected view and its first rebuild provisions the collection (`mongo-schema-evolution`,
     `Version(1)` — delegated to `evolve-view`). **A view that inherited a READ JOIN loses
     that reach in the flip**: the traversal survives untouched on the repository and the
     entity and its rules still read the field, but the projection cannot carry it, so
     every filter and sort declared over a joined field dies with the flip and the field
     leaves the served shape. That is a consumer-visible LOSS, not a detail — name it per
     view in the plan, from the Phase 0 inventory (`shared/read-joins.md`), and let the
     delegated `evolve-view` run own the wording of each one.
     **A flip the OTHER way leaves a collection and an `omnicore_mongo_views` row behind
     that nothing drops for you** — the DB-per-service guard then aborts the boot outside
     `dev`; the drop belongs IN the plan, not in the incident afterwards.
     **And say WHAT it unlocks** — usually the actual reason for the conversion: the view KINDS
     that were unavailable (identity / `SharedBaseView`, `ComposedView`, the Embed/Link family,
     Upstream) and integration events. Name the ones THIS project was told it could not have —
     they are on record as `n/a — needs Mongo` in `specs/scaffold-system/domain-map.md` (§3/§5) and in
     the entity specs. Availability in BOTH directions:
     `${CLAUDE_PLUGIN_ROOT}/shared/capabilities.md` + `shared/read-side.md` (owners) — route to
     them, don't restate.
   - **Add broker** — enables integration events. Publish rides the CDC relay (same tailable-engine
     requirement); subscribe works once a broker exists. (`transport`, `integration-events`.)
   - **Engine swap** — new `migrations/<target>/` in the TARGET dialect's SQL (types/constraints
     per that dialect's `table-schema` tables); the skill generates a first pass, the dev reviews —
     DDL correctness in the new dialect is theirs to confirm. **Re-key every repo `Constraints`
     map to the TARGET dialect's violation-key form** (`${CLAUDE_PLUGIN_ROOT}/shared/dialects/<target>.md`):
     swapping SQLite→SQL turns dotted `table.column` keys into `<table>_<col>_key` NAMES (and PKs
     into `<table>_pkey`/`PRIMARY`); SQL→SQLite is the reverse. A stale key form silently misses →
     the custom 409 regresses to a 500. Moving existing MVP data is a manual ETL, out of scope: say
     so. Application/domain/web code is untouched.
   - **Remove infra (full → MVP)** — projections and integration events go away, views become
     relational (what that serves: `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`), Mongo/broker/
     relay/compose are dropped (SQLite ⇒ no Docker). Honest loss list.
   - **Transport swap (kafka ⇄ nats)** — build tag + `transport:` endpoints + the Debezium sink
     block; consumers are byte-identical by contract (`transport`).
4. **Reversibility** — restate: application code is untouched; the inverse conversion is another
   `configure` run. No lock-in.
5. **Surfaces / tuning** [low-risk] — decided and shown, not asked.

## Phase 2 — Execute the impact map

One pass in dependency order — read the owning `/docs` section BEFORE each artifact:
1. `microservice.*.yaml` — add/remove/edit the blocks (yaml-reference).
2. `devops/` — instantiate compose + Debezium relay from `scaffold-service/templates/` for the
   target dialect × transport (or delete `devops/` entirely when converting to zero-infra SQLite —
   no Docker); validate framework-facing values against `transport.html`.
3. Build tags + start wrappers — engine + transport tags (or `-tags sqlite` tagless,
   `CGO_ENABLED=0`, no compose, from `templates/sqlite-mvp.md`). **A tag change pulls
   tag-gated deps `go mod tidy` cannot see** (`//go:build kafka|nats` files): follow
   with `GOFLAGS=-mod=mod go build …` so go.sum gains the entries — go.mod and go.sum
   move together, and a missing-entry build failure at the verify means THIS step was
   skipped (`scaffold-service` step 10 owns the same trap).
4. `migrations/<target>/` — on an engine swap, generate the ported DDL (docs-first per dialect);
   flag it for dev review — and repoint `migrations.dir` in every profile (impact-map
   item 2).
5. **View backings** — delegate each flip to `/omnicore:evolve-view` (it owns the
   re-declaration, the `Version`/rebuild discipline on the Mongo side AND the by-hand
   collection + registry-row drop on the relational side); this skill never rewrites a view
   declaration itself. **A flip is a conversion between two declaration TYPES, not a flag**
   (pin ≥ v0.57.0), so it also moves the view between two feature seams. When a flip lands a
   relational read model, its feature must reuse the aggregate's EXISTING loader — never a
   second one, which would quietly serve a different reach (the rule and why:
   `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`, Mechanics). See the feature example in
   `relational-view.html`.
6. `integration:` — enable/disable publishes/subscribes per the target.
Edit ONLY what the plan lists.

## Knowledge routing — change → `/docs` section

Resolve `<name>.html` at `<omnicore-dir>/docs/content/sections/<name>.html`, where `<omnicore-dir>`
= `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`. Route first, read ONLY the routed
section(s); the Documentation Map in `<omnicore-dir>/CLAUDE.md` is the fallback index.

| When changing… | Read section(s) |
|---|---|
| infra opt-out semantics (mongo/transport absent) · yaml blocks | yaml-reference |
| strict-decoded blocks · publicRoutes validation · autoRun modes · env-var posture | `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md` (owner) |
| what the posture gates vs what works everywhere · integration-style choice | `${CLAUDE_PLUGIN_ROOT}/shared/capabilities.md` (owner) |
| read-side posture · MVP framing · what each backing serves | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) · relational-view for version-exact parity |
| reaching ANOTHER aggregate from a query — read joins (repository-declared), and the rule-vs-wire split | `${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md` (owner) · read-joins for version-exact contract |
| view flip / Version / rebuild | mongo-schema-evolution · evolve-view (skill) |
| broker / CDC relay / transport swap / tagless | transport |
| integration events (publish rides CDC · subscribe) | integration-events |
| engine types / DSN / SQLite specifics (:memory:, ASCII, decimal-TEXT) | table-schema |
| per-engine specifics: id/decimal/boolean columns · **constraint-violation KEY form** · active-only unique · read-side posture | `${CLAUDE_PLUGIN_ROOT}/shared/dialects/<dialect>.md` (read ONLY the target's) |
| migrations layout / autoRun / per-dialect | migrations |
| bootstrap / build tags / probes | bootstrap · architecture |
| docker bench + Debezium glue · SQLite zero-infra wrappers | scaffold-service/templates/ (validated vs transport) |

## Final verify (the gate)

0. **Reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`) — walk the
   conversion plan's own promises item by item with evidence; an unmet target is RED or
   an explicit dev-accepted deviation.
1. **Mechanical, pre-boot:** yaml coherent with the target (a Mongo-backed/composed view present
   ⇒ `mongo.uri` present, else the boot aborts by design; `migrations.dir` points at the
   target dialect's folder in every profile); build tags match the engine+transport;
   `gofmt -l` + `go vet` + `go build` (target tags) clean.
1b. **prd static sanity** — when the conversion added/removed `mongo`/`transport`/
   `relational` blocks, the prd profile moved too: the new blocks present there as pure
   `${VARS}` (no localhost defaults), the removed ones gone, `auth` intact. The prd is
   never boot-tested here — check it statically and say so (`scaffold-service`'s 1b, same
   discipline).
2. **Boot — posture-appropriate.** Zero-infra: `CGO_ENABLED=0 -tags sqlite`, no compose,
   `/livez`+`/readyz` 200 — **and probes are NOT proof on SQLite**: `/readyz` is a
   `SELECT 1` that passes over an EMPTY database, so a full→MVP conversion must also
   prove the schema landed where the runtime reads — boot via the wrapper (absolute
   `SQLITE_PATH`) and inspect the schema (`sqlite3 app.db ".tables"` → the framework
   control-plane tables + the service's entity tables present), exactly like
   `scaffold-service`'s level 3. Full: bench healthy, relay reaches streaming, probes 200.
3. **Functional honesty:** a full CDC round-trip / a view rebuild proves itself only after writes
   flow — state what was verified (boot, registration) vs what needs a write-and-wait.
4. **Regression** — the project's suite if it has one.
5. **Honor what the conversion unlocked.** On a run that GAINS Mongo/relay, close the loop the
   earlier skills opened: the map and the specs PROMISED these kinds would arrive "later via
   `/omnicore:configure`" — this is later. Name what is now servible (the `n/a — needs Mongo`
   slots collected in Phase 1) and OFFER the route: a NEW kind is `/omnicore:scaffold-view`; an
   existing view merely changing backing was already Phase 2 step 5's `evolve-view`. **Offer,
   never auto-create** — this skill writes no view declaration. Nothing on record ⇒ state that
   the kinds are available now and move on. On a run that REMOVES infra, the mirror: name what
   stops being servible (it is in the plan's honest loss list). No silent capability change in
   either direction.
6. **Offer to run.** ONE question: boot to click through? Yes → delegate `/omnicore:run` (it follows
   the chosen infra). No → done.

Leave `specs/configure/plan.md` in place for review.

## Re-entry — plan already exists

`Status: DRAFT` → reopen the gate with what's answered. `Status: APPROVED` → apply only the
not-yet-applied impact-map items, then re-verify. A changed target reopens the plan.

## What this skill never does

No domain/application/web code edits (a posture change never rewrites business code; a field the
new backing needs is `evolve-entity`'s job), no view declaration rewrite (delegated to
`evolve-view`), no framework edits, no git, no silent data migration on an engine swap, no
fabricated DDL passed off as verified in a dialect it wasn't checked against, no capability refused
(there is always a conversion that enables it).

## templates/ index

Reuses `scaffold-service/templates/` (the sanctioned devops-glue exception) rather than
duplicating them:

| File | Covers |
|---|---|
| `../scaffold-service/templates/docker-bench.md` | compose skeleton per dialect × transport + Mongo + relay, healthchecks, ports, start wrappers |
| `../scaffold-service/templates/cdc-relay.md` | Debezium Server `application.properties` — source × sink blocks, the outbox + integration_events EventRouter, relay traps |
| `../scaffold-service/templates/sqlite-mvp.md` | SQLite zero-infra glue — no Docker: `CGO_ENABLED=0 -tags sqlite` start wrappers, `app.db`/`:memory:` DSN, `.gitignore` |
