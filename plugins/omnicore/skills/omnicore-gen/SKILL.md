---
name: omnicore-gen
description: >-
  omnicore: drive omnicore-gen, the spec-driven code generator, to produce a
  complete entity from one YAML file — then review it, implement what the generator
  refused, and prove it; and to CHANGE one, by editing that spec and regenerating. Read this
  ONLY after the dev has chosen the codegen path at a generation gateway — scaffold-entity's
  (creating) or evolve-entity's (changing); it is not a standalone entry point. To run a single
  generator command against a project that already exists — doctor, check, explain, adopt —
  use /omnicore:gen instead.
---

# omnicore-gen

The generator writes the mechanical 1,600–3,400 lines of an entity from a spec. You keep
what actually needs judgement: the MODEL (already approved before you got here), the
business rules the spec language cannot express, the review, and the final tests.

**This skill is only reached through a generation gateway** — `scaffold-entity`'s when the
entity is being created, `evolve-entity`'s when an existing generated one is being changed.
If you are reading it because the dev asked about the generator directly, that is fine — but
a real entity still starts at `/omnicore:scaffold-entity` and a real change at
`/omnicore:evolve-entity`, because the model gate, the plan gate and the impact map are
where the thinking happens and this skill does not repeat them. And if what they actually
want is ONE command against a project that already exists — `doctor`, `check`, `explain`,
`adopt` — that is `/omnicore:gen`, which is forty lines instead of four hundred.

