# shared/read-side.md — read-side posture, one owner

The single home for **how entity read models are served** and what each posture can and
cannot do. Every skill that decides, offers, asks about, or explains a view's backing
routes HERE instead of restating posture facts inline — one owner, no drift. No code here,
by design: knowledge and elicitation policy only. Reaching ANOTHER aggregate is its own
owner beside this one — `read-joins.md`.

## The two postures

- **Mongo-projected** (canonical) — views projected through the CDC pipeline: O(1)
  document reads, the FULL read-side vocabulary (embeds, links, composed/shared views,
  free-text search, child/sibling filter+sort), eventually consistent (CDC lag).
- **Relational-served** — the view reads straight from the SoR: read-your-writes, NO CDC
  wait, read-time aggregate composition instead of an O(1) fetch. Capability limits below.

**A relational read model is its own TYPE, not a flag on the projected one** (pin ≥
v0.57.0). It is declared by name over the aggregate's existing loader and contributed
through its own feature seam — the sibling of the one Mongo views use. Everything a
projection carries is absent by construction, not by convention:

- **no `Version`, no rebuild, no drift, no registry row, no collection, no Mongo spec.**
  Changing what it serves — a new column, a new read join — needs no bump and no
  operational step; the next read simply reads the new shape.
- **the loader is its ONLY structural input**, and the loader carries the schema — so the
  view's shape and its loader cannot disagree, and there is no boot guard to satisfy
  because the mismatch is not expressible. (The old `BoundTable()` guard is gone with the
  thing it guarded.)
- **indexes, `DeleteOnArchive`, TTL, the Embed family and roles are not methods on it** —
  combining them with a relational view does not compile.

**SQLite forces the relational posture.** SQLite has NO CDC source (Debezium cannot tail
a SQLite file), so there is NO Mongo projection and NO integration-event PUBLISHING —
both ride the CDC relay. (SUBSCRIBING to another service's events does NOT ride the
relay — it needs only a broker + the transport build tag; `capabilities.md` owns that
split.) The "filter it on the Mongo view instead" fallback that applies on the full
engines does NOT apply here.

## INVARIANT — the posture NEVER constrains write-side modeling

SharedBase, aggregate children, siblings, modes, VOs, aggregates model IDENTICALLY on
every engine, infra-free / SQLite included — there is no "simpler domain because it's an
MVP". Model what the domain IS; never soften or skip a write-side pattern because Mongo
is absent. The posture restricts exactly one thing: which view KINDS can be served.
Never let a read-side limit leak backwards into the domain model.

## Kinds vs plain views

- A multi-source view KIND (SharedBaseView, ComposedView, the Embed/Link family, Upstream)
  is **relational-INELIGIBLE by construction** — a different type, or a 400. The exact kind
  set and failure mode per kind are the PIN's (parity table in `relational-view`).
- **"The relational side cannot reach another aggregate" is NO LONGER TRUE** (pin ≥
  v0.57.0). A **read join** declared on the repository reaches across a foreign key into
  another aggregate, and a relational view inherits it from the loader: a root join's field
  is filterable, sortable and served there like a schema field. That closed the single
  widest gap between the two backings — but it did not close the gap. A join is 1:1 and
  horizontal, one column at a time; Mongo still does strictly more (materialized 1:N
  embeds, links, read-time composition across views and services, free-text search,
  filter/sort on child fields). `read-joins.md` owns the boundary; do not offer a join
  where the honest answer is an Embed, and do not push a Mongo conversion where a join
  already answers it.
- **"Aggregated view" is NOT a view kind.** A filtered TOTAL over an existing listing is
  the `?onlyTotal` DTO opt-in on the list request (`auto-query-handlers` at the pin,
  both backings). Richer counts/sums/report scalars are the
  `Aggregate`/`AggregateBy` DSL on the write-side RELATIONAL aggregate loader
  (`custom-command-handler` at the pin — the AggregateLoader section) — computed on the SoR, never a Mongo view, and
  therefore available on EVERY posture, SQLite included. Refusing "give me totals"
  on an infra-free project (or pushing a Mongo conversion for it) is a wrong refusal.
  Which primitive answers which question, once the request lands in hand-written code,
  is `query-primitives.md` — the owner beside this one.
