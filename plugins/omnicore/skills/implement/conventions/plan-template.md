# conventions/plan-template.md — the Phase 1 capability plan skeleton

Copy VERBATIM to `specs/implement/<slug>/plan.md`, then fill. Rules: every section stays
(inapplicable → `N/A — <why>`); a decision only the dev can make is `⚠️ OPEN:
<question>`; high-risk picks carry `(proposed)` + the alternative(s) beside them; NO
code — code shapes live in the routed `/docs` sections cited in §2. Approval flips
`Status` to `APPROVED`; no edit before that.

---

# Capability plan — <slug>

- **Status:** DRAFT
- **Framework pin:** <from `go.mod` — informative; that pin's docs are the authority>

## §1 The request (restated)

> <the dev's ask, verbatim>

<one paragraph: what will exist when this is done, in the run's language>

## §2 Routing evidence — the owning docs

| Capability piece | Owning section(s) at this pin | Existence check |
|---|---|---|
| <piece> | `<name>.html` — <the contract it owns> | features/reference · changelog (newer pin) |

Routing outcome: **offered at pin** · **offered at <newer version> only → upgrade
offered (accepted/declined)** · **offered but the current posture lacks its infra →
`/omnicore:configure` offered (accepted/declined); the missing infra named
(broker · relay/tailable engine · Mongo — per `shared/capabilities.md`'s three
axes)** · **not offered → honest-no path: <what was recommended
instead>**. A plan line with no §2 row behind it is a defect.

## §3 Integration semantics [high-risk — propose + CONFIRM]

- **Seam:** <in-TX lifecycle hook · after-commit integration event · middleware ·
  surface registration · …> (proposed — why, and the alternative)
- **Sync or async** relative to the command/query flow: <…>
- **Failure policy:** ⚠️ OPEN unless the dev already said it — external dependency
  down/slow ⇒ reject? degrade? queue? (timeouts/retries proposed from the docs'
  defaults)
- **Idempotency / replay:** <what happens on retry, redelivery, double-fire> — for an
  event RECEIVER this is ⚠️ OPEN by default: delivery is at-least-once, the strategy is
  the dev's design duty (named patterns in `integration-events` at the pin;
  `shared/capabilities.md`)
- **Cache slots (caching only):** ⚠️ OPEN — private (`Deps.Cache`) or shared
  (`Deps.SharedCache`)? which backend? (`shared.store: memory` is a boot reject;
  `shared/capabilities.md`) — never silently pick. Carve-out: the httpclient RESPONSE
  cache has no slot to elicit (`Deps.Cache` unconditionally — declare the `cache:`
  block or the layer bypasses); this bullet is then `N/A — httpclient response cache`
- **Wire/API impact:** <new surface = new public contract; anything breaking for
  existing consumers is listed here, flagged, dev decides>

## §4 External contract (integrations only)

Source of truth for the external API: <dev-provided spec / doc link — NEVER invented>.
Auth model, environments (sandbox/prod), and who owns the credentials. `N/A — no
external system` otherwise.

## §5 Impact map — every artifact touched

| Artifact | Change | Owning doc section |
|---|---|---|
| `microservice.*.yaml` (ALL boot profiles) | <keys added> | <section> |
| infra prerequisite (when §2's outcome named one) | <what `configure` enables first — or `N/A`> | `shared/capabilities.md` |
| build/run commands | <tag set after this change — e.g. a first consumer adds the transport tag; `N/A — unchanged`> | `shared/boot-contract.md` (Build tags) |
| bootstrap/wiring | <…> | <section> |
| <handlers/middleware/proto/views/…> | <…> | <section> |
| notification type(s) + the seven translation catalogs | <each type, the BASE it embeds and the layer it is declared in, its `Semantic()` — or `N/A — this capability raises no typed rejection`> | `status-mapping` · `shared/notification-bases.md` |
| tests | <new branches covered> | — |

**Every row names the file PATH it touches**, taken from `service-layout.html` at the pin —
a row that names only an artifact kind ("the handler", "the notification") has not decided
where it lands, and the default an agent reaches for is the wrong layer. A notification
raised by a HANDLER is an application notification declared under `internal/application/`;
it does not join the aggregate's `internal/domain/notifications.go`
(`shared/notification-bases.md` is the owner of that decision, and of the question that
rides along with it — a persisted table keeps its domain struct even with an empty
`BuildRules`).

Phase 2 edits ONLY these rows, in dependency order (config → wiring → artifacts →
tests).

## §6 Config & secrets

Keys per profile, env placeholders per the configuration reference; secrets are env
refs, never literals. Which profiles boot in dev/QA/prod and got the keys.

## §7 Verify step — how this will be PROVEN

<the concrete proof: the surface answers a real call · cache hit observed on repeat ·
event lands on the broker · sandbox call succeeds — plus what CANNOT be proven locally
and the exact step the dev must run to close it>
