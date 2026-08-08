# conventions/web.md — the web layer (flat / base case)

> NO code here, by design. Layout/naming/granularity: `service-layout.html`. API mechanics
> (route constructors, wrappers, control keys): the routed `/docs` — reading them before
> generating this layer is MANDATORY. This file carries only skill-level process, decisions
> and traps.

This skill scaffolds **REST + GraphQL**; the gRPC surface is `/omnicore:implement`'s job
(needs a proto contract — leave its slot in the routes file and point the dev there).

Docs for this layer: route wrappers + strict body → `auto-handlers.html` · OpenAPI specs +
Mount → `openapi.html` · read control keys + filter operators + export →
`auto-query-handlers.html` · GraphQL surface → `graphql.html`.

## Files & placement

Per `service-layout.html`: wire DTOs one file per OPERATION (request+response co-located);
all of an entity's surfaces in ONE `<entity>_routes.go`; the shared-identity read surface
(the identity view) in its OWN routes file + feature + permission — never inside the role's.
**Routes never live in `bootstrap/`** — the feature's Mount is a one-line delegation to
this layer.

## The Mount signature — layering rule

The repo parameter is the **interface** (`persistence.ScopedRepository[*T]`, plus the
`domain.Service` param only when the entity requires one — no dead nil), never the concrete
infra type: `internal/web` must not import `internal/infra` (`architecture.html`), and the
interface is what lets this layer compile before infra exists.

## Traps

- **`path:"id"` on a by-id request = boot panic** — the primary `:id` is auto-bound; only
  EXTRA path segments declare `path:"…"` (`:gradeId` → `path:"gradeId"`).
- **The `?fields=` guard is RECURSIVE**: a list request declaring `Fields` forces EVERY
  response field — including every field of every NESTED response type — to be
  pointer/slice + `omitempty`, or boot panics (invisible to `go build`).
- **Children BELONG in the list response — dropping them buys NOTHING.** On both backings
  the full aggregate is already in hand when the list is served (a Mongo view returns the
  whole projected document; a relational view loads the whole aggregate through the same
  loader) — omitting children discards fetched data, saves zero IO, and forces the client
  into N+1 by-id calls. Mirror the child collections as nested slices, exactly like the
  by-id response (`auto-query-handlers.html`'s `?fields=` example is a LIST response with
  a nested child array); the recursive `?fields=` guard is satisfied by making every
  nested response type pointer/slice + `omitempty` (previous trap), never by amputating
  the shape. A child-less listing exists only if the dev explicitly asked for one in the
  spec.
- **Every SCALAR request/response field carries an `example:` tag** — omit it and Swagger's
  "try it out" renders garbage placeholders. Composite fields (struct/slice/map) must NOT
  carry one — that is a boot reject; whole-body samples go through `Doc.RequestExamples`/
  `Doc.ResponseExamples` (`openapi.html`). Low-risk: decide plausible values yourself.
- **Strict vs lenient is decided per operation by its FIELDS** — any optional field ⇒ the
  lenient handler, on child ADD exactly as on UPDATE (a strict add 400s an omitted
  optional); all-required (especially numeric — a missing number defaults to 0 and can
  slip range rules) ⇒ strict.
- **Read controls are DTO-GOVERNED and the control vocabulary is CLOSED.** A reserved
  read control is served ONLY when the list Request DTO declares it (`query:"onlyTotal"`,
  `query:"includeArchived"`, …): undeclared, its mere PRESENCE on the wire is a typed
  400 (GraphQL omits it from the SDL; gRPC answers INVALID_ARGUMENT) — so declare
  exactly the controls the spec wants served, nothing silently. And the set is closed
  at BOOT: a top-level `query:`-tagged scalar with no `filter:` tag whose key is not
  one of the canonical controls (`auto-query-handlers.html` owns the list) panics at
  wrapper construction — a typo (`query:"orderby"`) or a stale spelling
  (`query:"limit"`) fails loud instead of advertising a dead parameter in OpenAPI.
  Invisible to `go build`, like every boot guard.
- **Read-request query params default to OPTIONAL — pointer fields.** Every scalar
  `query:`-tagged field on a read request (filters, pagination keys, the reserved
  archived-visibility flag included) is optional by default: declare it a pointer and
  resolve absent → its default at the boundary mapper. A filter is REQUIRED only when the
  dev explicitly says so in the spec — then the non-pointer IS the honest contract. Why it
  bites: the OpenAPI generator marks any non-pointer field required (`openapi.html`,
  required-field rule), so one accidental value-typed flag turns an optional parameter
  mandatory and Swagger refuses the call until it is set. The required→value /
  optional→pointer ruler below is for COMMAND bodies; on reads it only encodes an
  EXPLICIT spec decision. (Struct-typed filter GROUPS are namespaces, not params — see
  `auto-query-handlers.html`.)

## Boundary rules

- `ToCommand()` is body-only — NO ctx (identity enters at the Command layer); request ≡
  command shape (required → value, optional → pointer), 1:1, no normalization.
- Wire DTOs carry the VO's UNDERLYING scalar (`string`/`int`/pointer) — a request field
  for a `vos.Email`/`vos.Ethnicity` is a plain `string` (int enum → plain
  `int`). The wire→VO cast is the Command's job (`application.md`); keep `ToCommand`/
  `FromResult` raw 1:1 and don't import `vos` into `web/`. This is THIS SKILL's
  convention (layering hygiene), not the framework's limit: the framework also accepts
  a RESPONSE DTO field typed as the VO itself (both render natively on every surface;
  OpenAPI/SDL describe it by the underlying type) — so never flag VO-typed response
  fields in existing consumer code as misuse; just don't generate them.
- Write responses project via `FromResult`; reads via the framework's doc projector keyed
  by Go field name; bodyless verbs use the no-body responder (204).
- Filter operators are AI-chosen per field type (strings: eq/ne/in/startswith/contains +
  i-variants; numbers/dates: + gt/gte/lt/lte) — low-risk, decide and show in the spec.
- Exports (when the spec asks) mount at the APP ROOT (`/<entities>.csv`), not under the
  group — path collision with `/:id` otherwise.

## Authorization (Layer 1) — one decision, every surface

`RequirePermission("<resource>:<action>")` on every registration; by handler invariance
the SAME permission attaches at each surface's unit (REST route · GraphQL field · gRPC
procedure) — decide once per operation, apply on all. **This is boot-FATAL, not
convention**: under `auth.authorization.enabled: true` a sweep panics at boot listing
every non-public route missing `RequirePermission`, and a parallel scan panics on any
route registered outside Mount/MountRaw while OpenAPI is on (`authz-seams.html`). Taxonomy shape per
`service-layout.html`; granularity is a modeling choice confirmed at the spec.

## GraphQL

One cumulative registry for the whole service — **built by the framework**, handed to
each feature that opts in by implementing `bootstrap.GraphQLFeature`
(`MountGraphQL(reg, deps)`); the registry is never created in `bootstrap/` and there is
no `Wiring` field for it (`bootstrap.html`). Reuses the exact same handlers as REST —
never a second implementation; each field carries the same permission as its REST twin.
Constructors/Relay semantics: `graphql.html`.
