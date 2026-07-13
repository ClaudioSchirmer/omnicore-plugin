# CDC relay template — `devops/debezium/application.properties`

Debezium Server for EVERY combination — one container, static config, no REST
registration step. Compose ONE file: the shared core + the chosen SOURCE block
(mysql | postgres) + the chosen SINK block (nats | kafka), names from the spec.

**Validate before writing** (against the pinned `transport.html`): the payload format
(`simplestring` — pass the payload through as OPAQUE TEXT, never let the relay type
it), the header contract, and the topic/subject naming the consumers expect. The doc
wins over this template on any drift.

Properties-file escaping: `$${routedByValue}` (the `$$` is the literal-`$` escape).

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
debezium.source.snapshot.mode=no_data          # fresh service — stream only
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
debezium.source.database.hostname=mysql        # in-network compose name
debezium.source.database.port=3306
debezium.source.database.user=root             # binlog client needs root
debezium.source.database.password=root
debezium.source.database.include.list=<svc>_db
debezium.source.database.server.id=<unique>    # UNIQUE per binlog client on this
                                               # server — pick e.g. 1840NN, never
                                               # reuse across services/relays
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
debezium.source.database.hostname=postgres     # in-network compose name
debezium.source.database.port=5432
debezium.source.database.user=omnicore
debezium.source.database.password=omnicore
debezium.source.database.dbname=<svc>_db
debezium.source.topic.prefix=omnicore_<svc>
debezium.source.plugin.name=pgoutput           # container must run wal_level=logical
debezium.source.publication.autocreate.mode=filtered
debezium.source.table.include.list=public.outbox
```

Predicate pattern: `debezium.predicates.isOutbox.pattern=.*\\.public\\.outbox`
(No schema-history block — that is a MySQL-only concern.)

## Sink — nats

```properties
debezium.sink.type=nats-jetstream
debezium.sink.nats-jetstream.url=nats://nats:4222
debezium.sink.nats-jetstream.create-stream=false   # the FRAMEWORK owns the stream
                                                   # (created at app boot) — the relay
                                                   # only publishes into it
```

Route (in the shared EventRouter block):
`debezium.transforms.outboxRoute.route.topic.replacement=omnicore.$${routedByValue}.events`
— the `omnicore.`-prefixed subject lives inside the framework-owned stream.

## Sink — kafka

```properties
debezium.sink.type=kafka
debezium.sink.kafka.producer.bootstrap.servers=kafka:9092   # in-network listener
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
  consumer's QA bench config for the proven shape). Do not add it to a fresh shell.
- **A second engine/transport**: a new combination means a new relay config (and on
  MySQL a new unique `server.id`) — a separate step, never part of this start.
