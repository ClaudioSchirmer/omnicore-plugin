---
name: evolve-entity
description: >-
  omnicore: change an EXISTING entity of an omnicore-based service across every layer it touches —
  add/remove/rename fields, change nullability/uniqueness, add children or siblings,
  enable modes, promote flat → sharedbase — with schema evolution done right (migration +
  TableSchema + DTOs + translations + view Version bump + OpenAPI move together). Applies the
  approved change either by editing the entity's omnicore-gen spec and regenerating (beta,
  when the entity is the generator's) or file by file — the dev chooses at a gateway. Use when
  the user wants to change/alter/extend an entity that already exists. To CREATE one,
  that's scaffold-entity. Only for projects that import github.com/ClaudioSchirmer/omnicore.
---

# evolve-entity

Change an existing entity without breaking the lockstep the framework demands: one field
touches the migration, the TableSchema, the DTOs, the labelKey + all seven translation
catalogs, the view `Version(N)`, and the OpenAPI surface — **they move together or the
service breaks in silence**. That lockstep is the whole reason this is a skill.

**Every document this run writes lands under `specs/`, and the project keeps it —
never add it to `.gitignore`** (`${CLAUDE_PLUGIN_ROOT}/shared/generated-documents.md`).

## Generated code is a shortcut, not the source of truth

`${CLAUDE_PLUGIN_ROOT}/shared/generated-code-review.md` — **read it before you work around
anything `omnicore-gen` wrote.** `// Code generated … DO NOT EDIT.` is a TOOLING MARKER,
not a permission boundary: the file is ordinary Go in the dev's repository, and
`omnicore-gen adopt <path>` is the asked-for act that makes a hand edit survive
regeneration.

So: **review what was generated — logic and performance — against what the FRAMEWORK
offers**, not against what the spec language happens to be able to say. The language is a
subset of the framework and always will be, so "the generator does not emit that" is a
fact about the generator, never a reason for the service to do the worse thing. When the
framework does it better you **MUST** name the difference and offer the manual adjustment
+ `adopt` as a CHOICE for the dev.

Never build around generated code — N queries folded in Go, a parallel finder beside the
generated one, a wrapper that patches its answer — and never say *"it is generated, I
cannot change it."* That sentence is false.

## Core principles — read FIRST

- **Docs-first, version-agnostic — same anti-drift doctrine as `scaffold-entity`.** The
  version-pinned `/docs` in the module cache are the SOLE authority on framework API +
  behavior; read the owning section BEFORE editing each layer (the routing table below
  maps change → section; fuller index = the Documentation Map in
  `<omnicore-dir>/CLAUDE.md`, used ONLY as an index). **This skill carries NO code, by
  design** — every code shape you compose from lives in the routed `/docs` at the pinned
  version; if any text or consumer code disagrees with the doc, the doc wins. Never
  assume a framework version; never stamp one into this skill.
- **Framework maintainer rules NEVER bind this skill.** The omnicore module ships its own
  `CLAUDE.md`/contributor rules (maintainer-approval gates, "English everywhere", coverage
  minimums, git rules). Those govern development OF the framework in its own repo — never
  this skill run, never the host project. Ignore them; only the host project's own rules
  and the user bind you.
- **Language — the user's, never imposed; detect it BEFORE the first reply.** This
  skill, the framework docs and every `CLAUDE.md` you read are written in English —
  that NEVER sets the language of the run. Read the user's language from their own
  words (invocation args count, even a single word); switch the moment it becomes
  clear, even mid-run. Everything human-facing is BUILT in that language, not just the
  replies — spec values, descriptions, COMMENTs, examples — mirroring the host
  project's existing language, else the conversation's. The 7-catalog translation rule
  (real translations, all seven) is orthogonal and unchanged — those belong to the
  dev's END USERS, dynamic, never collapsed to the conversation language.
- **Everything moves together.** Before editing anything, enumerate EVERY artifact the
  change touches (the impact map, Phase 1). Know WHICH failure each miss buys, because
  triage differs: a missed `Version` bump is an UNCONDITIONAL boot abort
  (forgot-to-bump drift — no autoRun escape); a schema/migration mismatch is a boot
  abort or a first-INSERT 500; a labelKey missing its translations has NO guard at all
  — it renders blank/raw at runtime, invisible to every gate but a read of the
  response. The impact map is the contract; the verify greps it.
