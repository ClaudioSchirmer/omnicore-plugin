# CDC relay template — `devops/debezium/application.properties`

Debezium Server for EVERY combination — one container, static config, no REST
registration step. Compose ONE file: the shared core + the chosen SOURCE block
(mysql | postgres | sqlserver | oracle) + the chosen SINK block (nats | kafka),
names from the spec.

**Validate before writing** (against the pinned `transport.html`): the payload format
(`simplestring` — pass the payload through as OPAQUE TEXT, never let the relay type
it), the header contract, and the topic/subject naming the consumers expect. The doc
wins over this template on any drift.

Properties-file escaping: `$${routedByValue}` (the `$$` is the literal-`$` escape).

Java `.properties` files have NO end-of-line comments — a trailing `# …` on a value
line becomes PART OF THE VALUE (`snapshot.mode=no_data   # comment` is an invalid
mode that kills the relay at boot). Every comment in the blocks below sits on its
own line; keep it that way in the generated file.

## Shared core — formats + the outbox EventRouter

```properties
# --- formats: opaque string payload, JSON key/headers ---
debezium.format.value=simplestring
debezium.format.key=json
debezium.format.header=json
debezium.format.value.schemas.enable=false
debezium.format.header.schemas.enable=false

# --- offsets survive restarts (file on the debezium_data volume) ---
debezium.source.offset.storage.file.filename=/debezium/data/offsets.dat
debezium.source.offset.flush.interval.ms=1000

debezium.source.tombstones.on.delete=false
# fresh service — stream only, no initial snapshot
debezium.source.snapshot.mode=no_data
debezium.source.poll.interval.ms=100

# --- one EventRouter: outbox -> the per-aggregate topic/subject ---
debezium.transforms=outboxRoute
debezium.transforms.outboxRoute.type=io.debezium.transforms.outbox.EventRouter
debezium.transforms.outboxRoute.predicate=isOutbox
debezium.transforms.outboxRoute.table.field.event.id=id
debezium.transforms.outboxRoute.table.field.event.key=aggregate_id
debezium.transforms.outboxRoute.table.field.event.type=event_type
debezium.transforms.outboxRoute.table.field.event.payload=payload
debezium.transforms.outboxRoute.route.by.field=aggregate_type
# route.topic.replacement is SINK-SPECIFIC — see the sink blocks below
debezium.transforms.outboxRoute.table.fields.additional.placement=aggregate_type:header,event_type:header,traceparent:header:traceparent,aggregate_id:header
debezium.transforms.outboxRoute.table.expand.json.payload=false

# --- predicate: match the outbox by its pre-reroute topic name ---
debezium.predicates=isOutbox
debezium.predicates.isOutbox.type=org.apache.kafka.connect.transforms.predicates.TopicNameMatches
# pattern is SOURCE-SPECIFIC — see the source blocks below
```

The `aggregate_id:header` placement is REQUIRED for NATS (JetStream has no
Kafka-style message key; the adapter reads the header) and harmless for Kafka (which
gets its key from `table.field.event.key`) — keep it in both.

## Source — mysql

```properties
debezium.source.connector.class=io.debezium.connector.mysql.MySqlConnector
# hostname is the in-network compose name; the binlog client needs root
debezium.source.database.hostname=mysql
debezium.source.database.port=3306
debezium.source.database.user=root
debezium.source.database.password=root
debezium.source.database.include.list=<svc>_db
# server.id must be UNIQUE per binlog client on this server — pick e.g. 1840NN,
# never reuse across services/relays
debezium.source.database.server.id=<unique>
debezium.source.topic.prefix=omnicore_<svc>
debezium.source.table.include.list=<svc>_db.outbox

# MySQL-only, both LOAD-BEARING:
# the binlog carries DDL from EVERY database on the server; an unparseable foreign
# statement otherwise stops the connector even for databases outside include.list
debezium.source.schema.history.internal=io.debezium.storage.file.history.FileSchemaHistory
debezium.source.schema.history.internal.file.filename=/debezium/data/schema-history.dat
debezium.source.schema.history.internal.skip.unparseable.ddl=true
# schema-change events go to a topic named after the prefix — no consumer, and on a
# NATS sink the publish fails with "No Responders" and KILLS the relay
debezium.source.include.schema.changes=false
```

Predicate pattern: `debezium.predicates.isOutbox.pattern=.*\\.<svc>_db\\.outbox`

## Source — postgres

