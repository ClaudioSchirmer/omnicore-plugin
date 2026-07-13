# conventions/domain.md — the domain layer (flat / base case)

> NO code here, by design. Layout/naming/granularity: `service-layout.html`. API mechanics
> (signatures, DSL, behavior): the routed `/docs` sections — reading them before generating
> this layer is MANDATORY. This file carries only skill-level process, decisions and traps.

Covers a **flat** entity's domain. Deltas load separately: children →
`aggregate-children.md` · SharedBase → `sharedbase.md` · siblings → `siblings.md`.

Docs for this layer: rules DSL → `rules-dsl.html` · notification→status →
`status-mapping.html` · `Old()` → `old-state.html` · authz → `authz-seams.html`.

## Files

Per `service-layout.html`: one domain type per file (root, each VO, the service port in its
own `<entity>_service.go`); `notifications.go` is the single shared home of every custom
notification. Both `notifications.go` and the translation catalogs are **registration
sites** — existing files you APPEND to (like `wire.go`), never per-entity copies, never
regenerated.

## The aggregate struct — decisions

- Nullable ⇒ pointer; money = `int64` minor units, never float; every persisted field
  carries a `labelKey`; **no `db:` tags** (physical names live only in the infra schema).
- A **flat** entity does NOT implement `domain.AggregateRootProvider` — that is the
  children delta.

## Modes() + BuildRules — traps the docs route you past

- **`Modes()` ⟺ `SoftDelete` must agree** — `ModeArchive` without a schema `SoftDelete`
  panics at repo construction (and vice-versa keep them in lockstep).
- **There is NO `IfArchive`/`IfUnarchive`** — archive/unarchive rules ride `actionName`
  (`"GetArchivable"`/`"GetUnarchivable"`) inside `IfUpdate`. Full actionName↔verb map:
  `rules-dsl.html`.
- **`domain.Old(e)` is nil on Insert** — guard before dereferencing (transition rules run
  under `IfUpdate`).
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