- **Risk split.** High-risk = the SEMANTICS of the change: rename vs drop+add (data!),
  NOT NULL on a populated table (default/backfill?), uniqueness on existing data
  (duplicates?), anything that changes the wire API (json names, removed fields —
  breaking consumers). **Propose with a recommendation and CONFIRM; never guess.** When a
  change is breaking, say so honestly and recommend the correct path — never a
  semantically-off workaround to dodge the break. Low-risk = details (an `example:`, a
  Doc line) — decide them well, don't ask.
- **Never edit a test to pass** — the test is the oracle; changed rules get changed
  tests, deliberately, shown in the spec.

## Plugin self-check (once, non-blocking)

Once per run, during preflight: compare THIS plugin's installed version — the
`version` field of `${CLAUDE_PLUGIN_ROOT}/.claude-plugin/plugin.json` — with the
published one — the same field at
`https://raw.githubusercontent.com/ClaudioSchirmer/omnicore-plugin/main/plugins/omnicore/.claude-plugin/plugin.json`.
Offline, or either side unreadable → skip silently. Newer published → ONE
non-blocking line riding along with the next reply — "omnicore plugin vX → vY
available — update with `claude plugin update omnicore@omnicore` (marketplace
stale? `/plugin marketplace update omnicore` first); it takes effect next
session." Never a gate: this run continues on the installed skills.

