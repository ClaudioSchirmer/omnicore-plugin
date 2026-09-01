# shared/direct-schema.md — one table, no aggregate: which DOOR into the database

The single home for one decision: **this persisted table — does it go through an aggregate,
or through a Direct schema?** Every skill that adds a persisted resource, writes a
hand-written query, or diagnoses a write that produced no view routes HERE instead of
answering it locally. No code here, by design — the vocabulary and the decision only; every
constructor name, signature and option is the PIN's (`direct-schema`, under Infrastructure,
beside `table-schema`).

It exists because the answer that has always been right is about to be right LESS OFTEN, and
a rule that stopped being true is worse than one that never existed. Until this feature the
relational engine had exactly one door — a repository bound to a `domain.Entity` — so a
control table with no rules, no lifecycle and no read model still had to be an entity with an
empty `BuildRules`, or it fell out of the framework entirely into hand-written SQL against
the neutral transaction, with placeholders, identifier quoting and id encoding re-derived per
dialect. Both answers are still in the other shared files; this one says when they stop being
the answer.

## Availability — pin ≥ v0.64.0, and the test is mechanical

**Direct is NOT in every release.** It shipped in framework **v0.64.0**; on anything older
the door does not exist. The version is named here the way `read-joins.md` names its own —
as the fact a reader needs to place the feature in time — and it is never the thing an agent
DECIDES on, because a stamped version drifts and a pin does not.

**The test, always run against the project's own pin:** does the pinned framework's
`docs/content/sections/` carry `direct-schema.html` (equivalently: does `nav.json` list
`direct-schema` under Infrastructure)? Present → the door exists in this project and
everything below applies. Absent → the pin predates the feature, and this file's only correct
output is the OLD answer plus, if the developer wants it, a route to `/omnicore:upgrade`.
Never design a consumer against a door its pin does not have; the failure is an undefined
symbol at build time, three steps after the decision.

## What it is — and what it is NOT

**It is the same engine, reached without an entity.** The declaration produces the same
`TableSchema` every other path consumes, born from its own constructor beside the four that
already exist; the reads run through the same field resolution, the same criteria compiler,
the same declared joins and the same aggregate DSL an aggregate loader runs through, and the
writes go through the same statement builders, the same dialect seam and the same
framework-minted identity. There is no conversion step and no second engine — which is
exactly why "we'll just write the SQL by hand" was never the right shape and is now not even
the cheap one.

**It is not a lighter aggregate.** Everything an aggregate gives you is absent, deliberately.
See the two lists below before proposing it for anything.

**The two axes — the one sentence that settles "why are children refused but joins fine?"**
The DOWNWARD composition an aggregate is — root plus children, satellites and shared identity
persisted as ONE unit — is gone: a Direct write is one statement against the anchor table.
The SIDEWAYS reach is untouched: a read traverses its declared joins with the same rules and
the same reach an aggregate repository has. So "it cannot compose" is the wrong summary; it
composes horizontally and not vertically, and every refusal below falls out of that.

## ⚠️ THE READ IS UNRESTRICTED — READ THIS BEFORE DESIGNING ANY QUERY

**A DIRECT SCHEMA PLUS A DIRECT REPOSITORY IS A FULLY MANUAL QUERY DOOR. THERE IS NO LIMIT
ON WHAT A DIRECT READ MAY SELECT OR JOIN. THE FRAMEWORK ASSEMBLES THE SELECT YOU DECLARE
AND VALIDATES ONLY THAT THE COLUMNS AND THE GO FIELDS EXIST — IT NEVER ASKS WHETHER THE
TABLES ARE RELATED.**

This section exists because the failure it stops has already happened, repeatedly: an agent
decides a query "is not possible in this framework" and then PAYS for that belief — an N+1
loop, whole aggregates loaded to fold three numbers in Go, a denormalized column bolted onto
the write path, a Mongo view invented for a one-off report, or the aggregate repository bent
into serving a report shape it was never for. **None of it was necessary. The limit was
imagined.** What is actually yours:

- **THE ANCHOR IS YOURS.** Any table. One that never had an aggregate → `NewDirectSchema`.
  One that DOES have an aggregate → the reduction, `AsDirectSchema()`. The anchor does not
  have to be "a control table": the decision table below answers the WRITE question, and a
  read has no such question to answer.
