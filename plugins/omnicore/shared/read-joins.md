# shared/read-joins.md — reaching ANOTHER aggregate, one owner

The single home for **read joins**: what they are, where they are declared, what they
reach, and — the part that changes how you model — what they make unnecessary. Every skill
that writes a rule, a service fact, a repository, or a relational read model routes HERE
instead of restating any of it. No code here, by design: knowledge and decisions only; the
exact API is the PIN's (`read-joins`, plus the parity table in `relational-view`).

**Availability: the pin must be ≥ v0.57.0.** Below that the capability does not exist and
every "no, copy the column / project it into Mongo" answer this file overturns is still the
correct one. Check the project's `go.mod` before offering it — and note that the
no-domain-type rule below was settled INSIDE that line, so confirm it against the pin's
`read-joins` rather than assuming an early v0.57 build enforces it.

## What it is

A read join is a **read-only traversal from one aggregate to another, across a foreign key
that aggregate already stores.** Declared on the **repository** — never on the
`TableSchema`, never on a view — it maps a few columns of the target onto ordinary Go
fields of the entity, under names you choose.

Three consequences, and each one is why the placement is what it is:

- **It is not storage.** The `TableSchema` is untouched, so a joined column can never enter
  an `INSERT` or `UPDATE` — that is structural, not a convention — and a Mongo projection
  of the same entity is completely unaffected.
- **It is not the read model either.** It hangs off the **loader**, so ONE declaration
  reaches every read at once: `FindOne`, `FindAll`, `Exists`, the aggregate DSL, the
  request-scoped reader, **`FindByID` — which the write-side auto handlers load through** —
  and any relational read model declared over that loader, which inherits the reach and
  declares nothing itself.
- **It is 1:1 and horizontal.** One column onto one field at a time. There is no
  many-valued form: a 1:N traversal would multiply the root's rows and break the paged
  read. "Bring the order's items" is a child collection, never a join.

## The answer this file exists to change

**A business rule that needs a value belonging to ANOTHER aggregate no longer has to copy
that column into this table.**

That was the old shape and it was expensive in exactly the way duplicated data always is:
a denormalized column, a write path that has to keep it in step, and a rule that is quietly
wrong the moment the source changes. The declaration replaces all of it — the value is an
ordinary field of the entity, **populated on every load**, and the rules, the `BuildRules`
clauses and the domain service read it exactly like a field of the row itself.

So when a dev says *"the rule needs the customer's tier / the campus's budget code / the
supplier's country, and it is not on this entity"*:

1. Is there already a foreign key to that aggregate on this table (or on one of its
   collections)? → **declare the traversal.** Nothing is copied and nothing is synchronized.
2. No foreign key, and the relationship is genuinely 1:N or many-to-many? → that is a child
   collection or a separate query, not a join.
3. Needs to match on something other than the target's id (a code, a natural key)? → **not
   expressible.** The predicate is always `fk = target.id`. Model it as a real foreign key,
   or read it with the raw querier.

## Which columns of the target you can reach

