# shared/query-primitives.md — which QUESTION you are asking the database, one owner

The single home for **choosing the read primitive** when writing code by hand: a domain
`Service` implementation, a custom repository finder, a custom command/query handler.
Every skill that writes or reviews one routes HERE instead of restating the choice
inline — one owner, no drift. No code here, by design: the vocabulary and the decision,
never a snippet; exact signatures are the PIN's (`custom-command-handler.html`, "Loading
by criteria" / the AggregateLoader section, plus `table-schema.html` for the scalar half).

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

**`FindAll(…)[0]` is a correctness bug, not only a slow one.** `FindOne` is the
framework's BIRTH point for a write-side entity: it stamps the old-state snapshot, so
`domain.Old[T]` answers the PERSISTED state for every state-changing verb. `FindAll`
deliberately does not snapshot — it is the list path, where nothing mutates and the
per-row clone would be pure cost. An entity loaded through it and then written has no
old state, and every rule and every audit line that reads `Old` is quietly wrong.

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
whether several facts compute in one query, and the exact grouped API are the PINNED
version's to state. Read `custom-command-handler.html` before writing the impl — an older
pin without part of this surface falls back to the load-and-fold shape, and THAT is the
only situation in which it is the right answer.
