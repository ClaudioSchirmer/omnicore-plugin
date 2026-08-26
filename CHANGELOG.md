# Changelog

All notable changes to the omnicore plugin. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions are the
`version` field of `plugins/omnicore/.claude-plugin/plugin.json` — each release
is the commit bumping that field on `main`, tagged `v<version>`.

## [Unreleased]

## [0.41.0] — 2026-08-26

Three emitter defects reported from one generation — a User aggregate with two child
collections, in a project that already had Tenant, Permission, Role and Group. All three
were the emitter's, not the spec's: `check` was green on the spec that produced each one.
Two of them stop the tree from compiling, which is the good failure. The third compiles,
passes `gofmt`, passes `vet`, passes the generated suite, and hands a plaintext password
back to the caller.

### Fixed

- **A `source: body` runtime field is never echoed in a refusal.** `echoValue` defaults to
  true, and that default is right almost everywhere — "the cap is 4" without "you sent 6"
  is half an answer. It was wrong for exactly one kind of field, and it was wrong by
  default: a `runtime: true, source: body` value exists to reach no copy of anything, and
  the canonical one is a password confirmation. A mistyped confirmation answered 422 with
  the plaintext, into the response body and from there into every log that renders a
  notification. The emitter now drops the echo for such a field on every rule kind,
  whatever the rule declared, and `check` refuses `echoValue: true` written over one
  rather than ignoring it. Nothing else about echoing changed: which persisted values are
  sensitive is still the spec author's call, and `echoValue: false` still says it.
  **`explain keys` was half the reason this shipped**: it prints one sentence per key, and
  `echoValue`'s opening sentence said what the key does and not that it is on by default —
  so an author read it as opt-in, declared it nowhere, and got the echo anyway. The default
  and the exception are now in that first sentence, where the reference can show them.
- **`kind: comparison` unwraps a value object on either side.** It is the only rule kind
  whose both operands are entity fields, and so the only one a value object breaks:
  `range` and `length` compare against a literal, and an untyped constant converts to
  whatever named type the field carries. Two typed operands get no such leniency —
  `e.PasswordConfirmation != e.Password` is `string` against `vos.Password`, a build
  failure in a tree the spec had already accepted. Each side is now reduced to its
  underlying scalar independently, including a composite's part (reached through its
  owner) and a value object over `time` (whose `Before`/`After`/`IsZero` are on the
  instant, not on the wrapper).
- **The collection projector is qualified by its entity.** It was named from the plural
  alone — `projectRoles` — while the per-entry projector beside it already carried the
  entry type (`projectOneUserRole`). Every entity's commands land in one Go package, so
  two entities that legitimately share a plural collided: `Group → Roles` and
  `User → Roles` is what an RBAC service IS, and the second one generated failed with
  `projectRoles redeclared in this block`. It is now `projectUserRoles` /
  `projectGroupRoles`, which is what the base-mounted case already emitted. **Regenerating
  an existing project renames these functions**; they are generator-owned and nothing
  hand-written calls them, but a `doctor` run will report the drift until you do.
- **The generated "rejects an invalid value" test picks a sample the rule actually
  rejects.** For a string-backed value object it was the fixed `"!!not-valid!!"` — thirteen
  characters of punctuation, which any regex worth declaring rejects and which a value
  object whose only rule is a length band containing 13 accepts, correctly. The generated
  suite then failed against a correct generator, on a spec that was never wrong. The sample
  now comes from the bounds when there is no regex to violate, exactly as the numeric
  branch beside it already did. Found while building the fixture for the three defects
  above: no string value object in the corpus had ever been declared without a regex.

### Changed

- **`hidden` says what it does NOT do.** Its documentation listed everything the key still
  leaves reachable — the column, the filters, the sort, a computed field deriving from it —
  and a reader could finish that list still believing the column is not fetched. On a
  relational read model it is: such a read builds no SELECT of the projected fields at all,
  it loads the whole aggregate through the write side's loader and prunes the document in
  memory, so a hidden column is selected on every row of every page and `?fields=` does not
  narrow the query either. The key reference and the skill now say so, and name the one
  lever that does answer "never loaded" — `redact` over a Mongo-backed read, whose document
  IS the redacted payload. Asked by an agent looking at a `PasswordHash` that was
  `hidden` + `redact` + in no filter and no sort, and reasoning that it therefore had no
  reader; it has none, and it is fetched anyway. Also documented, because it was a promise
  nobody had written down: a hidden field is not in the `?fields=` vocabulary, so naming
  one is a typed 400 rather than a 200 with an empty object.

### Added

- **Two golden-gate lanes** (`88 passed`, was 86). `34-plural-repetido` is a comparison
  whose two sides are an unwrapped value object and a plain `source: body` string;
  `35-plural-repetido-vizinho` is an independent entity whose collection reuses the plural,
  and it is generated into the SAME tree. That pairing is the whole point: the projector
  collision is cross-entity, every other lane generates one spec per tree, and a name two
  entities both claim compiles in each of them and fails only where they meet. The corpus
  could not have seen it, which is why it shipped.

## [0.40.0] — 2026-08-26

Two holes in the same wall. The field model had exactly two states — persisted, or fed from
the caller's token — and the spec language could not say where a value comes from when the
answer is neither. One case is a value the caller sends that nothing stores: a password and
its confirmation. The other is a value the SERVER fills, except for the one caller allowed
to say otherwise: the operator crossing a row scope. Both were being worked around by hand,
in the two files a regeneration overwrites most.

### Added

- **`fields[].source: body` — a field the caller sends that nothing stores.** Declared on a
  `runtime: true` field, it says the value comes from the REQUEST rather than from a claim:

  ```yaml
  - name: PasswordConfirmation
    type: string
    runtime: true
    source: body
    modes: [insert]
    vo: {kind: reuse, ref: Password}
    description: The password typed a second time — checked, never stored.
  ```

  It reaches the write request DTO, the command and its mapper, and the aggregate — where
  the framework validates its value object on every write that carries one, because that
  pass walks the STRUCT and not the `TableSchema`. It reaches no `TableSchema`, no
  migration, no outbox payload, no audit event, no Response, and no filter or `sort:`.
  There is no column, so there is nothing to redact and nothing to leak; `redact:` on such
  a field is refused for saying otherwise.

- **`fields[].modes` — which write verbs carry it.** Omitted means every write verb the
  entity has. The values are the two the DOMAIN can tell apart, `insert` and `update`: a
  PATCH is dispatched into the same `IfUpdate` clause a PUT is, so `update` names both
  shapes and `patch` is refused rather than promising a distinction `BuildRules` cannot
  make. The generator emits `r.IgnoreValueObject(...)` in every gate the field does not
  name — without it an archive, a delete or a patch of any other field would be answered
  with "the confirmation is required" for a value the request had no business sending — and
  a value-guarded exclusion in the update gate the field DOES name, because per-entry child
  writes and facet clears are dispatched there too and carry no body for it.

- **`source: claim` — the same key, spelling what `runtime: true` already meant.** It is
  the default, so every existing spec still says what it said. The blocker for a runtime
  field with no claim now names BOTH answers: the one it printed sent two consumers away
  believing runtime meant "from the token, full stop".

- **`fields[].bypassMaySet` — a server-assigned scope that yields to the bypass.**
  `assignedFrom: identity-claim` on a tenant is the right shape and, on its own, a trap: it
  removes the field from every write body, so the operator `authz.bypass` lets cross the row
  scope could read and repair a customer's records and never create one. Declared on the
  field `authz.tenantField`/`authz.ownerField` names, this puts it back in the INSERT body
  as an OPTIONAL value:

  ```yaml
  - name: TenantID
    type: id
    column: tenant_id
    assignedFrom: identity-claim
    claim: tenant_id
    bypassMaySet: true
  ```

  Absent means "mine", which the identity already wrote. It stays out of the update and the
  patch: a record does not change scope by being edited.

  **The mapper does not check who sent it, and that is the design.** The stated value is
  applied whoever sent it, so the row-scope guard the entity already carries is what
  answers — a caller who may not cross the scope meets the same notification a write into a
  foreign record meets, instead of having their value silently swapped for their own. That
  is also why `check` refuses the key anywhere but on the scope's own subject: on any other
  field nothing would compare it, and the value would be accepted from everybody. It is
  refused, too, without a scoped `dataAccess`, without `authz.bypass`, on `derived`, on a
  field nothing assigns, on a collection entry or facet, and on an entity with no insert.

  What this replaces is the workaround two consumers reached for: dropping `assignedFrom`
  and putting the tenant in the body with a rule, which makes every ordinary caller send a
  value the server already knows and leaves that rule as the only thing between them and
  their neighbour's data.

### Changed

- **`check` refuses a `source: body` field as an `ownerCheck`'s `ownerField` or
  `adminField`.** The caller chooses what such a field holds, so "the row is yours" would
  have been compared against a string the caller typed — and passed. Who the caller is
  comes from the token.

- **The gen-report gained "Fields the caller sends and nothing stores".** A reviewer
  reading the migration finds no column and concludes the field was forgotten; one reading
  the aggregate finds it and concludes it is stored. The section says which verbs carry it,
  what validates it, and that the comparison against the value it confirms is a rule the
  author owes.

- **And "The tenant is server-assigned, and the insert accepts one anyway".** A reviewer
  reading that insert DTO sees a caller choosing their own scope and is right to stop; what
  makes it safe is a guard in another file, and the section puts the two side by side —
  naming the guard, and saying what breaks if it is ever narrowed.

### Fixed

- **A `comparison` rule over a non-numeric field emitted a generated test that did not
  compile.** `violatingComparison` fell through to `999999` for everything that was not a
  time, so a string, an id or a bool compared against another field produced
  `e.Field = 999999`. It now answers per type, and derives the violating value from the
  OTHER field where it can — "the same value" and "anything but this value" are the only
  two answers that hold whatever the valid sample happens to be.

## [0.39.0] — 2026-08-25

Framework 0.60.0 gave a field a way to keep its real value in the column and wear a mask in
every copy the framework makes of the row. The generator now speaks it — all four
redactors, at every seat a field can occupy, including the one it has to hand back.

### Added

- **`fields[].redact` — the framework's `RedactedField`, declared in the spec.** The real
  value stays in the relational column and in the hydrated entity; every copy the framework
  makes of the row carries a mask instead — the outbox payload, and with it the topic, every
  consuming service, both failure ledgers and the projected document, plus the audit event.
  Two axes, both mandatory:

  ```yaml
  - name: Documento
    type: string
    column: documento
    length: 14
    hidden: true
    redact:
      inSync: {kind: keep-last, keep: 4}
      inAudit: {kind: fixed, value: "***"}
  ```

  ```go
  RedactedField("Documento", "documento",
      core.InSync(core.RedactKeepLast(4)),
      core.InAudit(core.RedactWith("***"))).
  ```

  **Nothing above the schema changes.** Not the aggregate, not the commands, not the DTOs,
  not the migration, not the view — the column still holds the real value and the entity
  still hydrates with it, so the rules read it, filters, `?search=` and the aggregate DSL
  reach it, and a write may set it. The one line that moves is `Field(...)` becoming
  `RedactedField(...)`, which is exactly why a redaction that fails to reach the schema is
  invisible: the tree compiles, boots, passes every test, and the value travels in full.

  The family is the framework's, closed and small: `plain` (the real value, said out loud —
  with both axes mandatory it is the only way to write "masked in the payload, intact in the
  trail"), `fixed` (a constant, written in the COLUMN's own Go type — `core.RedactWith(int64(0))`
  on an int64 column, a `time.Date(…)` literal on a timestamp), `keep-last` and `hook`.

  Declarable at every seat: the root, a shared base, a 1:1 facet, a collection entry, a facet
  of an entry, and one PART of a composite value object — independently of its siblings
  inside it, because the amount of a salary is sensitive and its currency is not.

- **`kind: hook` — the mask the family cannot express, handed back like every other ELSE.**
  The generator writes the call and creates
  `internal/infra/schemas/<entity>_redactors_manual.go` — write-once, never regenerated, one
  function per AXIS — with a stub that **panics** until it is written, so the first write
  carrying the field is abandoned and rolled back.

  That direction is deliberate. A stub returning `"***"` fails SAFE and is still the wrong
  answer, and it is the one wrong answer that is expensive to undo: the framework cannot see
  that a hook's body changed — a closure has no portable identity, so the view's rebuild hash
  mixes in only the KIND — so documents already projected through a placeholder are repaired
  by a `read.view.version` bump and a rebuild, by hand, months later. A panic costs one
  failed write. The gen-report carries the signatures, and names any hook the file predates.

- **`check` refuses what the framework would panic on at boot, and warns where the promise
  is narrower than it looks.** A missing axis (naming what that axis governs, and teaching
  `{kind: plain}` as the way to say "the real value belongs here"); a `value` or a `keep` on
  a kind that ignores it, which the framework silently drops and the author believes is in
  force; `keep-last` or `hook` on a column that is not text; a `fixed` value that is not of
  the column's type; the shared identity's `naturalKey`, whose id is UUIDv5 over a fixed
  public namespace and that value in the clear; a redaction on a base column from a role that
  REUSES the base and therefore writes no schema for it; `type: id`; a runtime-only field;
  and a composite redacted as a whole rather than per part.

  Two warnings, for the two ways a declaration says less than it appears to. Both axes
  `plain` masks nothing. And **a relational read model serves a redacted field IN THE CLEAR**
  — it SELECTs the column, and redaction governs only the copies the framework makes — unless
  the field is `hidden: true` or behind `read.fieldRestrict`; on a Mongo backing the document
  IS the redacted payload, so the reads serve the mask.

- **The gen-report carries the whole decision.** A table of every redacted field with what
  each axis does in words rather than keywords, the read-side answer for THIS project's
  backing, and the two things a reviewer cannot see from the generated files: that declaring
  or changing a redaction is a shape change requiring a `read.view.version` bump and the
  rebuild that replaces plaintext already projected, and that a `hook` is invisible to that
  check.

### Changed

- **The generator now targets framework `v0.60.0`** (`explain compat`, and the pin `check`
  compares a project against). Floor is ceiling, as always: an older pin is refused with the
  upgrade as the fix, `--force-unsupported` overrides.

### Fixed

- **A collection with a `time` field emitted a test file that did not compile.** The
  generated `internal/application/dtos/<entity>_dtos_test.go` imported `time` only when the
  collection carried a VALUE OBJECT, which is the wrong question — a plain `type: time` entry
  field samples as `time.Date(…)` just the same. A collection with a date and no value object
  therefore produced an OWNED file naming `time` and importing nothing that provides it, from
  a run that reported success. The import is now unconditional and pruned when unused, like
  the `domain` one beside it.

## [0.38.0] — 2026-08-24

A value object could never be a precondition: the framework validates every one of them on
every write, and that pass runs after the rules that depend on it. Now a rule can say where.

### Added

- **`rules.list[].kind: valueObject` — a value object validated WHERE THE RULE IS, so it can
  be a premise.** The framework validates every value-object field on every write, and that
  pass runs AFTER `BuildRules`. Which means a value object could never be the precondition of
  the rules below it — and a tenant a row-scope check compares against, a foreign key the next
  rule reads, a state a `transition` moves are exactly that. The language had no way to say
  it: `kind: required` over a value-object field is warned about by name (it reports the same
  empty value twice), so the author who needed the check EARLY was told to drop the rule and
  offered nothing in its place. This is the something.

  ```yaml
  - id: tenant-required
    kind: valueObject
    scope: [insertOrUpdate, archive, unarchive, delete]
    fields: [TenantID]
    guard: true
    description: the row-scope check below compares this value against the caller's.
  ```

  ```go
  e.TenantID.IsValid("TenantID", r.Context())
  r.IgnoreValueObject("TenantID")

  // guard (tenant-required): the rules below depend on these having passed.
  r.StopIfInvalid()
  ```

  **It adds no check — it moves one.** That is what makes it safe: the same validation the
  framework would have run, run earlier, and the field then excluded from the automatic pass
  so nothing is reported twice. The exclusion is scoped to the rule's own verbs, so every
  other verb still gets the check for free.

  **The call is bare, and the generated comments say why.** `IsValid` REPORTS and EMITS — the
  value object owns its own notification — so there is nothing to raise beside it and no
  result to test. The hand-written version of this rule, `if !e.TenantID.IsValid(...) {
  r.AddNotification("TenantID", domain.RequiredFieldNotification{}) }`, is the failure the
  kind exists to prevent: one wrong value, two complaints. `notification`, `attachTo`,
  `echoValue`, `skipWhen` and a bound are therefore all refused on it — a key that decides
  nothing reads, to the next author, like a key that does.

  **The kinds are not interchangeable, and the generator does not guess.** A raw value
  object, a composite and an `id` (`domain.ID` writes its own `IsValid`) are asked directly;
  an enum declares no `IsValid` at all and is asked for membership, `domain.ValidateEnum`.
  For a `vo.kind: reuse` field the type is one the spec never described, so its shape is read
  out of `internal/domain/vos` — and when the file answers neither, the rule is refused with
  the distinction spelled out instead of emitting a call that may not compile. An OPTIONAL
  value object is called behind a nil guard: absence is not a violation there any more than
  it is in the automatic pass.

  **Refused:** two rules validating one field on the same verb (`insert` and
  `insertOrUpdate` both run on an insert — a duplicate that is invisible in the yaml), one
  rule naming a field twice, a field with no value object to pull forward, a part of a
  composite (there is no such field on the aggregate), and the kind inside a composite value
  object's own rules, where there is no pass to move anything into.

  The `check` warning for `required` over a value object now names this kind as the way to
  get the check early, `BuildRules`' own doc comment stops claiming no value object is
  validated there, and the gen-report lists every field that moved — a 422 that now carries a
  field it used to hide behind a barrier is the feature working, and reads like a regression
  otherwise.

## [0.37.0] — 2026-08-24

The one file no entity owns, stamped with the name of whichever entity ran last — and a
rule that can now end the validation pass it is a precondition for.

### Added

- **`rules.list[].guard` — a rule that ENDS the validation pass.** The framework gives the
  domain `Rules.StopIfInvalid()`, a barrier that halts a pass where something has already
  been rejected instead of letting the rules below it run on a premise that is already
  false. A rule marked `guard: true` now emits it:

  ```go
  if e.EnrollmentNumber == "" {
      r.AddNotification("EnrollmentNumber", domain.RequiredFieldNotification{})
  }
  // guard (enrollment-required): the rules below depend on these having passed.
  r.StopIfInvalid()
  ```

  The call is bare. `StopIfInvalid` is itself the condition — it returns without doing
  anything when nothing has been rejected — so there is no `if` around it and no `return`
  to write; the framework unwinds the body from the seat that invoked the rules.

  **It is positional, and that is the design.** The barrier lands on the line after the
  rule's block, at the clause's own indentation, never inside it. Pushed into the `if`, it
  would fire on the first arm that rejected and hide the rest of what the same rule found;
  out here, every rule declared above has already had its say — which is what lets four
  preconditions all be reported with the key on the LAST of them. It sits outside a
  `skipWhen` gate too: the barrier is about the pass, not about whether this rule was
  evaluated.

  What it stops is everything the pass has not done yet: the rules below it, the entity's
  automatic value-object validation, and the `BuildRules` and value objects of every
  collection. The framework's structural gates — the verb being allowed at all, and id
  validity — sit outside it and always report.

  **It can never skip validation.** The framework stops only where a notification has
  ALREADY been emitted, so a clean write runs whole. What changes is the shape of a 422:
  what was found up to the barrier, instead of that plus every field the write would also
  have failed on. The gen-report lists every barrier for exactly that reason — a response
  that used to name five problems and now names two is the feature working, and reads like
  a regression.

  Order is the author's inside a verb gate and fixed between them: rules keep their
  declared order, but the gates are emitted `insert`, `insertOrUpdate`, `update`,
  `archive`, `unarchive`, `delete` — so on an insert a guard under `insertOrUpdate` sits
  after everything under `insert`, whatever the yaml said.

  It takes the key at both seats. On a **collection's** rule it ends that entry's pass —
  the rest of its `BuildRules`, its own value objects, and every sibling still queued
  behind it; at the root it stops every collection outright. Refused on a **composite value
  object's** rule, where there is nothing to stop: a value object checks itself inside
  `IsValid`, which is handed a `NotificationContext` and no `Rules`.

- **A collection's rule tests now run through the framework's own seat.** They called
  `BuildRules` directly, which was fine while nothing in a rule could end the pass — a
  barrier unwinds from inside the seat that invoked the rules, so a body called by hand
  let the unwind escape and every test failed on a rule doing exactly what it was declared
  to do. They go through `domain.ValidateAggregateChild` now, against a stand-in root
  declared in the test file itself: the real root is in `internal/domain`, which imports
  `aggregatevos`, so it cannot be imported back. The seat also validates the entry's value
  objects, which a write does too — so what these tests see is what the service sees.

  Opt-in: a spec without the key generates exactly the tree it generated before.

### Changed

- **The generator now targets framework `v0.59.0`** (was `v0.57.0`). That is the release
  carrying `Rules.StopIfInvalid()`, which `rules.list[].guard` emits, so the pin and the
  capability move together. `internal/compat` keeps floor == ceiling — one supported
  version, one shape of emitted code — so a project still pinned below it is refused with
  the usual overridable message rather than handed a tree that will not compile.

### Fixed

- **`generate` is a no-op again in a project with more than one entity.**
  `internal/domain/vos/doc.go` is written by EVERY spec that declares a value object —
  it documents the package, not an aggregate — but its header named the entity and the
  spec of the run that produced it. So generating `role` rewrote what `permission` had
  just written, generating `permission` rewrote `tenant`'s, and each run reported
  `updated 1` forever: the file cycled between owners and the working tree was never
  clean.

  Two things were wrong and only one of them was cosmetic. The header stated something
  false — a comment about the whole `vos` package attributed to a single spec — and,
  worse, it made the file's bytes depend on WHO generated it. That rules out the cheap
  CI check this generator's whole model rests on: regenerate, and prove nothing moved.

  Such a file now says what it is instead of guessing an owner:

  ```
  // shared:     the whole project — no single spec owns this file
  ```

  Nothing else about it changes: still owned, still checksummed, still refused when
  edited by hand. Every other generated file keeps naming its entity and its spec — the
  exception is narrow, and `internal/domain/vos/doc.go` is the only file that meets it.
  The first regeneration after upgrading rewrites that one header once.

- **`prune` no longer deletes a file another entity still generates.** The same shared
  file was recorded under every entity that produced it, so pruning the first of them
  removed it from disk while the others still emit it on their next run — and
  `generate` listed it as an orphan, which is how a reader would have been talked into
  doing exactly that. Both now recognise a neighbour's claim: prune reports the file as
  left alone and says which entity still generates it, and the orphan list leaves it
  out. This is the rule the registration merge already applied one declaration at a
  time, applied to whole files.

- **The gate could not have caught either.** Its regeneration lane compared the tree
  before and after a full sweep, which matches even when every entity rewrites the
  previous one's files — as long as the sweep runs in the same order both times, the end
  state is identical. Each run is now also required to report nothing written.

## [0.36.0] — 2026-08-24

A derived read field that validated green, generated happily, compiled, and was empty
forever — plus the six defects around it that made that one survivable, and the seat it
never had.

### Added

- **`children[].computed` — a derived read field on the ENTRY.** `read.computed` only ever
  had a seat at the ROOT, and a root derivation runs once per DOCUMENT: what the root holds
  for a collection is a slice, so there was nothing to hand it and no way to produce one
  answer per row. Every "one label per grant", "one flag per line" question had no key at
  all. Declared here, the derivation runs once per entry and takes that entry's own fields.

  `from:` names the entry's fields **bare** — its own, a facet folded into it, a field a
  join declared `inChild` brought onto it. The entry is the scope, so the collection is not
  spelled in front of them, and that is also what the framework expects: it records a
  nested field's computed sources under the same segment prefix as the field itself, so
  `?fields=<collection>.<name>` pushes `<collection>.<source>` down without either side
  spelling the segment out. The entry's row therefore earns the read contract at its own
  level: the sources are fetched instead of a name no column has, and `?orderBy=` over it is
  a typed 400.

  **It does not reach the tabular export, and neither does any other field of a
  collection** — a CSV/XLSX row is FLAT, so the export carries the root's columns and stops.
  That is the framework's shape rather than a gap here, and the root's `read.computed` is
  the one that does head a column. The derived field's `labelKey` is still registered in all
  seven catalogs: it is what a flattening export would need, and it costs nothing meanwhile.

  The bodies land in the same hook file as the root's, one exported function per field.
  Naming a ROOT field under `from:` is refused — the store would be asked for
  `<collection>.<rootField>`, a path no document has — and the refusal points at
  `read.computed`, which is the seat that can answer it. Read only, and one step more so
  than the root's: the per-entry write verbs return the entry through its own
  `<Entry>Response`, which carries what was STORED. A collection owned by a shared identity
  declares its derivations on the spec that owns the identity, so both roles serve the same
  field from one shape.

### Changed

- **⚠️ BREAKING — a computed read field's derivation is now named for its entity.**
  `Compute<Field>` became `Compute<Entity><Field>` (and `Compute<Entity><Entry><Field>` for
  a per-entry one). Every entity of a project writes its derivations into ONE package,
  `internal/application/queries`, so two specs that each declared a computed field called
  `Permission` emitted `ComputePermission` twice and the package stopped compiling — with
  the worse half being that the file is a hook, written once and never rewritten, so the
  obvious way out was editing one of the two by hand and losing whichever body was already
  there.

  **What a consumer has to do:** rename the function in each existing
  `<entity>_computed_manual.go` to match the new signature — the report and the generated
  call sites both print it. Nothing is lost and nothing is rewritten.

  **And `check` says so before anything is written.** A hook is written once, so by the time
  this rename lands the body is the author's work; a run that renamed the call sites and said
  nothing would leave them with a function nobody calls beside call sites for a function
  nobody wrote, and no statement anywhere that the two are the same derivation. Both `check`
  and `generate` refuse, naming the file, the function that is there and the one the tree now
  expects. The signature they print is the emitted one by construction — one format string
  serves the hook and the report, which used to be two that happened to agree.

- **Every key that addresses a collection now accepts EITHER of its two names.** A
  collection has two, and both are real: `name` is the entry's Go type, `plural` is the
  collection's name — the document segment, the read DTO's field and the notification's
  wire path. The keys disagreed about which one they wanted, and they disagreed silently:
  `joins[].inChild`, `rules.list[].fields` and `read.computed.from` resolved the singular;
  `service.facts[].filters` resolved the plural, and its refusal argued that plural was
  "the name the projection, the read DTO and the notification path already use" — true of
  the framework and the exact opposite of what the other three keys did. One spec, one
  collection, three spellings, and the only way to learn which key wanted which word was to
  be refused by it.

  All of them now go through one resolver and the IR canonicalises to a single spelling
  below that line, so no emitter learns there were two — including the dotted head of
  `read.indexes` and `read.fieldRestrict`, which resolves a collection's field to a document
  path and would otherwise have declared an index over the literal string
  `"Permissoes.PermissaoID"`. Messages show the `plural`. The one new refusal is the
  ambiguity that would make "either name" impossible: one word naming the entry type of one
  collection and the plural of another.

### Fixed

- **A computed read field's `from:` was resolved TWICE, against two different sets, and the
  gap was swallowed in silence.** Validation blessed six categories — a root join's field,
  a collection's field, a collection's joined field, a `read.managed` column, the entity's
  own fields, a facet's. Emission resolved two. A source in the gap was dropped with a bare
  `continue`, so the derivation's signature came out one parameter short — which compiles,
  because a function with fewer parameters is valid Go — and the field rendered empty on
  REST, on GraphQL and in the export at once. `check` was green, `generate` succeeded, the
  tree built, the tests passed, and nothing anywhere could detect it.

  Resolution now happens exactly once, in the IR, and the emitters read the result. A
  source a ROOT JOIN brings onto the read model resolves, which is the most natural use of
  the feature and the one that silently vanished. A name that resolves to nothing is a hard
  failure of the generator, not a shorter signature: if the two halves ever drift again,
  the run stops instead of shipping an empty column.

  Validation was narrowed to match in the same round: a source that lives inside a
  collection is refused at the root with the reason — this derivation is handed values, not
  an entry — and pointed at `children[].computed`.

- **An orphaned hook was invisible to every command.** Take the last computed field (or the
  last manual rule) out of a spec and regenerate: the hook file stays on disk declaring
  functions nothing calls. `prune` reads the lock, and a hook was recorded nowhere at all —
  by construction, since a hook carries no checksum — so it saw nothing, and the report
  listed the orphaned translation keys next to it while never naming the Go file. There was
  no tool path back to a tree that compiles.

  The lock now records what the generator CREATED a hook as, and only that: the record
  never follows the file, so a hand edit still is not drift and the hook is still written
  once and never rewritten. It is enough to tell the two cases apart — untouched since it
  was written, an orphan is removed like any other leftover; written in, it is reported as
  yours and left, because deleting somebody's body is not a tool's call. Migrations stay
  out, deliberately: a migration RAN, and no report may offer to clean one up.

  Two commands gained the same knowledge. `adopt` refuses a hook and says why (there is no
  refusal here for an adoption to lift), and `doctor` says something true about one instead
  of announcing a refusal regeneration never makes: a hook still byte-for-byte as it was
  created is one nobody has written in — and it says which KIND of silence that is, because
  the three are not interchangeable: unenforced rules accept a write the spec calls invalid,
  a derivation renders a column empty on every surface, and an unimplemented service fact
  PANICS the first time a rule asks it. One sentence for all three is the sentence that gets
  somebody paged.

  **A project generated before this version is picked up on its next regeneration**, or the
  fix would only ever reach new trees while every hook already on disk stayed invisible.
  What gets recorded there is what the generator WOULD write, never the file's own bytes —
  recording those would make a hook somebody had already filled in look untouched, and
  "untouched" is the one verdict that authorises deletion. The comparison is therefore
  conservative for a retrofitted hook: it is reported and left, not removed.

- **A derivation over an `id` or a timestamp emitted a hook that did not compile.** The
  derivation file's import block was hard-coded to `application/configuration`, on the
  assumption that a derivation only ever sees builtins. It does not: `type: id` is
  `domain.ID` and `type: time` is `time.Time`, in a source or in the derived value — so
  `read.computed` with `from: [<an id field>]` wrote `undefined: domain`, in a write-once
  file the author is then left to repair by hand. The generated test file alongside it had
  the same block and the same defect, since it BUILDS one of each. Both now decide their
  imports from the types they actually name, which is the rule the query files already
  followed.

  It survived this long because no fixture that BUILDS had ever derived from an id — the
  gate's boot host now does, on a per-child collection, which is where it surfaced.

