# conventions/domain.md — the domain layer (flat / base case)

> NO code here, by design. Layout/naming/granularity: `service-layout.html`. API mechanics
> (signatures, DSL, behavior): the routed `/docs` sections — reading them before generating
> this layer is MANDATORY. This file carries only skill-level process, decisions and traps.

Covers a **flat** entity's domain. Deltas load separately: children →
`aggregate-children.md` · SharedBase → `sharedbase.md` · siblings → `siblings.md`.

Docs for this layer: rules DSL → `rules-dsl.html` · notification→status →
`status-mapping.html` · `Old()` → `old-state.html` · authz → `authz-seams.html`.

## Files — THREE domain packages (per `service-layout.html`)

The domain layer is not one flat folder. One domain type per file, split across three
packages by what the type IS:

- `internal/domain/` — the **root aggregate** (`<entity>.go`), its `domain.Service` port
  (`<entity>_service.go`), and `notifications.go` for the notifications the ROOT emits.
- `internal/domain/vos/` — the **value objects** (one file each: `email.go`, `zip_code.go`,
  `relationship.go`, …) + this package's own `notifications.go` + a `doc.go` package comment.
  A VO used ONLY by a child still lives here (an enum a `Dependent` carries → `vos/`, never in
  `aggregatevos/` or the child's file); `aggregatevos` imports `vos`, never the reverse (`vos`
  stays a leaf).
- `internal/domain/aggregatevos/` — the **aggregate value objects** (the children:
  `address.go`, `dependent.go`, …) + this package's own `notifications.go`.

**Three separate `notifications.go`, by necessity not taste:** `domain` imports `vos` and
`aggregatevos`, so a notification a VO emits cannot live in `domain` (it would cycle) — each
package owns the notifications ITS types emit. All three `notifications.go` and the seven
translation catalogs are **registration sites** — existing files you APPEND to (like
`wire.go`), never per-entity copies, never regenerated.

## The aggregate struct — decisions

- Nullable ⇒ pointer; money = `int64` minor units, never float.
- **Struct tags: a persisted field carries `labelKey` and NOTHING ELSE — no `json:`
  tag, no `db:` tag, ever.** This is the reflex to resist: a domain aggregate is NOT a
  wire DTO, so the Go habit of stamping `json:"..."` on every field is WRONG here. Wire
  names live on the web-layer DTOs (`internal/web/dtos`); physical column names live only
  in the infra schema. Worse than redundant: a `json:"-"` silently drops the field from
  the `Old()` snapshot the framework builds via a json round-trip. The canonical example's
  domain structs carry `labelKey` only — match them.
- A **flat** entity does NOT implement `domain.AggregateRootProvider` — that is the
  children delta.

## Value objects — the DEFAULT for any validated field, never inline the rule

**The decision rule (inverted on purpose): a field that needs ANY validation beyond
presence/nullability — a format, a length/range bound, a closed set — IS a value object, by
DEFAULT.** Inline in `BuildRules` stays for exactly two cases: a pure-presence rule
(required non-empty) and a cross-field invariant (spanning two+ fields). "Only one aggregate
carries it today" is NOT a reason to inline — a VO is single-responsibility + one home for
the rule, not merely reuse. Model it as a VO type in `internal/domain/vos/`, never a bare
`string`/`int` with the check re-written in `BuildRules`.

**A field whose valid values are a FIXED, CLOSED set is ALWAYS an enum value object — no
exception, no judgment, no `plain`.** The test is mechanical and property-based, NOT a
Go-typing question: Go has no `enum` keyword — `EnumValueObject` is the framework's construct
(a named type over a `string`/`int` + a declared member list; the framework validates
membership). So don't ask "is this a Go enum?" (there is none) — ask "are the allowed values
a fixed list known in advance?" If yes — a status, kind, type, state, relationship,
frequency, ANY "one of N" — it is an `EnumValueObject`, every time. The ONLY field that may
stay inline as a `plain` exception is a RAW/format shape check (a local, one-off regex like a
country-specific `State`) the dev deliberately signs off in §2's `VO?` column — NEVER a
fixed-value set, never the agent's silent default. Full contract + examples:
`value-objects.html` (read it before generating this layer).

