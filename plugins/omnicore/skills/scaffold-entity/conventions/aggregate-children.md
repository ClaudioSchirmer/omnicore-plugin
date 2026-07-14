# conventions/aggregate-children.md — 1:N aggregate children (DELTA)

> NO code here, by design. Layout/naming: `service-layout.html`. The aggregate-child API
> (the exact primitives, cascade, audit bucketing): the routed `/docs` — reading them
> before generating is MANDATORY. This file carries only skill-level process, decisions
> and traps.

Load only when the model has a **child collection**. Spans every layer. First decide the
**child-edit strategy** — it changes what you generate.

Docs: `table-schema.html` (Child, aggregate depth) · `aggregate-persistence.html`
(primitives, cascade, child-id write-back) · `old-state.html` (audit op bucketing).

## The child-edit decision (per child — PROPOSE with a recommendation, never guess)

- **B — targeted per-child edit (RECOMMENDED DEFAULT).** Whole-aggregate insert +
  root-only update + a per-child trio of explicit ops (there is NO child "upsert" in the
  framework):
  - **ADD** (`POST …/:id/<children>`) — server mints the id; body carries none.
  - **UPDATE** (`PUT …/:id/<children>/:childId`) — updates an EXISTING child, never
    creates: YOU implement the by-id guard in a domain method (find among current
    children; found → apply via the framework's change primitive; absent → the canonical
    not-found notification, 404 — never the framework's does-not-exist one, which is 422).
  - **ARCHIVE** (`PATCH …/:id/<children>/:childId/archive`) — soft removal; same guard.
  - **Wiring — all THREE ops are partial updates of the ROOT**: each rides the
    partial-update auto handler + `ApplyPartiallyTo` (load root → your domain method
    mutates the one child → the framework persists the diff). Child ops are NOT in
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
- **Every child-mutation method opens with the framework's ensure-initialized call** —
  else construction-time notifications are lost.
- **The change/remove primitives match the child BY VALUE (deep equality), not by id** —
  operate on the LOADED item, never a hand-built value carrying only the id. The by-id
  semantics is YOUR domain method's lookup, not the framework's.
- **A child is a value type (struct, not pointer)** with a string id field + its own
  scoped `BuildRules` (notifications surface as `children[i].field`).

## The verb tells the truth (soft removal)

Removing a child with a soft-delete column ARCHIVES it (the row lingers, hidden) — so the
route is **`PATCH …/archive`, never `DELETE`** (a lying contract). A role child is ALWAYS
soft-removable (no soft-delete column errors at the remove); only a base-child explicitly
opting out of soft-delete is hard-deleted (then `DELETE` is honest).

> **⚠️ NO per-child unarchive** — the edit-path load hides archived children and the
> update never clears the soft-delete, so a soft-removed child cannot be revived alone;
> only the ROOT's unarchive revives children, in cascade. **A child needing its own
> reversible archive⇄unarchive is NOT a value object — promote it (model C).** Surface
> this in the child-edit elicitation: "should a removed <child> be restorable on its
> own?" → yes ⇒ C.

## Application / Web

Child inputs are `dtos.<Child>Input` files (ctx-free, no wire tags, a `To<Child>()`
builder; model B: an optional id — empty on add, set on update). The insert command
carries the slice; per-child ADD/UPDATE carry one input; archive carries just the id.
The child id arrives via an extra path segment (`path:"childId"` — never `path:"id"`).
Write responses mirror the post-write aggregate WITH the minted child ids
(application.md).

## Base-children (SharedBase) — the hazard + the routing sub-question

A child declared on the BASE is shared by every role — **a role's replace-all would
delete rows other roles depend on**: model B is a correctness requirement there, not a
preference. When per-child endpoints are wanted for a base-child, offer a light follow-up
(two legitimate options, no manufactured debt): **A)** mount them under the role (simple,
valid permanently; a future role would expose its own edit routes over the same shared
rows — a consequence, not a problem); **B)** promote the child to its own aggregate with
one dedicated edit surface. Recommend by the dev's intent; reads of the shared identity
stay under the person view either way. Role-private children skip this entirely.

## Cascade & audit (for generated comments)

Archive/unarchive/delete cascade to all loaded children in the same TX. The auditor
buckets each child op by DIFF (a DB-loaded child re-added is `updated`, not `inserted`);
replace-all emits add+remove pairs; only the targeted change path emits `changed`.
Children auto-project into the view under the type-derived segment — never `Embed` them.