- **THE TARGET IS YOURS.** Any table you can hand a Direct schema for. **NO DECLARED FOREIGN
  KEY IS REQUIRED — no referential constraint, no relationship of any kind.** The declaration
  is checked for exactly four things: the target is ONE table, the key column exists on the
  joining side, each mapped column exists on the target, and the Go field can receive it.
  Two tables nothing relates join exactly as well as two a constraint ties together.
- **THE DEPTH IS YOURS.** Chains continue past the target with no depth limit and kinds mix
  freely — and a chain on a Direct anchor logs NO boot advisory at any depth, because the
  reach rides only the reads you issue. `read-joins.md` owns the mechanics; every rule there
  applies here unchanged.
- **THE PREDICATE HAS ONE FIXED SHAPE, AND IT IS THE ONLY FIXED THING:**
  `target.<the column that target schema's ID(...) names> = joining.<any declared column>`.
  Equi-join, one column each side. Because a Direct schema names its OWN id slot, **the join
  key on the target side is a CHOICE, not a given** — which is how a read reaches from a
  PARENT down into a CHILD table: declare a read-only Direct schema over the child whose
  `ID(...)` is the foreign-key column, and the traversal renders `child.<fk> = parent.id`.
  What is genuinely not expressible is a non-equality predicate, several columns on one side,
  or an `ON` carrying more than that one comparison — everything else belongs in the criteria.
- **THE WHOLE CRITERIA SURFACE RIDES ALONG.** Joined fields are filterable, orderable,
  groupable and usable in the aggregate DSL under the Go names YOU chose; subqueries,
  `Exists`, windows and the archived scope work exactly as they do anywhere else.
- **A DIRECT READ COSTS NOTHING.** The guarantees this file says the Direct door does not
  carry are about the **WRITE**. Reading an aggregate's own table through a reduced schema
  takes nothing away from that aggregate — no outbox row was due, no audit line, no revision
  guard, no projection; reading never owed any of them. **Name the trade-off out loud on a
  Direct WRITE, and never on a Direct READ, because there is none to name.**

### What the AGGREGATE repository genuinely cannot do — and why that is not a framework limit

The aggregate loader's statement shape is fixed by what it must RETURN: one row per root,
plus one batched SELECT per child collection, hydrated into entities. That shape — not the
framework — is what refuses the three things an agent keeps misreading as a framework limit:

- a 1:N traversal from the root into its OWN child table as a flat join: it would multiply
  the root's rows and break both the paged read and the hydration;
