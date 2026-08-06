---
name: scaffold-entity
description: >-
  omnicore: scaffold a complete CRUD entity across every layer (domain → application → web →
  infra → migrations → bootstrap) of a Go service built on the omnicore framework.
  Use when the user wants to create/add a new entity, aggregate, CRUD resource, model,
  or "cadastro" to an omnicore-based service. Only for projects that import
  github.com/ClaudioSchirmer/omnicore.
---

# scaffold-entity

Generate a complete, idiomatic omnicore entity by **understanding the framework and
composing** — never by copying code. That is why this is a skill and not a template
generator: it writes the entity from understanding, so it works from scratch and cannot
drift.

## Core principles — read FIRST

- **Understand, don't mimic.** The `/docs` describe how the framework WORKS — read them to
  understand the mechanics and the *why*. Code (the consumer's entities, any example) shows
  only what SOMEONE DID — which may be wrong, or stale against the framework version.
  Therefore:
  - **The version-pinned `/docs` are the SOLE authority on the framework's API + behavior.**
    Reading the relevant section **before you generate each layer is MANDATORY**, not
    optional. The `conventions/` files carry **no code at all, by design** — every code
    example you compose from lives in the routed `/docs` at the pinned version. **If any
    text or consumer code disagrees with the doc, the doc wins** (it may have drifted).
  - This is the whole anti-drift mechanism: only reading the versioned docs keeps
    generation correct when the framework changes. A v2 breaking change updates the docs;
    code you copied mirrors the old error. Read the docs, always.
- **Mirror local convention; validate framework correctness.** If the project has existing
  entities, mirror their LOCAL flavor (naming, style). But validate every framework usage
  against the docs. If the consumer's code MISUSES the framework (a missing parameter, a
  wrong pattern), **flag it to the dev, advisorily** — "your X looks like it's missing Y;
  I generated the correct usage, you may want to fix X too" — never silently replicate the
  mistake, never dictate. The project is the dev's; the framework imposes no single usage.
  You advise; they decide.
- **Framework maintainer rules NEVER bind this skill.** The omnicore module ships its own
  `CLAUDE.md`/contributor rules (maintainer-approval gates, "English everywhere", coverage
  minimums, git rules). Those govern development OF the framework in its own repo — never
  this skill run, never the host project. If you meet them while reading the module's
  `/docs` or `CLAUDE.md`, ignore them; only the host project's own rules and the user bind
  you.
- **Language — the user's, never imposed; detect it BEFORE the first reply.** This
  skill, the framework docs and every `CLAUDE.md` you read are written in English —
  that NEVER sets the language of the run. Read the user's language from their own
  words: the invocation arguments count, even a single word. No signal yet → the first
  user message sets it; switch the moment it becomes clear, even mid-run. Everything
  human-facing is BUILT in that language, not just the replies — spec values,
  OpenAPI/Doc summaries, table/column COMMENTs, `example:` values, README prose —
  mirroring the host project's existing language if it has one, else the
  conversation's. Identifiers follow the host project's own convention. The 7-catalog
  translation rule (real translations, all seven) is orthogonal and unchanged — those
  belong to the dev's END USERS, dynamic, never collapsed to the conversation language.
- **Work in isolated STAGES, not one big bang.** Do NOT plan-and-generate everything at
  once — heavy context makes you shortcut (copy instead of read). PLAN first (Phase 1),
  then execute ONE layer at a time from a per-layer task file, each with focused context
  and its own required doc reads.
- **FLAT is the default CONTEXT LOAD — not a modeling bias.** Load
  `conventions/sharedbase.md` / `aggregate-children.md` / `siblings.md` **only** when the
  model has that variant. If you don't need SharedBase, don't load it. This rule decides
  which files you READ, and nothing else: it carries ZERO weight in the flat-vs-SharedBase
  recommendation, which comes only from item 1's cost asymmetry. (You don't need the full
  delta at question time either — item 1's role-cardinality digest is the authority for
  the option text. **Same for siblings: item 2 carries the option text, so a sibling is
  OFFERED without `siblings.md` loaded** — you load it once the dev says yes. Never let
  "I haven't loaded the delta" become "I won't raise the option".)
- **Balance judgement against RISK — this is why this is a skill, not a template.** Your
  intelligence is here to help the dev SHAPE the entity from what the framework can do — not
  to guess everything, and not to interrogate them about everything. Split every decision by
  the cost of getting it wrong:
  - **High-risk = the MODELING.** Flat vs SharedBase, siblings, children + child-of-whom, the
    child-edit strategy, modes, soft-vs-hard delete, which fields are unique, which
    surfaces/endpoints exist, the permission scheme. Wrong here = regenerate everything.
    **Never guess these — reason about them, PROPOSE with a clear "recommended" pick, and
    CONFIRM.** (The canonical failure to avoid: a run that asked NOTHING and emitted ~10
    endpoints out of thin air — REST + CSV + XLSX + a permission it invented — pure guessing.)
    **Do NOT relabel these as "lower-risk defaults I'll apply, veto later" and bury them** —
    that hides the trade-off. For each
    high-risk pick, SHOW the trade-off: state the alternative(s), why you'd pick one, and let
    the dev choose. The plan-review gate is for a dev to weigh a VISIBLE trade-off, not to
    hunt for buried decisions.
  - **Low-risk = the DETAILS.** An `example:` tag value, a sensible filter-operator set per
    field, a Doc summary. Wrong here = a one-line edit. **Just decide them well and move on —
    don't ask.** (Do the cheap things well — e.g. never leave every OpenAPI `example:`
    rendering as `"string"` — and never punt them to the user.)
  - The skill's job is a GOOD BALANCE: guess the modeling = high risk (don't); suggest the
    modeling with a recommendation = the channel; decide the low-risk details yourself.

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

## Phase 0a — Preflight: is there a service to graft onto?

This skill **adds an entity to an existing omnicore service** — it does NOT create a
project. And it adds a NEW entity: if the requested entity already exists in the
service, changing it is `evolve-entity`'s job and deleting it is `remove-entity`'s —
hand off, don't regenerate over a living entity. A read model BEYOND the entity's own
view (composed across entities, shared-base identity, upstream/embed) is
`scaffold-view`'s job. Before anything else, confirm the host exists:
- **`go.mod` present AND it requires `github.com/ClaudioSchirmer/omnicore`** (check with
  `go list -m github.com/ClaudioSchirmer/omnicore` — it must resolve).

