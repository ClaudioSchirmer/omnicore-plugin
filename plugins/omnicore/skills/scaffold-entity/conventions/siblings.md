# conventions/siblings.md — 1:1 nullable satellite (DELTA)

> NO code here, by design. Layout/naming: `service-layout.html`. The sibling DSL and write
> behavior: `table-schema.html` (Sibling tables) — reading it before generating is
> MANDATORY. This file carries only skill-level process, decisions and traps.

Load only when the model has a **sibling**: a disjoint slice of the SAME entity's fields
in a secondary table sharing the owner's PK (a 1:1 vertical split) — peel optional/bulky/
rarely-read facets off a hot row with **no new domain type**.

## Where a sibling can attach

A **single-owner node only**: a flat root, a ROLE, or a role-child. Width unlimited.

> **⚠️ A sibling CANNOT attach to a SharedBase (the base) nor to a base-child — boot
> PANIC.** A base has many roles, so the 1:1 doesn't apply. In a SharedBase model the
> elicitation splits: a PERSON-level 1:1 facet → nullable columns ON the base (not a
> sibling); a ROLE-specific facet → a sibling on the role table.

## Shape rules (decisions)

- The sibling's fields live on the OWNER struct as **pointers** (nil = no row) — no
  sibling Go type; the split is purely physical, declared only in the schema.
- **Strict partition boot check:** every Go field/column belongs to exactly one of
  {owner, its siblings} — overlap panics.
- DDL: the sibling table's PK IS the owner's PK (FK, cascade delete), columns nullable,
  **no lifecycle columns** — the owner owns the lifecycle.
- Commands/requests: the sibling fields are just more pointer fields on the owner's DTOs —
  no separate input type, **no dedicated endpoint ever** (the facet is part of the root's
  contract, managed through the root's verbs).

## Write behavior (from the docs — drives two rules here)

All-nil facet: skipped on INSERT, untouched on PATCH, **deleted on a full PUT** (the
absent facet means "remove it"). Hence:

> **⚠️ Sibling ⇒ the update shape CANNOT be PATCH-only — include PUT on the root.** PATCH
> never assigns NULL, so with PATCH-only a granted facet could NEVER be cleared. The
> clear path is the framework's native one: the ROOT's PUT with the facet's fields all
> null/absent. Surface this §4↔§8 coupling in the spec.

## Elicitation reminder

Siblings are hard to infer from prose — for any group of optional/sparse fields, weigh
*sibling table* vs *plain nullable columns on the owner*, plus the attachment node, and
surface the choice in `spec.md` §4 (guidance: SKILL.md Phase 1, item 2).
