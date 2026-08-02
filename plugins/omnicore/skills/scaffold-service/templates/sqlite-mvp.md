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
- **relative `file:app.db` (the DEFAULT)** — created NEXT TO THE BINARY, so the single-file MVP is
  portable (a pendrive carries binary + `.db` together). Under `go run` the binary is a throwaway temp
  file, so it falls back to the **working dir** — which is why the wrappers below `cd` into the project
  first (the dev-loop `app.db` lands there). Parent dir auto-created.
- **absolute `file:/var/lib/app/app.db`** — used verbatim; the escape hatch for a fixed external
  location. NOT portable, so don't use it for a USB deploy.
- **`:memory:`** — RAM-only, ephemeral (gone on exit); demos/tests.

The SQLite factory FORCES the correctness pragmas (`foreign_keys=ON`, `case_sensitive_like=ON`) —
never the dev's job; tuning pragmas (`journal_mode=WAL`, `busy_timeout`) are overridable defaults.
SQLite is single-writer / single-node — the MVP, not-production posture (ASCII-only case folding,
decimal stored as TEXT). See `table-schema.html` for the Go↔SQLite type table + the MVP warning.

## `start.sh` (darwin|linux) — no compose, just run

```bash
#!/usr/bin/env bash
# Run <svc> in dev mode against SQLite — no bench, no Docker.
# The cd makes this folder the working dir, so under `go run` the app.db is
# created HERE (the project), not in the temp build dir. Set SQLITE_PATH to an
# absolute path to pin it elsewhere.
set -euo pipefail
cd "$(dirname "$0")"
APP_PROFILE=dev CGO_ENABLED=0 go run -tags sqlite ./bootstrap
```

## `start.cmd` (windows, zero-friction)

```bat
@echo off
REM The cd makes this folder the working dir, so under `go run` app.db is created
REM HERE (the project), not the temp build dir. Set SQLITE_PATH for a fixed path.
cd /d "%~dp0"
set APP_PROFILE=dev
set CGO_ENABLED=0
go run -tags sqlite ./bootstrap
```

## `start.ps1` (windows, robust)

```powershell
#!/usr/bin/env pwsh
# Set-Location makes this folder the working dir, so under `go run` app.db is
# created HERE (the project), not the temp build dir. Set SQLITE_PATH for a fixed path.
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
