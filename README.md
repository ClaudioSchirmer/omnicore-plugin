# omnicore — Claude Code plugin

Tooling for the [omnicore](https://github.com/ClaudioSchirmer/omnicore) DDD + CQRS
framework, delivered as a Claude Code plugin. This repository is **both** the plugin and
its marketplace: add the marketplace, install the `omnicore` plugin, and the skills below
become available in any omnicore-based project.

## Skills

Invoked as `/omnicore:<skill>` once installed:

| Skill | What it does |
|---|---|
| `/omnicore:scaffold-service` | Create a brand-new omnicore service from an empty directory — `go.mod` pinned to a published release, the bootable bootstrap shell, the `microservice.*.yaml` profiles, migrations skeleton, and EITHER a local docker bench (DB + Mongo + broker + Debezium CDC relay) OR a zero-infra SQLite MVP (single pure-Go binary, one `app.db` or `:memory:`, no Docker) — then prove the shell boots. |
| `/omnicore:scaffold-system` | Turn a whole-system/MVP description — several entities, shared identities and read models in one prose drop — into an approved domain map, then scaffold it entity by entity by delegating to `scaffold-entity` (and cross-entity read models to `scaffold-view`). Decomposition only: it never generates code itself. |
| `/omnicore:scaffold-entity` | Scaffold a complete CRUD entity across every layer (domain → application → web → infra → migrations → bootstrap) of an existing omnicore service. |
| `/omnicore:omnicore-gen` | Drive **omnicore-gen** (beta), the spec-driven code generator, to write a whole entity from one YAML spec — then review it, implement the rules the spec language cannot express, and prove it with build + tests + a real boot — and to CHANGE one, by editing that spec and regenerating. Reached from a generation gateway — `scaffold-entity`'s when creating, `evolve-entity`'s when changing — where the dev chooses between the generator (seconds, a fraction of the tokens) and file by file by the agent. |
| `/omnicore:gen` | Run ONE `omnicore-gen` command against an existing project and read the answer — `doctor` (drift between the spec, the lock and the files on disk), `check`, `explain`, `adopt`, `init`. The door to the generator's CLI for a project that already exists; creating an entity stays with `scaffold-entity` (model and plan gates) and changing one with `evolve-entity` (impact map + the migration a regeneration never writes). |
| `/omnicore:evolve-entity` | Change an EXISTING entity — add/remove/rename fields, uniqueness, children, modes — with schema evolution done right: migration, TableSchema, DTOs, translations, view `Version` bump and OpenAPI move together, via an approved impact-map spec. When the entity is the generator's, it offers the same two-option gateway as `scaffold-entity`: edit the spec and regenerate (beta), or change every file by hand — the migration pair is hand-written either way. |
| `/omnicore:remove-entity` | Surgically remove an entity from every layer via an inventory-first removal plan you approve before anything is deleted; shared bases, composed views and integration-event consumers are detected and block until you decide. |
| `/omnicore:scaffold-view` | Create a NEW read model beyond an entity's own view — ComposedView across entities, SharedBaseView identity, Upstream/Embed composition — projected to Mongo and exposed on REST/GraphQL/gRPC, via an approved spec. |
| `/omnicore:evolve-view` | Change an EXISTING view — projected fields, legs/roles, indexes, operators, surfaces — with the `Version` bump and rebuild discipline done right, write side untouched. |
| `/omnicore:implement` | Wire a framework capability into an existing service — another surface (gRPC, GraphQL), an external API call from a handler (httpclient + middleware), cache, integration events, lifecycle hooks, authz, tracing — anything the pinned framework offers that no dedicated skill owns. Routes the request against the pin's docs (the capability catalog); if the framework doesn't offer it, it says so honestly. |
| `/omnicore:run` | Boot the service locally (bench up, background boot, readiness) and hand you clickable links — OpenAPI UI, GraphQL, probes. The app stays running. |
| `/omnicore:qa` | Generate and run a CONTRACT QA SUITE for the service: read its entities/views/surfaces/posture, derive the pinned framework's promised behaviors (verbs per mode, status codes, archive semantics, filter vocabulary, typed 400s), and produce an executable e2e suite (`qa/*.sh` + runner) that proves them against the running service — fail-fast, honest GREEN/RED. |
| `/omnicore:configure` | Change a service's INFRASTRUCTURE POSTURE and configuration — convert a zero-infra/SQLite MVP into full distributed CQRS (add Mongo + broker + CDC relay + docker) or back, swap the relational engine, switch transport (kafka ⇄ nats), tune the `microservice.*.yaml` / devops glue. Every conversion is reversible and no application code is lost. |
| `/omnicore:doctor` | Diagnose a misbehaving service or bench: walks the pipeline (build → boot → serve → write → relay → broker → projection), proves the cause with evidence, and prescribes the fix. Read-only — it never edits your files. |
| `/omnicore:upgrade` | Upgrade a service's omnicore pin: check the current version, show the target release's changelog, and on your ok run `go get` + `go mod tidy` + build — with rollback to the previous version if the build breaks, or an approved migration plan to fix the breaking-change fallout. |
| `/omnicore:help` | Docs-first conversational guide to how the framework works. Read-only: it explains, it never changes anything. |

All skills are **version-agnostic**: they read the framework version from the project's
`go.mod` and treat that version's bundled `/docs` as the sole authority, so they never
drift as the framework evolves.

Works with any published omnicore release (docs-pinned by design). Version-matched to the
framework's v0.43.0 (value objects + IfArchive/IfUnarchive) · plugin 0.12.0 — publish the two
in sync.

## Install

```
# 1. Register this marketplace (from a local clone, or straight from GitHub)
/plugin marketplace add ClaudioSchirmer/omnicore-plugin

# 2. Install the plugin
/plugin install omnicore@omnicore

# 3. (mid-session) pick up changes without restarting
/reload-plugins
```

`/plugin marketplace add` also accepts a local path (`/plugin marketplace add
./omnicore-plugin`) or a URL to the `marketplace.json`.

## Develop / test locally

Run Claude Code with the plugin loaded directly from disk — no install needed:

```
claude --plugin-dir ./plugins/omnicore
```

Validate the manifest before publishing (CI-friendly):

```
claude plugin validate ./plugins/omnicore --strict
```

The skills in `plugins/omnicore/skills/` are the **single source of truth** — edit them
here, not in any `~/.claude/skills` copy. During development, `--plugin-dir` above loads
your working tree directly, so changes show up on the next `/reload-plugins` without a
reinstall.

## Releasing

Clients pin to a released **version**, so a code change reaches them only when you bump it:

1. Edit skills under `plugins/omnicore/skills/` and verify with `--plugin-dir`.
2. **Bump `version`** in `plugins/omnicore/.claude-plugin/plugin.json` (semver). Pushing
   commits *without* bumping does not deliver the change — installed clients stay on the
   cached version.
3. Record the release in [`CHANGELOG.md`](CHANGELOG.md) (same PR as the bump), under a
   `## [<version>]` heading — that section IS the release notes (see step 4).
4. Commit → open a PR → merge to `main` → push, then tag the bump commit `v<version>`.
   `main` is the source of truth; there is no Anthropic-hosted copy.
   Pushing the tag (or drafting the release in the web UI) fires
   [`.github/workflows/release.yml`](.github/workflows/release.yml), which publishes the
   GitHub Release with the matching `## [<version>]` CHANGELOG section as its body —
   creating the release from a CLI tag, or syncing the body in place when the release came
   from the UI. Two guards: the run fails BEFORE touching any release if the tag disagrees
   with `plugin.json`'s `version` (clients install what the manifest declares), and a
   missing CHANGELOG section never clobbers an existing release body — it warns instead.
   Nothing to do by hand; the Release page stops drifting behind the tag list.
5. Clients pick it up with:
   ```
   claude plugin marketplace update omnicore     # re-pull the catalog
   claude plugin install omnicore@omnicore       # reinstall to fetch the new version
   /reload-plugins                               # (in-session) activate it
   ```
   There is NO per-plugin `update` command — refresh the marketplace, then reinstall (or let
   auto-update fetch it where enabled). Auto-update is off by default for self-hosted
   marketplaces; clients opt in per marketplace to get background checks + a
   "run `/reload-plugins`" notification instead.

> **Everything the plugin needs lives UNDER `plugins/omnicore/`** — that is Claude Code's
> rule, not a preference: components must be at the plugin root, and a path that traverses
> outside it (`../`) stops resolving once the plugin is cached. That is why the generator
> sits at `plugins/omnicore/gen/` rather than at the repo root, and why its launcher is in
> `bin/`, which is added to PATH for the session.

> **Manifest gotchas (bench-proven):** the marketplace entry's `source` must be the explicit
> relative path `./plugins/omnicore` — a bare string (`"omnicore"`) is rejected at install as
> an unsupported source type. And keep `.gitignore` from matching the skill directories
> (`scaffold-entity/` / `scaffold-service/` as bare patterns silently exclude them from the
> push).

## Layout

```
omnicore-plugin/                     # repo root = marketplace
├── .claude-plugin/
│   └── marketplace.json             # marketplace manifest (name: omnicore)
└── plugins/
    └── omnicore/                    # the plugin
        ├── .claude-plugin/
        │   └── plugin.json          # plugin manifest (name: omnicore)
        ├── bin/
        │   └── omnicore-gen         # on the session PATH — builds gen/ and execs it
        ├── gen/                     # omnicore-gen: the generator, as Go source
        ├── shared/
        └── skills/
            ├── scaffold-service/
            ├── scaffold-system/
            ├── scaffold-entity/
            ├── omnicore-gen/
            ├── evolve-entity/
            ├── remove-entity/
            ├── scaffold-view/
            ├── evolve-view/
            ├── implement/
            ├── configure/
            ├── run/
            ├── doctor/
            ├── upgrade/
            └── help/
```

## License

Apache License 2.0 — see [LICENSE](./LICENSE).
