# shared/domain-membership.md — what may live in `internal/domain/`, one owner

The single home for one decision: **this new type, port, interface or constant — does it
belong in the domain package?** Every skill that writes a type into a consumer service
routes HERE instead of deciding locally. No code here, by design — knowledge and decisions
only; the exact API and the layer table are the PIN's (`service-layout`, `architecture`,
`value-objects`). The sibling decision — where a REJECTION is declared — is owned by
`notification-bases.md`; this file is everything else.

This file exists because the answer an agent reaches for is "yes" far more often than the
answer is yes, and because the reasons it gives are always the same three. They are not
hypothetical: asked to explain a hashing port and two JWT claim names it had put in
`internal/domain/`, an agent named all three at once — least-resistance in the import
graph, a similar-looking file already there, and a consumer that did not exist yet. Each is
refuted below, by name, because a rule with no answer to the argument against it loses to
the argument every time.

## What lives there — the CLOSED list

| Path | Holds |
|---|---|
| `internal/domain/` (root) | the **aggregate roots** (`<entity>.go`, one per file) · the aggregate's own **ports** — its `domain.Service` (`<entity>_service.go`) and a repository port typed in the aggregate · `notifications.go` for what the ROOT raises |
| `internal/domain/vos/` | the **value objects** — raw, enum and composite (one per file) + this package's `notifications.go` + `doc.go` |
| `internal/domain/aggregatevos/` | the **aggregate value objects** (the children) + this package's `notifications.go` |

**Anything not on that list is not domain**, however domain-shaped its name reads. The list
is closed on purpose: "it felt like a business concept" is not a membership test, it is a
feeling, and it is the feeling that produced every misplacement this file exists to stop.

## The two questions

Both answerable by reading the declaration. Both YES → domain. **Either NO → not domain.**

1. **Is it expressed in THIS service's domain vocabulary?** Does the signature mention an
   aggregate, a child, a value object, `domain.ID`? A contract written entirely in
   primitives (`string`, `[]byte`, `bool`) is describing a MECHANISM, and a mechanism is
   never a domain concept — the give-away is that the type is named after how it works
   (`…Hasher`, `…Encoder`, `…Client`, `…Signer`) rather than after what the business calls
   it.
2. **Is the consumer domain code?** `BuildRules`, a method on the aggregate, a value
   object's `IsValid`, `RequiresService()`. If the only caller is a handler, a route or an
   adapter, the pin has already decided: *the interface stays with its consumer, never with
   its implementation* (`service-layout`, Domain layer). Consumer is the handler → it is
   declared where the handler is.

**Worked both ways, on real files.** The reference service declares a repository port in
the domain package (`internal/domain/<entity>_custom_repository.go`) and it is correct: it
composes the framework's `domain.Repository[*Entity]` and returns `*Entity` (Q1 yes), and
it exists so the aggregate's own persistence contract is a domain interface rather than an
infra one (Q2 yes). A `SecretHasher` with `Hash(plain string) (string, error)` fails both:
primitives only, named for its mechanism, and no rule in any `BuildRules` calls it — the
token handler does. Two JWT claim names fail both before the second question is reached.

## Where the rest goes — the positive destinations

The reason the domain package wins by default is that nothing tells the agent where else to
put things. This table is that answer; use it instead of the least-resistance package.

| What it is | Where it goes |
|---|---|
| a contract for a MECHANISM a handler consumes — hasher, token issuer, clock, id generator, an outbound gateway | `internal/application/`, beside the handler that consumes it (`commands/handlers/` · `queries/handlers/`); the **implementation** in `internal/infra/` — `internal/infra/external/` when it talks to another system |
| the same, consumed only by an adapter | `internal/infra/`, beside that adapter — it never needed a shared home at all |
| the vocabulary of a PROTOCOL — JWT claim names, header names, scope strings, an upstream's status codes | the layer that speaks that protocol: `internal/web/` for anything wire- or token-shaped, `internal/infra/` for an upstream's. A protocol name is not ubiquitous language; it is someone else's |
| a closed set of values the DOMAIN reasons about — a status, a kind, a relationship | `internal/domain/vos/` as an **`EnumValueObject`**, never a `const` block at the domain root. A const block of domain values is a value object that was not modelled (`value-objects` at the pin) |
| a rejection | `notification-bases.md` — the owner of that decision |
| a persisted resource whose rules are not the aggregate's, and which is LISTED, audited, projected or lifecycle-driven | still a domain struct, with an **empty `BuildRules`** — the write-backed schema is type-anchored (`notification-bases.md`, "the adjacent trap"). Placement out is never the conclusion for a resource the framework's aggregate repository writes |
| a table with **no aggregate behind it at all** — a control table, a job queue, a lookup, an idempotency ledger: nothing lists it, audits it or projects it | a **Direct schema**, and its row struct is a storage shape that lives in `internal/infra/` beside it — the one persisted type that is genuinely not domain. `direct-schema.md` owns the decision AND the availability test; on a pin without it, the row above is still the answer |
| a type two layers genuinely share and neither owns | it does not exist yet — see argument 1 below. Declare it at each consumer |

