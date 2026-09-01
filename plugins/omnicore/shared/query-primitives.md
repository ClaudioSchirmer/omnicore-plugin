# shared/query-primitives.md — which QUESTION you are asking the database, one owner

The single home for **choosing the read primitive** when writing code by hand: a domain
`Service` implementation, a custom repository finder, a custom command/query handler.
Every skill that writes or reviews one routes HERE instead of restating the choice
inline — one owner, no drift. No code here, by design: the vocabulary and the decision,
never a snippet; exact signatures are the PIN's — the criteria DSL in `criteria.html` where
the pin carries that section and inside `custom-command-handler.html` ("Loading by criteria")
where it does not, the primitives themselves in `custom-command-handler.html`'s
AggregateLoader part, plus `table-schema.html` for the scalar half.

## The rule

**A list load is for rows you are going to ITERATE. Every other question has a cheaper
primitive, and the framework ships all of them on the same criteria surface.**

The failure this file exists to stop is always the same shape: `FindAll` is reached for
first because it is the one method whose name obviously means "get me data", and then the
answer is computed in Go — `len(...)`, `> 0`, a running total, a loop that buckets the
rows by a key. That reads a whole table (plus one batched SELECT per child type, plus the
hydration of every aggregate it returns) to compute what the database computes in one
pass, on the WRITE path, inside the request.

Do not reason about whether the table is "small enough". The set a rule asks about is
the one that grows; the primitive costs nothing extra to pick correctly at write time.

## The decision

| The question being asked | Ask it with | Never |
|---|---|---|
| "is there one / is this taken / does this FK point at something" | the existence probe — `Exists(ctx, q)`, a bare `SELECT 1 … LIMIT 1`, hydrating nothing | `FindOne` + err check · `len(FindAll(…)) > 0` |
| "how many / what do they total, average, min, max" | the aggregate DSL — `Aggregate(ctx, q, specs…)`, any COMBINATION of `Count`/`SumInt`/`Sum`/`Avg`/`MinInt`/`MaxInt`/`Min`/`Max` in **one** SELECT | `len(FindAll(…))` · summing in Go |
| "…per key — counts/totals per group, how many distinct keys" | the grouped form — `AggregateBy(ctx, q, By(fields…), specs…)`; `len(groups)` IS the distinct-key cardinality | `FindAll` + bucketing in Go |
| "give me THE aggregate — by id, or by a key that identifies one" | `FindOne` (or the promoted `FindByID` / `FindArchivedByID`) | `FindAll(…)[0]` — see below, it is not just wasteful |
| "give me the rows, I am going to walk them" | `FindAll` — this is what it is for | — |
| a user-facing LISTING | the read side: the query handler → `ViewReader.ReadPage` | `FindAll` on the request path |
| a user-facing "how many match" | the request DTO's `onlyTotal` opt-in (`auto-query-handlers.html`) — no documents materialized | `ReadPage` + `len(Items)` |
| "the rule needs a field that belongs to ANOTHER aggregate" | a **read join** declared on the repository — the value becomes an ordinary field of the entity, filled on every load (`read-joins.md`) | a second `FindOne` inside the rule · copying the column into this table and keeping it in step |
| the truth lives in a table this service has **no aggregate for** — another aggregate's CHILD table, a control table, a lookup | a **Direct schema** anchoring the same primitives on that one table (`direct-schema.md`) — where the pin has it | hand-written SQL against the neutral transaction · promoting the table to an aggregate just to be able to ask |
| "which rows have at least one / no row over THERE pointing back at them" — the 1:N reverse filter | a **subquery** in the criteria — `Exists` / `NotExists` over the other table, correlated with `Outer(...)` (below) | loading the collection and filtering in Go · a first query to collect ids and a second to filter by them |
| the right-hand side of a comparison is itself a SELECT — "in the set this other table defines", "greater than the max over there" | the same **subquery**, under the operator you already wanted — `InSub`/`NinSub`, `EqSub`/`GtSub`/… (below) | two round trips with the ids carried between them in Go — which also loses the snapshot |

