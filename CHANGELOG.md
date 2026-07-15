# Changelog

All notable changes to the omnicore plugin. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions are the
`version` field of `plugins/omnicore/.claude-plugin/plugin.json` — each release
is the commit bumping that field on `main`, tagged `v<version>`.

## [0.6.0] — 2026-07-15

### Added
- **New skill `implement`** (`/omnicore:implement`, 12th skill): wire a framework
  capability into an existing service — another surface (gRPC/GraphQL), an external
  API call from a handler, cache, integration events, lifecycle hooks, authz,
  tracing, resilience — anything the PINNED framework offers that no dedicated skill
  owns. The pin's docs are the capability catalog: requests route dynamically
  against the Documentation Map (`features.html`/`reference.html` as existence
  check); a capability claim with no doc section behind it never enters the plan.
  Honest-no path: not at this pin but in a newer release → offer
  `/omnicore:upgrade`; not offered at all → name the closest legitimate path, never
  a workaround. Standard rituals: plan gate (`conventions/plan-template.md` —
  routing evidence, integration semantics, impact map, config/secrets, verify
  step), doc-read-before-artifact, capability PROOF in the final verify (unprovable
  steps reported honestly), fallback-router handoffs to every dedicated skill.

### Changed
- `scaffold-system` Phase 3: the domain map's §6 items (integration events,
  external calls, extra surfaces) now have an executor — each is delegated to
  `/omnicore:implement`, one per invocation, after the §5 read models.

## [0.5.0] — 2026-07-15

### Added
- **New skill `scaffold-system`** (`/omnicore:scaffold-system`, 11th skill): turn a
  whole-system/MVP description — several entities, shared identities and read models
  handed in one prose drop — into an approved **domain map**
  (`conventions/domain-map-template.md`), then scaffold it entity by entity by
  delegating each one to `scaffold-entity` (and each cross-entity read model to
  `scaffold-view`). Decomposition at SYSTEM altitude only (boundaries, shared
  identities + natural keys, role cardinalities, references, order); generation stays
  per-entity with fresh context — the map pre-answers the structural spec slots
  (§9 delegation contract) but never waives the per-entity gates. The map is the
  durable checklist: re-entry resumes at the first `pending` row; conflicts between
  the map and a delegated run's discovery stop and surface, never silently resolve.

### Changed
- `scaffold-entity` — receiving hook for the domain map: Phase 0b now looks for
  `scaffold-system/domain-map.md` (delegated run or direct invocation alike — if it
  exists, reading it is mandatory). APPROVED + entity listed → §9 slots enter the
  spec as DECIDED (`per domain-map §9`), never re-asked; discovery-vs-map conflicts
  stop and surface; DRAFT map → surface and ask; entity absent → advisory flag;
  delegated runs skip their own Phase 0v (the orchestrator resolved it once).

## [0.4.3] — 2026-07-15

Fix from a real scaffold run: the flat-vs-SharedBase question described the
SharedBase mechanism from memory ("1:1 per role"), conflating the ≤1-ACTIVE-row
invariant with one-row-forever, and used that to disqualify a case (sequential
listings over the same property) the separate-FK model handles natively.

### Fixed
- `scaffold-entity` `SKILL.md` item 1: new **role-cardinality digest** — the only
  mechanism facts the question's option text may state (≤1 ACTIVE role row per
  identity per role table, 409 on `POST`/`/unarchive`; separate-FK allows archived
  remnants + a new active row; shared-PK caps at one row forever). Names "1:1 per
  role" without ACTIVE as the canonical mis-summary.