```properties
debezium.source.connector.class=io.debezium.connector.postgresql.PostgresConnector
# hostname is the in-network compose name
debezium.source.database.hostname=postgres
debezium.source.database.port=5432
debezium.source.database.user=omnicore
debezium.source.database.password=omnicore
debezium.source.database.dbname=<svc>_db
debezium.source.topic.prefix=omnicore_<svc>
# pgoutput requires the container to run wal_level=logical
debezium.source.plugin.name=pgoutput
debezium.source.publication.autocreate.mode=filtered
debezium.source.table.include.list=public.outbox
```

Predicate pattern: `debezium.predicates.isOutbox.pattern=.*\\.public\\.outbox`
(No schema-history block — postgres does not need one.)

## Source — sqlserver

> **Provenance caveat — the only source block here NOT proven by a reference bench.**
> The proven sqlserver relay shape is Debezium **Connect** (REST-registered connector,
> Kafka-backed schema history) on a dedicated Kafka. This Debezium-Server adaptation
> (file-backed `FileSchemaHistory`) is the simple-bench transcription of that config;
> after bringing it up, PROVE it streaming (the "Starting streaming" log gate + a
> sentinel write reaching the broker) before trusting it — and on any trouble, prefer
> the Connect shape.

PREREQUISITE — CDC must be ENABLED before the connector can stream: the SQL Server
Agent on the container (`MSSQL_AGENT_ENABLED=true` in the bench) plus
`sp_cdc_enable_db` / `sp_cdc_enable_table` on the outbox — only possible AFTER the
first app boot creates the table, so the start wrapper carries the idempotent
enable arm (see `templates/docker-bench.md`). Until it lands the relay crash-loops;
`restart: unless-stopped` absorbs that window.

```properties
debezium.source.connector.class=io.debezium.connector.sqlserver.SqlServerConnector
# hostname is the in-network compose name; password is the bench's SA password
debezium.source.database.hostname=sqlserver
debezium.source.database.port=1433
debezium.source.database.user=sa
debezium.source.database.password=<strong-password>
# database.names is the PLURAL key on this connector; encrypt=false because the
# dev bench runs without TLS
debezium.source.database.names=<svc>_db
debezium.source.database.encrypt=false
debezium.source.topic.prefix=omnicore_<svc>
debezium.source.table.include.list=dbo.outbox

# schema history — required like MySQL (file-backed, survives restarts)
debezium.source.schema.history.internal=io.debezium.storage.file.history.FileSchemaHistory
debezium.source.schema.history.internal.file.filename=/debezium/data/schema-history.dat
# schema-change events have no consumer; on a NATS sink the publish KILLS the relay
debezium.source.include.schema.changes=false
```

Predicate pattern: `debezium.predicates.isOutbox.pattern=.*\\.dbo\\.outbox`

## Source — oracle

PREREQUISITES — two layers, both provisioned by the bench
(`templates/docker-bench.md`): database-level at the DB's first boot (ARCHIVELOG,
supplemental logging, the `c##dbzuser` LogMiner user, the seeded
`debezium_heartbeat` table) and per-table supplemental logging on the outbox —
only possible AFTER the first app boot creates it (the start wrapper's idempotent
arm). Until both land the relay crash-loops; `restart: unless-stopped` absorbs
that window. Expect a ~2-3s write→doc floor — the LogMiner mining cadence, not a
bug.

```properties
debezium.source.connector.class=io.debezium.connector.oracle.OracleConnector
# hostname is the in-network compose name; the LogMiner user is the COMMON user
debezium.source.database.hostname=oracle
debezium.source.database.port=1521
debezium.source.database.user=c##dbzuser
debezium.source.database.password=<strong-password>
# CDB + PDB pair: LogMiner attaches to the CDB, the tables live in the PDB
debezium.source.database.dbname=FREE
debezium.source.database.pdb.name=FREEPDB1
debezium.source.topic.prefix=omnicore_<svc>
# schema = the app user, UPPERCASE (the Oracle catalog form)
debezium.source.table.include.list=OMNICORE.OUTBOX

# schema history — required like MySQL (file-backed, survives restarts); BOTH
# extra knobs are LOAD-BEARING: the DDL parser dies on 23ai syntax from OTHER
# schemas/tables without them
debezium.source.schema.history.internal=io.debezium.storage.file.history.FileSchemaHistory
debezium.source.schema.history.internal.file.filename=/debezium/data/schema-history.dat
debezium.source.schema.history.internal.skip.unparseable.ddl=true
debezium.source.schema.history.internal.store.only.captured.tables.ddl=true
# schema-change events have no consumer; on a NATS sink the publish KILLS the relay
debezium.source.include.schema.changes=false

# LOAD-BEARING: the CDC-tailed payload columns are CLOB by framework design
# (LogMiner cannot decode native-JSON/OSON redo — pinned table-schema.html,
# Oracle column shapes); lob.enabled makes the connector deliver their values
debezium.source.lob.enabled=true
debezium.source.log.mining.strategy=online_catalog

# tight mining cadence — default sleeps push the write→doc floor further
debezium.source.log.mining.sleep.time.default.ms=100
debezium.source.log.mining.sleep.time.max.ms=300
debezium.source.log.mining.sleep.time.increment.ms=100

# LogMiner holds the LAST event of a burst until new redo advances the SCN — the
# periodic UPDATE keeps it moving (table seeded by the bench's init script)
debezium.source.heartbeat.interval.ms=1000
debezium.source.heartbeat.action.query=UPDATE debezium_heartbeat SET ts = SYSTIMESTAMP WHERE id = 1
```