- filtering or sorting the root by a field of a 1:N child (a child join's field is load-only);
- anything report-shaped — several rows per root, arbitrary grouping, a projection that is
  not this aggregate.

And every join declared on an aggregate repository **rides EVERY read through that loader,
`FindByID` included — the load the write-side handlers go through.**

**So none of that is "impossible in the framework". It is the wrong door.** A report-shaped,
join-heavy, many-rows-per-root read is a Direct anchor's job, where the row type is a storage
shape you define and nothing is being hydrated into an aggregate. **NEVER reuse an aggregate
repository for a query whose shape it cannot hold, and NEVER tell the developer their query
cannot be done: build the Direct anchor and write the query they asked for.**

### Where the freedom stops — state these, and do not overclaim either

- **A Direct read is served by code you write** — a custom query handler, a custom command
  handler, or a domain service. It is not an `AggregateReader`, so it cannot back a
  relational read model, and it never backs a Mongo projection.
- **The row is a FLAT struct.** Joined columns land on ordinary exported scalar fields; no
  domain type crosses, and an identity crosses as canonical text (`read-joins.md`).
- **The envelope and the authorization belong to the surface, not to the door.** Pagination,
  the wire shape and any tenant / permission restriction are what YOUR handler builds into
  the criteria — a Direct read has no read model behind it to supply them.

## The decision — read it top to bottom, first YES wins

**This table answers the WRITE question — which door a persisted table is MAINTAINED
through.** A READ never has to pick: see the section above.

| If the table… | Door |
|---|---|
| has business rules the framework must enforce on write (`BuildRules` with anything in it) | **aggregate** |
| has a lifecycle the framework drives — modes, archive/unarchive cascade, revision guard | **aggregate** |
| is projected into a read model — any Mongo-backed view, any view kind at all | **aggregate** (a Direct write emits NO outbox row, so no CDC, no projection, ever) |
| must leave an audit trail, or raise domain / integration events | **aggregate** |
| needs optimistic concurrency (two concurrent updates must not silently overwrite) | **aggregate** |
| has children, a sibling facet, or a shared base | **aggregate** (declaring one on a Direct schema panics at declaration) |
| is a control table the service maintains by hand — a job queue, an import ledger, a lookup, an idempotency ledger: query, insert, update, delete, and nothing above `infra/` names it | **Direct** |
| is only ever ASKED a scalar — "does a row exist", "how many children does this aggregate have", "what do they total over this window" | **Direct** (a read-only anchor; see the fact case below) |
| is written from a lifecycle hook and must commit or roll back with the write that caused it | **Direct**, bound to the caller's open transaction |

**Both columns can be right for one service and even for one request** — a Direct control row
stamped inside an aggregate write's transaction is the shape the feature was designed around.
What is never right is reaching for Direct to skip an aggregate's guarantees on a resource
that needs them; that trades every promise the write path makes for a shortcut, and nothing
in the build will say so.

## What a WRITE deliberately does not do — say this out loud, every time

**Every line below is about the WRITE.** A Direct READ gives up nothing at all — reading
owes an aggregate none of these, so none of them is a cost a query has to justify.

No outbox row · no audit event · no domain events · no revision guard · no old-state snapshot
· no cascade · no lifecycle hooks.

The consequences are concrete and belong in any proposal that offers Direct, because each one
is a question the developer would otherwise discover later:

- **A Direct write never feeds a view.** No outbox row means no CDC and no projection. A
  resource the user is going to LIST through the read side is an aggregate, full stop.
- **It leaves no audit trail.** If "who changed this row, and when" is a question anyone will
  ask, that is the aggregate path's answer, not this one.
- **It carries no optimistic concurrency.** Two concurrent updates are last-writer-wins.
- **The multi-row verbs report a COUNT, not a status.** There is no 404/409 split beyond the
  single-row verbs' not-found.

## Two verbs the design depends on, when the pin has them

The door gained capability after it shipped, so the same mechanical test applies per verb —
does the pin's `direct-schema` section describe it? — and the design changes if it does.

**`Upsert` — insert-or-update on a DECLARED CONFLICT KEY, in one statement.** It is the
answer to the shape a control table almost always has: two callers race on the same key and
both decide the row is missing. Without it the honest options were a SELECT-then-INSERT that
loses the race, or a unique-violation caught and retried.

    w.Upsert(ctx, write.Values{
        "Identity":        id,
        "IdentityKind":    kind,
        "Outcome":         "FAILURE",
        "TotalCount":      write.Stamp,                   // += 1, server-side
        "WindowStartedAt": write.OnInsert(write.Stamp),   // dated once, never re-dated
        "RepeatedAt":      write.OnUpdate(write.Stamp),   // only a SECOND arrival has one
        "LastAt":          write.Stamp,                   // the operation's instant
        "LastIP":          ip,                            // overwritten on conflict
    }, write.OnConflict("Identity", "IdentityKind", "Outcome"),
       write.KeepArchiveStateOnConflict())

Every slot says what happens ON A CONFLICT, and the slots are the kinds of column an upsert
has: an ordinary value is overwritten, `write.Stamp` is filled by the framework on BOTH paths,
`write.OnInsert(v)` is established at creation and never revised, `write.OnUpdate(v)` binds
only when the row was already there, and the conflict key itself is insert-only by definition
— it is the thing that matched. The key is named PER CALL, by Go field name, because one table
legitimately has more than one way to be conflicted on.

**Both wrappers take a stamp verb, not only a value — pin ≥ v0.67.0, same mechanical test**
(does the pin's `direct-schema` section show `write.OnUpdate`, and a `write.Stamp` inside a
wrapper?). `write.OnInsert(write.Stamp)` dates a creation and never re-dates it,
`write.OnUpdate(write.Stamp)` dates only the collision, and `write.StampNull` /
`write.StampEmpty` scope the same way. That pairing is the ONLY way to write one half of the
statement with the framework's OWN instant, and the reason it cannot be done by binding a
value is `relational.clock: db`: the instant is read from the very transaction the statement
runs in, so the caller has nothing to compute it from. On a pin without the wrapped form, a
column dated once is a constraint to state — never a hand-computed `time.Now()` smuggled in.

`OnUpdate` also shapes the TABLE, so the migration settles it at design time: the column is
left out of the proposed row ENTIRELY, so the creating path takes its DEFAULT, and `NOT NULL`
with no `DEFAULT` makes that path fail.

Three things belong in any proposal that reaches for it:

- **MySQL is the documented exception.** `ON DUPLICATE KEY UPDATE` fires on ANY unique key the
  row violates, not the one named. With a single unique key the behaviour is identical
  everywhere; with more than one, MySQL may resolve a conflict the others would have let fail.
  A table designed for upsert on MySQL wants exactly one unique key.
- **A schema declaring the archive column MUST state which way the conflict goes** —
  `write.UnarchiveOnConflict()` or `write.KeepArchiveStateOnConflict()`. This is the one write
  that cannot be archive-gated: `INSERT … ON CONFLICT` has no `WHERE` for its conflict target,
  so an archived row still holds the unique key and still absorbs the write. Both answers are
  defensible and the framework refuses to pick; so does any proposal that offers this verb.
- **It returns only an error, and that is deliberate.** A row count cannot mean the same on
  every backend (MySQL reports 2 for a conflicting upsert, 1 for an inserting one; the others
  report 1 for both). Do not design a caller that branches on "was it an insert".

**`write.Stamp` — the Direct half of the stamped family.** A Direct write has no entity to
carry a request, so the ask rides the only channel it has: a marker in the `Values` map, where
an ordinary write puts a value.

    w.Update(ctx, write.Values{"Status": "PAID", "PaidAt": write.Stamp}, q)

What it may mark is what the SCHEMA declared with `StampedTimeField` / `StampedCounterField`
(the full contract is owned by `../skills/scaffold-entity/conventions/infra.md` — a Direct
schema declares them the same way an entity schema does). Marking a plain field, or binding a
real value into a stamped slot, is a typed write-time error rather than a silent pass. This
is what the refusal below means by "a framework-stamped timestamp written by hand": the
column is asked for, never assigned.

## Refusals to design around, not into

Each one fails loudly — at declaration, at construction, or before any statement runs — which
is good, and it is still cheaper to know them at design time:

- children, siblings and a shared base on a Direct schema — vertical composition is the
  aggregate's job;
- an entity / sibling / shared-base / external schema as the repository's **anchor**, as
  declared. The reuse is still the intended one — a traversal only reads, so a control table
  reaches another aggregate through the very schema that aggregate already declares — but on
  a pin that carries the reduction it is the REDUCED copy that travels, both as a join target
  and as an anchor (see below);
- a schema with no id column, or a row type with no exported identity field — identity is
  neither optional nor inferred, and a row read back without it could not be the target of the
  next write;
- the identity, or a framework-stamped timestamp, written by hand in the values map — those
  have one origin, and the archive transition has its own verbs;
- **a write with an empty predicate** — a criteria assembled conditionally that came out empty
  would become a full-table sweep with nothing at the call site showing it. The deliberate
  sweep has its own verb, so the call site says it;
- **`write.OnInsert` / `write.OnUpdate` outside an `Upsert`, nested in each other, or on a
  conflict-key field** — an `Insert` is insert-only already, an `Update` has no insert path,
  and the key is written once by definition (the MERGE dialects reject assigning a join
  column). All three are raised before a transaction is opened;
- a second match where exactly one row was expected;
- joins into a child — there is no child; the reach here is horizontal only.

## A second way to get one — reducing a schema that already exists

`NewDirectSchema` declares a table that never had an aggregate. The other road starts from a
schema that does: **`AsDirectSchema()` returns a COPY of any schema limited to its own
table**, with the children, the siblings and the shared base dropped and the original
untouched.

**Shipped in framework v0.68.0; the same mechanical test as everything else here:** does the
pin's `direct-schema` / `table-schema` section describe the reduction? Absent → the pin predates it and this whole
section does not apply; a join target is passed as declared and an aggregate's table has no
Direct door at all.

It exists because two consumers put exactly ONE table in their `FROM` — a read join's TARGET
and a criteria subquery's SOURCE — and therefore take a schema that is one table rather than
a node they could only read in part. Where the pin has it, that is not a preference: both
refuse anything else, and `read-joins.md` owns what it means for the declarations.

**What it means for THIS decision, and it is the sharper half.** The result is an ordinary
Direct schema — including as a repository's anchor. So an aggregate's own table CAN be
written through the Direct door, and everything at the top of this file applies unchanged:
no outbox row, so no CDC and no projection; no audit event; no revision guard; no old-state
snapshot; no cascade; no lifecycle hooks. A write made that way never feeds the view the
aggregate otherwise keeps in step — which is a real escape hatch, left open deliberately
rather than by oversight.

Treat it as one. The decision table above still answers the question: a table with rules, a
lifecycle, a view or an audit trail goes through its aggregate, and reaching for the reduced
form to skip them is the exact trade this file warns about, now available on a table where
the guarantees actually exist. Where it is genuinely right — a maintenance sweep, a column
no domain rule owns — say out loud, in the proposal, which of the seven guarantees the write
is giving up.

**Two kinds do not convert**, and both panic where they are written: a SIBLING borrows its
owner's primary key, so it is not a row source (and reducing the owner yields the OWNER's
table — a facet's columns leave with the facet; to read that table standalone, declare it as
its own Direct anchor over the shared id column), and an EXTERNAL schema names an upstream
service's mirrored collection, which is not a table on this connection at all.

