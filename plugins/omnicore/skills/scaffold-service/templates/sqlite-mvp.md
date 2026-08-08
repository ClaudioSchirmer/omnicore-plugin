# SQLite zero-infra MVP template — `start.sh` / `start.cmd` / `start.ps1` (no bench)

**SQLite ⇒ no Docker.** The SQLite / infra-free posture has **no `devops/`**: no compose, no
Mongo, no broker, no Debezium relay, nothing to `docker compose up`. One binary + one `app.db`
file (or `:memory:`). This template is the whole glue. Every framework-facing value (DSN form,
pragmas, build tags) is validated against the pinned `yaml-reference.html` + `table-schema.html` —
**the doc wins on any drift.**

## Build & run shape

- Build: `CGO_ENABLED=0 go build -tags sqlite ./bootstrap` — pure-Go, no cgo, one static binary.
- Transport is tagless in the ZERO-INFRA DEFAULT because the yaml declares no
  `transport:` block (a no-op adapter, no messaging) — the tag follows the yaml, not the
  engine: a SQLite service that later declares `transport:` to SUBSCRIBE to another
  service's events builds with that transport's tag.
- Run: always the dev profile — `APP_PROFILE=dev CGO_ENABLED=0 go run -tags sqlite ./bootstrap`.
- No `mongo:` and no `transport:` block in `microservice.dev.yaml` — the zero-infra
  DEFAULTS (both opt-out by absence, both reversible later via `/omnicore:configure`;
  a SharedBaseView would require re-adding `mongo:` — it boots and serves empty, no CDC
  feed).

## DSN — where the database lives

`relational.dsn` default `${SQLITE_PATH:file:app.db}`:
- **relative `file:app.db` (the yaml DEFAULT)** — the portable single-file story: the `.db` lives
  NEXT TO THE BINARY (a pendrive carries binary + `.db` together). Under `go run` the
  ephemeral-binary carve-out applies on BOTH sides — engine AND migration runner resolve
  against the working dir (the project), mirrored deliberately since v0.44.2 — so the dev
  loop migrates the database it serves. The wrappers below still pin an ABSOLUTE
  `SQLITE_PATH` next to the script (recomputed each run, so the `.db` travels with the
  project): harmless belt-and-suspenders, plus one unambiguous path across
  build-vs-`go run` and OS shells. On a pin OLDER than v0.44.2 the runner resolved
  against the exe dir and the dev loop really did split (green boot, empty schema,
  `no such table`) — the pin's changelog decides which world you are in. Parent dir
  auto-created.
- **absolute `file:/var/lib/app/app.db`** — used verbatim; a fixed external location (also the form
  the wrappers compute for the local `app.db`). Only a hand-typed external path is non-portable.
- **`:memory:`** — RAM-only, ephemeral (gone on exit); demos/tests. An explicit `SQLITE_PATH` (this,
  or any path) always wins over the wrapper's computed absolute default.

The SQLite factory FORCES the correctness pragmas (`foreign_keys=ON`, `case_sensitive_like=ON`) —
never the dev's job; tuning pragmas (`journal_mode=WAL`, `busy_timeout`) are overridable defaults.
SQLite is single-writer / single-node — the MVP, not-production posture (ASCII-only case folding,
decimal stored as TEXT). See `table-schema.html` for the Go↔SQLite type table + the MVP warning.

## `start.sh` (darwin|linux) — no compose, just run

```bash
#!/usr/bin/env bash
# Run <svc> in dev mode against SQLite — no bench, no Docker.
# Pin SQLITE_PATH to an ABSOLUTE path next to this script: under `go run` a
# relative file:app.db is unreliable — the migration step and the runtime can
# resolve it to different files ("migrations applied" then "no such table"), so
# the dev loop hands the engine an absolute DSN. The .db still lives in the
# project (recomputed each run → portable). An explicit SQLITE_PATH still wins.
set -euo pipefail
cd "$(dirname "$0")"
: "${SQLITE_PATH:=file:$(pwd)/app.db}"
export SQLITE_PATH
APP_PROFILE=dev CGO_ENABLED=0 go run -tags sqlite ./bootstrap
```

## `start.cmd` (windows, zero-friction)

```bat
@echo off
REM Pin SQLITE_PATH to an ABSOLUTE path next to this script: under `go run` a
REM relative file:app.db is unreliable (migration vs runtime resolve differently).
REM The .db still lives in the project. An explicit SQLITE_PATH wins. Forward
REM slashes in the file: URI (%CD:\=/% swaps backslashes) so it parses on Windows.
cd /d "%~dp0"
if "%SQLITE_PATH%"=="" set SQLITE_PATH=file:%CD:\=/%/app.db
set APP_PROFILE=dev
set CGO_ENABLED=0
go run -tags sqlite ./bootstrap
```

## `start.ps1` (windows, robust)

```powershell
#!/usr/bin/env pwsh
# Pin SQLITE_PATH to an ABSOLUTE path next to this script: under `go run` a
# relative file:app.db is unreliable (migration vs runtime resolve differently).
# The .db still lives in the project. An explicit SQLITE_PATH wins. Forward slashes
# in the file: URI so it parses on Windows.
Set-Location $PSScriptRoot
if (-not $env:SQLITE_PATH) { $env:SQLITE_PATH = 'file:' + ($PSScriptRoot -replace '\\','/') + '/app.db' }
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