- A **plain single-aggregate view is relational-ELIGIBLE — whatever its aggregate's
  write-side shape**. In particular, a plain view rooted at a shared-base ROLE stays
  eligible: its served document carries the full aggregate — the role's fields, the
  shared base's fields FLATTENED, root- and child-level siblings, the role's children
  AND the base's native children — so with one role it matches the flat shape a Mongo
  view of the same role would carry, and a single-role service loses nothing. It is the
  `SharedBaseView` KIND (one document per identity, a sub-document per role — worth it
  at 2+ roles) that needs Mongo, never the role's own view.

## The capability rule (relational-served views)

A relational view serves filter/sort on any field the aggregate reaches with a **1:1
load** — a root column, a sibling, a shared-base field (the last two joined in), **or a
field a declared ROOT read join brought across** — and rejects what would need a **1:N
pushdown**: `?search=`, and filter/sort on a 1:N child field (a dotted child path, a
child-level sibling, or a CHILD join's field), plus an unknown field.

Rejections are a typed 400 (`UnsupportedCapabilityNotification`, `SemanticSchema` → 400,
the offending capability or Go field path named), raised at the engine's entry point before
any IO — never a 500. **The notification names no backing**: every read engine raises the
same one, so the four surfaces render one refusal whatever serves the view. (It replaced
the old relational-specific name; a skill still saying `RelationalCapabilityNotification`
is stale.)

Two refusals that are NOT capability refusals, and are worth telling apart when explaining
a 400:

- **a `?fields=` path the read model does not have** → `SchemaViolationNotification`, the
  same answer the Mongo reader gives the same token. A projection has no capability
  boundary to hit — "that is not a field of mine" is a different statement from "I cannot
  do that". What a selection MAY name: root fields, the managed slots, root-level sibling
  and shared-base fields, a leaf inside a child collection, and read-join fields at the
  level the join lands on (a root join's under its bare name, a child join's under
  `<segment>.<field>`).
- **an undecodable or context-mismatched cursor** → also `SchemaViolationNotification`,
  identically on both backings.

**Anti-drift boundary: the authoritative, version-exact capability/parity table is
`relational-view` at the PIN.** Older pins genuinely differ (read joins are v0.57; satellite
filter/projection is recent). Read it only when you must answer a precise capability
question, explain a 400, or walk a backing flip — the pin is the truth, never this file.

## Mechanics that never drift

- **Loader reuse** — a relational view takes the aggregate's EXISTING loader, never a
  second one. It is not merely wasteful now: the loader is what carries the schema AND the
  declared read joins, so a second one is a view that quietly serves a different reach.
- **`DeleteOnArchive()` is a MONGO-PROJECTION knob** (it drops the projected document).
  A relational-served view composes from the SoR at read time — there is no document to
  drop, so its archive regime is kept-but-hidden ONLY (`?includeArchived` reveals). It is
  not a method on the relational type at all.
- **Pagination differs underneath, not on the wire.** Mongo pages by keyset
  (insertion-stable); relational pages by an offset carried inside a wire-identical cursor
  (stable over a static set, NOT insertion-stable — a concurrent write ahead of the window
  can make a row skip or repeat). Same `?first=/?after=/?before=`, same envelope rule, same
  400 on a bad cursor. A walk that must be stable under heavy concurrent writes wants the
  projection; for the dashboard / freshest-read / MVP cases this backing targets, it is the
  right trade.
- **`?fields=` shapes the answer here, it does not narrow the query.** The relational
  engine composes the aggregate as declared and prunes afterwards, so a narrow selection
  buys no I/O on this backing (the wire result is identical either way). The same holds for
  a read-time field restriction: the column is read and dropped before serving. A column
  whose VALUE must not leave the database belongs behind a view that does not project it —
  on either backing.
- **No lock-in, ever — but the flip is now a CONVERSION, not a flag.** The two are
  different types, so moving a view between backings means re-declaring it on the other
  seam. Relational → Mongo: declare the projected view with `Version(1)` and contribute it
  through the Mongo seam; one rebuild provisions it, nothing is re-scaffolded. Mongo →
  relational: **drop the collection and delete its registry row BY HAND as part of the
  conversion.** Nothing does it for you any more — a relational view never reaches the sync
  engine — and the leftover collection is then reported as foreign by the DB-per-service
  guard: a warning under `dev`, **a boot abort in every other profile**. Do it before the
  service reaches an environment that aborts.
  On SQLite, unlocking the Mongo kinds means **Mongo + a CDC relay over a tailable
  engine — an engine swap ALONE does not provide it**; `/omnicore:configure` does the
  whole conversion in one pass (engine swap, devops, Mongo, broker, relay; re-asks the
  infra questions; ports the migrations to the new dialect's SQL) — fully reversible,
  no application code lost.

## Naming — one namespace, one reserved suffix

Every read model of a service — plain, shared-base, composed and relational — shares ONE
name namespace; a collision aborts the boot. And **a name may not end in `__0` or `__1`**:
those are the blue-green slot suffixes a projected view's two physical collections are
addressed by, so `users__0` would own a collection byte-identical to `users`'s first slot,
and every consequence of that is silent (the DB-per-service guard reads the overlap as
legitimate, a rebuild of `users` drops what is there, the orphan diagnostic names the wrong
row). Refused at boot in all four families. There is nothing to migrate if you hit it —
rename; a name that would trip this guard could never have been safely deployed.

## Elicitation contract — what to ASK vs what to decide

- **Backing (per plain entity view).** A posture already on record (`scaffold-service` /
  `scaffold-system` spec, or inferred from existing views) is the DEFAULT — do NOT
  re-ask. Nothing on record → ASK once, NEUTRALLY, no silent default (an empty project
  is equally likely an MVP or a seasoned team's service). On SQLite there is nothing to
  ask — the posture is forced.
- **Archive regime (whenever Archive is in the mode set).** ALWAYS settle it — never a
  silent default — but settle the BACKING first and offer only what it serves:
  relational → kept-but-hidden vs `?includeArchived` only, the drop half is never
  offered; Mongo → kept-but-hidden (default) OR dropped (`DeleteOnArchive()` — the
  hot-tier choice). The pin's `views` section carries the contract.
- **A read join is NOT a backing question and must not be folded into one.** It is decided
  by whether this aggregate genuinely reads across that foreign key, and it is legitimate on
  BOTH postures — on Mongo it simply reaches the entity and the rules rather than the view.
  `read-joins.md` owns when to offer it.
- **SharedBaseView (whenever a SharedBase is in play).** Mongo present → OFFER it
  (mechanics in `scaffold-entity`'s `sharedbase.md`) — where "present" means `mongo:`
  AND `transport:` both configured: a `mongo:` block WITHOUT `transport:` boots the
  declared collections but the sync consumer is skipped (boot INFO line), so they
  never receive a row — a bench/QA shape that does NOT count for this offer. No
  Mongo → do NOT offer it as
  available and do NOT refuse or go silent: point at the per-role plain view the dev
  already gets (base fields flattened — see Kinds above), frame the kind as a complement
  switched on later via `/omnicore:configure`, and offer that route only when the dev
  actually wants the multi-role identity document.
- Never ask what the posture already answered; never skip a question it doesn't answer.
- **Tone, always:** state costs plainly, no manufactured debt, no lock-in framing —
  "reversible, no code lost" is the literal truth.