- `scaffold-entity` `SKILL.md` item 1: when the request ALREADY names the other
  roles (even "out of scope for now"), the scripted question is answered — the
  OPEN slot becomes role cardinality, asked literally ("can the same identity
  hold TWO ACTIVE rows of this role at once?"), never self-answered.

### Changed
- `scaffold-entity` `SKILL.md`: "FLAT is the default" retitled "FLAT is the
  default CONTEXT LOAD — not a modeling bias" — it decides which conventions to
  read and carries zero weight in the recommendation.
- `scaffold-entity` `SKILL.md` item 1: identity smell broadened beyond persons to
  any party/asset with a natural registry key (property by land-registry number,
  vehicle by VIN, company by tax-id).

## [0.4.2] — 2026-07-15

Fixes from the first real sqlserver×nats scaffold runs: both templates carried
traps that made the fresh bench fail its first boot.

### Fixed
- `scaffold-service` `templates/cdc-relay.md`: the properties blocks carried
  inline `# …` comments — Java `.properties` files have no end-of-line
  comments, so a faithful copy shipped `snapshot.mode=no_data   # …` as a
  literal (invalid) value and killed the relay at boot. Every comment now sits
  on its own line, plus an explicit no-inline-comments warning.
- `scaffold-service` `templates/docker-bench.md`: the mssql image has no
  auto-create-database env (no `MYSQL_DATABASE`/`POSTGRES_DB` equivalent), so
  the first app boot died with `Cannot open database "<svc>_db"`. The sqlserver
  variant now says so, and the start wrappers gain a synchronous idempotent
  `CREATE DATABASE` step before the app boot (the reference consumer's
  `qa/_backend.sh` shape).

### Changed
- `scaffold-service` build steps (Phase 2 step 10 and the final verify) use
  `go build -o /dev/null … ./bootstrap` — the default output name `bootstrap`
  collides with the directory of the same name (hit in every real run).
- `scaffold-service` Phase 2 step 1: after `go get @latest`, cross-check
  `go list -m -versions` — the proxy's `@latest` endpoint can lag a
  just-published tag (a run pinned v0.31.0 minutes after v0.32.0 shipped).

## [0.4.1] — 2026-07-14

### Changed
- `scaffold-entity` `conventions/domain.md`: persisted fields carry `labelKey`
  and NOTHING else — the no-tags rule now names `json:` explicitly (a real
  scaffold run added wire tags to the domain by Go reflex). Framework ≥ v0.32.0
  turns the dangerous case (`json:"-"`, custom entity JSON codecs) into a boot
  panic; the convention keeps generated code clean on every pin.
- `upgrade` Phase 3 (green): the run offer is part of the VERIFY, not a
  click-through — several framework guards are boot panics no compile surfaces
  (e.g. the closed persistable type set, old-clone safety).

## [0.4.0] — 2026-07-14

SQL Server joins the dialect set (framework v0.31.0). The skills stay
version-agnostic: every dialect list is phrased as "the closed set the pinned
release supports — read it from the pinned docs", with today's latest named for
visibility; a service pinned to a pre-SQL-Server release is unaffected.

### Added
- `scaffold-service`: sqlserver bench variant in `templates/docker-bench.md`
  (mssql 2022 image, amd64-only note, image-enforced strong SA password,
  `MSSQL_AGENT_ENABLED` as load-bearing for CDC) plus the idempotent CDC-enable
  arm in the start wrappers (per-database and per-table enablement is only
  possible after the first boot creates the outbox); sqlserver source block in
  `templates/cdc-relay.md` (plural `database.names`, no-TLS dev bench, MySQL-like
  file-backed schema history, `dbo.outbox` predicate) and the note that
  `integration_events` needs its own table enablement later.
- `scaffold-service` traps: SQL Server relay prerequisites (Agent + CDC enable
  ordering) and the SA-credentials exception to the bench-DSN rule.

### Changed
- Dialect/engine enumerations in `scaffold-service`, `run` and `upgrade` are now
  doc-routed instead of hardcoded (`postgres` | `mysql` | `sqlserver` named as
  today's latest set; the pinned docs are the authority).
- `scaffold-entity`: the id-typing trap and the migrations/infra/sharedbase
  conventions carry the SQL Server facts per the pinned `table-schema.html` —
  ids/FKs are `BINARY(16)` (never `UNIQUEIDENTIFIER`; GUID sort order would
  destroy the UUIDv7 time locality), the PK is named `<table>_pkey` explicitly
  (unlike MySQL's `PRIMARY`), and the mechanical `CHAR(36)`/`VARCHAR(36)` sweep
  also covers `migrations/sqlserver/`. Where the pinned docs define no mechanism
  for a dialect (self-documenting DDL comments, active-only uniqueness on SQL
  Server today), the skill routes to the doc instead of inventing one.

## [0.3.3] — 2026-07-13

### Changed
- Business-neutral vocabulary sweep across `scaffold-entity`: the shared-identity
  read model is now consistently "the identity view" (routes file, feature and
  permission named after the BASE), sibling elicitation says "base-level facet",
  and canonical-example names (`persons`, `person_view.go`, …) remain only where
  explicitly marked as examples. The skill legislates process, never a business
  domain. (#8)

### Added
- `repository` and `license` (Apache-2.0) fields in the plugin manifest, per the
  community-marketplace submission recommendations. (#8)

## [0.3.2] — 2026-07-13

Correctness fixes for `scaffold-entity`, all three from a monitored field run
(gaps found in generated services, then closed at the source):

### Fixed
- **Child archive wiring**: the aggregate-children convention now states that all
  three per-child operations (add / update / archive) are partial updates of the
  ROOT, and calls out the trap by name — the word "archive" on a child route must
  never be wired to the root's archive auto handler (it compiles, answers 200 and
  silently archives the whole aggregate). A final-verify checklist item enforces
  it mechanically. (#7)
- **Read-request query params are optional by default**: scalar `query:`-tagged
  fields are pointers unless the spec explicitly declares a filter required — a
  value type renders the parameter REQUIRED in the OpenAPI spec and Swagger
  refuses the call without it. New web-layer trap + final-verify checklist item.
  (#7)
- **Identity view read surface**: offering the SharedBase identity view now
  includes its full read surface — the standard by-id + by-params pair with
  filters, never a lone by-id. Elicitation and sharedbase convention updated. (#7)

## [0.3.1] — 2026-07-13

### Added
- User-language policy across all skills: converse in the user's language;
  human-facing generated text follows the host project's language. (#6)
- Scope immunity across all skills: framework maintainer rules (leaked via the
  module cache's `CLAUDE.md`) never bind a skill run in a consumer project. (#6)

## [0.3.0] — 2026-07-13

### Added
- Six new skills — the plugin now ships ten: `evolve-entity`, `remove-entity`,
  `scaffold-view`, `evolve-view`, `run`, `doctor`. (#5)
- `scaffold-entity` final-verify guards: domain `Service` wired end-to-end on
  every write handler; one schema declaration per file. (#5)

### Changed
- Every skill description leads with `omnicore:` so the whole package surfaces
  when typing "omnicore" in the slash-picker (plugin skills list by bare name).
  (#5)

## [0.2.0] — 2026-07-13

### Added
- `scaffold-entity` migrations carry the spec's one-line table descriptions and
  column meanings as SQL comments. (#4)

## [0.1.0] — 2026-07-13

### Added
- Initial release: the `omnicore` marketplace + plugin with four skills —
  `scaffold-service`, `scaffold-entity`, `upgrade`, `help`.

### Fixed
- Packaging (still 0.1.0): skill directories dropped from the first push by bare
  `.gitignore` patterns (#1); marketplace plugin `source` as an explicit relative
  path (#2). Dev/release workflow documented in the README (#3).
