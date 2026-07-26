# conventions/sharedbase.md — SharedBase role (N:1 party-role) (DELTA)

> NO code here, by design. Layout/naming: `service-layout.html`. The base/role DSL, upsert
> handler and view mechanics: the routed `/docs` — reading them before generating is
> MANDATORY. This file carries only skill-level process, decisions and traps.

Load only when the entity is a **role over a shared identity**: one identity table (e.g.
`persons`) shared by several role tables, deduplicated by a **natural key** — the
framework derives the identity PK as a deterministic UUIDv5 of it (no read-back). Only the
insert path and the schema/repo/migration differ from flat; update/patch/archive/unarchive
are the flat shapes — except base-children editing (see the hazard in
`aggregate-children.md`).

Docs: `table-schema.html` (Shared base + Shared-base view + managed columns on the base) ·
`auto-handlers.html` (the 7th handler — shared-base insert/upsert).

## Domain

**No separate base Go type** — the role struct carries the shared identity fields AND its
role-private fields; the schema partitions them physically. The **natural key is
immutable**: the framework raises its own immutability notification on the write path —
check `table-schema.html` before adding a manual `Old`-guard, and don't duplicate the
framework's check.

## Schema — decisions

- The base is a type-less `NewSharedBaseSchema(table)` declared as an **exported function in
  `schemas/`**, callable from every role file — the engine registers by the base's TABLE,
  so N identical declarations behave as one; two DIVERGENT declarations panic at boot.
- **Link model (one-line choice, surface it in the spec):** shared-PK (role.id == base.id;
  ≤1 row per identity structurally) vs separate-FK (own id + FK; needs its own role-row
  uniqueness strategy — see Migration). Detected from whether the SharedBase column equals
  the PK column.
- **Managed columns on the base are honored when DECLARED** — stamped on the identity's
  creation and on role-driven changes of shared fields; confirm the pinned version's
  behavior in `table-schema.html` (Shared base). Undeclared = never touched (keep a DDL
  default if you only want a creation timestamp).

## Repository

Embed the SharedBase role repository (the plain aggregate one rejects a SharedBase
schema). Bind the PK/FK-collision constraint names per dialect — the happy-path 409 for
"this identity already holds an active role". The `Constraints` match is by NAME and
table-agnostic: base-table unique constraints (e.g. `persons_email_key`) bind HERE too.

## Insert — the 7th handler

- The command declares **`ApplyTo` (not `ToEntity`)** and it must be a **pure, idempotent
  mapper** — the handler may run it twice (a throwaway call to read the natural key, then
  again on the loaded identity on the warm path). No side effects.
- **Cold vs warm:** base absent → `actionName == "GetInsertable"`; base present → the same
  call arrives as `"GetUpsertable"` — branch rules there to tolerate base state a sibling
  role already set.
- **Warm merge-vs-reject (base-children):** on the warm path the identity's existing
  base-children load as Constructor items; the ROOT method decides — merge (silently skip
  a re-sent duplicate; keeps the cross-role POST idempotent — recommended) or reject
  (422). Put that logic in the root method, never inline in `ApplyTo`.

## Migration — rules

- **The natural key is `UNIQUE NOT NULL` — mandatory** (it derives the PK; a null key
  collapses every key-less record into one identity — silent corruption).
- The role→base FK is **`ON DELETE RESTRICT`** (any referencing table vetoes the orphan
  purge).
- **Role-row uniqueness:** shared-PK gets it free (the PK). Separate-FK: plain
  `UNIQUE(fk)` (an archived remnant blocks a new POST) OR active-only uniqueness (the
  mechanism is per dialect — partial index on postgres, filtered index on sqlserver, a
  soft-delete-gated generated column on mysql, a function-based index on oracle; read
  the PINNED `table-schema.html`, Active-only uniqueness, for the target dialect's
  shape) to allow a remnant beside a new active row.
- **⚠️ MySQL & SQL Server `BINARY(16)`, Oracle `RAW(16)`: every id/FK here** — base id, role id, the role→base FK, every
  base-child/role-child FK. These are all framework-MANAGED slots, native on EVERY pin —
  the pin-driven id-typing rule (SKILL.md boot-traps, "Id typing") governs only the
  non-managed reference fields (`migrations.md`).

## Lifecycle & 409 semantics (for generated comments)

The base has no lifecycle of its own — it **converges** from its roles: archiving the last
active role archives the base + its native children; deleting the last role converges per
`OrphanPolicy`. A POST for a document with an ACTIVE role → 409; an ARCHIVED role's
remnant vetoes on the PK/unique index (same 409); `/unarchive` is the explicit way back.

## Read — the SharedBaseView — ALWAYS OFFER IT

The read counterpart of the shared write: one document per identity — base fields flat,
base-children at the root, one sub-document per role. Elicit it whenever a SharedBase is
in play (spec §1), two cases told apart in Phase 0b:
- **No identity view exists** → offer to CREATE it (first role).
- **One exists** (adding a NEW role to an existing base) → offer to ADD the role to it —
  **and BUMP its `Version(N)`**: the role set is in the rebuild hash; forgetting the bump
  aborts boot.

**Tone:** never say "costly to add later" — false. A view is an additive projection over
the same CDC stream; adding/extending later is the SAME effort (one automatic rebuild).
Offer neutrally, no manufactured debt. Role segments are named by the role's Go type;
an absent role is an omitted key (response fields pointer + omitempty); it COMPLEMENTS the
per-role views, never replaces them.

**Placement — the shared identity is its OWN bounded context** (`service-layout.html`):
the identity view gets its OWN routes file, its OWN feature and its OWN `<identity>:read`
permission — all named after the BASE, never after a role, and never mounted inside a
role's routes or feature. Roles write; the identity view reads.

**Read surface — the standard PAIR, never by-id alone.** The identity view mounts by-id
AND by-params (a paged, filtered list; filterable paths cover base fields, base-children
and role segments — the filter-path mechanics are in `auto-query-handlers.html`), exactly
like any entity's read surface — an identity view without its list is half a feature.
Elicit its filters in the same breath as the role's, and offer the view's exports only if
the dev asks (same rule as any view).
