# shared/read-side.md — read-side posture, one owner

The single home for **how entity read models are served** and what each posture can and
cannot do. Every skill that decides, offers, asks about, or explains a view's backing
routes HERE instead of restating posture facts inline — one owner, no drift. No code here,
by design: knowledge and elicitation policy only.

## The two postures

- **Mongo-projected** (canonical) — views projected through the CDC pipeline: O(1)
  document reads, the FULL read-side vocabulary (embeds, links, composed/shared views,
  free-text search, child/sibling filter+sort), eventually consistent (CDC lag).
- **Relational-served** (`.RelationalSource(repo.Loader)`) — the view reads straight from
  the SoR: read-your-writes, NO CDC wait, read-time aggregate composition instead of an
  O(1) fetch. Capability limits below.

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

- A multi-source / read-time-join view KIND (SharedBaseView, ComposedView, the
  Embed/Link family, Upstream) is **relational-INELIGIBLE by construction**
  — boot fail, a 400, or no `.RelationalSource()` to carry at all. The exact kind set
  and failure mode per kind are the PIN's (parity table in `relational-view`).
- **"Aggregated view" is NOT a view kind.** A filtered TOTAL over an existing listing is
  the `?onlyTotal` DTO opt-in on the list request (`auto-query-handlers` at the pin,
  both backings). Richer counts/sums/report scalars are the
  `Aggregate`/`AggregateBy` DSL on the write-side RELATIONAL aggregate loader
  (`custom-command-handler` at the pin — the AggregateLoader section) — computed on the SoR, never a Mongo view, and
  therefore available on EVERY posture, SQLite included. Refusing "give me totals"
  on an infra-free project (or pushing a Mongo conversion for it) is a wrong refusal.
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
load** — a root column, a sibling, or a shared-base field (the last two joined in) — and
rejects what would need a **1:N pushdown** — `?search=`, and filter/sort on a 1:N
child field (a dotted child path, or a child-level sibling) — plus an unknown field.
Rejections are a typed 400
(`RelationalCapabilityNotification`, field named), never a 500.

**Anti-drift boundary: the authoritative, version-exact capability/parity table is
`relational-view` at the PIN.** Older pins genuinely differ (satellite filter/projection
is recent). Read it only when you must answer a precise capability question, explain a
400, or walk a backing flip — the pin is the truth, never this file.

## Mechanics that never drift

- **Loader reuse** — a relational view takes the aggregate's EXISTING `repo.Loader`,
  never a second loader. A wrong-table loader fails the `BoundTable()==schema.Table()`
  boot guard; a second loader on the SAME table boots fine and is pure waste.
- **`DeleteOnArchive()` is a MONGO-PROJECTION knob** (it drops the projected document).
  A relational-served view composes from the SoR at read time — there is no document to
  drop, so its archive regime is kept-but-hidden ONLY (`?includeArchived` reveals).
- **No lock-in, ever.** On a full engine the bench ships FULL (Mongo + relay) either
  way, so moving a view to Mongo later is a per-view flag: drop `.RelationalSource()` +
  bump `Version(N)` → one automatic online blue-green rebuild, nothing re-scaffolded.
  On SQLite, unlocking the Mongo kinds means **Mongo + a CDC relay over a tailable
  engine — an engine swap ALONE does not provide it**; `/omnicore:configure` does the
  whole conversion in one pass (engine swap, devops, Mongo, broker, relay; re-asks the
  infra questions; ports the migrations to the new dialect's SQL) — fully reversible,
  no application code lost.

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
