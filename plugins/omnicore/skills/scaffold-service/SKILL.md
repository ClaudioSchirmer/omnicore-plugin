---
name: scaffold-service
description: >-
  omnicore: create a brand-new omnicore service from an empty directory — go.mod pinned to a
  published omnicore release, the bootable empty bootstrap shell, the
  microservice.*.yaml profiles, the migrations skeleton, and EITHER a local docker bench
  (relational DB + Mongo + broker + Debezium CDC relay) OR a zero-infra SQLite MVP (single
  pure-Go binary, one app.db or :memory:, no Docker) — then prove the shell boots.
  Use when the user wants to start/initialize/set up a NEW omnicore-based microservice
  or its local environment from scratch. Counterpart of scaffold-entity, which adds
  entities to an EXISTING omnicore service.
---

# scaffold-service

Create the smallest thing that is honestly a running omnicore service: an EMPTY shell
that compiles, boots, answers its probes, and has a live CDC pipeline waiting for its
first entity. Entities are NOT this skill's job — hand off to `scaffold-entity`. Two
postures: the full CDC bench (canonical), or — with SQLite — a zero-infra MVP that needs
no Docker at all. Both boot to green; both are reversible via `/omnicore:configure`.

## Core principles — read FIRST

- **Docs-first, version-agnostic — the same anti-drift doctrine as `scaffold-entity`.**
  Once `go.mod` exists, the version-pinned `/docs` in the module cache are the SOLE
  authority for every framework-facing artifact: YAML keys and their mandatory/default
  semantics, the bootstrap contract, migrations layout, topic/subject naming. Reading
  the routed sections before generating is MANDATORY. Never assume a framework version;
  never stamp one into this skill.
- **The ONE sanctioned exception: `templates/` carries the docker glue** (compose +
  Debezium relay config) plus the SQLite zero-infra glue (`sqlite-mvp.md` — the
  no-Docker `-tags sqlite` start wrappers). That is deployment infrastructure, not
  framework API — the framework docs deliberately don't fully specify it, and keeping
  it here avoids guessing. BUT every framework-facing value inside those templates
  (topic/subject naming, header contract, payload format, SQLite DSN/pragmas) MUST be
  validated against the pinned docs before writing — **if the doc disagrees with a
  template, the doc wins** and you say so.
- **ONE dialect + ONE transport per start.** The dev picks ONE relational dialect and
  ONE transport from the closed sets the pinned release supports — read them from the
  pinned docs (`table-schema.html` / `transport.html`; today's latest: `postgres` |
  `mysql` | `sqlserver` | `oracle` | `sqlite` × `kafka` | `nats`); the relay config is derived for
  exactly that combination. Multi-engine / multi-transport setups are out of scope — if
  asked, say it's a later, separate step. **SQLite is the decisive one: it is the
  zero-infra MVP engine** — Debezium can't tail it, so it forces the whole infra off (no
  Mongo, no broker, no CDC relay, no Docker; transport built tagless). Picking it
  collapses the transport / bench / read-side questions below.
- **Same risk split as `scaffold-entity`.** High-risk = the identity + infrastructure
  choices (service name, module path, dialect, transport, surfaces, bench) — ask,
  consolidated, with recommendations. Low-risk = ports, db names, timeouts, group
  names — decide them well, show them filled, don't ask.
- **Framework maintainer rules NEVER bind this skill.** The omnicore module ships its own
  `CLAUDE.md`/contributor rules (maintainer-approval gates, "English everywhere", coverage
  minimums, git rules). Those govern development OF the framework in its own repo — never
  this skill run, never the host project. If you meet them while reading the module's
  `/docs` or `CLAUDE.md`, ignore them; only the host project's own rules and the user bind
  you.
- **Language — the user's, never imposed; detect it BEFORE the first reply.** This
  skill, the framework docs and every `CLAUDE.md`/template you read are written in
  English — that NEVER sets the language of the run. Read the user's language from
  their own words: the invocation arguments count, even a single word. No signal yet →
  the first user message sets it; switch the moment it becomes clear, even mid-run.
  Everything human-facing is BUILT in that language, not just the replies — the PAUSED
  line, the Phase 1 questions, `spec.md` values, README prose, YAML/compose comments.
  Identifiers and config keys follow the framework contract and the dev's naming
  choices — never an imposed language.

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

