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
  `postgres`|`mysql`|`sqlserver`|`oracle` and `kafka`|`nats`; the pinned docs are the
  authority on what the pin supports). Both mandatory — a tagless build aborts
  at boot.
- **Profile + ports:** `APP_PROFILE=dev` unless the dev says otherwise; resolve the
  EFFECTIVE host port from the yaml (`${VAR:default}` — apply the env override rule).
- **Already up?** Probe `/livez` on the resolved port first — answering ⇒ skip boot,
  jump to Phase 3 saying it was already running.

## Phase 1 — Bench

- `devops/` compose exists → `docker compose ps`; anything down → `up -d`, wait
  healthy. Trap: stopping Docker / `compose down` KEEPS named volumes — a
  migration-version-mismatch abort at boot usually means a stale volume; surface
  `down -v` as the fix but the DEV decides (it destroys data).
- Existing-infra project (no `devops/`) → check the yaml endpoints answer (relational,
  Mongo, broker); unreachable → report WHICH and stop.

## Phase 2 — Boot

- Prefer the project's start wrapper (`start.sh` / `start.cmd`); else
  `APP_PROFILE=dev go run -tags '<engine> <transport>' ./bootstrap`.
- Run in BACKGROUND, log to a file; poll `/readyz` until 200 (bounded ~60s).
- Failure → show the FIRST error from the log verbatim and stop. This skill fixes
  nothing — point at `help` (understand) or the skill that generated the code.

## Phase 3 — The links (the deliverable)

Only surfaces the yaml actually enables:

- App root: `http://localhost:<port>/` (302 when rootRedirect)
- OpenAPI UI: `http://localhost:<port><uiPath>`
- GraphQL / gRPC when enabled (gRPC isn't clickable — report host:port + reflection
  state instead)
- Probes: `/livez` · `/readyz`

The app STAYS RUNNING — that is the point. Close with how to stop: the PID +
`kill <pid>`, and `docker compose down` for the bench (volumes survive; `down -v`
wipes data — say so).

## Knowledge routing — question → `/docs` section

When a boot fact is in doubt (a yaml key's default, what readiness proves, the relay's
expected log line), don't guess — read the pinned manual: `<name>.html` lives at
`<omnicore-dir>/docs/content/sections/<name>.html`, where `<omnicore-dir>` =
`go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`. Read ONLY the routed
section for the fact in doubt — never sweep the manual to boot an app.

| When checking… | Read section(s) |
|---|---|
| yaml keys / ports / profiles / env overrides | yaml-reference |
| boot order / probes semantics / surfaces enabled | bootstrap |
| relay / broker / outbox expectations | transport |
| OpenAPI UI path / rootRedirect | openapi |
| GraphQL / gRPC endpoints | graphql · grpc |

## What this skill never does

No file writes, no edits, no scaffolding, no migrations, no git. It starts
infrastructure and the app, verifies readiness, and hands over the links; fixing a
broken boot belongs to the dev or the skill that generated the code.
