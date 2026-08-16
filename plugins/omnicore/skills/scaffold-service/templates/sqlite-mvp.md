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
- Run: always the dev profile, from the BUILT binary —
  `CGO_ENABLED=0 go build -tags sqlite -o ./bin/<svc> ./bootstrap && APP_PROFILE=dev ./bin/<svc>`.
  A bare `go run` is fine for a throwaway foreground poke, but never for the start
  wrappers or for any boot that will be signalled: it does not forward SIGTERM.
- No `mongo:` and no `transport:` block in `microservice.dev.yaml` — the zero-infra
  DEFAULTS (both opt-out by absence, both reversible later via `/omnicore:configure`;
  a SharedBaseView would require re-adding `mongo:` — it boots and serves empty, no CDC
  feed).

## DSN — where the database lives

`relational.dsn` default `${SQLITE_PATH:file:app.db}`:
- **relative `file:app.db` (the yaml DEFAULT)** — the portable single-file story: the `.db` lives
  NEXT TO THE BINARY (a pendrive carries binary + `.db` together). The wrappers build into
  `bin/`, so a relative default would put the `.db` there rather than in the project — which
  is why they pin an ABSOLUTE `SQLITE_PATH` next to the script (recomputed each run, so the
  `.db` travels with the project) and why that pin is not optional decoration. It also gives
  one unambiguous path across OS shells. If you run it by hand with `go run` instead, the
  ephemeral-binary carve-out applies on BOTH sides — engine AND migration runner resolve
  against the working dir, mirrored deliberately since v0.44.2 — so that path migrates the
  database it serves; on a pin OLDER than v0.44.2 the runner resolved against the exe dir
  and the dev loop really did split (green boot, empty schema, `no such table`). The pin's
  changelog decides which world you are in. Parent dir auto-created.
- **absolute `file:/var/lib/app/app.db`** — used verbatim; a fixed external location (also the form
  the wrappers compute for the local `app.db`). Only a hand-typed external path is non-portable.
- **`:memory:`** — RAM-only, ephemeral (gone on exit); demos/tests. An explicit `SQLITE_PATH` (this,
  or any path) always wins over the wrapper's computed absolute default.

The SQLite factory FORCES the correctness pragmas (`foreign_keys=ON`, `case_sensitive_like=ON`) —
never the dev's job; tuning pragmas (`journal_mode=WAL`, `busy_timeout`) are overridable defaults.
SQLite is single-writer / single-node — the MVP, not-production posture (ASCII-only case folding,
decimal stored as TEXT). See `table-schema.html` for the Go↔SQLite type table + the MVP warning.

**The wrappers BUILD and then run the binary — never `go run`.** `go run` compiles to a
temp binary and runs it as a CHILD: it does not forward SIGTERM, so a wrapper started in
the background and signalled dies without the app ever draining — which is exactly the
shape of the verification boot every skill here performs (boot → SIGTERM → read the drain
narration), and it orphans the listener on the port. Building costs one incremental
compile and makes the wrapper's PID the app's.

## `start.sh` (darwin|linux) — no compose, just build and run

```bash
#!/usr/bin/env bash
# Run <svc> in dev mode against SQLite — no bench, no Docker.
# Build + exec, never `go run`: exec makes THIS pid the app, so SIGTERM reaches it
# and the drain runs. Under `go run` the signal stops at a parent that ignores it.
# Pin SQLITE_PATH to an ABSOLUTE path next to this script, so the migration step and
# the runtime can never resolve to different files ("migrations applied" then "no such
# table"). The .db still lives in the project (recomputed each run → portable). An
# explicit SQLITE_PATH still wins.
set -euo pipefail
cd "$(dirname "$0")"
: "${SQLITE_PATH:=file:$(pwd)/app.db}"
export SQLITE_PATH
CGO_ENABLED=0 go build -tags sqlite -o ./bin/<svc> ./bootstrap
APP_PROFILE=dev exec ./bin/<svc>
```

## `start.cmd` (windows, zero-friction)

```bat
@echo off
REM Build + run the binary, never `go run`: the signal must reach the app itself.
REM Pin SQLITE_PATH to an ABSOLUTE path next to this script (migration vs runtime must
REM resolve to ONE file). The .db still lives in the project. An explicit SQLITE_PATH
REM wins. Forward slashes in the file: URI (%CD:\=/% swaps backslashes) so it parses.
cd /d "%~dp0"
if "%SQLITE_PATH%"=="" set SQLITE_PATH=file:%CD:\=/%/app.db
set APP_PROFILE=dev
set CGO_ENABLED=0
go build -tags sqlite -o .\bin\<svc>.exe .\bootstrap || exit /b 1
.\bin\<svc>.exe
```

## `start.ps1` (windows, robust)

```powershell
#!/usr/bin/env pwsh
# Build + run the binary, never `go run`: the signal must reach the app itself.
# Pin SQLITE_PATH to an ABSOLUTE path next to this script (migration vs runtime must
# resolve to ONE file). The .db still lives in the project. An explicit SQLITE_PATH
# wins. Forward slashes in the file: URI so it parses on Windows.
Set-Location $PSScriptRoot
if (-not $env:SQLITE_PATH) { $env:SQLITE_PATH = 'file:' + ($PSScriptRoot -replace '\\','/') + '/app.db' }
$env:APP_PROFILE = 'dev'; $env:CGO_ENABLED = '0'
go build -tags sqlite -o .\bin\<svc>.exe .\bootstrap
.\bin\<svc>.exe
```

`bin/` is build output, not a document — it belongs in `.gitignore`.

Keep all shipped wrappers in lockstep. Invoke `start.ps1` as `pwsh -File .\start.ps1` (execution
policy); `start.cmd` is the safe default to point a new dev at.

## `.gitignore` additions (file-backed SQLite)

```
app.db
app.db-wal
app.db-shm
```

(The `-wal`/`-shm` sidecars appear under `journal_mode=WAL`. Nothing to ignore for `:memory:`.)
