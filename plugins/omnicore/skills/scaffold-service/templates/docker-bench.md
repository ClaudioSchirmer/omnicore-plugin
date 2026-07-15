# Docker bench template — `devops/docker-compose.yml` + `start.sh` / `start.cmd` / `start.ps1`

Devops glue (the sanctioned template exception). Instantiate for the ONE chosen
dialect × transport combination; names and ports come from the approved spec. The
shapes below are proven by the reference consumer's benches (its dev bench and QA
lanes) — keep the healthchecks and the relay restart policy; they are load-bearing.

## Naming & ports

- Compose project: `name: <svc>-dev` · containers: `<svc>-dev-<component>`.
- Volumes named per component (`db_data`, `mongo_data`, `nats_data`/`kafka_data`,
  `debezium_data`).
- **Standard host ports** (default when free): app `8080`, postgres `5432` / mysql
  `3306` / sqlserver `1433`, mongo `27017`, nats `4222` (+ monitor `8222`), kafka
  external `9094`.
- **Shifted ports** when Phase 0 found collisions: pick free ports near the standard
  ones (the reference QA bench shifts to mysql `3317` / mongo `27028` / nats `4232` to
  coexist with its dev bench — same idea). The YAML's `${VAR:default}` defaults must
  match whatever the compose publishes.

## Compose skeleton — pick ONE relational + ONE broker block

Top of file: a comment stating what the bench is (this service's exclusive local dev
bench: relational + Mongo + broker + CDC relay), and how to run it (`./start.sh` on
Unix/WSL; `start.cmd` or `pwsh -File .\start.ps1` on Windows).

### Relational — mysql variant

```yaml
  mysql:
    image: mysql:8.4                    # binlog defaults on 8.4 satisfy Debezium
    container_name: <svc>-dev-mysql
    ports: ["<hostport>:3306"]
    environment:
      MYSQL_ROOT_PASSWORD: root         # the relay tails the binlog as root
      MYSQL_USER: omnicore
      MYSQL_PASSWORD: omnicore
      MYSQL_DATABASE: <svc>_db
    volumes: [db_data:/var/lib/mysql]
    healthcheck:
      test: ["CMD-SHELL", "mysqladmin ping -h 127.0.0.1 -uomnicore -pomnicore --silent"]
      interval: 5s
      timeout: 5s
      retries: 15
```

### Relational — postgres variant

```yaml
  postgres:
    image: postgres:16-alpine
    container_name: <svc>-dev-postgres
    ports: ["<hostport>:5432"]
    environment:
      POSTGRES_USER: omnicore
      POSTGRES_PASSWORD: omnicore
      POSTGRES_DB: <svc>_db
    command:                            # logical decoding for the pgoutput relay —
      - "postgres"                      # WITHOUT this the relay can never attach
      - "-c"
      - "wal_level=logical"
      - "-c"
      - "max_wal_senders=10"
      - "-c"
      - "max_replication_slots=10"
    volumes: [db_data:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U omnicore -d <svc>_db"]
      interval: 5s
      timeout: 5s
      retries: 10
```

### Relational — sqlserver variant

