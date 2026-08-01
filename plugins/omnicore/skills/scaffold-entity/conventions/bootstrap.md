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
- **Relational view ⇒ reuse `repo.Loader`, never a second loader.** When the entity's
  view is relational (`.RelationalSource(...)`, per the read-side posture), build the
  aggregate repo ONCE in the feature constructor, store it in the `repo` field, and pass
  that SAME `repo.Loader` into the view's constructor. Do NOT call the aggregate-loader
  constructor a second time: a duplicate loader for the same aggregate boots fine and
  reads correctly, so the boot guard (which catches a WRONG-aggregate loader via
  `BoundTable()==schema.Table()`, not a redundant duplicate) will NOT flag it — it is
  pure waste the doc's feature example is written to avoid. See the `NewGadgetFeature`
  example in `relational-view.html`.
- **Service-backed entity:** build the infra Service impl in the feature constructor and
  pass it through `Mount<Entity>`'s `svc` param; a no-service entity OMITS the param
  entirely (no dead nil) — the pairing is enforced at runtime (application.md).
- GraphQL/gRPC mounts are called explicitly by `Wire` on each feature into the single
  shared registries (they are not `Feature` interface methods); `MountGRPC` belongs to the
  gRPC skill.

## wire.go — the single registration site

Adding an entity means INSERTING (never creating a file): (1) instantiate the feature;
(2) contribute its GraphQL fields to the one registry; (3) add it to the `Features` slice
(declaration order = the Swagger sidebar order); (4) on a first entity, also wire the 7
translation catalogs.

## Boot order (context)

`Wire(deps)` → migrations (down-validation → framework → service) → features' `Views()`
feed the SyncEngine → features' `Mount()` register routes → serve. A broken schema/view
surfaces at boot, before serving — which is why the mechanical checklist runs before
declaring done. See `bootstrap.html`.
