# Changelog

All notable changes to the omnicore plugin. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions are the
`version` field of `plugins/omnicore/.claude-plugin/plugin.json` — each release
is the commit bumping that field on `main`, tagged `v<version>`.

## [0.12.0] — 2026-08-03

### Added
- **Value objects are now a first-class, self-taught concept in `scaffold-entity`.** A new
  descriptive section in `conventions/domain.md` ("Value objects — PERCEIVE them, never
  inline the rule") teaches the agent to distinguish a **raw value object** (`ValueObject[T]`,
  bespoke `IsValid` — Name/Email/Phone/Document/ZipCode) from an **enum value object**
  (`EnumValueObject[E,T]`, declares `Values()` + an Unknown notification, framework validates
  membership), that VO validation is AUTOMATIC on root AND aggregate value object alike
  (`IgnoreValueObject`/`ValidateValueObject` to opt out/force), and the boundary parse via
  `EnumByValue`. Deliberately more prose than the skills' usual doc-pointer style — VO
  perception is a judgement the agent must make on its own. Routes to the framework's new
  `value-objects.html`. New docs-map rows in `scaffold-entity` and `evolve-entity`, and a
  spec-time "perceive value objects" cue in `conventions/spec-template.md` (§2 Fields).
- **The VO decision criterion is INVERTED — VO is the default for any validated field.**
  `conventions/domain.md` now states the rule as: a field needing ANY validation beyond
  presence/nullability (a format, a bound, a closed set) IS a value object by default; only a
  pure-presence rule or a cross-field invariant stays inline in `BuildRules`; "only one
  aggregate carries it" is not a reason to inline. A deliberately-local one-off shape check is
  the exception — marked `plain` in the §2 `VO?` column and signed off by the dev. A companion
  Final-verify smell check (`scaffold-entity/SKILL.md`) greps for `regexp`/`MatchString` inline
  in a root's/AVO's `BuildRules` and prompts extraction into a VO (investigate, not auto-fail;
  `vos/` is not swept). A field whose valid values are a FIXED, CLOSED set is ALWAYS an enum
  value object (no exception, no `plain`) — framed as a mechanical, property-based test ("are
  the allowed values a fixed list known in advance?"), explicitly NOT a Go-typing question (Go
  has no `enum` keyword; `EnumValueObject` is the framework's construct), so the agent decides
  by fact, not by the ambiguous word "enum".
- **`conventions/tests.md` follows the VO split.** Format/length/range/closed-set coverage now
  lives with the VO (tested DIRECTLY in `internal/domain/vos/` — `IsValid`/membership are plain
  methods), not as `BuildRules` branches; an AVO gets a direct `IsSameBusinessIdentity` test.
- **VO reuse is now investigated up front and approved per field.** `scaffold-entity` Phase 0b
  gains a "existing value objects (`internal/domain/vos/`)" inventory step — a field whose rule
  matches an existing VO REUSES it (never a second copy); a new VO only when none fits.
  `conventions/spec-template.md` §2 Fields gains a MANDATORY `VO?` column
  (`reuse`/`new-raw`/`new-enum`/`plain`) so the VO/reuse decision is visible, editable and
  APPROVED by the dev before generation (a blank cell = an incomplete spec, blocked by the
  existing DRAFT gate).
- **The wire→VO mapping is taught as a plain type CAST, to prevent bloated mappers.**
  `conventions/application.md` gains a "Wire → VO mapping — a CAST, not a constructor"
  section: raw/enum fields convert by a direct cast (out-of-set caught by automatic
  validation, NOT `EnumByValue` by default), nullable fields by a nil-safe pointer cast, and
  the `if x != nil` guard belongs to PATCH's tri-state `ApplyPartiallyTo` ONLY — insert/PUT
  assign unconditionally. `conventions/web.md` adds a Boundary rule that wire DTOs carry the
  VO's underlying scalar (never the VO type; don't import `vos` into `web/`), and
  `conventions/domain.md`'s VO "Boundary" note now frames `EnumByValue` as the optional
  convergence helper rather than the default mapper move.

