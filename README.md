# omnicore — Claude Code plugin

Tooling for the [omnicore](https://github.com/ClaudioSchirmer/omnicore) DDD + CQRS
framework, delivered as a Claude Code plugin. This repository is **both** the plugin and
its marketplace: add the marketplace, install the `omnicore` plugin, and the skills below
become available in any omnicore-based project.

## Skills

Invoked as `/omnicore:<skill>` once installed:

| Skill | What it does |
|---|---|
| `/omnicore:scaffold-service` | Create a brand-new omnicore service from an empty directory — `go.mod` pinned to a published release, the bootable bootstrap shell, the `microservice.*.yaml` profiles, migrations skeleton, and the local docker bench (DB + Mongo + broker + Debezium CDC relay) — then prove the shell boots. |
| `/omnicore:scaffold-system` | Turn a whole-system/MVP description — several entities, shared identities and read models in one prose drop — into an approved domain map, then scaffold it entity by entity by delegating to `scaffold-entity` (and cross-entity read models to `scaffold-view`). Decomposition only: it never generates code itself. |
| `/omnicore:scaffold-entity` | Scaffold a complete CRUD entity across every layer (domain → application → web → infra → migrations → bootstrap) of an existing omnicore service. |
| `/omnicore:evolve-entity` | Change an EXISTING entity — add/remove/rename fields, uniqueness, children, modes — with schema evolution done right: migration, TableSchema, DTOs, translations, view `Version` bump and OpenAPI move together, via an approved impact-map spec. |
| `/omnicore:remove-entity` | Surgically remove an entity from every layer via an inventory-first removal plan you approve before anything is deleted; shared bases, composed views and integration-event consumers are detected and block until you decide. |
| `/omnicore:scaffold-view` | Create a NEW read model beyond an entity's own view — ComposedView across entities, SharedBaseView identity, Upstream/Embed composition, or an aggregated view — projected to Mongo and exposed on REST/GraphQL, via an approved spec. |
| `/omnicore:evolve-view` | Change an EXISTING view — projected fields, legs/roles, indexes, operators, surfaces — with the `Version` bump and rebuild discipline done right, write side untouched. |
| `/omnicore:implement` | Wire a framework capability into an existing service — another surface (gRPC, GraphQL), an external API call from a handler (httpclient + middleware), cache, integration events, lifecycle hooks, authz, tracing — anything the pinned framework offers that no dedicated skill owns. Routes the request against the pin's docs (the capability catalog); if the framework doesn't offer it, it says so honestly. |
| `/omnicore:run` | Boot the service locally (bench up, background boot, readiness) and hand you clickable links — OpenAPI UI, GraphQL, probes. The app stays running. |
| `/omnicore:doctor` | Diagnose a misbehaving service or bench: walks the pipeline (build → boot → serve → write → relay → broker → projection), proves the cause with evidence, and prescribes the fix. Read-only — it never edits your files. |
| `/omnicore:upgrade` | Upgrade a service's omnicore pin: check the current version, show the target release's changelog, and on your ok run `go get` + `go mod tidy` + build — with rollback to the previous version if the build breaks, or an approved migration plan to fix the breaking-change fallout. |
| `/omnicore:help` | Docs-first conversational guide to how the framework works. Read-only: it explains, it never changes anything. |

All skills are **version-agnostic**: they read the framework version from the project's
`go.mod` and treat that version's bundled `/docs` as the sole authority, so they never
drift as the framework evolves.

Works with any published omnicore release (docs-pinned by design). Latest release
exercised: framework v0.32.0 · plugin 0.4.2.

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
3. Record the release in [`CHANGELOG.md`](CHANGELOG.md) (same PR as the bump).
4. Commit → open a PR → merge to `main` → push, then tag the bump commit `v<version>`.
   `main` is the source of truth; there is no Anthropic-hosted copy.
5. Clients pick it up with:
   ```
   claude plugin marketplace update omnicore   # re-pull the catalog
   claude plugin update omnicore@omnicore       # fetch the new version
   ```
   Auto-update is off by default for self-hosted marketplaces; clients opt in per
   marketplace to get background checks + a "run `/reload-plugins`" notification instead.

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
        └── skills/
            ├── scaffold-service/
            ├── scaffold-system/
            ├── scaffold-entity/
            ├── evolve-entity/
            ├── remove-entity/
            ├── scaffold-view/
            ├── evolve-view/
            ├── implement/
            ├── run/
            ├── doctor/
            ├── upgrade/
            └── help/
```

## License

Apache License 2.0 — see [LICENSE](./LICENSE).
