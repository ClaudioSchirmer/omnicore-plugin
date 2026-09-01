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

**A child entry's wire shapes belong to no single operation** — `<Child>Row`,
`<Child>Request` and `<Child>Response` are carried by the insert, the update, both reads
and every per-entry verb — so they sit in `internal/web/requests/dtos/<child>.go`, one file
per collection, imported as `webdtos`. Everything else stays one file per operation, the
GraphQL variant of a verb included: it is the same operation on another surface, and
splitting it puts one endpoint's wire surface in two places.

## The Mount signature — layering rule

The repo parameter is the **interface** (`persistence.ScopedRepository[*T]`, plus the
`domain.Service` param only when the entity requires one — no dead nil), never the concrete
infra type: `internal/web` must not import `internal/infra` (`architecture.html`), and the
interface is what lets this layer compile before infra exists.

## Traps

- **`path:"id"` on a by-id request = boot panic** — the primary `:id` is auto-bound; only
  EXTRA path segments declare `path:"…"` (`:gradeId` → `path:"gradeId"`).
- **The `?fields=` guard is RECURSIVE, and it fires TWICE**: a list request declaring
  `Fields` forces every response field — including every field of every NESTED response
  type — to be pointer/slice + `omitempty`, and a second guard forces the same discipline
  on the query's RESULT (`application.md`). Either one unmet is a boot panic, invisible to
  `go build`.
- **Children BELONG in the list response — dropping them buys NOTHING.** On both backings
  the full aggregate is already in hand when the list is served (a Mongo view returns the
  whole projected document; a relational view loads the whole aggregate through the same
  loader) — omitting children discards fetched data, saves zero IO, and forces the client
  into N+1 by-id calls. Mirror the child collections as nested slices, exactly like the
  by-id response (`auto-query-handlers.html`'s `?fields=` example is a LIST response with
  a nested child array); the recursive `?fields=` guard is satisfied by making every
  nested response type — and its Result twin — pointer/slice + `omitempty` (previous
  trap), never by amputating the shape. A child-less listing exists only if the dev explicitly asked for one in the
  spec.
- **Every SCALAR request/response field carries an `example:` tag** — omit it and Swagger's
  "try it out" renders garbage placeholders. STRUCTURED fields (a struct, slice or map — not
  to be confused with a composite VALUE OBJECT, whose parts arrive here as plain scalars and
  DO carry one) must NOT
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
- **A COMPOSITE value object is FLAT on the wire — never a nested object.** Its parts are
  ordinary fields here, one per part, named exactly as the schema exposed them
  (`salaryAmount`, `salaryCurrency`), each typed by the part's own underlying scalar. That
  is not a simplification: the read side never learns a composite exists, so a nested wire
  shape would be a hand-built layer with nothing behind it — the filter, `?fields=`,
  `orderBy`, the export column and the projected document all speak the flat names. Every
  part of an OPTIONAL composite is a POINTER on the wire, because the value object is absent
  as a whole.
- **BOTH sides project via `FromResult` — the read has the write's anatomy.** A write
  Response maps from the command's Result; a read Response maps from the QUERY's Result
  (`application.md`). A raw view document reaches no transport any more, so there is no
  doc projector to key by hand. Bodyless verbs still use the no-body responder (204).
- **The generic mappers are OPT-IN, and the marker is a claim you are making.** A
  Response that embeds `fwresponses.Auto` gets `AutoFromResult`; a Request that embeds
  `fwrequests.Auto` gets `AutoFromRequest`, and neither helper COMPILES without the embed
  — the constraint is a sealed interface the framework grants only through it. Embed it
  when the two shapes are name-aligned (which is the normal case: the wire name and the
  Go name come from one declaration). Leave it off and write the body by hand when the
  seat exists precisely to rename, flatten or fold — that is what the escape hatch is for,
  not a fallback.
- **With the marker, the pair is checked at BOOT, in both directions.** Every one of the
  five route constructors validates it: names must align AND each mapped pair must be
  directly assignable, or the mount panics naming the field. The rule reads by LAYER — a
  type in `web/` (Request, Response) must be FULLY connected, because a wire field with no
  counterpart either renders null forever or has its value dropped in silence; a type in
  `application/` (Command, Result) may carry MORE, which is how a Command holds its path
  id or an identity overlay and how a Result holds a field the Response deliberately cuts
  off the wire. A Result carrying `json` tags is refused outright, marker or not — wire
  naming is the Response's job.
- **A NESTED wire type carries no marker.** The marker rides the type at the TOP of a
  walk — the one the constructor is handed. A child's entry type is reached as an element
  of the parent's collection, and the mapper recurses into it by field name; embedding the
  marker there would claim a check nothing performs.
- **A read Response is the SINGLE wire authority, on every surface at once.** REST,
  GraphQL and the CSV/XLSX export all render exactly the fields it declares, under its
  `json` names — a field outside it exports nowhere, and `?fields=` speaks that one
  vocabulary, so a selection valid on `GET /things` is valid on `GET /things.csv`. This is
  the trap worth stating: a business column present in the view and absent from the DTO
  used to reach the export and no longer does.
- **`exportLabelKey:"<catalog key>"` is where an export COLUMN HEADER comes from** —
  translated per request language, falling back to the json name. It rides the Response
  (recursively — a nested row's fields need their own), because the Response is what the
  export projects. Reuse the same catalog key the schema's `labelKey` uses so the two
  converge instead of drifting into two translations of one word.
- **A by-id read takes its criteria the same way the listing does:
  `ToQuery(criteria queries.ReadCriteria)`.** The wire vocabulary is smaller — exactly one
  reserved control, `includeArchived`, still honored only when the Request DTO declares
  it and 400 otherwise — but the SEAT is identical, so the query stores the criteria and
  its `ToCriteria` returns it. Do not re-read the DTO's `*bool` by hand; that seat is gone.
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

**The taxonomy is the DEV's decision** — what follows is the default to PROPOSE at the
spec gate when nothing else is on record, never a shape to impose. **The ACTION spells the
OPERATION** — `:insert`, `:update` for both PUT and PATCH,
`:delete`, `:archive` for both archive and unarchive, `:read`; a field-level restriction
extends the same word (`:read-grade`), it does not invent a new one (`:view-…`). Two
operations deliberately share an action: PUT and PATCH are one update, and unarchive is the
undo of archive, so whoever may archive may put it back. Synonyms are the whole problem —
`create` on one entity and `write` on the next means a deployment grants three words for one
verb. **Two things outrank it**: a taxonomy the project already grants — match it, in its own
language — and the dev preferring another spelling, which they owe no reason for. A
permission is compared exactly against the token, so the only wrong answer is one nothing
grants.

## GraphQL

One cumulative registry for the whole service — **built by the framework**, handed to
each feature that opts in by implementing `bootstrap.GraphQLFeature`
(`MountGraphQL(reg, deps)`); the registry is never created in `bootstrap/` and there is
no `Wiring` field for it (`bootstrap.html`). Reuses the exact same handlers as REST —
never a second implementation; each field carries the same permission as its REST twin.
Constructors/Relay semantics: `graphql.html`.
