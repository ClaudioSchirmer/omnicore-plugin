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

**Pass `-coverpkg=./internal/...`.** Without it Go credits a file only to tests in its OWN
package, so a mapper exercised from another package's test reads as 0% and a package with
no test file of its own (dtos, web) reads as entirely untested while being covered. That
under-report is worse than no number: it sends someone writing tests for code that has
them, and it hides the files that genuinely have none. A 0% on a file you know is
exercised means the measurement is wrong, not that the test is missing.

## What to cover

- **Value objects (`internal/domain/vos/`):** test each VO DIRECTLY. A raw VO's
  `IsValid(fieldName, ctx)` takes a `*domain.NotificationContext` — construct one in the
  test and assert valid / malformed / empty cases (table-driven). An enum VO has NO
  `IsValid` at all (`EnumValueObject` declares members; the FRAMEWORK validates
  membership — `value-objects.html`): assert its `Values()` set and
  `UnknownNotification()`, and prove out-of-set rejection through the entity's
  validation path. A **composite VO** owns an `IsValid` like a raw one and is tested the
  same way, with the pair that a cross-field rule makes essential: one case proving a
  well-formed value is ACCEPTED, and one per declared rule proving it is refused. The
  positive case is not ceremony — a cross-field rule reading its two operands the wrong way
  round refuses everything and passes every negative test there is. Cover the parts that are
  themselves value objects too (an out-of-set enum part must be refused from INSIDE the
  composite: the framework's automatic pass validates the composite, never its interior).
  Format/length/range/closed-set coverage lives HERE now — NOT in the
  entity's `BuildRules`.
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
