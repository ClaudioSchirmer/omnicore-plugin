# conventions/domain-map-template.md — the Phase 1 domain map skeleton

Copy VERBATIM to `specs/scaffold-system/<system>/domain-map.md`, then fill. Rules: every section
stays (inapplicable → `N/A — <why>`); a decision only the dev can make is
`⚠️ OPEN: <question>`; high-risk picks carry `(proposed)` + the alternative(s) beside
them; NO code and NO field-level detail beyond what's needed to place a field group on
a table — those belong to the delegated per-entity specs. Approval flips `Status` to
`APPROVED`; no delegation before that.

---

# Domain map — <system name>

- **Status:** DRAFT
- **Framework pin:** <from `go.mod` — informative only; docs of that pin are the authority>

## §0 Source request (verbatim)

> <the dev's prose, untranslated, unabridged — every delegated run receives its slice
> of THIS text>

## §1 The system, in one paragraph

<what is being built, restated in the run's language>

## §1p Read-side posture (system-wide)

Full distributed CQRS (Mongo-projected, canonical) · Reduced/MVP (relational, SoR-served)
— the DEFAULT backing for every entity view below; from `specs/scaffold-service/spec.md` if it
set one, else decided here. Per-entity override allowed (§9), with a reason.

Engine/infra: full-CQRS engine (Mongo + broker + CDC) · SQLite zero-infra (no Docker;
Mongo views §5 + integration events §6 deferred to a later `/omnicore:configure`
conversion — noted in the map, never dropped).

## §2 Aggregate inventory

| # | Aggregate | Kind | Purpose (one line) | Depends on | Status |
|---|---|---|---|---|---|
| 1 | <name> | flat · role of `<base>` | … | — · #n | pending |

Kind and dependencies come from §3/§4. `Status`: `pending` → `scaffolded` (Phase 2
marks it) · `exists → evolve-entity` (Phase 0b collision) · `parked` (§8).

## §3 Shared identities (SharedBase)

Apply `scaffold-entity` Phase 1 item 1 — identity smell, the two-active question asked
literally, the role-cardinality digest — across ALL §2 entities at once. One block per
base; `N/A — no shared identity detected` if none.

- **Base:** `<table>` — create NEW · REUSE existing (per Phase 0b)
- **Natural key:** `<field>` (proposed — CONFIRM; the highest-risk slot of the map)
- **Shared fields:** <the identity's field group>
- **Roles now:** <which §2 entities>  ·  **Roles later:** <named-but-parked, from §8>
- **Two ACTIVE rows of the same role per identity?** ⚠️ OPEN — asked literally, per
  role; the answer decides role vs plain 1:N
- **Identity view:** create · add-role · skip (decided ONCE here; the delegated run
  executes, doesn't re-ask) — gated on §1p's infra: no Mongo (SQLite / zero-infra MVP) ⇒
  `n/a — needs Mongo (enable via /omnicore:configure)`, never offered as available; a full
  engine still HAS Mongo even when its entity views are relational-backed. The base and its
  roles stay in the map either way (§3 is write side).

## §4 Cross-aggregate references

| From (#) | To (#) | Field | Nullable? | Reached on read? | Why this direction |
|---|---|---|---|---|

The referenced side scaffolds first (§7). `N/A — single cluster, no cross links` if
none.

## §5 Read models beyond per-entity

| View | Kind (composed · upstream · embed) | Joins / covers | After rows |
|---|---|---|---|

Delegated to `scaffold-view` in Phase 3. Identity views do NOT list here — they ride
with their base's first role (§3). Report SCALARS (counts/sums/totals) are NOT a view
kind — a filtered total over a listing is the `?onlyTotal` DTO opt-in; richer scalars
are the `Aggregate`/`AggregateBy` DSL on the write-side aggregate loader, available on
every posture (`shared/read-side.md`); list them in §6 as an `implement` delegation
instead.

## §6 Integration events / external systems

<events consumed/produced across services, external calls, extra surfaces — or
`N/A — self-contained`. Executed in Phase 3, one `implement` delegation per item>

## §7 Scaffolding order

1. #<n> `<aggregate>` — <forcing reason: first role of base X · referenced by #m · …>
2. …
Then Phase 3: <§5 views, in order>.

## §8 Out of scope — parked explicitly

<what the request named but excluded, and the map consequence — e.g. "sale/management
parked, BUT they are named roles of the same asset, so the identity is modeled NOW
(§3) and they arrive later as add-role runs">

## §9 Pre-answered slots, per entity (the delegation contract)

One block per §2 row. These are handed to the delegated run AS ANSWERS — it must not
re-derive them; everything absent stays the run's own to decide or ask.

### #<n> <Aggregate>

- **Kind:** flat · role of `<base>` (create/reuse per §3)
- **Natural key (if role):** <from §3>
- **Identity view (if role):** <from §3 — create · add-role · skip · n/a; copied here so
  the delegated run never re-asks what §3 already decided>
- **Children (1:N):** <name → child of role/flat root vs child of base> — hint only;
  edit strategy, DTOs, endpoints are the run's
- **Sibling hint (1:1 optional group):** <name it if the prose shows one — the run
  details it>
- **Cross-references:** <fields from §4, with the referenced aggregate's status at
  delegation time>
- **Slice of §0:** <the sentences of the source request this entity owns>
- **View backing:** relational · Mongo (default = §1p posture; override only with a reason)
- **Read joins:** <the §4 references this entity READS ACROSS, per the "Reached on read?"
  column — which columns each brings back, and whether they are served to callers or exist
  for the rules alone. Blank = none.>