The mssql image is amd64-only (Apple-Silicon hosts run it via Rosetta). It enforces
SA-password complexity — pick a strong password for the spec and use it EVERYWHERE
the bench references it (compose, healthcheck, relay source, the YAML's DSN default);
there is no `omnicore:omnicore` on this dialect. `MSSQL_AGENT_ENABLED` is
load-bearing: the CDC relay cannot stream without the Agent. And unlike
`MYSQL_DATABASE` / `POSTGRES_DB`, the image has NO auto-create-database env —
the start wrappers must create `<svc>_db` themselves (see the sqlserver-only
notes below) or the first app boot dies with `Cannot open database "<svc>_db"`.

```yaml
  sqlserver:
    image: mcr.microsoft.com/mssql/server:2022-latest
    container_name: <svc>-dev-sqlserver
    ports: ["<hostport>:1433"]
    environment:
      ACCEPT_EULA: "Y"
      MSSQL_SA_PASSWORD: "<strong-password>"  # image-enforced complexity
      MSSQL_AGENT_ENABLED: "true"             # LOAD-BEARING: CDC requires the Agent
      MSSQL_PID: "Developer"
    volumes: [db_data:/var/opt/mssql]
    healthcheck:
      test: ["CMD-SHELL", "/opt/mssql-tools18/bin/sqlcmd -C -S localhost -U sa -P '<strong-password>' -Q 'SELECT 1' -h -1"]
      interval: 10s
      timeout: 5s
      retries: 12
```

### Read side — always

```yaml
  mongo:
    image: mongo:7
    container_name: <svc>-dev-mongo
    ports: ["<hostport>:27017"]
    volumes: [mongo_data:/data/db]
    healthcheck:
      test: ["CMD", "mongosh", "--quiet", "--eval", "db.adminCommand('ping').ok"]
      interval: 5s
      timeout: 5s
      retries: 10
```

### Broker — nats variant

```yaml
  nats:
    image: nats:2.10-alpine
    container_name: <svc>-dev-nats
    command: ["-js", "-sd", "/data", "-m", "8222"]   # JetStream, file-backed (survives restart)
    ports:
      - "<hostport>:4222"
      - "<monitorport>:8222"
    volumes: [nats_data:/data]
    healthcheck:
      test: ["CMD-SHELL", "wget -q -O- http://localhost:8222/healthz >/dev/null 2>&1 || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 10
```

### Broker — kafka variant

Single-broker KRaft (no Zookeeper), TWO listeners: `PLAINTEXT` (`kafka:9092`) for
in-network services (the relay), `EXTERNAL` (`localhost:<hostport>`) for the Go app on
the host. The YAML's `transport.endpoints` default must be `localhost:<hostport>`.

```yaml
  kafka:
    image: confluentinc/cp-kafka:7.6.1
    container_name: <svc>-dev-kafka
    ports: ["<hostport>:9094"]
    environment:
      KAFKA_NODE_ID: 1
      KAFKA_PROCESS_ROLES: "broker,controller"
      KAFKA_CONTROLLER_QUORUM_VOTERS: "1@kafka:9093"
      KAFKA_LISTENERS: "PLAINTEXT://0.0.0.0:9092,CONTROLLER://0.0.0.0:9093,EXTERNAL://0.0.0.0:9094"
      KAFKA_ADVERTISED_LISTENERS: "PLAINTEXT://kafka:9092,EXTERNAL://localhost:<hostport>"
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: "PLAINTEXT:PLAINTEXT,CONTROLLER:PLAINTEXT,EXTERNAL:PLAINTEXT"
      KAFKA_INTER_BROKER_LISTENER_NAME: "PLAINTEXT"
      KAFKA_CONTROLLER_LISTENER_NAMES: "CONTROLLER"
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
      KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: 1
      KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: 1
      KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS: 0
      KAFKA_AUTO_CREATE_TOPICS_ENABLE: "true"    # relay publishes before any consumer exists
      CLUSTER_ID: "MkU3OEVBNTcwNTJENDM2Qk"
    volumes: [kafka_data:/var/lib/kafka/data]
    healthcheck:
      test: ["CMD-SHELL", "kafka-broker-api-versions --bootstrap-server localhost:9092 >/dev/null 2>&1 || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 10
```

### CDC relay — always (Debezium Server, config from `templates/cdc-relay.md`)

```yaml
  debezium-server:
    image: quay.io/debezium/server:3.6.0.Final
    container_name: <svc>-dev-debezium
    restart: unless-stopped             # LOAD-BEARING: the relay crash-loops until the
    depends_on:                         # first app boot creates the outbox (and, on
      <broker>:                         # NATS, the framework-owned stream); restart
        condition: service_healthy      # absorbs that window
      <relational>:
        condition: service_healthy
    volumes:
      - ./debezium:/debezium/config:ro
      - debezium_data:/debezium/data    # offsets + (mysql/sqlserver) schema history survive restarts
```

## `start.sh` / `start.cmd` / `start.ps1` — the one-command dev loop

Project root. ALL THREE ship together — same steps, one per host shell, so the bench
boots natively on Unix/WSL AND on Windows without WSL. On Windows both a batch and a
PowerShell wrapper ship: `start.cmd` is the zero-friction path (no execution policy,
double-click or `cmd`), `start.ps1` the robust one for devs who prefer it (proper
error handling, clean per-process env). `start.sh` is executable. Shapes:

```bash
#!/usr/bin/env bash
# Bring up the local bench and run <svc> in dev mode (always the dev profile).
set -euo pipefail
cd "$(dirname "$0")"

docker compose -f devops/docker-compose.yml up -d --wait
APP_PROFILE=dev go run -tags '<engine> <transport>' ./bootstrap
```

```bat
@echo off
REM Bring up the local bench and run <svc> in dev mode (always the dev profile).
cd /d "%~dp0"

docker compose -f devops/docker-compose.yml up -d --wait || exit /b 1
set "APP_PROFILE=dev"
go run -tags "<engine> <transport>" ./bootstrap
```

```powershell
#!/usr/bin/env pwsh
# Bring up the local bench and run <svc> in dev mode (always the dev profile).
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

docker compose -f devops/docker-compose.yml up -d --wait
$env:APP_PROFILE = 'dev'
go run -tags '<engine> <transport>' ./bootstrap
```

Notes:
- `--wait` blocks on the healthchecks — but NOT on the relay (no healthcheck by
  design: it cannot be healthy before the first app boot). If the compose version in
  use chokes on a restarting service under `--wait`, fall back to `up -d` + a small
  wait loop on the DB/broker healthchecks only.
- `APP_PROFILE=dev` → `./microservice.dev.yaml` — the start script ALWAYS points at
  the dev profile; prd is for real deployments, never for this script.
- First run: migrations create the outbox, the app creates the NATS stream (when
  NATS), the relay settles into streaming on its next restart — tell the user the
  read side goes live moments after the first boot, not before.
- **sqlserver only — create the database.** After `up -d --wait` and BEFORE the
  foreground `go run`, all wrappers run synchronously (idempotent):

  ```
  docker exec <svc>-dev-sqlserver /opt/mssql-tools18/bin/sqlcmd -C -S localhost \
    -U sa -P '<strong-password>' -Q "IF DB_ID('<svc>_db') IS NULL CREATE DATABASE <svc>_db"
  ```

  The image creates no databases on its own; without this step the app cannot
  connect on first boot.
- **sqlserver only — the CDC-enable arm.** On SQL Server the relay cannot stream
  until CDC is enabled on the database AND on the outbox table — and the table
  enable is only possible after the first app boot creates it. All three wrappers
  gain an idempotent arm, launched in the BACKGROUND before the foreground `go run`,
  that polls `<svc>_db` via `sqlcmd` (in-container `/opt/mssql-tools18/bin/sqlcmd`
  through `docker exec` works) until the outbox table exists, then runs (proven
  shape: the reference consumer's `register-connector.sh`):

  ```sql
  IF (SELECT is_cdc_enabled FROM sys.databases WHERE name='<svc>_db') = 0
    EXEC sys.sp_cdc_enable_db
  IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name='outbox' AND is_tracked_by_cdc=1)
    EXEC sys.sp_cdc_enable_table @source_schema=N'dbo', @source_name=N'outbox', @role_name=NULL
  ```

  Idempotent by the guards — safe on every start. The relay's `restart:
  unless-stopped` absorbs the window until the enable lands.
- `start.cmd` and `start.ps1` mirror `start.sh` step-for-step — keep all three in
  lockstep; any change to one applies to the others. On Windows the default execution
  policy may block a bare `.\start.ps1`, so document the PowerShell invocation as
  `pwsh -File .\start.ps1` (or `powershell -ExecutionPolicy Bypass -File .\start.ps1`);
  `start.cmd` has no such caveat and is the safe default to point a new dev at.
