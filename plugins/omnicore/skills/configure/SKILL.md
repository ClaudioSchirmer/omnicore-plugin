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

The two anchor postures:
- **Zero-infra / MVP** — SQLite (`-tags sqlite`, `CGO_ENABLED=0`, one binary + `app.db` or
  `:memory:`), **no Docker**, no Mongo, no broker, no CDC relay. All views served relational from
  the SoR (read-your-writes). No Mongo projections; no integration events (publish and Mongo both
  ride the CDC relay, which no relay tails on SQLite — `relational-view`, `transport`).
- **Full distributed CQRS** — a Debezium-tailable engine (Postgres / MySQL / SQL Server / Oracle)
  + Mongo + broker (kafka | nats) + the CDC relay + a docker bench. Mongo-projected views,
  integration events (publish + subscribe), the whole read-side vocabulary.

…and every point between them: Mongo without changing engine is impossible on SQLite but every
other axis (broker on/off, transport kafka⇄nats, per-view backing) is independent.

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

## Phase 0v — Version check (delegate)

Detect a newer published omnicore than the pin (skip silently on `go.work`/`replace`/offline);
if newer, mention ONCE and offer `/omnicore:upgrade` BEFORE reading any doc. Never bump inline.

## Phase 0b — Discover the CURRENT posture (read, don't ask)

Map what exists before proposing — this is the whole safety of the run:
- **Engine + DSN** — `relational.dialect` and the DSN across every `microservice.*.yaml`
  (resolve `${VAR:default}`); the `migrations/<dialect>/` folders present; the build tags in the
  start wrappers.
- **Mongo** — is a `mongo:` block / `mongo.uri` present? (absent ⇒ relational-only posture.)
- **Transport** — a `transport:` block + which build tag (`kafka`/`nats`/tagless)?
- **Views** — which carry `.RelationalSource()` (SoR-served) vs Mongo-projected; the
  composed/shared/embed ones (Mongo-only by construction).
- **Integration events** — `integration.publishes` / `integration.subscribes` declared?
- **Devops** — is there a `devops/` bench (compose + Debezium)? Which dialect × transport?
- **Surfaces** — REST / GraphQL / gRPC.

## Phase 1 — Plan gate: `configure/plan.md`

`Status: DRAFT`, hard STOP until approved; `⚠️ OPEN` slots answered, never defaulted; sections
structural (`N/A — <why>`):

1. **From → To** — the current posture and the target, in one paragraph each.
2. **Impact map** — every artifact touched: yaml blocks (`mongo`/`transport`/`relational`/
   surfaces/tracing/cache/audit/shutdown), `devops/` (compose + Debezium relay), build tags +
   start wrappers, `migrations/<dialect>/`, each view to flip (→ delegated to `evolve-view`),
   `integration:` config. Phase 2 edits NOTHING outside it.
3. **Consequences, taught per axis** [high-risk] — from the pin's docs:
   - **Add Mongo** — requires a Debezium-tailable engine (so on SQLite this implies an engine
     swap) + the broker + the CDC relay; each view flipped relational→Mongo triggers an online
     blue-green rebuild (`mongo-schema-evolution`, `Version` bump — delegated to `evolve-view`).
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
   `CGO_ENABLED=0`, no compose, from `templates/sqlite-mvp.md`).
4. `migrations/<target>/` — on an engine swap, generate the ported DDL (docs-first per dialect);
   flag it for dev review.
5. **View backings** — delegate each flip to `/omnicore:evolve-view` (it owns the `Version` bump
   + rebuild discipline); this skill never rewrites a view declaration itself. When a flip
   lands a relational view, its feature must reuse the aggregate's EXISTING `repo.Loader` —
   never a second loader (the rule and why: `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`,
   Mechanics). See the feature example in `relational-view.html`.
6. `integration:` — enable/disable publishes/subscribes per the target.
Edit ONLY what the plan lists.

## Knowledge routing — change → `/docs` section

Resolve `<name>.html` at `<omnicore-dir>/docs/content/sections/<name>.html`, where `<omnicore-dir>`
= `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`. Route first, read ONLY the routed
section(s); the Documentation Map in `<omnicore-dir>/CLAUDE.md` is the fallback index.

| When changing… | Read section(s) |
|---|---|
| infra opt-out semantics (mongo/transport absent) · yaml blocks | yaml-reference |
| read-side posture · MVP framing · what each backing serves | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) · relational-view for version-exact parity |
| view flip / Version / rebuild | mongo-schema-evolution · evolve-view (skill) |
| broker / CDC relay / transport swap / tagless | transport |
| integration events (publish rides CDC · subscribe) | integration-events |
| engine types / DSN / SQLite specifics (:memory:, ASCII, decimal-TEXT) | table-schema |
| per-engine specifics: id/decimal/boolean columns · **constraint-violation KEY form** · active-only unique · read-side posture | `${CLAUDE_PLUGIN_ROOT}/shared/dialects/<dialect>.md` (read ONLY the target's) |
| migrations layout / autoRun / per-dialect | migrations |
| bootstrap / build tags / probes | bootstrap · architecture |
| docker bench + Debezium glue · SQLite zero-infra wrappers | scaffold-service/templates/ (validated vs transport) |

## Final verify (the gate)

1. **Mechanical, pre-boot:** yaml coherent with the target (a Mongo-backed/composed view present
   ⇒ `mongo.uri` present, else the boot aborts by design); build tags match the engine+transport;
   `gofmt -l` + `go vet` + `go build` (target tags) clean.
2. **Boot — posture-appropriate.** Zero-infra: `CGO_ENABLED=0 -tags sqlite`, no compose,
   `/livez`+`/readyz` 200. Full: bench healthy, relay reaches streaming, probes 200.
3. **Functional honesty:** a full CDC round-trip / a view rebuild proves itself only after writes
   flow — state what was verified (boot, registration) vs what needs a write-and-wait.
4. **Regression** — the project's suite if it has one.
5. **Offer to run.** ONE question: boot to click through? Yes → delegate `/omnicore:run` (it follows
   the chosen infra). No → done.

Leave `configure/plan.md` in place for review.

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
