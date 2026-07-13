# conventions/web.md — the web layer (flat / base case)

> NO code here, by design. Layout/naming/granularity: `service-layout.html`. API mechanics
> (route constructors, wrappers, control keys): the routed `/docs` — reading them before
> generating this layer is MANDATORY. This file carries only skill-level process, decisions
> and traps.

This skill scaffolds **REST + GraphQL**; gRPC is a separate skill (needs a proto contract —
leave its slot in the routes file).

Docs for this layer: route wrappers + strict body → `auto-handlers.html` · OpenAPI specs +
Mount → `openapi.html` · read control keys + filter operators + export →
`auto-query-handlers.html` · GraphQL surface → `graphql.html`.

## Files & placement

Per `service-layout.html`: wire DTOs one file per OPERATION (request+response co-located);
all of an entity's surfaces in ONE `<entity>_routes.go`; the shared-identity read surface
(person view) in its OWN routes file + feature + permission — never inside the role's.
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
- **Idiomatic list shape**: list responses carry ROOT scalars only; child collections nest
  in the by-id response (which has no `?fields` contract) — that is what keeps the
  recursive guard from biting.
- **Every request/response field carries an `example:` tag** — omit it and Swagger's
  "try it out" renders garbage placeholders. Low-risk: decide plausible values yourself.
- **Strict vs lenient is decided per operation by its FIELDS** — any optional field ⇒ the
  lenient handler, on child ADD exactly as on UPDATE (a strict add 400s an omitted
  optional); all-required (especially numeric — a missing number defaults to 0 and can
  slip range rules) ⇒ strict.

## Boundary rules

- `ToCommand()` is body-only — NO ctx (identity enters at the Command layer); request ≡
  command shape (required → value, optional → pointer), 1:1, no normalization.
- Write responses project via `FromResult`; reads via the framework's doc projector keyed
  by Go field name; bodyless verbs use the no-body responder (204).
- Filter operators are AI-chosen per field type (strings: eq/ne/in/startswith/contains +
  i-variants; numbers/dates: + gt/gte/lt/lte) — low-risk, decide and show in the spec.
- Exports (when the spec asks) mount at the APP ROOT (`/<entities>.csv`), not under the
  group — path collision with `/:id` otherwise.

## Authorization (Layer 1) — one decision, every surface

`RequirePermission("<resource>:<action>")` on every registration; by handler invariance
the SAME permission attaches at each surface's unit (REST route · GraphQL field · gRPC
procedure) — decide once per operation, apply on all. Taxonomy shape per
`service-layout.html`; granularity is a modeling choice confirmed at the spec.

## GraphQL

One cumulative registry for the whole service (created in bootstrap; each entity's
`Mount<Entity>GraphQL` contributes fields). Reuses the exact same handlers as REST — never
a second implementation; each field carries the same permission as its REST twin.
Constructors/Relay semantics: `graphql.html`.
