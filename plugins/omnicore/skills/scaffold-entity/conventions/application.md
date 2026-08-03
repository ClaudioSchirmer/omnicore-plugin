# conventions/application.md — the application layer (flat / base case)

> NO code here, by design. Layout/naming/granularity: `service-layout.html`. API mechanics
> (command bases, handler table, signatures): the routed `/docs` — reading them before
> generating this layer is MANDATORY. This file carries only skill-level process, decisions
> and traps.

Deltas load separately: SharedBase insert → `sharedbase.md` · child input DTOs →
`aggregate-children.md`.

Docs for this layer: Auto handlers (the six-verb table: base to embed, input method,
handler, actionName, strict-body flag) → `auto-handlers.html` · CommandHandler / Result /
Dispatch / ScopedRepository → `command-handler.html` · query handlers + control keys →
`auto-query-handlers.html`.

## Files & naming

Per `service-layout.html`: one Command per verb (`insert_`/`update_`=PUT/`patch_`=PATCH/
`delete_`/`archive_`/`unarchive_`), Result co-located with its Command; queries by-id
SINGULAR / by-params PLURAL, one type per file; `dtos/` exists only for child-collection
inputs. **NO handler files** — the Auto handlers are framework generics instantiated at
the web layer, never written here.

## The verbs — process notes on top of the docs

- Take every base/input-method/actionName from `auto-handlers.html`'s handler table —
  never from memory.
- **The SharedBase insert is the 7th handler** and differs (ApplyTo not ToEntity, upsert
  semantics) → `sharedbase.md`.
- Mappers raise notifications **by return** — a fallible step returns the error; a pure
  mapper returns nil.

## Result — the entity has the final word (the FULL mirror)

`FromEntity` projects the **post-write entity**, never an echo of the input — the same
instance was validated (and possibly normalized), persisted, and is now projected. The
projection is the FULL aggregate mirror: every root field INCLUDING a sibling facet's
pointer fields, plus child collections via `domain.GetCurrentItemsOf` — ids included (the
persister writes minted child ids back; confirm availability for the pinned version).
**NEVER copy fields from the Command into the Result — read ONLY from the entity
parameter**; an input-echo silently hides every domain transformation. Contract text:
`auto-handlers.html` (projection contract). Bodyless verbs return the framework's empty
result.

## Wire → VO mapping — a CAST, not a constructor (avoid bloat)

The Command holds the VO fields' UNDERLYING scalars (string VO/enum → `string`, int enum →
`int`, nullable → a pointer), mirroring the Request 1:1. Turning them into VO fields in
`ApplyTo`/`ApplyPartiallyTo` (and a child input DTO's `To<Child>()` builder) is a plain Go
**type conversion** — never a constructor, never a per-field validate/normalize step (the
framework auto-validates VO fields — domain.md):
- raw / enum field → a direct cast (`vos.Email(c.Email)`, `vos.Ethnicity(c.Ethnicity)`); an
  out-of-set enum value is caught by the automatic check, NOT by an `if` here — and NOT via
  `EnumByValue` by default.
- nullable field → a POINTER cast (`(*vos.Phone)(c.Phone)`) — nil-safe by construction, so
  NO `if != nil` guard on insert/PUT.
- `FromEntity` is the inverse: `.Value()` for a raw/enum field, a pointer cast back
  (`(*string)(u.Phone)`) for a nullable one.

**`if x != nil` belongs to PATCH only.** `ApplyPartiallyTo` is tri-state (nil = not sent), so
each line is guarded `if c.X != nil { u.X = <same cast> }`; insert and PUT assign
unconditionally. An `if != nil` on the insert/PUT mapper, or a hand-rolled validate/normalize
per field, is the bloat to avoid — the cast plus automatic validation is the whole job.

## PUT vs PATCH — the decision + the trap

Default PATCH; ask PUT/PATCH/both at the spec. **PATCH can never assign NULL** (absent and
explicit null are indistinguishable) — clearing nullable state needs PUT or an
intent-specific operation, and a sibling facet makes PATCH-only INVALID (§4↔§8 coupling —
`siblings.md`). PUT is strict (a missing body field 400s before dispatch); if both verbs
exist, split rules via `actionName`.

## The `ctx` gateways — the ONLY consumers of `ctx` below web

The Command/Query layer is where identity-derived security lives:
- **Write:** the input mappers feed runtime-only authz fields from `ctx.Identity()` onto
  the entity (read by `BuildRules` — domain.md Layer-2).
- **Read:** `ToCriteria(ctx)` injects restrictions/tenant filters.
See `authz-seams.html`.

## Service injection — enforced pairing

When the entity declares `RequiresService()`, set the handler's `Service` field at the web
wiring — on EVERY handler whose verb can trigger the rule (unarchive included: reactivating
state can collide). Declaring without injecting raises `ServiceIsRequiredNotification` at
invocation — generate both sides together, never one without the other.
