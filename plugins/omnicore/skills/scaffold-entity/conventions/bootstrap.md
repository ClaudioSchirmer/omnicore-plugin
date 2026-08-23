# conventions/bootstrap.md — assembly: feature + wire

> NO code here, by design. Layout/naming: `service-layout.html`. The `Deps`/`Wiring`/
> `Feature` interfaces and boot order: `bootstrap.html` — reading it before generating
> this layer is MANDATORY. This file carries only skill-level process, decisions and traps.

## The feature file

Per `service-layout.html`: one feature per aggregate — holds that aggregate's repo + view
(built once, the single call site), contributes the view via `Views()` (feeds the
SyncEngine), and its `Mount` is a **ONE-LINE delegation** to the web layer's
`Mount<Entity>`.

- **❌ Route bodies in a feature file = the web layer leaked into the composition root** —
  the single most common structural failure. `bootstrap/` imports NO route constructors.
- **❌ SharedBase models: the identity view is NOT the role's cargo** — it gets its
  OWN feature, named after the BASE (holds the view, mounts the identity read routes),
  registered beside the role features. A role's feature carries the ROLE only.
- **A relational read model is contributed through its OWN seam** — the sibling of the
  one Mongo views use, and a different method on the feature (pin ≥ v0.57.0). It is a
  different KIND of read model, not a flag: nothing is materialised, so the sync engine,
  the drift detector and the rebuild have nothing to receive, and the type system is what
  keeps it away from them. A feature declaring both backings declares both methods.
- **Relational view ⇒ reuse `repo.Loader`, never a second loader.** Build the aggregate
  repo ONCE in the feature constructor, store it in the `repo` field, and pass that SAME
  loader into the view's constructor. Do NOT call the aggregate-loader constructor a
  second time: a duplicate boots fine and reads correctly, so nothing flags it — and it is
  not merely waste now, because the loader is what carries the schema AND any declared
  READ JOINS. A second loader is a view that quietly serves a different reach. See the
  `NewGadgetFeature` example in `relational-view.html`.
- **Service-backed entity:** build the infra Service impl in the feature constructor and
  pass it through `Mount<Entity>`'s `svc` param; a no-service entity OMITS the param
  entirely (no dead nil) — the pairing is enforced at runtime (application.md).
- **GraphQL/gRPC are FEATURE-DECLARED surfaces, never wired in `Wire`.** A feature
  opts in by implementing the opt-in interfaces (`bootstrap.GraphQLFeature` /
  `bootstrap.GRPCFeature` — `MountGraphQL` / `MountGRPC` methods), discovered by type
  assertion exactly like `ReadableFeature`; **the framework builds the one registry
  per surface** and hands it in. `wire.go` never constructs a registry and there is
  no `Wiring` field for these surfaces — read `bootstrap.html` before touching this.
  Wiring either surface for a new entity is `/omnicore:implement`'s job.

## wire.go — the single registration site

Adding an entity means INSERTING (never creating a file): (1) instantiate the feature;
(2) add it to the `Features` slice
(declaration order = the Swagger sidebar order); (3) on a first entity, also wire the 7
translation catalogs. (GraphQL/gRPC never appear here — they are feature-declared
interfaces, previous section.)

- **Catalogs and the Swagger language dropdown are ONE edit.** Wiring the catalogs is
  precisely what makes the dropdown renderable: `openapi.Config.LanguageSelector`
  (default `false`) opts in, and bootstrap then auto-populates the options from
  `Wiring.Translations` — with no translations the selector renders nothing, which is
  why a shell can look correctly configured and still show no combo. So in the SAME
  edit that adds the catalogs, if `Wiring.OpenAPI` is set WITHOUT
  `LanguageSelector: true`, flip it on. This is also the retrofit path for services
  scaffolded before their generator set it. See `openapi.html`.

## Boot order (context)

`Wire(deps)` → migrations (down-validation → framework → service) → features' `Views()`
feed the SyncEngine → features' `Mount()` register routes → serve. A broken schema/view
surfaces at boot, before serving — which is why the mechanical checklist runs before
declaring done. See `bootstrap.html`.
