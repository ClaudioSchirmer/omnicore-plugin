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

## The decision — read it top to bottom, first YES wins

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

## What it deliberately does not do — say this out loud, every time

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
- an entity / sibling / shared-base / external schema as the repository's **anchor**. As a
  **join target** the same schema is welcome and is the intended reuse: a traversal only
  reads, so a control table joins another aggregate through the very schema that aggregate
  already declares;
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

## Two shapes that will be asked for by their old names

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