- **A shared-base role's insert Result mapper had no generated test at all.** The round-trip
  test skipped any command whose input method is `ApplyTo` — which is every role's insert,
  since it is an upsert — so `FromEntity` went unexercised there. It is the same mapper
  either way, and on a role it is also where a computed field's derivation is called, so the
  one seat whose body is hand-written had no coverage on that shape. Found by adding a
  derivation to a role fixture and watching the coverage lane, not by reading the emitter.

- **A derivation's sources were never proved to be distinct PARAMETERS.** Each source
  becomes one, under its camelCase name, and a leading run of capitals lowercases as a unit
  — so `from: [IDNumber, IdNumber]` emitted `func Compute…(ctx …, idNumber string, idNumber
  string)`, and a source listed twice did the same trivially. Neither compiles, and neither
  is fixable from the spec that produced it without knowing why. The domain-service facts
  have refused exactly this since a manual fact could take two filters; the read side's
  derivations went without it. Refused now at both levels, `ctx` included — every
  derivation already takes the AppContext under that name.

- **A computed read field could take a collection's name and emit a struct with two fields
  under it.** `children[].plural` is already refused against `fields[]` — the read DTO
  cannot carry a collection and a value under one name — and `read.computed` was the half
  that check never covered, so the collision surfaced as a compile error in a tree the
  author did not write. Refused at `check` now, by name. The entry TYPE stays accepted:
  `<Name>RowResult` collides with nothing, and refusing it would be a refusal with no defect
  behind it.

## [0.35.0] — 2026-08-24

The hand-off page said a two-column constraint was a one-column one — and the reviewer
who trusts a summary is exactly who the page is written for.

### Fixed