If that fails (empty folder, no `go.mod`, or a `go.mod` that doesn't require omnicore),
**STOP — do not scaffold**. The skill cannot run: with no omnicore require it cannot even
resolve the `/docs` it is required to read, and there is no `wire.go`/feature/`migrations/`
to hook into. Tell the user plainly:

> This directory isn't an omnicore service yet (no `go.mod` requiring
> `github.com/ClaudioSchirmer/omnicore`). `scaffold-entity` grafts an entity onto an
> existing service; creating the service skeleton from an empty folder is the job of the
> **`scaffold-service`** skill — it creates `go.mod` + the bootstrap shell + the
> `microservice.*.yaml` profiles + the docker bench (DB, Mongo, broker, CDC relay) and
> proves the empty shell boots. Want me to run `scaffold-service` here now?

Do not try to improvise a `go.mod` / bootstrap to work around this — that is
`scaffold-service`'s job, and guessing it here would produce an unbootable stub. If the
dev says yes, invoke `scaffold-service`; when its final verify is green, resume THIS
skill from Phase 0b.

## Phase 0v — Version check: offer the latest before generating

Host confirmed (0a), and BEFORE reading any docs (0b onward) — an accepted upgrade changes
WHICH version's `/docs` are authoritative, so it must resolve first.
1. **Current pin:** `go list -m -f '{{.Version}}' github.com/ClaudioSchirmer/omnicore`. If it
   resolves to a LOCAL checkout (`replace`/`go.work` → `(devel)` or a path), **skip this whole
   step silently** — a working copy can't be bumped; go straight to 0b on what's there.
2. **Newer published?** `go list -m -u -f '{{with .Update}}{{.Version}}{{end}}'
   github.com/ClaudioSchirmer/omnicore` (empty = already current). Proxy unreachable/offline →
   **skip silently** and proceed; never block generation on a network check.
3. **Newer exists → DELEGATE to the `upgrade` skill** (`/omnicore:upgrade`) — do NOT bump
   inline. It owns the whole flow: show the target version's `changelog.html`, offer, and on
   the dev's yes run `go get` + `go mod tidy` + build **with rollback if the build breaks**.
   Invoke it, and when it returns continue to 0b on whatever the project now pins — the new
   version if the dev upgraded, or the current one unchanged if they declined or rolled back.
   (Same handoff shape as 0a delegating an empty folder to `scaffold-service`.)

A convenience gate, not a barrier: usually the check finds nothing and passes instantly. Never
upgrade without the dev's explicit yes (the `upgrade` skill enforces that); never let the
check itself abort a run.

## Phase 0b — Discover the project (read, don't ask)

Self-configure by reading the project:
- **Dialect(s):** `relational.dialect` across every `microservice.*.yaml` (resolve
  `${VAR:default}`) + the existing `migrations/<dialect>/` folders. Their union = the
  target dialects (drives migrations + repo constraint bindings).
- **Module path** (`go.mod`), the layer layout, wired surfaces (REST / GraphQL).
- **Framework version = whatever the project pins.** Read it from the Go configuration:
  `go list -m github.com/ClaudioSchirmer/omnicore` (i.e. `go.mod`). THAT version's `/docs`
  — resolved from the module cache — are the authority this skill reads. The skill itself
  is version-agnostic and never assumes a release: the doc-first read is the whole
  anti-drift mechanism.