**Creating and changing enter this skill at different steps.** Creating: steps 1→9 in
order. Changing: the spec already exists, so step 2 is an EDIT of it (never `init`), and
the caller — `evolve-entity` — owns three things this skill does not: the migration pair an
evolution needs (written by hand, from step 5's report), the orphans a shrinking spec leaves
behind, and the artifacts outside the generator's ownership. Steps 3 and 5–9 are identical
either way.

## What the generator guarantees, and what it does not

Five invariants decide how you treat its output. They are not slogans; each one changes
what you do:

- **Nothing is generated half-way.** Every key of the spec is either consumed by an
  emitter or REFUSED by name, with the reason and the fix. There is no "accepted and
  quietly ignored". So: if `check` is green, everything you wrote in the YAML is in the
  code. If something you wanted is missing, you did not declare it — go look for the
  refusal rather than patching the output.
- **A green spec compiles AND boots.** `check` saying yes means the tree will pass
  `gofmt`, `go vet`, `go build`, its own generated tests, and a real boot. If it does
  not, that is a generator bug worth reporting, not something to work around by hand.
- **Boot-traps are static errors.** The ~20 traps the manual path asks you to remember —
  archive column ⟺ `Modes()`, view `Version`, a child carrying a revision, a reserved
  word per dialect, a column starting with `_`, two entities claiming one view name — are
  refusals from `check`. You do not spend a boot discovering them.
- **It needs no AI and no network.** It reads YAML and writes files. You are a client of
  it, not a prerequisite for it.
- **It owns whole files.** Never hand-edit a generated file. Change the spec and
  regenerate. The escape hatches are named, declared and permanent (below).

## Step 0 — the command

`omnicore-gen` is on PATH: the plugin ships it in `bin/`, which Claude Code adds to the
session's PATH. Call it as a bare command from wherever you are, pointing it at the
service:

    omnicore-gen <command> -project <service-dir> …

**`-project` is not optional in practice, and getting it wrong is the one mistake that
writes a whole service into the wrong repository.** The flag defaults to `.`, and the
generator then walks UP from there to the nearest `go.mod` and treats THAT module as the
service. So it never fails loudly for a bad target — it silently finds a different one.
Run it from anywhere inside another module and the tree lands there. It has happened while
building this generator: invoked from the generator's own directory with the flag omitted,
it resolved `Project: github.com/…/omnicore-plugin/gen` and would have generated a service
into the generator's source.

So:

- **Always pass `-project` explicitly**, and point it at the SERVICE ROOT — the directory
  whose `go.mod` declares the service's module, not a subdirectory of it and not the
  workspace above it.
- **Read the `Project:` line every command prints before letting a write proceed.** It
  names the module AND the absolute path the generator resolved. If that is not the
  service you meant, stop; nothing else in the output will tell you — every path it
  lists is relative, so a wrong root looks identical to a right one.
- When the dev has not named a directory, ask. Do not infer it from the shell's working
  directory, which is whatever the last command left behind.

`doctor`'s "No generated entity is recorded in this project" is usually this same mistake
seen from the other side: the right generator, the wrong root.

It builds itself from source on first use — the generator travels as Go source, so the
only external requirement is a **Go toolchain on PATH**. If it answers `needs a Go
toolchain` or `this plugin install is incomplete`, say so plainly and go back to the
gateway: the manual path is the honest fallback, never a hand-rolled imitation of the
generator.

Exit codes are real and worth reading: `0` fine, `1` refused (a blocker you must fix),
`2` you called it wrong. Every command also states its verdict in words — quote those to
the dev, not the number.

## Step 1 — learn the language from the binary, not from memory

**You do not know this language.** It is not YAML-flavoured Go, it is not the framework's
API, and it is not any spec format you have seen: it is narrower than all three ON PURPOSE,
because every key it accepts is a key some emitter reads. Writing a first draft from
what seems reasonable produces a file the validator rejects — reliably, because the shape
is the part that is easiest to invent and the part that is never guessable.

So, before a single line of YAML, in this order:

    omnicore-gen explain keys                 # ← FIRST. Every key there IS.
    omnicore-gen explain example              # a whole spec that validates (flat)
    omnicore-gen explain example sharedbase   # …and the shared-identity posture
    omnicore-gen explain vocabulary   # every closed key and what each choice decides
    omnicore-gen explain rules        # what the rule DSL can express
    omnicore-gen explain names        # the names YOU declare — it invents none
    omnicore-gen explain coverage     # what THIS build generates and what it refuses
    omnicore-gen explain ownership    # which files it owns, the two escapes, and prune

**`explain keys` first**, and read it whole once. It is the whole surface, derived from
the language definition itself, so it cannot be behind what the loader accepts. It exists
because of a specific failure: an author needed "unique among the rows that are not
archived", found nothing, edited the generated SQL by hand — and only later stumbled on
`unique.scope: active-only`, which had been there all along. **Most of what looks like
"the generator cannot express this" is a key whose name you did not guess.**

Then the examples, both of them. The vocabularies tell you what a key may HOLD; only an
example tells you what a spec LOOKS LIKE — that rules nest under `list:`, what a value
object's members look like, where a collection's name goes, how a facet attaches. There
are two because `storage.kind` is ONE value: the flat one cannot show a shared identity,
and the shared-identity one carries what only it can (the whole `base` block, per-child
collections, the full rule set, indexes, exports). Both are test-gated — they validate
AND they are generated, built and tested by the golden gate, so neither can teach you
something stale.

Do not paraphrase any of it from what you remember of the framework. The generator's
vocabulary is deliberately narrower than the framework's API, and `explain coverage` is
the only honest list of what this build will actually emit.

**It invents no names.** There is no pluraliser: `plural`, each `children[].plural` (which
IS the collection's `CollectionName()` — a persisted document key), each
`children[].parentColumn`, and for a shared-base role `storage.base.schemaFunc` /
`linkColumn` are all REQUIRED and read verbatim. Declare them the way the domain says
them, in the domain's language. A guessed plural compiles, boots, and writes the document
under a key nothing reads back.

## Step 2 — write the spec from the APPROVED model

    omnicore-gen init <Entity> -project <service-dir>

**Start from `init`, never from a blank file.** It pre-fills what the project already
states — the dialects, whether there is a Mongo to project into, which value objects
already exist — and its comments say WHY each choice matters. A spec typed from scratch
re-derives all of that from memory, which is where the wrong dialect and the impossible
read backing come from. FILL the template; do not replace it.

**Everything the template INVENTS is an English placeholder, and none of it is a default
to keep.** It is written that way on purpose: `init` cannot know what language the project
speaks, so it says nothing about one rather than stamping a guess into a spec. Three slots
are yours before anything else, and the template's own comments point at each:
`language:` (the language of THIS spec's human-facing text — it decides no catalog; it is
what the gen-report tells a reviewer the descriptions and labels below are written in),
the placeholder field/rule/collection names, and the `authz.permissions` strings. Fill
them from the run's language and the model approved at the gate — and take
the permission taxonomy from what the PROJECT already grants, since a permission is
matched exactly against the caller's token and an invented one is a permission nobody
holds. The identifiers themselves follow the host project's own convention, exactly as
the calling skill's language rule says.

Fill it from the model the dev approved at scaffold-entity's gate. Do not re-decide the
model here; if writing the YAML exposes a genuine modelling question, take it back to the
dev.

**Changing an entity instead: the spec exists — EDIT it, and do not run `init`.** That file
is the entity's source of truth; `init` refuses to overwrite it without `-force`, and that
refusal is a feature. Change only what the approved impact map says, `check` after each
block, and — **on a MONGO backing** — bump `read.view.version` when the projected shape
moves: `generate` compares this run's shape against the one it last wrote and refuses an
unbumped version, naming the number. **On a relational backing there is no version to
bump and the key is refused outright**: a read model that materializes nothing has no
stored shape to grow stale against, so a shape change there costs nothing and needs no
operational step. The one thing an edit cannot do is move a table: migrations are hooks (step 5), so
the ALTER pair is hand-written by the caller.

**Check as you go, not at the end.** Write the storage block and the fields, run `check`,
read what it says; then the rules, `check` again; then the read side. A 300-line spec
checked once returns a wall of blockers where each one might be a symptom of the one
above it. Checked in four passes, each blocker is about the thing you just wrote — and
`check` is instant and free.

Four things to get right, because they are the ones that cost a migration later:

- **`storage.kind`** — `flat` (its own table) vs `sharedbase-role` (a ROLE over an
  identity other roles may share). Starting flat and extracting an identity afterwards is
  real data movement.
- **`joins:` — before copying ANY column that belongs to another aggregate.** This is the
  block that most often goes unwritten because nobody knows it is there, and the cost of
  not knowing is a denormalized column plus a write path that has to keep it in step
  forever. If a rule, a service fact or the listing needs a value from an aggregate this
  entity already holds a foreign key to, declare the traversal: the value becomes an
  ordinary field of the entity, filled on every load, and NOTHING is duplicated. The
  decisions the spec asks you to make are all consequential and none of them has a safe
  default:
  - **`kind`** — `left` keeps the row and answers an ABSENCE where there is no
    counterpart; `inner` drops the row, and is legal only over a NON-NULLABLE foreign key.
    `inner` reaches `FindByID` too, so over a nullable key it would turn a legitimate
    write into a 404 — `check` refuses that combination and names `left` as the fix.
    Default to `left` unless the key is genuinely mandatory.
  - **`hidden` per field — ON THE ENTITY vs ON THE WIRE.** A traversal declared for a
    RULE belongs on the entity and nowhere near the API: `hidden: true` keeps it out of
    every response body and every export while the rules still read it. Needing a value
    and publishing it are different decisions; do not let the first quietly become the
    second, because once it is on the wire it is a promise.
  - **`inChild`** hangs the traversal off one of this entity's own collections. Its fields
    are LOAD-ONLY — served inside the entry, never filterable or sortable — and an `inner`
    there drops the ENTRY, not the root, which is a silent hole in the array.
  - **no field carries a DOMAIN TYPE** — `type: id` is refused, and so is any value
    object. The value belongs to another aggregate and arrives read-only, so a domain
    type would be an instance no rule ever approved; an identity crosses as TEXT
    instead. You rarely state a type at all: it is derived from the target's spec,
    identity included.
  - **the pointer follows from what can be ABSENT**, not from the kind alone: a left
    join with no counterpart, OR a column the target declares nullable — which makes a
    field nullable even under an `inner` join.
  - **the target** is normally another spec of this project, and then the column, its
    type and its nullability are checked and DERIVED from that spec — restating any of
    them here is refused, because a second copy goes stale the first time the target
    changes. A hand-written aggregate is accepted on your word, and then the derivation
    has nothing to read: `check` warns, each field must state its own `type` AND
    `nullable: true` on any column that is nullable over there, and the generated
    repository assumes the project's own schema-function naming. Do not skip the
    nullability: an `inner` join proves the joined ROW exists, not that every column of
    it is filled, so a non-pointer field receiving a NULL is refused at repository
    construction — a boot to find what the file could have said. The naming assumption
    fails at BUILD time if it is wrong, which is the honest failure — do not paper over it.
  - **the framework-STAMPED columns of the target are reachable too**, and nothing in its
    `fields:` names them: the three it registers under `storage.managed` — `createdAt`,
    `updatedAt` and `archivedAt` — are declared by presence, and its schema resolves them
    on the read path. So "when was it archived?" is a traversal, not a copied column.
    **Write the column exactly as THAT spec spells it** (`deleted_at`, `dt_exclusao`,
    whatever its author chose): the SLOT is what the framework fixes, never the column
    name, and `check` reads the name out of the target's own `storage.managed`. Two
    consequences: the archive column lands in a POINTER on EITHER kind of join (a row that
    was never archived holds NULL there, which is the normal state, and it is the one
    nullability the framework cannot check for you — the fields of `domain.Managed` are
    unexported), and the REVISION slot is refused, because it guards the target's own
    writes and a copy of it goes stale the moment that aggregate is written again.
  What it is NOT: a 1:N reach, a match on anything but the target's id, a second hop, or a
  way to bring a collection. `${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md` owns the
  boundary and the alternatives; `explain keys` prints the language half.
- **Value objects — and ask the closed-set question about EVERY one.** Before writing
  `kind: raw`, ask: is the set of valid values FINITE and known? If it is, it is an
  `enum`, not a shape. `raw` with `regex: '^[A-Z]{2}$'` accepts `XX`, `ZZ` and `QQ` — a
  Brazilian state field that takes 676 values, 649 of which do not exist. The enum gives
  the caller the list in OpenAPI, gives the reader named constants, and converges an
  out-of-set value to Unknown instead of storing it.
  The reverse holds too: a value with a SHAPE and no fixed set (a document number, a URL,
  a plate) is a `raw` VO, not a regex inline in a rule.
  **And once a field IS a value object, do NOT also declare `kind: required` on it.** The
  framework validates every value-object field by reflection on every write — nothing
  declares it and nothing skips it — and a string-backed raw VO reports an empty value as
  `RequiredFieldNotification`, exactly the notification the rule would add. The caller then
  reads "Required field" TWICE for one empty field. An enum is the same story with a
  different second message: `""` is not a member, so it already answers with the VO's
  unknown-member notification. `check` warns about both, naming the field. Presence on a
  VO-backed field is the value object's job; the rule list carries what the VO cannot see —
  ranges over plain numbers, immutability, state transitions, and cross-field invariants
  between fields that are UNRELATED. One between fields that are ONE concept belongs on a
  composite value object instead (next bullet), and declaring it on the entity is refused.
  Reuse an existing one with `vo: {kind: reuse, ref: <Name>}` — a second copy of a rule is
  a rule that can disagree with itself. Reuse reaches **every** value object the project
  has, including the ones a previous run generated for another entity: two roles over one
  shared base share the base's columns, so they share the value objects on them. If a
  refusal names a type ending in `Notification`, that is the answer to a different
  question — a notification is what a rule RAISES, a value object is what a field IS.
- **A value object is not limited to one column — ask the "do these mean anything alone?"
  question too.** `Money{Amount, Currency}`, `Period{From, To}`, `Address{Street, City,
  ZipCode}`: neither half means anything by itself, which is exactly when several columns
  are ONE value object. Declare it `kind: composite` with its `parts` and its own `rules`,
  and place it on the field with `vo: {kind: composite, ref: <Name>}` plus one
  `parts[]` entry per part saying which column it lives in. Two halves, two owners: the
  declaration says what the value object IS (for every entity that uses it), the field says
  where THIS entity stores it. Reading the pair back tells you the whole design.
  - **A CROSS-FIELD rule belongs there, not in `rules.list`.** "The end may not precede the
    start" is a rule about the period, not about the entity carrying it — declaring it on
    the entity is refused, and the message says where it goes. What a composite may check is
    what its own parts can answer (`required`, `length`, `range`, `comparison`,
    `requiredIf`); anything needing the old state, another field of the entity or the domain
    service stays on the entity, where it can see them.
  - **`as:` renames the part on the OUTSIDE, and generic value objects need it.** The
    default exposed name is the part's own, which reads right for `Address{Street, City}`
    (`?street=`, `?city=`) and wrong for `Money` on a salary field, which would expose
    `?amount=`. Those exposed names are the ONLY names the outside world ever sees — the
    filter, `?fields=`, `orderBy`, the export column, the JSON field, the projected document
    — because nothing above the schema learns a composite exists. Renaming one later is a
    wire break.
  - **`nullable` on the FIELD and on a PART are different questions.** On the field it means
    the whole value object may be absent (every part column NULL-able, absence written and
    read as all-NULL); on a part it means that one part is optional INSIDE a value object
    that is present. Both reach the DDL, and getting them backwards is a column that refuses
    a legitimate row.
  - **`unique` and `immutable` address the composite AS A WHOLE, and only as a whole.**
    `fields[].unique` on a composite is uniqueness over the TUPLE: one multi-column
    constraint, `scope: active-only` included, plus the 409 binding the repository
    registers — and with `enforce: service-precheck+constraint` the fact it asks for is the
    one filtered by every part. `rules.list[].kind: immutable` naming the composite compares
    the value object against the pre-write snapshot, which is what `==` means for a struct of
    comparable fields; it is refused when the value object is optional or a part is, because
    there `==` would compare addresses. Naming a PART in either is still refused: half a
    value object frozen, or unique on its own, is not a rule anyone means.
  - **Each composite type appears ONCE per entity** — across the root, its facets and its
    shared base. A second `Money`-shaped concept on one entity is its own type, not a second
    decomposition; the framework refuses the split at boot and `check` refuses it earlier.
  - **`written: manual` when the invariant is beyond the five rule kinds.** "If the resource
    is `*`, the action must be `*` too", a format that depends on ANOTHER part's value, a
    `String()` that renders the concept: none of that is `required`/`length`/`range`/
    `comparison`/`requiredIf`, and none of it fits `kind: manual` either — a composite's
    `parts` are not decoration, they are what the schema decomposes, the mappers fold and
    the migration sizes. So the SHAPE stays declared and only the FILE moves: the generator
    writes no `internal/domain/vos/<name>.go` and no test for it, the report asks for the
    type with the exact struct (field names and types are the contract — the mappers build
    it as a `vos.<Name>{Part: v, …}` literal), and everything else is unchanged. Such a
    declaration carries no `rules`: there is nowhere left to emit them, and the refusal says
    so. Copy the `labelKey` tags the report prints — the catalogs already hold those keys,
    and a tag left out is a silent fallback, not an error. **The type must NOT declare
    `Value()`**: its absence is what tells the framework to decompose the value instead of
    storing a rendering in one column. For a value that occupies ONE column the answer stays
    `kind: manual`.
- **Every generated DTO opts into the framework's generic mappers, and that is a claim,
  not a shortcut.** A Response embeds `fwresponses.Auto`, a Request embeds
  `fwrequests.Auto`, and the bodies become one call each. The generator can make that
  claim honestly because nothing in this language renames a field on one side only:
  `fields[].name` drives the Go name and the wire name together, and `parts[].as` renames
  both halves of a composite at once. What the marker buys back is a check the generator
  cannot give itself — at Mount, every Request field must land on a same-named Command
  field and every Response field must read from a same-named Result field, or the boot
  panics naming it. That is a regression net over the EMITTERS: two of them drifting apart
  used to ship a silent null.
- **`read.byParams.controls`.** A control is served ONLY if declared, and an undeclared
  one arriving on the wire is answered with a typed 400. That is a contract, not an
  omission.
- **`?orderBy=` is declared in TWO halves, and neither is legal alone.** `controls.orderBy`
  is the SWITCH — whether the endpoint accepts the parameter at all — and `read.byParams.sort`
  is the VOCABULARY: which paths it admits. Declaring one without the other fails the
  FRAMEWORK's boot, so `check` refuses it first, naming the missing half. **Nothing is
  orderable until `sort:` names it** — ordering used to come free from whatever the Response
  rendered, and an unindexed sort is a blocking sort whose cost grows with the matching set.
  - A path in `sort:` does **not** need a filter of its own. One that is filtered gets its
    ordering tag beside the filter; one that is not becomes a leaf that is orderable, carries
    no value on the wire and emits no query parameter — "order by it, do not search by it" is
    an ordinary ask.
  - It draws from the same set a filter does: the entity's own row, its facets, a composite's
    exposed parts, and the framework-stamped columns `read.managed` exposes. A collection's
    field and a computed field are refused — the first has no single value per row, the
    second no column at all.
  - **`ID` is in that set on every entity, and is declared nowhere.** It is not a `fields[]`
    entry (the name is reserved — the framework's managed carrier owns the aggregate id) and
    not a `read.managed` name (that key is the three timestamps); the framework resolves the
    logical name against the root's `TableSchema` and the projector writes it as the
    document's `_id`. So `sort: [ID, …]` and `filters: [{field: ID, ops: [eq, in]}]` are
    both ordinary declarations. Reach for it when the listing needs a **deterministic**
    order rather than a meaningful one: it is the only path indexed before anybody asks,
    and it is already the tie-break the keyset cursor appends to every key. Its operators
    are an opaque value's — `eq/ne/in/nin`; a range over an id is refused.
  - The id is orderable and filterable and **not** projectable: `read.indexes`,
    `read.fieldRestrict`, `read.computed.from` and `controls.search` refuse it, because
    those name a column in the PROJECTION and the id is not one — every response already
    carries it, put there by the framework rather than by the field pipeline.
- **`read.computed` is the read side's `manual` fact: a field no column holds.** You
  declare the shape — `name`, `type`, and `from:` naming the STORED fields the derivation
  reads — and the body is yours, in
  `internal/application/queries/<entity>_computed_manual.go`, written once and never
  rewritten. **ONE exported function per FIELD**, taking the sources it declared — a source
  declared nullable arrives as a pointer, the rest as values. The read shapes are the
  generator's problem: it unwraps the listing's sparse pointers, guards the ones that were
  not selected, and calls the same function the by-id read calls.
  - **The WRITE responses call it too.** A POST or PATCH returns the field it just created,
    derived through the same function rather than a second time somewhere else. The one
    exception is a derivation reading a framework-stamped column (`read.managed`): the entity
    does not carry those, so that field is served by the reads alone.
  - What the declaration alone buys: `?fields=<name>` fetches `from:` instead of a name
    no column has, `?orderBy=<name>` is a typed 400 on every surface, and the CSV/XLSX
    export keeps the column under its `labelKey`.
  - So it is neither filterable nor sortable nor indexable, and naming it under
    `byParams.filters`, `byParams.sort`, `controls.search`, `read.indexes` or
    `read.fieldRestrict` is refused with the reason.
  - **The failure mode is SILENT, unlike a manual fact's.** A fact panics until it is
    written; an unwritten derivation renders the field absent and nothing reports it —
    the read answers 200 and one column is empty on REST, on GraphQL and in the export at
    once. The gen-report lists them for exactly that reason.
- **`read.managed` puts the framework's own timestamps on the reads.** `createdAt`,
  `updatedAt` and `deletedAt` are stamped by the framework and declared under
  `storage.managed`, so no `fields[]` entry describes them and the aggregate carries no Go
  field for them. Listing them under `read.managed` projects them into the view, returns them
  from the by-id read and every listing row, keeps them in the CSV/XLSX export, and makes
  them filterable like any other field — `{field: CreatedAt, ops: [gte, lte]}` is the
  "created between these dates" every listing eventually needs. It is declared rather than
  automatic because it changes the view's SHAPE — which on a MONGO backing means bumping
  `read.view.version` in the same edit, or the framework refuses to boot against a
  projection built to the older one. On a relational backing there is no projection to be
  older than, so the change is free.
- **"They query by it and never receive it" is `fields[].hidden`, not `read.fieldRestrict`.**
  The two look alike and answer different questions. `fieldRestrict` is about WHO is asking:
  a caller with the permission receives the field, one without it gets a 403 for naming it
  and silence for not asking. `hidden: true` is about the field: nobody receives it, on any
  surface — not the by-id read, not a listing row, not the write response, not the CSV/XLSX
  export. Everything else is untouched: the column exists, the migration writes it, filters,
  sort and indexes reach it, a write may set it, the rules read it, and a `read.computed`
  field may derive FROM it — which is the shape this exists for, "filter by these three and
  return a description plus a derived value". Declaring both on one field is refused: there
  would be a permission that unlocks nothing.
- **A 1:1 facet (`siblings`) has one coupling worth knowing before you write it.** It is
  cleared by the ROOT's full update with its fields null — so `update.shape` cannot be
  `patch` alone, or a granted facet could never be revoked. And **with GraphQL on, the
  generator adds a `clear<Facet>Of<Entity>` mutation by itself**: there, an omitted field
  and an explicit null reach the code identically, so "clear this" cannot be told from
  "leave this alone" and the intent needs its own verb. You declare the facet; the clear
  path on both surfaces comes with it.
- **`children[].editStrategy`.** `atomic-replace` means the root's update swaps the whole
  collection — a caller adding one entry must resend every other one, and two callers
  doing that lose each other's work. `per-child` adds POST/PUT/DELETE on
  `/<entity>/:id/<collection>[/:entryId]`, a duplicate answer on ADD (declare
  `duplicateNotification`) and a 404 for an entry that is not there. Per-child needs
  `businessIdentity`: it is what "the same entry" means.
  - **`children[].operations` picks WHICH of the three to mount** — absent means all
    three, and any non-empty subset of `[add, change, remove]` is legal. Reach for it when
    every field of the entry IS its business identity (a grant holding one catalog id and
    nothing else): there is nothing such an entry can change and still be the same entry,
    so the change verb turns entry A into entry B while keeping A's row id, which an audit
    trail reads as one grant BECOMING another instead of as two events. `operations: [add,
    remove]` is the honest surface there. Do NOT reach for `atomic-replace` instead: it is
    a different contract, not a smaller one — the root's update carries the whole
    collection, so every partial client silently revokes what it omits. `check` warns
    about the swap verb when the key is absent, and stops warning once you have answered
    it. A verb you leave out leaves no trace: no route, no command, no wire type, no
    domain method, no generated test.
  - **`children[].permissions` gates those verbs APART from the root's update.** By
    default a per-entry route requires whatever the root's update requires — editing one
    entry is editing the aggregate — and that default does not change: a collection that
    declares nothing keeps generating exactly what it generated before this key existed.
    Reach for it when the collection edge is a **different job** from editing the record:
    on an RBAC entity, "may rename the group" and "may change what the group confers" are
    one permission only by accident, and the second is the one that lets an administrator
    hand themselves power they were not granted (Entra ID guards a role-assignable group
    behind Privileged Role Administrator, not Groups Administrator; IAM spells
    `AttachGroupPolicy` as its own action rather than part of `UpdateGroup`). The map is
    keyed by the same verbs `operations` uses and may be **partial** — `permissions: {add:
    group:grant}` gates the add and leaves change and remove inheriting. It is per
    COLLECTION, so an entity with two of them can gate one and not the other. `check`
    refuses a verb the collection does not mount, a name outside `add | change | remove`,
    and any of it under `atomic-replace`. **Ask before declaring it**, the same way you
    ask about `dataAccess`: a permission the deployment has not granted turns a route that
    used to answer into a 403, and nothing on the wire says which claim is missing.
- **`unique.enforce: service-precheck+constraint` is a pair, and the fact is your half.**
  The precheck asks an `exists` fact filtered by the unique field (`filters: [<Field>]`,
  `excludeSelf: true`) under `service.facts`. Declaring the enforcement without the fact is
  refused — the build used to accept the string and silently emit only the constraint.
  - **Unique WITHIN what? Say it, or the two halves disagree.** A natural key is almost
    never unique across the whole table: a role handle is unique per tenant, a code per
    workspace, a registration number per campus. `unique.within: [TenantID]` sizes the
    index AND is held to the fact's filters — which must be exactly `within` + the field,
    in both directions. Getting this wrong used to be silent and one-sided: the precheck
    came from the fact (per tenant) and the index from the field alone (global), so the
    domain accepted a handle the database then refused, reported as "already taken" for a
    tenant where it was free. Every multi-tenant entity landed there, on exactly the
    handles two customers both pick — `administrator`, `owner`, `viewer`.
- **Uniqueness on a COLLECTION ENTRY is generated, and it is per owner.**
  `children[].fields[].unique` emits the index on the entry's table — always led by the
  parent column, because an entry has no identity outside its collection — plus the
  binding that makes a duplicate a clean 409. `enforce` is `constraint-only`: a pre-check
  would be a query over the collection's own table and this build writes none. It is the
  backstop `businessIdentity` cannot be: that check sees ONE write, so two concurrent
  requests adding the same entry both pass it and both rows land.
- **An aggregating fact answers in the shape the KIND decides, not the column.** `sum`,
  `avg`, `min` and `max` compute in the database over a numeric field (`int`, `int64`,
  `float64` — anything else is refused, because the framework carries an aggregate as an
  exact integer or a float and has no carrier for text or a timestamp). `avg` is `float64`
  even over an integer field; `sum`/`min`/`max` over any integer width is exact `int64`.
  And **`min`, `max` and `avg` return `(value, bool)`** — over an empty set SQL says NULL,
  so the rule consulting the fact has to decide what "there was nothing to average" means
  rather than reading a zero nobody computed. `sum`, `count` and `exists` answer with the
  value alone: there, the zero IS the answer.
- **A state machine wants a closed, PRESENT state.** `transition` requires a non-nullable,
  string-backed enum declared in the same spec, and every state in the map must be one of
  its member values. "No state yet" is an explicit member, never a nullable field.
- **A cap counts what you tell it to count.** `groupCap` takes `cap` plus, optionally,
  `groupBy` (a cap per key) and `only: {field: X, equals: v}` (count only the entries that
  match). "At most 3 proposals under review" is `cap: 3` + `only`, with no `groupBy` — with
  a `groupBy: [Status]` and no `only` the same cap lands on accepted and rejected too, a
  rule nobody declared and nothing reports. With NEITHER, the cap is on the size of the
  collection itself ("at most 30 photos") — a real rule, and also what you get by
  forgetting the restriction you meant, which is why it is accepted only when
  `description:` says out loud that the whole collection is the subject.
- **Counting rows is the DATABASE's job — decide which set you are counting FIRST.** This
  is the one place where the two halves of the language overlap by appearance and not by
  meaning, and picking the wrong one is not a style mistake:
  - the rows are **already in the table** (other aggregates included) → a `service.facts`
    entry. `count`/`sum`/`avg`/`min`/`max`, plus **`groupBy: [Field]`** when the question is
    "…per category" — one SELECT with GROUP BY, answered as one entry per key. **Never**
    answer this by listing rows and folding them in Go: that reads a whole table to compute
    what the database computes in one pass, and it is the shape the framework added
    `AggregateBy` to kill.
  - the entries are **what this write carries** → `groupCap`. No query can see them: they
    are not in the table yet. Counting in memory is not a shortcut here, it is the only
    correct answer.
  - a fact's `field`, `filters` and `groupBy` may name a **composite's exposed part** —
    it is an ordinary column by the time the store sees it, which is what makes a pre-check
    over the two halves of a pair ("is this resource:action already taken?") expressible at
    all. Naming the composite ITSELF is refused: it has no single column. A declarative
    `factRange` fills the arguments from the entity (`e.<Owner>.<Part>`, unwrapped when the
    part is a value object), so a part of an **optional** composite is refused there — the
    value object may be absent and there is nothing to pass; call that fact from
    `rules.manual`, where the absent case is a branch you write.

  - a fact may ask about **ONE ENTRY of a collection**: `filters: [<collection>.<field>]`,
    where the collection is a `children[].plural`. It emits a method taking that entry
    field's type, asked once per entry — the shape of every "these referenced ids must
    exist and be active" rule and of every per-entry authorization check. Only on
    `kind: manual`: a computed fact is a query over this entity's own table, and the
    entry's column is on another one. Naming the entry's field BARE, or naming the entry's
    Go type instead of the collection, is refused with the spelling that works — for a
    while it was accepted in silence and the method arrived with no parameter at all,
    uncallable, discovered while hand-writing the rule that needed it.
  - **name a fact for the PROBLEM, not for the healthy state.** The generated test suite
    stubs the service so every probe answers "nothing found", which is what lets the valid
    fixture through. `TenantIsUnavailable`, `WorkspaceTaken`, `PermissionKeyTaken` read
    false under that stub and the happy path passes; the same question spelled
    `TenantIsActive` reads false too — and now means the tenant is gone, so a perfectly
    correct spec ships with a red generated test.

  So: *"no more than 5 active enrolments per course"* is a fact with `groupBy` — it is about
  rows that exist. *"no more than 30 photos in this listing"* is a `groupCap` — it is about
  what is being saved right now. If a rule needs both (the table's count PLUS what the write
  adds), the fact gives you the first half and the arithmetic belongs in `rules.manual`.
- **A fact is only half a rule — `factRange` is the other half.** Declaring
  `TurmasPorSituacao` puts a number on the port; it enforces nothing until something
  compares it. `kind: factRange` with `fact: <name>` and `min:`/`max:` writes the call, the
  comparison and the notification, exactly as `range` does over a field — including `{min}`
  and `{max}` in the message, filled from the same bounds the code enforces, and
  `echoValue: true` to send back the number the service answered. It handles all three
  answer shapes: a plain scalar, one carrying `found` (the rule stands down when there is
  nothing to compare), and a grouped one (it fires when ANY group is out of bounds).
  `attachTo:` is required — a fact's answer is not a field, so the notification needs to be
  told where to land; over a grouped fact, name the key field and the caller sees which
  group. **Only reach for `rules.manual` when the comparison itself is not a bound** — an
  arithmetic combination of two facts, or the table's count plus what this write adds.
- **Rules on a collection are declared on the collection.** `children[].rules` takes the
  same DSL the root does, including `transition` and `skipWhen`, plus `rules.manual` with
  a hook of its own (`aggregatevos/<child>_rules_manual.go`). Two kinds are refused there
  by name and belong at the ROOT, naming the collection: `childDuplicate` and `groupCap`
  ask about the whole set, and one entry cannot see it. `ownerCheck` is the root's too.
  A rule that compares an entry against the way it WAS — `transition`, `immutable` — is
  declared on the collection and enforced from the root, because an entry has no previous
  version; the generator moves it and the report says so.
- **A field the CALLER owns, not the client: `assignedFrom: identity-subject`** (or
  `identity-claim` with `claim: <name>`). The field is persisted, written on insert from
  the authenticated identity, and **absent from every write request and command** — so
  there is no client value to ignore, and an update cannot reassign it. This is the field
  an `owner-only` policy then reads; letting the body carry it means anyone can create a
  row owned by someone else. Do not describe it in `rules.manual` and hand-write the
  mapper: it is a key.
- **A field the SERVER computes from another one: `assignedFrom: derived`.** Same
  exclusion, different source: a public key derived from an immutable handle is never
  proposed by a caller, so it leaves every write request, command and OpenAPI request
  schema — while `identity-*` would be a spec that lies about where the value comes from.
  The generator writes NO assignment for it (it cannot know the derivation): **you owe a
  `rules.manual` entry scoped to insert** that computes it, and the report lists the field
  until you do. Declaring the field ordinarily instead is the failure this exists to kill —
  it lands in the write DTO, the OpenAPI documents it as writable, and the caller's value
  is accepted and silently ignored.
- **The field's LABEL is `text:`, not `description:`.** The description is a sentence about
  what the field means and becomes the column comment; the label is its short human name —
  what a 422 payload puts in `fieldLabel` and what a CSV/XLSX export puts in a column
  header. `text:` takes the seven catalogs like a notification's, is optional, and may be
  partial: a catalog left out falls back to the field's own name. Give it to any field
  whose name does not read as a label in the languages the project serves.
- **An update that RETIRES the row: `delete.archiveWhen`.** When a field reaching one
  value means the record should not be left active ("dropped", "terminated", "cancelled"),
  declare it — `field` + `equals`, plus an optional `becomes` — and the generated
  `IfUpdate` clause ends by asking the framework to finish THAT write as an archive: the
  archive stamp, the child cascade, the `ARCHIVED` event the read side routes on, an
  archive audit entry, all off a plain PUT/PATCH. It needs `archive` in `modes` (an entity
  that forbids archiving panics when the rule fires) and it is checked against the enum's
  members when the field is one, so a typo'd trigger is refused instead of silently never
  matching — as is a trigger no update can SET, which warns when the field is in
  `update.patchExcludes` on a patch-only entity or carries an `immutable` rule scoped to
  update. **`becomes` is the trap worth reading twice**: the archive rules do NOT re-fire
  on this path, so the row is archived holding whatever the closure leaves — right when the
  trigger is a resting state, wrong when it is a request.

  **Before declaring it, answer who is allowed through it — the generator does not, and
  cannot.** Three different things decide a write, and this key moves between them:

  | | What it decides | Where it is declared |
  |---|---|---|
  | permission | who may attempt the verb on this ROUTE at all | `authz.permissions.<verb>` |
  | rule | whether that attempt is allowed on THIS row | `rules.list[].scope` |
  | `archiveWhen` | what the write turns out to BE | `delete.archiveWhen` |

  It changes the third, so the first two stay the update's. The write arrives under
  `<res>:update`, never `<res>:archive` — the archive permission guards the archive route,
  and this request did not use it — and it runs the `IfUpdate` rules, so a rule with
  `scope: [archive]` never sees it. **Declare it when both populations are the same**
  (`explain example` shows exactly that: whoever may edit an enrollment may close it).
  When they are not, either restate the guard with `scope: [update]` so it fires on the
  door actually used, or leave the key out and keep removal on its own route —
  `explain example sharedbase` is that case, and says so where the key would have gone.
  The gen-report lists it as a SECOND removal door for the same reason.
- **A second role can EXPOSE the identity's collection.** With `storage.base.reuse: true`,
  declaring `children[].ownedBy: base` MOUNTS a collection the base-owning spec already
  declared: restate its shape verbatim (the generator compares it field by field against
  that spec and refuses a disagreement), and this run writes the surface only — routes,
  commands, requests, named `<Entity><Child>…` so they cannot collide with the other
  role's. No table, no entry type, no input DTO: those are the owner's, and adding a
  field to the collection means adding it there first.

## Step 3 — check, and read the refusals as instructions

    omnicore-gen check -spec specs/omnicore-gen/<entity>.omnicore.yaml -project <service-dir>

Every blocker names the key, what is wrong, and the fix. Fix the SPEC. The two verdicts
that are not about your YAML:

- **framework `behind`** — the project pins an older line than this build targets.
  Upgrade the pin (`/omnicore:upgrade`), do not force past it: going backwards the gap is
  usually an API that does not exist yet.
- **framework `ahead` / `unknown`** — the framework moved, or the pin is a local
  checkout. This never blocks. Generate, then let the compiler be the oracle: if
  something does not build, read the framework's changelog for that version and fix the
  small thing, or report it.

## Step 4 — the ELSE: what the generator cannot do is DECLARED, never improvised

When the DSL cannot express an invariant, you do NOT drop it and you do not write it into
a generated file. You declare it as a named manual item:

    rules:
      manual:
        - id: <a-name>
          description: >-
            What must be guaranteed, in one sentence someone can implement.
          scope: [insert]

The generator then emits `internal/domain/<entity>_rules_manual.go` with the signature,
the contract comment, and a stub per item — **write-once, never re-generated, never
hashed**. The same shape exists three times more:

| The ELSE | Declared as | Where the body goes |
|---|---|---|
| an invariant the rule DSL cannot say | `rules.manual` | `internal/domain/<entity>_rules_manual.go` |
| a question the service cannot answer declaratively | `service.facts[].kind: manual` | `internal/infra/<entity>_service_manual.go` |
| a read field no column holds | `read.computed` | `internal/application/queries/<entity>_computed_manual.go` |
| a VALUE OBJECT whose rule is neither a shape nor a set | `valueObjects[].kind: manual` | `internal/domain/vos/<name>.go` — **you create the file** |
| a COMPOSITE value object whose invariant no rule kind states | `valueObjects[].written: manual` | `internal/domain/vos/<name>.go` — **you create the file**, and the parts stay declared |

A `manual` fact is usually manual because the truth lives ELSEWHERE (another service, an
external API, an upstream mirror). When it is local instead — a question this database can
answer that the DSL could not phrase — the primitive is still the hydration-free one:
`${CLAUDE_PLUGIN_ROOT}/shared/query-primitives.md` owns that choice, and the generated
declarative facts next to it are what the right shape looks like.

All five are the same bargain — the language declares the shape, a human writes the body —
and they differ only in what an unwritten body does: a rule quietly enforces nothing, a
fact panics on first use, a derivation renders an empty column, and a missing value object
**does not compile**. The gen-report says which is which, and for the value object it
prints the exact shape: `type X <backing>` with `Value()` and `IsValid` for a scalar — the
backing is a contract, because the emitted mappers convert with `vos.X(v)` and read back
with `.Value()` — and for a composite the struct itself, whose FIELD names and types are
the contract instead, with no `Value()` at all.

**`kind: manual` is the answer to "the generator cannot write this value object", and
`kind: reuse` is not.** Reuse resolves against types the project ALREADY has, so pointing
it at one you are about to write is refused with "the project declares no value object
named X" — which reads like a naming mistake and is not one. When the rule is a checksum,
a lookup, anything raw and enum cannot say: declare it `manual`, with the `description`
that tells whoever writes it what to enforce. The same trap has a second mouth on the
composite side: a composite you are about to write is `kind: composite` + `written:
manual`, never a `ref` to a type that is not there yet.

**The hook file arrives with ONE gate per verb holding every rule scoped to it — keep it
that way when you implement them.** A gate per rule is the shape that grows by accident,
and it costs twice: the file becomes a wall of near-identical closures a reader has to diff
to find the rules, and the framework dispatches the same verb check once per block on every
write. Fill in the TODOs in place; do not wrap each one in its own `r.IfInsert`. A rule
listed under two gates is still one rule — write it as a method on the receiver and call it
from both blocks.

The two hook files are NOT equally quiet, and their headers say which is which: an
unwritten rule leaves an invariant unenforced and the service runs on; an unwritten fact
panics the moment a rule asks for it. Implement both before you call the entity done.

## Step 5 — generate

    omnicore-gen generate -spec specs/omnicore-gen/<entity>.omnicore.yaml -project <service-dir>

It prints created / updated / unchanged / kept-as-is. **`kept as-is` is two different
things, and the run tells them apart** — read the report rather than the count:

- **A `_manual` file** — the rules hook, the service hook, the migration pair. Nothing
  happened; they are yours by design and a routine regeneration always keeps them.
- **An OWNED file that was hand-edited**, which it REFUSED to touch. Your edit is safe,
  and it will keep being refused until you either move the change into the spec or run
  `adopt` on that path. This one is called out separately, by path.

**Migrations.** Written ONCE, named `NNNN_<entity>_manual.sql`, and never regenerated —
the same posture as the `_manual` rule files, and for a sharper reason. A migration is
the only output whose effect outlives the file: once it has run anywhere, the framework's
tracking table records it as applied, so rewriting the file changes what the file CLAIMS
and not a single table. The generator creates a schema; it does not evolve one — **the CODE
regenerates from an edited spec, the DATABASE never does.** A later change is a NEW numbered
pair, in every target dialect, written by whoever knows where the first one has run. The
report prints the shape the regenerated code expects, to compare against — read it as
confirmation when nothing about the storage moved, and as the specification of the pair you
owe when it did. There is no flag: the question it would answer has one permanent answer.

**A shrinking spec leaves ORPHANS, and `prune` is what removes them.** Files the previous
run generated and this one does not are listed in the report under `No longer generated` —
they are not deleted by a write, they still compile, and they mean nothing. The same
asymmetry holds inside the shared files: a write inserts and replaces its own entries but
never deletes, so a labelKey or notification the spec no longer declares stays in the
notifications file and in all seven catalogs. Both are cleaned by one command:

    omnicore-gen prune -spec specs/omnicore-gen/<entity>.omnicore.yaml -project <service-dir>
    omnicore-gen prune -spec … -project … -apply

It is DRY by default — read the three lists (remove · forget in the lock · left alone with
the reason) before `-apply`. It touches only text the lock still recognises as the
generator's own, byte for byte: a hand-edited file, an adopted one, a declaration another
entity also claims and every migration are reported and left. Build and run the tests after:
a notification type removed while the code still references it is a compile error, which is
the good kind.

## Step 6 — read the report. It is the handover, not a log

`specs/omnicore-gen/<entity>.gen-report.md` is written every run and is the list of what YOU still owe:

- **What still needs implementing** — every manual rule and manual fact, by id, with the
  description you wrote and the file it landed in. Missing translations too, if any were
  filled with a loud placeholder.
- **What to check** — the decisions the spec made that a reviewer should confirm against
  the domain (uniqueness scope, delete semantics, permissions per operation, the archive
  column, the view version) — **including one row per declared read join**, which is the
  one decision on that page a reviewer cannot see by reading the entity, since a joined
  field looks exactly like the entity's own. Each row states what a missing counterpart
  does and whether the value is on the wire.
- **What was generated** and **what was NOT** — the refused capabilities, so the gap is
  visible rather than assumed.

Read it fully. It is the reason the codegen path does not lose the review.

## Step 7 — review the generated code (this is your job, and it is not a formality)

**You are asking TWO questions, not one, and the second is the one that gets skipped.**

1. *Does the SPEC say what the model meant?* A spec that validates can still be wrong,
   because the generator wrote exactly what you declared. This is the question the
   checklist below is mostly about.
2. *Does the OUTPUT say what the SPEC declared?* The generator can be wrong too. Read the
   emitted code against the yaml that produced it, key by key, for the keys that carry a
   promise — not just for plausibility.

Question 2 is not theoretical, and it does not announce itself. Every defect found this
way so far survived `gofmt`, `go vet`, `go build` AND the generated test suite, because
none of them is a Go error:

- a `groupCap` whose notification declared `tvars: [max]` got the struct field, the seven
  catalogs and the hard-coded cap in the comparison — and raised `Notification{}`, so the
  422 read "at most  permissions", with a hole, in every language. Only an end user ever
  saw it;
- `authz.dataAccess: tenant` generated its write guard and fed it on the root's mappers
  and not on the per-entry child ones, so a caller holding the update permission could
  grant into and revoke from another tenant's aggregate. The build was green, the
  generated suite was green, and the read filter hid the damage from the caller's own
  side.

So: for every key in the spec that promises RUNTIME behaviour — an authz scope, an
interpolated message, a cap, a unique scope, a cascade — find the line that keeps the
promise and read it. "The file exists and looks reasonable" is not the check.

**When question 2 answers badly, that is a generator defect and it is worth reporting.**
Say so in the hand-back, with the spec fragment, the emitted fragment, and what you
expected. Three of the language's newest capabilities exist because somebody did that
instead of hand-patching the output and moving on.

Do not re-read every file. Read against the plan the dev approved and against the report:

1. **The domain type + `BuildRules`** — do the emitted rules say what the model meant?
   This is where a spec that validates can still be WRONG, because the generator wrote
   exactly what you declared.
2. **The rules the report listed as manual** — implement them now, in the hook file, from
   the routed `/docs`. Nothing else in the tree is yours to write.
3. **The migration** — column types, nullability, uniqueness scope, FK direction. Read
   the DDL, not the YAML you wrote. **Every `description:` is in there as a real database
   comment** (postgres/mysql/oracle `COMMENT`, sqlserver `MS_Description`; SQLite is the
   one engine that has nowhere to store one, so there it stays a `--` line) — which is
   what makes a vague description expensive: it is what a DBA, a BI tool and the next
   developer read off the catalogue, not something only this file carries.
4. **Authz** — the permission per operation, and whether `dataAccess` narrows rows the
   way the domain needs (`owner-only`/`tenant` are a different question from the
   permission). A scoped `dataAccess` generates BOTH halves: the read filter in the
   query, and a guard in `BuildRules` under every write gate — insert, update/patch,
   archive, unarchive, delete — answering `TenantMismatchNotification` (403). Read both;
   for a while only the read half existed, and the output LOOKED complete, so a reviewer
   read tenant isolation on the listings and reasonably concluded the posture was in
   place while a caller with the ordinary permissions could create a row inside another
   tenant, edit one and archive one. Two knobs at its edges, both optional and both
   policy: `authz.bypass` says WHO crosses the scope (the operator supporting a customer),
   and `authz.noIdentity` says what an absent identity
   means — `stand-down` (the default: the scope applies to every authenticated caller and
   steps aside only where there is nobody to scope to) or `refuse` (enforced even with
   authentication off, so the entity serves nothing on a dev bench).

   **`authz.bypass` has two spellings and they are two different policies.** A concrete
   permission (`platform:cross-tenant`) is grantable on its own — and note that HOLDING it
   is wider than being granted it, since `HasPermission` answers yes for `platform:*` and
   for `*:*` too. `bypass: "*:*"` is the other one: the super-admin wildcard itself, and
   nothing new becomes grantable, which is the right spelling when the approved policy was
   "a super-admin crosses the tenant" and minting a permission would widen it. The
   generated guard cannot ask `HasPermission` about a wildcard — it panics on one, because
   the claim wildcards and the question does not — so for `*:*` it calls
   `Identity.IsSuperAdmin()`, the framework's own question for that grant (nil-safe, and
   honouring the configured permissions claim); the gen-report names it, and a generated
   test proves it against the framework with a real wildcard claim rather than against a
   comment. Every OTHER wildcard is still refused: `role:*` cannot be a bypass — it has no
   question of its own.

   **The generated guards ask whether an identity was PRESENT, never whether the scope
   came out empty**, and that distinction is the whole reason the default is safe. They
   are two different facts arriving as one value: no identity at all is confined to
   `auth.mode: disabled`, which the framework refuses outside `APP_PROFILE=dev`; a real,
   signed token that simply carries no such claim is an ordinary production request, and
   is still refused. The same applies to a `rules.list` `ownerCheck`, which has tolerated
   an absent principal since long before the row scope existed — for a while it tolerated
   an empty VALUE, so a token without the claim walked through it in production.
5. **The read side** — the view name and `Version`, the declared filters and controls,
   the indexes. A `?search=` needs a declared text index. If the spec declares
   `read.computed`, the derivation hook is the one place where a green build still means
   an empty column — check its bodies are written.
6. **If the entity has a facet and GraphQL**, check that both clear paths are there: the
   root's PUT with the facet's fields null, and `clear<Facet>Of<Entity>`. The report lists
   them side by side. A facet a caller can grant and never revoke is the failure this
   pair exists to prevent.

Anything wrong here is fixed **in the spec**, then regenerated. The only files you author
are the `*_manual.go` hooks — the rules one, the service one, and the computed-read one
when the spec declares derived fields.

### When the output is wrong and the spec seems unable to say so

Editing a generated file is the LAST move, not the first. Before it, exhaust the spec, in
this order — each step is cheap and most problems die at the first:

1. **Re-read `explain keys`, `explain vocabulary` and `explain rules` for that exact
   concern.** The key usually exists and is named something you did not guess:
   uniqueness scoped to the active rows is `unique.scope`, "required only when that other
   field is filled" is `requiredIf`, "valid IF present" is `skipWhen`, per-entry endpoints
   are `editStrategy: per-child` and which of those verbs to mount is
   `children[].operations`, "adding to this collection needs a permission of its own,
   not the one that edits the record" is `children[].permissions`,
   "a super-admin crosses the tenant" is `authz.bypass: "*:*"`,
   "the server fills this from the caller" is
   `assignedFrom`, and a collection of the shared identity on a second role is
   `ownedBy: base` under `base.reuse: true`.

   Most of these were found AFTER someone hand-wrote the thing they express — a
   hand-edited SQL constraint, a hand-written insert mapper, four hand-written files for
   a collection. The two newest ones came the cheap way instead: an author hit the wall,
   stopped, and reported it, and the language grew a key. Reading this list costs
   seconds; the alternative cost hours and left files that no longer track the spec.
2. **Change the spec and regenerate.** Regeneration is seconds and idempotent; there is
   no cost to trying.
3. **If the invariant genuinely cannot be declared, use `rules.manual`** — a named item,
   a stub in a file regeneration never touches. That is the designed escape, and it does
   not fight the generator.
4. **Only then** consider editing a generated file — and that is a GATE, below. You never
   take this step on your own.

### ⏸️ GATE — a generated file is never edited without the dev's yes

Editing generated code is a legitimate move. `omnicore-gen adopt` exists precisely for it:
it is part of the generator's plan, not an admission of defeat, and being timid about it
is how a project ends up with a spec that quietly lies about what is on disk. So do not
refuse the idea.

But the cost is permanent and it lands on somebody who is not in this conversation, so
**the decision is the dev's, every single time.** Stop, ask, and wait. Never edit first
and mention it after; never fold it into a larger "here is what I did".

Ask in one message, carrying all five of these:

1. **The divergence, concretely** — the spec fragment, the emitted fragment, and what you
   expected instead. Not "the authz output looks off": the two snippets, side by side.
2. **Which of the two questions it answers.** A spec that cannot express the need is one
   conversation; a generator that did not keep a promise the spec made is a different one,
   and the second should usually be REPORTED and worked around temporarily rather than
   adopted permanently.
3. **What you already ruled out** — which `explain` topics you read, which key you looked
   for, why `rules.manual` does not cover it. Step 1–3 above, named, so the dev can
   disagree with a step rather than with a conclusion.
4. **What `adopt` costs.** Say it plainly: `omnicore-gen adopt <path> -why "<reason>"`
   marks the file hand-owned, and from that moment it **STOPS TRACKING THE SPEC**. Every
   later improvement to the emitters — a bug fixed, a verb corrected, a security guard
   closed — lands everywhere except there, silently, forever. The two defects at the top
   of Step 7 were fixed for every project on the next release; a project that had adopted
   those files kept both bugs and nobody was told.
5. **The alternatives you can offer**, so it is a decision and not a rubber stamp:
   *(a)* adopt the file and hand-edit it; *(b)* leave the output as generated, report the
   defect, and carry the gap in a `*_manual.go` hook or in the caller until a release
   fixes it; *(c)* change the model so the question does not arise.

Then wait for an explicit answer. Silence is not a yes, and "faz o que for melhor" is
worth one clarifying sentence rather than a guess — the whole reason this gate exists is
that the person who pays the cost is the one who should choose it.

Whatever the answer, **it goes in the hand-back**: every adopted file with its `-why`, and
every divergence you reported instead of adopting. That list is how the generator gets
better; a hand edit nobody hears about is a gap that stays open.

One more thing about the `-why`, since it is the only part of an adoption that survives
the conversation: write it for the next person, who will not be the one who edited the
file. "adopted" alone does not say whether the reason still holds — `-why "the framework
has no key for X; revisit after <version>"` does.

## Step 8 — prove it

The generator writes its own tests; they are part of the tree and must pass. Run the
service's whole check:

    (cd <service-dir> && GOWORK=off go build -tags "<engine> <transport>" ./... \
       && GOWORK=off go vet -tags "<engine> <transport>" ./... \
       && GOWORK=off go test -tags "<engine> <transport>" ./...)

Then apply the migration and boot, and call the entity for real — a write and a read at
minimum. The generated tests cover the mappers, the rules, the schemas, the catalogs and
the read criteria; they do NOT cover the repository, the domain service, the routes or a
running projection, so the boot is what proves those.

**Write the tests the generator cannot**: the invariants you implemented in the hook
files. They are the only logic in the tree with no generated test behind them, which is
exactly why they are the ones worth testing by hand.

## Step 9 — hand back

Report to the dev: what was generated, what you implemented by hand and why the DSL could
not express it, what the report flagged to check, and the result of build/vet/test/boot. On
a CHANGE, add the three things only that path produces: the migration pair you wrote (and in
which dialects), the orphans you deleted, and any file you adopted or forced.
If any capability was refused, say which and what the consequence is — never let a
refusal reach them as silence.

## Failure modes worth recognising

| What you see | What it means | What to do |
|---|---|---|
| `kept as-is (yours, by design)` | the `_manual` files (rules, service fact, migration) — OR an owned file that was hand-edited | nothing for the `_manual` ones; for a hand edit, move the change into the spec, or `adopt <path>` if it is a framework fix worth surviving regeneration |
| a blocker naming a key you did not write | a default the loader applied | read the fix line; it names the key and the value it needs |
| `not generated by this build` | the capability is refused, with the phase that will bring it | express it another way, or move it to `rules.manual` — never hand-write it into a generated file |
| a spec that validates but the tree does not build | the framework moved past this build | read the framework changelog for the pinned version, fix the small thing, `adopt` the file so it survives |
| a second entity refused on `read.view.name` or `plural` | another spec in this project already claims it | rename this one; the framework aborts the boot on a duplicate |

## What this skill never does

- **Never writes a spec before reading `explain keys` and the examples.** A first draft
  written from what seems reasonable is a guess, and the validator will say so — after you
  spent the tokens.
- **Never concludes "the language cannot express this" without having read `explain
  keys`.** That conclusion is what ends in a hand-edited generated file, and it has been
  wrong every time so far: the key existed, with a name nobody would guess.
- Never hand-edits a generated file. Never.
- Never re-decides the model. That was approved before the gateway.
- Never invents a name the spec must declare.
- Never reports green without a build, a vet, the tests and a boot.