## The three arguments that are not arguments

### 1. "`internal/domain` is the only package everyone can import without a cycle."

**That is the definition of the violation, not a justification for it.** The domain package
is importable by everyone precisely BECAUSE it depends on nothing — its emptiness of
mechanism is the property being spent when a mechanism is parked there. Placing by import
graph inverts the architecture: the layer that is supposed to be pure becomes the one that
accumulates everything impure, one convenience at a time, and each addition is individually
defensible.

The cycle pressure is information, and it is saying something specific: **this type is
consumed by two layers.** In Go that has an idiomatic answer that is not a shared package —
interfaces are **structural**, so each consumer declares the one-method interface it
actually needs and the same implementation satisfies both without either knowing the other
exists. Two small declarations at two consumers is the correct shape, not duplication to be
refactored away later; the standard library is built this way. What a shared declaration
buys is a compile-time coupling between two layers that had none — the exact thing the
dependency table forbids.

And if the type is consumed by domain code at all, question 2 already answered yes and
there was never a cycle to route around.

### 2. "There is already something similar there."

**Precedent is not authorization.** A misplaced file already in the tree is a FINDING to
report, not a licence to add the second one — and the second one is what turns a slip into
a convention, because the next run reads N=2 as the house style and stops asking. When a
run finds a misplacement it did not create, it says so in its report (`doctor` and
`remove-entity` name it explicitly); it does not extend it and it does not silently fix
someone's file either.

This trap has a sharp edge in this framework, which is why it deserves its own paragraph:
**the domain package legitimately contains ports** — the aggregate's `domain.Service` and
its repository port. So "there is already an interface in here" will ALWAYS be true, in
every well-formed service, and it will never once be evidence. Similar-looking is not the
test. The two questions are.

### 3. "A future consumer will need it."

**The consumer that does not exist does not vote.** Placement is decided by who consumes
the type TODAY. The argument is seductive because it is unfalsifiable — the endpoint that
would justify the location is always about to be written — and it is wrong on its own
terms: when the second consumer does arrive, moving a type across packages in Go is a
mechanical, compiler-verified rename of minutes, done at a point where its real shape is
known instead of guessed. Nothing is saved by deciding early; a wrong guess is paid for by
every reader in between.

Its cousin, refuted the same way: "the dev will probably want this shared." Ask, or place
it at the consumer. Do not pre-position.

## The gate — mechanical, run it in Level 1

Cheap checks, all of them greps, run before any boot step. Each hit is investigated against
the two questions, not auto-deleted.

- **Imports.** Every file under `internal/domain/` imports stdlib, the framework's `domain`
  package, and this service's own `vos`/`aggregatevos` — **nothing else**. A `crypto/…`, a
  JWT library, `net/http`, a cloud SDK or any `internal/infra` path is the mechanism
  arriving with the type; the pin's dependency table calls the domain layer stdlib-only and
  zero-IO (`architecture`). Highest-precision check in this file — run it first.
- **Interfaces.** Every `interface {` declared at the domain ROOT must pass both questions.
  In practice that means each one is either an `<Entity>Service` for an entity whose
  `RequiresService()` returns true, or a repository port composing the framework's
  `domain.Repository` over this service's aggregate. A third kind is a finding.
- **Constants.** A `const`/`var` string block at the domain root is protocol vocabulary or
  an unmodelled enum — both belong elsewhere (the table above). Enum MEMBERS declared by an
  `EnumValueObject` live under `vos/` and are not what this check sweeps.
- **File inventory.** At the domain root, every `.go` file is an entity, that entity's
  port, or `notifications.go`. A file whose name matches no entity in the service
  (`secret_hasher.go`, `claims.go`, `constants.go`, `types.go`, `shared.go`) is a finding
  by its name alone.

`hooks/guard-domain-membership.sh` in this plugin enforces what is decidable from the text
at write time, on omnicore projects only: it **refuses** a forbidden import and an
off-convention interface at the domain root (both are layer violations whatever the intent),
and **asks the developer** about a string constant (that one can be flat domain vocabulary
the author meant). It is a floor, not the rule — it sees one file at a time and cannot
answer question 2, so a run still owes the gate above and this file stays the authority.

## Pin note

Nothing here is version-gated — the closed list, the dependency table and the
interface-with-its-consumer rule are in every pin that has `service-layout` and
`architecture`. What varies is only how explicitly a given pin's `service-layout` states
the application/infra placement bullets. Where the pin is terser than this file, this file
is the authority for the PLACEMENT decision and the pin stays the authority for the API.
