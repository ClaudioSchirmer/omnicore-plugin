# shared/notification-bases.md — which LAYER declares a rejection, one owner

The single home for one decision: **when this service refuses a request, where does the
notification type live?** Every skill that writes a rule, a hand-written handler, an
adapter or a capability routes HERE instead of guessing. No code here, by design —
knowledge and decisions only; the exact API is the PIN's (`status-mapping`, and the
placement table in `service-layout`).

This file exists because the default answer an agent reaches for is wrong often enough to
be a pattern: **`internal/domain/notifications.go` is not the home for every notification a
service declares.** It is the home for the ones the AGGREGATE raises.

## The three bases

A notification type embeds a base from the framework's `domain` package. There are three,
one per layer that can author a rejection:

| Base | Raised by | Declared in |
|---|---|---|
| `domain.DomainNotificationBase` | a `BuildRules` rejection · a value object's `IsValid` · an aggregate invariant | `internal/domain/notifications.go` — or `vos/` / `aggregatevos/` for what those packages emit |
| `domain.ApplicationNotificationBase` | a hand-written command/query handler — an orchestration precondition, a cross-resource check the aggregate has no business knowing, an outbound failure translated into this service's vocabulary | `internal/application/`, beside the handler that raises it (`commands/handlers/` · `queries/handlers/`; a `handlers/notifications.go` once there is more than one) |
| `domain.InfrastructureNotificationBase` | an adapter — a fixed upstream→semantic mapping the adapter owns as part of its contract | `internal/infra/`, beside that adapter |

All three embed the same kernel base, so they seal the interface identically and carry the
same default `Semantic()` (validation → 422), and **no framework code branches on which
one was picked.** Nothing breaks at compile time or at boot if the wrong one is used —
which is precisely why the decision has to be made deliberately rather than discovered.

## The rule

**The base names the layer that RAISES the notification, and the type is declared in that
same layer.** Never one layer's base on a type declared in another. The framework holds
itself to it — its domain kernel notifications, its application kernel and its migration
failures each sit in their own layer on their own base — and the invariant is stated in the
framework's own contributor rules, which the consumer-facing skills do not otherwise read.

Two consequences worth stating, because both have been gotten wrong:

- **A handler-validated endpoint declares its rejections in `application/`.** When the
  request is checked by the handler rather than by `BuildRules`, the aggregate never raises
  that rejection — so the domain must not declare it. Pushing it into
  `internal/domain/notifications.go` "to keep the notifications together" makes the domain
  package name a concern only the handler has, which is the boundary the architecture
  depends on.
- **The wire does not change.** The notification KEY is the struct name whatever base it
  carries, so the translation key, the `notificationKey` clients branch on, and the status
  the `Semantic()` selects are identical either way. Placement is the only thing at stake —
  and it is the thing a reviewer, a `remove-entity` run and the next agent all read.

## The adjacent trap — placement is not the same question as "does this need a domain type"

Moving a notification out of the domain does NOT mean the endpoint has no domain artifact.
Ask the two questions separately:

- **Does a persisted table need a domain struct? YES, when the framework's AGGREGATE
  repository writes it.** The write-backed schema is type-anchored over a Go type, and
  binding a type-less schema to a repository is a boot abort, not a style opinion. A
  resource whose rules are not domain rules gets a domain struct with an **empty
  `BuildRules`** — that is the legitimate shape, not a workaround. Confirm the exact
  contract in the pin (`table-schema`, `custom-command-handler`) before writing it.
  - **The carve-out, on a pin that carries it:** a table with no aggregate behind it at all
    — a control table, a job queue, a lookup — is written through a **Direct schema**, and
    its row is a storage shape declared in `internal/infra/`, not a domain struct. That is
    the one persisted type this rule does not claim. `${CLAUDE_PLUGIN_ROOT}/shared/direct-schema.md`
    owns which door a given table goes through and how to test whether the pin has it; the
    paragraph above is what remains true when the answer is the aggregate one, which is
    still every resource that is listed, audited, projected or lifecycle-driven.
- **Does it need domain NOTIFICATIONS? Only if the aggregate raises any.** An empty
  `BuildRules` raises none, so that entity contributes no `notifications.go` entries at
  all, and every rejection the endpoint can produce is an application notification.

An endpoint that reads or writes outside the framework's repository (a bespoke handler over
its own reads) is a different shape again — route it through `custom-command-handler` /
`custom-query-handler` at the pin, and its rejections are still application notifications.

## Duties that follow, whatever the layer

- **All seven translation catalogs get the key**, exactly as a domain notification would —
  the catalogs are keyed by struct name and know nothing about layers. A notification
  declared and never catalogued surfaces untranslated.
- **`Semantic()` is declared on the type**, from the framework's closed enum; the default
  is validation → 422. Picking the status is part of declaring the notification, not a
  later step (`status-mapping` at the pin).

## Pin note

The three bases are part of the framework's `domain` kernel and are not gated behind a
recent release — verify the identifiers in the pin's `domain` package if in doubt. What is
recent is the DOCUMENTATION of the choice: the `status-mapping` section only grew its
"Which base — the layer that raises it" subsection, and `service-layout` only grew its
application/infra placement bullets, in a later release. **On an older pin those sections
show the domain base alone.** That is a gap in that pin's docs, not a signal that the other
two bases do not exist or should not be used — this file is the authority for the placement
decision; the pin stays the authority for the API.
