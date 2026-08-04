# conventions/tests.md — unit tests (≥ 80% per generated file)

> NO code here, by design. The framework's test entry points (`IsValid` + enum membership →
> `value-objects.html`; the `Get*` family, notification-context reading → `rules-dsl.html`)
> — read them before writing the
> tests. This file carries only what to cover and how to measure.

Tests sit **beside the source**, one `<source>_test.go` per file (`service-layout.html`).
Never edit a test to make code pass — the test is the oracle.

## Measurement

Target ≥ 80%, measured **per generated FILE, not per package**: run with a coverage
profile and read the entity's own files — the bare package number mixes in pre-existing
code and misstates the entity (only in a brand-new package do they coincide).

## What to cover

- **Value objects (`internal/domain/vos/`):** test each VO DIRECTLY (its `IsValid` /
  membership is a plain method — no framework entry point needed): a raw VO's valid /
  malformed / empty cases (table-driven), an enum's members accepted + the zero/Unknown and
  an out-of-set value rejected. Format/length/range/closed-set coverage lives HERE now —
  NOT in the entity's `BuildRules`.
- **Domain (the coverage driver):** the happy path + EVERY `BuildRules` branch — each
  required plain field, cross-field invariants, optional fields passing as nil, transition
  rules via the update path with an apply-mutation (and their no-op on Insert — `Old` is nil
  there), actionName-gated branches (archive owner-checks firing only under their action),
  child invariants (add accepts distinct / rejects duplicate). Drive everything through the
  framework entry points from `rules-dsl.html` — never by calling `BuildRules` directly. An
  AVO also gets a direct `IsSameBusinessIdentity` test (same identity / different / type
  mismatch).
- **Application:** each input mapper transfers scalars + nil pointers correctly (PATCH
  leaves nil untouched); `FromEntity` mirrors the post-write entity (root + sibling facet
  + children WITH ids — stand in for the persister's id write-back via the domain's
  assign-id API); query `ToCriteria` returns the expected criteria.
- **Web:** request `ToCommand` (minimal/full/edge — empty-string pointer preserved) and
  response `FromResult` (scalars transfer, nil stays nil).

## What NOT to unit-test

Handlers (framework generics), routes/OpenAPI/GraphQL wiring, the persister — those belong
to the end-to-end suites. Unit tests own exactly the code the entity adds: rules, mappers,
criteria.