**`FindAll(…)[0]` is a correctness bug, not only a slow one.** `FindOne` is the
framework's BIRTH point for a write-side entity: it stamps the old-state snapshot, so
`domain.Old[T]` answers the PERSISTED state for every state-changing verb. `FindAll`
deliberately does not snapshot — it is the list path, where nothing mutates and the
per-row clone would be pure cost. An entity loaded through it and then written has no
old state, and every rule and every audit line that reads `Old` is quietly wrong.

## Which ANCHOR the primitive hangs off — the second question

**⚠️ AND IT IS THE ONE THAT GETS ANSWERED WRONG. THE SHAPE OF THE QUERY IS NEVER A REASON TO
GIVE UP: A `DirectRepository` OVER A DIRECT SCHEMA IS A FULLY MANUAL DOOR — ANCHOR ON ANY
TABLE, JOIN ANY TABLE (NO FOREIGN KEY, NO RELATIONSHIP REQUIRED, CHAINS AT ANY DEPTH),
FILTER / ORDER / GROUP ON EVERYTHING IT REACHED.** An aggregate's own table becomes such an
anchor through the reduction, and reading it that way costs the aggregate NOTHING — the
guarantees a Direct door drops are all about writes.

**So the following are bugs, not designs, and every one of them has been shipped here by an
agent who believed the framework refused the query:** an N+1 loop; a `FindAll` of whole
aggregates folded in Go; a column denormalized onto the write path so a read would not have
to join; a Mongo view invented for a one-off report; an aggregate repository bent into
serving a report shape; and the sentence *"that join is not possible"*. If a query needs a
shape the aggregate loader cannot hold, **build the Direct anchor and write the query.**
`direct-schema.md` (section *THE READ IS UNRESTRICTED*) owns it, `read-joins.md` owns the
traversal rules — which are the same on both anchors.


Every row above assumes the question is about an aggregate this service owns, because for a
long time that was the only anchor there was. It is not any more, on a pin that carries it:
the same existence probe, the same aggregate DSL and the same grouped form run over a
**Direct schema** — one table, no entity behind it — which is what makes "how many rows does
this aggregate's child table hold" and "does this control row exist" askable without either
hand-written SQL or a whole aggregate declared to host the question.

`direct-schema.md` owns that decision, including the availability test (the pin's docs are
the oracle) and the guarantees a Direct write does NOT carry. Pick the primitive here; pick
the anchor there. Neither choice changes the other.

## The other half of a question — a SUBQUERY on the right-hand side

Every row above compares a column against VALUES. A subquery makes the right-hand side the
result of another `SELECT`, which is what reaches the questions no list of literals can
answer — and what stops the shape this file exists to stop from reappearing one level down:
a first query run only to collect ids, and a second one filtered by them, computed across two
round trips and two snapshots.

**Availability — shipped in framework v0.68.0, and the test is the mechanical one everything
else here uses:** does the pin's docs carry a `criteria` section (equivalently, does `Sub`
appear in its criteria vocabulary)?
Present → everything below applies. Absent → the pin predates it and the honest answers are
the old ones: a declared read join where the value is a field of an aggregate this row points
AT, or two queries with the ids carried between them.

**What it changes, said once.** The operator set does not grow: `eq`, `ne`, `gt`, `gte`,
`lt`, `lte`, `in` and `nin` all take a subquery the moment the operand can be one. Only
`Exists` / `NotExists` are genuinely new, because they have no left-hand side at all.

- **`Sub(source)` opens the nested SELECT**, and the source is ONE table — so it takes a
  reduced (Direct) schema, exactly as a read join's target does (`read-joins.md` owns that
  rule; `direct-schema.md` owns the anchor it produces).