## What it keeps, because it is the same funnel

The criteria vocabulary in every dialect · the single field-resolution surface (including the
fixed logical names the framework answers for — the id, the parent link, the stamped
timestamps) · declared read joins on the root · the archived scope gate when the schema
declares the archive column · framework-minted identity · the typed unique-violation
notification · the transaction.

That last one is the whole reason this beats hand-written SQL even for a two-column table: the
same criteria works on all four engines, and nobody re-derives placeholder syntax or id
encoding per dialect.

## Where the artifacts land

A Direct row is a **storage shape, not a domain concept** — it is named after the table, not
after a business term, and nothing above `infra/` has a reason to mention it. The row struct
and its schema live in `infra/`, beside each other. This is the one case where "a persisted
type still needs a domain struct" does NOT hold, and it is why `domain-membership.md` and
`notification-bases.md` both route here: their answer is right for a persisted resource the
FRAMEWORK'S REPOSITORY writes, and a Direct table is written by a different door.

The rest is unchanged: any rejection this code raises is an application or infrastructure
notification declared beside its raiser (`notification-bases.md` owns that), and the
repository is wired from the relational engine every repository in the service already
receives.

## Three shapes that will be asked for by their old names

- **"A couple of endpoints with a table behind them, the logic is in the handler."** Ask the
  decision table before answering. It is Direct when nothing lists it, audits it or projects
  it; it is still an aggregate with an empty `BuildRules` the moment a view, an audit trail or
  a lifecycle appears. Say WHICH, and why, rather than picking the newer door because it is
  newer.
- **"A rule needs a fact from another table."** Direct is the supported anchor for the probe
  (existence, count, total, extreme, per-key breakdown) — but it is the SECOND question.
  `query-primitives.md` owns the first one (which primitive answers the question at all) and
  `read-joins.md` owns the case where the value is a field of an aggregate this entity already
  points at. Reach for a Direct anchor when the truth lives in a table this service has no
  aggregate for — another aggregate's child table, a control table, a lookup.
- **"A report / a fast listing that joins several tables / can we just run THIS query?"**
  **YES — and the answer is a Direct anchor, never a contortion of the aggregate repository.**
  This is the shape the top section of this file exists for. Declare the anchor (any table,
  reduced from an aggregate's schema if that is where the rows are), declare the traversals
  the query needs, write the criteria, and serve it from a handler you write. Do NOT
  denormalize a column, do NOT loop a per-row query, do NOT load aggregates to fold the
  answer in Go, and do NOT project a Mongo view for a query that has no read model behind it.
  The only honest refusals are the three at the end of that section — the fixed `ON` shape,
  the flat row, and the fact that the surface is yours to write.
