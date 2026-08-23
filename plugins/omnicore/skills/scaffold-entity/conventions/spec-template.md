# conventions/spec-template.md — the Phase 1 spec artifact (load in Phase 1, every run)

> Copy the template below VERBATIM to `specs/scaffold-entity/<entity>/spec.md` and FILL every
> slot. **Completeness is STRUCTURAL, not memory:** no section may be deleted or left
> blank — a section that does not apply stays in place marked `N/A — <why>` (proof it was
> considered, not forgotten). A slot you cannot responsibly recommend becomes
> `⚠️ OPEN: <the question for the dev>`. **Generation MUST NOT start while the spec has any
> ⚠️ OPEN slot or `Status: DRAFT`.** The reasoning for each section (trade-offs, what to
> confirm, tone) is SKILL.md Phase 1; the framework mechanics are the routed /docs.

Marking convention inside the filled spec:
- low-risk slots → just the value (you decided; the dev can still edit).
- high-risk slots → the value + `(proposed)` + the alternative(s) in one line.
- unanswerable → `⚠️ OPEN: <question>` (must be resolved by the dev, never defaulted).

---

```markdown
# Spec: <Entity>

- **Status:** DRAFT <!-- flip to APPROVED only after the dev's explicit ok -->
- **Approved:** `<pending>` <!-- who/when -->
- **Language:** <x> <!-- working language for human-facing text (descriptions, COMMENTs, examples) — detected from the user's own words (invocation args count, even one word); edit to override -->
- **Generation:** `<pending>` <!-- omnicore-gen | manual — the dev's answer at gate 1d; written the moment they answer, so a resumed run does not ask again -->

## 1. Storage model                                    [high-risk — confirm]
- Kind: flat | sharedbase-role — <pick> (proposed; alternative: <other> — <one-line why>)
  **IDENTITY-SMELL RULE: if the fields carry person/party identity (name +
  document/tax-id and/or email/birth date) PLUS role-specific fields, this slot is
  ⚠️ OPEN — ask "could this also become another role for the same person one day?". It
  cannot be self-answered from the request (the second role never exists yet when the
  first is modeled) and it is NOT covered by a blanket "ok". No smell → propose flat.**
- ER sketch: every table, PK/FK, which table each field lives on, **plus a one-line
  description per table** (→ becomes the table COMMENT in the migration). **Child/sibling
  table names are OWNER-PREFIXED** (e.g. `person_addresses`,
  `student_grades`, `student_scholarships` — never bare `addresses`/`grades`); name them
  right HERE so the sketch and the migrations agree (`migrations.md`).
- If sharedbase-role (else `N/A — flat`):
  - Base: <table> — new | REUSE existing (from Phase 0b)
  - **Natural key: <field>** ⚠️ HIGHEST-RISK SLOT of the whole spec — derives the identity
    PK (UUIDv5) and IS the dedup key; wrong = identities wrongly merged/split. Confirm
    explicitly, never infer.
  - Link model: shared-PK | separate-FK
  - Orphan policy (what happens to the base when its LAST role is deleted):
    KeepOrphan (framework default) | DeleteWhenUnreferenced — a declared schema choice
    (`.OrphanPolicy(p)`, `table-schema.html`), never left implicit; the canonical
    example picks the non-default.
  - Identity view (SharedBaseView): create | add-role (bump Version) | skip
    — only where the project HAS a `mongo:` block (a full engine with relational-backed
    views still does). On SQLite it is STILL a choice, stated honestly: declarable, but
    it requires adding `mongo:` (the collections must exist to boot) and it serves EMPTY
    until the posture flips to a CDC-capable engine (`/omnicore:configure`) — meanwhile
    the per-role view serves the base's fields flattened. Default on zero-infra = skip;
    record the decision, never silently `n/a` it.

## 2. Fields                                 [one row per field — none may be missing]
| Field | Go type | VO? (reuse/new-raw/new-enum/new-composite/plain) | Nullable | Unique | Lives on (root/base/role/child/sibling) | example: | Description |
|---|---|---|---|---|---|---|---|
- Nullable ⇒ pointer. Money = int64 minor units, never float. Exact decimals → `string`
  (float64 rounds); `float64` is fine for non-money numerics. Column types per dialect:
  the "Go ↔ …" tables in `table-schema.html` — the authority, never from memory.
  `example:` always filled (low-risk).
- **`Lives on = root` for an OPTIONAL field is a DECISION, not a default** — it must have
  been shown at the model gate (SKILL.md Phase 1 item 2) with its alternative (a sibling).
  A spec where no optional field ever had that trade-off surfaced is incomplete.
- **The `VO?` column is MANDATORY per field (`value-objects.html`) — a blank cell = an
  incomplete spec.** Classify each field against the Phase 0b `vos/` inventory: `plain` (only
  a presence rule, or none — NEVER a field whose values are a fixed set: that is ALWAYS an
  enum VO), `reuse vos.X` (an existing VO whose rule fits — REUSE it, never a
  second copy), `new-raw vos.X` (a bespoke format/length/range → a new raw VO),
  `new-enum vos.X` (a closed set/status/kind → a new enum VO) or
  `new-composite vos.X` / `reuse vos.X` for a value that spans SEVERAL columns. A 1:N child is an AGGREGATE
  value object → §3 (`internal/domain/aggregatevos/`). This is what lets the dev see, edit and
  APPROVE the VO/reuse decision before any code is generated.
- **A COMPOSITE occupies ONE row of §2 and several columns.** When two or three fields only
  mean something together — an amount and its currency, a start and an end, a street/city/zip
  — write ONE row for the concept, name the VO, and list its parts with their columns and
  the name each is EXPOSED under (that exposed name is what the filters in §9, the wire and
  the exports all use). Write the parts as a nested list under the row rather than as
  separate §2 rows: separate rows are exactly the flattening this kind exists to replace, and
  a reviewer cannot tell from them that the fields are one value. Say which parts are
  nullable, and whether the composite AS A WHOLE is optional — they are different questions
  and both reach the DDL.
- Id/uuid fields (the PK, every FK, any cross-aggregate reference): the Go type follows
  the PIN's identity contract (SKILL.md boot-traps, "Id typing") — on a typed-identity
  pin write **`domain.ID`** (nullable ⇒ `*domain.ID`); on older pins (≤ v0.29.0) write
  **`string (uuid)`**, never `domain.ID`.
- `Description` = one concise line on what the field means (low-risk, always filled). It
  becomes the column COMMENT in the migration DDL (`migrations.md`); reuse it for the
  OpenAPI field doc where the surface wants one.
- **Unique is high-risk — confirm per field AND surface the enforcement style:**
  - **(recommended) domain pre-check + DB backstop**: a `domain.Service` check in
    `BuildRules` (with exclude-self semantics on update; unarchive included when
    reactivation can collide) so the violation reports TOGETHER with the other validation
    errors, plus the UNIQUE index + repo `Constraints` binding as the race-window backstop
    — the framework's own defense-in-depth guidance (the check itself is the loader's
    hydration-free `Exists` probe when the pinned version ships it — `infra.md` point 0);
  - **(alternative) constraint-only**: simpler, but the duplicate surfaces alone as a 409
    only after every other error is fixed.
  Either way the notification is a custom `<Field>AlreadyExists…` (409, all 7 catalogs);
  `EntityAlreadyAddedNotification` is the PK-collision one, not this.

## 3. Children (1:N)                        [or `N/A — no collections`]
| Child | Of whom (base / role / flat-root) | Edit strategy (A / B / C) | Restorable alone? |
|---|---|---|---|
- Restorable-alone = yes ⇒ strategy C (own aggregate). Base-child + per-child endpoints ⇒
  the routing sub-question (under the role vs own aggregate).

## 4. Siblings (1:1)                        [or `N/A — no facet worth splitting`]
Per sibling: <field group> → attachment node (flat root | role | role-child).
- On a SharedBase: base-level (identity-wide) facet = nullable columns ON the base, NOT a sibling.

## 5. Modes                                             [required]
display + <subset of insert / update / delete / archive / unarchive> — <why this subset>

## 6. Delete semantics                                  [required]
soft | hard (rarely both). Verb truth: DELETE = hard purge only; soft = PATCH
archive (+ unarchive). Root AND per-child.

## 7. Business rules                        [required — never boilerplate-only]
| # | Field(s) | Rule | Verb scope (IfInsert/IfUpdate/IfArchive/IfUnarchive/…) | Notification | HTTP |
|---|---|---|---|---|---|
- At minimum the required/format/range rules per field; elicit the REAL invariants
  (cross-field, immutability, transitions, per-group caps) — this is where the domain
  earns its keep.

## 8. Update shape                                      [required]
PATCH | PUT | both — default PATCH. **INVARIANT, not a trade-off: if §4 has a sibling, the
shape MUST include PUT.** PATCH cannot assign null, and the ROOT's PUT with the facet
all-null is the ONLY thing that clears the sibling row — a sibling never gets its own REST
endpoint (`siblings.md`). PATCH-only beside a facet is therefore not a deviation this spec
may record as a conscious choice: it ships a facet a caller can grant and never revoke. The
codegen path refuses it outright (`update.shape` is a hard blocker in `omnicore-gen check`,
not a warning), and the manual path has no override either — if the dev wants PATCH-only,
the answer is to drop the sibling and keep those fields nullable on the root, not to keep
both. **With GraphQL: yes there is
one exception** — omitted and null are indistinguishable there, so clearing needs a
bodyless intent mutation dispatched through the FULL update handler (`siblings.md`; the
generator emits it).

## 9. Surfaces & reads                       [required — never an endpoint unasked]
- REST: yes/no · GraphQL: yes/no · gRPC: via `/omnicore:implement` (after this run) ·
  Exports (CSV/XLSX): yes/no · Integration events (this entity publishes facts /
  reacts to another service's): via `/omnicore:implement` (after this run — note the
  ask here so it isn't lost)
- Reads: by-id + by-params (expected defaults).
- Reserved read controls SERVED by the listing (the Request DTO governs — declared =
  served, undeclared = typed 400): pagination/`orderBy` expected defaults; `?fields=` /
  `?search=` / `?onlyTotal` / `?includeArchived` each yes/no (low-risk — filled, shown;
  `?search=` only where a text index will serve it).
- Computed read fields: any value the reads should return that NO column holds (a display
  label, a total, a status derived from two dates)? Name it, its type, and the stored
  fields it is derived from. It is filled once per document in `FromQueryResult`, so every
  surface agrees; `?fields=` on it fetches the sources, `?orderBy=` on it is a 400, and a
  filter over it is impossible — say so when offering it. Ask: it is cheap here and a
  hand-built layer later.
- Field-level read authz: any field only SOME callers may see? (`ReadCriteria.Restrict`
  — passive omission vs active 403, prunes `?fields=`/GraphQL selection/exports alike;
  `authz-seams.html`.) Ask — invisible at modeling time, loved when offered.
- View backing: relational (SoR, read-your-writes — its own declaration type: no version,
  no collection, no rebuild) | Mongo (canonical) — default = project posture; on SQLite the
  plain per-entity view is FORCED relational (no CDC source ⇒ a Mongo projection never
  materializes; `shared/read-side.md`). Shapes a relational read model cannot serve
  (SharedBaseView) follow the §1 identity-view rule.
- Read joins: does a RULE, a service fact or the listing need a field belonging to ANOTHER
  aggregate this entity already holds a foreign key to? (pin ≥ v0.57.0 — declared on the
  repository, `shared/read-joins.md`.) Two answers per traversal, and they are separate
  questions: WHICH columns it brings across, and whether the caller RECEIVES them or the
  field exists for the rules alone. Ask — a dev who does not know it exists will otherwise
  ask for the column to be copied into this table.
- Filter/sort/search operators per field (low-risk — filled, shown):

## 10. Authorization                          [required — BOTH slots, a blank is invalid]
- Permission gate (Layer 1): <resource:action per operation>
- Data-access (Layer 2/3): <who can read/modify which rows — "anyone with the permission"
  is a valid answer; a blank is not>
```