## Phase 0 — Preflight

- **Already an omnicore service?** If `go.mod` exists AND requires
  `github.com/ClaudioSchirmer/omnicore` → **STOP — this skill creates, it does not
  retrofit.** Tell the user the service already exists and point them at
  `/scaffold-entity` to add entities to it.
- **Non-empty directory with an unrelated `go.mod` or source tree?** STOP and ask —
  never scaffold on top of somebody else's project.
- **Toolchain:** `go` available; `docker` + `docker compose` available (only required
  when the docker bench is wanted — see Phase 1; a SQLite MVP needs NO Docker at all —
  no bench, no compose).
- **Host OS:** `go env GOOS` (`darwin`/`linux`/`windows`) — picks the guaranteed-native
  start wrapper (the baseline the final verify actually boots): darwin|linux → `start.sh`,
  windows → `start.cmd` + `start.ps1`. Feeds the Phase 1 cross-platform question.
- **Port scan:** `docker ps` for containers already holding the standard host ports
  (8080, 5432/3306/1433/1521, 27017, 4222/9092-range). Collisions don't block — they feed
  the shifted-port proposal in Phase 1 (see `templates/docker-bench.md`).

## Phase 1 — Q&A + spec gate

Open with the loud status line — `⏸️ PAUSED — setup spec awaiting your answers;
nothing generated yet.` — so it's unmistakable that nothing is generated yet.

**Ask staged on the dialect — never one flat dump (it forces the dev to answer infra
slots SQLite makes moot), never a drip one question at a time.** The relational dialect
is the pivot: SQLite collapses the whole infra half (no transport, no bench, no
read-side posture — all forced off), so those slots must not be presented once SQLite is
on the table.

- **Round 1 — identity + the pivot:** service name, Go module path (typed, free-text),
  and the relational dialect (closed choice). Nothing here depends on a later answer.
