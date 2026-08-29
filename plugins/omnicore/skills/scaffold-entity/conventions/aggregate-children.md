# conventions/aggregate-children.md — 1:N aggregate children (DELTA)

> NO code here, by design. Layout/naming: `service-layout.html`. The aggregate-child API
> (the exact primitives, cascade, audit bucketing): the routed `/docs` — reading them
> before generating is MANDATORY. This file carries only skill-level process, decisions
> and traps.

Load only when the model has a **child collection**. Spans every layer. First decide the
**child-edit strategy** — it changes what you generate.

Docs: `table-schema.html` (Child, aggregate depth) · `aggregate-persistence.html`
(primitives, cascade, child-id write-back) · `old-state.html` (audit op bucketing) ·
`status-mapping.html` (which notification maps to which status — the child not-found
404 vs the framework's does-not-exist 422 below).

## The child-edit decision (per child — PROPOSE with a recommendation, never guess)

- **B — targeted per-child edit (RECOMMENDED DEFAULT).** Whole-aggregate insert +
  root-only update + a per-child trio of explicit ops (there is NO child "upsert" in the
  framework):
  - **ADD** (`POST …/:id/<children>`) — server mints the id; body carries none.
  - **UPDATE** (`PUT …/:id/<children>/:childId`) — updates an EXISTING child, never
    creates: YOU implement the by-id guard in a domain method (find among current
    children; found → apply via the framework's change primitive; absent → the canonical
    not-found notification, 404 (`RecordNotFoundNotification`) — never the framework's
    does-not-exist one (`EntityDoesNotExistNotification`), which is 422; the mapping
    table is `status-mapping.html`).
  - **ARCHIVE** (`PATCH …/:id/<children>/:childId/archive`) — soft removal; same guard.
  - **Wiring — all THREE ops are commands ON THE ROOT** (load root → your domain method
    mutates the one child → the framework persists the diff). The handler follows the
    operation's field contract like any root op (`auto-handlers.html`): a full-replace
    child `PUT` is the strict full-body handler + `ApplyTo`; an op with optional fields
    rides the partial-update handler + `ApplyPartiallyTo`.
    **The lookup + not-found lives in the DOMAIN method (`<Verb><Child>ByID`), never as a
    loop inside `ApplyPartiallyTo`** — the command is a one-liner that calls it. Two ops
    scanning the collection themselves is the same invariant written twice, in the layer
    that does not own it. Since **v0.63.0** the change-time duplicate guard is NOT yours to
    write: `ChangeAggregateChild` refuses a replacement that would take a business identity
    another ACTIVE child already holds, answering `EntityAlreadyAddedNotification`
    (409) and leaving the collection untouched — the same answer the ADD path gives. Before
    it, CHANGE only swapped, so a payload could edit one child into another's identity
    unchallenged and every later match (`remove`, the next `change`, the outbox payload)
    resolved to the wrong row. What the framework still does NOT freeze is the identity
    itself: a replacement may reshape those fields freely as long as the value is free, so
    "this reference must never move" is a rule of yours — a child `immutable` rule, or the
    generator's `children[].change: {shape: patch, patchExcludes: [...]}`, which keeps the
    field off the wire altogether.
    Child ops are NOT in
    `auto-handlers.html`'s six-verb table — that table is root-only. In the docs this
    is `aggregate-persistence.html`'s "Update(root) with items StatusRemoved →
    Archive of those specific items".
- **A — replace-all (only when the dev asks).** The root update replaces the whole
  collection — omitting a child DELETES it. Destructive; never default to it silently.
- **C — promote to its OWN aggregate** when the child has an independent lifecycle, is
  edited entirely on its own, or must be restorable alone. It becomes a flat entity with
  an FK — generate via the flat flow and stop reading this file for it.

Handler choice per child: by its FIELDS (any optional ⇒ lenient, ADD and UPDATE alike;
all-required ⇒ strict — mind the missing-number-defaults-to-0 range-rule footgun). See
web.md.

## Traps

- **⚠️ "Archive" the word ≠ the archive handler.** The child ARCHIVE op shares the
  root's verb, route shape and id-only command — but NEVER its handler. The root's
  archive auto handler takes an id and archives the WHOLE aggregate (cascading to
  every child); wired to a child op it type-checks, boots and returns 200 while
  silently archiving the entire root. Child archive = partial update of the root
  with the item removed (see the wiring note above). If a routes file instantiates
  the root-archive handler more than once per aggregate, one of them is this trap.
- **⚠️ The root does NOT declare a `[]Child` struct field** — a dead footgun: the loader
  hydrates children into the framework's internal aggregate map, never into a slice (on
  read the field stays empty forever; on write it's never consumed). Children live ONLY in
  the map: declared by `AggregateChildren()` (the TYPE set), mutated and read through the
  framework primitives (`aggregate-persistence.html`).
- **Boundary agreement boot check:** the `AggregateChildren()` type set and the schema's
  `.Child(...)` set must match, or schema binding panics. **Depth = 1** — a child
  declaring its own child panics; model a separate aggregate instead.
- **A child-mutation method that EMITS a notification before delegating opens with the
  framework's ensure-initialized call** — else that notification is silently dropped. **A
  method that only delegates does NOT need it**: the add/change/remove primitives already
  ensure-init the root themselves; emitting it there is noise, not safety.
- **A child-mutation method must EARN its existence.** It exists to carry what the primitive
  cannot: an aggregate-spanning invariant (duplicate detection is the canonical one — reuse
  `IsSameBusinessIdentity`), strategy B's by-id guard, or a genuine domain verb. **A method
  whose body is nothing but the primitive call adds no safety and no vocabulary the command
  lacks** — treat it as a smell and ask which of the three it was supposed to carry. On
  strategy B, a change/archive method taking the AVO instead of a childId is the by-id guard
  MISSING, not a style choice: the per-child routes then have no not-found path.
- **One validation path per item, never both**: a child validated inline in the root
  method AND again through the boundary `ValidateAggregateChild` pass reports every
  notification twice. Pick the boundary pass (the default) OR inline — not both.
- **The framework matches a child through its MANDATORY `IsSameBusinessIdentity`, never
  `reflect.DeepEqual`** — every AVO must declare it (an interface method: omit it → compile
  fail) and `GetID` comes from the embedded `domain.Managed`. It answers "is this the SAME
  child?" from the aggregate's BUSINESS view, not "did nothing change?": implement it on the
  natural-key subset (so a cosmetic edit is still the same child), and delegate to
  `domain.IsSameByBusinessFields` ONLY when identity genuinely = every field. The choice
  changes PUT re-send behavior (id-preserving re-activation vs archive-then-reinsert as
  history) — `aggregate-persistence.html`. Operate the change/remove primitives on the LOADED
  item, and REUSE this method as the root's duplicate rule instead of a second check.
- **A child is a value type (struct, not pointer)** — an AGGREGATE value object, in
  `internal/domain/aggregatevos/` (its own file + that package's `notifications.go`). It
  **embeds `domain.Managed` and declares NO id field of its own** — `GetID`/`SetID` come
  from the carrier, ids are set via `domain.WithID(AVO{…}, id)` / `.SetID(id)`. A
  hand-declared exported `ID` field is the silent-wrong trap: it compiles, is never
  persisted, and the real id never round-trips (see `table-schema.html`, "Reading the
  managed columns back" — the changelog's v0.40.0 entry and the example's
  `aggregatevos/` are the truth if any doc sentence still shows an exported `ID`).
  It carries its own scoped `BuildRules` (notifications surface as
  `children[i].field`); its VO-typed fields auto-validate exactly like a root's, so
  `BuildRules` carries ONLY the non-VO rules (`value-objects.html`).

## The verb tells the truth (soft removal)

**The child's own column is the whole rule, exactly like the root's is.** A child that
declares an archive (deleted-at) column is ARCHIVED on removal (the row lingers, hidden) —
so the route is **`PATCH …/archive`, never `DELETE`** (a lying contract). A child that
declares none has its row deleted, and there `DELETE` is the honest spelling. Where the
child lives does not change the answer: a root's own child and a shared base's native child
behave identically, and a hard-removed child takes its sibling rows with it in the same
transaction.

> Since **v0.61.1**. Before it, only a base-child could be hard-deleted — a root's own child
> without the column reached the archive path with no column to stamp and failed with an
> infrastructure error on the first removal, at runtime, with no boot guard to catch it.
> On an older pin, a removable root child MUST declare the column.

> **⚠️ NO per-child unarchive** — the edit-path load hides archived children and the
> update never clears the archive stamp, so a soft-removed child cannot be revived alone;
> only the ROOT's unarchive revives children, in cascade — **and it revives only the ones
> the root's OWN archive put to sleep** (below). **A child needing its own reversible
> archive⇄unarchive is NOT a value object — promote it (model C).** Surface this in the
> child-edit elicitation: "should a removed <child> be restorable on its own?" → yes ⇒ C.

## Application / Web

Child inputs are `dtos.<Child>Input` files (ctx-free, no wire tags, a `To<Child>()`
builder; the input carries FIELD values only — the child id never rides inside it, it
travels beside it on the command, bound from the path). The insert command
carries the slice; per-child ADD/UPDATE carry one input; archive carries just the id.
The child id arrives via an extra path segment (`path:"childId"` — never `path:"id"`).
Write responses mirror the post-write aggregate WITH the minted child ids
(application.md) — for **ADD and UPDATE**, where the minted id is exactly what the caller
needs back. **ARCHIVE/DELETE of one child answers `204` with no body**, like the root's own
archive and delete: the entry is gone, and the only other thing in scope is the owner id
the caller already put in the path. A `200` echoing it invites callers to look for content
that is never there, and leaves one service answering `204` at `DELETE …/:id` and `200` at
`DELETE …/:id/<children>/:childId` — same verb, same semantics, two contracts. Result and
Response types for that verb are `results.None` / `responses.NoBody`, so don't declare a
pair at all.

## Base-children (SharedBase) — the hazard + the routing sub-question

A child declared on the BASE is shared by every role — **a role's replace-all replaces
the IDENTITY-WIDE set, visible to every other role**: legal, but say it to the dev —
model B keeps edits per-child and is the safer default here. When per-child endpoints are wanted for a base-child, offer a light follow-up
(two legitimate options, no manufactured debt): **A)** mount them under the role (simple,
valid permanently; a future role would expose its own edit routes over the same shared
rows — a consequence, not a problem); **B)** promote the child to its own aggregate with
one dedicated edit surface. Recommend by the dev's intent; reads of the shared identity
stay under the identity view either way. Role-private children skip this entirely.

## Cascade & audit (for generated comments)

Archive/unarchive/delete cascade to all loaded children in the same TX. The auditor
buckets each child op by DIFF (a DB-loaded child re-added is `updated`, not `inserted`);
replace-all emits add+remove pairs; only the targeted change path emits `changed`.
Children auto-project into the view under the type-derived segment — never `Embed` them.

**An unarchive brings back the children the root's own archive stamped — not every
archived child.** The archive writes ONE instant on the root row and on every child row
its cascade reaches (a child already archived keeps its own older stamp), so the unarchive
restores exactly the rows carrying the root's stamp: a child removed on its own months
earlier — through a PUT replace-all or a per-child archive — stays down. Same rule on a
shared base, with TWO instants: the role's children come back from the ROLE's stamp and
the base's native children from the BASE's, which are not the same write whenever a
sibling role kept the identity up. This is what makes the column type a correctness
question, not a style one — `deleted_at` must be sub-second and identical between root and
child (migrations.md, Traps).

> Since **v0.62.0**. Before it, the cascade was gated on "is this child archived?", so a
> root's unarchive resurrected every archived child under it. On an older pin, warn the dev
> that an aggregate mixing whole-root archives with per-child removals will over-restore.
