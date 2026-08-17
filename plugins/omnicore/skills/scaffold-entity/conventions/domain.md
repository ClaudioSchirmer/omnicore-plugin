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
  names live on the web-layer DTOs (`internal/web/requests/` — request+response
  co-located per operation; `internal/application/dtos/` holds child-collection INPUT
  DTOs only, never wire types); physical column names live only
  in the infra schema. Worse than redundant: a `json:"-"` on a persisted field would
  break the `Old()` snapshot the framework builds via a json round-trip — and a custom
  `json.Marshaler` on the entity is the SAME trap by another door (it hijacks that
  round-trip). Both are caught LOUDLY: boot panic at `WithSchema` naming the offender
  (`old-state.html`). Domain structs carry `labelKey` only.
- A **flat** entity does NOT implement `domain.AggregateRootProvider` — that is the
  children delta.

## Value objects — the DEFAULT for any validated field, never inline the rule

**The decision rule (inverted on purpose): a field that needs ANY validation beyond
presence/nullability — a format, a length/range bound, a closed set — IS a value object, by
DEFAULT.** Inline in `BuildRules` stays for exactly two cases: a pure-presence rule
(required non-empty) and a cross-field invariant **between fields that are not one concept**
(a rule spanning the entity's own unrelated fields). "Only one aggregate
carries it today" is NOT a reason to inline — a VO is single-responsibility + one home for
the rule, not merely reuse. Model it as a VO type in `internal/domain/vos/`, never a bare
`string`/`int` with the check re-written in `BuildRules`.

**And a value object is NOT limited to one field.** When two or three fields only mean
something together — `Money{Amount, Currency}`, `Period{From, To}`,
`Address{Street, City, ZipCode}` — they are ONE value object spanning several columns, not N
loose fields with a hand-written rule between them. That is the **composite** kind below.
The old workaround (flatten the fields onto the entity and force the rule into `BuildRules`)
is the shape to stop writing: the rule leaves the concept it belongs to, and nothing stops a
second entity from re-deriving it differently.

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

**Which kind — the three VO shapes.** Two questions decide, in order: *how many fields does
the value occupy* (one → raw/enum, several → composite), then *who owns the rule* (the type
writes it → raw, the framework checks membership → enum).

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
- a value that spans SEVERAL fields — `Money{Amount, Currency}`, `Period{From, To}`,
  `Address{Street, City, ZipCode}`: a **composite value object**. A plain struct in
  `vos/` that declares `IsValid` and **no `Value()`** — that absence IS the discriminator, so
  a composite may never declare one (expose a canonical rendering as `String()` instead). It
  is where a CROSS-FIELD rule belongs ("the end may not precede the start"), which is the
  thing a single-scalar VO cannot express and the reason the kind exists. A part may itself
  be a raw or enum VO (`Money.Currency`) — and the composite validates that part from inside
  its own `IsValid` (`domain.ValidateEnum(v.Currency, "Currency", ctx)`), because the
  framework's automatic pass validates the COMPOSITE, never its interior. Nesting a
  composite inside a composite is not modelling: that is an entity in disguise.
  - **Mandatory or optional as a WHOLE.** Held by value → the value object is always there,
    and each part follows its own Go type. Held as a `*Period` → the whole thing is optional:
    every part column NULL reconstructs as `nil`, any part carrying a value makes it present,
    and a NULL on a non-pointer part is then a half-written row and a loud error.
  - **The domain declares nothing extra.** The struct owns its rule and that is all it knows;
    which columns it occupies is declared ONCE, in the `TableSchema` (see infra.md), and
    nothing downstream — criteria, audit, the projection, the read DTO, filters, `orderBy`,
    `?fields=`, OpenAPI, GraphQL, gRPC, the exports — ever learns a composite exists.

**Validation is AUTOMATIC — the part a code-first reflex trips on.** You never call a VO's
validation by hand. The framework discovers every VO-typed field by reflection and
validates it on every write, IDENTICALLY on a root AND on an aggregate value object (a
`nil` pointer field is skipped as absent). So `BuildRules` carries ONLY the non-VO rules (a
required plain `string`, a cross-field invariant); an Email/ZipCode/enum field has nothing
to wire. **That includes PRESENCE — never add a required rule for a VO-typed field.** A
string-backed raw VO reports an empty value as `RequiredFieldNotification` from inside its
own `IsValid` (the canonical shape in `value-objects.html`), and an enum answers `""` with
its unknown-member notification, since `""` is not a member. A required rule beside either
one makes the caller receive the SAME complaint twice for one empty field — the failure a
run here actually shipped. Presence on a VO field is the VO's job; `BuildRules` carries
what the VO cannot see. **A COMPOSITE field is discovered and validated by the same pass** —
nothing registers it, and a `nil` optional composite is skipped entirely, because absence is
not a violation. To opt one out in a mode, `r.IgnoreValueObject("Field")` inside that mode's gate;
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

- **`Modes()` → the schema's archive-column declaration must exist** — `ModeArchive`
  without the declared column panics at repo construction. (The reverse is legal:
  `DeletedAt(col)` with no archive verb boots fine.)
- **Archive/unarchive have their OWN clauses — `IfArchive`/`IfUnarchive`** (gate on
  ModeArchive/ModeUnarchive). `IfUpdate` is PUT/PATCH exclusively; a rule left in `IfUpdate`
  will NOT fire on an archive transition. `actionName` is a free-form label, never a verb
  selector. Full clause set: `rules-dsl.html`.
- **`domain.Old(e)` is nil on Insert** — guard before dereferencing.
- **ONE clause per verb, holding every rule that runs on it. Not one clause per rule.**
  This is the single most common readability failure in a hand-written `BuildRules`: two
  rules that both run on insert, each wrapped in its own `IfInsert`. The result is a file of
  near-identical wrappers where the reader has to diff the closures to find the rules, and
  the framework runs the verb check once per block on every write.
  Write the block once and put both rules inside it, separated by a blank line and a
  `// ── <rule-id> ──` header — the same shape the codegen path's hook file is generated
  with. It is what a reader expects: all of a verb's invariants in one place, in reading
  order. Repeating a clause is *legal* (`IfInsertOrUpdate` just runs its closure each time —
  nothing is registered, stored, or overwritten), which is precisely why nothing catches it
  for you. Keep all of a field's rules (required / immutable / unique) adjacent inside the
  block, and if one rule genuinely runs on two verbs, write it as a method on the entity and
  call it from both clauses rather than pasting it twice.
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
  `Semantic()` per notification for conflict (409) / forbidden (403). **409 is TWO
  semantics — pick the right one**: duplicate/already-exists → the Conflict semantic
  (gRPC `ALREADY_EXISTS`); wrong-state ("cannot ship a cancelled order") → the
  STATE-conflict semantic (gRPC `FAILED_PRECONDITION`). Mechanics + exact names:
  `status-mapping.html`.
- Unique-field conflicts are custom `<Field>AlreadyExists…` notifications with the
  conflict semantic — `EntityAlreadyAddedNotification` is the framework's PK-collision
  one, not a substitute.

## Translations — ALL SEVEN, always

Every key in all 7 catalogs (`ptbr,eng,esp,fra,deu,ita,nld`), each a REAL translation —
never English copied seven times, never a subset, and **NEVER an elicitation question**.
Four key kinds per entity:
1. every custom notification name,
2. every `labelKey`,
3. **the ENTITY NAME itself** — it is the aggregate's `ContextName`, rendered as the
   `context` label of every error envelope; omit it and the label falls back to the raw
   type name untranslated (`bootstrap.html`),
4. **(optional) enum VALUE keys** — each member CAN render per-locale via
   `EnumDescriptionKey(v)` (`"<Type>.<value>"`), registered in the catalogs like any
   other key (`value-objects.html`). Nothing on the scaffolded path calls it — the wire
   DTOs carry the raw scalar — so register the N value keys only when the spec asks for
   human-readable enum labels, not as a blanket rule.
Reuse a labelKey (and the entity-name entry) when the field lands on a shared base —
don't duplicate translations across roles.

## Domain events (optional — mention when a rule wants a reaction)

An aggregate method may `RegisterEvent(...)` a domain event on the `BaseEntity`; the
framework auto-publishes it POST-COMMIT on both the Auto and manual handler paths
(Slog publisher by default — swap the publisher, don't re-plumb). In-process and
per-aggregate — distinct from broker-carried integration events (`shared/capabilities.md`
owns that split). Not scaffolded by default; when the spec's rules describe "and then
notify/react", name this seam instead of inventing one (`auto-handlers.html` ·
`command-handler.html`).

## Authorization inside the domain (Layer 2, optional)

Runtime-only fields on the entity (not persisted — no labelKey, no schema entry), fed by
the Command mapper from `ctx.Identity()`, read in `BuildRules` under the verb's
`actionName` for owner-checks (403 via a forbidden-semantic notification). Only when a
verb carries an authorization rule — see `authz-seams.html`.