- **The gen-report's `Unique` row named half of a composite constraint.** Uniqueness
  declared on a composite value object is uniqueness over the TUPLE, and it lands on the
  value object's FIRST PART so the constraint is built once over the whole run. The report
  printed that part's name and stopped there: a permission keyed by `recurso:acao` read as
  `Unique | ChaveRecurso — across the whole table`, while the migration created a partial
  index over `(chave_recurso, chave_acao) WHERE deleted_at IS NULL`. Nothing was wrong with
  the generated code — the DDL and even its own comment ("over the TUPLE: this constraint
  belongs to a composite value object") said it correctly. Only the summary a reviewer
  reads FIRST contradicted them, and it contradicted them in the direction that looks
  plausible: a single-column unique is what most entities have. The row now names the value
  object with its parts and says the constraint covers them together.

  The scope half of the same line was wrong for the same reason. `within` was read off the
  constraint by matching the field's JSON name, but a composite's constraint is FILED under
  the value object's name — so the lookup never matched a composite at all and every one of
  them fell back to "across the whole table". A composite unique per tenant reported as
  global while the index was scoped, which is the report saying the opposite of what was
  built. The lookup now keys on the value object for a composite and on the field for a
  plain one, which is how `resolveConstraints` files them.

## [0.34.0] — 2026-08-24

A key the spec language documented, validated and threw away — and the guard that makes
its whole class visible from now on.

### Fixed

- **The seven texts of an enum member now reach the catalogs.**
  `valueObjects[].members[].text` was a first-class key of the language: listed by
  `explain keys` as *"the member's human-facing text, per language catalog"*, parsed,
  validated, and accepted by `check` with *"✓ this spec can be generated"*. It reached the
  IR and stopped there — `ir.EnumMember` carried `ConstName`, `Literal` and `Name`, so no
  emitter could ever see the translations. Nothing failed anywhere. The author declared
  *Aberto* / *Open* / *Ouvert*, got a green check, and read `SituacaoCurso.aberto` on the
  screen in all seven languages, because that is exactly what
  `Translator.EnumDescription` answers when the catalog has no entry for the value.

  **The key is the framework's, not the generator's.** `domain.EnumDescriptionKey`
  reflects over the value and answers `"<Type>.<value>"` — `EnrollmentStatus.active` for a
  string backing, `NivelContrato.1` for an int one — and that is the only key
  `translator.EnumDescription(lang, v)` ever looks up. An entry filed under the member's
  Go NAME would be well-formed, complete, and never found, so the entry is built from the
  VALUE and a test pins that shape. A `written: manual` value object contributes its
  entries too: the TYPE is the author's, the member set is still the spec's, and the
  framework derives the key by reflection either way — translating the generated enums and
  not the hand-written ones would be a new silence in place of the old one.

  A member left without `text` still gets its entry, filled with its own name spaced out.
  That is the LABEL discipline, deliberately, and not the notification one: a notification
  with no text is emitted as a loud `TODO(LANG):` placeholder because nobody can guess the
  sentence, while `Aberto` is a heading — a placeholder in its place is what the end user
  reads. The report names every catalog that got the fallback (see below).

  **What this does NOT change is the wire.** REST, GraphQL and gRPC carry the raw value in
  every language, by the framework's own design — *"standardized value in, standardized
  value out"*; `EnumDescription` is a deliberate per-request helper for showing a label,
  never a step in persistence, audit or the response DTO. So a status still arrives as
  `active`. What the entry buys is that the helper has something to find, and that the
  field's `labelKey` translating the column HEADING is no longer the only half that works.

  The asymmetry was the tell, and it is worth naming: the sibling key `descriptionKeys`
  was REFUSED by name, so the key that did nothing announced it while the key actually
  carrying the text failed in silence.

### Added

- **`### Enum values reading in the wrong language`, a new section of the gen-report.** It
  names every `<Type>.<value>` whose text the spec left out, per catalog. It is separate
  from *Missing translations* on purpose: that list is about entries emitted as marked
  placeholders, and these are emitted as a real word. Nothing in the generated code looks
  wrong, the build is green and the screen is in one language — the hand-off is the only
  place that can say so.

- **Two guards in `internal/emit/silence_test.go`**, the file whose whole subject is
  *declared, then forgotten*. `TestEveryEnumMemberTextReachesACatalog` asserts the property
  over whatever the coverage matrix contains — every member of every enum has an entry in
  every catalog, and never the key itself as the value.
  `TestEnumDescriptionKeyMatchesTheFramework` pins the key SHAPE, which is the half no
  amount of emitting can fix from the inside. The matrix gained the two backings to exercise them (`04` int, `11` string, with
  one member deliberately textless to cover the fallback).

### Changed

- **`valueObjects[].descriptionKeys` is still refused, and no longer for the wrong
  reason.** It used to say *"per-value translation keys are not generated"*, which was
  true of the build and false as advice — it sent an author away from the feature instead
  of to it. It now says the entries are not asked for by a flag: every member is registered
  under the key the framework derives, and what fills that entry is `members[].text`.

- **Both `explain example` specs and `skills/omnicore-gen/SKILL.md` teach the key.** The
  flat example declares all seven texts on `EnrollmentStatus` with the key shape spelled
  out; the sharedbase one deliberately declares none and says what the shorthand costs.

## [0.33.1] — 2026-08-24

A one-line hole in the read-join work of 0.33.0, and the guard that makes its whole
class visible from now on.

### Fixed

- **A collection that gains a TIMESTAMP through a read join no longer emits a file
  missing its `time` import.** The read shape of a collection —
  `internal/application/queries/<entity>_row_results.go` — carries the collection's own
  fields AND the fields a `joins[]` entry with `inChild:` lands inside each entry. The
  struct was written from both; the IMPORT was decided from the first alone. So a child
  join onto any `time` column emitted `*time.Time` under an import block that names only
  `domain`, and the tree stopped compiling at the consumer:

      internal/application/queries/role_row_results.go:27:24: undefined: time

  The place it broke is the worst one a generator has: `check` answered *"✓ this spec can
  be generated"* and `generate` wrote the file without a word, because neither of them
  compiles anything. Only `go build` found it, in the developer's own tree, minutes after
  the generator had said everything was fine.

  It is not specific to the framework-stamped columns 0.33.0 taught a join to reach. An
  ordinary `time` column of the target breaks identically, and always did — a child join
  simply had no way to carry a timestamp before, so the defect was unreachable rather than
  absent. The import decision now unions the collection's served join fields, which closes
  both readings at once: `time` is the only import-bearing type in the join field
  vocabulary (`string | int | int64 | float64 | bool | time`).

  The root-level query files were never affected — their own import decision already walked
  the root joins' fields — which is why the breakage showed on exactly one file.

### Added

- **Every emitted Go file is now checked to import what it qualifies**, over every spec in
  the coverage matrix. `internal/gofile` has always pruned the OPPOSITE class — an import
  nothing uses — once, for every emitter at once; nothing covered this direction, and a
  qualifier used with no import is invisible to `check`, survives `generate`, and lands as
  a build error somebody else has to read. The property is asserted structurally now, so an
  emitter that grows a new type cannot forget its import quietly, and a spec added to the
  matrix widens the guard for free. The matrix and the golden fixture both grew a
  time-carrying child join, so the gate compiles the case end to end rather than only
  inspecting it.

## [0.33.0] — 2026-08-23

The first release of read joins met a real project, and it refused a traversal the
framework accepts. Both halves of that gap are closed here.

### Fixed

- **A read join now reaches the target's framework-STAMPED columns.** The columns a spec
  registers under `storage.managed` — `createdAt`, `updatedAt`, `archivedAt`, under
  whatever names its author chose — are columns of its table like any other: the schema
  resolves them on the read path under the fixed LOGICAL names `CreatedAt`, `UpdatedAt` and
  `DeletedAt`, which is exactly what the framework's own join check consults. The generator
  read a neighbouring spec's `fields:` and nothing else, and those columns are declared BY
  PRESENCE, never there. So a legal traversal was refused with the one message an author
  cannot act on: *"`deleted_at` is not a column of Campus"* — about a column that is one.
  The claim now carries all three, read out of the target's own `storage.managed`, so the
  declaration names the column as THAT spec spells it and the type and nullability are
  derived as they are for any other column.

  **The archive column crosses into a POINTER, on either kind of join**, and that half is
  the generator's alone to get right. The framework deliberately does not police the
  nullability of a managed slot: the fields of `domain.Managed` are unexported, so its
  reflective check has nothing to point at and answers "not nullable" rather than
  guessing. A non-pointer `time.Time` therefore passed repository construction and failed
  on the first ACTIVE row scanned — `deleted_at IS NULL` being the normal state of a row,
  not the exception. The column is named in the target's own spec, so the generator says
  what the framework cannot.

  **The revision is still refused, now by name.** It is the guard of the target's OWN
  writes, so a copy carried across a join is stale the moment that aggregate is written
  again; the read path does not resolve it and the framework would fail at construction.
  Falling back to "is not a column of" sent the author to fix a declaration that was
  already right.

- **A join field may no longer shadow what the JOINING table's own schema stamps.** The
  same blind spot on the other side of the traversal: `owner.Resolve` reaches the owner's
  stamped columns and its link column right after its own — `CreatedAt`, `UpdatedAt`,
  `DeletedAt`, and `ParentID` on a collection or a shared-base role — and none of them is
  under `fields:`. The framework refuses those names at repository construction; the
  generator accepted them, which is a boot to find what the file could have said.

## [0.32.0] — 2026-08-23

The framework's v0.57.0 landed two things at once: it turned a relational read model into
its own TYPE, which broke every tree this generator emits, and it added READ JOINS, which
change how a rule reaches a value that belongs to another aggregate. This release is both
halves — the repair, and the capability.

**This release targets framework v0.57.0 and does not support anything older.** The
generator's supported line moved, and the emitted shape moved with it; a project pinned to
v0.56 or below has to upgrade first.

### Added

- **`joins:` — read joins in the spec language.** A read-only traversal across a foreign
  key into ANOTHER aggregate, declared on the REPOSITORY: the mapped columns become
  ordinary Go fields of the entity, filled on every load, and no `INSERT` or `UPDATE` can
  ever carry them because the `TableSchema` is untouched.

  The reason this matters more than "one more key" is what it makes unnecessary. A rule
  that needs a value belonging to another aggregate used to mean a denormalized column
  plus a write path keeping it in step — a rule that is quietly wrong the moment the
  source changes. Now the value is simply there, on the entity, at every load, with
  nothing duplicated and nothing to synchronize.

  `kind: inner | left` decides what a joining row with no counterpart means. `on:` names
  the foreign key on the joining table, `inChild:` hangs the traversal off one of the
  entity's own collections, and each `fields[]` entry maps one column of the target onto
  a Go name of your choosing.

  The Go name must not shadow what the JOINING TABLE already answers to, which is the
  framework's own rule and not a wider one: an entry's join may carry a name the root also
  uses, because a collection's schema does not resolve the root's fields and the two are
  separate structs in the emitted Go.

- **A joined field carries no domain type, and an identity crosses as TEXT.** `type: id`
  is refused on a join field, and so is any value object: the value belongs to another
  aggregate and arrives read-only, so a domain type there would be an instance no rule
  ever approved. The type is normally not stated at all — it is derived from the target's
  own spec, identity included. The generator emits `string` (or `*string`) for an identity
  column, which is also the only shape correct on all four engines, since three of them
  store an id as raw bytes.

  `explain keys` says so too. It matches a key path to its closed set by SUFFIX — which is
  what lets a nested `unique.scope` show the same values as the top-level one — and under
  first-match-wins `joins[].fields[].type` inherited `fields[].type`'s set and advertised
  `id`, the one value it blocks by name. The exact path now wins, and among suffixes the
  longest; a reference that recommends what the validator refuses is the failure it exists
  to prevent.

- **A join may traverse onto a column of the target's COMPOSITE value object.** A
  composite owns no column of its own — its value spans several, one per part — and those
  part columns are ordinary columns of the table, entered in the schema's bijection under
  the same rules a plain field's column is. The generator read only `fields[].column` when
  resolving a target, so they were invisible and a legal traversal was refused with the
  wrong reason ("not a column of X" — it is one). It now resolves each part's column,
  its exposed name (`as`, else the part's own) and its type — from the value object's own
  declaration, which is the only place the type lives when the composite is declared in
  that same spec. A part of an OPTIONAL composite is nullable whatever the parts say,
  because "every part column NULL" is how the absence of the whole value is written.

  What still does not cross is the composite as a CONCEPT: it stays whole only in the
  aggregate that declares it, and the invariant tying its parts together is that
  aggregate's to keep.

- **The pointer follows from what can be ABSENT, not from the kind alone.** A left join
  with no counterpart is one source of NULL; a column the TARGET declares nullable is the
  other, and it makes a field nullable even under an `inner` join — an inner join proves
  the joined ROW exists, never that every column of it is filled. A field that cannot hold
  NULL fails on the first row that has one, so both are applied. The framework guards the
  same pair; the generator derives it from the target's own spec, so the two agree by
  construction rather than by a boot that says no. A nullable identity and a nullable
  plain column are both crossed under an inner join in the fixtures.

  Where there is nothing to derive from — a HAND-WRITTEN target, which the generator
  cannot read — `joins[].fields[].nullable` is how the author says it, alongside the
  `type` that case already demands. Without it the emitted field was a non-pointer the
  framework refuses at repository construction, and the author had no key to prevent it
  with: a boot to find what the file could have said. Stating it for a target that IS a
  spec of this project is refused, for the same reason a restated `type` is — the target's
  own declaration is the one place the two cannot disagree.

- **`joins[].fields[].hidden` — on the entity, off the wire.** Needing a value and
  publishing it are different decisions, so the language asks them separately. A hidden
  joined field is filled on every load and read by the rules and the domain service, and
  it is in no response body and in no export. It is the shape for a traversal declared for
  a RULE, and without it "the rule needs it" quietly becomes "the caller receives it",
  which is an API promise nobody planned to make.

- **`shared/read-joins.md` — the plugin-side owner of the whole subject.** What a join is,
  where it is declared, the two kinds and what a missing counterpart means, the load-only
  boundary inside a collection, what it costs, and the line to hold when someone asks a
  join to do more than it can. Every skill that writes a rule, a repository or a read
  model now routes there. The three previously-correct answers it overturns — copy the
  column, load the other aggregate inside the rule, or materialize a Mongo view for it —
  are named as the thing not to do. The language half is `explain keys`, which takes no
  argument — `omnicore-gen` answers `explain keys joins` with "unknown topic", so the
  skill sent an agent at a command that prints nothing it asked for.

  Three routings the first pass left open close with it. **`remove-entity` now sweeps the
  joins that reach INTO the entity being removed** — the declaration lives on the OTHER
  aggregate's repository and names this entity only as its target, so an inventory
  organised per layer walked straight past it and the build broke on a plan that had been
  approved as complete. Every joining repository is now an edit in the inventory and a
  blocking `⚠️ OPEN` under Dependents, carrying the two follow-ons that land on the OTHER
  entity's contract rather than on this one's: its joined FIELDS go (a rules-only one
  changes a rule, a served one is an API break for that entity's consumers), and with them
  any filter or sort a relational read model declared over one. **`configure` stops calling
  a join backing-independent without qualifying it** — the DECLARATION is, its READ-side
  reach is not: a view flipped relational→Mongo keeps the traversal and loses every filter,
  sort and served field that came through it, a consumer-visible loss that now has to be
  named per view in the plan. And **`help` lists `read-joins` among the owner sheets it
  frames availability from**, which is where "can my service do this, and from which pin"
  gets answered.

- **Seven golden-gate lanes** (`78 passed`, was 71). The running service is asked to prove
  a join end to end — an inner join serves the counterpart's value, a hidden field never
  reaches the wire, a left join with no counterpart answers an ABSENCE rather than the
  zero value, a joined identity arrives as text, and a joined field filters and orders the
  listing. The **CSV export** is asserted separately, because it is a third rendering
  written by its own emitter and a missing header there surfaces as an internal name
  rather than as an error. The prune lane compiles a join whose target is HAND-WRITTEN,
  and a new **coverage-matrix row** (`29-read-joins`) carries both join shapes over a
  hand-written target through build, vet and the generated tests. With v0.57.0 published,
  the vendored golden host pins the released tag instead of a local checkout, so the gate
  measures the emitters against the API consumers actually receive.

  The DDL job stages its specs where the generator LOOKS for them — `specs/omnicore-gen/`,
  not a bare `specs/` — and takes the join's TARGET along. That directory was never only an
  output location: a read join derives its columns and their Go types from the target's own
  spec, so a sibling staged beside it rather than in it is invisible, and the join is
  refused for having no type to derive. Nothing before read joins needed a spec to see
  another one, which is why a job that had been green for months failed the first time the
  fixture reached across.

- **`doctor` and `adopt` have a lane at all.** Neither had one: the pair that decides
  whether a hand edit is recoverable was proven by nobody. The gate now checks the whole
  cycle on a join-bearing tree — doctor reports the edit, adopt records the exception,
  doctor reports it as adopted, and the next regeneration PRESERVES it instead of
  refusing the run. Reporting an exception and honoring it are two different things, and
  only the last step proves the second.

### Fixed

- **A value object naming a FRAMEWORK notification no longer generates a tree that does
  not compile.** The reference was emitted bare inside package `vos`, where nothing
  declares it — while the framework's notifications live in the framework's own domain
  package, which every generated value-object file already imports (the
  `RequiredFieldNotification` path writes it qualified two lines above).

  The generator invited the mistake: when a value object names a notification the spec
  does not declare, the refusal offers the framework's own BY NAME — "or name one of the
  framework's: …, `SchemaViolationNotification`". Following that advice produced a spec
  that passed `check` and a tree that failed `go build`, against the invariant the
  generator states about itself: a green spec compiles and boots.

  The reference is now qualified where every other default is materialised — in the
  resolver, not at the emission site — so the two cases differ only by where the type
  lives: a notification the service declares is generated INTO the `vos` package and is
  referenced bare; a framework one carries its qualifier. A golden fixture now declares
  one, so the lane that builds the tree is the guard.

  Present since the value-object emitters landed, and unrelated to read joins.

### Changed

- **breaking: the generator targets framework v0.57.0.** A relational read model is now
  emitted as its own declaration type over the aggregate's existing loader, contributed
  through the framework's relational feature seam rather than the Mongo one. The emitted
  view function returns a different type and takes the loader as its only structural
  input — the loader carries the schema, so nothing about the shape is restated and the
  old `BoundTable()` boot guard is gone with the thing it guarded.

  *Migration*: regenerate. Every generated view, feature and mount signature moves
  together, and no spec key changes except the one below.

- **breaking: `read.view.version` is REFUSED on a relational backing.** A read model that
  materializes nothing has no stored shape to grow stale against, nothing to rebuild and
  no boot to refuse, so a version there is a number nobody ever compares. It joins
  `read.indexes`, `read.view.deleteOnArchive` and `read.view.ttlSeconds` in the set the
  build refuses rather than silently discards — and the generator's own drift guard
  (shape-changed-without-a-bump) now stands down for that backing, because there is
  nothing it could be guarding.

  *Migration*: delete the key. A relational shape change is free — no bump, no rebuild, no
  operational step.

- **A read-model name may not end in `__0` or `__1`.** They are the blue-green slot
  suffixes the framework addresses a projected view's two physical collections by, and
  every consequence of the collision is silent. `check` refuses it here, in the same words
  the framework refuses it at boot — and by the same test, `HasSuffix`, so the degenerate
  name `__0` is refused on both sides rather than passing `check` and aborting the boot.

- **The backing flip is a CONVERSION, not a flag** — and `evolve-view` now plans both
  halves of it. Relational→Mongo re-declares the view on the other seam and rebuilds;
  Mongo→relational leaves a collection AND an `omnicore_mongo_views` row behind that
  nothing drops for you, so the DB-per-service guard aborts the next boot outside `dev`.
  The registry row is the half that gets forgotten, and forgetting it aborts the boot for
  a different reason than the one you just fixed. Both statements are now in the plan,
  which is read BEFORE the boot fails.

- **`UnsupportedCapabilityNotification` replaces the relational-specific name** across the
  skills, and `doctor` now tells the two 400s apart: a capability the backing cannot serve
  versus a `?fields=` path or cursor the read model does not have — the second is a
  client-side problem and must not be answered with a backing conversion.

- **The read-side knowledge was wrong in one load-bearing place and is fixed.**
  "The relational side cannot reach another aggregate" was true until v0.57 and is now
  false. `shared/read-side.md` says so plainly, and says the gap NARROWED rather than
  closed: a join is 1:1 and horizontal, one column at a time, while Mongo still does
  strictly more. `scaffold-view` gained a gate ahead of its composition catalog for the
  request that a join already answers, so a ComposedView is no longer proposed for a
  problem that needs no view at all.

## [0.31.0] — 2026-08-21

One key, reported from a consumer that had written its spec, got `✓ this spec can be
generated`, and still could not say the one thing its approved model required.

### Added

- **`children[].permissions` — a per-child collection can gate its own verbs.** Until now
  the add/change/remove routes of a `per-child` collection took whatever the root's update
  took, and there was no key to say otherwise: `authz.permissions` is a closed set of the
  seven ROOT operations, and declaring an unused `update` beside `patch` to smuggle a
  second value in was correctly refused as a permission for an operation nobody serves.

  The map is keyed by the same verbs `operations` uses, and may be partial —
  `permissions: {add: group:grant}` gates the add and leaves change and remove inheriting.
  It is per COLLECTION, so an entity with two of them can gate one and not the other.

  The gap matters because the collection edge is sometimes a different job from editing
  the record. On an RBAC entity, "may rename the group" and "may change what the group
  confers" are one permission only by accident, and the second is the one that lets an
  administrator hand themselves power they were not granted — which is why Entra ID guards
  a role-assignable group behind Privileged Role Administrator rather than Groups
  Administrator, and IAM spells `AttachGroupPolicy` as its own action instead of folding it
  into `UpdateGroup`.

  **Absent, nothing changes, and that is the point.** A collection that declares nothing
  keeps requiring the root's update permission. Re-gating those routes behind something new
  would start refusing callers who hold exactly what they were told to hold, on a
  regeneration that changed no key — so the three existing fixtures regenerate byte for byte
  (the only difference anywhere in the tree is the new gen-report row below).

  `check` refuses, each naming the key and the fix: a verb the collection does not mount, a
  name outside `add | change | remove`, any of it under `atomic-replace`, an empty map, and
  an empty permission.

### Fixed

- **A per-entry verb with nothing to inherit no longer generates a route no permission can
  satisfy.** The collection's routes are mounted from the COLLECTION, not from the root's
  modes, so an entity serving no `insert`, `update` or `patch` — a display-and-archive
  record that still owns an editable collection — published them anyway and inherited the
  empty string. The emitter wrote `RequirePermission("")`, which fails closed at runtime
  and said nothing at generation time; `check` had the same refusal for the root's own
  permissions and no equivalent here. It is now a blocker, and `children[].permissions` is
  the fix it names.

### Changed

- **The gen-report shows what gates each collection.** The decisions table listed the
  root's operations and said each one is a route with a permission; the per-entry verbs are
  routes with permissions too and appeared in no table at all. A reviewer had no way to see
  what guarded the collection edge, or that it was the root's update by inheritance. There
  is now one row per `per-child` collection naming the permission per verb and whether it
  was **declared** or **inherited** — the value alone cannot say which, since a collection
  may deliberately declare the very permission it would have inherited.

## [0.30.0] — 2026-08-21

The two `Fixed` entries come from the same report, the same agent and the same entity as
`[0.27.0]` and `[0.28.0]` — a tenant-scoped `Role` whose one collection holds catalog ids.
Both survive `gofmt`, `go vet`, `go build` and the generated suite, which is the property
that kept them alive across three rounds of review.

The two `Changed` entries are the answer to that pattern rather than to that report: a
refusal that never said what it refused, and a review step that read the spec and took the
emitted code on trust.

### Changed

- **A refusal now carries the value it refused, by default.** `rules.list[].echoValue`
  existed, the framework has carried the value as `NotificationMessage.FieldValue` since
  the beginning, and almost no spec ever wrote the key — so a 422 stated the rule and
  never what broke it. "At most 4 guardians" without "you sent 6"; "that key is taken"
  without which key. The half a caller can act on was the half being dropped.

  The key defaults to TRUE and became a pointer, so `echoValue: false` still says
  otherwise — which is the spelling to reach for on a value that should not travel back in
  a response, since nothing in the language marks a field as sensitive and the generator
  cannot guess. The printed shared-identity example now demonstrates the opt-out on a
  national id rather than demonstrating the opt-IN, which is no longer a thing to write.

  Four refusals that could never echo learned to. Three of them have no rules entry to
  carry the key at all, so they simply do it: the unique pre-check (which value is taken),
  the per-entry add door and the whole-collection duplicate check (which entry came
  twice — only when the business identity is a single field; a tuple echoes nothing,
  because one half of a key points at the wrong thing). The fourth is `groupCap`, which
  echoes the COUNT that broke the cap, on the same argument `factRange` already made: the
  limit plus where you are is a message someone can act on.

  Two defects surfaced with the default, both invisible while every fixture left the key
  off: a `comparison` rule declared on a COLLECTION emitted `e.Field` into a method that
  has no `e` (it went through the root-receiver helper), and a unique pre-check on a
  COMPOSITE key echoed the part's logical name, which is not a field of the entity. Both
  produced code that did not compile, so they were reachable by any spec that had written
  `echoValue: true`.

- **`scaffold-entity`, `evolve-entity` and `omnicore-gen` review the OUTPUT against the
  spec, and stop before editing it.** The review step said what to check about the spec
  and treated the emitted code as correct by construction. It is not: every generator
  defect found so far — the two below among them — survived gofmt, vet, build and the
  generated suite, because none of them is a Go error. Step 7 now asks two questions
  explicitly (does the spec say what the model meant; does the OUTPUT say what the spec
  declared), names the failure shape, and says to report a divergence rather than quietly
  work around it.

  It also gains a real gate: **a generated file is never edited without the dev's yes.**
  Editing generated code is supported — `adopt` is part of the generator's plan, and being
  timid about it produces a spec that lies about what is on disk — but the cost is
  permanent and lands on somebody who is not in the conversation, so the choice is the
  dev's every time. The gate asks with the two snippets side by side, what was ruled out,
  what `adopt` costs (the file stops tracking the spec forever, so later emitter fixes
  land everywhere except there), and the alternatives, so it is a decision rather than a
  rubber stamp. Both scaffolding skills state the promise at their generation gateway, so
  a dev sees it when they approve the path.

### Fixed

- **A tenant-scoped entity generated its write guard and then left it unfed on four
  mappers.** `authz.dataAccess: tenant` synthesises three runtime fields, a
  `refuseForeignTenant` guard under every write gate, and the block that carries the
  caller onto the entity. The block reached the root's own mappers — insert, patch,
  archive — and none of the others: the three per-entry child verbs
  (`children[].editStrategy: per-child`) and the GraphQL facet-clear mutation all
  discarded the `AppContext` as `_`. All four dispatch through `UpdateCommandHandler`, so
  the guard RUNS for them; it just runs on zeroed fields, and `noIdentity: stand-down` —
  the default — makes an absent identity stand down. Net: a caller holding nothing but the
  entity's update permission could add an entry to, change an entry of, revoke an entry
  from, and clear the facet of an aggregate owned by ANOTHER TENANT. The read filter never
  covered it, because a child verb loads the root through the repository, which the read
  side never touches — so the damage was invisible from the caller's side too.

  It is the same defect the bodyless archive verb had, fixed one release earlier and never
  propagated: the fix there taught the archive mapper to name its context, and the four
  mappers emitted elsewhere kept the flat no-op. What let it survive is that the fixture
  the write guard was built against is FLAT — no children, no siblings — so every
  assertion about the guard read mappers that were already fed. The fixture now carries a
  per-entry collection and a 1:1 facet, and the generated tests for those verbs supply an
  identity and assert the caller's scope arrived, which is also what puts them back above
  the coverage floor.

- **A `groupCap` declared its bound, emitted the field for it, and raised the notification
  empty.** A cap whose notification declares `tvars: [max]` got the struct field, the seven
  catalogs and the hard-coded number in the comparison one line above the raise site — and
  `TooManyThingsNotification{}` at the raise site itself. The 422 rendered "A role may
  grant at most  permissions.", with a hole, in all seven languages. Nothing in a build, a
  vet or a generated test is capable of noticing: the only reader of the gap is an end
  user, in production. `range`, `length` and `factRange` were binding correctly; `groupCap`
  was the one kind with a bound that reached for the literal helper that cannot fill
  anything. Its bound lives under `cap:` rather than `max:`, so the binding now sources
  `{max}` from a cap as well — writing the number into the sentence to work around this was
  a copy no regeneration kept in step, and the golden fixture that did exactly that has
  been converted to `{max}` so the gate compiles the bound end to end.

  Spelling and qualification were separated in the same change: the helper that fills
  variables answers with a BARE type name, because the kinds that use it emit inside `vos`
  and `aggregatevos`, where that is the only spelling that compiles — a rule emitted in the
  root's package needs the qualified one, and `groupCap` is emitted there.

## [0.29.0] — 2026-08-21

### Added

- **`shared/query-primitives.md` — which QUESTION you are asking the database, one owner.**
  Reported from the outside: an agent implementing a domain `Service` reaches for the list
  load and folds the answer in Go — `len(...)`, `> 0`, a running total, a loop that buckets
  rows by a key — when the framework ships a hydration-free primitive for every one of
  those, on the same criteria surface. The generator itself cannot make this mistake (a
  declarative `service.facts` entry emits the existence probe or the aggregate DSL), so the
  gap was entirely in what the skills TELL somebody writing the file by hand.

  It was also, until now, badly placed: the only sentence in the plugin that said "never
  FindAll-and-filter" lived inside `scaffold-entity`'s numbered unique-field chain, where
  it reads as uniqueness guidance rather than as data-access guidance — and `implement`,
  which `scaffold-view` and `scaffold-system` explicitly route report scalars TO, named no
  primitive at all.

  The new owner carries the decision table (existence · scalar aggregates · grouped facts ·
  one aggregate · rows you will iterate · the user-facing listing and its `onlyTotal`
  count), the four things that bite in a hand-written impl (fail loud, `Found` vs the
  value, `SumInt` for money, a spec instance is stateful), and the argument that is about
  CORRECTNESS rather than latency: `FindOne` is the framework's birth point and stamps the
  old-state snapshot, the list load deliberately does not — so an entity loaded with
  `FindAll(…)[0]` and then written has no `Old()`, and every rule and audit line reading it
  is quietly wrong.

  Routed from `scaffold-entity` (SKILL doc-map ×2 and `conventions/infra.md`, whose two
  restatements became pointers), `implement` (a Core principle, since it is where report
  scalars land), `evolve-entity`, `doctor` (a diagnosis row: a write that got slower as the
  table grew, an empty `Old()` on a hand-loaded write), `omnicore-gen` (a `manual` fact
  whose truth turns out to be local) and `shared/read-side.md`. The generated
  `<entity>_service_manual.go` header says it too — that file is the last place the choice
  is made, and it is made by hand.

### Changed

- **`authz.bypass: "*:*"` now asks the framework's own question — `Identity.IsSuperAdmin()`.**
  When `0.28.0` shipped the wildcard bypass there was no way to ask "is this caller a
  super-admin?": `HasPermission` panics on a wildcard (the CLAIM wildcards; the question
  does not), so the emitted guard asked **two reserved concrete permissions** under
  resources no catalog contains — `superadmin.probe.a:cross-scope` and
  `superadmin.probe.b:cross-scope` — on the reasoning that a `*:*` claim answers yes to
  every concrete question and nothing else answers yes to both.

  It worked, and it was still a workaround: two invented permission strings that read as
  a bug in generated code, a bypass that a permission catalog could hand out by accident
  (granting both probes IS crossing the scope), and an argument that had to be repeated
  in four places for the reader to know why one probe was not enough. Framework `v0.56.0`
  gives the question a method of its own, so the guard now calls it — nil-safe, honouring
  `authorization.permissionsClaim`, and sharing the parsed-claim cache with
  `HasPermission`. A resource wildcard like `role:*` does not answer it, which the pair of
  probes only achieved by construction.

  Both halves of the scope moved together — the read filter in the query and the runtime
  field the write guard reads — and so did the gen-report's **Crossing the scope** row,
  which used to name the two probes for a reviewer who would otherwise meet them cold.
  The generated proof is unchanged in shape and stronger in what it proves: a real `*:*`
  claim set, put through the framework itself. One wart went with the probes — the
  mappers' generated test fed the identity a claim literally named `*:*`, as though the
  wildcard were a custom claim looked up by name; the super-admin question is answered
  from the permissions claim, like the concrete one, so nothing is invented there now.

- **The supported framework line is now `v0.56.0`** (`compat.Supported`, and the vendored
  host the golden gate builds), because that is the release carrying
  `Identity.IsSuperAdmin` — generated code calling it does not compile against an older
  pin. A project still on `v0.55.0` is `behind` and refused by default with the fix named,
  as any older line always was; `omnicore-gen` says so before generating anything.

### Fixed

- **The generated mapper tests carried a claim with no name.** An entity under
  `authz.noIdentity: stand-down` synthesises a runtime field recording that the request
  carried an identity AT ALL — the fact an empty scope cannot distinguish from a token
  without the claim. It is the nil check itself, so it has no claim name; the test-identity
  emitter did not know that and fell through to the branch for author-declared claims,
  putting `"": "true"` into every generated `Claims` map. Harmless at runtime and wrong to
  read: it looks like the framework resolves something by that name. Presence is now
  excluded there alongside the two permission questions, which never were claims either.

## [0.28.0] — 2026-08-21

Both entries came from the same report as `[0.27.0]`, from the same agent and the
same entity — a tenant-scoped `Role` whose one collection holds catalog ids. They
are the two places where that model could be generated only by changing the model:
the spec language could not say what had been approved, and the two ways out on
offer each meant approving something else.

### Added

- **`children[].operations` — WHICH per-entry verbs a collection mounts.** Absent
  means all three, so nothing regenerates differently; any non-empty subset of
  `[add, change, remove]` is legal.

  The trio is sometimes a pair. A collection whose every field IS its business
  identity — a grant holding one permission id and nothing else — has nothing a
  change verb can change and still leave it the same entry: the `PUT` turns entry A
  into entry B while keeping A's row id, which an audit trail reads as one grant
  BECOMING another rather than as a revocation and a grant. `check` had warned about
  exactly that for a release, and both exits it offered were worse than the model:
  `atomic-replace`, which is a different contract rather than a smaller one (the
  root's update carries the whole collection, so every partial client silently
  revokes what it omits), or inventing a mutable field the domain does not have so
  the verb has something to change. An author took the second one to satisfy the
  generator, which is the failure this key exists to end.

  A verb left out leaves **no trace**: no route and no OpenAPI entry, no command, no
  request/response pair, no domain method on the aggregate, no generated test. The
  domain method matters most — a leftover `ChangeXByID` compiles, is callable, and is
  an invitation to hand-mount the verb the spec decided against. The warning stops
  once the key answers it, and `duplicateNotification` is now refused on a collection
  that mounts no `add`: it names a conflict nothing can raise.

- **`authz.bypass: "*:*"` — the super-admin wildcard as the thing that crosses the
  row scope.** Until now the key took a concrete permission only, because the
  framework's `HasPermission` panics on a wildcard (the CLAIM wildcards; the question
  does not). So a spec whose approved policy was "a `*:*` holder crosses the tenant"
  had to mint a grantable string like `role:cross-tenant` to say it — a permission
  that can be handed to somebody who is not a super-admin, which is a wider policy
  than the one that was approved.

  The wildcard cannot be asked about, but it can be ANSWERED: a `*:*` claim says yes
  to every concrete question. The generated guard therefore asks two reserved
  concrete permissions under resources no catalog contains, and only a claim set
  carrying the wildcard says yes to both. Two and not one, because a single probe is
  also satisfied by a `<its resource>:*` grant. Both halves of the scope use it — the
  read filter in the query and the write guard's runtime field — and a **generated
  test proves it against the framework's own `HasPermission`** with a real wildcard
  claim, rather than against a comment. That test is new for the concrete form too:
  the domain's bypass test sets the runtime flag by hand and so could never see
  whether anything ever raised it.

  Every other wildcard is still refused, now saying which one is the exception. And
  the gen-report gained a **Crossing the scope** row, because a scope with an
  exception is two decisions and only one of them was being reported: for a concrete
  permission it says that HOLDING it is wider than being granted it (`platform:*` and
  `*:*` both answer yes), and for the wildcard it names the two probes, so a reviewer
  meeting those strings in the generated code can find out what they are.

### Changed

- The coverage matrix gained case 28 — a per-child collection with `operations:
  [add, remove]` and a wildcard bypass, i.e. the reported entity — so both features
  are generated, built, vetted and tested by the gate rather than by a unit test
  alone.

## [0.27.0] — 2026-08-21

Everything in this section came out of ONE report: an agent generating a single
entity — a tenant-scoped `Role` with a per-tenant handle and one collection of
catalog ids — and writing down every place the generator made it work by hand.
Four of the nine findings produced hand-written code or a wrong artifact; after
this release that entity has no adopted file and no hand-written SQL, and its
only hand-written code is the rules the spec genuinely cannot express.

### Security

- **A scoped `authz.dataAccess` guards the WRITE side, not only the read side.**
  `owner-only` and `tenant` emitted the read filter and no write guard, and the
  output looked complete: a reviewer read tenant isolation on the listings and
  reasonably concluded the posture was in place. It was not. A caller holding
  nothing but the ordinary permissions could **create a row inside another
  tenant** (the insert mapper copies the tenant straight from the request body,
  and no rule objected), **edit one** (the write path loads through the
  repository, not through the filtered query, so the read filter is not on that
  path at all) and **archive one** (same load, and the bodyless verb's `ApplyTo`
  was a flat no-op). The asymmetry is what made it dangerous: the caller could
  not read back the row they had just archived, so the damage was invisible from
  the side that caused it.

  The guard is now emitted in `BuildRules`, registered under **every write gate
  the entity mounts** — `IfInsertOrUpdate`, `IfArchive`, `IfUnarchive`,
  `IfDelete`, each named explicitly because there is no "any write" gate and
  archive does not dispatch under `IfUpdate` — and answers the framework's own
  `TenantMismatchNotification` (403, already translated in all seven catalogs).
  The caller's own scope reaches the entity through a runtime-only field the
  resolver synthesises, fed by every write command's mapper **including the
  bodyless ones**, which is what turns that no-op into the one thing an archive
  applies.

  Two things this also fixed, neither of them in the report. A `rules.list`
  `ownerCheck` scoped to `[archive]` was **inert**: the archive command's mapper
  fed no runtime field, so the check compared against `""` and its own
  empty-principal tolerance let every call through — a documented rule that ran
  on no verb. And the by-id read had no generated scope test while the listing
  did, so the read where a leak is most direct (the caller already holds the id)
  was asserted nowhere.

  **Regenerating an existing owner-only or tenant entity changes its runtime
  behaviour**, which is the point — writes that previously succeeded across the
  scope now answer 403.

- **Every rule that stands down for an absent principal now asks whether an
  identity was PRESENT, not whether its value came out empty.** The two are
  different facts that arrive at the domain as one, because the domain sees only
  the entity: `""` means either "no identity at all" — confined to
  `auth.mode: disabled`, which the framework refuses outside `APP_PROFILE=dev` —
  or "a real, signed, valid token that simply carries no such claim", which is an
  ordinary production request.

  A `rules.list` `ownerCheck` had been standing down on the VALUE since long
  before the row scope existed (`e.RequesterEmail != "" && …`), so **a caller
  holding a token without the claim passed the check and could edit or archive a
  row that was not theirs, in production**. The row scope's own `stand-down`
  inherited the same shape and the same hole. Both now read a runtime flag the
  mapper sets INSIDE the nil check, so reaching the assignment is the fact being
  recorded, and a claimless token is refused like any other caller who is not the
  owner.

### Added

- **`fields[].unique.within`** — what a uniqueness is scoped BY. A natural key is
  almost never unique across a whole table: a role handle is unique per tenant, a
  code per workspace, a registration number per campus. The clause sizes the
  index *and* is held to the pre-check fact's `filters`, which must now be
  exactly `within` + the field, refused in both directions. See *Fixed* for what
  the two halves used to do instead.
- **`children[].fields[].unique`** — uniqueness on a COLLECTION ENTRY, previously
  refused outright. The index is emitted on the entry's table, always led by the
  parent column (an entry has no identity outside its collection, and the same
  value under a different owner is a different, legitimate row), together with
  the constraint binding that makes a duplicate a clean 409. `enforce` is
  `constraint-only`: a pre-check would be a query over the collection's own table
  and this build writes none.

  The refusal pointed at `businessIdentity`, which cannot do this job —
  it is an in-process check over what ONE write carries, so two concurrent
  requests adding the same entry both pass it and both rows land. The author's
  only recourse was an index written by hand, into a migration the generator
  would then describe incorrectly, with **no way to register a binding for it**,
  so the violation surfaced as a raw 500 where the root's equivalent is a 409.
- **Per-entry facts** — `service.facts[].filters` accepts
  `[<collection>.<field>]`, where the collection is a `children[].plural`, and
  emits a port method taking that entry field's type, asked once per entry. It is
  the shape of every "these referenced ids must exist and be active" rule and of
  every per-entry authorization check. `kind: manual` only: a computed fact is a
  query over this entity's own table, and the entry's column is on another one.
- **`authz.bypass`** — a concrete permission that crosses the row scope, on both
  the read and the write side: the platform operator supporting a customer. It
  had to be a key because the caller could not even ask before — the framework's
  `HasPermission` panics on the wildcard a superadmin claim carries, so an entity
  wanting the exception had to parse the raw claim itself. A wildcard here is
  refused, with that reason.
- **`authz.noIdentity`** — `stand-down` (the default: the scope applies to every
  authenticated caller and steps aside only where there is nobody to scope to) or
  `refuse` (enforced even with authentication off). It replaces a hardcoded
  `else` branch that scoped an absent identity to `""`, which meant a freshly
  generated tenant-scoped entity answered **every listing empty on the dev
  bench**, the first place anybody runs it.

  The default matches what every other identity-derived rule this generator
  writes already does — an `ownerCheck` tolerates an absent principal, and has
  to, since with `auth.mode: disabled` no request carries one. A row scope that
  alone failed closed would be the odd one out, and the surprise would land on
  the one profile nobody watches for surprises. It is safe because the guard asks
  about PRESENCE (see *Security*), so the only thing it steps aside for is a
  request with no caller at all. `check` warns on `refuse` instead, since that is
  the setting under which a bench serves nothing — which reads as a broken
  service rather than as a policy.
- **`type: id` is accepted where an identity is compared or assigned** — on a
  `rules.list[].ownerCheck` subject and on an `assignedFrom: identity-subject` /
  `identity-claim` field. Both were refused with the same true and unhelpful
  reason ("an identity is text"), and together they forced a permanent trade at
  the moment the author was least equipped to make it: keep the id and the column
  can carry a foreign key while the declarative rules are unavailable, or take
  the rules and the column becomes a `VARCHAR` that cannot reference a `UUID`
  column on postgres — permanently, since a column's type is a migration over
  live data. The comparison now unwraps with `Value()` and the mapper parses the
  claim into the id.
- **Generated tests for all of it**: a write into a foreign scope refused on
  every write verb (the acceptance criterion the security finding asked for by
  name), an authenticated caller whose token carries no such claim refused while
  an anonymous one stands down, the bypass crossing the scope, and the by-id
  read's scope. The `valid<Entity>()` fixture now states that a request made it,
  by a caller entitled to make it — including the ownerCheck's own principal,
  which it used to leave empty, so those cases passed by standing the rule down
  rather than by running it.

  The gate carries a new fixture — the reported entity itself — that generates,
  builds, boots and is exercised over HTTP (**70 passed · 0 failed · 0
  skipped**). Beside it, a service booted with `auth.mode: jwt` and real RS256
  tokens covers what a gate with authentication disabled structurally cannot:
  **18 passed · 0 failed**, and reverting just the two guard conditions to their
  previous form turns four of them red — a claimless token answering `201` on a
  write into a tenant, and `204` archiving a row it does not own.

### Fixed

- **`unique` emitted a pre-check and an index that contradicted each other.**
  With `enforce: service-precheck+constraint`, the pre-check came from the
  fact's `filters` (`[TenantID, Key]` → per tenant) and the index came from the
  FIELD ALONE (`role_key` → global). Nothing reported it. Tenant B creating a
  role named `administrator` was accepted by the domain, refused by the database,
  and told the handle was already taken — for a tenant where it was free. Every
  multi-tenant entity with a per-tenant natural key landed there, on exactly the
  handles two customers both pick: `administrator`, `owner`, `viewer`. The two
  halves are now held to one list (see `unique.within`), and disagreeing either
  way is a blocker naming the exact clause to paste.

  **This blocks regeneration of an existing spec** whose pre-check fact filters
  by more than the unique field — deliberately, since that spec has the wrong
  index on disk. Declaring `within` changes the index's column list and therefore
  its NAME, and migrations are hooks: the `DROP INDEX` / `CREATE UNIQUE INDEX`
  pair is hand-written, as the report's migration section says.
- **A `service.facts[].filters` entry that named a collection's field was
  silently dropped**, emitting a port method with **no parameter at all** —
  uncallable, for a question it is named after. `check` was green, `generate`
  succeeded, and the tree compiled (a method with no parameter is valid Go), so
  it was discovered three steps and two "the spec is correct" verdicts later,
  while hand-writing the rule that needed it. A manual fact's filters were
  validated nowhere; they are now validated like a computed fact's, every
  parameter is proved distinct, and each of the four spellings the report's
  author tried is refused with the one that works.
- **A `children[].fields[]` of `type: id` emitted a DTO and a test that did not
  compile.** `internal/application/dtos/<child>_input.go` and the collection's
  input test declared `domain.ID` with no import behind it — the CHILD path only;
  the root's DTOs, the aggregate value object and the web-layer child types were
  all correct. It broke the build's own stated invariant, "a green spec compiles
  AND boots".
- **The `_manual` service hook was emitted with no import block** while its
  signatures used `domain.ID`. The file is the author's, but what is handed over
  still has to compile — on a first run it reads as a broken generation rather
  than as a TODO.
- **The generation report now names a manual fact the existing hook does not
  implement.** A hook is written once, so a fact ADDED to a spec that has already
  generated arrives on the port and nowhere else: the package stops compiling on
  a run whose summary calls it successful, and the file is listed under "left
  untouched (yours, by design)", which is true and reads like reassurance.
- **The per-child change verb's response mapper had no generated test** while its
  two siblings did, so the coverage report read 0% for that one function — about
  code the other two paths prove is reachable. A number that says "untested"
  about tested code is worse than a low number.
- **MySQL's active-only unique index no longer rebuilds the table.** The
  condition is materialised as a generated column, and `STORED` cannot be added
  in place: the `ALTER` copies the whole table, which MySQL refuses outright
  (ERROR 1215) on a table that is the CHILD of a foreign key — so a collection's
  active-only uniqueness could not be created at all — and is an expensive copy
  everywhere else. `VIRTUAL` enforces exactly the same thing at no storage cost.

### Changed

- `check` warns when a per-entry collection has **no field outside its business
  identity**: the generated `PUT` can then only replace one entry with another
  while keeping the first one's row id, which is a revoke plus a grant wearing an
  edit's clothes. Said rather than refused — keeping the row through the swap is
  a defensible thing to want, and it is the author's API; what is not defensible
  is finding out by reading the generated routes.
- The generation report states **what a uniqueness is scoped by** ("per
  TenantID"), read off the constraint so the line and the DDL cannot drift. It
  said nothing before, which reads as "across the whole table" whether or not
  that is what was built.
- `explain rules` writes down the fact-naming convention the generated suite
  silently depends on: **name a fact for the problem, not for the healthy
  state**. The suite stubs every probe with "nothing found", so
  `TenantIsUnavailable` passes the happy path and `TenantIsActive` — the same
  question — fails it, against a spec that is perfectly correct.
- `explain keys` marks a **sub-key of a refused block** as refused. Exact-match
  lookup meant `siblings[].fields[].unique.scope` was listed like an available
  key while its parent was refused, sending an author to write it, run, and get
  blocked.

## [0.26.0] — 2026-08-20

### Fixed

- **`ID` filters and orders a listing.** The aggregate id was nameable NOWHERE on the read
  side: `fields[].name: ID` is a reserved name (the framework's managed carrier owns the id,
  and mapping it is a boot panic) and `read.managed` admits only the three timestamps — so
  every read key resolved the name through the declared set, found nothing, and refused it
  with `"ID" does not name a readable field`. True, useless, and about the one column every
  entity has. It was not a framework limit: `TableSchema.ID` maps the logical name, the
  projector writes it as the document's `_id`, and a hand-written service has always been
  able to declare `` ID *string `query:"id" filter:"eq" sort:"asc,desc"` ``. The generator
  simply had no way to say it, so `?orderBy=id` — a listing's cheapest total order, and the
  tie-break the keyset cursor already appends to every key — was inexpressible, and the
  author's next move was to invent a surrogate column for it.

  `read.byParams.filters` and `read.byParams.sort` now resolve `ID` on every entity, ahead of
  the declared set and without anything declaring it. The operators are an opaque value's,
  checked like any other `type: id` field — `eq/ne/in/nin`, and a range refused. The lowering
  resolves it in the same order, so the leaf reaches the request struct instead of being
  dropped after a green `check`, which is the failure the two halves disagreeing used to
  produce.

  The id stays refused under `read.indexes`, `read.fieldRestrict`, `read.computed.from` and
  `controls.search`, and now says why: those name a column in the PROJECTION, and the id is
  not one — every response carries it because the framework puts it there, so an index would
  be declared over a field the document does not have, and a restriction would be asked to
  scrub the handle the response is required to carry. Any spelling of the name lands on that
  explanation instead of on the old refusal, which said nothing about the id at all.

  The gate proves it at RUNTIME, not only at compile time: the boot lane writes a record and
  asserts `?id=`, `?id.in=` and `?orderBy=id` against the live service — the last one
  checking the rows actually come back in id order, because a leaf that binds nothing is
  indistinguishable from the outside up to that point. Both emitted shapes are covered: one
  fixture filters AND orders by the id (the ordering tag rides on the filter leaf), the other
  only orders by it (a vocabulary leaf carrying no value on the wire, which is the branch
  that emits a Go field nothing ever binds). A `sharedbase` case in the matrix covers the id
  as the shared key on the base table.

## [0.25.0] — 2026-08-20

### Added

- **`unique` on a composite value object: one multi-column constraint, and the 409 binding
  that goes with it.** Uniqueness over a tuple was refused — "it would need a multi-column
  constraint, and none is emitted" — so a `UNIQUE(resource, action)` had to be split three
  ways: the pre-check into a hand-written rule, the index into a hand-written migration, and
  the binding that turns a duplicate into a typed 409 into a hand EDIT of the generated
  repository, adopted forever. The last one was the expensive half, and none of it was a
  framework limit: `ConstraintBinding` has always taken any key, and the SQLite dialect has
  always parsed the multi-column form (`"t.a, t.b"`) the engine reports.

  Declaring `unique` on the composite now means uniqueness over the TUPLE, which is the only
  reading a value object admits — "neither half means anything alone" is what made it one
  field. The migration writes one constraint over every part column, `scope: active-only`
  included, in each engine's own spelling: a partial index on Postgres, SQLite and SQL
  Server; one CASE expression per part on Oracle, which omits an entry only when all of them
  are NULL; one generated column per part on MySQL, each carrying its own part's type. The
  repository binds both key forms. With `enforce: service-precheck+constraint` the fact it
  asks for is the one filtered by EVERY part, the gate reads the parts off the value object,
  and the notification lands on the concept rather than on whichever part came first.

- **`immutable` on a composite value object.** It is the one invariant of the entity a value
  object can answer, because it is the only one that asks about the VALUE rather than about a
  part, and Go compares a struct of comparable fields by value — so the rule the DSL already
  had was the right rule, refused for being on the wrong shape. It is refused, with the
  reason, exactly where `==` would compare addresses instead: an optional value object, or
  one with an optional part. The generated immutability test assigns a value object literal
  with one part moved, instead of the scalar it used to invent.

- **`read.managed`: the framework's own timestamps on the read side.** `createdAt`,
  `updatedAt` and `deletedAt` are stamped by the framework and belong to no `fields[]` entry,
  so filtering a listing by them was impossible and the reads returned neither — twice noted
  as a deviation in one project before it was reported. Listing them under `read.managed`
  projects them into the view, returns them from the by-id read and every listing row, keeps
  them in the CSV/XLSX export under a translated header, and makes them filterable like any
  other field. The framework resolved these names on the read path all along; the spec had no
  way to ask. They are declared rather than automatic because they change the view's shape,
  and they are part of the shape hash for the same reason.

### Changed

- **⚠️ The generator targets framework v0.55.0, where the ordering vocabulary moved from the
  Response to the Request.** Every service this generator wrote before now fails its BOOT on
  that framework: the listing declares `query:"orderBy"` — the switch — and no leaf declares
  the vocabulary it switches on, so `ExtractRequestSchema` panics naming the DTO. Regenerating
  against this build is the fix, and the spec needs one new line to do it.

  `read.byParams.sort` was refused by every previous build ("a declared sort allowlist is not
  generated") because ordering came free from whatever the Response happened to render. That
  is exactly what v0.55.0 removed — an unindexed sort is a blocking sort whose cost grows with
  the matching set — so the key is now the ordering VOCABULARY and is required wherever
  `controls.orderBy` is on. The two halves are refused apart, at `check` time, with the half
  that is missing named: a switch with no vocabulary would accept `?orderBy=` and refuse every
  token it could be given; a vocabulary with no switch tags paths that reach no wire.

  A path listed there does not need a filter. One that is filtered gets `sort:"asc,desc"`
  beside its `filter:` tag; one that is not becomes a leaf of its own — orderable, carrying no
  value on the wire and emitting no query parameter — which is how "order by it, do not search
  by it" is now said. The vocabulary draws from the set a filter draws from: the entity's own
  row, its facets, a composite's exposed parts and the columns `read.managed` exposes. A
  collection's field is refused (no single value per row) and a computed field stays refused
  (no column at all), now with that as the stated reason rather than the removed
  `ComputedFieldNotSortableNotification`.

- **A computed read field is now ONE exported function per field, and the write responses
  call it.** The derivation used to be one function per READ SHAPE, each handed a whole
  Result — so an author wrote the same derivation twice, once against plain values and once
  against the listing's sparse pointers, and kept the two in step by hand. Each is now
  `Compute<Field>(ctx, <sources…>)`: a source declared nullable arrives as a pointer, the
  rest as values, and unwrapping whatever a shape holds is the generator's problem — it
  guards a source the caller did not select and leaves the field absent, exactly as before.

  Because there is one function, the WRITE responses call it too: a POST or PATCH now returns
  the derived field it just created instead of forcing a second GET to see it. The exception
  is a derivation reading a `read.managed` column, which no entity carries; that one is
  served by the reads alone.

  ⚠️ **A hook file written before this does not satisfy the new call sites** — the build fails
  naming `Compute<Field>` as undefined. Move each body into the signature the gen-report
  prints and delete the old per-shape functions.

### Fixed

- **The archive endpoint documented an undo that does not exist.** Its OpenAPI summary ended
  in `(reversible)` and its description in "Reversible through unarchive", both fixed strings
  written without consulting `modes`. An entity that archives without unarchiving is a
  legitimate model — the row survives as history and nothing brings it back — and there the
  generator wrote no route, no mutation and no command for the undo it was advertising, in
  the one place a caller looks to find out whether an action can be taken back. Both now
  depend on the unarchive operation existing, and its absence is stated instead of implied.

- **A service fact naming a composite AS A WHOLE resolved instead of being refused.** The
  composite is a declared field, so the filter passed validation and emitted
  `criteria.Eq("Key", e.Key)`: a logical name the view cannot map, and a struct where a
  scalar belongs. It is refused now, with the parts listed.

- **The header of every document a skill writes rendered as ONE run-on paragraph.** The three
  templates opened with consecutive `Label: value` lines aligned by spaces — a table to the
  eye in an editor, and nothing at all to a markdown renderer, which joins consecutive lines
  into a single paragraph. Because the instruction beside them is "copy VERBATIM", every spec
  the skills ever wrote inherited it: `specs/scaffold-entity/<entity>/spec.md` collapsed four
  labels into one line (the worst of the three — `Approved:` is filled with the whole history
  of the gate decisions, so the run-on is long), and `specs/scaffold-system/domain-map.md`
  and `specs/implement/<slug>/plan.md` collapsed two. The `<!-- … -->` comment ending each
  line disappears in rendering too, leaving even less of a visual seam. They are list items
  now (`- **Status:** DRAFT`), which renders as the four lines it always looked like in the
  editor. `scaffold-service` was never affected: its `Status:` stands alone between blank
  lines.

- **`Approved: <pending>` rendered as `Approved:` and nothing else.** The placeholder survives
  in the file until the dev approves, and `<pending>` is an unknown HTML tag to a renderer,
  which drops it — so the one slot whose whole job is to say "nobody has approved this yet"
  read as an empty label. Both surviving placeholders (`Approved:`, `Generation:`) are in
  backticks now; the ones replaced while the spec is written (`<Entity>`, `<x>`) are not,
  because they never reach a rendered document.

## [0.24.0] — 2026-08-19

### Added

- **A composite value object can be written by hand: `valueObjects[].written: manual`.**
  The escape hatch existed for a value that occupies one column (`kind: manual`) and for
  nothing else, so an invariant a composite could not state — "if the resource is `*`, the
  action must be `*` too", a format that depends on ANOTHER part's value, a `String()` that
  renders the concept — had no home: the composite's rule list accepts only `required`,
  `length`, `range`, `comparison` and `requiredIf`, `rules.manual` is refused there, and the
  generated file is owned. `kind: manual` could not answer it either, because a composite's
  `parts` are not decoration: they are what the schema decomposes into columns, what the
  mappers fold, what the migration sizes and what the catalogs translate.

  So the two questions are now asked separately. `kind` says what the value object IS,
  `written` says whose file it is: the SHAPE stays declared in full — parts, part value
  objects, labels, descriptions — and the generator writes no `internal/domain/vos/<name>.go`
  and no test for it. The gen-report asks for the type with the exact struct, its `labelKey`
  tags, and the two things nothing else would say: the FIELD names and types are the contract
  (the mappers build the value as a `vos.<Name>{Part: v, …}` literal), and it must NOT declare
  `Value()` — that absence is what tells the framework to decompose the value instead of
  storing a rendering in one column. Such a declaration carries no `rules`: there is no file
  left to emit them into, and the refusal says where they go.

  Refused on the scalar kinds, where `kind: manual` already says the same thing with one key.

- **A domain-service fact can name a composite's exposed part.** `service.facts[].field`,
  `filters` and `groupBy` resolved against the entity's DECLARED fields, while filters,
  indexes, `?fields=` and `orderBy` had long resolved against the expanded set — so a
  pre-check over the two halves of a pair ("is this resource:action already taken?") was the
  one uniqueness question the language could not ask, and the refusal read as a naming
  mistake. A part is an ordinary column by the time the store sees it, and the emitted query
  filters on it under the name `as:` exposed. Naming the composite ITSELF is now refused with
  the parts listed — it used to RESOLVE, because the composite is a declared field, and
  emitted `criteria.Eq("Key", e.Key)`: a logical name the view cannot map, and a struct where
  a scalar belongs. A declarative `factRange` fills the fact's arguments from the entity
  — `e.<Owner>.<Part>`, unwrapped when the part is a value object — so a part of an OPTIONAL
  composite is refused there and pointed at `rules.manual`, where the absent case is a branch
  someone writes on purpose.

- **`fields[].hidden`: stored, filterable, and in no response.** The only way to keep a field
  out of what a caller receives was `read.fieldRestrict`, which answers a different question
  — 403 for callers without a permission, the field for everyone who has it. `hidden: true`
  is not about who is asking: the field is absent from the by-id read, from every listing
  row, from the write responses and from the CSV/XLSX exports that render the listing, for
  everyone. Everything else is unchanged — the column exists, the migration writes it, the
  filters, sort and indexes reach it, a write may set it, the rules read it, and a
  `read.computed` field may derive FROM it, which is the shape this exists for: filter by
  three columns, return a description and a derived value. Refused on a runtime-only field
  (it is in no response to begin with) and on a field `read.fieldRestrict` also names, which
  would be a permission that unlocks nothing.

## [0.23.0] — 2026-08-19

### Changed

- **Generated code no longer carries the lines it wrote to appease a compiler that was
  never complaining.** Two shapes, both noise a reader had to look past:

  `var _ = time.Time{}` closed every generated DTO, command, child input and command
  test (and `var _ = strings.Repeat` the value-object tests). They existed to excuse a
  `"time"`/`"strings"` import the emitters wrote unconditionally — but every emitted file
  passes through the import pruner, which drops what nothing references. Being a selector
  expression, the sentinel marked the package as used, so it kept the dead import alive
  and left an inert line behind. Without it the pruner decides: the import stays where a
  field actually uses it and disappears where none does.

  `_ = actionName` / `_ = service` / `_ = r` closed `BuildRules` and the `customRules`
  hook — the first thing an author had to delete in a file that is theirs after the first
  write — and the same shape appeared in `Mount<Entity>` (`_, _, _ = app, repo, view`),
  `ToEntity`/`ApplyTo` and `ToCriteria` (`_ = ctx`), and the computed-field stubs. All of
  them blank an unused FUNCTION PARAMETER, which Go has always allowed: only unused local
  variables and imports are errors. Nothing changes about what the code does; there is
  simply less of it.

  A hook file with one gate also no longer opens with a blank line under its signature.

- **⚠️ Every document the tooling writes now lives under `specs/`.** Each skill keeps its
  own directory inside it, so the service root holds the SERVICE and `specs/` holds what
  was decided about it: `specs/scaffold-service/spec.md`,
  `specs/scaffold-entity/<entity>/`, `specs/scaffold-system/domain-map.md`,
  `specs/evolve-entity/<entity>/`, `specs/remove-entity/<entity>/`,
  `specs/scaffold-view/<view>/`, `specs/evolve-view/<view>/`, `specs/implement/<slug>/`,
  `specs/configure/plan.md`, `specs/upgrade/migration-plan.md` (+ `specs/upgrade/rollback/`),
  `specs/qa/` and `specs/omnicore-gen/` (the specs, their gen-reports and `lock.json`).

  Before this, eight working dirs sat loose at the root, interleaved with `internal/`,
  `bootstrap/`, `migrations/` and `devops/` — and telling the tooling's output from the
  service's own source meant already knowing which names belonged to which. The name is
  `specs/` and not `docs/` on purpose: `docs/` belongs to the project's own end-user
  documentation, which has a different audience and a different lifetime.

  **Upgrading an existing project is a move, not a migration:** shift each of those
  directories under `specs/` (`git mv omnicore-gen specs/omnicore-gen`, and the same for
  every working dir the project has) before the next run. Nothing rewrites paths for you,
  and no skill looks in the old location — the generator that finds no
  `specs/omnicore-gen/` reports a project it never wrote into. Generated file headers cite
  the spec by path, so they change on the next regeneration; `lock.json` itself is
  path-agnostic and moves as it is.

  `qa/` moved wholesale, runner included: the suite is now `specs/qa/run.sh` +
  `specs/qa/<entity>.sh`, and it resolves the project root from its own location rather
  than assuming the caller's working directory — a suite two levels down that hard-codes
  `devops/docker-compose.yml` only runs one way.

## [0.22.0] — 2026-08-19

### Changed

- **⚠️ The generator targets framework v0.54.0** (was v0.53.0), and the golden host it is
  proven against pins that line. There is no compatibility branch — one supported version,
  one shape of emitted code. Nothing the generator emits changed shape for it: the release's
  breaking changes land under the emitted code rather than in it (`core.Querier` split, the
  removal of `metadata.Signature()`), and the two that DO reach a generated service were
  already satisfied — `Revision` has always been mandatory on every entity and base schema
  this build writes, and the repositories it emits are `BaseAggregateRepository`, which
  carries the archived-reader capability unarchive now requires.

- **Emitted comments that v0.54.0 made false.** Every root update is now guarded on the
  revision it was loaded with, so two callers replacing a collection through the root no
  longer "lose each other's work" — the second is refused with a 409 and reloads. The
  per-child commands' doc comment said the old thing, and the revision column's own
  comment (spec key, migration comment, table documentation) described a stamp nobody
  read anything from.

### Added

- **`delete.archiveWhen` — an ordinary update that RETIRES the row**, which is v0.54.0's
  `CompleteAsArchive()` made declarable. Naming a field, the value that means "retire
  this" and optionally the value to rest at appends one condition to the end of the
  generated `IfUpdate` clause; the framework then runs the whole archive off that same
  request — the archive stamp, the child cascade, the `ARCHIVED` event the read side
  routes on, and an archive audit entry.

  It lives under `delete` rather than among the rules because it is not a rule: every
  other clause in `BuildRules` decides whether a write is ALLOWED, and this one decides
  what the write IS. Refused when the entity does not declare `archive` in `modes` (the
  framework panics when a rule asks an entity that forbids archiving to complete as one)
  or serves no update at all, and — when the deciding field is an enum declared in the
  spec — the trigger and resting values are checked against its members, so a typo is
  refused rather than compiling into a condition that never matches.

  Two more ways to reach that same silence are WARNED about, because a trigger no update
  can set retires nothing and the generated code looks exactly like the working version:
  the deciding field sitting in `update.patchExcludes` when patch is the entity's only
  shape, and an `immutable` rule over it scoped to update. Warnings and not refusals
  because one path survives both — a row INSERTED already holding the trigger, retired by
  the next update that touches it, whatever it changes.

  **What it does NOT change is which gates the write passed**, and that is the question to
  settle before declaring it. A write is decided by a permission (who may attempt the verb
  on this ROUTE) and by the rules that fire for the verb it turned out to be. This key
  moves neither: the request arrives under the UPDATE's permission, never the archive's,
  and it runs the `IfUpdate` rules — `IfArchive` does not fire on this path, so a rule
  scoped to `archive` never sees it. Declare it when both populations are the same;
  restate the guard with `scope: [update]` when they are not. The gen-report lists it as a
  SECOND removal door and now names the archive-scoped rules of that entity by id, because
  "check the permissions" was the half of the warning that pointed at nothing specific.

  `becomes` exists because the archive rules do NOT re-fire on this path: the row is
  archived holding whatever the closure leaves behind. That is right when the trigger is a
  resting state ("dropped") and wrong when it is a request ("closing"), which is the
  distinction the key exists to let an author make.

- **`fields[].text` — the field's LABEL, per catalog.** A label is the field's short human
  name: it is what a 422 payload puts in `fieldLabel` and what a CSV/XLSX export puts in a
  column header. It takes the same seven catalogs a notification's text does, and unlike
  one it is optional and may be partial — a catalog left out falls back to the field's own
  name, spaced out. Declarable on a persisted field, on a composite's part
  (`valueObjects[].parts[].text`, because the value object owns its vocabulary for every
  entity that uses it) and on a computed read field.

  **This replaces seeding the label from `description:`, which was wrong.** A description
  is a sentence about what the field means — it is what the column COMMENT wants; a label
  is a name. The generator used to copy the description into whichever catalog matched the
  spec's `language:`, so a field documented as "Immutable handle of the tenant; reaches
  URLs, logs and external configuration…" came back in a live validation payload with that
  whole paragraph as its `fieldLabel`, in exactly one language — the other six fell back to
  the field name and were right. The fallback is now what every catalog does, and it also
  stopped splitting runs of capitals (`TenantID` reads as "Tenant ID", not "Tenant I D").

  **The seven catalogs are now declared once**, which is what made the label reachable from
  every consumer without a fourth copy of the list. `spec.Texts.Map` owns the yaml-key →
  catalog-code mapping and `spec.CatalogCodes` owns the order; the notification resolver
  and the translation validator read them instead of spelling the seven out, and the
  emitters take the order from the IR. An eighth catalog added to one and not the other
  used to be invisible — the language would be accepted from a spec, validated against
  nothing and never emitted; a test now fails on the drift instead.

- **`fields[].assignedFrom: derived` — a persisted field the SERVER computes from the
  entity's own fields.** The two identity values already answered "the server fills this,
  so the client never sends it", but only for a value read from the caller's token. A
  public key derived from an immutable handle had no equivalent, so it was generated as an
  ordinary field: it landed in every write DTO and in the published OpenAPI as writable,
  and the caller's value was accepted and silently ignored.

  `derived` takes the field out of every write request, command and OpenAPI request schema
  and emits NO assignment — the generator cannot know the derivation. What computes it is a
  `rules.manual` entry scoped to insert, which the generation report now lists as owed, at
  the top, with what happens if it is never written: the column keeps its zero value and
  nothing reports it. Declaring `identity-subject` and overwriting it in a rule produces
  the right bytes and a spec that lies about where the value comes from; this is the honest
  key for it.

- **`valueObjects[].kind: manual` — a value object this language cannot express, which you
  write.** `reuse` resolves against types the project ALREADY has, so an author who needed
  a value object with a rule no `raw` or `enum` can state (a checksum, a lookup) had no
  legal declaration at all: pointing `reuse` at a type they were about to write was refused
  with "the project declares no value object named X", which reads like a naming mistake
  and is not one.

  A `manual` value object is declared with its name, its backing and a REQUIRED description
  of what it enforces. The generator types the field as it and converts to and from the
  backing exactly as it does for a generated one, and writes no file — a stub that
  validates nothing would pass every check the framework runs, silently. The report asks
  for it by name with the exact shape (`type X <backing>`, `Value()`, `IsValid`) and says
  the backing is a contract — until the type is on disk, at which point it stops billing it
  as blocking the build and asks instead whether what it enforces still matches what the
  spec says it enforces, which is the ask that survives the first run: the generator never
  opens a file it does not own, so a description rewritten to mean something stricter
  leaves a stale rule behind it. It is the loudest of the four escape hatches by design: an
  unwritten rule quietly enforces nothing, an unwritten fact panics on first use, an
  unwritten derivation renders an empty column — an unwritten value object does not
  compile.

  Two consequences the gate found, both about the run AFTER the author writes the type.
  The declaration STAYS legal: the type is now in the project, and the "already declared —
  reuse it instead" refusal would otherwise tell the author to stop asking for the thing
  they were asked to write, so the feature would work exactly once. And no test is
  generated for it: the generator does not know the rule, so anything it asserted would be
  its own guess failing inside the author's file — the one property it could check, that
  the type exists with the right shape, the compiler already checks louder on the same run.

### Fixed

- The gen-report's advice on a translation you edited by hand — "if your version is the
  better one, put it in the spec" — could not be followed for a field label, because no key
  held one. `fields[].text` is that key.

## [0.21.0] — 2026-08-17

### Changed

- **BREAKING — the generator emits the framework's new read anatomy: a Query declares a
  RESULT, and the Response is the single wire authority.** The framework retired the path
  where each transport received a raw view document and decided for itself what to do with
  it, and with it the seat the generator used (`responses.AutoFromDoc`). The read side now
  mirrors the write side, and so does the generated code.
  - **Every read emits a Result type** — `Find<Entity>ByIDResult` / `Find<Plural>ByParamsResult`,
    application-pure, no wire tags, co-located with its query, plus one `<Child>RowResult`
    per collection shared by both reads exactly as `<Child>Row` already was. The Result owns
    field EXISTENCE: a field absent from it can reach no surface, because none of them ever
    sees the document again.
  - **`FromQueryResult` is emitted on both queries** — mandatory on the new interfaces, and
    the one seat where read-side computation may happen. It runs once per DOCUMENT, so REST,
    GraphQL and the tabular exports cannot disagree; the same work in a Response would run
    once per SURFACE.
  - **Both read Responses gain `FromResult`**, delegating to the framework's generic
    name-based mapper (which they opt into by embedding `fwresponses.Auto` — see the
    Added entry below), and every mount passes it where `AutoFromDoc` used to go: the REST
    pair, the CSV and XLSX exports (which take the projection as a new argument, because the
    export's columns now come from the Response instead of the view's `TableSchema`) and the
    GraphQL connection. Both read handlers are generic over two parameters now — the query
    AND its Result.
  - **A by-id request maps `ToQuery(criteria queries.ReadCriteria)`**, the same seat the
    listing has, and the query carries `Criteria` instead of a hand-copied
    `IncludeArchived bool`. The wire vocabulary is unchanged: one reserved control, declared
    by the DTO, everything else a typed 400.
  - **`exportLabelKey` is emitted on every read Response field, recursively.** An export
    column's header lives on the DTO the export projects — with the columns themselves now
    coming from the Response, a field's label had nowhere else to go. The key is the one the
    schema already declares, so the two converge instead of drifting apart.
  - Pointer discipline follows: a listing that serves `?fields=` emits a sparse Result as
    well as a sparse Response, which is what the framework's second boot guard requires.
  - The supported framework line moves to `v0.53.0`. There is no compatibility branch — a
    project on `v0.52.x` is refused with the upgrade path, because the emitted code does not
    compile against it.

### Added

- **Every generated DTO opts into the framework's generic mappers.** A Response embeds
  `fwresponses.Auto` and its `FromResult` becomes `AutoFromResult`; a Request embeds
  `fwrequests.Auto` and its `ToCommand` becomes `AutoFromRequest`. Neither helper compiles
  without the embed — the constraint is a sealed interface — so the opt-in is a claim, and
  the generator can make it honestly: nothing in this language renames a field on one side
  only, since `fields[].name` drives the Go name and the wire name together and
  `parts[].as` renames both halves of a composite at once.
  What it buys back is a check the generator cannot give itself. At Mount, every Request
  field must land on a same-named Command field and every Response field must read from a
  same-named Result field, directly assignable, or the boot panics naming it. **That is a
  regression net over the EMITTERS** — the command side validated nothing before, so two
  emitters drifting apart shipped a silent null. The rule reads by layer: a type in `web/`
  must be fully connected, a type in `application/` may carry more (a path id, an identity
  overlay, a Result field the Response deliberately cuts off the wire).
  Measured on a spec carrying a composite value object at the root, another in a child and
  per-entry verbs: **467 → 394 lines** of generated mapper across the touched files. The
  entry types nested inside a walk (`<Child>Request`, `<Child>Response`) carry no marker —
  it rides the type at the TOP of each walk, which is the one the constructor is handed.

- **A per-entry command is FLAT, and `<Child>Request.ToInput()` is gone.**
  `Add<Child>Command` / `Change<Child>Command` now carry the entry's fields directly
  instead of one nested `dtos.<Child>Input`, because that is what the verb is: the root's
  insert handles MANY entries and carries a slice, a per-entry verb handles exactly ONE
  and the entry IS the command. Flat is also what lets its Request build it through the
  generic mapper — the wire body is flat too, so the last hand-written `ToCommand` in the
  generated tree goes away.
  `ApplyTo` still routes through `dtos.<Child>Input`, built inline from those fields: the
  input type is where a value object is REASSEMBLED (an enum cast, a composite folded from
  its parts), and that reassembly stays in one place. `ToInput()` had no caller left once
  the root's mappers went generic, so it is no longer emitted — the copier recurses
  `[]<Child>Request → []dtos.<Child>Input` by field name.
  New refusal at `check`: a child field named `<Child>ID` under `editStrategy: per-child`
  collides with the path field those commands declare, which would emit a struct with a
  duplicate field — a compile error in code the author did not write and cannot fix.

- **Computed read fields — `read.computed`, a read field no column holds.** The read side's
  `manual` fact: the language declares the shape (`name`, `type`, and `from:` naming the
  STORED fields the derivation reads) and the body is the author's, in
  `internal/application/queries/<entity>_computed_manual.go` — written once, never
  rewritten. Two functions, one per read shape, because a listing serving `?fields=` has a
  sparse Result and a by-id read does not.
  What the declaration alone buys, with no code at all: `?fields=<name>` fetches the sources
  instead of a name no column has, `?orderBy=<name>` is a typed 400 on every surface, and the
  CSV/XLSX export keeps the column under its `labelKey` — which is now fed into all seven
  translation catalogs like any other labelled field, so the header is a translation and not
  an internal identifier.
  It is therefore neither filterable nor sortable nor indexable, and naming it under
  `byParams.filters`, `byParams.sort`, `controls.search`, `read.indexes` or
  `read.fieldRestrict` is refused at `check` with the reason — before the framework's own
  boot guard would say the same thing later and further away.
  **The failure mode is silent, and that is why it is reported twice.** An unwritten rule
  leaves an invariant unenforced; an unwritten fact panics on first use; an unwritten
  derivation renders one column empty on REST, on GraphQL and in the export at once, and
  nothing complains. The gen-report gives it its own section saying so, and
  `explain ownership` now lists three Go hooks rather than two.
- **The GraphQL surface gains the by-id read.** An entity with `read.byId` and a GraphQL
  surface now registers the singular node beside the connection, under the same entity name,
  over the same handler and Response. It was the one read the REST surface served and
  GraphQL did not.
- **Two generated tests per read.** One asserts the Result→Response travel actually happens
  (the framework boot-guards the alignment, but a guard that fires when a service starts is
  a guard nobody sees in a pull request); the other exercises `FromQueryResult` on an empty
  Result — the shape a `?fields=` selection that named none of a computed field's sources
  really produces, and where a derivation that dereferences a source without checking fails.
- **`OMNICORE_LOCAL` on the golden gate.** Points the vendored host at a framework CHECKOUT
  instead of the pinned release, staged once and copied by every lane. Without it there is no
  way to gate the emitters against a framework version that is not published yet, which is
  exactly when the emitters change; the gate now also says so on startup when the host's pin
  and the targeted line disagree.

## [0.20.0] — 2026-08-16

### Added

- **Composite value objects — the generator learned the framework's third VO kind.** A value
  object may now span several persisted columns (`Money{Amount, Currency}`,
  `Period{From, To}`, `Address{Street, City, ZipCode}`), and the language says so in two
  halves that belong to two different owners: `valueObjects[]` with `kind: composite` says
  what the value object IS — its `parts` and its own `rules` — once, for every entity that
  uses it; `fields[].parts[]` says where THIS entity stores it, one `{part, column, as}` per
  part. That split is the framework's own: the domain declares a plain struct that owns its
  rule and nothing else, and the `TableSchema` is the only place that knows it is stored
  across N columns.
  **What it replaces is a workaround, not a gap.** Before, a multi-field concept had to be
  flattened onto the entity with the rule between the fields hand-written in `BuildRules` —
  the rule leaving the concept it belongs to, and nothing stopping the next entity from
  re-deriving it differently. A `comparison` rule between two parts now lives on the value
  object, where it can be reused and cannot drift.
  - **The parts EVAPORATE below the schema, and so does the feature.** One spec field expands
    into one ordinary logical field per part, under the name `as:` exposes it by, and exactly
    four emitters ever learn otherwise — the aggregate struct (one field of the value
    object's type, not N), the `TableSchema` (`Composite(core.NewCompositeValueObject[…]…)`),
    the command mappers (fold the flat wire fields back into the value object) and the
    valid-entity fixture. The migration, the view, the request and response DTOs, the
    listing, the filters, `?fields=`, `orderBy`, OpenAPI, GraphQL and the exports read the
    expanded parts as ordinary fields and never ask — which is the same claim the framework
    makes, held to by the generator.
  - **Every schema position the framework accepts is generated and PROVEN, not assumed**: the
    root, a 1:1 facet, an aggregate child and a shared base. Two golden-matrix cases cover
    them — one flat entity carrying a mandatory composite, an optional one, one in a child
    and one in a facet; one shared-base role carrying one on the identity and one on the
    role. Both generate, gofmt, build, vet and run their generated tests, and the DDL applies
    on all five engines. Three of the four positions cross a package boundary that looks
    alike and is not (the entry lives in `aggregatevos`, the value object in `vos`; the
    shared base is type-less and still names the type), and each needed its own import fix —
    found by the gate, not by reading.
  - **The generated tests cover the kind too.** A composite gets an accepts-a-well-formed-value
    case plus one per declared rule. The positive case is not ceremony: a cross-field rule
    written against the wrong operand refuses everything and passes every negative test
    there is.
  - **Absence is decided as a GROUP, symmetrically.** A composite held as a pointer is
    optional as a whole: the mapper builds it only when at least one part arrived, and the
    read side reconstructs `nil` only when every part column is NULL. The DDL follows —
    every part column of an optional composite is NULL-able, whatever the parts' own shapes,
    because a single NOT NULL among them makes absence impossible to store.
  - **`as:` exists because a part's name belongs to the value object, not to the consumer.**
    The default exposed name is the part's own, which reads right for `Address{Street, City}`
    and wrong for `Money` on a salary field, which would expose `?amount=` — and a second
    `Money`-shaped concept on one entity would collide on it with no way out.
  - Every misuse is refused with the fix named, and the refusals go in both directions: a
    composite declared as a plain `column:`, a scalar VO given `parts:`, a rule of the ENTITY
    declared on a value object (it needs the old state or a service, which a value object
    cannot see), a cross-field rule between two parts left on the entity, one composite split
    across a table and its facet (the once rule), a mandatory composite on a facet (a facet is
    emptied by nilling its fields, and a composite held by value has no nil), a composite in
    `businessIdentity`, a composite named as a filter or a sort (it has no single value — the
    message offers its parts by name). `check` catches each of them before the framework's
    boot panic does.
  - **`prune` and reuse learned the kind at the same time.** The value-object inventory read
    `Value()` to tell a value object from a notification — and a composite declares none, by
    definition. Left alone, a generated composite would have been invisible: `vo: {kind:
    reuse}` would refuse the type as naming nothing, and `prune` would read its file as an
    orphan and offer to delete a type another entity depends on. The inventory now accepts
    `IsValid` as the second half of the test.

### Changed

- **⚠️ The generator targets framework v0.52.0** (was v0.51.0), and the golden host it is
  proven against pins that line. Composite value objects are a v0.52.0 capability — the
  emitted `Composite(...)` chain does not exist before it — so this is the release to take
  together with the framework's. A project still on v0.51.x reads as `Behind` and is refused
  by default (`--force-unsupported` overrides, and the compiler is the oracle).
- **The skills carry the third kind now, everywhere the second one was taught.** The
  scaffold-entity conventions gained it in all six places a hand-written entity meets it —
  the VO taxonomy and the "which kind" decision (`domain.md`), the schema decomposition with
  its five boot panics (`infra.md`), the fold/unfold seam in the command mappers
  (`application.md`), the flat-wire contract (`web.md`), the per-part column nullability
  (`migrations.md`), and what a composite's tests owe (`tests.md`) — plus the §2 spec row that
  keeps a reviewer from seeing N loose fields where the model has one value. `doctor` can name
  the composite boot panics, `evolve-entity` says that renaming an exposed part is a wire
  break while ADOPTING a composite over existing flat columns costs no DDL and no rebuild,
  and `remove-entity` now checks `vos/` for types another entity still reuses before listing
  any of them for deletion — the failure `prune` already had a guard for, on the by-hand path.

## [0.19.0] — 2026-08-16

### Added

- **`omnicore-gen prune` — the cleanup a shrinking spec used to hand a human with no tool.**
  `generate` inserts and replaces but never deletes, deliberately: a run that writes files is
  the wrong moment to remove other ones, and the shared registration files carry other
  entities' content. The cost was real work nobody had a command for — orphaned Go files that
  still compiled and meant nothing, and notification declarations plus labelKeys left in all
  seven catalogs, where a dead translation key is invisible to `check`, to the compiler and
  to every test. `prune` lists the three classes and writes nothing until `-apply`: what it
  would REMOVE, what it would FORGET in the lock (files already deleted by hand — the reason
  `doctor` repeated `is gone` forever), and what it LEAVES ALONE with the reason. It removes
  only text the lock still recognises as the generator's own, byte for byte: hand-edited,
  adopted, claimed by another entity, or a migration — reported, never touched. The lock now
  MERGES what each run wrote into the shared files instead of replacing it, because dropping
  the record at the same moment the text stops being written destroyed the only evidence that
  the leftover was ever the generator's. `evolve-entity`'s codegen path runs it at step 7 and
  its verify now asks for a clean `prune` — the one check that sees both the orphaned files
  and the dead keys.
  **One guard exists because the first answer was wrong.** A value object is declared by one
  spec and REUSED by others, which emit no copy of it — so dropping the field that declared
  it makes the file an orphan by every measure the generator has, while another entity still
  has it as a field type. Prune offered it, and applying that left a project that did not
  compile. It now reads the sibling specs for the value objects they name, keeps the file,
  and says which spec still needs it. The whole scenario is a lane of the golden gate now,
  along with the ordinary half — orphaned collection files and their labelKeys in all seven
  catalogs removed, the tree still building and passing, and a second prune finding nothing.

- **`evolve-entity` can now apply an approved change through `omnicore-gen`**, with the same
  two-option gateway `scaffold-entity` uses and the same beta stance — no default, neither
  option marked recommended: edit `omnicore-gen/<entity>.omnicore.yaml` and regenerate
  (seconds, a fraction of the tokens, reviewed and proved by the agent), or change every file
  by hand. The gateway is offered **only when the entity is the generator's** — Phase 0b now
  runs `omnicore-gen doctor` and reads the lock; an entity nobody generated has no codegen
  path, because regenerating over hand-written files is a rewrite, not an evolution. The
  answer is recorded as `Generation:` in the spec so a resumed run does not ask again, and the
  impact map gains a per-artifact owner (generator-owned · `_manual` hook · hand-written
  either way).

- **What regeneration does NOT do is now stated where it bites, on both sides of the
  gateway.** The code comes back from the spec; the database never does — so the ALTER pair
  is hand-written, in every target dialect, against the shape the gen-report prints (indexes
  included: a uniqueness whose SCOPE changed adds no column, so a shape read only down to the
  columns looks already satisfied). Three more asymmetries an evolution meets and a creation
  does not: a hand-edited owned file is REFUSED by `generate`, so it must be reconciled BEFORE
  regenerating or it silently keeps the old shape while everything around it moves; a
  shrinking spec leaves orphan files (`No longer generated` in the report) that still compile
  and mean nothing; and the generator inserts into the shared notification/catalog files and
  replaces its own entries but never REMOVES, so a labelKey the spec dropped stays behind in
  all seven catalogs. Choosing the by-hand path on a generated entity has its own permanent
  cost, and the option text says it: every owned file touched stops tracking the spec, with
  `adopt … -why` offered at the end so `doctor` tells the truth afterwards.

### Changed

- **⚠️ The generator targets framework v0.51.0** (was v0.49.1), and the golden host it is
  proven against was bumped with it. **This is the one change here that can stop working on a
  project that works today**: a service still pinned to v0.49.x or v0.50.x now reads as
  `behind`, which the compat doctrine BLOCKS by default — the fix is `/omnicore:upgrade`, or
  `--force-unsupported` to generate anyway and judge the result. Nothing else in this release
  changes what an existing tree does. The skew had a visible cost: `scaffold-service` pins the newest
  published release, so every new project was born with the generator warning `ahead` about
  itself, and the gate proved a line no project used. 0.50 and 0.51 are purely additive (the
  authcore Issuer), the emitters needed no change, and the whole matrix — generate, gofmt,
  vet, build, the generated tests, a real boot and DDL on five engines — is green against the
  new pin. The compat fixtures now assert the supported version they were written for, so a
  future bump that leaves them behind fails loudly instead of testing the wrong three
  relations.

- **A childless entity embedded `AggregateRoot`, and now embeds `BaseEntity`.** The two are
  not alternatives — `AggregateRoot` IS `BaseEntity` plus the carrier a root keeps its child
  collections in — and the framework dispatches on the INTERFACE, not the embed: an entity is
  treated as an aggregate when it implements `AggregateRootProvider`, which the generator
  emits only for an entity that HAS children. So the extra embed changed no behaviour, took
  no different write path, and cost nothing at runtime. What it did was tell every reader of
  the file that the entity has collections, in the first place they look to find out — on
  entities that have none. The struct's own doc comment now states which one it embeds and
  why, so the answer is in the file rather than in a maintainer's head.

- **The generated tests now cover what they claimed to.** Six gaps closed, and one of them was
  a bug rather than an omission: the "accepts a well-formed value" test for a string-backed
  value object was never emitted, because the sample lookup compared a field's value-object
  type against the BARE name while a field records it qualified (`vos.Email`) — so the half
  that proves a VO does not reject everything, and the only caller of `Value()`, were missing
  from every entity ever generated. Also added: the aggregate contract the framework itself
  calls (`AggregateChildren`, `RequiresService`, `Add<Child>` — none of which fails loudly);
  the collection input mappers, in a test file inside `dtos` (a package with no test of its
  own reads as 0% however well it is exercised); the result mappers of every write verb, both
  the build-an-entity and the mutate-an-entity shapes; each collection's `CollectionName`,
  which is a PERSISTED key; and the whole per-entry wire surface — Add/Change/Remove requests
  and responses — which was the last generated file sitting at zero while its commands looked
  covered. On the richest model in the matrix that moves the per-file floor from 0% to ≥80%
  everywhere except the four files the generator's own docs declare boot-proven.

- **Coverage is measured with `-coverpkg`, and the skills now say so.** `scaffold-entity`'s
  verify and `conventions/tests.md` asked for a plain `-coverprofile`, which credits a file
  only to tests in its own package: whole test-less packages read as untested while being
  exercised, and a reviewer was sent to write tests for code that had them. The instruction
  now names the flag and says what a 0% on an exercised file actually means — the measurement
  is wrong, not the test missing.

- **The two specs `explain example` prints are in English now**, `language: en-US` included —
  the flat one (Student) and the shared-identity one (Teacher over a Person base, renamed
  from Professor/Pessoa). They are the first thing an agent reads before writing a spec, so
  a Portuguese domain in them was a nudge toward a Portuguese spec in every project. What
  did NOT change: the `text:` blocks still carry real translations in all seven languages —
  that is the point of the key — and the structure, key coverage and comments are the same.
  The shared-identity example is matrix case 18 byte for byte, so the fixture moved with it;
  both were generated into the vendored golden host and pass build + vet + the generated
  tests. One column had to be renamed on the way (`number` → `street_number`): it is a
  reserved word on Oracle, and `check` refused it — the guard working, in the example that
  teaches the language.

- **The generator is no longer described as scaffold-only.** `omnicore-gen`'s skill states
  the two entry points and where they diverge (creating runs steps 1→9; changing EDITS the
  existing spec — never `init`, which refuses without `-force` — and hands the migration,
  the orphans and the non-owned artifacts back to `evolve-entity`). `gen` now refuses a
  storage-moving `generate`, pointing at `evolve-entity`, because regenerating writes the
  code and not the migration and the tree boots green against a table that never moved. The
  gen-report's "changing this entity once it exists — still a manual edit; the generator
  creates, it does not evolve" said the opposite of what now happens and is rewritten in the
  same round.

### Fixed

- **A table's and a column's description never reached the database.** The generated
  migration carried every `description:` as a `-- ` line above the column, which documents
  the FILE and nobody else: the DBA on the catalogue, the BI tool listing columns and the
  next developer opening the table in a client all saw nothing. The description is now
  stored where a connection can read it, per engine — `COMMENT ON TABLE/COLUMN` on postgres
  and oracle, an inline `COMMENT` on the mysql column plus `ALTER TABLE … COMMENT` for the
  table, an `MS_Description` extended property on sqlserver (schema taken from
  `SCHEMA_NAME()` through a declared variable, never hardcoded `dbo`). **SQLite is the one
  engine with nowhere to store one, and is the only one that still keeps the text in the
  file.** Apostrophes are escaped, the text is clamped to MySQL's column limit, and the
  managed columns (`id`, the FK, `revision`, the timestamps, the archive stamp) carry a
  description too — a catalogue that documents half a table is one nobody trusts. Verified
  on all four engines of the generator's own bench: the DDL applies, and the descriptions
  come back out of `pg_description`, `information_schema.columns`, `sys.extended_properties`
  and `user_col_comments`.
  Two consequences, both now written down where they are read: a facet's `description:` was
  being dropped on the way to the emitter and now reaches its table's comment, and a
  REWORDED description is a migration like any other shape change — the code regenerates
  from the spec, the catalogue does not, so it needs a new pair carrying the statement (the
  gen-report, `conventions/migrations.md` and `evolve-entity`'s migration-strategy slot all
  say so). `scaffold-entity`'s migration convention said the opposite — "dialect
  COMMENT-metadata DDL is NOT emitted by default" — and is rewritten, with the per-dialect
  spelling added to each of the five dialect sheets.
  The report's warning about it is per-dialect: it tells a reader targeting postgres, mysql,
  oracle or sqlserver to write the new pair, and tells a SQLite-only project plainly that
  the same reword costs nothing there — an unconditional paragraph that is wrong for the
  reader's project every run is one they learn to skip.

- **A start wrapper could not be drained, and the verification boot inherited that.** Every
  wrapper ran the app with `go run`, which compiles to a temp binary and runs it as a CHILD
  without forwarding SIGTERM — so the boot-then-SIGTERM step that `scaffold-service`,
  `scaffold-entity` and `evolve-entity` all end on exited with no drain narration at all, and
  often left the listener holding the port. It reads as "this service has no graceful
  shutdown" when nothing was ever asked to shut one down. The wrappers now `go build -o
  ./bin/<svc>` and run that (bash `exec`s it, so the wrapper's pid IS the app's), on both
  templates and all three shells, and `shared/boot-contract.md` — the owner every skill
  routes to for shutdown — states the rule with the fallback for a `go run` you are stuck
  with: signal the LISTENER's pid or the process group, never the parent alone. Found by an
  end-to-end trial of the plugin, where a compiled binary drained perfectly and the wrapper
  did not.

- **`scaffold-entity`'s spec template offered a choice the framework does not have.** §8
  phrased the sibling-facet coupling as "COUPLING: if §4 has a sibling, include PUT", which
  reads as a convention a spec may consciously deviate from — and a trial run recorded
  exactly that, PATCH-only beside a facet, as an accepted trade-off. It is not one: PATCH
  cannot assign null, so the facet could be granted and never revoked, and `omnicore-gen
  check` refuses it as a hard blocker. The template now states it as an invariant and names
  the real alternative — drop the sibling and keep the fields nullable on the root — rather
  than letting the manual path promise a flexibility the codegen path then denies.

- **The permission vocabulary was a synonym generator.** `init` handed out
  `<resource>:write` for insert AND patch, which is neither the operation's name nor a
  vocabulary anything else shares — so one entity was granted `write`, the next `create`,
  and a deployment ended up with three words for one verb. The house taxonomy is now stated
  once and used everywhere: **the ACTION spells the OPERATION** — `:insert` · `:update` for
  BOTH put and patch · `:delete` · `:archive` for BOTH archive and unarchive · `:read`. The
  two shared actions are deliberate: PUT and PATCH are one update, and unarchive is the undo
  of archive, so whoever may archive may put it back. It reaches the author at every point
  they might invent a word instead: the `init` template, the two `explain example` specs (a
  field restriction there also became `:read-contact` rather than `:view-contact` — a field
  rule extends the read action, it does not open a vocabulary), the `authz.permissions` key
  doc that `explain keys` renders, and the blocker for a served operation with no permission,
  which now names the canonical string instead of `<action>`. The by-hand path is held to the
  same taxonomy in `scaffold-entity`'s authorization item and `conventions/web.md`.
  **It is a SUGGESTION and every touchpoint says so.** Nothing enforces the spelling — the
  validator still only warns when a permission leaves the resource's namespace, and what it
  blocks is a served operation with NO permission, which aborts boot. Two things outrank the
  default: a taxonomy the project already grants — in any language, matched exactly, since
  that is what the caller's token carries — and the dev simply preferring another spelling,
  which they owe no reason for. In the skills it lands as a `(proposed)` value the dev
  confirms or overwrites at the spec gate, never a string the agent applies silently.

- **The rules hook arrived as one verb gate PER RULE.** Two invariants that both run on
  insert came out as two separate `r.IfInsert(func(){…})` blocks, and a third added a third —
  a file of near-identical wrappers where a reader has to diff the closures to find the
  rules, and where the framework runs the same verb check once per block on every write. Both
  hook files (the entity's and a collection's) now group the residual rules BY VERB: one
  `r.IfInsert` / `r.IfInsertOrUpdate` / … block holding every rule scoped to it, each under
  its own `── <rule-id> ──` header — the shape the generated `BuildRules` beside it always
  had. The file's own doc comment states the rule, so it survives being read without the
  skill, and says what to do with a rule scoped to two verbs: write it as a method and call
  it from both blocks, never paste it twice. The by-hand path is held to the same standard —
  `conventions/domain.md`'s "one clause per mode" bullet was too soft to stop this and now
  names it as the single most common readability failure in a hand-written `BuildRules`,
  with `evolve-entity` told that a NEW rule joins the existing clause for its verb rather
  than opening a second one beside it.

- **A `required` rule on a value-object field made the caller read "Required field"
  twice.** The framework validates every VO-typed field by reflection on every write, and a
  string-backed raw VO answers an empty value with `RequiredFieldNotification` from inside
  its own `IsValid` — so declaring the rule as well adds a second notification for the one
  empty field. An enum is the same shape with a different second message: `""` is not a
  member, so it already answers with the VO's unknown-member notification. `check` now warns
  about both, naming the field and the value object, and the four places an author reads
  before writing a rule say it: `explain rules`, the `omnicore-gen` skill, `scaffold-entity`
  (the rules item and `conventions/domain.md`, which govern the by-hand path too) and
  `evolve-entity`'s routing table. Only value objects the spec itself declares are judged —
  for `vo.kind: reuse` the generator has not seen the `IsValid` and does not guess.

- **`omnicore-gen init` wrote a Portuguese spec into every project.** The template it hands
  the author carried `language: pt-BR`, a field called `Nome`, a rule id
  `nome-obrigatorio`, a collection `Itens`, and — the one that reaches a running service —
  permissions spelled `<resource>:escrever`, `:arquivar`, `:ler`. A permission is matched
  EXACTLY against what the caller's token carries, so a spec that kept them declared
  permissions nobody grants, in a language the project may not speak. The template is now
  English throughout, and says so in its own header: the placeholders are there to be
  renamed into whatever the project speaks and already grants, not to be kept. `init`'s
  closing line and the `omnicore-gen` skill say the same thing, so the agent localises
  `language:`, the names and the permission taxonomy deliberately instead of inheriting one
  language by accident.

- **`explain ownership` promised a prune that did not exist.** It described the shared
  registration files — `wire.go`, the notification declarations, the seven catalogs — as
  "inserted into and removed from"; a WRITE inserts and replaces its own entries and removes
  nothing, deliberately, because those files carry other entities' content and hand edits.
  Harmless while every spec only grew; misleading the moment one shrinks, which is exactly
  what an evolution does. The text and the merge's own doc comment now say what a write does,
  and point at the command that removes — which, as of this release, is a real one.

## [0.18.0] — 2026-08-15

### Changed

- **`scaffold-entity` gained a mandatory generation gateway (1d)**, presented with the
  plan gate as the last stop before any code exists. Two options and no default:
  generate with `omnicore-gen (beta)` and have the agent review it (seconds, far fewer
  tokens, complex rules and their tests still written by hand), or generate file by file as
  before. **Neither is marked recommended**: while the generator is in beta the two are
  presented on their merits and the dev chooses without a nudge. The beta label carries the
  one thing a dev needs to know with it — the generator's gate covers a lot and it can
  still meet a case nobody has met, usually a spec that validates and produces something
  that does not compile — and says what happens then: the agent says so, works around it,
  and it is fixed upstream, which is exactly what the review and proof steps in that option
  are for. The answer is recorded in `spec.md` so a resumed run does not ask
  again. **Nothing about the generator's spec YAML is written before the answer** — that
  is the waste the gate exists to prevent, and on the manual path it is never written at
  all. Everything downstream of the gate — the plan, the conventions, the final verify —
  is unchanged.
- **A generated file carried its header twice.** Every one opened with the sealed header —
  banner, what the file is, entity, spec, date, checksum, and what happens if you edit it —
  and then a second block from the emitter saying the same three things in different words:
  another `Code generated by … DO NOT EDIT.`, another one-line description, another "change
  the spec and regenerate". The only sentence unique to the second block named the rules
  hook, and it was written into every file, including the ones that have nothing to do with
  rules — schemas, catalogs, request mappers. The emitter's block is gone; ~9 lines off the
  top of every generated file, and `package` now follows the header directly. The four
  descriptions that said MORE than their sealed counterpart (the schema and view tests
  explaining that a boot panic is a test failure, the translation coverage test, the facet
  clear command) were moved up into it rather than dropped.

- **Everything the generator reads or writes about a service now lives in one directory,
  `omnicore-gen/`.** It was in three places: the spec in a generic `specs/`, its report
  beside it, and the lock loose at the project root as a dotfile. A reader had to already
  know which tool each belonged to — `specs/` claims a name any other tool could want —
  and the one file whose loss actually costs something was the one hidden from `ls`.
  - `omnicore-gen/<entity>.omnicore.yaml` — the spec, and what `init` writes by default
  - `omnicore-gen/<entity>.gen-report.md` — the hand-off, still written beside its spec
  - `omnicore-gen/lock.json` — was `.omnicore-gen.lock`. Visible now, and no longer
    repeating the name of the directory holding it.

  `-spec` still takes any path, so nothing forces the convention on a caller who means
  otherwise; it is the DEFAULT that moved, along with where sibling specs are looked for.
  The four call sites that resolved these paths now read them from one `internal/layout`,
  because a layout spelled out four times is a layout that moves three times.

- **The generator maintains its own declarations in the SHARED files, and can now tell them
  apart from yours.** `notifications.go` and the seven catalogs belong to every entity, so
  the generator only ever inserted into them — and therefore never updated a declaration it
  had written itself. That is how a notification which gained a `tvars` entry stopped a
  package compiling: the rules emitted for it write `N{Max: "50"}` and the struct on disk
  had no such field, with the compiler pointing at the rule rather than at the declaration.
  The file still has no header and no checksum — nothing seals a file five entities write
  to — so the LOCK now records a hash per declaration and per message, and each one is
  answered separately:
  - recorded hash matches what is on disk → the text is still the generator's own, and a
    spec that moved moves it. Only that declaration is rewritten; everything around it,
    including every other entity's, is untouched.
  - it differs → somebody edited it. Left exactly as it is, and NAMED IN THE REPORT, with
    the note that a stale declaration is what breaks the build while a stale message only
    reads oddly.
  - no record at all → left alone and reported too. Not knowing who wrote something is not
    a licence to overwrite it, and that is every tree generated before this change.

  Inserting is unchanged, which is the part that was never the problem. A translator's
  improved wording still survives regeneration — it simply survives by being recognised
  rather than by the generator refusing to look. Hashes are whitespace-normalised, so
  gofmt realigning a struct when a longer field arrives is not read as an edit.

  Three defects in this bookkeeping were found by reading a real project's report
  against its own files, and fixed before any of it shipped:

    report against its own files.** They compounded: the report claimed work was outstanding
    that had been done, and claimed a hand edit that never happened.
    - **A notification whose semantic is not `validation` is emitted as a struct FOLLOWED BY
      its `Semantic()` method — one unit — and only the struct was read back.** The hash
      recorded for the pair could therefore never match, so every such declaration read as
      hand-edited from its second regeneration onwards, permanently. Six of the twelve in the
      project where it was found. The range now spans the type and the methods written with
      it, stopping at the first declaration that is not the type's.
    - **A declaration TWO entities declare was reported as a hand edit to the second one.**
      Two roles over one identity raise the same notification about the collection they both
      expose, so both specs declare it; the first to run wrote it and recorded the hash under
      its own name. The merge now also consults what the project's other entities recorded:
      recognised as the generator's, left alone (it is not this spec's to rewrite), and
      reported only when the two specs actually disagree about it — which is a real thing to
      know and was invisible before.
    - **The report said "the file was created with a stub for each; the code is yours to
      write" about a hook that had existed for three runs.** A hook is never rewritten, so
      "created now" and "already on disk" mean opposite things to the reader: work to start
      versus work to verify. The generator knows which — it plans the file either way — and
      now says so. It still never opens the file, so the second wording asks for a check
      rather than announcing completion.
    - Also: a catalog rewritten to update a message was described as `0 translation key(s)`,
      because the count only ever counted INSERTED keys. It now distinguishes new from
      updated, through a named result instead of the fifth positional return that made the
      mistake easy.

- **A migration is written ONCE and never regenerated — it is a hook, named
  `NNNN_<entity>_manual.sql`, exactly like the `_manual` rule files.** It is the only output
  whose effect outlives the file: once it has run anywhere, the framework's tracking table
  records it as applied, so rewriting the file changes what the file CLAIMS and not a single
  table. Every other generated file is a claim about code, which the compiler checks. So the
  generator creates a schema and never evolves one; a later change is a NEW numbered pair,
  written by whoever knows where the first one has been. Consequences:
  - **`--migrations=yes|no` is gone.** Its entire job was arbitrating rewrite-vs-not, and
    that question now has one permanent answer. `yes` meant "REWRITE the existing file in
    place", which is the thing being removed.
  - **The false "these are left over" report is gone.** Skipping DDL on a regeneration
    dropped the pair out of the planned file set, so the orphan check — which compares the
    lock against that set — announced the entity's own migrations as leftovers, on EVERY
    regeneration, and advised writing a migration to drop what they created. A hook never
    enters the lock, so it can no longer be orphaned.
  - **The report no longer asks for an `ALTER` on every run.** It now speaks only when the
    SQL was found already on disk, says plainly that a change is a new numbered pair, and
    prints the shape the regenerated code expects as something to compare against — "read
    this as confirmation, not as a task" when nothing about the storage moved.
  - **A pair generated by an older build keeps its name.** The stem gained `_manual` after
    projects existed; the emitter prefers a pair already on disk, because writing the new
    name beside the old one would create a second migration for the same tables.
  - **`Apply` drops a lock record when a path changes class.** Without it, a migration
    recorded as `owned` by an older build stayed recorded, and `doctor` would keep verifying
    a checksum on a file the author is now invited to edit — reporting every edit as drift,
    on the one command whose whole job is telling the truth about drift.

- **scaffold-entity teaches the no-file-names rule at WRITE time, not at lint time.** The
  1c self-lint kept catching the same 1b violation — task files enumerating generated file
  names instead of tables/operations/types — and every run paid a rewrite pass for it. The
  task-file template now carries the ✗/✓ example right where the enumerations are typed,
  and 1c states that a clean grep is the expected outcome, not a fix-up round.

### Added

- **`omnicore-gen`, a spec-driven code generator, ships with the plugin** — Go
  source under `plugins/omnicore/gen/` with a launcher in `plugins/omnicore/bin/`, which
  Claude Code puts on the session PATH, so it is a bare `omnicore-gen` command. It writes
  a whole entity — domain, application, web, infra, migrations, wiring, the seven
  translation catalogs and its own tests — from one YAML spec, in seconds and at a
  fraction of the tokens the file-by-file path costs. It needs no AI and no network: a
  dev can run it by hand. Five invariants govern it, and each one changes how the output
  is treated: nothing generated half-way (every spec key is consumed or refused BY NAME),
  a green spec compiles and boots, boot-traps are static errors, self-sufficient, and it
  owns whole files (the escapes are two named write-once hook files).

  **Hardened before it shipped.** Everything below was found and fixed while the generator was being built, so none of it ever reached a project. It is recorded because each one documents a trap in the design — not because anybody suffered it:

  - **A value object echoed the rejected value through a conversion that has done nothing
    since framework v0.49.1.** `IsValid` emitted `ctx.AddNotification(fieldName, N{}, int(v))`
    / `string(v)`; `AddNotification` renders its variadic with `fmt.Sprint`, and a value
    object is a named type over `string`/`int`/`float64` declaring no `String()`, so it
    already prints as the value it carries. The conversion produced identical text and read
    as if the framework needed the help. The generator requires v0.49.1 or later, which is
    the release that made a pointer of any type dereference correctly, so nothing about what
    the caller sees changes.

  - **The first regeneration on a later day rewrote every owned file.** The header's
    keep-the-date comparison tested the new content against itself-with-the-old-date, which
    only agreed when the dates were already equal — so "regeneration is a no-op" held within
    one day and broke across days, tree-wide. It now compares against the bytes on disk,
    modulo checksum and date, and the golden lanes pin both directions.

  - **Two valid spec shapes produced a tree that did not build, one of them past a green
    `check`.** A write-only entity with a domain service lost the comma between `repo` and
    `svc` in its feature literal (generation aborted on the parse error); GraphQL with
    mutations only and no `read:` block emitted `f.view` on a struct with no such field
    (`check` said yes, `go build` said no). Both are fixed and both are matrix cases now.

  - **MySQL's `active-only` unique typed the shadow column as the ID type.** The generated
    column that materialises "unique among the active rows" was `BINARY(16)` whatever the
    real column was, so any active VARCHAR value longer than 16 bytes failed at INSERT —
    after the DDL applied cleanly. The shadow column now carries the constrained column's
    own type.

  - **Nullable fields broke half the emitters that touched them.** A nullable state field
    made `transition` emit an invalid indirect (written silently); `immutable` on a nullable
    collection field compared pointer identity, so every update of the aggregate was
    rejected forever; a nullable `businessIdentity` field made every re-sent entry read as a
    different one (wholesale replace archived and re-inserted the collection on every PUT);
    the generated patch test compared two freshly minted pointers and failed against a
    correct mapper; `groupCap`'s `only` dereferenced without a nil guard. Comparisons are
    now pointer-safe end to end, `transition` and `groupBy` refuse nullable subjects with a
    modelling fix, and a nullable-everything matrix case pins all of it.

  - **A writable `type: id` field aborted generation, and `required` on one emitted a
    method that does not exist.** The generated tests spelled a composite literal in an
    if-condition (a parse error that killed the whole run) and the emptiness check called
    `IsZero()` where `domain.ID` answers `IsEmpty()`. Test literals now use
    `domain.NewID(…)` and the check calls the method that exists.

  - **Six spellings the language accepted and nothing implemented are now refused, by
    name.** `livesOn: sibling:<x>` (the column landed on the ROOT table while the spec said
    facet), `unique` on a collection or facet field (no index, no precheck, no report line),
    `unique` on a base-lived field (DDL against a column the role's table does not have),
    listing filters on a collection's fields (silently dropped from the request type),
    `read.byParams.filters[].required` and `delete.children` (read by nobody). Each refusal
    names the working alternative; `explain keys` marks them REFUSED.

  - **`service-precheck+constraint` silently degraded to constraint-only.** Without a
    domain service carrying an `exists` fact filtered by the unique field, the precheck
    half was skipped and the report still printed the enforce string as declared. The
    coupling is now validated — and the sharedbase example itself was missing the fact.

  - **`check` and `generate` disagreed in both directions.** `check` never read the lock,
    so a projected-shape change without a version bump answered `canGenerate: true` and was
    refused one command later; `generate` never applied the zero-dialects refusal, so it
    proceeded and emitted no migration files with the report claiming nothing was skipped.
    Both judgements now run in both commands.

  - **The documented `adopt <path> -why '…'` spelling always died.** The CLI's flag
    splitter did not know `-why` takes a value ("flag needs an argument", exit 2); only
    `-why=…` worked. The splitter's list also named a flag that no longer exists.

  - **Transition states were never checked against the enum.** A typo'd state validated,
    generated, and silently never fired; an int-backed enum emitted a string-keyed map
    indexed by an int. States must now be member values of a string-backed enum declared in
    the same spec, and a raw value object's constraint families are cross-checked against
    its backing (`minLength` on an int and `min` on a string were compile errors; `regex`
    on an int was an int→rune conversion that validated garbage silently).

  - **Assorted refusals that used to be silence**: duplicate field names/columns in
    collections and facets, duplicate collection plurals, a facet field shadowing its
    node's field, reserved words in base/child/facet table names and managed columns, two
    spec files declaring one entity (the neighbour dedup was by entity name, so the copy
    passed every collision check), `runtime: true` on a collection field (a DDL column with
    an empty name), `unique` on a bool, `active-only` without an archive column (a plain
    unique that permanently reserved archived values), an owner check against a
    non-string or nullable field, `patchExcludes` covering every field (a generator
    panic), an undeclared notification named by `unique`/VO/duplicate keys (an undefined
    type at build), and a second YAML document in a spec file (silently dropped).

  - **Smaller emitter lies.** The gen-report's target shape described tables the migrations
    would never create (facet lifecycle columns, the root's archive stamp on children,
    facet-owned fields, mounted collections listed as yours to create, a separate-fk role's
    link column missing); per-child OpenAPI summaries interpolated the Go type ("a
    appdomain.Person"); the PTBR catalog was hardcoded as the description's language
    regardless of `language:`; the CSV delimiter was pasted as a raw rune (a quote aborted
    generation); a role keeping every column on the base emitted a schema chain with a
    dangling dot; `doctor`/`adopt` walked lock maps in random order; the notification-
    semantics test was keyed by the ANSWERED semantic, so colliding entries collapsed and a
    wrong pairing could hide; a hand comment with a brace could derail the catalog merge;
    hand-written grouped `type (…)` blocks were invisible to the reuse inventory; and the
    composition-root merge could double the `Translations:` key or silently skip a root
    with a single-statement import.

  - **Every key of the language now carries its doc.** `explain keys` derives from the
    spec struct's leading comments; 179 of 193 fields had none (or had it in a trailing
    position the renderer never reads). All 269 derived key paths render documentation.

  - **Labels for the fields of a collection or a facet were never translated.** The generator
    emits a `labelKey` tag on them and only ever registered the ROOT's keys in the seven
    catalogs, so those resolved to nothing and the raw Go identifier reached the end user —
    `ProposalProponentDocumentField` as a CSV column heading. Nothing reported it: the export
    succeeded and the data was right.

  - **`doctor` reported a hand edit nobody had made.** It judged by the hash the lock records
    per entity, while `generate` judges by the checksum in the file's own header. A file two
    entities legitimately share — the `vos` package doc — goes stale in the first one's record
    the moment the second regenerates, so the one command whose whole job is to tell the truth
    about drift invented some, and the fix it implied was `adopt`, which would have frozen a
    current file out of every future improvement. It now asks the file, like `generate` does.

  - **`vo: {kind: reuse}` could not see a single real value object.** The project inventory
    skipped every generated file — so `UF`, `URL` and the enums a previous entity created were
    invisible — and collected every exported type of the hand-written `notifications.go`, so
    the answer to "which value objects does this project have?" was three NOTIFICATIONS. The
    author of a second role over a shared base was told to reuse one of those, did, and
    validation accepted it. The inventory now identifies a value object by its `Value()`
    method and records who generated it: referencing one is open to everybody, redeclaring is
    refused to everybody except the entity that already owns it, and a `ref` ending in
    `Notification` is named as the confusion it is.

  - **A value object's own notification was emitted into `domain`**, where the package that
    declares the type cannot reach it: the tree did not compile, and the only clue was
    "undefined" in a generated file. It is placed in `vos`, the same way a child-raised one is
    placed in `aggregatevos`.

  - **`comparison` between two non-nullable fields emitted `true && true`**, which `go vet`
    rejects — so a generated project could fail its own checks.

  - **The launcher could serve the PREVIOUS version after a plugin update.** The compiled
    binary is cached in `${CLAUDE_PLUGIN_DATA}`, which survives updates by design, and the
    freshness check was by mtime — only as trustworthy as whatever wrote the files. A
    session could therefore run the old generator against the new skills, silently:
    everything answers, nothing is current. The binary is now keyed by the plugin root,
    which changes on every update, and older ones are removed. Gated: the golden simulates
    an update with backdated sources and fails if the cached binary is reused.

- **`/omnicore:omnicore-gen`** — the skill that drives it: learn the language from
  the binary's own `explain`, write the spec from the approved model, check, generate,
  read the report, implement what was refused, review, and prove it with build + vet +
  tests + a real boot.

- **`help` knows the generator is not the framework, and where to read about it.** The
  skill answers from the version-pinned `/docs`, and `omnicore-gen` has no section there —
  it ships inside the plugin. So a question about the spec language routed through the
  Documentation Map, found nothing, and the honest failure mode was silence while the
  likely one was an answer from memory: a key that does not exist, recommended
  confidently. `help` now states where that authority lives — the binary's own `explain`,
  derived from the language definition and therefore never stale — with the topic per
  question, and it says the generator is in beta and covers less than the framework does.
  It also carries the distinction the wording almost never makes: "how do I cap a
  collection per key?" is the framework when the dev is writing Go and the generator when
  they are writing YAML, and a question about a `.omnicore.yaml` is always the generator's.

- **The generated tests now PROVE the row scope, instead of proving nothing.** An
  `owner-only` or `tenant` entity got one criteria test, and all it asserted was that the
  mapper does not error — which an omitted filter satisfies too. That is the one
  restriction whose failure is silent in both directions: the read still answers, with
  somebody else's rows. A real project's author noticed and wrote the coverage by hand, in
  two files. It is generated now, and it asserts the three ways a row leaks: the filter
  carries the identity that ASKED (checked with two different identities, so a pinned
  constant cannot pass), a value the CALLER sent for that field is overwritten rather than
  merged, and no identity at all yields the empty scope rather than the unfiltered one.
  Gated in turn by a matrix-wide test that the assertion EXISTS — checking for a test by
  name would pass again the day its body stops asserting.

- **`/omnicore:gen` — a door to the generator's CLI for a project that already exists.**
  Five of the six commands were reachable only as steps inside `scaffold-entity`'s codegen
  flow, and `doctor` was reachable from nowhere at all: the word did not appear once in any
  skill. So "is this tree still in step with its spec?" — a read-only, offline, instant
  question — had no way to be asked. The new skill is deliberately small: run one command,
  read its answer, act on it. It documents what each line of `doctor` means and what to do
  about it, that its exit code is NOT the verdict (it exits 0 either way — only `check`
  answers through its status), and that an adopted file prints the cost of the adoption
  every time. Authoring a spec and generating an entity stay where the model and plan gates
  are: `generate` here is allowed only for an entity the lock already knows, and a first
  generation is handed back to `/omnicore:scaffold-entity`.

- **A declared fact can now be ENFORCED declaratively: `rules.list[].kind: factRange`.**
  The generator wrote the query and stopped there — the port answered a number and nothing
  compared it, so every limit over rows in the table was a hand-written clause in the manual
  hook, for an invariant whose shape never varies. The pattern was already in the build
  (`unique.enforce` emits its precheck AND the call); this is the same wiring for the rest
  of the facts. `fact:` names the entry of `service.facts`, `min:`/`max:` say what its
  answer may be, and the generator writes the call — arguments filled from the entity's own
  fields, exactly as the precheck does — the comparison and the notification.
  - all three answer shapes emit a different body, and all three are covered: a plain
    scalar; one carrying `found`, where the rule STANDS DOWN over an empty set instead of
    comparing the zero; and a grouped one, which fires when any group is out of bounds —
    the same invariant as `groupCap`, over rows that are already in the table.
  - the notification is built as `range` builds it: `{min}`/`{max}` are filled from the same
    bounds the emitted code enforces, so the text cannot drift from the check. `echoValue:
    true` sends back the number the SERVICE answered — "the limit is 50" plus "you are at
    51" — and in the grouped form it is the offending group's own value, with `attachTo`
    naming the key field so the caller sees which group.
  - `attachTo` is REQUIRED here: a fact's answer is not a field of the entity, so there is
    no natural place for the notification to land. Declaring `fields` is refused for the
    same reason.
  - refused by name: a fact nobody declared, no bound at all, an `exists` (nothing to
    compare — that one goes through `unique.enforce`), a `manual` fact that returns a
    non-number, the rule on a CHILD's rule set (only the root is handed a service), and a
    spec with the rule and no service.

- **A fact can be computed PER GROUP, by the database: `service.facts[].groupBy`.** The
  language could ask "how many rows match" and could not ask "how many, per category" — so
  an author who needed a distribution had two ways out, and both were wrong: bend
  `rules.list[].groupCap`, which counts what the WRITE carries and not what the table
  holds, or declare a `manual` fact and fold the rows in Go. The framework has had the
  primitive all along and says what it is for — `AggregateBy` is "the write-path primitive
  for business rules over per-group facts", and "fetching the rows to bucket them in Go is
  the anti-pattern it exists to kill". Now `count`, `sum`, `avg`, `min` and `max` all take
  `groupBy: [Field, …]` and emit ONE select with a GROUP BY.
  - the port answers `[]<Entity><Fact>Group`, a struct generated beside it carrying the
    key(s) and the value. It lives in the DOMAIN: a rule that had to name the framework's
    own `*read.Group` would drag infra in to state an invariant.
  - a key is carried as text on every backend — the framework normalises it to a
    driver-neutral value handed over as `any`, and rendering it is the one reading that
    cannot fail on either engine. A key is read to be compared or reported, not summed.
  - no `found` flag, unlike the ungrouped `min`/`max`/`avg`: a group EXISTS because a row
    matched, and an empty set answers no groups at all.
  - refused, each by name: a nullable key (a row without it belongs to no group), a
    runtime-only key (no column to group by), a repeated key, an unknown key, `groupBy` on
    `exists` (the fix names `count`) and on `manual` (no generated query to group).
  - **`groupCap` and a grouped fact now say out loud that they are not alternatives** — in
    the skill, in `explain rules` and in the example: one counts rows in the TABLE, the
    other counts the entries THIS WRITE carries, which no query can see yet. `explain
    rules` also stops claiming `groupCap` "needs a service fact to count them"; it never
    did, and that line is how someone talks themselves into the wrong one.
  - covered by a new matrix fixture (`24-fatos-agrupados`: every kind grouped, a composite
    key, grouped-and-filtered, alongside a `groupCap` in the same spec), by a grouped fact
    on the tree the gate BOOTS, and by a refusal test for each rejected shape.

  The ungrouped kinds needed fixing on the way, and none of it ever shipped:

    `service.facts` has accepted `sum`, `avg`, `min` and `max` since they shipped, and no
    fixture had ever declared one.
    - **Four of the six kind/type combinations did not compile.** The return type was taken
      from the aggregated COLUMN and the framework function named by appending `Int` to the
      kind, so `avg` over an `int64` emitted `read.AvgInt`, which has never existed — an
      average is fractional even over an integer column, which is why the framework offers
      only `Avg`. `sum` over a plain `int` returned a `float64` into an `int` signature;
      `min`/`max` over a `time` or a `string` returned a `float64` into that type, and the
      port did not even import `time`. The return type now follows the KIND — `avg` is always
      `float64`, `sum`/`min`/`max` over any integer width is exact `int64` (narrowing a total
      to the column's width is how one silently wraps) — and a non-numeric field is REFUSED,
      naming the field and its type: the framework carries an aggregate as `CountAgg`,
      `IntAgg` or `FloatAgg` and has no carrier for text, a timestamp or a boolean.
    - **An empty set was reported as zero.** The framework pairs every aggregate with
      `Found`, because SQL answers NULL over no rows: for a sum the zero IS the empty sum,
      but for a minimum, a maximum or an average it is indistinguishable from a real result.
      The port returned `Value` alone, so a rule asking "is the lowest grade below 5?" got a
      yes from an empty table. Those three kinds now answer `(value, bool)`; `sum`, `count`
      and `exists` are unchanged, and the generated stub answers `0, false`.

    The example and the coverage matrix now declare an `avg` and a `max` over an integer
    field, so both paths are generated, built and DDL'd by the gate rather than by a user.

- **`assignedFrom` — a persisted field the SERVER fills from the caller's identity.**
  `assignedFrom: identity-subject` (or `identity-claim` with `claim:`) writes the field on
  insert and leaves it out of every write request and command, so there is no client value
  to ignore and an update cannot reassign it. It is the field an `owner-only` policy reads:
  letting the body carry it means anyone can create a row owned by someone else. It had no
  key, so the invariant went to `rules.manual`, the insert mapper was edited by hand, the
  generated test that asserted the old mapping started failing, and both files were adopted.
  It also closes a hole that was open the whole time it was missing: the validator's own
  fix text recommended "a runtime-only field fed from the caller's identity" for
  `owner-only`/`tenant`, a runtime field has no column, the lowering carried the emptiness
  through, and the emitter quietly skipped the row filter — spec green, report saying
  "owner-only", every permission holder reading every row. That shape is now REFUSED: the
  owner/tenant field must be persisted, the fix text says `assignedFrom`, and the emitter
  panics rather than skips if the invariant is ever violated again.
- **A second role can EXPOSE a collection of the shared identity.** `children[].ownedBy:
  base` under `storage.base.reuse: true` used to be refused outright — "this role reuses an
  existing base, so it does not write the base's schema". True of the SCHEMA, and the
  refusal covered far more: the role needed no table, it needed the collection on its own
  surface. It now mounts: the shape is compared field by field against the spec that
  declares it (a disagreement is a blocker, not a runtime surprise), no table, entry type or
  input DTO is written, and the commands, requests and routes are named `<Entity><Child>…`
  so they cannot collide with the owning role's. The run that motivated this wrote 389 lines
  and four files by hand for exactly this.
- **`groupCap` can count only the entries a rule is about.** `only: {field, equals}`
  restricts what the cap counts, and `groupBy` became optional — with neither, the cap is on
  the collection as a whole. "At most 3 proposals under review" had no expression: grouping
  by the status field capped accepted, rejected and withdrawn at 3 as well, so the rule went
  to `rules.manual` and was written by hand, twice, in two runs of the same domain. The
  bare form — no key, no filter — is accepted only when `description:` says the whole
  collection is the subject, because it is also what you get by forgetting the restriction
  you meant. It is emitted as `len(items)`: the general counting loop's body is nothing but
  `n++` there, which never mentions the entry it ranges over, and Go refuses `declared and
  not used`. No fixture had used that shape, so `student` — a tree the gate BUILDS — now
  carries a bare cap.
- **Per-entry command tests are generated.** The three verbs that address ONE entry had no
  generated tests at all — the root's were thorough, so coverage looked healthy while the
  add/change/remove mappers sat at zero. Two consecutive real runs closed it by hand with
  the same four shapes. They are emitted into the command-test file the generator already
  owns and declare nothing at package scope, so a project that filled the gap itself does
  not break on a name nobody chose; a colliding TEST name is reported by the compiler and
  the report says what it means.
- **Rules on a collection now take the whole DSL**, including `transition`, `skipWhen` and
  `rules.manual` with a hook of the collection's own. `childDuplicate`, `groupCap` and
  `ownerCheck` are refused there BY NAME, pointing at the root — they ask about the whole
  set, and one entry cannot see it. What was there before did not deserve the name: the
  collection had a resolver of its own that had fallen behind the root's, silently dropping
  `Transitions`, `GroupBy`, `Cap`, `SkipWhen`, `AdminField` and `OwnerField`, so a
  `transition` under `children[]` validated, generated its notification and all seven
  translations, and emitted a clause with NO edges — allowing every move. `rules.manual`
  there was parsed and discarded whole: no hook, no call site, no report line. There is one
  resolver now, asserted per ATTRIBUTE. A rule that compares an entry against its previous
  state could not work where it was declared either (`domain.Old` is defined over `Entity`;
  an aggregate child is not one), so `transition` and `immutable` on a collection are
  enforced from the root — pairing surviving entries with their former selves, by id when
  the collection is per-child and by business identity when it is replaced wholesale — and
  the report says where they went. The matrix had only ever exercised `required` and
  `length` inside a collection, which happen to be the two kinds the broken resolver kept.

- **Three tests that check for SILENCE**, in `internal/emit/silence_test.go`, over every
  spec of the coverage matrix at once: every declared rule's notification must be RAISED by
  some emitted line (a clause with no edges, a kind the emitter skips, a resolver that fell
  behind — all leave it unreferenced); every `labelKey` tagged onto a field must exist in
  the catalogs; and no two emitted files may declare one symbol, including across the two
  roles of a shared identity, which is the collision the compiler only reports once someone
  assembles both specs by hand. Each was verified by reintroducing the bug it is named for
  and watching it fail. They widen for free: a new matrix case is new coverage for all
  three.

### Fixed

- **`scaffold-service` shipped a `.gitignore` that hid the reasoning, and only two of the
  eight working dirs had any rule at all.** The generated file
  listed the `scaffold-service/` and `scaffold-entity/` working dirs, so the approved
  model (`spec.md`) and the per-layer plan the service was built from were excluded from
  the repository by default — the result was committed and the decisions behind it were
  not. They are documents the project follows, and a resumed run reads them to know what
  is already done. The line is gone, and the rule is now stated ONCE for everything the
  tooling writes — the working dirs of all eight skills, `omnicore-gen/*.omnicore.yaml`, the
  gen-report, `omnicore-gen/lock.json` and the QA suite — in
  `shared/generated-documents.md`, which the nine skills that write into a project point
  at. It carries the test for the other direction too: if running a command would
  reproduce it byte for byte it may be ignored, and a decision never would.

- **`scaffold-entity/conventions/siblings.md` prescribed an idiom that does not work.**
  For clearing a 1:1 facet on GraphQL it called for a "mini-PATCH mutation" whose
  `ApplyPartiallyTo` assigns nil. The framework's sibling write reads an all-nil facet as
  "untouched" on a partial write and as "delete the row" only on a full one, so that
  mutation answers 200 and changes nothing — worse than not offering it, because the
  caller believes the facet is revoked. The convention now says FULL update handler, and
  states the real reason the surface needs a verb at all: an omitted field and an explicit
  null reach the DTO identically, so "clear this" cannot be told from "leave this alone".
  Found by following the convention literally and testing the result. The generator emits
  that mutation now — `clear<Facet>Of<Entity>`, its command and its tests — for any
  root-attached facet when GraphQL is on, so the contract closes on both surfaces without
  anyone writing it by hand.

## [0.17.1] — 2026-08-07

Full-plugin audit purging **invented conventions** — prescriptive claims with no backing
in the framework's docs or source (every fix was verified against both at v0.46.1).
Trigger: a scaffolded service omitted child collections from `GET` listings, citing an
"idiomatic list shape" that contradicts the framework (the server already holds the full
aggregate when serving a list; omitting children saves nothing and forces N+1 by-id
calls). Grounding rule reaffirmed: skills cite the framework's docs and source ONLY —
never the example repo, which is a test bench invisible to plugin users.

### Fixed

- **`scaffold-entity` — list responses mirror the aggregate.** The "root scalars only"
  convention is gone from `conventions/web.md`: child collections nest in the LIST
  response exactly as in by-id; the recursive `?fields=` guard is met with
  pointer/slice + `omitempty` on nested types, never by amputating the shape. A
  child-less listing is a spec decision, not a default. (`service-layout.html` was
  fixed upstream in the same round — framework `docs/stale-doc-claims`.)
- **SQLite posture is a set of reversible DEFAULTS, not engine laws** (`scaffold-service`
  + templates, `spec-template`, `qa`, `run`, `upgrade`): no `mongo:`/`transport:` block
  is the zero-infra default — but `mongo:` returns if the service declares a
  SharedBaseView (boots, serves empty — no CDC source), and `transport:` + its build
  tag return to SUBSCRIBE to another service's events; both via `/omnicore:configure`.
  The transport tag follows the YAML on every engine, never the engine itself.
- **Stale "tagless build aborts at boot" claims** (`scaffold-service`, `run`): since
  framework v0.40.0 a transport-tagless build boots with a valid no-op transport;
  consumers fail at the point of use with the REAL error string
  (`transport: no transport registered for %q …`) — the fabricated
  "no transport linked" match string and the nonexistent `parsePublicRoutes` symbol
  were replaced everywhere (`shared/boot-contract.md`, `doctor`).
- **Stale pre-v0.44.2 SQLite `go run` bug taught as current** (`sqlite-mvp` template,
  `scaffold-service`, `run`): the migration runner mirrors the engine's DSN resolution
  since v0.44.2 — the dev loop is safe; wrappers' absolute `SQLITE_PATH` stays as
  belt-and-suspenders. The "anti-drift exception" instructing agents to override the
  pinned doc was removed (the doc is right).
- **Invented covering-index law for `EmbedMany`** (`scaffold-view`, `evolve-view`, incl.
  the verify gates): an EmbedMany needs NO index on the declaring view; the per-kind
  truth (1:1 Embed parent join column, EmbedInChild multikey, JoinView leg on the
  SOURCE view) is now stated and routed to `views` at the pin. Index keys documented as
  PATHS (Go segment names + physical leaf), not "physical columns".
- **`shared/read-side.md` + `scaffold-view` + `domain-map-template` — totals routed
  right:** a filtered total over a listing is the `?onlyTotal` DTO opt-in; the
  `Aggregate`/`AggregateBy` DSL lives in `custom-command-handler` (the AggregateLoader
  section), not `custom-query-handler` (dead route).
- **`shared/dialects/sqlite.md` — partial indexes exist on SQLite** (same statement as
  Postgres; the framework's own embedded SQLite migrations use them). The old text
  denied the mechanism and routed to a doc passage that doesn't exist.
- **`shared/boot-contract.md` — the 4th `/readyz` 503 reason**
  (`initializing: view rebuild in progress`) added; `doctor`/`run` aligned.
- **`scaffold-entity` truths restored:** enum VOs are first-class persistable fields
  (only NON-VO named types boot-fail) and have no `IsValid` (membership is
  framework-validated — `tests.md` fixed); the root-archive handler gate is per
  SURFACE, not per aggregate; child ops pick their handler by the operation's field
  contract; separate-FK row multiplicity is the DDL uniqueness choice; `DeletedAt`
  without archive verbs is legal (no vice-versa panic); `Modes()`-column agreement is
  one-directional; enum translation VALUE keys are opt-in, not mandated; the
  self-documenting-DDL "COMMENT ON law" replaced by plain `-- ` comments (dialect
  COMMENT DDL only on request); `example:` tags scoped to scalar fields (composite =
  boot reject); `CHAR(36)` and `path:"id"` verify greps scoped to their real rules.
- **`remove-entity` honesty:** a zero-role SharedBaseView cannot boot — retire it, no
  "bump it empty" option; the role→base RESTRICT FK exists only if the dev's
  migrations declared it (check, don't assert); featureless wiring outside dev is
  rejected only with no `BeforeServe` (third option surfaced).
- **`qa`:** integration events are IN scope (in-TX row always provable; consume side
  when the bench has a live relay — `⚠️ OPEN` otherwise, never silently dropped);
  SQLite read-back expectations split per view backing (a kept SharedBaseView asserts
  "boots, serves empty", not read-your-writes).
- **`doctor`:** the document-store registry guard is profile-split (dev = WARN + boot
  continues + foreign docs leak into reads; elsewhere = abort) — diagnosis updated.
- **Bench templates:** `platform: linux/amd64` on the sqlserver service (Apple-Silicon
  manifest match); relay recovery documented as restart-policy OR post-boot recreate;
  compose `name:` optional; provenance caveat on the unproven sqlserver
  Debezium-Server block (the proven shape is Connect).

## [0.17.0] — 2026-08-07

Alignment with framework v0.46.0 (DTO-governed read controls): universal Relay read
vocabulary (`?orderBy=`, the directional `?first=`/`?last=`
pair, envelope `totalCount`/`hasNextPage`/`hasPreviousPage`/`startCursor`/`endCursor`),
the reserved-control opt-in gate on every surface, and the closed control vocabulary
enforced at boot. The Go/proto renames (`OrderByField`, `PaginationRequest.first`/`last`,
builder `OrderBy()`/`FieldMask()`) need no skill text — no skill names those symbols;
docs-first routing absorbs them per pin.

### Changed

- **`qa` — read-side contract families speak the Relay wire.** Phase 1's coverage
  matrix now names `?orderBy=`, the Relay pagination envelope as the asserted contract
  (window-edge cursors walked via `?after=`/`?before=`), `?last=` alone as the TAIL
  window, and only-total (the "count-only" spelling is retired). The rejected-reads
  family gains the DTO opt-in gate (an undeclared reserved control rejects on PRESENCE
  — `?onlyTotal=false` included), the directional 400s (`first`+`last`,
  `first`+`before`, `after`+`before`; backward navigation is `last`+`before`) and the
  only-total conflict matrix (`?onlyTotal=true` beside a page-shaping control), and
  retires `?limit`/`?sort=` spellings. Phase 0's view inventory now reads WHICH
  reserved controls each list Request DTO declares — declared = served, undeclared =
  typed 400.
- **`scaffold-entity` — the web layer teaches the gate.** New `conventions/web.md`
  trap: read controls are DTO-governed (undeclared reserved control = typed 400 on
  presence; GraphQL SDL-cut; gRPC INVALID_ARGUMENT) and the control vocabulary is
  CLOSED at boot — a top-level `query:`-tagged scalar with no `filter:` tag outside
  the canonical set panics at wrapper construction (a `query:"limit"`/`query:"orderby"`
  typo fails loud instead of advertising a dead OpenAPI parameter). The spec template's
  §9 now records which reserved controls the listing serves, as an explicit low-risk
  spec decision.
- **`scaffold-view` — composed-leg limitation re-spelled**: a segment order on a
  ComposedView leg is named `?orderBy=` (the retired `?sort=` spelling is gone).

## [0.16.0] — 2026-08-06

Alignment with framework v0.45.0 (gRPC pagination mirroring the REST envelope; the
projection consumer skipped when no `transport:` block is configured). The gRPC
envelope rename itself needs no skill text — no skill ever named the proto messages,
and docs-first routing absorbs it per pin; the new surface facts (`search` opt-in,
`only_total` conflict matrix, `has_next`/`has_prev`, enum request seats, 64-bit DTO
seats) are documented in the pin's `grpc` section, which executing agents already
read.

### Added

- **`upgrade` — fifth Phase 2b fallout class: the shared gRPC proto contract
  changed.** `go build` does catch it (the service's generated `.pb.go` loses the
  framework symbol), but the fix is a toolchain step, not a Go edit: re-spell the
  service's hand-written `.proto` against the new pin's shared proto and re-run
  `protoc`/`buf` — never patch a generated `.pb.go` by hand. When the changelog
  preserves field numbers, deployed binary clients keep decoding — the break is
  source-level only, and the plan says so (it changes the rollout conversation).
- **`doctor` — direct boot-log evidence for "writes 2xx, views never arrive".** The
  ladder now starts at the INFO anchor `projection consumer not started: no
  transport configured` (consumer skipped by design; registry/specs/drift still
  run; Mongo-backed views never materialize, relational views unaffected) and
  explicitly separates its twin — `transport:` block present but built without the
  transport tag (no INFO line, "no transport linked" at the point of use). The
  relay → broker → sync walk applies only with block AND tag present. Same anchor
  added to `shared/boot-contract.md`'s Build-tags section and diagnosis quick-map.

### Changed

- **The no-transport posture nuance, stated wherever posture is read** (`configure`
  anchor postures + posture mapping, `scaffold-view` Phase 0, `shared/read-side.md`
  SharedBaseView offer): a `mongo:` block WITHOUT a `transport:` block boots green
  with collections that exist but never receive a row — a bench/QA shape useful
  only to let Mongo-declared views boot, which does NOT count as "Mongo present"
  when offering Mongo-only view kinds. `configure` also stops implying a `mongo:`
  block is illegal on SQLite — impossible there are Mongo PROJECTIONS (no relay
  tails SQLite); the block itself is legal on any posture.

## [0.15.0] — 2026-08-05

The full-plugin audit release: every skill, owner sheet, convention and template was
reviewed against the framework docs at v0.44.1, the reference consumer and the
framework source — ~85 findings (15 of them capable of producing a broken or
unbootable result) fixed across 29 files. Highlights below, grouped by what an
executing agent would have gotten wrong.

### Fixed — factual corrections (the skill said the opposite of the framework)

- **`evolve-view`: flipping a view Mongo→relational DROPS the old collection** — the
  skill claimed it was "left frozen, not dropped", teaching a false rollback story
  (flipping back is a fresh backfill, not a resume). And the **embedder-`Version`
  coupling was inverted**: a `JoinView` leg folds the source view's `Version` into
  the embedder's rebuild hash, so "bump the embedder consciously later" is not an
  option — it is an unconditional forgot-to-bump boot abort; the skill now orders
  bump-both-deploy-once and notes ad-hoc rebuilds don't refresh dependents.
- **`scaffold-view` ran ComposedView through a template it cannot satisfy** —
  `Version(1)`, rebuild discipline and `DeleteOnArchive()` do not exist on a
  ComposedView (never materialized), and it registers via
  `ComposingFeature.ComposedViews()`, not `ReadableFeature.Views()`; both facts now
  stated and routed.
- **The covering-index law was wrong in both view skills**: only the 1:N-leg FK was
  gated, missing the boot-fatal indexes of the 1:1 `Embed` (parent join column),
  `EmbedInChild` (multikey) and `EmbedMany`/`LinkMany` over a `JoinView` leg —
  where the index lives on the SOURCE view, not the new one.
- **`scaffold-entity` conventions taught the pre-v0.40 surface model**:
  `bootstrap.md`/`web.md` still had `Wire` building GraphQL/gRPC registries via
  removed `Wiring` fields — replaced with the feature-declared opt-in interfaces
  (`bootstrap.GraphQLFeature`/`GRPCFeature`, framework-built registries). And
  `aggregate-children.md` told the agent an AVO "has a string id field" — an AVO
  embeds `domain.Managed` and declares NO id field (a hand-declared `ID` compiles
  and silently never persists).
- **Integration events: the SUBSCRIBE half does not ride the CDC relay.**
  `capabilities.md` (and `read-side.md`'s mirror) gated publish AND subscribe behind
  Mongo+relay — consuming needs only a broker + the transport build tag, so an
  infra-free service asking to react to another service's events was getting a wrong
  refusal. The availability sheet now models the three independent posture axes
  (Mongo · broker+tag · relay/tailable engine).
- **"Aggregated view" is not a view kind** — removed from every kind list
  (descriptions, catalogs, the owner sheet, README): counts/totals are the
  relational `Aggregate`/`AggregateBy` DSL, available on EVERY posture — the
  opposite of the "needs Mongo" gate the owner sheet declared.
- **`qa` asserted 405 for every absent verb** — the framework's contract is a
  three-way split (mode missing but route mounted → 403; no route → 404; same path
  under another method → 405); generated suites would have asserted the wrong code.
- **`doctor` contradicted its own owner sheet on readiness** (a `/readyz` 503 carries
  a reason — `rebuilding view` means UP-and-serving, not a store failure) and
  attached the transport-tag exception to SQLite instead of to the absence of a
  `transport:` block (legal on any engine).
- **`run` handed out dead links and green-but-empty boots**: the GraphQL/gRPC enable
  switch is the feature interface, not the yaml block; the bare SQLite fallback
  (`go run` without the wrapper) boots green over an empty schema — it now pins an
  absolute `SQLITE_PATH` like the wrapper does; `docker compose` calls carry
  `-f devops/docker-compose.yml`; stopping names the LISTENER, not `go run`'s parent.
- **`upgrade` omitted `sqlite` from the engine list** and verified the rollback (and
  `go vet`) untagged — an untagged build can be green while the tagged one is broken.
- Smaller factual fixes: `audit.destinations` key shape (a bare sequence under
  `audit:` is a decode failure); "mandatory `mongo`/`transport` blocks" → opt-out by
  absence; `internal/web/dtos` → `internal/web/requests/`; VO-typed response DTO
  fields are framework-legal (the raw-scalar rule is this plugin's convention, not a
  misuse flag); `help`'s framework-repo ground detection (in the framework repo
  `go list -m` RESOLVES with an empty Version — the old condition never fired) plus
  its missing disclosure.

### Added — information an executing agent had to guess

- **`shared/boot-contract.md` — a Build-tags section** (its biggest hole): one engine
  tag always (`sqlite` included), missing engine = boot abort; the transport tag
  follows the `transport:` block and its absence fails at the POINT OF USE on a green
  service; SQLite = `CGO_ENABLED=0 -tags sqlite`. Quick-map rows for both failures.
- **`doctor` — the projection failure-ledger layer**: `omnicore_projection_failures`
  (parked events + failed ripples), the `parkedRetry` replay loop (on by default),
  `reconcile` (off by default) and `ProjectionHealth()` — the missing floor under its
  own headline symptom ("writes accepted, views never arrive" with relay/broker/sync
  all green). Plus new signatures (poison relay message after first boot; dead
  reactions on a green service; cache `failMode: open` swallowing errors) and routing
  rows for declaration boot panics, auth-middleware 401s, HTTP-layer statuses
  (413/408/504), outbound httpclient/grpcclient, cache degradation and GraphQL.
- **`qa` — the boot-and-bench contract it lacked**: build with the yaml-derived tag
  set, bench up first on the Mongo posture, resolve the effective port and probe it
  (something already listening = you may be testing the wrong binary), suite-owned
  throwaway config via `OMNICORE_CONFIG_PATH` (reconciling the "dedicated profile"
  option with never-touch-yaml), state-reset discipline (relational wipe does NOT
  clear the projection; drain before seeding), per-lane namespacing of ports/binary/
  log (not just temp files) + drain-wait before rebinding, `Accept-Language` pinned,
  suites exit non-zero. Coverage matrix grew the golden-record field-round-trip
  family, the FULL typed-400 guard family, `?onlyTotal=`/`?search=`/pagination-
  envelope cases, and `GET /openapi.json` as the wired-verb oracle.
- **`upgrade` — the operational fallout no compiler surfaces**, scanned per version
  range and planned explicitly: required DDL on the service's own tables, demanded
  view rebuilds, the framework's embedded migration sequence growing (non-dev
  `autoRun: check` aborts on purpose), and mechanical yaml key renames (the approved
  plan may now touch `microservice.*.yaml` for exactly that class). Plus a downgrade
  direction guard (read the HIGHER pin's changelog), the module-root `CHANGELOG.md`
  as the per-symbol map, a named snapshot home (`upgrade/rollback/`) with a
  `git status` check before the git-restore path, vendor/`GOTOOLCHAIN` notes, and a
  `/omnicore:qa` offer on the green path.
- **`evolve-entity` — a verification BOOT in the final gate** (every characteristic
  evolution failure is boot-time, not compile-time), the scaffold-side boot-trap
  checklist (sparse-render `omitempty` panic, `Modes()`⟺archive-column lockstep,
  value-typed query param turning required, new-table shape rules), gRPC/proto and
  yaml/wiring as impact-map classes, the multi-dialect migration law (same pair,
  every target folder), the full rebuild-hash routing (root columns move it — routed
  to `views`, whose list is complete), per-kind embedder consequences, the generated
  QA suite as a plannable artifact, and sharedbase→flat demotion routed instead of
  unhandled.
- **`remove-entity`** — the Level-0 reconcile walk (the one mutating skill without
  it), `auth.publicRoutes` and last-feature in the sweep (both non-dev boot
  breakers a dev-profile verify can't catch — now called out explicitly), the
  foreign-collection consequence of "keep the Mongo collections" (non-dev boot
  abort; honest keep = export/move), base-removal and last-role verdicts
  (`OrphanPolicy`, zero-role SharedBaseView), view-registry row hygiene with the
  stale-row re-add trap.
- **Read-side skills** — a read-AUTHORIZATION spec item in both (a composed view
  reaches via join what the caller couldn't query; `ToCriteria`/`crit.Restrict`
  routed), the read-capability axis (segment filter selects on materialized, 400s on
  composed — the docs' trigger to materialize), per-source-kind archive levers
  (`Fields()` is JoinView-only; upstream = external `DeletedAt` + subscription
  `fields:`), external-leg preflight (subscription + linked transport + covering
  `fields:`), index-vs-options bump rules (index-only = no bump; collation immutable),
  healthy post-rebuild boot shapes (`autoRun: check` aborts on purpose; `/readyz` 503
  while rebuilding is not a failure), and gRPC routing rows.
- **`scaffold-entity`/`scaffold-system`** — enum VALUE translation keys as the fourth
  mandatory catalog kind, the cross-aggregate reference rule (bare column + index,
  no DB FK across aggregate boundaries — the canonical example's shape), an
  `OrphanPolicy` spec slot, the identity-view verdict carried into §9 so delegated
  runs stop re-asking, a domain-events (`RegisterEvent`) seam note + routing,
  `views` routing rows (previously unrouted despite owning the ViewDefinition
  surface), integration-events exit route in §9, and SQLite `TEXT` in the id-column
  enumerations.
- **`implement`** — the fourth routing outcome (offered but posture lacks infra →
  `configure`) in the plan template, an infra-prerequisite row and a
  build/run-commands row in the impact map (a first consumer changes the required
  tag set — wiring passes every gate and dies at runtime otherwise), verify against
  the plan's TARGET tags, boot-contract routing for new yaml/routes, exact section
  names in the router examples, and `qa` in the fallback router.
- **`scaffold-service` + templates** — the sqlserver DSN grammar the routed docs
  understate (`database=<svc>_db` + `encrypt=true;TrustServerCertificate=true`,
  full proven shape in `docker-bench.md`), the compose file's required top-level
  keys (undeclared named volumes hard-fail `up`), the Go version floor in preflight,
  `relational.pool` for the unlimited-pool engines in the prd template, Windows
  `NUL` for build sinks, and the strict-decoded block list completed
  (`reconcile`/`parkedRetry`).
- **`configure`** — inherits the scaffold-side traps it lacked: `migrations.dir`
  repoint on an engine swap (silent empty-sequence degradation), tag-gated go.sum
  refresh (`GOFLAGS=-mod=mod`), the SQLite schema-persistence proof (probes pass
  over an empty DB), a docker preflight for the target posture, and a prd
  static-sanity level.
- **`shared/capabilities.md`** — an "Owning docs sections — exact names" block
  (`authz-seams` not `authz`; the middleware chain is a heading inside `httpclient`),
  logs/probes/request-correlation added to already-automatic, the httpclient
  response-cache elicitation carve-out, and the inbound-surface vs outbound-toolbox
  gRPC split.

### Changed

- **`scaffold-view`'s infra-free bullet now carries the SharedBase exception
  inline** — the same "later bolded imperative overrides the owner-sheet gate"
  pattern 0.14.2 fixed for scaffold-entity had one remaining instance: the
  never-refuse/offer-the-upgrade script now defers to `read-side.md`'s elicitation
  contract for SharedBase asks (per-role plain view first, kind framed as a
  complement, `configure` offered only on real multi-role intent).
- **`sqlite-mvp.md` carries an explicit anti-drift exception**: pinned docs exist
  claiming the `go run` dev loop is safe on a relative DSN — true of the engine's
  resolution only; the migration runner resolved against the executable dir
  (framework ≤ v0.44.1), so the template's `SQLITE_PATH` pin OUTRANKS the routed doc
  on this one point and must never be removed by "the doc wins" (framework v0.44.2
  fixes the runner; the pin stays as harmless belt-and-suspenders on fixed pins).
- **`evolve-view`'s SQLite flip guidance** now says the honest thing: enabling Mongo
  alone on SQLite yields a one-time backfill nothing ever updates (no CDC source) —
  the offer is the FULL `configure` conversion, and a silently-stale view is worse
  than a refusal.
- `qa`'s plugin self-check now uses the canonical block (path + published URL + the
  `claude plugin update omnicore@omnicore` remediation) like the other 13 skills.

## [0.14.2] — 2026-08-05

### Added
- **Release notes are now published automatically** (`.github/workflows/release.yml`,
  ported from the framework repo). A `vX.Y.Z` tag — pushed from the CLI or drafted in the
  web UI — publishes the GitHub Release with this file's matching `## [X.Y.Z]` section as
  the body: created from a CLI tag, synced in place when the release came from the UI.
  CHANGELOG.md is the single source of truth, so notes typed into the UI form are
  replaced. Two guards: the run fails before touching any release when the tag disagrees
  with `plugin.json`'s `version` (clients install what the manifest declares, so a
  mismatch would advertise a version nobody can install), and a missing CHANGELOG section
  warns instead of clobbering an existing body. Prerelease tags (`-rc1`, `-beta.2`) keep
  the "Latest" badge off.

### Fixed
- **`SharedBaseView` was offered on Mongo-less (SQLite / zero-infra) projects — again.**
  The gate was already correct in its owner (`shared/read-side.md`, Kinds + elicitation)
  and in `scaffold-entity`'s `sharedbase.md`, and `scaffold-entity`'s own posture invariant
  says "never offer a view kind the posture cannot serve" — but 80 lines later, at the exact
  moment of the question, Phase 1 item 1 ordered the opposite in bold: "ALWAYS offer the
  all-in-one identity read … always surface the option", with no gate and no route to the
  owner. A local, imperative, bolded instruction beats a general invariant stated earlier,
  which is why this recurred. That bullet is now gated on the one question that decides it —
  **does the project HAVE Mongo?** — with the axis spelled out (a full engine whose entity
  views are relational-backed still has Mongo: keep offering there; only a Mongo-less infra
  closes the door) and the no-Mongo script routed to the owner instead of restated. Same
  gate added to the two templates that presented the slot unconditionally: `scaffold-entity`'s
  `spec-template.md` and `scaffold-system`'s `domain-map-template.md` (§3 — where the choice
  is made ONCE at map time, before delegation). Nothing changes on a CDC/Mongo project: the
  offer, both cases (create / add-role + `Version` bump), the tone rule and the by-id +
  by-params pair are untouched.
- **The sibling (1:1) trade-off could be decided silently and never reach the dev.**
  `scaffold-entity` classes siblings as high-risk modeling — "never guess these, PROPOSE
  with a recommended pick and CONFIRM", and explicitly forbids burying them as "defaults
  I'll apply, veto later". But Phase 1 item 2 was phrased as a self-question ("any
  optional/sparse/bulky field group better split…? Name it, recommend, ask"), so an
  internal "no" produced NO output at all: the run considered a satellite for a couple of
  optional fields, dropped it, and moved on. The rule that says to surface the choice lived
  in `conventions/siblings.md`, which by policy is loaded only once the model HAS a sibling —
  circular: you had to have decided in order to read the instruction to offer. Item 2 now
  states that ANY optional/nullable field makes the question one answered in the open —
  deciding NOT to split is the same decision, of the same risk class — with the honest
  threshold (two optional scalars usually stay nullable columns on the root; that is a
  recommendation to SHOW, not a call to bury). The context-load rule now says a sibling is
  offered WITHOUT `siblings.md` loaded, that file marks itself post-decision, and the spec
  template marks `Lives on = root` on an optional field as a decision that must have been
  surfaced.
- **Generated child-mutation methods carried a redundant ensure-initialized call, and
  empty wrappers passed unchallenged.** Two lines (`SKILL.md` traps and
  `conventions/aggregate-children.md`) stated flatly that EVERY child-mutation method opens
  with `domain.EnsureInitialized(root)`. The framework's own contract is narrower: it matters
  only when the method emits a notification BEFORE delegating (`AddNotification` is a no-op
  while the context is nil) — and `Add/Change/RemoveAggregateChild` already ensure-init the
  root themselves, so on a method that only delegates the call is noise. Both lines now say
  when it is needed and when it is not. Separately, nothing pushed back on a wrapper that
  adds nothing at all: a child-mutation method must carry an aggregate-spanning invariant,
  strategy B's by-id guard, or a real domain verb — and a change/archive method taking the
  AVO instead of a childId is now called what it is, the by-id guard MISSING (per-child
  routes with no not-found path), not a style choice. And the guard's HOME is now explicit:
  a `<Verb><Child>ByID` domain method the command calls in one line — not a collection scan
  inside `ApplyPartiallyTo`, which writes the same invariant once per child op in the layer
  that does not own it. That method is also where the change-time duplicate guard belongs:
  the framework rejects a same-identity duplicate on ADD but CHANGE only swaps, so a payload
  can edit one child into another's identity unchallenged.
- **`/omnicore:configure` never honored the promise the other skills make in its name.**
  Three places now tell the dev that a Mongo-only view kind "arrives later via
  `/omnicore:configure`" — `shared/read-side.md`, the generated domain map and the entity
  specs. But `configure` mentioned view KINDS nowhere: it converted the infra, delegated
  backing flips to `evolve-view`, and finished, leaving the dev to work out on their own that
  the identity view they were promised had just become possible. The plan gate now states what
  adding Mongo UNLOCKS (naming the slots this project has on record as `n/a — needs Mongo`),
  and the final verify closes the loop — naming what became servible and routing to
  `scaffold-view` (new kind) or `evolve-view` (backing flip), offering and never auto-creating,
  since this skill writes no view declaration. The removal direction gets the mirror, so no
  conversion changes capabilities silently either way. Not covered by the blind run: proving
  it needs a full SQLite→Postgres+Mongo conversion.
- **Two more decision points that could resolve in silence** — found by sweeping all 11
  high-risk elicitation slots of `scaffold-entity` Phase 1 for the pattern behind the three
  fixes above (a rule stated correctly in an owner sheet, absent or contradicted at the
  moment of the question). Item 3: "independently-managed child → its OWN aggregate, not
  nested" read as a rule the run APPLIES — now a call it must SHOW (nested vs own aggregate
  is a regenerate-everything mistake). Item 10: the surface question never routed to
  `shared/capabilities.md`, whose whole point is stating availability BOTH ways — GraphQL,
  gRPC and exports work on every posture, SQLite included, and refusing an available
  capability "because it's an MVP" is the mirror image of offering an unavailable one. The
  other nine slots hold; item 11 (authorization) is the pattern to copy — it already spells
  out that even the "anyone with the permission" answer must be said out loud.
- **Scaffolded services shipped an OpenAPI spec with no description.**
  `openapi.Config.Description` is what the framework renders as the paragraph under the
  title on `/docs` (omitted from `info` entirely when empty), and no skill owned it:
  `scaffold-service` named only `Title`/`Version`/`LanguageSelector` when writing
  `Wiring.OpenAPI`, and — as that same rule states — no skill downstream ever revisits
  this config, so a service born without a description kept none. Same shape as the
  `LanguageSelector` gap fixed in 0.14.1, one field over. `scaffold-service` now carries
  the description as a low-risk filled slot: ONE sentence taken from the dev's own words
  in the invocation (fallback `<Service Name> service.`), recorded in `spec.md` so it is
  correctable at the gate, and wired into `Wiring.OpenAPI`. Unlike the selector this one
  has no automatic retrofit — it is service intent, which no entity-level skill can
  infer; for a service generated before this fix, add `Description:` to the existing
  `&openapi.Config{...}` in `bootstrap/wire.go`.

## [0.14.1] — 2026-08-05

### Fixed
- **Scaffolded services shipped a Swagger UI with no language dropdown.**
  `openapi.Config.LanguageSelector` (default `false`) opts the rendered `/docs` page
  into the `Accept-Language` selector, and no skill owned it: `scaffold-service` named
  only `Title`/`Version` when writing `Wiring.OpenAPI`, and `scaffold-entity` — the
  skill that wires the translation catalogs, i.e. the very thing that makes the
  dropdown renderable — never revisited that config. The gap was structural, not an
  oversight: the flag is invisible on a translation-less shell (bootstrap populates
  `Languages` from `Wiring.Translations`, and an empty slice renders no selector), so
  neither side could see it was missing. `scaffold-service` now sets it from the shell
  (free there, and the only moment anyone looks at this config), and
  `scaffold-entity`'s catalog step flips it on when absent — which is also the retrofit
  path for services generated before this fix.

## [0.14.0] — 2026-08-04

Two workstreams in one release: the read-side one-owner refactor (below), and a
five-layer framework-vs-skills audit (web · write side · domain · infra/read side ·
bootstrap/ops) whose findings land here as two more owner sheets, two real bug fixes
and ~20 targeted convention/routing additions.

### Fixed (audit)
- **`scaffold-service`'s prd template shipped unreachable probes.** `auth.mode: jwt`
  with no `auth.publicRoutes` — probes are framework-registered but NOT auto-public, so
  a tokenless kubelet gets 401 and the orchestrator kills a healthy pod; a hand-fix with
  a typo then hits the exact-match boot validation and ABORTS boot. The template now
  ships `auth.publicRoutes: ["/livez", "/readyz"]` with the exact-match warning, and
  `doctor`/`run` know both signatures via `shared/boot-contract.md`.
- **`scaffold-entity` pointed at a nonexistent "separate gRPC skill"**
  (`conventions/web.md`, `spec-template.md`) — the gRPC surface is
  `/omnicore:implement`'s job; both lines now say so.
- **`run` polled `/readyz` blind.** It now reads the 503 reason — `initializing:
  rebuilding view` means UP-and-rebuilding (wait), store-unreachable means diagnose —
  instead of timing out a healthy first boot.

### Added (audit)
- **`shared/boot-contract.md`** — one owner for the operational contract: probes +
  `publicRoutes` exact-match validation (+ `Doc.Public` for param routes), the three
  ordered `/readyz` 503 reasons (transport excluded by design), dev-only gates
  (`auth: disabled`, featureless shell), `autoRun`'s three profile-aware modes (prd
  pending = designed abort), interpolation strictness (`${file:}`/`${vault:}` abort,
  env silent), the closed four-env-var set (+ `OMNICORE_MONGO_FORCE_REBUILD` scope),
  strict-decoded yaml blocks, the narrated drain contract, and a doctor quick-map.
  Routed by `scaffold-service`, `run`, `doctor`, `configure`.
- **`shared/capabilities.md`** — one owner for capability choice: availability under
  the posture stated BOTH ways (events/Mongo-kinds/Upstream need Mongo+CDC; httpclient,
  cache, gRPC/GraphQL, authz, audit, tracing, hooks work EVERYWHERE, SQLite included —
  the mirror-image refusal is as wrong as the phantom offer), the service-to-service
  decision matrix + the no-cross-service-command doctrine, integration-event contracts
  (at-least-once ⇒ receiver idempotency is a design question; consumer-group fan-out;
  subscribe⇄receiver boot aborts), the two cache slots (+ `shared.store: memory` boot
  reject, nil degradation), and the already-automatic list (audit, domain events) that
  `implement` now checks BEFORE planning any wiring.
- **`scaffold-entity` conventions — the traps the audit found missing** (all
  version-stable structure; pin stays the authority): the managed-slot contract
  (`Revision` mandatory on root/base, forbidden on child/sibling; `ParentID`'s three
  boot-panics + its auto-projected read-only twin) · reserved `_` column namespace
  (boot failure) + revision DDL line in migrations · SharedBase insert cross-guards
  (handler two-way rejection, blind-insert guard) · one-validation-path rule
  (`ValidateAggregateChild` vs inline, never both) · named Modes patterns (append-only /
  freeze-once / full) as recommendation vocabulary · `IfDisplay`-is-inert caveat ·
  custom `json.Marshaler` = the `json:"-"` Old()-ghost trap by another door · dual-409
  pick rule (Conflict vs STATE-conflict) · authz sweep is boot-FATAL under the switch ·
  the GraphQL-null corollary (a clearable facet + GraphQL demands the intent-mutation
  idiom — otherwise the spec approves a contract one surface can't keep) ·
  `ReadCriteria.Restrict` elicitation in spec §9 · hydration-free aggregate DSL beside
  the `Exists` probe · empty-result name discipline.
- **Routing rows for orphaned sections**: `lifecycle-map` (the SQL↔outbox↔Mongo↔audit
  triage table; PUT/PATCH share audit verb `update`, `actionName` disambiguates) now
  routed by `doctor`, `evolve-entity`, `scaffold-entity`; `logs` by `run` and `doctor`;
  `authz-seams`/CDN-blank-`/docs` by `doctor`; ctx-bound Service probe row in
  `scaffold-entity`; `implement`'s plan template gains the cache-slot and
  receiver-idempotency ⚠️ OPEN elicitation slots; `scaffold-system` decides
  depth-one splits at map time.

### Fixed (verification pass)
- **The audit-round `publicRoutes` fix itself carried a boot-aborting format error** —
  caught by a line-by-line fact-check of the new sheets against docs AND code
  (`parsePublicRoutes` requires `"METHOD /path"`; a bare `"/livez"` aborts boot; one of
  the two proof runs generated the broken form, the other read the docs and got it
  right). `scaffold-service` and `boot-contract.md` now mandate
  `["GET /livez", "GET /readyz"]` explicitly. Every other claim in the three sheets
  verified against the pinned docs/code; one softened (`startFrom` = platform
  offset semantics, not doc-stated).

### Added — the 14th skill: `/omnicore:qa`
- **Contract QA suite generator+runner** — closes the loop the other skills open
  (scaffold builds, run boots, **qa proves**): reads the project's entities, modes,
  views/backings, surfaces and posture, derives the PIN's promised behaviors (verb set
  per mode with 405 for absent verbs, notification-key 422s, the dual 409, archive
  round-trip per regime, filter vocabulary, typed 400 on relational 1:N pushdown), and
  generates `qa/<entity>.sh` + a fail-fast `qa/run.sh` that exercises them against the
  real running service. Posture-aware read-backs (bounded poll for the NEWEST write on
  Mongo backing; IMMEDIATE read-your-writes on relational — a needed poll is itself a
  failure). Plan gate elicits data hygiene and auth; out-of-scope named plainly
  (event consumption, load). Verify includes the mandatory break-one-case honesty
  check (a suite that cannot fail proves nothing) and the reconcile contract.
  `scaffold-entity`'s "functional e2e is a separate step" now routes here.

### Added (second audit round — verify enforcement + the 6 under-audited skills)
- **`shared/verify-contract.md`** — the reconcile rule every mutating skill's Final
  Verify now opens with (generalized from `implement`'s "the plan's own verify step,
  executed"): reopen the run's spec/plan, walk ITS promises item by item with real
  command evidence; an unmet stated target is RED or an explicit dev-accepted
  deviation, never a green summary; numbers measured the way the convention defines.
  Wired as Level 0 into all 7 mutating skills. Teeth added locally: `scaffold-entity`
  Level 3 (per-file <80% = RED; the per-file `go tool cover -func` lines are mandatory
  in the report — the proof run shipped 70.6% on web/requests and sailed through);
  `scaffold-service` gains a static prd sanity check (prd is never boot-tested — say
  so; probe entries verbatim, pure-`${VAR}` endpoints, mandatory blocks).
- **`remove-entity` yaml sweep** — the footprint/inventory/verify now cover
  `microservice.*.yaml` (`integration:` pub/sub, `upstreamSubscriptions`): an orphan
  subscribe after removal is a boot ABORT and event names rarely contain the entity
  name, so the blocks are READ, not grepped.
- **`evolve-entity`**: spec §8 gives the promised-but-processless flat→sharedbase
  promotion a real path (base table + natural-key stakes, FK model, backfill
  migration, bounded-context split — routed to `table-schema` + `sharedbase.md`);
  §4 mirrors scaffold-entity's archive-regime elicitation when Archive is enabled
  later; routing row to `shared/read-side.md`.
- **`evolve-view`**: §3 warns that embedding views do NOT follow a rebuild (each
  listed with its own `Version` verdict); §4 carries the role-set-in-rebuild-hash
  bump rule.
- **`run`**: full-bench handover checks the relay reached streaming — green probes
  exclude the transport by design, so it says plainly when writes won't project.
- **`help`**: posture/availability answers may use the shared owners for the
  plugin-consistent framing; the pin's docs stay the factual authority.

### Added
- **`shared/read-side.md` — the read-side posture gets ONE owner.** New sibling of
  `shared/dialects/` under the same contract (route there, never restate; the pinned docs
  win on version-exact facts). It owns: the two postures and their honest trade-offs ·
  the INVARIANT that the infra posture never constrains write-side modeling (SharedBase,
  children, siblings, modes model identically on every engine — it restricts only which
  view KINDS can be served) · kinds vs plain views (a plain view rooted at a shared-base
  ROLE stays relational-eligible, base fields flattened — it is the `SharedBaseView` KIND
  that needs Mongo, worth it at 2+ roles) · the capability rule (serves what a 1:1 load
  reaches — root, sibling, shared-base; rejects 1:N pushdown as a typed 400) · mechanics
  (loader reuse, `DeleteOnArchive()` is a Mongo-projection knob, the no-lock-in flip, the
  real upgrade path: Mongo + CDC relay via `/omnicore:configure` — an engine swap alone
  does not unlock the kinds) · an **elicitation contract**: what to ASK vs decide
  (backing only when nothing on record; archive regime ALWAYS but gated on backing;
  SharedBaseView offered when servable, pointed-at + upgrade-framed when not) — so the
  agent asks exactly what needs asking, never what the posture already answered.
  **Anti-drift boundary stated in the file:** the version-exact capability/parity table
  is `relational-view` at the PIN — older pins genuinely differ; the plugin never
  restates pin-dependent capability lists again.

### Fixed
- **From a real run: `scaffold-entity` offered a `SharedBaseView` on a zero-infra SQLite
  service** (a Mongo projection, refused at boot with `RelationalSource`) **and asked the
  archive question in Mongo-document vocabulary.** Root cause was the convention itself:
  `conventions/sharedbase.md` said *"ALWAYS OFFER IT"*, unconditional and CDC-framed,
  with no infra-posture branch — while `scaffold-view` already carried the correct
  policy. Now the section is *"OFFER IT WHENEVER IT CAN BE SERVED"*, gated on posture via
  `shared/read-side.md`; `SKILL.md` item 5 settles the view BACKING before the archive
  regime and gates the `DeleteOnArchive()` half on it; Phase 0b carries the write-side
  invariant trigger.
- **Nine stale restatements of the relational-view capability list removed** — all
  claimed "root-only reads / no child/sibling filter+sort", outdated since satellite
  filter/projection (root-level siblings and shared-base fields DO filter/sort/project
  via LEFT JOIN on current pins): `scaffold-entity/SKILL.md` item 5 + gotchas bullet,
  `scaffold-entity/conventions/infra.md`, `.../spec-template.md`,
  `shared/dialects/sqlite.md` §Read side, `scaffold-service/SKILL.md` item 8 + SQLite
  block, `configure/SKILL.md` (full → MVP loss list), `evolve-view/SKILL.md` (flip
  consequence). Each now routes to `shared/read-side.md` (structure) and
  `relational-view` at the pin (version-exact table).

### Changed
- **Skills slimmed to trigger + route** (`scaffold-entity`, `scaffold-service`,
  `scaffold-view`, `scaffold-system`, `configure`, `evolve-view`, `run`, `doctor`):
  posture knowledge deduplicated from ~6 inline copies into the one owner; routing
  tables point at `shared/read-side.md` first, `relational-view` only for version-exact
  answers. `scaffold-service` item 8 and `scaffold-system`'s map-time reads no longer
  force a full `relational-view.html` read on every run — the owner file covers the
  common path, the pin is consulted on demand. No rule or tip was dropped: every inline
  fact either moved into `shared/read-side.md` or stayed where it was skill-specific
  (e.g. `scaffold-view`'s never-refuse upgrade script, `sharedbase.md`'s CREATE-vs-ADD
  offer mechanics + `Version` bump, SQLite DSN/portability guidance).

## [0.13.0] — 2026-08-04

### Changed
- **`scaffold-entity` now teaches clean `BuildRules` idiom + a context-bound Service probe.**
  Additions to `conventions/domain.md` and `conventions/infra.md`, prose (no code — the skills
  never carry code), from a real run whose generated rules had a duplicate-looking
  `IfInsertOrUpdate` and a `return`-to-dodge:
  - `domain.md`: **one clause per mode is the default — group a field's rules together, not by
    mode.** Repeating a mode clause is legal (`IfInsertOrUpdate` just runs its closure each
    time — nothing is registered or overwritten), but scattered blocks read as accidental
    duplication.
  - `domain.md`: **never write code to dodge an automation.** When a rule runs only if a VO
    field has a value, the VO's own automatic check already raises the required — gate the rule
    POSITIVELY (`if a.Field != "" { … }`), never an early `if a.Field == "" { return }`.
  - `domain.md`: **the Service port is an interface with the method ON it, returning PURE VALUES
    (never `(T, error)`), and `BuildRules` NEVER panics or guards the service defensively.** No
    struct-wrapping-a-sub-port; no `(T, error)` shape that forces the domain to handle an infra
    error; no `if !ok { panic }` / `if err != nil { panic }` — `RequiresService() true` + the
    infra compile-time assertion already cover presence and type, so assert-and-use in one line.
    A panic in a domain file is always a bug; the only rejection is a notification.
  - `infra.md`: **the Service impl owns IO failure and FAILS LOUD.** The port returns pure
    values, so an unrecoverable query error surfaces as a `panic` IN INFRA (→ 500 via the
    pipeline recover) — never swallowed into a false/empty answer (which silently skips the
    invariant), never pushed back to the domain to handle. The unique index stays defense in
    depth, never a licence to guess.
  - `infra.md`: **bind the request context to the probe.** Generate the Service impl with a
    nil-able `ctx *configuration.AppContext` field + `ScopedService(ctx)` (shallow copy) and
    query under it, via `persistence.ScopedServiceProvider` — the default shape for any Service
    that queries. Mirror of the read side's `ScopedReaderProvider`.

## [0.12.0] — 2026-08-03

### Added
- **Value objects are now a first-class, self-taught concept in `scaffold-entity`.** A new
  descriptive section in `conventions/domain.md` ("Value objects — PERCEIVE them, never
  inline the rule") teaches the agent to distinguish a **raw value object** (`ValueObject[T]`,
  bespoke `IsValid` — Name/Email/Phone/Document/ZipCode) from an **enum value object**
  (`EnumValueObject[E,T]`, declares `Values()` + an Unknown notification, framework validates
  membership), that VO validation is AUTOMATIC on root AND aggregate value object alike
  (`IgnoreValueObject`/`ValidateValueObject` to opt out/force), and the boundary parse via
  `EnumByValue`. Deliberately more prose than the skills' usual doc-pointer style — VO
  perception is a judgement the agent must make on its own. Routes to the framework's new
  `value-objects.html`. New docs-map rows in `scaffold-entity` and `evolve-entity`, and a
  spec-time "perceive value objects" cue in `conventions/spec-template.md` (§2 Fields).
- **The VO decision criterion is INVERTED — VO is the default for any validated field.**
  `conventions/domain.md` now states the rule as: a field needing ANY validation beyond
  presence/nullability (a format, a bound, a closed set) IS a value object by default; only a
  pure-presence rule or a cross-field invariant stays inline in `BuildRules`; "only one
  aggregate carries it" is not a reason to inline. A deliberately-local one-off shape check is
  the exception — marked `plain` in the §2 `VO?` column and signed off by the dev. A companion
  Final-verify smell check (`scaffold-entity/SKILL.md`) greps for `regexp`/`MatchString` inline
  in a root's/AVO's `BuildRules` and prompts extraction into a VO (investigate, not auto-fail;
  `vos/` is not swept). A field whose valid values are a FIXED, CLOSED set is ALWAYS an enum
  value object (no exception, no `plain`) — framed as a mechanical, property-based test ("are
  the allowed values a fixed list known in advance?"), explicitly NOT a Go-typing question (Go
  has no `enum` keyword; `EnumValueObject` is the framework's construct), so the agent decides
  by fact, not by the ambiguous word "enum".
- **`conventions/tests.md` follows the VO split.** Format/length/range/closed-set coverage now
  lives with the VO (tested DIRECTLY in `internal/domain/vos/` — `IsValid`/membership are plain
  methods), not as `BuildRules` branches; an AVO gets a direct `IsSameBusinessIdentity` test.
- **VO reuse is now investigated up front and approved per field.** `scaffold-entity` Phase 0b
  gains a "existing value objects (`internal/domain/vos/`)" inventory step — a field whose rule
  matches an existing VO REUSES it (never a second copy); a new VO only when none fits.
  `conventions/spec-template.md` §2 Fields gains a MANDATORY `VO?` column
  (`reuse`/`new-raw`/`new-enum`/`plain`) so the VO/reuse decision is visible, editable and
  APPROVED by the dev before generation (a blank cell = an incomplete spec, blocked by the
  existing DRAFT gate).
- **The wire→VO mapping is taught as a plain type CAST, to prevent bloated mappers.**
  `conventions/application.md` gains a "Wire → VO mapping — a CAST, not a constructor"
  section: raw/enum fields convert by a direct cast (out-of-set caught by automatic
  validation, NOT `EnumByValue` by default), nullable fields by a nil-safe pointer cast, and
  the `if x != nil` guard belongs to PATCH's tri-state `ApplyPartiallyTo` ONLY — insert/PUT
  assign unconditionally. `conventions/web.md` adds a Boundary rule that wire DTOs carry the
  VO's underlying scalar (never the VO type; don't import `vos` into `web/`), and
  `conventions/domain.md`'s VO "Boundary" note now frames `EnumByValue` as the optional
  convergence helper rather than the default mapper move.

### Fixed
- **`conventions/aggregate-children.md` no longer claims children are matched by
  `reflect.DeepEqual`.** The framework now matches an aggregate value object exclusively
  through its MANDATORY `IsSameBusinessIdentity` (an interface method — omitting it is a
  compile fail; `GetID` comes from the embedded `domain.Managed`). The trap note now teaches
  business-identity-vs-"did-anything-change", the natural-key-subset choice (vs
  `domain.IsSameByBusinessFields`) and its PUT re-send consequence, and reusing the method as
  the root's duplicate rule. Also: a value object used only by a child still lives in `vos/`
  (never `aggregatevos/` or the child's file); `aggregatevos` imports `vos`, never the reverse.

### Changed
- **Domain-layer layout updated to the THREE-package split** (`service-layout.html`):
  `conventions/domain.md` "Files" now describes `internal/domain/` (root aggregate + its
  `notifications.go` + service port), `internal/domain/vos/` (value objects + own
  `notifications.go` + `doc.go`) and `internal/domain/aggregatevos/` (children + own
  `notifications.go`) — three `notifications.go` by necessity (the `domain` package imports
  the sub-packages, so a shared file would cycle). Replaces the former single-folder,
  single-`notifications.go` description. `conventions/aggregate-children.md` now places a
  child in `aggregatevos/` and notes its VO fields auto-validate (its `BuildRules` carries
  only non-VO rules).

## [0.11.0] — 2026-08-01

### Added
- **`shared/dialects/` — one knowledge sheet per relational engine, the single home for
  per-dialect divergence.** New `plugins/omnicore/shared/dialects/{postgres,mysql,sqlserver,
  oracle,sqlite}.md` (+ a `README.md` stating the contract). Each sheet carries the axes
  where the dialects diverge — id/decimal/boolean column types, the **constraint-violation
  KEY the repo `ConstraintBinding` map binds**, active-only uniqueness, the read-side
  posture — as generic KNOWLEDGE (no SQL/Go code, by the skills' style rule), routing to the
  pinned `table-schema.html` as the authority for exact forms. The generating agent reads
  ONLY the sheet(s) for the service's target dialect(s) instead of wading through
  every-dialect prose with the exceptions as footnotes.

### Changed
- **Per-dialect facts moved OUT of scattered inline prose and INTO the shared sheets.**
  `scaffold-entity` (`SKILL.md` id-typing block + `conventions/infra.md` · `migrations.md` ·
  `sharedbase.md`) dropped their "named `<table>_<col>_key` in EVERY dialect / match by
  NAME" claims — which were false for SQLite — and now route to
  `${CLAUDE_PLUGIN_ROOT}/shared/dialects/<dialect>.md`. `configure` (engine swap) and
  `evolve-entity` (unique field add/remove) gained routing rows into the same sheets;
  `configure`'s engine-swap step now spells out re-keying every `Constraints` map to the
  target dialect's form.

### Fixed
- **`scaffold-service` — the SQLite dev loop no longer boots against an empty database.**
  Under `go run`, a relative `file:app.db` DSN was resolved to DIFFERENT files by the
  migration step and the runtime (the throwaway temp binary vs the project dir), so the
  boot logged `migrations applied` while the served `app.db` stayed empty and every request
  failed `no such table: <entity>`. The `cd`-into-project trick the wrapper relied on was
  not enough. Fixed in `templates/sqlite-mvp.md`: all three start wrappers now pin an
  ABSOLUTE `SQLITE_PATH` next to the script (recomputed each run, so the `.db` still travels
  with the project — portability kept; an explicit `SQLITE_PATH` still wins). And the
  scaffold-service final-verify was hardened — it no longer accepts `ls app.db` as proof of
  persistence (an empty 4096-byte file "appears"); it now inspects the SCHEMA and confirms
  the framework control-plane tables actually landed in the DB the runtime reads.
- **`scaffold-entity` — SQLite services no longer bind unique/PK violations by the wrong
  key.** SQLite reports a violation as the COLUMN LIST (`UNIQUE constraint failed:
  table.column`), never the index/constraint NAME the four SQL engines return — but the
  conventions told the agent constraints are "named `<table>_<col>_key` in EVERY dialect"
  and the repo "binds by NAME". On a SQLite service the agent therefore named its indexes
  `<table>_<col>_key` and bound those names, which SQLite never emits — the lookup missed,
  the raw DB error escaped unmapped, and the intended custom 409 became a generic 500 (it
  compiled and booted; only a duplicate INSERT revealed it). Fixed at the root: the shared
  `sqlite.md` sheet states the `table.column` bind-key rule prominently, the by-NAME claims
  are gone, and the Level-1 checklist gained a guard — on a SQLite service, a repo
  `Constraints` key ending `_key`/`_pkey` or the literal `PRIMARY` must hit NOTHING.
- **`scaffold-entity` — domain structs no longer get `json:` tags.** The "a persisted
  field carries `labelKey` and nothing else" rule was buried as a sub-clause of a dense
  bullet in `conventions/domain.md`, and nothing in the pre-boot checklist caught a
  violation — so the strong Go reflex of stamping `json:"..."` on every struct field won,
  and generated aggregates came out with `json:"..." labelKey:"..."` on every field (the
  canonical example's domain carries `labelKey` only). Two reinforcements: the tag rule is
  now a loud standalone rule that names the reflex and the layering reason (wire names live
  on the web-layer DTOs; a `json:"-"` even corrupts the `Old()` snapshot the framework
  builds via a json round-trip), and the Level-1 mechanical checklist gained a
  `grep -rn 'json:"' internal/domain/` → NOTHING guard (also sweeping `db:`) so a slip is
  caught before boot. `scaffold-system` inherits the fix (it delegates per entity to
  `scaffold-entity`).

## [0.10.2] — 2026-08-01

### Fixed
- **`scaffold-service` — the Phase 1 questions are now staged on the dialect
  instead of dumped in one flat round.** The old "ask in ONE consolidated round"
  rule contradicted the skill's own SQLite carve-outs (transport "asked ONLY when
  the engine is not SQLite", "no bench for SQLite", read-side "forced relational on
  SQLite"): a slot can't be skipped based on an answer collected in the same round,
  so the agent asked transport / broker / read-side alongside the dialect and
  papered over it with "(ignored if sqlite)" parentheticals — forcing a dev who
  picked SQLite to answer a broker and a read-side posture that get thrown away.
  Phase 1 now asks in two staged rounds: **Round 1** = identity + the dialect pivot
  (name, module, dialect); **Round 2** branches on the answer — SQLite asks only the
  slots that still exist (SQLite DSN, surfaces, wrappers), a full engine asks
  transport + bench + read-side + surfaces + wrappers. No more answer-to-discard,
  no more "ignored if sqlite" parentheticals. (Slots #4/#6/#8 already carried the
  correct SQLite semantics — they were simply unreachable behind the single-round
  rule.)

## [0.10.1] — 2026-08-01

### Changed
- **`scaffold-service` — SQLite DSN guidance rewritten for correctness, and the
  final verify now exercises the real file DSN.** Matches the framework fix that
  makes a relative `relational.dsn` resolve against the working directory under
  `go run` (so the dev-loop `app.db` persists in the project instead of a temp
  build dir). The skill + `templates/sqlite-mvp.md` now state the rule plainly:
  **the `.db` always lives in the app's own folder** — a relative `file:app.db`
  (the default) resolves next to the binary (portable, travels with it), and the
  dev loop lands it in the project; an absolute path is only an escape hatch for a
  fixed external location (not portable). The final-verify step boots the REAL
  `file:app.db` (not `:memory:`) and confirms the file appeared — the persistence
  it was silently failing to check before. The start-wrapper comments say where
  `app.db` is created.

## [0.10.0] — 2026-07-31

### Added
- **Relational-view awareness across the view-shaping and project-init skills** —
  the framework's `.RelationalSource(...)` read model (a plain view served straight
  from the SoR, read-your-writes, the deliberate CQRS exception for MVPs and
  freshest-possible dashboards) is now a first-class decision the skills teach and
  route, always docs-first against the pin's `relational-view` section:
  - `scaffold-service`: a new neutral, no-default Phase 1 question — **read-side
    posture** (full distributed CQRS, Mongo-projected · reduced/MVP, relational
    from the SoR) — recorded in the spec as the default backing for entity views.
    Framed as no lock-in: the bench ships full either way, so moving a view to
    Mongo later is a per-view flag (drop the marker + bump `Version` ⇒ one automatic
    online blue-green rebuild).
  - `scaffold-system`: the posture is decided ONCE at system altitude (`§1p` of the
    domain map, read from `scaffold-service/spec.md` when it just set one) and handed
    to every delegated run as the default view backing (per-entity overridable).
  - `scaffold-entity`: honors the posture when emitting the plain per-entity view —
    relational (`.RelationalSource(repo.Loader)`, root-only reads, no collection) vs
    Mongo — asking once when no posture is on record; reuses the aggregate's existing
    loader (boot guard `BoundTable()==schema.Table()`), never a second one.
  - `scaffold-view`: teaches the LIMITATION — every composition type it creates
    (ComposedView, SharedBaseView, the Embed/Link family, Upstream, aggregated) is
    relational-ineligible (boot fail, 400, or a different declaration type), so the
    option is never offered here; a plain single-aggregate listing routes to
    `scaffold-entity` instead.
  - `evolve-view`: the FLIP — adding/removing `.RelationalSource()` is a shape change
    (`Version` bump) with its two drift transitions taught (`DriftRelationalSync`,
    no rebuild ⇄ `DriftRebuildRequired`, full online blue-green rebuild).
  Mechanics stay in the pinned docs — the skills only force and route the decision;
  the capability applies on any pin that ships `relational-view`.
- **SQLite zero-infra MVP + infrastructure-posture awareness, plus a new `configure`
  skill** — the framework's SQLite engine and infra-optional boot (single pure-Go
  binary, one `app.db` or `:memory:`, no Docker/Mongo/broker/relay) are now first-class
  across the plugin, and every skill is **capability-aware, never capability-gated**:
  it warns of a posture's consequences, then OFFERS to enable what's missing (delegating
  `/omnicore:configure`), never refuses — every conversion reversible, no code lost.
  - **new skill `/omnicore:configure`** — converts a service's infrastructure posture in
    either direction (zero-infra/SQLite MVP ⇄ full distributed CQRS: add/remove Mongo +
    broker + CDC relay + docker), swaps the relational engine (porting migrations to the
    target dialect; data ETL flagged as the dev's), switches transport (kafka ⇄ nats),
    and tunes the `microservice.*.yaml` / devops glue. Docs-first, plan-gated; delegates
    each view flip to `evolve-view`, verification to `run`; reuses `scaffold-service`'s
    devops templates.
  - **new template `scaffold-service/templates/sqlite-mvp.md`** — the SQLite zero-infra
    glue: `CGO_ENABLED=0 -tags sqlite` start wrappers (no compose), `file:app.db` /
    `:memory:` DSN, `.gitignore` for the `app.db*` sidecars.
  - `scaffold-service`: `sqlite` joins the engine set as the decisive zero-infra MVP
    engine — picking it collapses the transport/bench/read-side questions (no Mongo, no
    broker, no Docker; tagless), records the posture, and states plainly that it's not
    optimized for it (canonical path is CDC + Mongo), that integration events + Mongo
    projections belong to the standard path, and that switching later is a reversible
    `/omnicore:configure` run.
  - `scaffold-view` / `evolve-view`: on an infra-free project a Mongo-only view (or a
    flip to Mongo) is never refused — the skill offers to enable Mongo via `configure`.
  - `implement`: a capability the framework offers but the current posture lacks the
    infra for (integration-event publish without a broker, anything Mongo on SQLite) is
    NOT an honest-no — it offers `/omnicore:configure` to enable it.
  - `run`: follows the chosen infra — a SQLite/infra-free project boots with no bench
    (`CGO_ENABLED=0 -tags sqlite`, no compose), and absent-by-design infra is never
    reported as unreachable.
  - `scaffold-system` / `scaffold-entity` / `doctor`: posture-aware — the domain map
    records the engine/infra choice (Mongo views + integration events deferred, never
    dropped); SQLite type/DDL specifics route to `table-schema`; and "writes 2xx, views
    never arrive" on an infra-free project is diagnosed as by-design, not a fault.
  Mechanics stay in the pinned docs (`relational-view`, `yaml-reference`, `transport`,
  `table-schema`, `integration-events`); the skills only teach, route and offer.

## [0.9.0] — 2026-07-30

### Added
- **Explicit ARCHIVE-regime decision gates** in the view-shaping skills — the
  read-side archive behavior is never left to a silent default:
  - `scaffold-view`: the spec gate's Consistency contract now forces, per
    embedded/linked segment, the follow-the-source vs retain-regardless
    decision (plus the view root's own kept-hidden vs `DeleteOnArchive()`
    choice), routing to the pin's `views` section for the exact lever;
  - `evolve-view`: the impact map flags when a change to a segment's projected
    fields or lifecycle FLIPS its archive regime — a shape change (`Version`
    bump ⇒ rebuild) that also changes what consumers see on default reads;
  - `scaffold-entity`: when Archive is among the chosen modes, the entity
    view's regime (kept-hidden default vs `DeleteOnArchive()`) is settled in
    the same question.
  Mechanics stay in the pinned docs — the skills only force the decision.
  Pairs with framework v0.39.x (`JoinView(...).Fields` — the per-leg allowlist
  whose `"DeletedAt"` entry is the segment's archive switch), while the
  decision gates themselves apply on every pin (the lever set is the pin's).

### Changed
- **`scaffold-entity` stops naming the archive-column builder** — the
  `Modes()` consistency invariant now names the CONCEPT (the schema's
  archive/deleted-at column declaration) and routes to the pin's table-schema
  docs for the builder name, staying correct on released pins (`SoftDelete`)
  and on v0.39.x+ (`DeletedAt`) alike, as the version-agnostic design intends.
  Prose sweeps soft-delete → archive vocabulary across the conventions
  ("archive column", "archive stamp", "default archive"); "soft removal" as
  the non-destructive-write concept stays.

## [0.8.4] — 2026-07-25

### Changed
- Skill references updated to track the framework's read-side surface renames
  (they ship together with the framework release that carries them):
  `core.NewSharedBase` → `core.NewSharedBaseSchema`, and
  `SharedBaseView(base, name)` → `SharedBaseView(name).Schema(base)` (the base
  schema now attaches via `.Schema(...)` like a regular view). Touches
  `scaffold-entity` (impact map + shared-base convention) and `scaffold-system`.
  The framework also removed the view `.Root(table)` builder (the root now
  derives from the attached schema); the skills never spelled out `.Root()`, so
  no skill change was needed there.
- Doc-routing pointers realigned to the framework's new consolidated **`views`**
  section (`docs/content/sections/views.html`, introduced in framework v0.37.0),
  which centralizes all read-side view declaration — the three view kinds, the
  view-exclusive external schema, `Embed`/`EmbedMany`, `SharedBaseView`,
  `ComposedView`, and the SyncEngine/recompose fan-out. `scaffold-view`,
  `evolve-view`, `scaffold-system`, and `remove-entity` now route view-kind /
  composition-type / view-shape questions to `views` instead of the former
  `query-side` + `table-schema` split. Write-side shared-base normalization
  references (`scaffold-entity`, and the base-schema rows) still point to
  `table-schema`, which retains that write-side material.

## [0.8.3] — 2026-07-17

### Changed
- `help`: version check + plugin self-check now fire on the session's FIRST turn
  — explicitly including a bare `/omnicore:help` that only prints the
  orientation greeting, no longer deferred to the first substantive answer. The
  plugin self-check must actually read the local `plugin.json` AND fetch the
  published one that turn (not assume a prior turn did it). (Observed: repeated
  no-question `/omnicore:help` invocations never surfaced an available plugin
  update because the checks were gated behind answering a question; the running
  install was genuinely behind — 0.8.1 vs 0.8.2 published.) Note: the check is
  prompt-driven, so a stale fetch cache (WebFetch's ~15-min per-URL cache /
  raw.githubusercontent's CDN) can still delay detection until it expires — this
  narrows the miss, it does not eliminate it.

## [0.8.2] — 2026-07-17

### Changed
- `help`: doc-URL resolution on the published site hardened. Section file names
  come from the Documentation Map ONLY — never derived from the concept's
  wording (the names are asymmetric and unguessable: read side is
  `query-side.html` not `query-handler`, write side is `command-handler.html`
  not `command-side`). The index is a single-page app, so its nav can't be
  scraped from a plain fetch of `/`; a `sections/<name>.html` that 404s means
  the name was a guess — STOP and get the real one from the Map, never
  improvise another URL. Inline concept list corrected accordingly
  (`command/query-handler` → `command-handler · query-side`). (Observed: a
  session guessed `query-handler.html`, hit a 404, then escalated to a raw
  GitHub URL of the framework repo.)
- `help`: added a hard guardrail against fetching
  `raw.githubusercontent.com/ClaudioSchirmer/omnicore/…` — the framework repo
  is PRIVATE, so every raw URL 404s regardless of path or branch (that failure
  reads as "missing docs" but isn't). The only sanctioned remote for framework
  docs is the published Pages site; the only legitimate raw-GitHub fetch stays
  the PUBLIC `omnicore-plugin` repo in the plugin self-check.
- `help`: "Never guess — verify" now covers claims of ABSENCE. A confident "no"
  is a claim like any other — before telling the dev their premise is mistaken
  or that a capability doesn't exist, read the section that would OWN it; never
  let a strong prior stand in for a read. Concretely: "reads come from Mongo" is
  true of the query path but not the whole story — the write path has its own
  read/aggregate primitives (count, sum, group-by, uniqueness probes) whose
  purpose is enforcing business rules, in the write-side handler section, not
  the query side. (Observed: a session denied write-side aggregation exists and
  called a correct premise a misunderstanding.)

## [0.8.1] — 2026-07-17

### Changed
- `help`: "Never guess — verify" now covers counting/enumeration questions —
  reproduce the doc's OWN taxonomy (its tables, headings and terms decide what
  counts as an X and what is merely a wrapper/variant of one), never
  re-classify, merge or promote categories the doc keeps distinct. (Observed: a
  session counted the read-side HTTP/export wrappers as auto-handlers,
  answering 11 where the manual's own table says 9 — 7 write + 2 read.)

## [0.8.0] — 2026-07-17

### Added
- **All 12 skills: plugin self-check.** Once per run, during preflight, each
  skill compares its own installed plugin version
  (`${CLAUDE_PLUGIN_ROOT}/.claude-plugin/plugin.json`) with the published one
  (the same file on the marketplace repo's `main`, read over raw.githubusercontent)
  and, when behind, rides ONE non-blocking line along with the next reply
  handing the dev the update command (`claude plugin update omnicore@omnicore`;
  `/plugin marketplace update omnicore` first if the marketplace is stale).
  Offline → silent skip; never a gate — the run continues on the installed
  skills and the update takes effect next session.

## [0.7.0] — 2026-07-17

### Changed
- **Oracle joins the dialect set across the skills** (framework v0.33.0 shipped it
  as the fourth first-class engine, Oracle Database 23ai+). Every "today's latest"
  dialect hint now reads `postgres|mysql|sqlserver|oracle` (`scaffold-service`,
  `run`, `upgrade`); the closed sets remain read from the PINNED docs, which stay
  the authority.
- `scaffold-service`: new **Oracle relay** trap (database-level LogMiner
  provisioning at first DB boot + per-table supplemental logging after the first
  app boot, CLOB CDC payloads by framework design) and the oracle DSN exception
  (no `<svc>_db` — the app connects to the `FREEPDB1` PDB as the app user;
  `ORACLE_PASSWORD` is the separate admin password). Port scan covers `1521`.
- `templates/docker-bench.md`: **Relational — oracle variant** (gvenzl
  `oracle-free` PINNED to a 23ai Release Update — the floating `:23` ships the
  "26ai" banner Debezium's version parser fails on; `APP_USER` envs; init-scripts
  contract: app grants incl. the documented `GRANT EXECUTE ON SYS.DBMS_LOCK`, and
  CDC provisioning — ARCHIVELOG + bounded FRA, `logminer_tbs`, the `c##dbzuser`
  COMMON user per Debezium's documented grant set, the seeded heartbeat table) +
  two wrapper arms: idempotent per-table supplemental logging (background, the
  sqlserver pattern) and the NATS-only `DEBEZIUM_HEARTBEAT` stream pre-create.
- `templates/cdc-relay.md`: **Source — oracle** block (OracleConnector over
  LogMiner: CDB+PDB pair, `lob.enabled` for the CLOB payloads,
  `skip.unparseable.ddl` + `store.only.captured.tables.ddl`, tight mining
  cadence, `heartbeat.action.query`) + the oracle-only EventRouter override
  (UPPERCASE catalog field names with lowercase header aliases — wire contract
  identical across dialects) and the UPPERCASE predicate pattern.
- `help`: the version heads-up grew into a **version check** — behind the latest
  now OPENS the first answer with a loud warning (grounded in the pinned vX while
  vY is out) pointing at `/omnicore:upgrade` (the bump's owner — the stale
  scaffold-skill/raw-`go get` pointers are gone) and offering the changelog —
  answers stay SCOPED to the pin (a feature that only exists in a newer release
  is named as such, never explained as if available); and
  the **no-project case is now defined**: never "I can't tell without a project" —
  ask once whether to read the published site
  (https://claudioschirmer.github.io/omnicore, always the LATEST release, section
  URLs mirror the Documentation Map names) or `go mod download` the latest for
  local docs, opening the answer with which ground it's on. With a pinned project
  the pin's module-cache docs remain the authority (the site would reintroduce
  version drift); the site also serves as the online changelog source.
- `scaffold-entity`: final-verify UUID grep extended to `migrations/oracle/`
  (`RAW(16)` ids, `VARCHAR2(36)` in the reject pattern); conventions updated —
  id/FK types add Oracle `RAW(16)` (`VARCHAR2(36)` for uuid-valued text), the PK
  name on oracle is `<table>_pkey` named explicitly (like sqlserver), the
  self-documenting DDL mechanism on oracle is `COMMENT ON TABLE/COLUMN` (the
  Postgres shape, single statements — compatible with the runner's plain-SQL
  split), and active-only uniqueness names all four dialect mechanisms (routing
  to the pinned `table-schema.html`, which now covers them).

## [0.6.0] — 2026-07-15

### Added
- **New skill `implement`** (`/omnicore:implement`, 12th skill): wire a framework
  capability into an existing service — another surface (gRPC/GraphQL), an external
  API call from a handler, cache, integration events, lifecycle hooks, authz,
  tracing, resilience — anything the PINNED framework offers that no dedicated skill
  owns. The pin's docs are the capability catalog: requests route dynamically
  against the Documentation Map (`features.html`/`reference.html` as existence
  check); a capability claim with no doc section behind it never enters the plan.
  Honest-no path: not at this pin but in a newer release → offer
  `/omnicore:upgrade`; not offered at all → name the closest legitimate path, never
  a workaround. Standard rituals: plan gate (`conventions/plan-template.md` —
  routing evidence, integration semantics, impact map, config/secrets, verify
  step), doc-read-before-artifact, capability PROOF in the final verify (unprovable
  steps reported honestly), fallback-router handoffs to every dedicated skill.

### Changed
- `scaffold-system` Phase 3: the domain map's §6 items (integration events,
  external calls, extra surfaces) now have an executor — each is delegated to
  `/omnicore:implement`, one per invocation, after the §5 read models.

## [0.5.0] — 2026-07-15

### Added
- **New skill `scaffold-system`** (`/omnicore:scaffold-system`, 11th skill): turn a
  whole-system/MVP description — several entities, shared identities and read models
  handed in one prose drop — into an approved **domain map**
  (`conventions/domain-map-template.md`), then scaffold it entity by entity by
  delegating each one to `scaffold-entity` (and each cross-entity read model to
  `scaffold-view`). Decomposition at SYSTEM altitude only (boundaries, shared
  identities + natural keys, role cardinalities, references, order); generation stays
  per-entity with fresh context — the map pre-answers the structural spec slots
  (§9 delegation contract) but never waives the per-entity gates. The map is the
  durable checklist: re-entry resumes at the first `pending` row; conflicts between
  the map and a delegated run's discovery stop and surface, never silently resolve.

### Changed
- `scaffold-entity` — receiving hook for the domain map: Phase 0b now looks for
  `scaffold-system/domain-map.md` (delegated run or direct invocation alike — if it
  exists, reading it is mandatory). APPROVED + entity listed → §9 slots enter the
  spec as DECIDED (`per domain-map §9`), never re-asked; discovery-vs-map conflicts
  stop and surface; DRAFT map → surface and ask; entity absent → advisory flag;
  delegated runs skip their own Phase 0v (the orchestrator resolved it once).

## [0.4.3] — 2026-07-15

Fix from a real scaffold run: the flat-vs-SharedBase question described the
SharedBase mechanism from memory ("1:1 per role"), conflating the ≤1-ACTIVE-row
invariant with one-row-forever, and used that to disqualify a case (sequential
listings over the same property) the separate-FK model handles natively.

### Fixed
- `scaffold-entity` `SKILL.md` item 1: new **role-cardinality digest** — the only
  mechanism facts the question's option text may state (≤1 ACTIVE role row per
  identity per role table, 409 on `POST`/`/unarchive`; separate-FK allows archived
  remnants + a new active row; shared-PK caps at one row forever). Names "1:1 per
  role" without ACTIVE as the canonical mis-summary.
- `scaffold-entity` `SKILL.md` item 1: when the request ALREADY names the other
  roles (even "out of scope for now"), the scripted question is answered — the
  OPEN slot becomes role cardinality, asked literally ("can the same identity
  hold TWO ACTIVE rows of this role at once?"), never self-answered.

### Changed
- `scaffold-entity` `SKILL.md`: "FLAT is the default" retitled "FLAT is the
  default CONTEXT LOAD — not a modeling bias" — it decides which conventions to
  read and carries zero weight in the recommendation.
- `scaffold-entity` `SKILL.md` item 1: identity smell broadened beyond persons to
  any party/asset with a natural registry key (property by land-registry number,
  vehicle by VIN, company by tax-id).

## [0.4.2] — 2026-07-15

Fixes from the first real sqlserver×nats scaffold runs: both templates carried
traps that made the fresh bench fail its first boot.

### Fixed
- `scaffold-service` `templates/cdc-relay.md`: the properties blocks carried
  inline `# …` comments — Java `.properties` files have no end-of-line
  comments, so a faithful copy shipped `snapshot.mode=no_data   # …` as a
  literal (invalid) value and killed the relay at boot. Every comment now sits
  on its own line, plus an explicit no-inline-comments warning.
- `scaffold-service` `templates/docker-bench.md`: the mssql image has no
  auto-create-database env (no `MYSQL_DATABASE`/`POSTGRES_DB` equivalent), so
  the first app boot died with `Cannot open database "<svc>_db"`. The sqlserver
  variant now says so, and the start wrappers gain a synchronous idempotent
  `CREATE DATABASE` step before the app boot (the reference consumer's
  `qa/_backend.sh` shape).

### Changed
- `scaffold-service` build steps (Phase 2 step 10 and the final verify) use
  `go build -o /dev/null … ./bootstrap` — the default output name `bootstrap`
  collides with the directory of the same name (hit in every real run).
- `scaffold-service` Phase 2 step 1: after `go get @latest`, cross-check
  `go list -m -versions` — the proxy's `@latest` endpoint can lag a
  just-published tag (a run pinned v0.31.0 minutes after v0.32.0 shipped).

## [0.4.1] — 2026-07-14

### Changed
- `scaffold-entity` `conventions/domain.md`: persisted fields carry `labelKey`
  and NOTHING else — the no-tags rule now names `json:` explicitly (a real
  scaffold run added wire tags to the domain by Go reflex). Framework ≥ v0.32.0
  turns the dangerous case (`json:"-"`, custom entity JSON codecs) into a boot
  panic; the convention keeps generated code clean on every pin.
- `upgrade` Phase 3 (green): the run offer is part of the VERIFY, not a
  click-through — several framework guards are boot panics no compile surfaces
  (e.g. the closed persistable type set, old-clone safety).

## [0.4.0] — 2026-07-14

SQL Server joins the dialect set (framework v0.31.0). The skills stay
version-agnostic: every dialect list is phrased as "the closed set the pinned
release supports — read it from the pinned docs", with today's latest named for
visibility; a service pinned to a pre-SQL-Server release is unaffected.

### Added
- `scaffold-service`: sqlserver bench variant in `templates/docker-bench.md`
  (mssql 2022 image, amd64-only note, image-enforced strong SA password,
  `MSSQL_AGENT_ENABLED` as load-bearing for CDC) plus the idempotent CDC-enable
  arm in the start wrappers (per-database and per-table enablement is only
  possible after the first boot creates the outbox); sqlserver source block in
  `templates/cdc-relay.md` (plural `database.names`, no-TLS dev bench, MySQL-like
  file-backed schema history, `dbo.outbox` predicate) and the note that
  `integration_events` needs its own table enablement later.
- `scaffold-service` traps: SQL Server relay prerequisites (Agent + CDC enable
  ordering) and the SA-credentials exception to the bench-DSN rule.

### Changed
- Dialect/engine enumerations in `scaffold-service`, `run` and `upgrade` are now
  doc-routed instead of hardcoded (`postgres` | `mysql` | `sqlserver` named as
  today's latest set; the pinned docs are the authority).
- `scaffold-entity`: the id-typing trap and the migrations/infra/sharedbase
  conventions carry the SQL Server facts per the pinned `table-schema.html` —
  ids/FKs are `BINARY(16)` (never `UNIQUEIDENTIFIER`; GUID sort order would
  destroy the UUIDv7 time locality), the PK is named `<table>_pkey` explicitly
  (unlike MySQL's `PRIMARY`), and the mechanical `CHAR(36)`/`VARCHAR(36)` sweep
  also covers `migrations/sqlserver/`. Where the pinned docs define no mechanism
  for a dialect (self-documenting DDL comments, active-only uniqueness on SQL
  Server today), the skill routes to the doc instead of inventing one.

## [0.3.3] — 2026-07-13

### Changed
- Business-neutral vocabulary sweep across `scaffold-entity`: the shared-identity
  read model is now consistently "the identity view" (routes file, feature and
  permission named after the BASE), sibling elicitation says "base-level facet",
  and canonical-example names (`persons`, `person_view.go`, …) remain only where
  explicitly marked as examples. The skill legislates process, never a business
  domain. (#8)

### Added
- `repository` and `license` (Apache-2.0) fields in the plugin manifest, per the
  community-marketplace submission recommendations. (#8)

## [0.3.2] — 2026-07-13

Correctness fixes for `scaffold-entity`, all three from a monitored field run
(gaps found in generated services, then closed at the source):

### Fixed
- **Child archive wiring**: the aggregate-children convention now states that all
  three per-child operations (add / update / archive) are partial updates of the
  ROOT, and calls out the trap by name — the word "archive" on a child route must
  never be wired to the root's archive auto handler (it compiles, answers 200 and
  silently archives the whole aggregate). A final-verify checklist item enforces
  it mechanically. (#7)
- **Read-request query params are optional by default**: scalar `query:`-tagged
  fields are pointers unless the spec explicitly declares a filter required — a
  value type renders the parameter REQUIRED in the OpenAPI spec and Swagger
  refuses the call without it. New web-layer trap + final-verify checklist item.
  (#7)
- **Identity view read surface**: offering the SharedBase identity view now
  includes its full read surface — the standard by-id + by-params pair with
  filters, never a lone by-id. Elicitation and sharedbase convention updated. (#7)

## [0.3.1] — 2026-07-13

### Added
- User-language policy across all skills: converse in the user's language;
  human-facing generated text follows the host project's language. (#6)
- Scope immunity across all skills: framework maintainer rules (leaked via the
  module cache's `CLAUDE.md`) never bind a skill run in a consumer project. (#6)

## [0.3.0] — 2026-07-13

### Added
- Six new skills — the plugin now ships ten: `evolve-entity`, `remove-entity`,
  `scaffold-view`, `evolve-view`, `run`, `doctor`. (#5)
- `scaffold-entity` final-verify guards: domain `Service` wired end-to-end on
  every write handler; one schema declaration per file. (#5)

### Changed
- Every skill description leads with `omnicore:` so the whole package surfaces
  when typing "omnicore" in the slash-picker (plugin skills list by bare name).
  (#5)

## [0.2.0] — 2026-07-13

### Added
- `scaffold-entity` migrations carry the spec's one-line table descriptions and
  column meanings as SQL comments. (#4)

## [0.1.0] — 2026-07-13

### Added
- Initial release: the `omnicore` marketplace + plugin with four skills —
  `scaffold-service`, `scaffold-entity`, `upgrade`, `help`.

### Fixed
- Packaging (still 0.1.0): skill directories dropped from the first push by bare
  `.gitignore` patterns (#1); marketplace plugin `source` as an explicit relative
  path (#2). Dev/release workflow documented in the README (#3).
