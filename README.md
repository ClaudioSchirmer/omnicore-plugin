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
| `/omnicore:scaffold-entity` | Scaffold a complete CRUD entity across every layer (domain → application → web → infra → migrations → bootstrap) of an existing omnicore service. |
| `/omnicore:upgrade` | Upgrade a service's omnicore pin: check the current version, show the target release's changelog, and on your ok run `go get` + `go mod tidy` + build — with rollback to the previous version if the build breaks. |
| `/omnicore:help` | Docs-first conversational guide to how the framework works. Read-only: it explains, it never changes anything. |

All skills are **version-agnostic**: they read the framework version from the project's
`go.mod` and treat that version's bundled `/docs` as the sole authority, so they never
drift as the framework evolves.

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
            ├── scaffold-entity/
            ├── upgrade/
            └── help/
```

## License

Apache License 2.0 — see [LICENSE](./LICENSE).
