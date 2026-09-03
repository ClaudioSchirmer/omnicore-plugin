---
name: qa
description: >-
  omnicore: generate and run a CONTRACT QA SUITE for an omnicore-based service — read the
  project's entities, views, surfaces and infra posture, derive the framework's promised
  behaviors from the pinned docs (verb set per mode, status codes, archive semantics,
  filter vocabulary, typed 400s), and produce an executable e2e suite (qa/*.sh + a runner,
  at the project root) that PROVES them against the running service — ALWAYS including a
  security lane that proves what the service REFUSES: 401 per token rule (missing,
  malformed, foreign signature, wrong iss/aud, alg outside the allowlist, expired),
  the public-route split in both directions, and 403 per authorization layer (missing
  permission, identity-derived rules, tenant scoping). Use when the dev wants e2e tests,
  contract tests, a QA suite, smoke tests, security/auth tests, or to "prove the service
  works". Only for projects that import github.com/ClaudioSchirmer/omnicore.
---

# qa

Close the loop the other skills open: scaffold builds it, run boots it, **qa proves
it**. This skill reads what the service declares (entities, modes, views, backings,
surfaces), derives what the PINNED framework therefore promises, and generates an
executable contract suite that exercises those promises against the real running
service — then runs it and reports GREEN/RED honestly.

**Two destinations, one rule.** The PLAN — the decision record — lands under
`specs/qa/<suite>/plan.md`, one directory per suite named after what that run PROVES,
like every other skill's document. The EXECUTABLE SUITE — `run.sh` and the
per-entity scripts — lands in **`qa/` at the PROJECT ROOT**: it is not a document to
read, it is a command a dev and a CI job RUN, and it belongs where they look for it.
Both are part of the project and the project keeps them — **never add either to
`.gitignore`** (`${CLAUDE_PLUGIN_ROOT}/shared/generated-documents.md`).

**One plan directory per round of decisions, still exactly ONE runner.** `<suite>` scopes
the PLAN, never the suite: a later round writes `specs/qa/<its-own-suite>/plan.md` and
EXTENDS the same `qa/run.sh`. A second plan directory is never a licence for a second
entry point.

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

- **The contract comes from the PIN's docs, never from this skill.** Which verbs an
  entity serves, which status code each failure maps to, what `?includeArchived`
  reveals, which reads a relational view rejects — all of it is derived per entity from
  the docs at the project's pinned omnicore version (`go list -m -f '{{.Dir}}'
  github.com/ClaudioSchirmer/omnicore` → `/docs/content/sections/<name>.html`). This
  file names the derivation, never the answers. Never stamp a framework version here.
- **Prove behaviors, not implementations.** A case asserts what the wire promises
  (code + envelope + effect visible on a read-back), never internals. The generated
  suite must fail if the service regresses and must NOT fail when internals are
  refactored behind the same contract.
- **Posture-aware expectations** (`${CLAUDE_PLUGIN_ROOT}/shared/read-side.md`, the
  owner): on a Mongo-projected backing, a write→read-back case POLLS the view for the
  NEWEST write (CDC lag is legitimate; a bounded poll, never a blind sleep); on a
  relational backing, read-your-writes is the promise — the read-back
  is IMMEDIATE and a needed poll is itself a failure. On SQLite the plain views are
  relational (read-your-writes), but a Mongo-backed shape the service kept
  (SharedBaseView) NEVER materializes there — no CDC source — so its honest assertion
  is "declared, boots, serves empty", never a read-back. The suite encodes the right
  expectation per view, not the loosest one.
- **A capability boundary is a PROMISE, so assert it like one.** On a relational read
  model the unsupported requests — `?search=`, filter/sort on a 1:N child field — are a
  typed **400** carrying `UnsupportedCapabilityNotification` (pin ≥ v0.57.0; every read
  engine raises the same one), and an unresolvable `?fields=` path or a bad cursor is a
  400 carrying `SchemaViolationNotification` — byte-for-byte what the Mongo reader
  answers for the same token. Assert the notification KEY and the status, never prose:
  a 500 there, or a silent `200 {}`, is exactly the regression these cases exist to
  catch. Where a joined field IS filterable, assert that too — that reach is the part
  that distinguishes this backing from the pre-v0.57 one.
- **A SECURITY boundary is a promise too — the suite always has one, and it never
  passes by accident.** Every other family here proves the service does what it should;
  this one proves it REFUSES what it should. 401 and 403 are the two answers a caller
  must be able to rely on, and they are the ones a regression turns into a 200 silently:
  a `publicRoutes` entry widened past what it meant to open, a `RequirePermission` lost
  when a route was re-mounted, a tenant filter dropped from `ToCriteria`. None of those
  breaks a single happy-path case. **Section 3 of the plan is therefore never `N/A` and
  never deferred** — what varies is how far it reaches, and the plan says so out loud.
- **Framework maintainer rules NEVER bind this skill** — the omnicore module ships its
  own `CLAUDE.md`; ignore it beyond the Documentation Map index. Only the host
  project's rules apply.
- **User language for talk, English for artifacts** — converse in the dev's language;
  generated scripts, case names and comments in English like the rest of the codebase.
- **This skill writes ONLY under `specs/qa/<suite>/` (the plan) and `qa/` (the runnable
  suite)** — never application code, never yaml, never migrations. A case that can't pass because the SERVICE is wrong is a finding to
  report (route `doctor` / the owning skill), never something to "fix" by weakening
  the case. **Never edit a case to pass — the suite is the oracle.**

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

- **omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` resolves —
  else STOP (this is not a generic test generator). At least one entity wired — else
  there is nothing to prove; say so and point at `scaffold-entity`.
- **No version check here** (like `remove-entity`): the suite proves the CURRENT pin's
  contract; upgrading first is `/omnicore:upgrade`, separately.

## Phase 0b — Discover (read, don't ask)

Map what the service declares — this inventory IS the test surface:
- **Entities**: schemas (fields, VOs — including any COMPOSITE value object and the
  names its parts are EXPOSED under, which are the only names any surface speaks; the
  composite's own field name appears NOWHERE on the wire), uniqueness incl. active-only, children,
  siblings, SharedBase roles), `Modes()` per aggregate (the verb set each entity
  actually serves — an absent verb still gets a case, never a skip, but ASSERT THE
  RIGHT CODE, three distinct shapes per `status-mapping`: mode missing from `Modes()`
  with the route mounted → the mode's `…NotAllowedNotification`, **403**; no route
  matches the path at all → **404**; the same path registered under another method
  only → **405**), constraint bindings (which duplicate → which 409 key).
  **Cross-check the derived verb inventory against `GET /openapi.json`** — the
  framework auto-registers it whenever `Wiring.OpenAPI` is set and it enumerates the
  routes actually wired (probes included): the cheapest, most reliable oracle for
  "which verbs does this entity really serve"; a source-vs-openapi disagreement is a
  finding, not a guess to resolve silently.
- **Views**: per view its backing (declared as a relational read model, or Mongo) → the read-back
  expectation per the posture rule above; archive regime (kept-but-hidden vs
  `DeleteOnArchive`); filter/sort/search vocabulary per field; which RESERVED controls
  the list Request DTO declares (`query:"…"` — the DTO opt-in gate: declared = served,
  undeclared = typed 400 on presence; `?fields=`/`?onlyTotal`/`?search` included).
- **Surfaces**: REST always; GraphQL/gRPC/exports only when wired — a case per
  enabled surface (handler invariance: the SAME operation answers identically), none
  for absent ones.
- **Infra posture** (`shared/read-side.md` + `shared/capabilities.md`): Mongo+CDC
  present, or relational-only/SQLite.
- **Security posture — read the WHOLE `auth:` block, per profile, not just the mode.**
  This inventory is what section 3 turns into cases, and every item of it is a boundary
  something can silently widen:
  - `auth.mode` per profile (`disabled` | `jwt`) — and which profile the suite will run
    under, which is a decision, not a given (see §3).
  - `auth.publicRoutes` — the EXACT `METHOD /path` entries. Exactness is the trap worth
    knowing before writing a case: matching is not prefix-based, so a sibling path that
    "looks covered" is not. Note separately the routes the FRAMEWORK appends at boot
    (the OpenAPI page + `/openapi.json`, the GraphQL playground, the JWKS document, a
    root redirect when enabled) — public by rule, needing no entry — and that `/livez`
    and `/readyz` are NOT among them: probes are public only if the project opted them
    in, and which way it went is itself assertable.
  - `auth.jwt` — `issuer`, `audience`, the `algorithms` allowlist, `leewaySeconds`, and
    whether keys come from `jwksUrl` or `publicKeyPem`. These four are what the 401 cases
    are built out of.
  - `auth.authorization.enabled` (the master switch — OFF means the gate no-ops and a
    `RequirePermission` route answers 200 to any authenticated caller, which is a posture
    to assert, not a bug to report) · `authorization.permissionsClaim` ·
    `authorization.tenant.required`.
  - `auth.issuer` — does the service MINT its own tokens? That answers §3's hardest
    question (where a VALID token comes from) without anyone inventing one.
  - `auth.externalValidator` and its `failMode` — when set, a locally-valid token can
    still be refused, and the suite must not read that as a defect.
  - **Which routes declare a permission** — take it from `GET /openapi.json`
    (`RequiredPermission` / the description suffix) cross-checked against the `Mount`
    calls, the same source-vs-openapi rule as the verb inventory; a disagreement is a
    finding, not a guess.
  - **Which entities carry identity-derived rules** — a `BuildRules` clause reading a
    principal field (owner-check, "unless admin"), a `ToCriteria` that injects a tenant
    filter or calls `Restrict`. Each one is a 403 (or an invisible-row/absent-column
    assertion) that no route declaration reveals.
- **Read joins**: which repositories declare one, and per joined field whether it is
  SERVED or exists for the rules alone (`shared/read-joins.md`). Both halves are
  assertable and both are worth asserting — a served field must come back with the
  counterpart's value, and a rules-only one must appear in NO response body and in no
  export. The second is the case nobody writes and the one that catches a leak.
- **Existing `qa/`**: if the project already has a suite, mirror its conventions and
  EXTEND it — never generate a parallel second style. An older run that put the scripts
  under `specs/qa/` is the SAME suite in the wrong place: MOVE it to `qa/` (fixing the
  root resolution as you go — see the runner contract) rather than leaving two homes.

## Phase 1 — Plan gate: `specs/qa/<suite>/plan.md`

**Name the suite first.** `<suite>` is a kebab slug of what THIS round proves, proposed by
this skill and confirmed by the dev in the same breath as the plan: `full-contract` for a
whole-service round, the scope itself when the dev asked for one (`person-orders`,
`auth-enabled`). Not a date, not `plan`, not the service's name — the reader of
`specs/qa/` must be able to tell two rounds apart by their directory names alone.

`Status: DRAFT`, hard STOP until approved. Sections (structural completeness — `N/A —
<why>`, never deleted):

1. **Coverage matrix** — per entity × surface: the case families derived from Phase 0b
   and the pin's docs — happy path per served verb · validation 422 (a representative
   VO/rule failure per shape, asserting the notification KEY, not prose) · the dual
   409 (duplicate vs wrong-state — the duplicate flavor only where the entity declares
   it; the wrong-state one is also raised by the FRAMEWORK on a write the pin's
   `lifecycle-map` guards, so derive it from there, not from the spec) · archive
   round-trip (`archive → hidden → ?includeArchived reveals → unarchive → visible`;
   with `DeleteOnArchive`, absence instead; on an aggregate whose child declares an
   archive column, the same round-trip proves the unarchive is STAMP-SCOPED — a child
   removed on its own BEFORE the root's archive stays archived after the root comes
   back, and the payload/audit report no transition for it) · read vocabulary (one filter/`?orderBy=`
   per declared operator family, `?fields=` when opted in, `?search=` where a text index
   serves it, `?onlyTotal=true` only-total, `?last=` alone serving the TAIL window, and
   the PAGINATION ENVELOPE as a contract: `pagination.totalCount`/`hasNextPage`/
   `hasPreviousPage`/`startCursor`/`endCursor` truthfulness (cursors are WINDOW EDGES —
   walk by echoing them into `?after=`/`?before=`), page-2 disjointness, cursor
   advance) · **golden-record round-trip** (one record exercising EVERY declared
   field — VO fields included, and a COMPOSITE value object as its EXPOSED PARTS, one
   wire field each, never the composite's own name — written then read back field-by-field on each enabled
   surface: the family that catches a field silently dropped from a DTO/projection,
   which every other family passes over) · rejected reads — the WHOLE typed-400 guard
   family, not one: the relational 1:N pushdown · unknown field · operator outside a
   field's allowlist · a RESERVED control the Request DTO does not declare (the DTO
   opt-in gate — PRESENCE trips it, so an undeclared `?onlyTotal=false` rejects too;
   surface idiom differs BY DESIGN: REST 400, GraphQL unknown argument — and `fields`/
   `onlyTotal` are selection-natural there, never gated — gRPC INVALID_ARGUMENT; the
   pin's `graphql`/`grpc` sections own the per-surface rendering, don't assert the
   REST envelope cross-surface) ·
   a filter VALUE outside the leaf's declared kind (`?age=abc` on an `int64`, `?someId=lixo`
   on an identity column) → 400 `InvalidFilterValueNotification`, **pin ≥ v0.70.0** on every
   engine; below it the same request was a 500 on a relational backing and an empty `200`
   page on Mongo, which is exactly why an older suite has no case for it ·
   `?first=`/`?last=` above the view's ceiling · mixed directions (`first`+`last`,
   `first`+`before`, `after`+`before` — backward is `last`+`before`) · `?onlyTotal=true`
   beside a page-shaping control (the only-total conflict matrix; filters/`?search=`/
   `?includeArchived` stay valid — counting a filtered subset is the point) · malformed
   `?after=`/`?before=` · cursor↔`orderBy` mismatch ·
   cursor↔`includeArchived` mismatch · `?search=` on a relational view · segment
   `?orderBy=` on a composed leg (derive the exact set from `status-mapping`'s
   SemanticSchema rows + `auto-query-handlers` at the pin — the enumeration here is
   the FAMILIES, the pin owns the members) · absent verbs
   → the 403/404/405 split above · not-found → 404 · **a by-id ADDRESS that is not a uuid,
   split by VERB and not by surface** (pin ≥ v0.70.0): a read answers 404
   `UnknownIDAddressNotification`, a write 400 `MalformedIDNotification`, identically on
   REST, GraphQL and gRPC, and the framework's own audit endpoint follows the same rule.
   It is the family an existing suite is most likely to miss, because before that pin a
   relational backing answered 500 and a Mongo-backed one 404 for the same request — so
   assert BOTH verbs, not just the read. Route: `auto-handlers` +
   `status-mapping` + `auto-query-handlers` at the pin for the exact contracts.
   · **and the SECURITY families — 401, the public-route split, and 403 per authz layer
   — which section 3 owns in full and which are never absent from this matrix.** They
   belong per entity × surface like everything else here: a route gated on REST is not
   thereby gated on GraphQL or gRPC, and "the other surface forgot the gate" is a real
   regression that only a per-surface row can catch.
2. **Data hygiene** [high-risk — ⚠️ OPEN, the dev decides]: where the suite's records
   live. Options, stated honestly: run against the DEV profile bench with
   uniquely-suffixed records the suite archives at the end (residue: archived rows) ·
   a dedicated throwaway database selected by a SUITE-OWNED config file (generated
   under `qa/` beside the runner, picked via `OMNICORE_CONFIG_PATH` — that is how a "dedicated profile"
   respects this skill's never-touch-the-project-yaml rule) · SQLite `:memory:`
   (only if the service already runs so). Never silently write into a database the
   dev cares about. **And state HOW state resets between runs/cases** — exact-count
   assertions demand it: on a Mongo-projected backing, wiping the relational rows
   does NOT clear the projection (a separate store — clear the view collections too),
   and a wipe racing in-flight CDC re-materializes documents, so reset = relational
   delete + view clear + a short DRAIN before seeding (the canonical suites treat the
   clean baseline as a precondition, not a hope).
3. **Security — ALWAYS present, never `N/A`.** The suite proves what the service
   REFUSES, not only what it serves. This section states three things: which of the
   halves below this round covers, where a valid token comes from, and — plainly — what
   stays unproven. Deferring the whole section is not one of the options; deferring the
   403 half for a named reason is.

   **3a. The 401 half — build it in every auth-enabled service; it needs no valid
   token.** This is the half nobody has an excuse to skip: every case below is
   constructed from a token that is meant to FAIL, and forging an invalid token requires
   no secret from anybody. Derive the exact keys from `auth-middleware` at the pin — the
   families are: no `Authorization` header · a header that is not `Bearer <token>`
   (wrong scheme, empty value; the scheme match is case-insensitive, so `bearer` is a
   PASS case, not a reject) · a token that is not a JWT at all · a well-formed JWT signed
   with a foreign key · wrong `iss` · `aud` missing the configured audience · an `alg`
   outside the allowlist (the algorithm-confusion guard — an HS256-signed token where the
   allowlist is asymmetric is the classic attack, and asserting it is the point) · an
   `exp` in the past beyond `leewaySeconds`. **Assert the notification KEY, not just the
   status**: the pin splits expired from invalid precisely so a client can branch on
   refresh-vs-reauthenticate, and a suite that only checks `401` cannot see that split
   collapse. The two "not a token" branches and the "bad token" branches carry different
   keys — a case that accepts either is a case that proves neither.

   **3b. The public-route half — assert BOTH directions.** One direction only ever
   catches half the regression: that every declared public route answers tokenless, AND
   that a route which is NOT declared answers 401 tokenless. The second is the one that
   catches a `publicRoutes` entry widened past its intent, and it is cheap — one
   protected route, no token, one assertion. Because matching is exact `METHOD /path`,
   add the neighbours that make exactness visible: the same path under another method,
   and a sibling path sharing a prefix with a public entry. Cover the framework's own
   appended surfaces as the posture the pin promises (docs page, `/openapi.json`, JWKS,
   playground — reachable tokenless), and assert the probes the way the project actually
   configured them, not the way they are usually configured. **Where GraphQL is wired
   with introspection on**, the introspection-only bypass is a security boundary with an
   exact documented rule and belongs here: an introspection-only document passes
   tokenless, while a data field beside `__schema`, a decoy introspection operation next
   to a real one, a root fragment spread, or a mutation does NOT — `auth-middleware` at
   the pin owns the rule; assert its edges, not just the happy bypass.

   **3c. The 403 half — needs a VALID token, so name its source. Never invent one.**
   Three sources, in this order of preference; the plan says which one this project has:
   - **The service issues its own** (`auth.issuer` in the yaml, `token-issuance` at the
     pin) → the suite obtains tokens through the service's own documented login/refresh
     flow. Nothing is invented and nothing external is needed; this is the best case.
   - **The suite owns the keypair.** Where the service validates via `publicKeyPem`, the
     suite can generate its own keypair, publish the public half through the SUITE-OWNED
     config file §2 already establishes (`OMNICORE_CONFIG_PATH`, never the project's
     yaml), and sign its own tokens with the private half — including tokens carrying the
     exact `permissions` / tenant claims a case needs. This invents no credential of the
     dev's: the suite is the issuer, and it says so in the plan. It is also how a
     project whose dev profile ships `auth.mode: disabled` gets a real security lane
     without its configuration being touched.
   - **The dev's IdP** → they supply the tokens or the mint command (an env var, a script
     the bench already has). Asked once, never guessed, never hardcoded into a committed
     script.

   With a valid token in hand, the families come from `authz-seams` at the pin, one per
   layer — they fail differently and a suite that tests only the first misses the other
   two: **layer 1**, an authenticated principal WITHOUT the permission a route declares
   → 403 with the missing-permission key, and its complement, the same call WITH the
   permission → 2xx (a gate that refuses everyone is also broken); **layer 2**, a rule
   in `BuildRules` that reads the principal (the owner-check, the "unless admin") → 403
   carrying that verb's own notification, proven by TWO calls that differ only in who is
   asking; **layer 3**, tenant scoping — a token with no tenant claim where
   `tenant.required` is on → 403, a cross-tenant READ returning NOT-FOUND-or-empty rather
   than someone else's row (an isolation leak is a 200, which is exactly why it needs its
   own case), and a cross-tenant WRITE → 403. Where a `ToCriteria` calls `Restrict`,
   assert the column is ABSENT for the unprivileged caller — in the JSON and in the
   tabular export, whose header is pruned by the same projection — and present for the
   privileged one; and where the field is ACTIVELY asked for, the refusal is a 403 of its
   own, not a silent scrub. **On GraphQL that case has an edge worth its own assertion**
   (pin ≥ v0.72.1): the same selection with `__typename` beside the restricted field must
   answer identically — every mainstream client appends it to every selection set for
   cache normalization, and below that pin its presence dropped the projection, taking
   the 403 with it while the value still got scrubbed. A boundary that holds only for
   documents no real client sends is not a boundary.

   **3d. When `auth.mode: disabled` — that is a posture to PROVE, not a reason to
   skip.** Assert what the profile actually promises (protected paths reachable
   tokenless; the framework's own `Identity()` absent, so audit rows record an anonymous
   actor) and state in the plan, in one sentence, that the authentication and
   authorization layers are UNPROVEN in this posture. Then offer the §3c suite-owned
   config lane, which turns them on for the security suite alone and touches nothing the
   project owns. **The same applies with `authorization.enabled: false`**: the gate
   no-ops by design, so the honest case asserts a `RequirePermission` route answering
   2xx to any authenticated caller — and the plan says the permission gate itself is
   unproven until the switch is on. Both of these are honest coverage; silently
   dropping the section is not.

   **3e. Report it as coverage, not as prose.** Whatever 3a–3d leaves out is named in
   the plan AND printed by the run — a security family that was never executed is the
   one place where "no failures" reads most like "we are safe". The SKIP column of §6's
   report is where that lands.
4. **Out of scope, named plainly**: load/performance, UI. An `⚠️ OPEN` only if the dev
   asks for it. Integration events are IN scope when the service publishes or consumes
   them: assert the in-TX `integration_events` row per publishing write (SQL — always
   provable), and when the bench has a live relay+broker, the consume side too
   (receiver → effect, dedup via `omnicore_integration_processed`); without a live
   relay, mark the delivery half `⚠️ OPEN` honestly instead of silently dropping the
   capability from the contract.
5. **Runner contract**: **there is exactly ONE runner — `qa/run.sh` — and it calls
   EVERY suite.** Never a second entry point, never one runner per entity: the dev
   learns a single command (`./qa/run.sh`) and CI wires a single line, and the list of
   lanes lives inside it (an explicit array of suite names, in a deterministic order,
   so what runs is readable in one place) with an optional argument to run a SUBSET
   (`./qa/run.sh person employee`) — the subset is a convenience on the same runner,
   never a rival script. Adding a suite means adding its name to that list in the same
   change that creates the file; a `.sh` no lane names is a suite nobody runs.
   It **resolves the project root from its own
   location** — `cd "$(dirname "$0")/.."` as its first act, and every suite does the
   same — so `devops/docker-compose.yml`, `migrations/` and the build all resolve no
   matter which directory the dev invoked it from. The suite lives one level down;
   a path written as if the CWD were the root is a suite that only runs one way. It
   executes suites **fail-fast by default** (first
   RED stops the run; an explicit flag for the exhaustive sweep), prints per-suite
   GREEN/RED counts and a final matrix line, and each suite EXITS NON-ZERO when any
   of its cases failed (without that, fail-fast can never trip); every temp file is
   namespaced per run
   (PID/timestamp — parallel lanes must never share a hardcoded `/tmp` path), **and
   so is every other shared artifact of a lane: the HTTP/gRPC ports, the compiled
   server binary, its log file** (two lanes sharing any of them corrupt each other);
   cleanup
   runs on exit; the service is stopped with SIGTERM, **never** `kill -9` — and the
   runner WAITS on the server PID until the drain completes (default budget ~30s)
   before the next suite binds the same port.
6. **Report contract — the run leaves a DOCUMENT behind: `qa/qa-report.md`.** A run whose
   only trace is terminal scrollback is a run the dev did not see: an agent boots the
   bench, executes hundreds of cases and hands back one sentence, and nothing on disk
   says which suites ran, how many cases passed, or which one failed. The runner writes
   the verdict itself. Canonical shape to copy — the reference service's own runner
   (`qa/run.sh`, its `render_report`) — scaled to this project's single lane:
   - **Header**: timestamp · profile + the engine/transport actually BUILT · the omnicore
     pin · the §2 hygiene mode · suite count · the plan it executes
     (`specs/qa/<suite>/plan.md`).
   - **Matrix**, one row per suite: `| Suite | Pass | Fail | Skip | Verdict | Time |`.
     EVERY declared suite appears — one that never ran prints `—`, never vanishes: a
     suite missing from a report reads exactly like a suite that passed.
   - **Failures section, only when RED**: per failed case its name, expected vs received,
     and the REAL response body (cap the first few per suite), each pointing at the full
     log under `qa/.logs/<run-id>/`. The report has to be enough to diagnose from — that
     is the whole reason it exists.
   - **Footer, one line**, printed to stdout beside the report's path:
     `✅ ALL GREEN — <n>/<n> suites · <cases> cases · <secs>s` or
     `❌ RED — <x> of <n> suites — logs: qa/.logs/<run-id>/`.
   - **Rendered LIVE — rewritten in full after EVERY suite**, never only at the end. A run
     killed halfway (a timeout, a Ctrl-C, a bench that died) still leaves what it had
     proven.
   - **A trap on `EXIT INT TERM` stamps `❌ RUN ABORTED — <reason>`**, disarmed only once
     the final verdict is on disk. Without it, yesterday's green report survives today's
     crash and reads as today's outcome — a stale report is worse than no report.
   - **SKIP keeps its own column** and is never folded into the pass count, same honesty
     rule as everything else here.
   - It is a RUN ARTIFACT, not a decision — running the command reproduces it. So it is
     the one thing this skill writes that a dev may reasonably ignore: **name it at
     hand-off and OFFER the lines (`qa/qa-report.md`, `qa/.logs/`), never edit
     `.gitignore`** (`${CLAUDE_PLUGIN_ROOT}/shared/generated-documents.md` — no skill does).

## Phase 2 — Generate + execute

1. Generate `qa/<entity>.sh` per entity + **`qa/security.sh`, its own lane** + the SINGLE
   `qa/run.sh` that calls them all,
   per the approved plan, at the PROJECT ROOT (`chmod +x` every one — a suite the dev
   cannot execute is not delivered); the plan stays at `specs/qa/<suite>/plan.md`. Every
   generated `.sh` must appear in the runner's lane list before this step is done.
   **The security family gets its own lane** because it is the one that may need a
   different boot — the §3c suite-owned config, a token source to prime, an IdP the bench
   must have up — and folding it into each entity's lane would either drag that setup
   through every one or quietly drop the cases. Its per-entity, per-surface rows still
   come from the same matrix; only the file is separate. When it cannot run at all
   (no token source and no `publicKeyPem` seam), the lane still EXISTS and prints its
   cases as SKIPPED with the reason — never omitted from the runner's list, where absence
   would read as a pass.
   Style:
   plain POSIX-friendly bash + `curl` (+ the project's own tooling for GraphQL/gRPC
   when enabled — `grpcurl` only if available, else mark those cases SKIPPED loudly);
   each case prints its name, expectation and verdict; a failed assertion shows the
   REAL response body, not a summary; **every request pins `Accept-Language`** (one
   fixed locale — envelope assertions must be deterministic, not hostage to the
   machine's locale).
2. **Bench first, on the Mongo/full posture**: `docker compose -f
   devops/docker-compose.yml ps` → missing/unhealthy services `up -d` and wait
   healthy — the read side depends on containers this skill never assumes are up.
3. **Build + boot the service under test — with the right tags and the right config,
   explicitly** (`shared/boot-contract.md`, Build tags, owns the law): build
   `-tags '<engine> <transport>'` from the yaml's `relational.dialect` + `transport:`
   block (no `transport:` block → no transport tag; SQLite → `CGO_ENABLED=0 -tags
   sqlite`); boot in background with `APP_PROFILE` and — when the plan's hygiene
   picked a suite-owned config — `OMNICORE_CONFIG_PATH`, log to a per-lane file, poll
   `/readyz` READING the 503 reason. **Resolve the effective port from the yaml's
   `${VAR:default}` interpolation and probe it FIRST**: something already listening
   there means you may be about to test a binary you didn't build — kill/free the
   port (SIGTERM) before booting, never assume the listener is yours. Full-bench:
   confirm the relay
   reached streaming BEFORE any CDC-dependent case, else those cases report a bench
   problem, not a service failure.
4. Execute `qa/run.sh` — the one runner, whole. Report the real counts **and hand the dev
   the path to `qa/qa-report.md`**: the verdict in your reply is the file's footer, quoted,
   not a paraphrase of it — if the two can disagree, the report is not doing its job.

## Final verify (the gate)

0. **Reconcile contract** (`${CLAUDE_PLUGIN_ROOT}/shared/verify-contract.md`) — walk
   the plan's coverage matrix item by item: every promised case family exists in the
   generated suite and RAN; an unimplemented family is RED or an explicit dev-accepted
   deviation. **And reconcile the lanes**: `ls qa/*.sh` minus `run.sh` must equal the
   runner's lane list exactly — a script on disk that the runner never calls is a
   suite that silently proves nothing, and a lane naming a missing file breaks the
   run for everyone. **`qa/security.sh` is checked by name**: it exists, the runner
   calls it, and its cases either ran or printed SKIPPED with §3's stated reason.
   "There was nothing to test" is not one of the outcomes — §3d says what a
   `disabled` posture proves instead.
1. **The suite is honest**: pick one generated case, break its expectation manually
   (e.g. assert 200 where the service returns 409), run it, watch it FAIL, restore it.
   A suite that cannot fail proves nothing — this meta-case is mandatory. **The same
   deliberate RED proves the REPORT for free**: while it is broken, `qa/qa-report.md`
   must show that suite RED, the case named in the failures section with the real
   response body, and a RED footer. A report that stayed green through a failing run is
   a broken report, and it hides every future failure.
2. **Full run green** (or the RED list reported verbatim with bodies — a RED here is a
   FINDING about the service, routed to `doctor`/the owning skill, never patched away
   in the suite).
3. **Residue as planned**: whatever §2 of the plan promised about data is what
   actually remains; say what was left and where.
4. Leave `specs/qa/<suite>/plan.md` and the `qa/` suite in place, and tell the dev the one
   command that re-runs it (`./qa/run.sh` from the project root), where the verdict landed
   (`qa/qa-report.md`) and that it plus `qa/.logs/` are run artifacts they may want in
   `.gitignore` — their call, offered, never made for them. Offer `/omnicore:run` if the
   dev wants the service kept up for manual poking.

## Re-entry — a plan already exists

`ls specs/qa/*/plan.md` before Phase 1; read every one — they say what earlier rounds
already prove, and duplicating a case family is how a suite doubles in runtime without
covering one more promise.

- **This round's own `<suite>` directory, `Status: DRAFT`** → reopen the Phase 1 gate with
  what is already answered, don't restart it.
- **`Status: APPROVED`, its suite generated** → the round is closed. New coverage is a NEW
  `<suite>` directory whose matrix names what it ADDS and what it inherits, extending the
  same `qa/run.sh`. Never overwrite an approved plan: it is the record of what was
  approved, not a scratch file.
- **An older run's `specs/qa/plan.md`** (flat, no suite directory) → the same document in
  the wrong place: MOVE it to `specs/qa/<suite>/plan.md`, naming the suite after what it
  actually covers, before adding anything to it.

## Knowledge routing — question → source

| When deriving… | Read |
|---|---|
| verb set / handler contracts / bodyless results | auto-handlers |
| status codes / envelopes / notification keys / dual 409 | status-mapping |
| filter operators / `?fields=` / pagination / exports | auto-query-handlers · query-side |
| read-back expectation per backing (poll vs immediate) / archive regime | `${CLAUDE_PLUGIN_ROOT}/shared/read-side.md` (owner) · relational-view for version-exact |
| reaching ANOTHER aggregate from a query — read joins (repository-declared), and the rule-vs-wire split | `${CLAUDE_PLUGIN_ROOT}/shared/read-joins.md` (owner) · read-joins for version-exact contract |
| probes / readyz reasons / boot & drain discipline | `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md` (owner) |
| what's testable under this posture / event semantics | `${CLAUDE_PLUGIN_ROOT}/shared/capabilities.md` (owner) · integration-events · transport |
| GraphQL parity / gRPC procedures | graphql · grpc |
| 401 branches / bearer extraction / publicRoutes matching / GraphQL introspection bypass | auth-middleware |
| 403 per layer — `RequirePermission`, identity-derived `BuildRules`, tenant scoping, `Restrict` | authz-seams |
| where a VALID test token legitimately comes from (self-issued) | token-issuance |

## What this skill never does

- Touch anything outside `specs/qa/<suite>/` (the plan) and `qa/` (the suite) — no
  application code, no yaml, no migrations.
- Weaken or delete a case to make a run green.
- Invent tokens, credentials or endpoints. Signing a deliberately INVALID token to prove
  a 401 is not this — it needs no secret and asserts a refusal; obtaining a VALID one is
  §3c's question, answered from the service, the suite's own keypair, or the dev.
- **Ship a suite with no security family** — see §3. A round may state, in the plan and
  in the report, that the 403 half is unproven and why; it may not leave the question
  unasked.
- Claim coverage it didn't run — SKIPPED is printed as SKIPPED, never folded into
  GREEN.
