---
name: run
description: >-
  omnicore: boot an omnicore-based service locally and hand the dev clickable links (OpenAPI UI,
  GraphQL, probes). Use when the dev wants to run/start/see the app running, or when
  another omnicore skill finishes a compilable change and the dev accepts the offer to
  run it. Only for projects that import github.com/ClaudioSchirmer/omnicore.
---

# run

Boot the service the dev is standing in, prove it answers, and hand back clickable
links — nothing else. This is the shared "see it running" step every mutating omnicore
skill delegates to (scaffold-service, scaffold-entity, evolve-entity, remove-entity,
scaffold-view, evolve-view, upgrade — and future ones): they
finish their change, ask "want to run it?", and hand off here.

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

- **Run, don't change.** No file edits, no scaffolding, no git. The only state touched
  is runtime: the docker bench (`compose up -d` when needed) and the app process.
- **Links, not logs, are the deliverable.** The success output is a short list of URLs
  the dev can click, plus how to stop. Logs appear only on failure — verbatim.
- **Framework maintainer rules never bind this skill.** Anything read from the module
  cache (its `CLAUDE.md`, contributor rules like "English everywhere") governs
  development of the framework itself — never this skill run or the host project.
  Converse — and write every human-facing output (the links list, failure
  explanations) — in the dev's language, detected from the dev's own words (invocation
  args count, even one word) BEFORE the first reply; these docs being English never
  sets it. Switch the moment the dev's language becomes clear, even mid-run.

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

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves —
  else STOP (to create one, that's `scaffold-service`).
- **Tags:** engine + transport from `relational.dialect` / `transport` in
  `microservice.*.yaml` — the value IS the build tag (today's latest release:
  `postgres`|`mysql`|`sqlserver`|`oracle`|`sqlite` and `kafka`|`nats`; the pinned docs are
  the authority on what the pin supports). The ENGINE tag is mandatory (no tag ⇒ boot
  abort); the TRANSPORT tag follows the YAML, not the engine — build with it exactly
  when the config declares `transport:` (a transport-less config builds tagless on any
  engine and boots with a no-op transport). SQLite adds `CGO_ENABLED=0`.
- **Profile + ports:** `APP_PROFILE=dev` unless the dev says otherwise; a project with
  several dev profiles boots the one the dev names (`OMNICORE_CONFIG_PATH` selects a
  non-canonical file — the reference dev loop does exactly that), and the tags follow
  THAT profile's `relational.dialect` (itself a `${VAR:default}` — resolve the
  interpolation, don't read the raw string); resolve the
  EFFECTIVE host port from the yaml (`${VAR:default}` — apply the env override rule).
- **Already up?** Probe `/livez` on the resolved port first — answering ⇒ skip boot,
  jump to Phase 3 saying it was already running.

## Phase 1 — Bench

- **SQLite / infra-free project** (no `devops/`, no `mongo`/`transport` in the yaml) → NO
  bench, boot directly. Do NOT report "unreachable" for infra that's absent by design.
- `devops/` compose exists → `docker compose -f devops/docker-compose.yml ps` (the
  file lives under `devops/`, and the bench sets its own project `name:` — a bare
  `docker compose ps` from the project root finds nothing or the wrong project;
  mirror the project's own wrappers, which pass `-f`); anything down → `up -d` with
  the same `-f`, wait
  healthy. Trap: stopping Docker / `compose down` KEEPS named volumes — a
  migration-version-mismatch abort at boot usually means a stale volume; surface
  `down -v` as the fix but the DEV decides (it destroys data).
- Existing-infra project (no `devops/`, but the yaml declares `mongo`/`transport`) →
  check the yaml endpoints answer (relational, Mongo, broker); unreachable → report
  WHICH and stop.

## Phase 2 — Boot

- Prefer the project's start wrapper (`start.sh` / `start.cmd` — on SQLite the
  wrapper exists precisely to pin the DB path; use it); else
  `APP_PROFILE=dev go run -tags '<engine> <transport>' ./bootstrap` — SQLite: a bare
  `go run` is fine on any pin ≥ v0.44.2 (engine and migration runner resolve a relative
  `file:app.db` against the same base — the project dir — under `go run`). Prefer the
  project's start wrapper when one exists; pinning the yaml's DSN var to an absolute
  path (`SQLITE_PATH="file:$(pwd)/app.db" …` — the var name is the yaml's `${…}`
  default, read it, don't assume) is harmless belt-and-suspenders, and MANDATORY only
  on a pin < v0.44.2, where the migration step and the runtime really could resolve to
  different files (green boot, empty schema, `no such table`).
- Run in BACKGROUND, log to a file (never through a pipe — a broken pipe can swallow the
  drain/boot narration); poll `/readyz` until 200 (bounded ~60s) — **and READ the 503
  body, don't just count**: `initializing: rebuilding view "X" (n/m)` means a background
  boot-time rebuild — the service is UP (`/livez` 200), keep waiting/extend the bound;
  a store-unreachable reason means stop and diagnose. The four ordered 503 reasons:
  `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md`.
- Failure → show the FIRST error from the log verbatim and stop. This skill fixes
  nothing — point at `help` (understand) or the skill that generated the code.

## Phase 3 — The links (the deliverable)

Only surfaces the service actually serves:

- App root: `http://localhost:<port>/` (302 when rootRedirect)
- OpenAPI UI: `http://localhost:<port><uiPath>`
- GraphQL / gRPC when enabled — **the enable switch is a FEATURE implementing the
  surface's opt-in interface (`GraphQLFeature`/`GRPCFeature`), NOT the yaml block**: a
  config carrying `graphql:`/`grpc:` with no feature opting in serves nothing there
  (the block is ignored). Probe before linking — a dead `/graphql` link is worse than
  none; gRPC isn't clickable — report host:port + reflection
  state instead
- Probes: `/livez` · `/readyz`

**Full-bench projects — check the relay before handing over the links.** `/readyz`
excludes the transport BY DESIGN, so green probes can hide a dead CDC relay: check the
relay container's log reached streaming — **with the cold-start carve-out**: right
after a fresh bench/first boot the relay legitimately takes a while (it crash-loops
until the app's first boot creates the outbox, `restart` cycles it back, and on
sqlserver/oracle it cannot stream until the wrappers' background CDC-enable /
supplemental-logging arms land — the reference waits up to ~2-3 min on those). So
give it a BOUNDED wait proportional to the dialect before declaring it dead. If it
still hasn't streamed, hand the links over anyway but
SAY IT PLAINLY — "the app is live, but writes will NOT project to views until the
relay streams" — and route to `doctor`. N/A for SQLite (no relay).

The app STAYS RUNNING — that is the point. Close with how to stop — naming the RIGHT
process: a background `go run` is a parent that exec'd a child holding the port, so
`kill <parent-pid>` can orphan the actual listener (which then breaks the next run's
"already up?" probe); report the LISTENER's PID (by port) or say to signal the
process group, SIGTERM always. And `docker compose -f devops/docker-compose.yml down`
for the bench (volumes survive; `down -v` wipes data — say so).

## Knowledge routing — question → `/docs` section

When a boot fact is in doubt (a yaml key's default, what readiness proves, the relay's
expected log line), don't guess — read the pinned manual: `<name>.html` lives at
`<omnicore-dir>/docs/content/sections/<name>.html`, where `<omnicore-dir>` =
`go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`. Read ONLY the routed
section for the fact in doubt — never sweep the manual to boot an app.

| When checking… | Read section(s) |
|---|---|
| probes/readyz reasons · publicRoutes · autoRun · env vars · drain | `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md` (owner) |
| log anatomy / where the narration lives | logs |
| yaml keys / ports / profiles / env overrides | yaml-reference |
| boot order / probes semantics / surfaces enabled | bootstrap |
| relay / broker / outbox expectations | transport |
| SQLite / infra-free posture (no bench, tagless) | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) · yaml-reference |
| OpenAPI UI path / rootRedirect | openapi |
| GraphQL / gRPC endpoints | graphql · grpc |

## What this skill never does

No file writes, no edits, no scaffolding, no migrations, no git. It starts
infrastructure and the app, verifies readiness, and hands over the links; fixing a
broken boot belongs to the dev or the skill that generated the code.