**Which kind — the two VO shapes:**
- a formatted/constrained primitive — Name, Email, Phone, Document/tax-id, ZipCode, a card
  number, a bounded quantity: a **raw value object** (`ValueObject[T]`), owns a bespoke
  `IsValid` (a regex, a length cap, a range).
- a FIXED/closed set of allowed values known in advance — a status, kind, type,
  relationship, frequency, ANY "one of N": ALWAYS an **enum value object**
  (`EnumValueObject[E,T]` — the framework's stand-in for the enum Go lacks), int- OR
  string-backed. Declares the const block
  (EXPLICIT values, never bare `iota`; the zero value is the `Unknown` sentinel, never a
  member) + `Values()` + an `Unknown…Notification`, and writes NO `IsValid` — the framework
  validates membership from the declared set.

**Validation is AUTOMATIC — the part a code-first reflex trips on.** You never call a VO's
validation by hand. The framework discovers every VO-typed field by reflection and
validates it on every write, IDENTICALLY on a root AND on an aggregate value object (a
`nil` pointer field is skipped as absent). So `BuildRules` carries ONLY the non-VO rules (a
required plain `string`, a cross-field invariant); an Email/ZipCode/enum field has nothing
to wire. To opt one out in a mode, `r.IgnoreValueObject("Field")` inside that mode's gate;
to force a VO that is not a plain field (computed, in a slice), `r.ValidateValueObject(name,
vo)`. Both on `*Rules`, root and child alike.

**Boundary + labels.** Turning a wire scalar into a VO field is a plain type CAST in the
command mapper — not a constructor, not hand-validation (details + the PATCH exception are in
application.md); an out-of-set enum value is caught by the automatic check, not by an `if`.
`domain.EnumByValue[E](raw)` is the OPTIONAL convergence helper for when you must fold junk to
`Unknown` explicitly (a differing wire type) — never the default mapper move. A VO never names
a label or a translation — the field LABEL stays on the aggregate struct field's `labelKey`
tag, enum values render per-locale via `EnumDescriptionKey` + the translator, and the VO's
own notifications go in `vos/notifications.go` (keys in all 7 catalogs).

## Modes() + BuildRules — traps the docs route you past

- **`Modes()` ⟺ the schema's archive-column declaration must agree** — `ModeArchive`
  without the declared column
  panics at repo construction (and vice-versa; keep them in lockstep).
- **Archive/unarchive have their OWN clauses — `IfArchive`/`IfUnarchive`** (gate on
  ModeArchive/ModeUnarchive). `IfUpdate` is PUT/PATCH exclusively; a rule left in `IfUpdate`
  will NOT fire on an archive transition. `actionName` is a free-form label, never a verb
  selector. Full clause set: `rules-dsl.html`.
- **`domain.Old(e)` is nil on Insert** — guard before dereferencing.
- **One clause per mode is the DEFAULT — group by field, not by mode.** Repeating a mode
  clause is legal (`IfInsertOrUpdate` just runs its closure each time — nothing is registered,
  stored, or overwritten), but two scattered `IfInsertOrUpdate` blocks read as accidental
  duplication. Keep all of a field's rules (required / immutable / unique) adjacent under a
  single clause of each kind it needs.
- **Never write code to dodge an automation.** When a rule must run only if a VO field has a
  value, remember the VO's own automatic check already raises the required — so gate the rule
  POSITIVELY on the value you want (`if a.Matricula != "" { …uniqueness… }`), never an early
  `if a.Matricula == "" { return }`. A `return // the VO already raised it` is ugly control
  flow that exists only to escape a validation the framework already runs for you; state the
  condition you DO want instead.
- Prefer framework built-in notifications (`RequiredFieldNotification`,
  `SchemaViolationNotification`) — they need no translation entry. Regex validations:
  package-level compiled vars.

## Service — rules that need the outside world (optional)

- **Opt-in + enforced:** the entity declares `RequiresService() → true` (default false);
  only then does `BuildRules` receive a non-nil service. Declaring it OBLIGATES injecting
  the `Service` at the handler wiring — a nil there raises
  `ServiceIsRequiredNotification` at invocation. Generate both sides together.
- **File placement:** the interface is one more domain type → its own
  `internal/domain/<entity>_service.go` (`service-layout.html`); never embedded in the
  entity's file.
- **Layer precision:** the domain holds the INTERFACE only (zero IO); the implementation
  lives in infra (repo read for facts this service owns · httpclient for the external
  world · grpcclient for another microservice — decision matrix in
  `service-to-service.html`); injection at the wiring.
- **The port declares the method(s) DIRECTLY and returns PURE VALUES — never an `error`.**
  Shape it as `type FooService interface { domain.Service; ActiveThing() bool }` — the fact
  method lives ON the interface, embedding `domain.Service`. Do NOT model a struct carrying a
  separate sub-port (`struct{ domain.ServiceBase; Stats SomePort }`) — that indirection is the
  over-engineering to avoid; one interface = the port with the method. And the method returns
  ONLY the value the rule needs (a `bool`, a count, a `[]Fact`), **never `(T, error)`**: the
  domain has zero IO and must never receive or handle an infra error. A port shaped
  `(T, error)` forces the rule to panic / notify / swallow — all three are wrong. IO failure is
  infra's problem (see `infra.md`).
- **`BuildRules` NEVER panics, and NEVER guards the service defensively.** `RequiresService()
  true` already guarantees a non-nil service (else `ServiceIsRequiredNotification` fires BEFORE
  rules run), and the infra compile-time assertion (`var _ FooService = (*FooServiceImpl)(nil)`)
  plus the wiring guarantee the concrete type. So assert-and-use in ONE line —
  `facts := svc.(FooService).ActiveThing()` — with NO `if !ok { panic }`, NO nil-check, NO
  `if err != nil { panic }`. The domain has no panic path whatsoever; the ONLY way a rule
  rejects is `r.AddNotification`. A panic in a domain file is always a bug.
- **The canonical use is uniqueness/anti-duplication among actives**: the pre-check with
  exclude-self semantics (and unarchive in scope when reactivation can collide) that lets
  the duplicate report TOGETHER with the other validation errors — the DB unique index
  stays as the race-window backstop, never as the primary UX (defense in depth; see the
  chain in `infra.md` and the pattern in `auto-handlers.html`, "Domain Service via Auto
  handler"). Cardinality and threshold rules ("at most N active X", "total/average/extreme
  within a bound") are the same pattern with the loader's other scalar facts, and PER-GROUP
  invariants ("at most N per category", "no more than K distinct keys") are the same
  pattern again over the loader's GROUPED facts when the pinned version ships them — see
  `custom-command-handler.html` (Loading by criteria and its grouped-aggregates part, per
  the pinned version).
- **Gap — no living molde:** no example entity uses a service; build from the framework
  contract and let the tests weigh more.

## Normalization — the entity has the final word

To canonicalize input ("claudio" → "Claudio"), mutate the entity inside `BuildRules`: the
same instance is persisted AND projected, so the change reaches the DB and the response.
The framework offers no `Normalize()` hook and we don't add one. Keep it **idempotent**
(rules may run more than once — e.g. the SharedBase warm path) and put it in a private
helper called at the top of `BuildRules`.

## Custom notifications — rules

- Struct name IS the translation key; parameterized values via `tvar` tags interpolated as
  `{var}` in the catalog string; default semantic is Validation → 422, override
  `Semantic()` per notification for conflict (409) / forbidden (403). Mechanics:
  `status-mapping.html`.
- Unique-field conflicts are custom `<Field>AlreadyExists…` notifications with the
  conflict semantic — `EntityAlreadyAddedNotification` is the framework's PK-collision
  one, not a substitute.

## Translations — ALL SEVEN, always

Every key in all 7 catalogs (`ptbr,eng,esp,fra,deu,ita,nld`), each a REAL translation —
never English copied seven times, never a subset, and **NEVER an elicitation question**.
Three key kinds per entity:
1. every custom notification name,
2. every `labelKey`,
3. **the ENTITY NAME itself** — it is the aggregate's `ContextName`, rendered as the
   `context` label of every error envelope; omit it and the label falls back to the raw
   type name untranslated (`bootstrap.html`).
Reuse a labelKey (and the entity-name entry) when the field lands on a shared base —
don't duplicate translations across roles.

## Authorization inside the domain (Layer 2, optional)

Runtime-only fields on the entity (not persisted — no labelKey, no schema entry), fed by
the Command mapper from `ctx.Identity()`, read in `BuildRules` under the verb's
`actionName` for owner-checks (403 via a forbidden-semantic notification). Only when a
verb carries an authorization rule — see `authz-seams.html`.
