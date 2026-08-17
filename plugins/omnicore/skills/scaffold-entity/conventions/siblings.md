# conventions/siblings.md — 1:1 nullable satellite (DELTA)

> NO code here, by design. Layout/naming: `service-layout.html`. The sibling DSL and write
> behavior: `table-schema.html` (Sibling tables) — reading it before generating is
> MANDATORY. This file carries only skill-level process, decisions and traps.

Load only when the model has a **sibling**: a disjoint slice of the SAME entity's fields
in a secondary table sharing the owner's PK (a 1:1 vertical split) — peel optional/bulky/
rarely-read facets off a hot row with **no new domain type**.

## Where a sibling can attach

A **single-owner node only**: any root (flat or ROLE) or any of its own aggregate
children. Width unlimited. The only exclusions are the SharedBase side (below) and a
sibling of a sibling.

> **⚠️ A sibling CANNOT attach to a SharedBase (the base) nor to a base-child — boot
> PANIC.** A base has many roles, so the 1:1 doesn't apply. In a SharedBase model the
> elicitation splits: a BASE-level (identity-wide) 1:1 facet → nullable columns ON the
> base (not a sibling); a ROLE-specific facet → a sibling on the role table.

## Shape rules (decisions)

- The sibling's fields live on the OWNER struct as **pointers** (nil = no row) — no
  sibling Go type; the split is purely physical, declared only in the schema.
- **Strict partition boot check:** every Go field/column belongs to exactly one of
  {owner, its siblings} — overlap panics.
- **A COMPOSITE value object is never SPLIT across the owner and a facet** — the framework
  refuses it at the boot checkpoint (the "once rule"): a facet is loaded by its OWN
  statement, so an absent facet row leaves half the value object zeroed and it reconstructs
  half-built, and an optional composite's "every part NULL ⇒ absent" verdict cannot be
  reached by either half alone. The strict partition above is about single fields and does
  NOT license this — every part of one value object lives in ONE table. And a composite that
  lives on the facet must be OPTIONAL (`*Money`): the facet is emptied by nilling its
  fields, and one held by value has no nil to be emptied to.
- **A COMPOSITE value object is never SPLIT across the owner and a facet** — the framework
  refuses it at the boot checkpoint (the "once rule"): a facet is loaded by its own
  statement, so an absent facet row would leave half the value object zeroed and reconstruct
  it half-built, and an optional composite's "every part NULL ⇒ absent" verdict cannot be
  reached by either half alone. The strict partition above is about single fields and does
  NOT license this: put every part of one value object in one table. A composite that lives
  ON the facet must be OPTIONAL (`*Money`) — the facet is emptied by nilling its fields, and
  one held by value has no nil to be emptied to.
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
>
> **⚠️ And the GraphQL corollary — check it whenever GraphQL: yes.** The PUT-all-null
> clear path does NOT exist on that surface, and both ways round are closed: a LENIENT
> mutation cannot express it because an omitted field and an explicit null reach the DTO
> identically (the args map is marshalled to JSON and decoded; both leave the pointer
> nil), so "clear this" and "leave this alone" are one message; a STRICT one cannot
> either, its input being NonNull throughout, so null dies at parse. The intent therefore
> needs its own VERB: a dedicated bodyless mutation via `MutationByID` (`graphql.html` at
> the pin).
>
> **That mutation must dispatch through the FULL update handler, not the partial one** —
> the difference is the whole feature. The framework's sibling write reads an all-nil
> facet as "untouched" on a partial write and as "delete the row" on a full one
> (`applySiblingUpdates`: `if allNilFields { if partial { continue }; …delete… }`), so a
> PATCH-shaped clear answers 200 and changes nothing, which is worse than not offering it
> — the caller believes the facet is revoked. Verified by A/B on one spec and one
> database, the handler the only variable: partial left `Helena` in place, full cleared it
> and kept the children.
>
> **`omnicore-gen` emits this automatically** for any root-attached facet when
> GraphQL is on — `clear<Facet>Of<Entity>`, its command, and its tests. On the manual path
> it is yours to write; either way, surface the coupling in the spec so the dev sees that
> the contract closes on both surfaces.

## Elicitation reminder

**The elicitation happens BEFORE this file is loaded** — Phase 1 item 2 owns it and carries
the option text; if you are reading this, the dev already chose. What follows is the
post-decision reminder, never the trigger.

Siblings are hard to infer from prose — for any group of optional/sparse fields, weigh
*sibling table* vs *plain nullable columns on the owner*, plus the attachment node, and
surface the choice in `spec.md` §4 (guidance: SKILL.md Phase 1, item 2).