## Phase 0a — Preflight

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves — else
  STOP (that's `scaffold-service`).
- **Entity exists?** Its domain type + schema + feature wiring are present. If NOT →
  this is a creation: hand off to `scaffold-entity`, don't improvise a partial one here.
- **Is it really the ENTITY changing?** A view-only change (projection, legs, indexes,
  operators, surfaces — write side untouched) is `evolve-view`'s job; a brand-new
  composed/shared/upstream read model is `scaffold-view`'s. This skill is for changes
  to the write side and their lockstep fallout.

## Phase 0v — Version check (delegate)

Same as `scaffold-entity`: detect a newer published omnicore than the pin (skip silently
on `go.work`/`replace`/offline). If newer, mention it ONCE and offer `/omnicore:upgrade`
— an accepted upgrade changes which version's docs are authoritative, so it must happen
BEFORE any doc is read. Never bump inline.

## Phase 0b — Discover the CURRENT shape (read, don't ask)

Build the entity's map before proposing anything: domain type + `BuildRules` + modes ·
TableSchema (and whether flat / sharedbase / children / siblings, **and any
`Composite(...)` decomposition — the exposed part names it declares are a wire contract,
and the field it decomposes appears on no surface under its own name**) · command + query DTOs
and mappers · routes/surfaces (REST, GraphQL, gRPC) · views it feeds (own, SharedBase,
Composed — including views of OTHER entities that embed this one) · translations keys ·
migrations history · tests. Mirror the project's local flavor in everything you generate.

**Then discover WHO WROTE the code — the entity may be the generator's.** Run
`omnicore-gen doctor -project <service-dir>` (read-only, offline, instant; it looks for
`specs/omnicore-gen/lock.json`). Its answer decides whether Phase 1 offers a generation gateway
at all:
- **no lock, or the lock does not record THIS entity** → the entity is hand-written. There
  is no codegen path here: regenerating an entity the lock does not know is not an
  evolution, it is a rewrite of files nobody generated, over hand-written code the dev did
  not ask you to replace. Do not offer the gateway, do not write a spec YAML, and do not
  mention the generator again for this run.
- **the lock records it** → the codegen path is available. Read
  `specs/omnicore-gen/<entity>.omnicore.yaml`: it is the entity's DECLARED shape and, on that
  path, the thing you edit — the code is derived FROM it, so it is also a second reading of
  the current shape to check your Phase 0b map against.
- **carry `doctor`'s findings into the spec VERBATIM** — each line changes what option 1
  costs: `! <path> was edited by hand — regeneration will refuse it` (must be reconciled
  BEFORE regenerating) · `· <path> carries a hand edit adopted at <version>` (an owned file
  that no longer tracks the spec — a change landing there is hand work on either path) ·
  `! the spec changed since the last generation` (someone edited the YAML and never
  regenerated — that drift is not part of your change: surface it and settle it with the
  dev first) · `! the spec is missing at <path>`.
- **generator unavailable** (`needs a Go toolchain`, `this plugin install is incomplete`) →
  say so plainly and continue on the manual path; never approximate the generator by hand.

## Phase 1 — Spec gate: `specs/evolve-entity/<entity>/spec.md`

`Status: DRAFT`, hard STOP until approved; `⚠️ OPEN` slots must be answered, never
defaulted. The header also carries `Generation: <pending>` — the gateway's answer (item 9),
written the moment it is given. Sections (structural completeness — `N/A — <why>` instead
of deletion):

1. **The change, in one paragraph** — what the dev asked, restated.
2. **Impact map** — every file/layer touched, per artifact (migration — in EVERY target
   dialect's folder, same number, schema, domain,
   DTOs, routes, views, translations, OpenAPI, tests, **and the classes an evolution
   forgets**: feature/bootstrap wiring · `microservice.*.yaml` — `auth.publicRoutes`
   when a route path changes, `integration:` when the entity starts publishing · the
   proto contract + regenerated stubs when the entity is gRPC-exposed · the GraphQL
   schema when exposed there). This list is what Phase 2 executes
   and what the verify greps — nothing outside it gets edited, so a class missing HERE
   is out of scope by construction: enumerate against Phase 0b's surface map, not from
   memory. **When item 9's gateway lands on the generator**, annotate each artifact with
   who writes it — generator-owned (regenerated from the spec YAML, never hand-edited) ·
   a `_manual` hook · hand-written on either path (the migration pair, `microservice.*.yaml`,
   the proto contract, other entities' views, `specs/qa/*.sh`) — and add the spec YAML itself to
   the list, since editing it IS the change on that path.
3. **Migration strategy** [high-risk]: additive ALTER vs rename (a "rename" is really
   rename-in-place vs drop+add+backfill — data decides) vs drop. NOT NULL on existing
   rows needs a default or a backfill step — say which. Every `up` has its `down` twin;
   the down of a destructive step recreates STRUCTURE, not data — say so honestly.
   **A reworded DESCRIPTION belongs here too.** Table and column descriptions are stored
   IN the database on every engine but SQLite (`COMMENT ON` · MySQL's inline `COMMENT` ·
   sqlserver's `MS_Description` extended property — `conventions/migrations.md` owns the
   per-dialect spelling), so changing the text in the spec or the schema changes nothing
   until a new pair carries the statement. Cheap and non-destructive, and invisible if it
   is forgotten: the catalogue keeps answering with the old wording.
4. **View evolution** [high-risk]: does the projected shape change? Then every affected
   MATERIALIZED view bumps `Version(N)` — and the root schema's COLUMNS are part of the
   rebuild hash, so a plain field add on the aggregate moves it: the full hash list
   lives in `views` at the pin (read it, not just `mongo-schema-evolution`, whose bump
   list is options-scoped); a missed bump is an unconditional boot abort.
   Views of other entities embedding this one count — **per kind, differently**: a
   `JoinView` embedder is Version-COUPLED (bump it in the same change — the guard fires
   on it otherwise); a `JoinView` leg's `.Fields()` allowlist does NOT pick up a new
   field (silent omission — extending the list is itself a shape change on the
   embedder), while a renamed/removed field a leg still lists is a BOOT REJECT; a
   ComposedView / `Link*` leg has NO `Version` at all — its lever is the leg/schema
   declaration itself. **Archive newly enabled ⇒ settle
   the view's archive regime in this spec** — gated on the view's backing, per
   `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`'s elicitation contract (relational serves
   no `DeleteOnArchive()`); never a silent default — the scaffold side asks this at
   birth, adding the mode later doesn't get to skip it.
3b. **Is the new field one the FRAMEWORK fills?** When what the change adds is a date
   for a business FACT (signed, paid, approved, cancelled) or a count of events on the
   row, and the pin carries the stamped family, it is a `StampedTimeField` /
   `StampedCounterField` rather than an ordinary column — the full contract is owned by
   `../scaffold-entity/conventions/infra.md`, and the domain's half by
   `../scaffold-entity/conventions/domain.md`. Three things belong in THIS spec because
   they only exist on an evolution:
   - **The write surface SHRINKS.** The field is absent from every request DTO, command,
     mapper and OpenAPI request schema. If callers were already sending a value for a
     column being converted to stamped, that is a CONTRACT BREAK on the request side and
     the spec says so rather than discovering it at the verify.
   - **The DDL for a counter needs a default it will not keep.** `NOT NULL` on a table
     that already has rows fails; the ALTER carries a default, backfills, then drops it,
     so the steady state matches what a fresh CREATE writes. A stamped TIME column is
     nullable and needs none — every existing row honestly has no such fact yet.
   - **Nothing asks for the stamp until a rule does.** Adding the column and the schema
     declaration changes no behaviour at all: the framework leaves an unasked-for stamped
     column out of every statement. The rule that calls `e.Stamp("<Field>")` is part of
     this change, on the verb where the fact happens, or the column is added and stays
     empty with nothing reporting it.

4b. **Is the "new field" a COLUMN at all?** If what the change adds is a value that
   belongs to ANOTHER aggregate this entity already holds a foreign key to, the answer is
   a READ JOIN on the repository, not a column: no migration, no data to backfill, nothing
   to keep in step (`${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md`, pin ≥ v0.57.0). Two
   decisions belong in the impact map and they are separate — WHICH columns it brings
   across, and whether the caller RECEIVES them or the field exists for the rules alone.
   A join declared for a rule and then quietly published is an API break nobody planned.
   It is NOT the answer for a 1:N reach, a match on anything but the target's id, a second
   hop, or a value that is genuinely this entity's own — say which one applies.
5. **API impact** [high-risk]: wire-visible changes (json names, removed/renamed fields,
   validation tightening) are BREAKING for consumers — list them, flag them, let the dev
   decide; never smuggle a break in silently.
6. **Translations** — every new/renamed labelKey and notification: all seven catalogs,
   real translations; removed keys leave no orphans.
7. **Tests** — which existing tests change and why (rule changed ⇒ test changes with it,
   shown here, not discovered later), which new branches need coverage. **The project's
   generated contract QA (`specs/qa/*.sh`, runner, fixtures) is an entity-shaped artifact,
   not a frozen oracle**: a deliberate contract change breaks it BY DESIGN — plan its
   update (or a regeneration via `/omnicore:qa`) here, explicitly; that planned update
   is not the "edit a test to pass" sin, silence about it is.
8. **Kind promotion (flat → sharedbase)** [high-risk — only when the change IS this;
   `N/A` otherwise]: the hardest migration in the catalog — never improvise it. The
   pieces, each explicit in the spec: the new base table + the natural-key choice (the
   base PK derives from it — UUIDv5; wrong key = merged/split identities, data
   corruption not a bug) · the FK model (shared-PK vs separate-FK — re-enrollment
   semantics, dev's call) · the data move as a real migration + backfill (up creates
   base + backfills from the flat table, down recreates STRUCTURE only — say so) · the
   role table slims to role-only fields · the identity becomes its OWN bounded context
   (routes file, feature, `<identity>:read` permission — named after the BASE) · views
   and translations follow. Mechanics: `table-schema` at the pin + scaffold-entity's
   `conventions/sharedbase.md` (the write/read contracts are THE reference — this
   skill adds only the migration path). The REVERSE (sharedbase→flat demotion, or
   moving a role to a different base) is the same catalog walked backwards — equally
   spec-gated here, never improvised and never refused as unsupported.
   **On the codegen path this is the widest gap between what regenerates and what does
   not**: `storage.kind` is one key, so the CODE for the role comes back in seconds — while
   the base table, the data move and the backfill are the hand-written migration, and the
   identity's own view is refused by this build (`read.identityView`) and stays hand work.
   Say that in the option text rather than letting "one key" read as "one command".
9. **Generation path — the GATEWAY** [only when Phase 0b found this entity in the
   generator's lock; `N/A — hand-written entity, no codegen path` otherwise]. WHAT changes
   is settled by the sections above; HOW it gets applied is the dev's call, not yours. Ask
   it as a real choice with exactly TWO options, **in the SAME message as the spec gate** —
   the dev is already being asked "is this right?", and a second consecutive stop buys
   nothing; their **1** or **2** then carries both answers. A reply that approves the spec
   without picking has answered the gate and not this: ask this one again, alone. Never
   proceed on silence, never pick for them, and **write nothing into the spec YAML before
   the answer.**

   Before offering option 1, confirm the change is EXPRESSIBLE — `omnicore-gen explain
   keys` / `explain coverage` for the concern at hand, never from memory (most of what
   looks like a missing capability is a key nobody guessed). A part that is genuinely
   outside the language does NOT disqualify option 1: name it in the option text as what
   lands by hand either way — a `rules.manual` item, the gRPC/proto surface, an integration
   event, the identity view of a shared base.

   > ⏸️ **How should this change be applied?** Two options, and only these two:
   >
   > **1. Change the spec and regenerate — `omnicore-gen` (beta) + review by me**
   > The change goes into `specs/omnicore-gen/<entity>.omnicore.yaml`; `check` validates it and
   > `generate` rewrites every file the generator owns — domain, application, web, infra,
   > wiring, translations and its own tests — in **seconds** and at a **fraction of the
   > tokens** the by-hand path costs. It also catches statically what an evolution forgets:
   > a projected shape that moved while `read.view.version` did not is a refusal that names
   > the number, not a failed boot.
   > **What it does not do is touch your database.** A migration is written once and never
   > regenerated — once it has run anywhere the framework records it as applied, so
   > rewriting the file would change what the file CLAIMS and not one table. The new
   > numbered pair, in every target dialect, per the migration strategy above, is written
   > BY HAND — by me, against the shape the generator's report prints. Same for the
   > invariants the spec language cannot express (they live in hook files it never touches)
   > and their tests, and for everything outside its ownership: `microservice.*.yaml`, the
   > proto contract, views of other entities, `specs/qa/*.sh`. I then review the output against
   > this spec, read the report, and prove it with build + vet + tests + a real boot.
   > That review reads the emitted code against the spec that produced it, not just for
   > plausibility — the generator can be wrong too, and its mistakes compile. If I find
   > one, **I stop and ask you before touching a single generated file**: what diverges,
   > what I ruled out, and what `omnicore-gen adopt` would cost (an adopted file stops
   > tracking the spec forever). Editing generated code is a supported move, never a
   > silent one.
   > **Beta**: it is still being improved round by round, and its gate covers a lot but can
   > still hit a case nobody has hit — usually a spec that validates and produces something
   > that does not compile. When that happens I say so, work around it, and it gets fixed
   > upstream; the review and the proof steps above are exactly what catch it.
   >
   > **2. By hand, file by file, by me**
   > I edit every file myself, reading the pinned `/docs` before each layer. Slower and far
   > more tokens, and the same review discipline applies — but nothing depends on the
   > generator or on the spec language covering the change. On THIS entity it carries one
   > permanent cost worth knowing before you pick it: every file the generator owns that I
   > edit stops tracking the spec — `doctor` reports it as a hand edit, the next
   > regeneration refuses it until it is adopted or forced, and later emitter fixes never
   > reach it. I will list what I touched and offer to record each one with `adopt … -why`,
   > which makes `doctor` tell the truth afterwards but does not undo the divergence.
   >
   > Reply **1** or **2**.

   **Neither option is marked recommended** while the generator is in beta: they are
   presented on their merits and the dev chooses. Record the answer in the spec header
   (`Generation: omnicore-gen` or `Generation: manual`) the moment it is given — a resumed
   run reads it and does not ask again, and asking twice invites a different answer
   half-way through one change.

## Phase 2 — Execute the impact map

**Read the spec's `Generation:` line first** — it decides the shape of this phase. Either
way: edit ONLY what the impact map lists, and read the owning `/docs` section BEFORE each
layer you touch by hand — the same mandatory-read rule as scaffolding.

### 2m — by hand (`Generation: manual`, and every hand-written entity)

One pass, in dependency order (migration → schema → domain → application → web → views →
translations → tests). If the entity IS generator-owned and the dev chose this path anyway,
keep the list of every OWNED file you edited and hand it back at the end with the `adopt`
offer promised at the gate.

### 2g — spec + regenerate (`Generation: omnicore-gen`)

The order is not the manual one, and the difference is the point:

1. **Read `${CLAUDE_PLUGIN_ROOT}/skills/omnicore-gen/SKILL.md` NOW** — not before, not from
   memory — and follow it end to end; it owns the language, the refusal ladder and the
   ownership rules. Then `explain keys` and the topics for the concern at hand, BEFORE
   touching the YAML. An evolution is exactly where "the language cannot say this" is most
   tempting, and it has been wrong every time so far.
2. **Clear `doctor`'s findings first.** A hand-edited owned file is REFUSED by `generate`,
   so it would keep the OLD shape while everything around it moves — an evolution that
   half-applies and still compiles. Reconcile each one before regenerating: move the edit
   into the spec (best), `adopt` it deliberately, or `--force=<path>` to discard it. Say
   which you did, per file.
3. **EDIT the existing spec — never `init` over it.** It is the entity's source of truth
   (`init` refuses without `-force` for exactly that reason). Change only what the impact
   map says, and `check` after each block rather than once at the end.
4. **Bump `read.view.version` when the projected shape moved.** The generator compares this
   run's shape against the one it last wrote and refuses to generate on an unbumped
   version, naming the number to use — that refusal is the check working, not a blocker.
   It sees only THIS entity's view: a JoinView embedder in another spec is still yours to
   bump (spec item 4).
5. **`generate`, then read the report as the hand-off, not as a log** — `Refused` (files
   still carrying a hand edit) · **`No longer generated`** (orphans: what the previous spec
   produced and this one does not — they compile and mean nothing) · the migration hand-off
   with the target shape · stale registrations · missing translations.
6. **Write the migration by hand.** A NEW numbered pair, in EVERY target dialect at the
   same number, per the approved strategy — never an edit to a pair that may have run.
   Check the DDL against the report's target shape: columns, nullability, **and the
   indexes** — a uniqueness whose SCOPE changed adds no column, so a shape read only down
   to the columns looks already satisfied while the rule the domain relies on is still the
   old one. The report names one exception: an entity that has never shipped ANYWHERE may
   have its pair deleted and regenerated fresh. Only the dev knows whether that holds —
   ASK, never assume.
7. **Prune — one command for everything the write path only ever adds to.**
   `omnicore-gen prune -spec … -project …` lists the orphaned files AND the notification
   declarations and translation keys the spec no longer declares (a write inserts and
   replaces, it never deletes), plus the lock records for files already gone — the reason
   `doctor` repeats `is gone` forever. Read the three lists, then `-apply`. It leaves alone
   anything hand-edited, adopted, claimed by another entity, or a migration, and says why
   for each. This is how spec item 6's "removed keys leave no orphans" gets done on this
   path; the dead translation key it removes is invisible to every other gate.
8. **Implement the hooks and everything outside its ownership** — the `_manual` rule and
   fact stubs (an unwritten rule leaves an invariant unenforced and the service runs on; an
   unwritten fact PANICS the moment a rule asks for it), plus `microservice.*.yaml`, the
   proto contract and its stubs, views of OTHER entities embedding this one, `specs/qa/*.sh`, and
   the tests for everything you wrote by hand.
9. **Review the tree against THIS spec** (omnicore-gen Step 7): the emitted rules against
   what the change was meant to mean, the DDL, authz, the read side. Anything wrong is
   fixed in the spec and regenerated — never by editing a generated file.

## Knowledge routing — change → `/docs` section

Resolve `<name>.html` at `<omnicore-dir>/docs/content/sections/<name>.html`, where
`<omnicore-dir>` = `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore` (the
version pinned in the consumer's `go.mod`). Read the section for the contract — it is
the authority at that pin. Route first, then read ONLY the routed section(s) for the
change at hand — never sweep the whole manual; the Documentation Map in
`<omnicore-dir>/CLAUDE.md` is the fallback index for concepts this table doesn't list.

| When changing… | Read section(s) |
|---|---|
| field add/remove/rename (Go ↔ column, id shapes) | table-schema · migrations |
| value objects: add/rename a VO field, a new enum, place in vos/ or aggregatevos/ | value-objects · service-layout |
| a value object spanning SEVERAL columns (composite): its decomposition, the exposed part names, the once rule. **Renaming an exposed part is a WIRE break, not a refactor** — that name is the filter, the `?fields=` key, the JSON field, the export column and the projected document key, because nothing above the schema knows a composite exists. And **turning existing flat fields into a composite costs no DDL and no view rebuild**: keep the same columns and pin every exposed name with `.As(...)`, and every name the outside world ever saw is preserved | table-schema · value-objects |
| unique field add/remove → `Constraints` binding · per-engine id/decimal/boolean columns | `${CLAUDE_PLUGIN_ROOT}/shared/dialects/<dialect>.md` (read every TARGET dialect's — targets = `relational.dialect` across ALL `microservice.*.yaml`; the ALTER pair lands in EVERY target's `migrations/<dialect>/` at the same number) |
| rules / notifications / actionName (**a new rule joins the EXISTING clause for its verb** — one `IfInsert`/`IfInsertOrUpdate` block holds every rule that runs on it; adding a second gate beside it for one new rule is the readability failure `conventions/domain.md` names, and it re-runs the verb check per block. **A rule a VALUE OBJECT already carries is not a rule to add** — VO fields are validated automatically on every write, presence included: a string-backed raw VO answers an empty value with `RequiredFieldNotification`, an enum with its unknown-member one, so a `required` beside either makes the caller read the same complaint twice) | rules-dsl · value-objects · old-state · status-mapping |
| WHICH LAYER declares a notification the change adds — a check moving out of `BuildRules` into a handler (or into it), a rejection authored by a handler or an adapter | `${CLAUDE_PLUGIN_ROOT}/shared/notification-bases.md` (owner) · status-mapping · service-layout |
| WHETHER a type/port/interface/constant the change introduces may live in `internal/domain/` at all — a mechanism contract the change needs (hasher, issuer, clock, gateway), protocol vocabulary (claim/header/scope names), a flat const set. The evolution shape that most often smuggles one in: a new rule that needs something computed OUTSIDE the aggregate. If domain code does not consume it, it is not domain — and "the domain package is what both layers can import" is the violation, not the reason | `${CLAUDE_PLUGIN_ROOT}/shared/domain-membership.md` (owner) · service-layout · architecture |
| children / siblings / cascade — AND any NEW table's shape rules (child FK + covering index; sibling shares the owner's PK, no lifecycle columns; role UNIQUE FK; revision column on entity/base tables only) | aggregate-persistence · table-schema · migrations · scaffold-entity's `conventions/{aggregate-children,siblings,migrations}.md` |
| write handlers / in-TX hooks | auto-handlers · lifecycle-hooks |
| full write-path ripple of a change (SQL ↔ outbox ↔ Mongo op ↔ audit verb) | lifecycle-map |
| read-side impact of a write change (backing, kinds, archive regime) | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) |
| reaching ANOTHER aggregate from a query — read joins (repository-declared), and the rule-vs-wire split | `${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md` (owner) · read-joins for version-exact contract |
| a new/changed domain Service fact — existence, counts, totals, extremes, per-key breakdowns — and which primitive answers it | `${CLAUDE_PLUGIN_ROOT}/shared/query-primitives.md` (owner) · custom-command-handler · service-to-service |
| **a field added to a read: it must land on the query's RESULT, not only on the Response.** The Result owns field existence — a Response field with no same-named Result field behind it is a BOOT PANIC, not a silently empty column, and a Result carrying `json` tags is refused. The reverse is safe and sometimes deliberate: a Result-only field feeds a computed field's derivation and never reaches the wire | auto-query-handlers · custom-query-handler |
| **a field removed from a read Response disappears from the EXPORT too** — the Response is the export's column source, so this is one change with two wire consequences, and `?fields=` stops accepting the name on both. Its column header lives in the same place: `exportLabelKey` on the Response field | auto-query-handlers · reference |
| a computed read field added/removed/re-sourced (`read.computed` on the codegen path) — the sources are what `?fields=` pushes down, so changing `from:` changes which columns the store fetches; the derivation body is a hook the generator never rewrites, so a NEW field needs its TODO filled or the column renders empty and nothing says so | auto-query-handlers · `${CLAUDE_PLUGIN_ROOT}/skills/omnicore-gen/SKILL.md` |
| view shape / indexes / `Version` bump (the FULL rebuild-hash list — root columns, embeds, embedder coupling — is in `views`) | views · auto-query-handlers · mongo-schema-evolution |
| SharedBaseView / ComposedView impact | views · query-side |
| REST routes / OpenAPI surface | openapi · reference |
| GraphQL surface | graphql |
| gRPC surface / proto contract + stub regeneration | grpc |
| migration numbering / down twins / dialect | migrations · yaml-reference |
| authz (permission / owner-check / tenant) | authz-seams |
| feature wiring / bootstrap | bootstrap |
| file layout / naming (ANY layer) | service-layout |
| the spec language, its keys and what regeneration owns (codegen path only) | `${CLAUDE_PLUGIN_ROOT}/skills/omnicore-gen/SKILL.md` (owner) + `omnicore-gen explain <topic>` — never from memory |

## Final verify (the gate)

0. **Reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`) — walk the
   plan's own promises item by item with evidence; an unmet target is RED or an explicit
   dev-accepted deviation.
1. **Mechanical checks** (pre-boot, all that apply — run to a CLEAN pass before
   anything boots): every changed `up` has its `down`
   twin, in EVERY target dialect's folder at the same number · projected-shape change ⇒
   `Version(N)` bumped on EVERY affected materialized view (JoinView embedders
   included — spec item 4) · grep the
   OLD field/key name across the repo → no stale references (code, translations, docs,
   tests, view legs/`Fields()` lists) · new labelKeys present in ALL SEVEN catalogs ·
   NOT NULL additions carry their
   default/backfill · id/FK column shapes match the pin's identity contract
   (`table-schema` — the authority, never memory) · a new Response field beside a
   request declaring `Fields` is `*T`/slice + `,omitempty` (the recursive sparse-render
   guard PANICS at boot otherwise) · Archive newly enabled ⇒ `Modes()` ⟺ schema
   archive-column ⟺ migration in lockstep · a value-typed `query:`-tagged scalar
   renders REQUIRED in OpenAPI (pointer unless the spec says required) · any NEW table
   follows the shape rules (revision column on entity/base only, no leading-`_`
   physical column, unique constraints named on the OWNING table).
   **On the codegen path, four more — all cheap, all static:** `omnicore-gen doctor` runs
   clean except for adoptions you made deliberately and named (an unexpected `! was edited
   by hand` means an owned file drifted DURING this change) · **`omnicore-gen prune` comes
   back with nothing to remove** — the one check that covers both the orphaned files and the
   dead translation keys, which no other gate can see · the new migration pair exists
   in every target dialect and matches the report's target shape, indexes included · the
   spec YAML and the tree agree (`doctor` does not say "the spec changed since the last
   generation" — that would mean the last `generate` never ran).
2. **`gofmt -l` + `go vet` + `go build`** (engine + transport tags) — clean.
3. **Unit tests** — updated branches green; never weakened to pass.
4. **BOOT the service** (a verification boot: dev profile, target tags, log to a file —
   never through a pipe — then SIGTERM; the same in-gate boot the sibling skills do —
   distinct from the interactive `/omnicore:run` offer below). Every characteristic
   failure of an evolution is boot-time, not compile-time: migration apply/dirty,
   schema binding, the sparse-render guard, `publicRoutes` exact-match, view drift.
   Know the healthy shapes: under non-dev profiles pending migrations
   (`autoRun: check`) and a pending view rebuild ABORT on purpose; `/readyz` 503
   `rebuilding view` = wait, not a failure (`shared/boot-contract.md`). A green build
   that was never booted is NOT a verified evolution.
5. **Regression** — the project's existing suite if it has one (proves no breakage);
   contract changes hit the planned qa updates from spec item 7, nothing unplanned.
6. **Offer to run.** Ask ONE question: boot the app to click through the changed
   endpoints? Yes → delegate to `/omnicore:run` (never boot inline). No → done.

Leave `specs/evolve-entity/<entity>/` in place so the dev can review the plan against the
diff.

## Re-entry — spec already exists

`Status: DRAFT` → reopen the gate with what's answered. `Status: APPROVED` → apply only
the not-yet-applied items of the impact map, then re-verify. A changed answer reopens
the spec. **`Generation:` is never re-asked** once it holds an answer — a resumed run reads
it and continues on that path; only the dev reopening it changes it.

## What this skill never does

No framework edits, no git, no test edited to pass, no wire-breaking change applied
without it being flagged in the spec the dev approved, nothing edited outside the
approved impact map. On the codegen path: never hand-edits a generated file (the spec is
where a wrong output is fixed), never `init`s over an existing spec, never rewrites a
migration that may have run, and never regenerates an entity the lock does not record —
that is a rewrite of hand-written code, not an evolution.