### Fixed
- **`conventions/aggregate-children.md` no longer claims children are matched by
  `reflect.DeepEqual`.** The framework now matches an aggregate value object exclusively
  through its MANDATORY `IsSameBusinessIdentity` (an interface method — omitting it is a
  compile fail; `GetID` comes from the embedded `domain.Managed`). The trap note now teaches
  business-identity-vs-"did-anything-change", the natural-key-subset choice (vs
  `domain.IsSameByBusinessFields`) and its PUT re-send consequence, and reusing the method as
  the root's duplicate rule. Also: a value object used only by a child still lives in `vos/`
  (never `aggregatevos/` or the child's file); `aggregatevos` imports `vos`, never the reverse.

### Changed
- **Domain-layer layout updated to the THREE-package split** (`service-layout.html`):
  `conventions/domain.md` "Files" now describes `internal/domain/` (root aggregate + its
  `notifications.go` + service port), `internal/domain/vos/` (value objects + own
  `notifications.go` + `doc.go`) and `internal/domain/aggregatevos/` (children + own
  `notifications.go`) — three `notifications.go` by necessity (the `domain` package imports
  the sub-packages, so a shared file would cycle). Replaces the former single-folder,
  single-`notifications.go` description. `conventions/aggregate-children.md` now places a
  child in `aggregatevos/` and notes its VO fields auto-validate (its `BuildRules` carries
  only non-VO rules).

## [0.11.0] — 2026-08-01

### Added
- **`shared/dialects/` — one knowledge sheet per relational engine, the single home for
  per-dialect divergence.** New `plugins/omnicore/shared/dialects/{postgres,mysql,sqlserver,
  oracle,sqlite}.md` (+ a `README.md` stating the contract). Each sheet carries the axes
  where the dialects diverge — id/decimal/boolean column types, the **constraint-violation
  KEY the repo `ConstraintBinding` map binds**, active-only uniqueness, the read-side
  posture — as generic KNOWLEDGE (no SQL/Go code, by the skills' style rule), routing to the
  pinned `table-schema.html` as the authority for exact forms. The generating agent reads
  ONLY the sheet(s) for the service's target dialect(s) instead of wading through
  every-dialect prose with the exceptions as footnotes.

### Changed
- **Per-dialect facts moved OUT of scattered inline prose and INTO the shared sheets.**
  `scaffold-entity` (`SKILL.md` id-typing block + `conventions/infra.md` · `migrations.md` ·
  `sharedbase.md`) dropped their "named `<table>_<col>_key` in EVERY dialect / match by
  NAME" claims — which were false for SQLite — and now route to
  `${CLAUDE_PLUGIN_ROOT}/shared/dialects/<dialect>.md`. `configure` (engine swap) and
  `evolve-entity` (unique field add/remove) gained routing rows into the same sheets;
  `configure`'s engine-swap step now spells out re-keying every `Constraints` map to the
  target dialect's form.

### Fixed
- **`scaffold-service` — the SQLite dev loop no longer boots against an empty database.**
  Under `go run`, a relative `file:app.db` DSN was resolved to DIFFERENT files by the
  migration step and the runtime (the throwaway temp binary vs the project dir), so the
  boot logged `migrations applied` while the served `app.db` stayed empty and every request
  failed `no such table: <entity>`. The `cd`-into-project trick the wrapper relied on was
  not enough. Fixed in `templates/sqlite-mvp.md`: all three start wrappers now pin an
  ABSOLUTE `SQLITE_PATH` next to the script (recomputed each run, so the `.db` still travels
  with the project — portability kept; an explicit `SQLITE_PATH` still wins). And the
  scaffold-service final-verify was hardened — it no longer accepts `ls app.db` as proof of
  persistence (an empty 4096-byte file "appears"); it now inspects the SCHEMA and confirms
  the framework control-plane tables actually landed in the DB the runtime reads.
- **`scaffold-entity` — SQLite services no longer bind unique/PK violations by the wrong
  key.** SQLite reports a violation as the COLUMN LIST (`UNIQUE constraint failed:
  table.column`), never the index/constraint NAME the four SQL engines return — but the
  conventions told the agent constraints are "named `<table>_<col>_key` in EVERY dialect"
  and the repo "binds by NAME". On a SQLite service the agent therefore named its indexes
  `<table>_<col>_key` and bound those names, which SQLite never emits — the lookup missed,
  the raw DB error escaped unmapped, and the intended custom 409 became a generic 500 (it
  compiled and booted; only a duplicate INSERT revealed it). Fixed at the root: the shared
  `sqlite.md` sheet states the `table.column` bind-key rule prominently, the by-NAME claims
  are gone, and the Level-1 checklist gained a guard — on a SQLite service, a repo
  `Constraints` key ending `_key`/`_pkey` or the literal `PRIMARY` must hit NOTHING.
- **`scaffold-entity` — domain structs no longer get `json:` tags.** The "a persisted
  field carries `labelKey` and nothing else" rule was buried as a sub-clause of a dense
  bullet in `conventions/domain.md`, and nothing in the pre-boot checklist caught a
  violation — so the strong Go reflex of stamping `json:"..."` on every struct field won,
  and generated aggregates came out with `json:"..." labelKey:"..."` on every field (the
  canonical example's domain carries `labelKey` only). Two reinforcements: the tag rule is
  now a loud standalone rule that names the reflex and the layering reason (wire names live
  on the web-layer DTOs; a `json:"-"` even corrupts the `Old()` snapshot the framework
  builds via a json round-trip), and the Level-1 mechanical checklist gained a
  `grep -rn 'json:"' internal/domain/` → NOTHING guard (also sweeping `db:`) so a slip is
  caught before boot. `scaffold-system` inherits the fix (it delegates per entity to
  `scaffold-entity`).

## [0.10.2] — 2026-08-01

### Fixed
- **`scaffold-service` — the Phase 1 questions are now staged on the dialect
  instead of dumped in one flat round.** The old "ask in ONE consolidated round"
  rule contradicted the skill's own SQLite carve-outs (transport "asked ONLY when
  the engine is not SQLite", "no bench for SQLite", read-side "forced relational on
  SQLite"): a slot can't be skipped based on an answer collected in the same round,
  so the agent asked transport / broker / read-side alongside the dialect and
  papered over it with "(ignored if sqlite)" parentheticals — forcing a dev who
  picked SQLite to answer a broker and a read-side posture that get thrown away.
  Phase 1 now asks in two staged rounds: **Round 1** = identity + the dialect pivot
  (name, module, dialect); **Round 2** branches on the answer — SQLite asks only the
  slots that still exist (SQLite DSN, surfaces, wrappers), a full engine asks
  transport + bench + read-side + surfaces + wrappers. No more answer-to-discard,
  no more "ignored if sqlite" parentheticals. (Slots #4/#6/#8 already carried the
  correct SQLite semantics — they were simply unreachable behind the single-round
  rule.)

## [0.10.1] — 2026-08-01

### Changed
- **`scaffold-service` — SQLite DSN guidance rewritten for correctness, and the
  final verify now exercises the real file DSN.** Matches the framework fix that
  makes a relative `relational.dsn` resolve against the working directory under
  `go run` (so the dev-loop `app.db` persists in the project instead of a temp
  build dir). The skill + `templates/sqlite-mvp.md` now state the rule plainly:
  **the `.db` always lives in the app's own folder** — a relative `file:app.db`
  (the default) resolves next to the binary (portable, travels with it), and the
  dev loop lands it in the project; an absolute path is only an escape hatch for a
  fixed external location (not portable). The final-verify step boots the REAL
  `file:app.db` (not `:memory:`) and confirms the file appeared — the persistence
  it was silently failing to check before. The start-wrapper comments say where
  `app.db` is created.

## [0.10.0] — 2026-07-31

### Added
- **Relational-view awareness across the view-shaping and project-init skills** —
  the framework's `.RelationalSource(...)` read model (a plain view served straight
  from the SoR, read-your-writes, the deliberate CQRS exception for MVPs and
  freshest-possible dashboards) is now a first-class decision the skills teach and
  route, always docs-first against the pin's `relational-view` section:
  - `scaffold-service`: a new neutral, no-default Phase 1 question — **read-side
    posture** (full distributed CQRS, Mongo-projected · reduced/MVP, relational
    from the SoR) — recorded in the spec as the default backing for entity views.
    Framed as no lock-in: the bench ships full either way, so moving a view to
    Mongo later is a per-view flag (drop the marker + bump `Version` ⇒ one automatic
    online blue-green rebuild).
  - `scaffold-system`: the posture is decided ONCE at system altitude (`§1p` of the
    domain map, read from `scaffold-service/spec.md` when it just set one) and handed
    to every delegated run as the default view backing (per-entity overridable).
  - `scaffold-entity`: honors the posture when emitting the plain per-entity view —
    relational (`.RelationalSource(repo.Loader)`, root-only reads, no collection) vs
    Mongo — asking once when no posture is on record; reuses the aggregate's existing
    loader (boot guard `BoundTable()==schema.Table()`), never a second one.
  - `scaffold-view`: teaches the LIMITATION — every composition type it creates
    (ComposedView, SharedBaseView, the Embed/Link family, Upstream, aggregated) is
    relational-ineligible (boot fail, 400, or a different declaration type), so the
    option is never offered here; a plain single-aggregate listing routes to
    `scaffold-entity` instead.
  - `evolve-view`: the FLIP — adding/removing `.RelationalSource()` is a shape change
    (`Version` bump) with its two drift transitions taught (`DriftRelationalSync`,
    no rebuild ⇄ `DriftRebuildRequired`, full online blue-green rebuild).
  Mechanics stay in the pinned docs — the skills only force and route the decision;
  the capability applies on any pin that ships `relational-view`.
- **SQLite zero-infra MVP + infrastructure-posture awareness, plus a new `configure`
  skill** — the framework's SQLite engine and infra-optional boot (single pure-Go
  binary, one `app.db` or `:memory:`, no Docker/Mongo/broker/relay) are now first-class
  across the plugin, and every skill is **capability-aware, never capability-gated**:
  it warns of a posture's consequences, then OFFERS to enable what's missing (delegating
  `/omnicore:configure`), never refuses — every conversion reversible, no code lost.
  - **new skill `/omnicore:configure`** — converts a service's infrastructure posture in
    either direction (zero-infra/SQLite MVP ⇄ full distributed CQRS: add/remove Mongo +
    broker + CDC relay + docker), swaps the relational engine (porting migrations to the
    target dialect; data ETL flagged as the dev's), switches transport (kafka ⇄ nats),
    and tunes the `microservice.*.yaml` / devops glue. Docs-first, plan-gated; delegates
    each view flip to `evolve-view`, verification to `run`; reuses `scaffold-service`'s
    devops templates.
  - **new template `scaffold-service/templates/sqlite-mvp.md`** — the SQLite zero-infra
    glue: `CGO_ENABLED=0 -tags sqlite` start wrappers (no compose), `file:app.db` /
    `:memory:` DSN, `.gitignore` for the `app.db*` sidecars.
  - `scaffold-service`: `sqlite` joins the engine set as the decisive zero-infra MVP
    engine — picking it collapses the transport/bench/read-side questions (no Mongo, no
    broker, no Docker; tagless), records the posture, and states plainly that it's not
    optimized for it (canonical path is CDC + Mongo), that integration events + Mongo
    projections belong to the standard path, and that switching later is a reversible
    `/omnicore:configure` run.
  - `scaffold-view` / `evolve-view`: on an infra-free project a Mongo-only view (or a
    flip to Mongo) is never refused — the skill offers to enable Mongo via `configure`.
  - `implement`: a capability the framework offers but the current posture lacks the
    infra for (integration-event publish without a broker, anything Mongo on SQLite) is
    NOT an honest-no — it offers `/omnicore:configure` to enable it.
  - `run`: follows the chosen infra — a SQLite/infra-free project boots with no bench
    (`CGO_ENABLED=0 -tags sqlite`, no compose), and absent-by-design infra is never
    reported as unreachable.
  - `scaffold-system` / `scaffold-entity` / `doctor`: posture-aware — the domain map
    records the engine/infra choice (Mongo views + integration events deferred, never
    dropped); SQLite type/DDL specifics route to `table-schema`; and "writes 2xx, views
    never arrive" on an infra-free project is diagnosed as by-design, not a fault.
  Mechanics stay in the pinned docs (`relational-view`, `yaml-reference`, `transport`,
  `table-schema`, `integration-events`); the skills only teach, route and offer.

## [0.9.0] — 2026-07-30

### Added
- **Explicit ARCHIVE-regime decision gates** in the view-shaping skills — the
  read-side archive behavior is never left to a silent default:
  - `scaffold-view`: the spec gate's Consistency contract now forces, per
    embedded/linked segment, the follow-the-source vs retain-regardless
    decision (plus the view root's own kept-hidden vs `DeleteOnArchive()`
    choice), routing to the pin's `views` section for the exact lever;
  - `evolve-view`: the impact map flags when a change to a segment's projected
    fields or lifecycle FLIPS its archive regime — a shape change (`Version`
    bump ⇒ rebuild) that also changes what consumers see on default reads;
  - `scaffold-entity`: when Archive is among the chosen modes, the entity
    view's regime (kept-hidden default vs `DeleteOnArchive()`) is settled in
    the same question.
  Mechanics stay in the pinned docs — the skills only force the decision.
  Pairs with framework v0.39.x (`JoinView(...).Fields` — the per-leg allowlist
  whose `"DeletedAt"` entry is the segment's archive switch), while the
  decision gates themselves apply on every pin (the lever set is the pin's).

### Changed
- **`scaffold-entity` stops naming the archive-column builder** — the
  `Modes()` consistency invariant now names the CONCEPT (the schema's
  archive/deleted-at column declaration) and routes to the pin's table-schema
  docs for the builder name, staying correct on released pins (`SoftDelete`)
  and on v0.39.x+ (`DeletedAt`) alike, as the version-agnostic design intends.
  Prose sweeps soft-delete → archive vocabulary across the conventions
  ("archive column", "archive stamp", "default archive"); "soft removal" as
  the non-destructive-write concept stays.

## [0.8.4] — 2026-07-25

### Changed
- Skill references updated to track the framework's read-side surface renames
  (they ship together with the framework release that carries them):
  `core.NewSharedBase` → `core.NewSharedBaseSchema`, and
  `SharedBaseView(base, name)` → `SharedBaseView(name).Schema(base)` (the base
  schema now attaches via `.Schema(...)` like a regular view). Touches
  `scaffold-entity` (impact map + shared-base convention) and `scaffold-system`.
  The framework also removed the view `.Root(table)` builder (the root now
  derives from the attached schema); the skills never spelled out `.Root()`, so
  no skill change was needed there.
- Doc-routing pointers realigned to the framework's new consolidated **`views`**
  section (`docs/content/sections/views.html`, introduced in framework v0.37.0),
  which centralizes all read-side view declaration — the three view kinds, the
  view-exclusive external schema, `Embed`/`EmbedMany`, `SharedBaseView`,
  `ComposedView`, and the SyncEngine/recompose fan-out. `scaffold-view`,
  `evolve-view`, `scaffold-system`, and `remove-entity` now route view-kind /
  composition-type / view-shape questions to `views` instead of the former
  `query-side` + `table-schema` split. Write-side shared-base normalization
  references (`scaffold-entity`, and the base-schema rows) still point to
  `table-schema`, which retains that write-side material.

## [0.8.3] — 2026-07-17

### Changed
- `help`: version check + plugin self-check now fire on the session's FIRST turn
  — explicitly including a bare `/omnicore:help` that only prints the
  orientation greeting, no longer deferred to the first substantive answer. The
  plugin self-check must actually read the local `plugin.json` AND fetch the
  published one that turn (not assume a prior turn did it). (Observed: repeated
  no-question `/omnicore:help` invocations never surfaced an available plugin
  update because the checks were gated behind answering a question; the running
  install was genuinely behind — 0.8.1 vs 0.8.2 published.) Note: the check is
  prompt-driven, so a stale fetch cache (WebFetch's ~15-min per-URL cache /
  raw.githubusercontent's CDN) can still delay detection until it expires — this
  narrows the miss, it does not eliminate it.

## [0.8.2] — 2026-07-17

### Changed
- `help`: doc-URL resolution on the published site hardened. Section file names
  come from the Documentation Map ONLY — never derived from the concept's
  wording (the names are asymmetric and unguessable: read side is
  `query-side.html` not `query-handler`, write side is `command-handler.html`
  not `command-side`). The index is a single-page app, so its nav can't be
  scraped from a plain fetch of `/`; a `sections/<name>.html` that 404s means
  the name was a guess — STOP and get the real one from the Map, never
  improvise another URL. Inline concept list corrected accordingly
  (`command/query-handler` → `command-handler · query-side`). (Observed: a
  session guessed `query-handler.html`, hit a 404, then escalated to a raw
  GitHub URL of the framework repo.)
- `help`: added a hard guardrail against fetching
  `raw.githubusercontent.com/ClaudioSchirmer/omnicore/…` — the framework repo
  is PRIVATE, so every raw URL 404s regardless of path or branch (that failure
  reads as "missing docs" but isn't). The only sanctioned remote for framework
  docs is the published Pages site; the only legitimate raw-GitHub fetch stays
  the PUBLIC `omnicore-plugin` repo in the plugin self-check.
- `help`: "Never guess — verify" now covers claims of ABSENCE. A confident "no"
  is a claim like any other — before telling the dev their premise is mistaken
  or that a capability doesn't exist, read the section that would OWN it; never
  let a strong prior stand in for a read. Concretely: "reads come from Mongo" is
  true of the query path but not the whole story — the write path has its own
  read/aggregate primitives (count, sum, group-by, uniqueness probes) whose
  purpose is enforcing business rules, in the write-side handler section, not
  the query side. (Observed: a session denied write-side aggregation exists and
  called a correct premise a misunderstanding.)

## [0.8.1] — 2026-07-17

### Changed
- `help`: "Never guess — verify" now covers counting/enumeration questions —
  reproduce the doc's OWN taxonomy (its tables, headings and terms decide what
  counts as an X and what is merely a wrapper/variant of one), never
  re-classify, merge or promote categories the doc keeps distinct. (Observed: a
  session counted the read-side HTTP/export wrappers as auto-handlers,
  answering 11 where the manual's own table says 9 — 7 write + 2 read.)

## [0.8.0] — 2026-07-17

### Added
- **All 12 skills: plugin self-check.** Once per run, during preflight, each
  skill compares its own installed plugin version
  (`${CLAUDE_PLUGIN_ROOT}/.claude-plugin/plugin.json`) with the published one
  (the same file on the marketplace repo's `main`, read over raw.githubusercontent)
  and, when behind, rides ONE non-blocking line along with the next reply
  handing the dev the update command (`claude plugin update omnicore@omnicore`;
  `/plugin marketplace update omnicore` first if the marketplace is stale).
  Offline → silent skip; never a gate — the run continues on the installed
  skills and the update takes effect next session.

## [0.7.0] — 2026-07-17

### Changed
- **Oracle joins the dialect set across the skills** (framework v0.33.0 shipped it
  as the fourth first-class engine, Oracle Database 23ai+). Every "today's latest"
  dialect hint now reads `postgres|mysql|sqlserver|oracle` (`scaffold-service`,
  `run`, `upgrade`); the closed sets remain read from the PINNED docs, which stay
  the authority.
- `scaffold-service`: new **Oracle relay** trap (database-level LogMiner
  provisioning at first DB boot + per-table supplemental logging after the first
  app boot, CLOB CDC payloads by framework design) and the oracle DSN exception
  (no `<svc>_db` — the app connects to the `FREEPDB1` PDB as the app user;
  `ORACLE_PASSWORD` is the separate admin password). Port scan covers `1521`.
- `templates/docker-bench.md`: **Relational — oracle variant** (gvenzl
  `oracle-free` PINNED to a 23ai Release Update — the floating `:23` ships the
  "26ai" banner Debezium's version parser fails on; `APP_USER` envs; init-scripts
  contract: app grants incl. the documented `GRANT EXECUTE ON SYS.DBMS_LOCK`, and
  CDC provisioning — ARCHIVELOG + bounded FRA, `logminer_tbs`, the `c##dbzuser`
  COMMON user per Debezium's documented grant set, the seeded heartbeat table) +
  two wrapper arms: idempotent per-table supplemental logging (background, the
  sqlserver pattern) and the NATS-only `DEBEZIUM_HEARTBEAT` stream pre-create.
- `templates/cdc-relay.md`: **Source — oracle** block (OracleConnector over
  LogMiner: CDB+PDB pair, `lob.enabled` for the CLOB payloads,
  `skip.unparseable.ddl` + `store.only.captured.tables.ddl`, tight mining
  cadence, `heartbeat.action.query`) + the oracle-only EventRouter override
  (UPPERCASE catalog field names with lowercase header aliases — wire contract
  identical across dialects) and the UPPERCASE predicate pattern.
- `help`: the version heads-up grew into a **version check** — behind the latest
  now OPENS the first answer with a loud warning (grounded in the pinned vX while
  vY is out) pointing at `/omnicore:upgrade` (the bump's owner — the stale
  scaffold-skill/raw-`go get` pointers are gone) and offering the changelog —
  answers stay SCOPED to the pin (a feature that only exists in a newer release
  is named as such, never explained as if available); and
  the **no-project case is now defined**: never "I can't tell without a project" —
  ask once whether to read the published site
  (https://claudioschirmer.github.io/omnicore, always the LATEST release, section
  URLs mirror the Documentation Map names) or `go mod download` the latest for
  local docs, opening the answer with which ground it's on. With a pinned project
  the pin's module-cache docs remain the authority (the site would reintroduce
  version drift); the site also serves as the online changelog source.
- `scaffold-entity`: final-verify UUID grep extended to `migrations/oracle/`
  (`RAW(16)` ids, `VARCHAR2(36)` in the reject pattern); conventions updated —
  id/FK types add Oracle `RAW(16)` (`VARCHAR2(36)` for uuid-valued text), the PK
  name on oracle is `<table>_pkey` named explicitly (like sqlserver), the
  self-documenting DDL mechanism on oracle is `COMMENT ON TABLE/COLUMN` (the
  Postgres shape, single statements — compatible with the runner's plain-SQL
  split), and active-only uniqueness names all four dialect mechanisms (routing
  to the pinned `table-schema.html`, which now covers them).

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
