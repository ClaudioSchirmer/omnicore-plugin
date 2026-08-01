# SQLite zero-infra MVP template — `start.sh` / `start.cmd` / `start.ps1` (no bench)

**SQLite ⇒ no Docker.** The SQLite / infra-free posture has **no `devops/`**: no compose, no
Mongo, no broker, no Debezium relay, nothing to `docker compose up`. One binary + one `app.db`
file (or `:memory:`). This template is the whole glue. Every framework-facing value (DSN form,
pragmas, build tags) is validated against the pinned `yaml-reference.html` + `table-schema.html` —
**the doc wins on any drift.**

## Build & run shape

- Build: `CGO_ENABLED=0 go build -tags sqlite ./bootstrap` — pure-Go, no cgo, one static binary.
- Transport is **tagless** (no `kafka`/`nats` tag): a no-op adapter, no messaging.
- Run: always the dev profile — `APP_PROFILE=dev CGO_ENABLED=0 go run -tags sqlite ./bootstrap`.
- No `mongo:` and no `transport:` block in `microservice.dev.yaml` (both opt-out by absence).

## DSN — where the database lives

`relational.dsn` default `${SQLITE_PATH:file:app.db}`:
- a **file path** (`file:app.db`, `file:data/app.db`) — a relative path resolves NEXT TO THE
  BINARY and is created if absent;
- or **`:memory:`** — entirely in RAM, nothing on disk, **data is ephemeral** (gone on exit);
  great for demos/tests.

The SQLite factory FORCES the correctness pragmas (`foreign_keys=ON`, `case_sensitive_like=ON`) —
never the dev's job; tuning pragmas (`journal_mode=WAL`, `busy_timeout`) are overridable defaults.
SQLite is single-writer / single-node — the MVP, not-production posture (ASCII-only case folding,
decimal stored as TEXT). See `table-schema.html` for the Go↔SQLite type table + the MVP warning.

## `start.sh` (darwin|linux) — no compose, just run

```bash
#!/usr/bin/env bash
# Run <svc> in dev mode against SQLite — no bench, no Docker.
set -euo pipefail
cd "$(dirname "$0")"
APP_PROFILE=dev CGO_ENABLED=0 go run -tags sqlite ./bootstrap
```

## `start.cmd` (windows, zero-friction)

```bat
@echo off
cd /d "%~dp0"
set APP_PROFILE=dev
set CGO_ENABLED=0
go run -tags sqlite ./bootstrap
```

## `start.ps1` (windows, robust)

```powershell
#!/usr/bin/env pwsh
Set-Location $PSScriptRoot
$env:APP_PROFILE = 'dev'; $env:CGO_ENABLED = '0'
go run -tags sqlite ./bootstrap
```

Keep all shipped wrappers in lockstep. Invoke `start.ps1` as `pwsh -File .\start.ps1` (execution
policy); `start.cmd` is the safe default to point a new dev at.

## `.gitignore` additions (file-backed SQLite)

```
app.db
app.db-wal
app.db-shm
```

(The `-wal`/`-shm` sidecars appear under `journal_mode=WAL`. Nothing to ignore for `:memory:`.)