- **An existing SharedBase?** Look for a `NewSharedBaseSchema("…")` schema (e.g. `persons`) + its
  identity view (a `SharedBaseView(…)`, e.g. `person_view.go`) + the roles already on it. If
  the entity being scaffolded is a NEW role over that SAME identity, you REUSE the base (declare
  the SAME `NewSharedBaseSchema("…")` — the registry keys by table) and ADD to the existing
  identity view (don't recreate either). This is the "add a role to an existing base" path
  (drives item 1's create-vs-add-role question).
- **Existing value objects (`internal/domain/vos/`).** Enumerate what the project already
  has — an `Email`, `ZipCode`, `Document`, a `Relationship` enum — BEFORE modeling any
  field: a field whose rule matches an existing VO REUSES it (import `vos`, no new type),
  never a second copy; a new VO is created only when none fits. This inventory feeds the
  spec's §2 `VO?` column so the reuse-vs-new call is decided per field, visibly.
- **A domain map from `scaffold-system`?** Look for `scaffold-system/domain-map.md` —
  whether this run was delegated by that skill or invoked directly in a project that has
  one; if it exists, reading it is MANDATORY, not optional. `Status: APPROVED` and it
  lists THIS entity → its §9 block is the dev's already-approved answers: Kind, base
  create-vs-reuse, natural key, child-of-whom, the identity-view verdict
  (create/add-role/skip — decided once at the map's §3, surfaced in §9) enter the spec
  as DECIDED (marked `per
  domain-map §9`, not `(proposed)`, never re-asked); everything §9 doesn't answer stays
  this run's own, per the normal risk split. Your 0b discovery contradicts the map →
  STOP and surface; the dev amends the map first (never silently obey either side).
  `Status: DRAFT` → the system gate isn't done: surface it and ask — finish the map in
  `scaffold-system` (recommended) or proceed explicitly independent of it. Entity absent
  from an APPROVED map → flag advisorily (the system plan may be stale) and proceed
  normally. When the run was delegated, also skip Phase 0v — the orchestrator resolved
  it once for the whole system.
- **The read-side posture (view backing default).** In order: a delegated
  `domain-map.md` §1p / this entity's §9 `View backing` (authoritative when present) →
  else `scaffold-service/spec.md` in the project root (a fresh project just scaffolded)
  → else INFER from existing per-entity views (do any carry `.RelationalSource()`?). It
  sets the DEFAULT for item 5's view-backing decision; only when NONE of these is on
  record does that item ASK. Never silently pick one when nothing is found.
  **INVARIANT — the infra posture NEVER constrains WRITE-SIDE modeling; it restricts
  only which view KINDS can be served.** Model what the domain IS and offer SharedBase
  whenever it fits, never softened or skipped because Mongo is absent — and never offer
  a view kind the posture cannot serve. The full statement, capability rule and
  elicitation contract are OWNED by `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` — read
  it whenever the posture is anything but full-Mongo, and route to it instead of
  restating posture facts.
- **`/docs` + `conventions/` are the ALWAYS-ON basis — read them every run, whether the
  project is empty OR already full of entities.** Existing code is never a substitute for
  the mandatory per-layer doc read (Core principles); "the project already has entities"
  changes nothing.
- **Existing entities are an ADDITIONAL, local-flavor input layered ON TOP** — mirror their
  naming/style/file layout, but validate every framework usage against the `/docs`, and flag
  misuse advisorily. They are NOT the API authority and NOT a reason to read fewer docs. If
  the project has none, you lose only the local-flavor hint — the docs + conventions you read
  are identical. **Do not depend on the `omnicore-example-users` reference repo — it is an
  authoring reference for this skill and won't exist in a real project.**

## Phase 1 — Model + PLAN (the one broad step)

**1a. Think in ER/storage terms FIRST, then fill the spec.** Before proposing anything, sketch how
this entity would be **stored** — the tables, their PK/FK, and **where each field lives**.
This is what makes the structural forks *visible* instead of guessed: a field group that
should dedup across a shared identity ⇒ a SharedBase; an optional/bulky 1:1 group ⇒ a
sibling; a 1:N group ⇒ a child (of the base? of the role?). You cannot ask the right
questions until you've placed every field on a table. Do this thinking even for a "simple"
request — "a student with name, email, studentNumber" still deserves the question *"could a
student also become a professor/staff for the same person?"* before you pick flat vs base.

Then **FILL THE SPEC — don't walk a prose checklist from memory.** Copy the template in
`conventions/spec-template.md` VERBATIM to `scaffold-entity/<entity>/spec.md` and fill
EVERY slot: low-risk slots you just decide; each high-risk slot gets your recommended pick
marked `(proposed)` with the alternative(s) named beside it; a slot you cannot responsibly
recommend becomes `⚠️ OPEN: <question>`. No section may be deleted — an inapplicable
section stays, marked `N/A — <why>`. **Completeness is STRUCTURAL:** a blank or missing
slot is a visible defect at the gate, not a forgotten question, and generation cannot start
while one exists. Slots pre-answered by an APPROVED domain map (Phase 0b) enter as
DECIDED, marked `per domain-map §9` — the `(proposed)`/`⚠️ OPEN` discipline applies to
what the map left open, and items 1–11 below are read through that filter (an answered
fork is not re-asked).

The guidance for filling each section — the reasoning, trade-offs, and what to confirm
(items → spec sections: 1→§1 · 2→§4 · 3,4→§3 · 5→§5 · 6→§7 · 7→§6 · 8→§2 · 9→§8 ·
10→§9 · 11→§10):
1. **Standalone (FLAT) vs party-role (SharedBase).** Own table, or a ROLE over a shared
   `persons`-like identity that could gain OTHER roles later? **The signal is the entity's
   NATURE, never the request's wording — when the FIRST role is modeled, the second role
   does not exist yet, so "the request doesn't mention another role" detects nothing.**
   Detect the **identity smell** instead: the field set carries a real-world party/asset
   identity — a person (name + document/tax-id and/or email/birth date) or any asset with
   a natural registry key (a property by land-registry number, a vehicle by VIN, a company
   by tax-id) — PLUS role-specific fields (enrollment number, salary, grades, rent
   amount…) — that is an identity PLAYING A ROLE (student, employee, customer, patient,
   listing, sale mandate…), the classic party-role shape. Then:
   - **Identity smell present → the Kind slot is ⚠️ OPEN, never self-answered.** Whether a
     future role will share this identity lives in the dev's head, not in the request —
     ask literally: *"could this also become a &lt;other-role&gt; for the same
     person/party one day?"* State the cost asymmetry when asking: starting flat and
     migrating to a base later is a REAL migration (new base table, PK re-derivation to
     UUIDv5, data move); a SharedBase that never gains a second role is only mild extra
     structure (base table + upsert semantics + an identity view).
   - **If the request ALREADY names the other roles** (even as "out of scope for now"),
     that question is answered — do NOT re-ask it, and do NOT self-answer the one that
     replaces it. The OPEN question becomes role cardinality, asked literally: *"can the
     same &lt;identity&gt; hold TWO ACTIVE &lt;this-role&gt; rows at the same time?"*
     No → this entity IS a role: SharedBase fits natively, and its 409 enforces that
     business rule for free. Yes → it is not a role (a plain 1:N off the identity):
     propose flat, noting the shared identity can still be extracted later when the
     other roles arrive.
   - **Role-cardinality digest — the ONLY mechanism facts the option text may state.**
     Never describe SharedBase mechanics from memory when formulating the question; use
     these lines, or quote the shared-base section of `table-schema.html`: the framework
     invariant is **at most ONE ACTIVE role row per identity per role table** (409 on
     `POST` and on `/unarchive`); **separate-FK** permits 0..N archived remnants plus one
     new active row — sequential re-role over time fits natively; **shared-PK** caps the
     role at one row per identity forever. Writing "1:1 per role" without the word
     ACTIVE is the canonical mis-summary — it conflates the two link models and wrongly
     disqualifies sequential over-time cases.
   - **No identity smell** (Product, Invoice, Warehouse…) → propose **flat** and move on —
     no question, no friction.
   Yes → SharedBase (`sharedbase.md`); genuinely standalone → flat.
   - **If SharedBase → the NATURAL KEY is the single highest-risk slot of the whole spec.**
     It derives the identity PK (`UUIDv5(naturalKey)`) and IS the dedup key — the wrong
     field means identities wrongly merged or split (data corruption, not a patchable bug).
     Propose the field (a document/tax-id is the usual pick), say why, and CONFIRM it
     explicitly — never infer it silently.
   - **If SharedBase → SETTLE the all-in-one identity read (the `SharedBaseView` / identity
     view — the READ counterpart of the shared write, unique to SharedBase) — but the offer
     is GATED ON ONE QUESTION: does the project HAVE Mongo?** The KIND needs Mongo; the
     SharedBase read does not depend on it. Mind the axis — **a full engine whose entity
     views are relational-backed still HAS Mongo: offer it there**; only a Mongo-less infra
     (SQLite / zero-infra MVP) closes the door. **No Mongo → do NOT offer it as available
     and do NOT go silent**: point at the per-role plain view the dev already gets (base
     fields FLATTENED — with one role it matches what a Mongo view would carry), and raise
     the complement-later route (`/omnicore:configure`) ONLY if the dev actually wants the
     multi-role identity document. The script is owned by
     `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (Kinds + elicitation contract) — follow it,
     don't restate it. **Mongo present → offer it**, explaining WHAT it is: one document per
     identity, base fields + base-children flat, a sub-document per role, roles added one at
     a time. Two cases, detected in Phase 0b: (a) **no identity view exists yet** → offer to CREATE it
     (`SharedBaseView("<identity-collection>").Schema(<base>).Role(<thisRole>)…`); (b) **an identity view already
     exists** (you're adding a NEW role to an existing base) → offer to ADD this role: append
     `.Role(<thisRole>Schema())` **and BUMP its `Version(N)`** (the role set is in the rebuild
     hash — forgetting the bump aborts boot). **Ask which** (create / add-role / skip);
     recommend it. **Tone — do NOT say "it's costly to add later": that's false.** A view is an
     additive projection over the same CDC stream (nothing changes on the write side), so
     adding or extending it later is the SAME effort as now; it just triggers the standard
     automatic view rebuild (trivial on a fresh service; a one-time automatic rebuild over
     existing data later). Present it as a neutral "want the identity view?" — no manufactured
     debt. The offer INCLUDES its read surface: the standard by-id + by-params pair with
     filters (`sharedbase.md`) — never a lone by-id. See `sharedbase.md` (Read).
2. **Siblings (1:1).** Any optional/sparse/bulky field group better split into a 1:1
   satellite than left as nullable columns? Name it, recommend, ask.
   **The moment the model has ANY optional/nullable field, this question is answered IN THE
   OPEN — never resolved in your head.** Name the candidate group, give the recommendation
   WITH its one reason, and let the dev pick: bulky / rarely-read / PII / genuinely sparse
   facets earn a satellite; two optional scalars usually do NOT — but "keep them as nullable
   columns on the root" is a RECOMMENDATION TO SHOW, not a call to bury. **Deciding NOT to
   split is the same modeling decision, of the same risk class** — it lands in the spec's
   `Lives on` column either way, and "I considered a sibling and dropped it" reaching the dev
   as silence IS the buried trade-off the high-risk rule forbids. Silence is allowed ONLY
   when the model has no optional field at all. **A sibling attaches
   ONLY to a single-owner node — a flat root, a ROLE, or a role-child — NEVER to a SharedBase
   (the base) or a base-child (the framework panics).** So in a SharedBase model, split the
   ask: a BASE-level 1:1 facet (shared across roles) → nullable columns ON the base, NOT a
   sibling; a ROLE-specific 1:1 facet → a sibling on the role table. (`siblings.md`)
3. **Children (1:N).** Which collections? For each: **child of whom** (base vs role/flat)?
   Independently-managed child → its OWN aggregate (FK), not nested — **and that call is
   SHOWN, never applied in silence**: name the collection, say which way you'd model it and
   the one reason (is it edited/listed on its own, or only ever through the root?), and let
   the dev decide. Nested vs own aggregate is a regenerate-everything mistake.
   (`aggregate-children.md`)
4. **CRUD shape + child-edit strategy** (high-risk — never silently default to a wholesale
   replace-all). **Recommended default: a complete insert of the whole aggregate +
   a root-only update + per-child operations (**add** / **update** (404 if the child isn't
   found — a plain update, never a create) / **archive**, by id)** — NOT a
   replace-all PUT that deletes any omitted child. Propose this, name the alternatives
   (replace-all; promote a child to its own aggregate), confirm. For SharedBase **base-**
   children, warn that a role's wholesale replace would delete children shared with OTHER
   roles. **And when per-child endpoints ARE wanted for a base-child (shared), offer a LIGHT
   follow-up (two valid options, not an obligation): A) keep it under the role (simple; just
   be aware a future role would expose its own edit routes for the same shared rows — a
   possible consequence, not a problem), or B) elevate it to its own root aggregate with
   dedicated endpoints (one edit home).** Don't frame A as temporary or as debt — "you can
   switch later without rework." See `aggregate-children.md`.
5. **Modes — ASK, never assume all six.** Which of insert / update / delete / archive /
   unarchive does this aggregate accept? Recommend a set from the entity's nature — the
   named patterns give you the vocabulary: **append-only** (`Display,Insert` — a ledger,
   immune to edit AND archive) · **freeze-once** (`Display,Insert,Archive,Unarchive`,
   no Update — issued documents) · **full CRUD** (the default six-ish) — but you
   MUST confirm — do NOT silently emit all six. (Archive modes ⟺ the schema's
   archive/deleted-at column declaration — the pin's table-schema docs name the builder.)
   **Settle the view BACKING FIRST (independent of modes/archive) — the archive regime
   below depends on it.** The project read-side posture (from `scaffold-service` /
   `scaffold-system`, if set) is the DEFAULT; with none on record (a lone entity run),
   ASK once, neutrally. It applies ONLY to this entity's OWN plain per-entity view —
   never a view KIND (`scaffold-view`'s). Trade-offs, eligibility (a plain view rooted
   at a shared-base ROLE stays relational-eligible), the loader-reuse rule and the
   no-lock-in flip: `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` — the owner, do not
   restate it. `relational-view` at the pin only for version-exact capability answers.
   **Then, when Archive is in the set, settle the VIEW's archive regime** — never a
   silent default, and GATED on the backing just settled (relational serves no
   `DeleteOnArchive()`): the exact question shape is in `shared/read-side.md`'s
   elicitation contract; the pin's `views` section carries the served contract.
6. **Business rules, validations, restrictions — ASK; do not skip and do not invent.** What
   must `BuildRules` enforce? Required fields, formats (email, document/tax-id), numeric
   ranges (e.g. a grade 0–100), string lengths, cross-field invariants, immutability, state
   transitions, uniqueness-driven checks, and per-group caps (counts/totals per key,
   distinct-key limits — "at most N active X per category", "no more than K categories").
   The domain is where the entity earns its keep —
   a run that asks nothing ends up with only "required + length" boilerplate. Elicit the
   real rules (propose sensible defaults per field type, then confirm), and map each to an
   `IfInsert/IfUpdate/IfInsertOrUpdate/IfArchive/IfUnarchive/IfDelete/IfDisplay`
   closure with a specific notification. (**`IfDisplay` caveat**: no framework path
   dispatches Display to `BuildRules` today — don't generate dead display rules;
   confirm against the pin's `rules-dsl` before using it.)
7. **Delete semantics — archive OR hard delete (rarely both).** An archive model
   (archive/unarchive + the schema's archive column) OR a hard-delete model. One simple
   question; **default archive**. Don't emit both a hard `DELETE` and archive/unarchive
   unless the user says so.
   - **The HTTP verb MUST match the truth (DDD/REST naming, not implicit surprises):** `DELETE`
     is EXCLUSIVELY a hard purge; a soft removal is `PATCH …/archive` (+ its `…/unarchive`
     undo). Never wire a soft/archive operation behind `DELETE` — it lies to the caller. This
     applies to the ROOT **and to per-child ops** (a soft child-removal is `PATCH
     …/:childId/archive`, not `DELETE`; see `aggregate-children.md`). A soft operation must
     ship its inverse — if a specific unit needs its own reversible archive⇄unarchive
     lifecycle, that unit is an aggregate, not a nested value object.
8. **Unique fields.** Which fields are unique (email? a document/tax id? a number)? A real
   modeling choice, so **ASK** (e.g. "should `email` be unique?"); don't leave it implicit.
   And per unique field, surface the ENFORCEMENT STYLE in the spec: **(recommended) a
   domain-Service pre-check in `BuildRules` + the DB unique as race backstop** (the
   duplicate reports together with the other validation errors — defense in depth) vs
   constraint-only (simpler; the duplicate surfaces alone, as a 409, after everything else
   passes). See the 5-point chain in `infra.md`.
9. **Update shape** — PUT · PATCH · both? Default PATCH (low-ish risk — recommend + confirm).
10. **Surfaces + reads.** Which surfaces: **REST? GraphQL? gRPC** (separate skill)? **Exports
    (CSV/XLSX)** — yes/no? These shape the whole web layer → **high-risk, ASK; never emit an
    endpoint or an export unasked.** Filterable/sortable/searchable fields + their operators
    are low-risk → infer + show. by-id / by-params reads are expected defaults.
    **Never cut a surface because the project is an MVP** — GraphQL, gRPC and exports work on
    EVERY posture, SQLite included (`${CLAUDE_PLUGIN_ROOT}/shared/capabilities.md`, the owner,
    states availability BOTH ways). Refusing an available capability is the mirror image of
    offering an unavailable one, and just as wrong.
11. **Authorization — TWO distinct questions, both asked (don't invent silently):**
    - **Permission gate (Layer 1):** the `resource:action` taxonomy per operation (generic vs
      per-op). Propose one, confirm — never fabricate a permission string unasked.
    - **Data-access security (Layer 2/3):** does a caller see/edit only THEIR OWN rows, or a
      tenant's subset? This shapes the command/query `ctx` gateways — an owner-check in
      `BuildRules` fed from `ctx.Identity()`, or a tenant filter in `ToCriteria`. Never write
      the commands+queries layer with zero access rules just because nobody asked.
      Ask: "who can read/modify which rows?" If the answer is "anyone with the
      permission," say so explicitly; don't just skip it.

Fields + Go types (nullable ⇒ pointer), `example:` tags, per-field + per-table
descriptions, and filter operators are low-risk: decide them yourself and show them FILLED
in the spec (§2/§9) — do NOT punt them.

**This is a hard STOP gate, and the SPEC is what passes through it.** Send ONE consolidated
message that **OPENS with a single loud status line — `⏸️ PAUSED — spec awaiting your
approval; nothing generated yet.`** — the ask must be the FIRST thing the dev reads, never
buried after the summary (a long summary that only asks at the end reads as a completion
report and gets skimmed past). Then: a short summary of the proposed model, the path to
`spec.md`, and the list of
`(proposed)` high-risk picks + every `⚠️ OPEN` question — then **wait before generating a
single file.** **Until the dev's ok, `spec.md` is the ONLY artifact you write — no
`tasks.md`, no `task_<layer>.md` (those are 1b, which runs AFTER this gate): a vetoed
structural pick would turn pre-written task files into stale garbage. Sole exception: the
simple-model merge (1c proportionality — flat, no children/siblings/service).** The dev
answers only where they differ (in chat, or by editing `spec.md`
directly — both are fine); a plain "ok" accepts every `(proposed)` pick, but **⚠️ OPEN
slots MUST be answered — they cannot be defaulted or okayed away.** A spec that silently
baked in modes/rules/security as unmarked values is NOT eliciting — every high-risk
decision must be visible as `(proposed)` or `⚠️ OPEN`. When agreed, write the resolved
values into `spec.md`, flip `Status: APPROVED`, and only then generate — the approved spec
is the model authority for every later phase. Never drip questions one message at a time,
never re-ask an answered item, never generate with a DRAFT spec or an OPEN slot.

**Tone — advise, don't lecture.** Present every recommendation and trade-off as neutral advice
with a clear pick, not a verdict. Do NOT manufacture future "debt" or urgency ("fine for now,
but you'll HAVE to migrate later"), and never imply the simpler choice is a mistake to be
corrected. A choice the dev makes — even the plain one — is legitimate; note consequences
lightly ("a possible outcome, not a problem; switchable later without rework") and move on.

**1b. Write the task PLAN — only AFTER the 1a spec gate passes** — into a VISIBLE working
dir in the project root:
**`scaffold-entity/<entity>/`** (e.g. `scaffold-entity/student/`). Not a hidden/UUID dir,
not your scratch dir — the dev must be able to open and read it. **Do NOT delete it on
success** — leave the plan + per-layer tasks in place for the dev to inspect (add
`scaffold-entity/` to `.gitignore` if they don't want it committed; that's their call).
- **`tasks.md`** — the CONTROL: the ordered layer list, a status per layer (pending/done),
  and a pointer to the APPROVED `spec.md` (the model authority — don't restate it).
- **One `task_<layer>.md` per layer** to generate (domain, application, web, infra,
  migrations, bootstrap, tests; + a delta task for sharedbase / children / siblings if the
  model has them). Each task file records:
  - the **model decisions** that touch this layer,
  - the **EXACT `/docs` sections to READ** for this layer (from Knowledge routing),
  - the **`conventions/<layer>.md`** to apply,
  - the **acceptance check** (what "done" means for this layer).

**Task files are PROSE ONLY — no code sketches.** A code sketch invites copy-paste in
Phase 2 no matter how loudly it's labeled a draft, and its guessed signatures are pure
drift risk; the real code is derived from the routed `/docs` at execution time. If a
direction genuinely needs stating, state it in words ("lenient handler, `ApplyPartiallyTo`,
guard by id → 404"), never in Go.

**Task files NEVER restate convention-owned mechanics.** File names, file counts,
migration granularity, DTO layout, naming patterns, the language-catalog set — that is
legislated by
`service-layout.html` + the layer conventions, which Phase 2 reads AFTER the plan is
written; a task file that pre-specifies them is guessing, and a wrong guess creates two
conflicting authorities inside one task (the exact bug that produced 5 per-table migration
pairs against a convention mandating one). Write "granularity/naming: per
`service-layout.html` / `conventions/<layer>.md`" and nothing more. The same don't-restate
rule that keeps conventions from copying the docs applies one level up: the plan carries
the MODEL, never the mechanics.

**Enumerate the WHAT, never the files.** The failure always enters through a "what to
generate" list: enumerating tables slides into enumerating `000N_` migration file names;
enumerating operations slides into `.go` file names. The rule: a migrations task lists
TABLES (in FK order, with their columns/constraints); a web task lists OPERATIONS and
routes; a domain task lists TYPES and rules — **never a generated-file name, anywhere in
any task file.** Citing the standard and then numbering files three lines later is the
exact contradiction this ban exists to kill.

**1c. Plan-review STOP gate.** After writing `tasks.md` + all `task_<layer>.md`, **and
BEFORE presenting, SELF-LINT the plan mechanically** — run against your own task files:

    grep -nE '[a-z0-9_]+\.(go|sql)|000[0-9]_' scaffold-entity/<entity>/task_*.md

Every hit naming a TO-BE-GENERATED file (a command/query/DTO/routes `.go`, a `000N_*.sql`
pair) is a 1b violation — replace it with the WHAT it was trying to say (the table, the
operation, the type) + "naming/granularity per `service-layout.html`", and only then
present. (Hits that reference EXISTING files to edit — `wire.go`, `notifications.go` — or
doc/convention filenames are fine.) Then **STOP and
present the plan to the dev for approval before executing a single task.** Open the gate
message with the loud status line — `⏸️ PAUSED at the plan gate — no code generated yet;
reply "go" to execute.` — never bury the ask at the end of a long summary. Point them at
`scaffold-entity/<entity>/` and give a short summary (the layers, the per-layer docs you'll
read, the acceptance checks). Say clearly it's a *plan/draft* — a dev who knows the framework
can validate the direction; a dev who doesn't may just okay it, and that's fine — the point is
to give them the CHANCE to read and correct before code is generated. **Wait for their go.**
(This is a second gate, distinct from 1a's model gate: 1a confirms WHAT to build; 1c confirms
the per-layer PLAN to build it.)

**Proportionality:** for a SIMPLE model — flat, no SharedBase / children / siblings, no
service — you may MERGE this gate into 1a: present the model AND a brief layer plan in the
same message and collect ONE approval (still write the task files before executing). Keep 1c
a separate gate whenever the model carries a variant delta or anything the dev hesitated on.

This front-loads the thinking into a plan, so execution runs in focused, isolated stages.

## Re-entry — `scaffold-entity/<entity>/` already exists

An existing working dir means a previous or interrupted run. Do NOT restart from scratch and
do NOT overwrite an approved plan. Check `spec.md` first: `Status: APPROVED` → resume from
the first **pending** task in `tasks.md` (the per-layer doc reads still apply);
`Status: DRAFT` or any `⚠️ OPEN` slot → resume Phase 1 at the spec gate. If the dev asks to
REDO one layer, re-open just that `task_<layer>.md`, regenerate that layer, and re-run the
final verify. Only a changed MODEL (new fields, a different variant) reopens the spec —
update it, re-approve, and say which tasks it invalidates.

## Phases 2…N — Execute each task (isolated, doc-forced)

For each task in `tasks.md` order (inside → out: domain → application → web → infra →
migrations → bootstrap → tests; deltas woven where they belong):
1. Open `task_<layer>.md`.
2. **READ the `/docs` sections it names** (MANDATORY — this is where you learn the current
   contract) + its `conventions/<layer>.md` + **`service-layout.html` when the pinned
   version ships it** (the layout/naming standard — normative for generators).
3. Generate the layer's files, understanding the contract from the docs (compose, don't
   copy). Flag any consumer-code misuse you notice (advisory).
   - **Precedence on conflict:** the task file governs WHAT to build (the approved model);
     the docs + conventions govern HOW (file layout, naming, granularity, mechanics). If a
     task file's mechanical detail contradicts a doc/convention rule, **the doc/convention
     wins** — apply it and record the deviation in `tasks.md` (a plan detail is a guess made
     before the layer's rules were read; it is never a license to violate them).
4. Verify the acceptance check — **including: layout and naming match `service-layout.html`
   (or the layer convention where the pinned docs predate it)**; run `go build` / `go vet`
   as you go.
5. Mark the task **done** in `tasks.md`; move to the next.

You never hold all layers at once — each stage loads only its task + docs + conventions.

## Final verify (the gate — non-negotiable)

**Level 0 — the reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`):
after the levels below pass, reopen `spec.md` and walk ITS promises item by item with
real command evidence; an unmet stated target is RED or an explicit dev-accepted
deviation — never a green summary.

Four DISTINCT levels — do not conflate them:
1. **Mechanical boot-trap checklist** — cheap checks; run ALL that apply, report each:
   - `grep -rn "CHAR(36)\|VARCHAR(36)\|VARCHAR2(36)" migrations/mysql/ migrations/sqlserver/ migrations/oracle/`
     → must hit NOTHING for the generated tables (every entity id/FK is `BINARY(16)`
     on mysql/sqlserver and `RAW(16)` on oracle; skip a directory the service
     doesn't have).
   - every generated `NNNN_*.up.sql` has its `NNNN_*.down.sql` twin.
   - `grep -rn 'path:"id"' internal/web/requests/` → nothing (boot panic).
   - `grep -rn 'json:"' internal/domain/` → must hit NOTHING (also sweep `db:`): a domain
     field carries `labelKey` and nothing else. A stray `json:` tag is the #1 reflex slip
     (a domain aggregate is not a wire DTO); a `json:"-"` also corrupts the `Old()`
     snapshot. Strip every hit — wire names live on the web-layer DTOs.
   - **VO smell (investigate, do NOT auto-fail):** `grep -rnE 'regexp|MatchString' internal/domain/*.go internal/domain/aggregatevos/`
     → a format/regex/length/range check inline in a root's or an AVO's `BuildRules` is
     usually a value object that wasn't extracted (its rule belongs in a `vos/` `IsValid`,
     tested there). For each hit, confirm it is either a signed-off `plain` exception in the
     spec's §2 `VO?` column or a genuine cross-field rule; otherwise lift it into a VO.
     (`internal/domain/vos/` is EXPECTED to hold regex — that path is not swept.)
   - **SQLite service only** — `grep -rnE '"[a-zA-Z_]+_(key|pkey)"|"PRIMARY"' internal/infra/`
     over the repo `Constraints` maps → must hit NOTHING: SQLite binds a unique/PK violation
     by the `<table>.<column>` column list, NEVER an index/constraint name
     (`${CLAUDE_PLUGIN_ROOT}/shared/dialects/sqlite.md`). A key ending `_key`/`_pkey` or the
     literal `PRIMARY` is the SQL-engine reflex leaking in — it silently misses, so the
     intended custom 409 becomes a raw 500. Rewrite each to `table.column` (dot).
   - if a read request declares `Fields *string`, every field of its Response AND of every
     NESTED response type is `*T`/slice + `,omitempty` (boot panic otherwise). Make it
     mechanical: `grep -rln 'Fields \*string' internal/web/requests/` → for each hit, open the
     paired list Response + its nested types and confirm EVERY field is `*T`/slice WITH
     `,omitempty` (a bare value type, or a tag missing `,omitempty`, IS the panic).
   - `Modes()` lists Archive ⟺ the schema declares its archive (deleted-at) column ⟺ the
     migration carries that column.
   - model has children (model B): in each `*_routes.go`, the ROOT-archive auto handler
     (name per `auto-handlers.html`) is instantiated AT MOST once per aggregate — its own
     archive route. A child-op route instantiating it is the whole-aggregate-archive trap
     (`aggregate-children.md`): it compiles and answers 200 while archiving the entire
     root. Child ops ride the partial-update handler, all three (add/update/archive).
   - every SCALAR `query:`-tagged field in `internal/web/requests/` is a pointer/slice
     UNLESS the spec explicitly declares that filter required (`grep -rn 'query:"'
     internal/web/requests/` → each value-typed hit must trace to a spec'd required
     filter; struct filter-groups are exempt): a value scalar renders REQUIRED in the
     OpenAPI spec (`openapi.html`, required-field rule) — one accidental value type turns
     an optional parameter mandatory and Swagger refuses the call without it.
   - any EXISTING view this run touched (e.g. a SharedBaseView gaining a role) had its
     `Version(N)` bumped.
   - every entity whose domain declares `RequiresService() … return true` must wire the
     Service end-to-end: `grep -rln "RequiresService() bool { return true }" internal/domain/`
     → for EACH hit, its feature constructs `New<Entity>Service(` and passes it to `Mount`,
     and EVERY write handler in its `*_routes.go` sets `Service:` (a nil there is a runtime
     `ServiceIsRequiredNotification` that `go build` cannot catch).
   - NO schema file is bundled — ONE schema per file (`service-layout.html`): under the
     schema dir, no `.go` file declares more than one schema (`grep -c` the schema-builder
     decl per file — the decl token per `table-schema.html`; ≥2 in one file is a layout
     violation `go build` won't flag).

   **Level 1 is a PRE-BOOT gate: run every applicable check to a clean pass BEFORE any step
   that BOOTS the service (level 4 QA regression; the separate functional e2e). Every item
   here is a boot panic detectable statically — hitting one AT boot means this gate was
   skipped. Never spend a boot to discover what a grep already catches.**
2. **`gofmt -l`, `go vet`, `go build`** (engine + transport tags) — FORMAT + VET + COMPILE.
   `gofmt -l` on the generated files must print NOTHING (unformatted = a Go-standards miss);
   `go vet` clean; then it compiles. Both are first-party Go tools (no install). Necessary,
   not sufficient.
3. **Unit tests ≥ 80% of the generated entity — WRITE them** (every `BuildRules` branch;
   the command mappers `ToEntity`/`ApplyTo`/`ApplyPartiallyTo`/`FromEntity`; the query
   `ToCriteria`). **Measure per generated FILE from the cover profile** —
   `go test -coverprofile` then read the entity's own files (`go tool cover -func`); the
   bare package percentage mixes in pre-existing code and misstates the entity. See
   `conventions/tests.md`. **A generated file under 80% is RED — add tests until it
   meets the target, or surface the miss as an explicit deviation for the dev to
   accept; the per-file `go tool cover -func` lines for every generated file MUST
   appear in the verify report** (a bare package number, or no lines, = this level did
   not run).
4. **Existing QA suite** — REGRESSION ONLY (proves no breakage; has NO cases for the new
   endpoints, so it does NOT prove the entity works).

Functional e2e of the new endpoints (create → wait for CDC → read back → CRUD round-trip →
204s → OpenAPI/GraphQL) is a **separate** step — its owner is `/omnicore:qa` (generates
and runs the contract suite); offer it when this run goes green. **Never edit a test to pass** — the test is
the oracle. **Leave `scaffold-entity/<entity>/` in place** (do not delete it) so the dev can
review the plan against the generated code.

**Offer to run (after a green verify).** Ask ONE question: boot the app so the dev can
click through the new endpoints? Yes → delegate to `/omnicore:run` (it owns bench
checks, background boot, readiness and the links — never boot inline here). No → done.

## Knowledge routing — flow step → `/docs` section

**Resolving a `<name>.html` reference (here AND in every `conventions/` file):** the actual
manual file is at

    <omnicore-dir>/docs/content/sections/<name>.html

where `<omnicore-dir>` = `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore` —
the version pinned in the consumer's `go.mod` (a `go.work` checkout points at the local
module). **Read that file for the contract; it is the authority — any text or consumer
code that disagrees with it has drifted.** Route first, then read ONLY the routed
section(s) for the step at hand — NEVER sweep the whole manual (slow, and it drowns the
step's contract in noise). Fuller index = the Documentation Map in
`<omnicore-dir>/CLAUDE.md` — the fallback for concepts this table doesn't list.

The `docs/` tree ships in the module (git-tracked, no nested `go.mod`), so `go get` puts it
in the module cache. **LAST-RESORT fallback — ONLY when a section is physically unreadable**
(e.g. `go mod vendor` stripped non-Go files and it's not in `go env GOMODCACHE`): degrade
gracefully — lean on `conventions/` + the consumer's entities and **TELL the user the
version-matched docs weren't reachable** (so they know generation ran un-verified). This is a
failure mode to report, NOT a shortcut — the presence of existing entities never triggers it;
only a genuinely missing docs file does.

| When generating… | Read section(s) |
|---|---|
| rules / notifications / Old / actionName | rules-dsl · old-state · status-mapping |
| value objects: raw vs enum, auto-validation, EnumByValue, Ignore/Validate | value-objects |
| domain service / scalar & grouped facts (rules needing existence, counts, totals, extremes, per-key breakdowns) | custom-command-handler · service-to-service |
| aggregate children / cascade | aggregate-persistence |
| insert/update/patch + in-TX hooks | auto-handlers · lifecycle-hooks |
| what one write touches end-to-end (SQL ↔ outbox ↔ Mongo op ↔ audit verb; PUT/PATCH share verb `update` — `actionName` tells them apart) | lifecycle-map |
| ctx-bound domain Service probe in rules | auto-handlers · custom-command-handler |
| schema (Go↔column) | table-schema |
| view: the ViewDefinition surface itself (declaration, `Version`, SharedBaseView contract, DeleteOnArchive, archived rule) | views |
| view: indexes / options / Version bump rules | auto-query-handlers · mongo-schema-evolution |
| view backing: relational (SoR) vs Mongo — posture, elicitation, gating | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) · relational-view for version-exact capability |
| SharedBaseView / ComposedView (read) | views · query-side |
| domain events (`RegisterEvent`, post-commit publish) | auto-handlers · command-handler |
| response projection (AutoFromDoc / FromDoc) | auto-query-handlers · custom-query-handler |
| REST routes / OpenAPI | openapi · reference |
| GraphQL surface | graphql |
| migrations (numbering, .down, dialect) | migrations · yaml-reference |
| authz (permission / owner-check / tenant) | authz-seams |
| bootstrap / feature / wire | bootstrap |
| file layout / naming / granularity (ANY layer) | service-layout |

## Boot-traps to respect AND verify (silent-wrong / panic / runtime-500)

- **Id typing is VERSION-DEPENDENT — detect the pin's contract in `table-schema.html`
  ("Supported column shapes") before generating any field or DDL.** On a typed-identity
  pin an id-holding field (child PK `ID`, every cross-aggregate reference like a
  `CourseID`/`BuyerID`) is declared **`domain.ID`** (nullable ⇒ `*domain.ID`) and the Go
  type alone drives the dialect's native id column across write, criteria and scan (`nil`
  ⇄ SQL NULL); a **`string` field is text, ALWAYS** (nothing is guessed from a value's
  shape). Both are first-class — but pair the field type with the DDL column or the FIRST
  INSERT fails (runtime 500 the build won't catch). The persistable field-type set is
  CLOSED — an unknown Go type (incl. `uuid.UUID` and named enums) is a BOOT FAIL at
  `Field(...)` with the fix in the message. Wire DTOs/requests stay `string` and convert at
  the mappers (`domain.NewID(s)` / `.Value()`). On an older pin (≤ v0.29.0) ids are plain
  `string` and `domain.ID` is never a field type — that divergence, and the exact native id
  column per engine, live in the dialect sheet.
  - **→ The per-engine specifics — id/decimal/boolean column types, the constraint-violation
    KEY the `ConstraintBinding` map binds, active-only uniqueness, the read-side posture —
    are in `${CLAUDE_PLUGIN_ROOT}/shared/dialects/<dialect>.md`. Read ONLY the sheet(s) for
    the service's target dialect(s) before generating any field, DDL or `Constraints`
    binding.** The framework's OWN control-plane tables use a different write path — never
    mirror them for entity tables (`conventions/migrations.md`).
- **Service migrations start at `0001`** (the service's own sequence; the framework's
  `0001_framework` is in a separate tracking table — no collision). Not `0002`.
- **`path:"id"` on a by-id request → boot panic** (the `*Spec`/HasPathID owns `:id`; never
  declare it — extra path segments use `path:"..."`).
- **`?fields=` opt-in → every Response field must be `*T`/slice + `,omitempty`** or panic.
- **View `Version(N)`** — bump on rebuild-relevant change (root/embeds/DeleteOnArchive/
  jsonSchema/collation/capped/time-series); index-only does NOT; forgetting panics.
- **A relational view (`.RelationalSource`) has NO Mongo collection** — SyncEngine,
  Mongo-spec and reconcile skip it by name; its reads are read-your-writes (provable
  immediately, no CDC round-trip). What it serves vs rejects (typed 400, never 500):
  `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`; version-exact contract: `relational-view`.
- **Every `.up.sql` needs a `.down.sql`** (may be a no-op) or boot aborts.
- **A ComposedView 1:N leg's FK must be indexed** or boot is fatal.
- **`Modes()` ⟺ the schema's archive-column declaration** must agree.
- **A child-mutation method that EMITS a notification before delegating opens with
  `domain.EnsureInitialized(root)`** — else that notification is silently dropped
  (`AddNotification` is a no-op while the context is nil). **A method that only delegates
  does NOT need it**: `Add/Change/RemoveAggregateChild` already call `ensureRootInit`
  themselves — emitting it there is noise, not safety.
- **`ApplyTo` on a SharedBase upsert may run twice** → pure/idempotent.
- **`domain.Old(e)` is nil on Insert** → guard.
- **Authorization is not surface-specific** — the same handler + `RequirePermission` at each
  surface's registration unit (route · field · procedure); decide once per operation.

## What this skill never does

No framework edits, no git, no test edited to pass, nothing generated outside the
approved spec, no silent replication of a consumer's framework misuse (flag it,
advisorily), no modeling decision guessed instead of proposed-and-confirmed. Existing
entities are `evolve-entity`/`remove-entity`'s turf; cross-entity read models are
`scaffold-view`'s; projects without a service are `scaffold-service`'s.

## conventions/ index

| File | Covers | Load when |
|---|---|---|
| `spec-template.md` | the Phase 1 `spec.md` skeleton — required slots, OPEN/proposed marks | always — Phase 1 |
| `domain.md` | aggregate, rules, notifications+labels, modes | always |
| `application.md` | commands+results, queries, dtos, actionName map | always |
| `web.md` | requests/responses, routes, authz, openapi, export | always |
| `infra.md` | schema, repo + constraints, view + indexes | always |
| `migrations.md` | numbering, dialects, up/down | always |
| `bootstrap.md` | feature + wire registration | always |
| `tests.md` | unit tests ≥80% — what to cover per layer | always |
| `sharedbase.md` | base/role split, upsert insert, SharedBaseView | only if SharedBase |
| `aggregate-children.md` | children, child dtos, cascade, editing (A/B/C) | only if children |
| `siblings.md` | 1:1 nullable satellite (schema `.Sibling`, conditional materialization) | only if a sibling |

All carry the **flat/base** case except the last three (deltas). **The conventions carry
no code** — process, decisions and traps only; layout/naming lives in
`service-layout.html`, every code example and API contract in the routed `/docs`.