- **It projects exactly one column** (`Select`, or `SelectCount`/`SelectMax`/`SelectMin`/
  `SelectSum`/`SelectAvg`), carries its own `Where`, order, `Limit` and quantifier
  (`Any()`/`All()`) — and `Exists` projects nothing, because the question is whether a row
  is there.
- **`Outer(field)` correlates**, and it is a VALUE rather than a builder, so every operator
  that takes a value takes it for free. It reaches exactly ONE level — the immediately
  enclosing scope — and a name that does not resolve there is refused rather than searched
  further out.
- **The archive gate rides along, unwritten.** A subquery starts on the active scope like
  every other read: a source declaring an archive column carries its own `IS NULL` gate, one
  that declares none carries nothing, and `IncludeArchived()` / `OnlyArchived()` are the
  opt-outs under the same names as on the `Query`.
- **It works in the predicate of a WRITE too** (`UpdateOne`, `Delete`, `Archive`, …). MySQL
  is the one engine with a restriction and it is narrow — a statement may not read its own
  target table in a subquery — and the framework refuses that case at compile time, naming
  the engine.

**Refused rather than silently wrong**, which is the part to design around: no projection or
two, a `Select` on an `Exists`, a `Limit` with no order, an `Outer` that does not resolve one
level up, a source that is not a reduced schema — and `NinSub` over a NULLABLE column, where
SQL's `NOT IN` matches nothing at all the moment the set contains one NULL. That last one has
an answer, not a workaround: `NotExists`.

**It is an infrastructure API, not a wire vocabulary — say this out loud whenever the
question arrives from the read side.** Only Go code builds a subquery. An end user's filter
is still the request DTO's declared `filter:` allowlist, so the read-model filter operators
are unchanged and nothing reaches `Sub` from HTTP, GraphQL or gRPC. A listing that must be
narrowed by "has at least one active phone" is either a projected field on the view or a
handler that asks this question on the write side — never a filter operator invented for it.

## What the choice does NOT change

Same criteria vocabulary, same schema resolution, same scope gate (active rows by
default; `IncludeArchived`/`OnlyArchived` on the Query) — and the criteria may reference
sibling and shared-base fields, because the same joins apply. Picking the cheap primitive
never costs expressiveness, which is why there is no trade-off to weigh and no reason to
"start with FindAll and optimize later".

**Nor does the REACH.** Every primitive above runs through the same loader, so a field a
declared read join brought across another aggregate's foreign key is addressable in all of
them alike — filtered by the existence probe, compared by a clause, grouped by the
aggregate DSL. One declaration on the repository, not one per call site (`read-joins.md`,
pin ≥ v0.57.0).

## Four things that bite in the hand-written impl

- **Fail LOUD.** A domain port returns pure values and no `error`, so an unrecoverable
  query failure is a panic in infra — the pipeline turns it into a 500 and the write never
  happens. Never fold it into a plausible "not found" / "zero": that silently skips the
  rule the fact exists to enforce.
- **`Found` vs the value.** The scalar carriers report whether any row matched, because a
  rule has to tell "the average is 0" from "there was nothing to average" (SQL returns
  NULL over an empty set). Check it before trusting the value. The count carrier is the
  exception — zero IS the answer there.
- **Money is `SumInt`.** Money is stored as int64 minor units per the framework's money
  doctrine; the float sum is for fractional quantities (areas, rates, measurements).
- **A spec instance is stateful** — it carries its own result. Create fresh specs per call
  site, never share one across calls or goroutines, and in the grouped form resolve the
  per-group carrier before reading it.

## Confirm at the pin, always

Names and shapes above are the vocabulary, not a version contract: which specs exist,
whether several facts compute in one query, whether the criteria carries subqueries at all,
and the exact grouped API are the PINNED version's to state. Read `custom-command-handler.html`
(and `criteria.html`, where the pin has it) before writing the impl — an older pin without
part of this surface falls back to the load-and-fold shape, and THAT is the only situation in
which it is the right answer.