**Every column of the target's OWN table — including the ones the framework stamps.** A
schema registers those columns in SLOTS (`CreatedAt(col)`, `UpdatedAt(col)`,
`DeletedAt(col)`, `Revision(col)`; `storage.managed.*` in the generator's spec), and the
column each slot holds is whatever that aggregate's author named it. What is fixed is the
LOGICAL name the read path resolves it back to — `CreatedAt`, `UpdatedAt`, `DeletedAt` —
and that resolution is exactly what the join's column check consults. So *"when was the
campus archived?"* is a traversal like any other, and it costs no denormalized copy, even
though no field declaration anywhere names those columns.

**Name the column as the TARGET spells it.** `deleted_at` is a convention, not the
contract: the mapping is slot → column, per aggregate. Read the target's own declaration —
its schema's `DeletedAt(...)` call, or its `storage.managed.archivedAt` — and write that.

Two edges of that reach are worth knowing BEFORE the declaration is written:

- **The archive column is NULL on every ACTIVE row**, which is the normal state — so it
  crosses into a POINTER even under an `inner` join. It is also the one nullability the
  framework cannot check for you: the fields of `domain.Managed` are unexported, so the
  reflective check has nothing to point at and answers "not nullable" rather than guessing.
  A non-pointer field there passes repository construction and fails on the first row
  scanned. Against a spec target the generator derives it from that spec's own
  `storage.managed`; against a hand-written one, the author says it (`nullable: true`, or a
  pointer field).
- **The revision does NOT cross.** It is the optimistic-concurrency guard of the TARGET's
  own writes — the value its `UPDATE` is matched on — so a copy carried across a join is
  stale the moment that aggregate is written again, and there is nothing this side could
  correctly do with it. The read path does not resolve it, and the traversal is refused.

What stays out of reach is the other direction: one predicate onto ONE table, so the
target's shared base and its facets are not reachable across the join — their columns live
in tables it never enters. Reach them from the spec that owns them.

## A joined field carries NO domain type

Not an identity, not a value object of any kind — scalar, enum or composite. The
framework refuses one at construction, and the reason is not stylistic: **the value
belongs to another aggregate and arrives read-only.** It is never written through this
entity and never validated by this domain, so a domain type here would be an instance no
rule ever approved — a `domain.ID` nobody checked, an enum nobody constrained, a value
object whose invariants this side does not own.

So an **identity column crosses as its canonical TEXT**, and that is also the only shape
correct on every engine: three of the four store an id as raw bytes, and the framework
decodes it on the way out. A predicate on that field still binds in the target's native id
form — the framework takes that typing from the TARGET's schema, precisely because nothing
about the field on this side says "identity".

The practical consequences, in order of how often they bite:

- **A joined identity is a string here.** Comparing it to this entity's own `domain.ID`
  needs the id's text form, not the other way round.
- **A composite value object has no form at all** — it spans several columns and a join
  maps exactly one. That is not a refusal to reach it, though: the target's part columns
  are ordinary columns of its table, so **traverse onto the parts you actually need, one
  scalar field each**. What does not cross is the composite AS A CONCEPT — it stays whole
  only in the aggregate that declares it, and the invariant tying its parts together is
  that aggregate's to keep.
- **A scalar or enum value object is refused too**; bring the underlying scalar across and
  let the owning aggregate keep the invariant.

## What can be ABSENT decides the pointer

Two independent sources, and either one alone is enough:

1. **a left join with no counterpart**, and
2. **a column the TARGET declares nullable** — which makes NULL reachable even under an
   `inner` join, because the row exists and the column in it is empty.

A field that cannot hold NULL fails on the first row that has one. So a nullable column
crossing an inner join is still nullable on this side; the kind of the join is only half
the question.

**Where the second half comes from decides who has to say it.** When the target is a spec
of the same project, the generator reads its nullability off that declaration and stating
it again is refused — one source, and it cannot go stale. When the target is HAND-WRITTEN
there is nothing to read, and the author is the only one who knows: say so per field
(`nullable: true` in the generator's spec; a pointer in a hand-written entity). Skipping it
is not a warning, it is a boot — the framework checks the field against the target's schema
at repository construction and refuses a non-pointer that can receive NULL.

## On the entity vs on the wire — two questions, asked separately

**Needing a value and publishing it are different decisions, and the language keeps them
apart.** A traversal declared FOR A RULE belongs on the entity and nowhere near the API:
the field is filled on every load and read by the rules, and it is in no response body, no
listing row and no export. Say so explicitly (it is `hidden` in the generator's spec, and
in a hand-written entity it is simply a field no Response DTO declares).

Never let "the rule needs it" become "the caller receives it". That is how an internal
attribute of another aggregate leaks into a public contract, and once it is on the wire it
is a promise.

## The two kinds — and what a missing counterpart means

| | A joining row with NO counterpart | Legal when |
|---|---|---|
| **left** | is still returned; the joined fields read as an ABSENCE | always — and its fields must be nullable |
| **inner** | is NOT returned | **only over a NON-NULLABLE foreign key** |

**`inner` over a nullable key is refused, and the refusal is the point.** The declaration
lives on the repository, so it applies to `FindByID` too — the load the write-side handlers
go through. Over a nullable key it would silently drop aggregates from every read, turning
a legitimate write into a 404. Default to `left` unless the foreign key is genuinely
mandatory.

**A `left` join with no counterpart is an ABSENCE, not the zero value**, and it stays one
all the way out: nil in the document, nil in the DTO's pointer field, `null` on the wire (or
absent entirely under `,omitempty`). That distinction is the whole reason the framework
insists on a pointer there — a non-nullable field would report a blank name where the truth
is "there is no counterpart", and those are not the same answer.

## From a collection — and why it is load-only

A traversal may also hang off one of the root's **own** collections: the foreign key is the
collection's, and the fields land on the entry.

- **Load-only.** Filled on every loaded entry and served inside it (`?fields=` names it
  `<segment>.<field>`), but **never filterable or sortable** — narrowing the root by a field
  of a 1:N collection is a pushdown one root SELECT cannot express, which is the same
  boundary every child field already has.
- **`inner` inside a collection drops the ENTRY, not the root.** The aggregate still comes
  back, with that element missing — a silent hole in the array rather than a missing
  aggregate. Prefer `left` whenever the relationship is optional.
- **Own collections only.** A collection owned by a SHARED BASE belongs to the base;
  declare the traversal on the base's own repository.

## What it costs, and what it does not

- **No extra round trip.** A root join is ALWAYS in the FROM and its columns ride the root
  SELECT. Being always present is what makes the field trustworthy: one that appeared only
  when a filter happened to mention it would be populated on one call and blank on the next.
- **The cost is a real join on every read through that loader**, `FindByID` included.
  Declare one because the aggregate genuinely reads that way — not "just in case".
- **It is not gated on the target's archived state.** A join answers "what is on the other
  side of this foreign key", and a soft-deleted counterpart is still what the key points at.
  The read scope governs which ROOTS come back, never the rows reached across into. Where an
  archived counterpart must disappear from the answer, that is a filter the criteria states —
  not a join that silently drops rows.
- **One foreign key reaches one table.** Two traversals to the same table need two foreign
  keys (`bill_to_id`, `ship_to_id`) — which is also what tells their SQL aliases apart.

## What it does NOT change on the read side

- **Mongo-projected views are unaffected — in both directions.** A projection is composed
  from the `TableSchema`, which a join never enters, so the joined fields reach the entity
  and the rules but **not the view**. Where the READS need another aggregate's data on that
  backing, that is still `Embed`/`Link` or a `ComposedView`.
- **A relational read model inherits the reach and declares nothing.** A root join's field
  is filterable, sortable and served there like a schema field; a child join's is served
  inside the entry and addressable nowhere.

## The line to hold when someone asks for more

A read join is not a query language. It reaches ONE table, across ONE declared foreign key,
by that table's id, bringing back scalars. Aggregating over the other side, matching on a
non-id column, reaching two hops, or pulling a collection are all outside it — and the
honest answers are, respectively: the aggregate DSL on the loader (`query-primitives.md`), a
real foreign key, a second join declared on the intermediate aggregate, and a child
collection or a Mongo composition. Say which one applies; never approximate it with a join.