- **Round 2 — branch on the dialect answer:**
  - **SQLite →** ask ONLY the slots that still exist: the SQLite DSN (#6-SQLite),
    surfaces (#5), cross-platform wrappers (#7). Do NOT ask transport, docker bench, or
    read-side posture — SQLite forces them (tagless / no bench / relational). No
    "ignored if sqlite" parentheticals, no answering-to-discard.
  - **Full engine (postgres|mysql|sqlserver|oracle) →** ask transport (#4), surfaces
    (#5), docker bench (#6), cross-platform wrappers (#7), read-side posture (#8) — all
    in this one round.

If the dev already named the dialect in their invocation ("scaffold a sqlite service"),
the pivot is settled — skip Round 1's dialect slot and open directly on the right
branch. **The agent chooses the medium** — a structured multiple-choice prompt (e.g.
AskUserQuestion) fits the closed-choice slots; the typed slots (service name, module
path) are plain text. Either medium is fine — lead every round with the loud PAUSED line.

High-risk slots — asked per the staged branch above (transport/bench/read-side only on a
full engine; the SQLite DSN only on SQLite), mark recommendations `(proposed)`:
1. **Service name** (kebab-case). Seeds `service:`, the databases (`<svc>_db`,
   `<svc>_views`), the sync group (`<svc>-sync`), the compose project (`<svc>-dev`)
   and container names. No default possible.
2. **Go module path** (e.g. `github.com/org/<svc>`). No default possible.
3. **Relational dialect** — the closed set the pinned release supports, read from the
   pinned `table-schema.html` (today's latest: `postgres` | `mysql` | `sqlserver` | `oracle` |
   `sqlite`). The framework itself refuses a default — so does this skill. Neutral advice:
   match what production will run. **`sqlite` is decisive**: it is the zero-infra MVP
   engine and collapses the transport / bench / read-side questions (see the SQLite block
   below); the others take the full-infra path.
4. **Transport: `kafka` | `nats`** — asked ONLY when the engine is not SQLite (SQLite is
   tagless: no broker). Same advice: match production; NATS is the lighter local bench
   when there's no constraint yet. No broker ⇒ no integration events (they ride the CDC
   relay — the canonical path).
5. **Surfaces** — one question, three parts: OpenAPI UI `(proposed: yes, /docs +
   rootRedirect)`; GraphQL `(proposed: no)`; gRPC `(proposed: no)`. All additive
   later without rework — say so, no manufactured urgency.
6. **Docker bench** — `(proposed: yes)` generate `devops/` (DB + Mongo + broker +
   CDC relay, one compose). Alternative: point at EXISTING infra — then ask only for
   the endpoints (relational DSN, Mongo URI, broker endpoints), skip `devops/`
   entirely, and warn plainly: without a CDC relay tailing the outbox, the read side
   never projects — `templates/cdc-relay.md` is the reference for wiring their own. For
   **SQLite there is NO bench (zero Docker)** — skip this entirely and instead ask the
   SQLite DSN: a file path — `(proposed: file:app.db)`, relative so the `.db` lives IN THE
   APP'S OWN FOLDER (portable — travels with the binary); an absolute path is an escape
   hatch for a fixed external location (not portable) — or `:memory:` (ephemeral, RAM-only).
7. **Cross-platform start wrappers** — the host-native wrapper (from `go env GOOS`,
   Phase 0) ALWAYS ships and is the one the final verify boots. Also generate the OTHER
   platform's? `(proposed: yes)` — on a Unix host that adds `start.cmd` + `start.ps1`
   for Windows teammates; on a Windows host it adds `start.sh`. Purely additive, no
   rework — decline if the team is single-OS. Record the resolved set in the spec.
8. **Read-side posture** — HOW entity read models are served, asked NEUTRALLY, NO
   default: an empty dir is equally likely an MVP or a seasoned team's solid service,
   and we can't tell which. The two postures, their honest trade-offs, the capability
   rule, the no-lock-in truth and the wording discipline are OWNED by
   `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` — read it BEFORE wording the question,
   and route to it instead of restating (consult `relational-view` at the pin only for
   version-exact capability answers). **On SQLite the posture is forced relational** —
   nothing to ask. Record the posture in the spec; it's handed to `scaffold-entity`
   as the DEFAULT backing per entity view (still per-entity overridable there).

**When the engine is SQLite — the zero-infra MVP posture (say all of this, calmly).**
Picking SQLite auto-resolves the infra questions and the spec records them: no `mongo`,
no `transport` (tagless), no `devops/`, no Docker — one pure-Go binary
(`CGO_ENABLED=0 -tags sqlite`) against a `file:app.db` or a `:memory:` database (RAM-only,
data ephemeral). All entity views are served relational from the SoR (read-your-writes).
DSN: ship the relative default `${SQLITE_PATH:file:app.db}` — the `.db` is created next to
the binary (portable: a pendrive carries app + `.db`; under `go run` it falls back to the
project dir, which is why the wrappers `cd` there first). An absolute path is honored
verbatim if the dev explicitly wants a fixed external location — don't propose one by
default (it's not portable).

Be honest AND reassuring:
- **Great for an MVP / a demo / a single-node tool** — it stands up with zero moving parts.
- **The framework is NOT optimized for this** — the canonical path is CDC + MongoDB; a
  relational view re-composes the aggregate per read, and only plain per-entity views
  are servable (what they serve vs reject, and what the posture never constrains —
  write-side modeling — per `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`, the owner).
- **Integration events and Mongo projections don't exist here** (both ride the CDC relay,
  which needs a Debezium-tailable engine).
- **Fully reversible, no code lost.** Every layer above infra is identical to a full
  service — switching to Postgres/MySQL/SQL Server/Oracle (then gaining Mongo, composed/
  shared views and integration events) is a `/omnicore:configure` run: it stands up the
  devops, Mongo, broker and CDC relay, re-asks the infra questions, and ports the
  migrations to the new dialect's SQL. You lose nothing by starting on SQLite.

Low-risk — decide and SHOW filled, don't ask: **the omnicore version — ALWAYS the
latest published release, resolved at generation time (`@latest`); never a question**
(honor an explicit pin only if the dev demanded one unprompted, and record it in the
spec), the working language for human-facing text (detected per the Language
principle — invocation args count, even one word; recorded in the spec — the dev can
override at the gate), host ports (standard, or shifted when Phase 0 found collisions — every endpoint
env-overridable via `${VAR:default}` so the YAML never needs edits to repoint),
database/group/container names from the service name, `migrations.dir
./migrations/<dialect>`, dev-profile autoRun defaults, audit `slog` in dev, shutdown
defaults, the prd template's JWT `${VARS}` placeholders.

Write the resolved answers to **`scaffold-service/spec.md`** (project root, visible,
one small file — no per-layer task files; generation is one pass): every slot filled,
`Status: DRAFT`. **Hard STOP** until the dev answers; a plain "ok" accepts every
`(proposed)` pick, but slots with no default (name, module, dialect, transport) MUST
be answered. Then flip `Status: APPROVED` and generate.

**Gate collapse — answers ARE the approval.** When EVERY high-risk slot was answered
by the dev interactively in this conversation (none defaulted, none guessed), the
spec gate is already satisfied: write `spec.md` with the answers, flip it straight to
`Status: APPROVED`, and proceed — do not re-ask what was just answered. The hard STOP
applies only when any high-risk slot would otherwise be filled by you.

## Phase 2 — Generate (order is load-bearing)

1. **`go.mod` first** — module path + the current Go toolchain version + the omnicore
   require. Then `GOFLAGS=-mod=mod go get github.com/ClaudioSchirmer/omnicore@latest`
   — ALWAYS the most current published release (an explicit dev-demanded pin is the
   only exception) — so the pinned module — **and its `/docs`** — lands in the module
   cache. The proxy's `@latest` endpoint can lag a just-published tag: cross-check
   with `go list -m -versions github.com/ClaudioSchirmer/omnicore` and, if a newer
   release is listed, `go get` that one instead. Until this step the docs don't exist
   locally; nothing else may be generated before them. Record the resolved version in
   the spec.
2. **READ the pinned docs** (resolution — same rule as `scaffold-entity`: the section
   file is `<omnicore-dir>/docs/content/sections/<name>.html` where `<omnicore-dir>` =
   `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`):
   - `bootstrap.html` — `Run`/`Wire` contract, the four env vars, boot order.
   - `yaml-reference.html` — every block you will write: mandatory keys, defaults,
     profile semantics (dev-only unlocks), `${...}` substitution forms.
   - `migrations.html` — dir layout, autoRun modes, the framework-vs-service sequence.
   - `transport.html` — the broker contract + the CDC-relay reference (validates the
     `templates/` values: topic/subject naming, `simplestring` payload, headers).
   - `service-layout.html` — skeleton naming, when the pinned version ships it.
   - **SQLite only:** `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (the zero-infra MVP
     posture — all views relational; the owner) + `table-schema.html` (Go↔SQLite types,
     `:memory:`, ASCII case-fold, decimal-as-TEXT, the forced correctness pragmas) +
     `yaml-reference.html` (the `mongo`/`transport` opt-out-by-absence semantics).
3. **`bootstrap/main.go` + `bootstrap/wire.go`** — the empty shell: `bootstrap.Run`
   wired to an empty `Wiring`, with a comment pointing at `scaffold-entity` as the way
   to add the first entity. Derive the exact signatures from `bootstrap.html` — this
   skill carries no Go by design. Two wiring rules:
   - **OpenAPI surface chosen ⇒ set `Wiring.OpenAPI`** (Title from the service name,
     an initial Version) — the yaml `openapi:` block only tunes HOW the UI is served
     and is ignored unless `Wiring.OpenAPI` is set; without it the `/docs` check in
     the final verify can never pass. Derive the config type/fields from
     `bootstrap.html` / `openapi.html`.
   - **NO translations, NO features, NO `BeforeServe`.** The framework's dev profile
     accepts the fully empty shell (features + translations arrive with the first
     entity — they are `scaffold-entity`'s job; translations on a shell with zero
     features are dead weight the entity run would have to reconcile).
4. **`microservice.dev.yaml`** — MINIMAL, not a reference dump: `service`, `http`, the
   mandatory `relational` / `mongo` / `transport` blocks, `migrations`, the chosen
   surfaces (`openapi` / `graphql`), `auth: {mode: disabled}` (dev-only), `audit:
   [slog]`, `shutdown`. Every endpoint `${VAR:default}` — and the DEFAULT half MUST
   match the compose bench EXACTLY, never a generic placeholder: relational DSN with
   user/pass `omnicore:omnicore` on db `<svc>_db` at `localhost:<published port>`, the
   mongo URI and broker endpoints likewise. A `user:password`-style default boots the
   shell against creds the container never created and `readyz` fails auth. Omit optional
   blocks whose framework defaults already do the right thing — the yaml-reference is the
   manual; the profile file is not. **SQLite ⇒ OMIT the `mongo` and `transport` blocks
   entirely** (opt-out by absence); `relational.dsn` default `${SQLITE_PATH:file:app.db}`
   (or `:memory:` when the dev chose ephemeral) — no compose creds to match.
5. **`microservice.prd.yaml`** — the honest template: same core, `auth.mode: jwt` with
   `${JWT_ISSUER}` / `${JWT_AUDIENCE}` / `${JWKS_URL}` placeholders (prd without an
   `auth` block aborts boot — that's WHY the template ships), **AND
   `auth.publicRoutes: ["GET /livez", "GET /readyz"]` — the `METHOD /path` form is
   MANDATORY** (a bare path without the method fails `parsePublicRoutes` and aborts
   boot). Probes are framework-registered but NOT auto-public; under jwt a tokenless
   kubelet gets 401 and the orchestrator kills a healthy pod. Entries are validated at
   boot against the registered route set (exact-match: a typo / wrong method / trailing
   slash ABORTS boot; path-param routes can't be listed — those use `Doc.Public` — see
   `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md`). Endpoints as pure
   `${VARS}` with no localhost defaults, no playground/introspection.
6. **`migrations/<dialect>/.gitkeep`** — empty; the service's own sequence starts at
   `0001` when the first entity arrives (that is `scaffold-entity`'s job). Do NOT
   invent a placeholder `0001_init` no-op pair — the framework treats an empty
   service sequence as legitimate (`Up` applies its own control plane and skips the
   service stage), and a placeholder would steal the `0001` slot the first entity is
   promised.
7. **`.gitignore`** — binaries, `go.work*`, `.env*`, OS/editor files, `devops/` local
   data dirs, the `scaffold-service/` + `scaffold-entity/` working dirs (commented,
   the dev's call).
8. **`devops/docker-compose.yml` + `devops/debezium/application.properties`** — from
   `templates/docker-bench.md` + `templates/cdc-relay.md`, instantiated for the ONE
   chosen dialect × transport combination, names/ports from the spec. Validate the
   framework-facing values against `transport.html` (step 2) before writing. **SKIPPED
   entirely for SQLite** — no `devops/`, no compose, no relay (zero Docker).
9. **Start wrappers — the set resolved in the spec (Phase 1 #7).** The one-command dev
   loop: compose up + wait healthy, then `APP_PROFILE=dev go run -tags '<engine>
   <transport>' ./bootstrap` (dev profile, always). ALWAYS write the host-native wrapper
   (`start.sh` on darwin|linux; `start.cmd` + `start.ps1` on windows); ALSO write the
   other platform's when #7 was accepted (`start.cmd` = zero-friction batch, `start.ps1`
   = robust PowerShell, `start.sh` = bash/WSL). Whatever ships stays in lockstep — same
   steps in every wrapper. Skipped (compose half) when the dev chose existing infra.
   **For SQLite, from `templates/sqlite-mvp.md`** — no compose, just
   `CGO_ENABLED=0 APP_PROFILE=dev go run -tags sqlite ./bootstrap`.
10. **Resolve deps:** `go mod tidy` **then** `GOFLAGS=-mod=mod go build -o /dev/null
    -tags '<engine> <transport>' ./bootstrap` (`-o` is required: the default output
    name `bootstrap` collides with the directory) — tidy alone
    CANNOT see the tag-gated transport dependency behind `//go:build kafka|nats`, so
    the build must be allowed to add its go.sum entries. Both `go.mod` AND `go.sum`
    ship — never one without the other. **SQLite:** `CGO_ENABLED=0 GOFLAGS=-mod=mod go
    build -o /dev/null -tags sqlite ./bootstrap` (engine tag only, transport tagless).

## Final verify (the gate — non-negotiable)

**Level 0 — the reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`):
after the items below pass, reopen the spec and walk ITS promises item by item with
real evidence; an unmet stated target is RED or an explicit dev-accepted deviation.

1. **Bench healthy** — every compose healthcheck green (when the bench was generated).
   N/A for SQLite (no bench).
1b. **prd static sanity — the prd profile is NEVER boot-tested here (only dev boots),
   so check it statically and say so plainly in the report:** `auth` block present with
   `mode: jwt`; `auth.publicRoutes` contains the exact entries `GET /livez` and
   `GET /readyz` (the `METHOD /path` form — a bare path aborts boot,
   `shared/boot-contract.md`); every endpoint a pure `${VAR}` with no localhost
   default; mandatory blocks for the chosen posture present. A prd that only fails at
   the first real deploy is this skill's failure.
2. **`gofmt -l`, `go vet`, `go build -o /dev/null -tags '<engine> <transport>' ./bootstrap`** — format
   (gofmt clean) + vet + compile. Both linters are first-party Go tools (no install).
   SQLite: `CGO_ENABLED=0 ... -tags sqlite` (engine only, transport tagless).
3. **Boot** with `APP_PROFILE=dev`: `/livez` 200 AND `/readyz` 200 (readyz proves the
   relational + Mongo request paths answer — on SQLite the Mongo path is absent, readyz
   proves the relational path only). **SQLite with a file DSN — boot the REAL DSN, not
   `:memory:`.** The point of the file posture is persistence; verify it. Boot via the
   start wrapper (which pins an absolute `SQLITE_PATH` next to the project), then CONFIRM
   THE MIGRATIONS ACTUALLY PERSISTED to the DB the runtime reads — not merely that a file
   appeared. **`ls app.db` is NOT enough**: a 4096-byte empty `app.db` "appears" yet holds
   ZERO tables when the relative-DSN split bit (the log says `migrations applied` but they
   were written to a DIFFERENT file, and every request then fails `no such table`). Inspect
   the SCHEMA — `sqlite3 app.db ".tables"` (or `SELECT count(*) FROM sqlite_master WHERE
   type='table'`) — and confirm the framework control-plane tables are present
   (`omnicore_migrations`, `outbox`, …; on an empty shell there are no entity tables yet,
   the framework ones are the proof). Empty schema WITH `migrations applied` in the log IS
   the relative-`file:app.db`-under-`go run` regression — the fix is the wrapper handing an
   absolute `SQLITE_PATH` (`templates/sqlite-mvp.md`), never a relative fallback. It is
   already in `.gitignore`; leave it (or remove it after the check and say so — never claim
   persistence you did not observe). Only use `:memory:` when the dev chose
   ephemeral. **Every approved surface knob must be IN the
   yaml and observable**: OpenAPI UI approved ⇒ `openapi:` block present AND its uiPath
   answers 200; `rootRedirect` approved ⇒ `GET /` answers 302 (the framework default is
   false — omitting the block silently drops the approved behavior into a `GET /` 404).
   The framework's dev profile boots the empty shell by design (a loud warn is
   expected in the log — it is the confirmation, not a problem). **Degradation:**
   if the boot instead aborts with "nothing to serve", the pinned omnicore predates
   the dev empty-shell boot — REPORT that to the user and stop (upgrading the pin is
   their call); do NOT work around it with a no-op `BeforeServe`/placeholder feature.
4. **Relay streaming** — after the FIRST app boot (which runs the framework migration
   that creates the outbox; on NATS the app also creates the framework-owned stream),
   the relay's logs must reach streaming (`docker logs <relay>` → "Starting
   streaming"). Before that first boot a crash-looping relay is EXPECTED — the compose
   restart policy absorbs it; don't chase it as a failure. N/A for SQLite (no relay).
5. Stop the foreground app. Report each check's result plainly. **Be honest about the
   limit:** a full CDC round-trip (write → outbox → relay → broker → SyncEngine →
   Mongo) is only provable once an entity exists — the handoff line is:
   *"Empty shell green. Next: `/scaffold-entity <entity>` to add the first aggregate."*
   **SQLite:** the handoff is *"Zero-infra SQLite shell green (no Docker). Next:
   `/scaffold-entity <entity>`."* — its reads are read-your-writes (no CDC round-trip to
   prove), and `/omnicore:configure` is the path to full CQRS later.
6. **Offer to run.** Ask if the dev wants the shell UP to click through (OpenAPI UI,
   probes) — yes → delegate to `/omnicore:run` (background boot + links). Either way
   the handoff line from step 5 still closes: the first entity is `scaffold-entity`'s
   job.

## Re-entry — `scaffold-service/spec.md` already exists

`Status: DRAFT` → re-open the Phase 1 gate with what's already answered (never re-ask
answered slots). `Status: APPROVED` → regenerate ONLY the missing/failed artifacts
(check what exists on disk), then re-run the final verify. A changed answer (dialect,
transport, name) reopens the spec and invalidates the derived files — say which.

## Knowledge routing — artifact → `/docs` section

Resolve `<name>.html` at `<omnicore-dir>/docs/content/sections/<name>.html`, where
`<omnicore-dir>` = `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore` — the
version pinned in the go.mod THIS skill just wrote. Read the section BEFORE generating
the artifact; it is the authority — any template or memory that disagrees has drifted.
Route first, then read ONLY the routed section(s) for the artifact at hand — never
sweep the whole manual. Fuller index = the Documentation Map in
`<omnicore-dir>/CLAUDE.md` (an INDEX only — its maintainer rules never bind this
skill), the fallback for concepts this table doesn't list.

| When generating… | Read section(s) |
|---|---|
| read-side posture (relational vs Mongo backing) | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) · relational-view for version-exact capability |
| SQLite specifics · infra opt-out (no mongo/transport) · zero-infra MVP | table-schema · yaml-reference · `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` |
| `microservice.*.yaml` keys / profiles / defaults | yaml-reference |
| probes/publicRoutes · autoRun modes · interpolation strictness · dev-only gates | `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md` (owner) |
| bootstrap shell / feature mounting / probes | bootstrap |
| migrations skeleton / autoRun / dir layout | migrations |
| outbox / relay / topic-subject naming / CDC | transport |
| OpenAPI UI / rootRedirect | openapi |
| GraphQL / gRPC surface (when accepted) | graphql · grpc |
| file/dir layout & naming | service-layout |

## Traps (bench-proven; re-verify framework-facing ones against the pinned docs)

- **Both build tags are mandatory** — an engine AND a transport (the pinned release's
  sets; today's latest: `postgres`|`mysql`|`sqlserver`|`oracle` and `kafka`|`nats`); no default
  on either axis, a tagless build aborts at boot — **except SQLite, which is engine-only
  (`-tags sqlite`) + tagless transport (valid: a no-op adapter, no broker, no messaging).**
- **SQLite = zero-infra, no Docker.** `CGO_ENABLED=0` (pure-Go, no cgo). DSN is a file path
  (default `file:app.db`, created next to the binary — portable; under `go run` falls back to
  the project dir) or `:memory:` (ephemeral). An absolute path is honored verbatim (fixed
  external location, not portable). The factory FORCES the correctness
  pragmas (`foreign_keys`, `case_sensitive_like`); no `mongo:`/`transport:` blocks, no
  `devops/`. SQLite is MVP-not-production (ASCII-only case folding, decimal stored TEXT —
  `table-schema.html`). Integration events + Mongo projections require a Debezium-tailable
  engine — reaching them later is `/omnicore:configure` (an engine swap), fully reversible.
- **`go mod tidy` prunes/misses tag-gated deps** → always follow with
  `GOFLAGS=-mod=mod go build` (step 10). Shipping `go.mod` without the matching
  `go.sum` is the classic silent break (a `go.work` overlay masks it locally).
- **prd without `auth` aborts boot**; `auth.mode: disabled` is accepted ONLY under
  `APP_PROFILE=dev`. `migrations.autoRun` / `mongo.rebuild.autoRun` default `true`
  only in dev, `check` elsewhere.
- **Boot order, CDC side:** the outbox table exists only after the first app boot; on
  NATS the JetStream stream is framework-owned (relay `create-stream=false`), so a
  relay started earlier crash-loops until then — `restart: unless-stopped` on the
  relay container is load-bearing, not cosmetic.
- **Topic/subject naming is transport-specific** — read `transport.html`; the
  templates carry the reference shapes but the doc wins on drift.
- **MySQL relay:** unique `server.id` per binlog client;
  `schema.history.internal.skip.unparseable.ddl=true` (foreign DDL in the shared
  binlog otherwise kills the relay); `include.schema.changes=false` (the
  schema-change topic has no home — on a NATS sink it dies with "No Responders").
- **Postgres relay:** the container must run `wal_level=logical` (+ wal senders/
  replication slots); source uses `pgoutput` + `publication.autocreate.mode=filtered`.
- **SQL Server relay:** CDC depends on the SQL Server Agent being enabled on the
  container AND on CDC being enabled per database and per tracked table — which is
  only possible AFTER the first app boot creates the outbox, so the start wrapper
  carries an idempotent enable arm. The concrete shape lives in
  `templates/cdc-relay.md` / `templates/docker-bench.md`, validated against the
  pinned `transport.html` — the doc wins on any drift.
- **Oracle relay:** LogMiner needs DATABASE-level provisioning at the DB's first
  boot (ARCHIVELOG + minimal supplemental logging + the `c##dbzuser` LogMiner user
  + the heartbeat table — image init scripts in the bench) AND per-table
  supplemental logging on the outbox — only possible AFTER the first app boot
  creates it, so the start wrapper carries an idempotent enable arm (the sqlserver
  pattern). The CDC-tailed payload columns are CLOB by framework design (LogMiner
  cannot decode native-JSON redo — pinned `table-schema.html`, Oracle column
  shapes). Concrete shape in `templates/cdc-relay.md` / `templates/docker-bench.md`,
  validated against the pinned `transport.html` — the doc wins on any drift.
- **Mongo database is per-service** (`<svc>_views`); the `rebuild:` block is
  strict-decoded — unknown keys abort boot.
- **DSN defaults must equal the bench, not placeholders** — the compose creds are
  `omnicore:omnicore` on `<svc>_db` (docker-bench template; exception: the sqlserver
  bench logs in as `sa` with a strong password — the image enforces password
  complexity, so `omnicore:omnicore` cannot exist there; second exception: the
  oracle bench KEEPS `omnicore:omnicore` — the image's APP_USER — but has NO
  `<svc>_db`: the app connects to the `FREEPDB1` PDB, where the schema IS the app
  user, so the DSN default is
  `oracle://omnicore:omnicore@localhost:<hostport>/FREEPDB1`; the image's admin
  `ORACLE_PASSWORD` is separate and strong). The dev YAML's
  `${VAR:default}` for the relational DSN (and mongo URI, broker endpoints) must embed
  exactly those as the default; `${VAR:...}` keeps it overridable but the fallback is
  the bench, never `user:password`. Mismatch = boot passes config but `readyz` fails
  the DB auth path. (Observed drift on a Sonnet run.)

## What this skill never does

No entities (that's `scaffold-entity`, handed off after the shell is green), no second
dialect/transport in one run, no framework edits, no git, no guessed identity slots
(name, module path, dialect are ALWAYS the dev's answers — transport too, except on
SQLite where it is tagless by construction), no invented framework version (always the
latest published release unless the dev pinned one unprompted).

## templates/ index

| File | Covers |
|---|---|
| `templates/docker-bench.md` | compose skeleton per choice (postgres\|mysql\|sqlserver\|oracle × kafka\|nats + Mongo + relay), healthchecks, volumes, port table + shifted-port rule, `start.sh` + `start.cmd` + `start.ps1` |
| `templates/cdc-relay.md` | Debezium Server `application.properties` — source blocks (mysql/postgres/sqlserver/oracle) × sink blocks (nats/kafka), the EventRouter contract, predicates, relay traps |
| `templates/sqlite-mvp.md` | SQLite zero-infra glue — **no Docker**: `CGO_ENABLED=0 -tags sqlite` start wrappers (no compose), `file:app.db`/`:memory:` DSN, `.gitignore` for the `app.db*` sidecars |

All three are DEVOPS GLUE templates (the sanctioned exception) — instantiate names/ports
from the spec, validate framework-facing values against the pinned docs (`transport.html`
for the bench/relay; `yaml-reference.html` + `table-schema.html` for the SQLite DSN/pragmas).
