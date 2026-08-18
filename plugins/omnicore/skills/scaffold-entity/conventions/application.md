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
SINGULAR / by-params PLURAL, one type per file, each with its Result co-located — the read
side mirrors the write's Command+Result pairing; `dtos/` exists only for child-collection
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
result — take its NAME from `auto-handlers.html` at the pin, never invent a local empty
struct for it.

## Read — the same anatomy, reversed

A query is a command read backwards, and the layer's shapes say so. Take the
signatures from `auto-query-handlers.html` at the pin; what follows is the process.

- **A query declares a RESULT, and the Result owns field EXISTENCE.** Application-pure —
  no wire tags, field names identical to the view document's Go keys — co-located with
  its query, one per read shape. A field absent from it can reach no surface: REST,
  GraphQL and the export all consume the Result through the Response, and none of them
  ever sees the document again.
- **`FromQueryResult(ctx, r)` is MANDATORY and is the read's `FromEntity`.** The framework
  fills the Result from the stored document and hands it here BEFORE any transport sees
  it. That placement is the point: derived values and ctx-aware shaping computed at this
  seat are computed ONCE per document, so every surface renders the same thing. The same
  work done in a Response's `FromResult` runs once per surface and the export disagrees
  with the JSON. Nothing to derive → `return r, nil`.
- **`?fields=` forces pointers, recursively, on the Result AND the Response.** A caller
  asking for a subset means a leaf has to arrive ABSENT, and a value type cannot tell
  absent from zero. The framework boot-guards it on both shapes; a by-id read declares no
  `?fields=`, so plain values are right there.
- **A COMPUTED field is one the store does not hold.** Declare it on the Result, fill it
  in `FromQueryResult`, and tag the Response `computed:"Src1,Src2"` with the STORED fields
  it reads. The tag is what makes it work rather than merely appear: `?fields=<computed>`
  fetches the sources (there is no column behind the name), `?orderBy=<computed>` is a
  typed 400 on every surface, and a `filter:` over one is a boot panic. A source need not
  appear on the Response — one that exists only on the Result feeds the derivation and
  never reaches the wire.

## A per-entry command is FLAT — one command, one responsibility

A collection reaches the application layer two ways, and the shapes differ because the
OPERATIONS differ. The root's insert/update handles MANY entries, so it carries
`[]dtos.<Child>Input`. A per-entry verb (`Add`/`Change`) handles exactly ONE, so the
entry IS the command: its fields sit directly on the command, beside the `<Child>ID`
the route supplies on `Change`/`Remove`. That is not an inconsistency to iron out —
each command says what it is for.

Two consequences worth knowing before writing either:

- **`ApplyTo` still goes through `dtos.<Child>Input`**, building it inline from the flat
  fields. The input type is where a value object is REASSEMBLED — an enum cast, a
  composite folded from its parts — and that reassembly belongs in one place. What
  `ApplyTo` writes is a flat copy of scalars, which is the half that can repeat without
  drifting.
- **A child field named `<Child>ID` collides** with the path field on the per-entry
  commands. `check` refuses it with that reason.

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
- **composite field → the ONE place the flat wire is folded back into the value object.** The
  Command carries the parts FLAT, under the names the schema exposed them by
  (`SalaryAmount`, `SalaryCurrency`) — the wire never sees a nested object, because nothing
  above the schema knows a composite exists. The mapper composes:
  `u.Salary = vos.Money{Amount: c.SalaryAmount, Currency: vos.Currency(c.SalaryCurrency)}`,
  each part cast by its OWN kind (a VO part is still just a cast). `FromEntity` reads it
  back part by part (`out.SalaryAmount = u.Salary.Amount`).
  - **An OPTIONAL composite (`*Period`) is decided as a GROUP, both ways.** Build it only
    when at least one part arrived, and leave it `nil` otherwise — that is what writes NULL
    to every part column, and it is the same verdict the read side takes when every one of
    them comes back NULL. Projecting is the mirror: `if e.Trial != nil { … }`, and a
    mandatory part inside it needs a local to point at (`from := e.Trial.From; out.TrialFrom = &from`).
  - Under PATCH the parts are guarded one by one like any other field, materialising the
    value object on first use (`if e.Trial == nil { e.Trial = &vos.Period{} }`). A PATCH can
    fill an absent composite in but never clear it — the same limit every nullable field has.

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
- **Read:** `ToCriteria(ctx)` injects restrictions/tenant filters — and `FromQueryResult(ctx, r)`
  is the second gateway, where ctx-aware SHAPING of the returned data belongs.
See `authz-seams.html`.

## Service injection — enforced pairing

When the entity declares `RequiresService()`, set the handler's `Service` field at the web
wiring — on EVERY handler whose verb can trigger the rule (unarchive included: reactivating
state can collide). Declaring without injecting raises `ServiceIsRequiredNotification` at
invocation — generate both sides together, never one without the other.