Predicate pattern: `debezium.predicates.isOutbox.pattern=.*\\.OMNICORE\\.OUTBOX`
(schema AND table UPPERCASE — the catalog form).

**Oracle-only EventRouter override — UPPERCASE fields, lowercase header aliases.**
Column names arrive UPPERCASE from the Oracle catalog, so the shared core's
lowercase `table.field.event.*` values do not match on this source. Override them
with the UPPERCASE forms and alias every header back to lowercase, so the wire
contract stays IDENTICAL to the other dialects:

```properties
debezium.transforms.outboxRoute.table.field.event.id=ID
debezium.transforms.outboxRoute.table.field.event.key=AGGREGATE_ID
debezium.transforms.outboxRoute.table.field.event.type=EVENT_TYPE
debezium.transforms.outboxRoute.table.field.event.payload=PAYLOAD
debezium.transforms.outboxRoute.route.by.field=AGGREGATE_TYPE
debezium.transforms.outboxRoute.table.fields.additional.placement=AGGREGATE_TYPE:header:aggregate_type,EVENT_TYPE:header:event_type,TRACEPARENT:header:traceparent,AGGREGATE_ID:header:aggregate_id
```

**NATS sink trap:** the heartbeat records are PUBLISHED too
(`__debezium-heartbeat.<prefix>`); with `create-stream=false` an uncovered publish
503s and kills the connector — the start wrapper pre-creates the tiny
`DEBEZIUM_HEARTBEAT` stream (`templates/docker-bench.md`). Kafka sinks
auto-create the topic — no step needed.

## Sink — nats

```properties
debezium.sink.type=nats-jetstream
debezium.sink.nats-jetstream.url=nats://nats:4222
# the FRAMEWORK owns the stream (created at app boot) — the relay only
# publishes into it
debezium.sink.nats-jetstream.create-stream=false
```

Route (in the shared EventRouter block):
`debezium.transforms.outboxRoute.route.topic.replacement=omnicore.$${routedByValue}.events`
— the `omnicore.`-prefixed subject lives inside the framework-owned stream.

## Sink — kafka

```properties
debezium.sink.type=kafka
# bootstrap.servers is the in-network listener
debezium.sink.kafka.producer.bootstrap.servers=kafka:9092
debezium.sink.kafka.producer.key.serializer=org.apache.kafka.common.serialization.StringSerializer
debezium.sink.kafka.producer.value.serializer=org.apache.kafka.common.serialization.StringSerializer
```

Route (in the shared EventRouter block):
`debezium.transforms.outboxRoute.route.topic.replacement=$${routedByValue}.events`
— Kafka topics carry NO `omnicore.` prefix (reference: the consumer's Kafka lane).
Confirm both namings against the pinned `transport.html` — the doc wins on drift.
(Debezium Server's Kafka sink JSON-quotes header values; the framework's consumer
normalizes both quoted and bare — documented in `transport.html`.)

## Later, out of start scope

- **Integration events**: when the service enables them, the relay gains a SECOND
  predicate-gated EventRouter tailing `integration_events` (see the reference
  consumer's QA bench config for the proven shape) — and on sqlserver that table
  needs its own `sp_cdc_enable_table`. Do not add it to a fresh shell.
- **A second engine/transport**: a new combination means a new relay config (and on
  MySQL a new unique `server.id`) — a separate step, never part of this start.
