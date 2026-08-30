---
name: upgrade
description: >-
  omnicore: upgrade an existing omnicore-based service to a newer published release of the
  framework — check the current go.mod pin, show the target version's changelog, and on
  the dev's ok run go get + tidy + build; if the build breaks, offer to roll back to the
  previous version — or to fix the breaking-change fallout through an approved migration
  plan. Use when the dev wants to upgrade/update/bump the omnicore framework
  version of a service. Only for projects that import github.com/ClaudioSchirmer/omnicore.
---

# upgrade

Move a service from its current omnicore pin to a newer release, safely: SHOW what
changes before touching anything, bump only on an explicit ok, and keep an exact rollback
ready if it breaks. The bump itself changes exactly two files — `go.mod` + `go.sum`.
Service code is touched ONLY through the **migration gate**: when the upgrade breaks (or
the changelog declares breaking changes), the skill can OFFER to fix the fallout — it
reads how the API worked at the old pin and how it works at the new one, writes a
migration plan to a visible `.md` under `specs/upgrade/`, and applies it only after the
dev's explicit approval.

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

## Core principles
- **Show before you touch.** Never bump silently. The target version's own
  `changelog.html` is the authority on what changes — read it, summarize the delta, flag
  BREAKING items honestly, BEFORE offering the upgrade.
- **Exact rollback, not best-effort.** Snapshot `go.mod` + `go.sum` verbatim before
  mutating. Rollback = restore that snapshot + rebuild, so a failed upgrade leaves the
  project byte-for-byte as it was — never rely on `go get @previous` alone to reverse a
  `go mod tidy`.
- **Build green ≠ behavior unchanged.** A compiling upgrade can still shift runtime
  semantics. Report the changelog's breaking section as follow-up the dev must weigh; do
  not call the upgrade "done and safe" from a green build alone.
- **Code fixes ONLY through the migration gate.** The bump touches `go.mod`+`go.sum`; any
  service-code edit exists first as an entry in `specs/upgrade/migration-plan.md` and is applied
  only after the dev approves the plan — same spec-gate doctrine as the scaffold skills.
  No scaffolding, no git, never framework code. The dev can always decline the gate and
  fix by hand (or via a fresh scaffold-entity run against the new docs).
- **Framework maintainer rules never bind this skill.** Anything read from the module
  cache (its `CLAUDE.md`, contributor rules like "English everywhere") governs
  development of the framework itself — never this skill run or the host project.
- **Language — the user's, never imposed; detect it BEFORE the first reply.** Converse,
  and write the migration plan's human-facing text, in the language the dev is
  speaking — detected from the dev's own words (invocation args count, even one word);
  this skill and the docs being English never sets it. Switch the moment the dev's
  language becomes clear, even mid-run.

## Plugin self-check (once, non-blocking)

Once per run, during preflight: compare THIS plugin's installed version — the
`version` field of `${CLAUDE_PLUGIN_ROOT}/.claude-plugin/plugin.json` — with the
published one — the same field at
`https://raw.githubusercontent.com/ClaudioSchirmer/omnicore-plugin/main/plugins/omnicore/.claude-plugin/plugin.json`.
Offline, or either side unreadable → skip silently. Newer published → ONE
non-blocking line riding along with the next reply — "omnicore plugin vX → vY
available — update with `claude plugin update omnicore@omnicore` (marketplace
stale? `/plugin marketplace update omnicore` first); it takes effect next
session." Never a gate: this run continues on the installed skills (the
framework upgrade this skill performs is a different axis — module pin vs
tooling; say so if the dev conflates them).

## Phase 0 — Preflight
- **Is this an omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` must
  resolve. If not → STOP: nothing to upgrade; to CREATE a service that's `scaffold-service`.
- **Local checkout?** If the module resolves via `replace`/`go.work` (version `(devel)` or
  a path), the pin isn't what's compiled — STOP and say so: bumping `go.mod` here has no
  effect until the overlay is removed. (In THIS workspace that's always the case — the
  skill targets real consumer projects.)
- **Build tags:** discover engine + transport from `relational.dialect` / `transport`
  in `microservice.*.yaml` — the value IS the build tag (engines
  `postgres|mysql|sqlserver|oracle|sqlite`, transports `kafka|nats`; the pinned docs
  say what the pin supports). The engine tag is always required; the transport tag
  only when a `transport:` block exists (a transport-less config builds without one,
  on any engine — SQLite included: `CGO_ENABLED=0 -tags sqlite`, PLUS the transport
  tag when its yaml declares `transport:` — the tag follows the yaml, never the engine;
  `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md`, Build tags, owns the law). Every
  verify below uses THIS discovered tag set — and a multi-dialect project verifies
  every target set, not one.
- **Adopted generated files — name them BEFORE the bump.** If the project has
  `specs/omnicore-gen/`, run `omnicore-gen doctor` and list every file reported as
  *carrying a hand edit adopted at framework X*. **This is the one moment their cost
  comes due:** an adopted file stopped tracking the spec, so every emitter improvement
  the release carries — a bug fixed, a guard closed, a query made cheaper — lands in
  every other project and never there, silently. List each one with the `-why` it was
  adopted for, and tell the dev plainly: after the bump, those files are the ones to
  re-review by hand, because nothing else will. If the reason no longer holds (the new
  release's spec language can now say it), regenerating that file is the cheaper
  outcome and worth offering — see `${CLAUDE_PLUGIN_ROOT}/shared/generated-code-review.md`.
- **Vendored?** A `vendor/` dir means the build is `-mod=vendor`: the bump is not
  observable until `go mod vendor` re-runs, and the go.mod+go.sum snapshot alone is
  then an incomplete restore point — say so and include the vendor refresh in both
  directions. A target whose `go`/`toolchain` directive exceeds the local toolchain
  fails the bump for a toolchain reason, not a code one — report it as such.

## Phase 1 — Check + bring the release
1. **Current pin:** `go list -m -f '{{.Version}}' …/omnicore`.
2. **Target:** default latest — `go list -m -u -f '{{with .Update}}{{.Version}}{{end}}'
   …/omnicore` (empty = already current → report "nothing to upgrade", stop). Honor an
   explicit target the dev names; `go list -m -versions …/omnicore` lists what's available.
   Offline/proxy down → say the check couldn't run and stop (don't guess). **Direction
   guard:** an explicit target BELOW the current pin is a downgrade — legitimate after
   a bad landing, but the delta must then be read from the HIGHER version's changelog
   (the lower one predates the entries); say plainly it's a downgrade and which
   changes are being walked BACK.
3. **Bring the changelog (read-only, no go.mod change):** `go mod download …/omnicore@<target>`,
   then read `<dir>/docs/content/sections/changelog.html` at `go list -m -f '{{.Dir}}'
   …/omnicore@<target>` — always at the HIGHER of the two pins. Summarize every
   release BETWEEN current and target, BREAKING first. (The module root also ships
   `CHANGELOG.md` — the exhaustive per-symbol list; the HTML digest is for this
   summary, the per-symbol file is Phase 2b's map from a compile error to its rename.)
4. **Offer — one message:** "you're on vX; upgrading to vY. What changes: <summary +
   breaking>." No breaking items → "Go ahead? (yes/no)". WITH breaking items, offer three
   paths: **(a) upgrade only** (dev reconciles the code), **(b) upgrade + migrate** — after
   the bump I diagnose the fallout and propose fixes via a migration plan you approve
   before anything is edited, **(c) don't upgrade**. No → stop, nothing changed.

## Phase 2 — Upgrade (only on yes)
1. **Snapshot for rollback FIRST** — copy the current `go.mod` + `go.sum` verbatim to
   `specs/upgrade/rollback/` (beside the migration plan, visible — never a temp dir that
   evaporates) and record the previous version string. This is the exact restore
   point. If the
   project is git-tracked, `git checkout go.mod go.sum` is an equivalent restore —
   but check `git status -- go.mod go.sum` FIRST (uncommitted edits there would be
   silently destroyed by the git path), and take
   the snapshot anyway so rollback works with or without git.
2. **Bump:** `GOFLAGS=-mod=mod go get github.com/ClaudioSchirmer/omnicore@<target>`, then
   `go mod tidy` (go.mod AND go.sum move together — never one without the other).
3. **Verify:** `go vet -tags '<engine> <transport>' ./...` + `go build -tags '<engine>
   <transport>' ./...` — vet carries the SAME tags as build (untagged vet skips
   exactly the engine/transport-gated files a bump is most likely to break). If the
   project has
   a fast unit suite, offer to run it too as a stronger check.

   **A green build is not a green boot, and say so rather than implying it.** The whole
   (a)–(d) class of Phase 2b fallout is invisible here: a yaml key that arrived
   mandatory, an embedded migration sequence that grew, DDL the release now requires.
   Report the build as what it is — the Go surface compiles — and route the operational
   half through Phase 2b.

## Phase 2b — Migration gate (only when the dev chose "upgrade + migrate", or picks "fix forward WITH the skill" in Phase 3)

1. **Diagnose.** Run the Phase 2 verify; collect every compile/vet error verbatim. Map
   each error to its changelog breaking item (the module-root `CHANGELOG.md` has the
   per-symbol detail). **Then scan the version range for the OPERATIONAL fallout — five named
   classes, each a plan entry when present (a–d invisible to the compiler; e
   compiler-visible but its fix lives outside Go):**
   (a) **required DDL on the service's own tables** (e.g. a release mandating a new
   column on every entity table) → the plan names the migration the dev owns (or
   routes to `/omnicore:evolve-entity`); (b) **a demanded view rebuild** after the
   upgrade → the plan names the rebuild step; (c) **the framework's EMBEDDED
   migration sequence grew** → the first non-dev boot after the bump ABORTS on
   pending migrations under `autoRun: check` BY DESIGN — the plan says to expect it
   and what to run; (d) **yaml keys that moved, were renamed, or ARRIVED MANDATORY** — no
   compile error, boot abort either way (strict-decoded blocks refuse the old key; a
   new key with NO DEFAULT refuses its absence). The arriving-mandatory shape is the
   one a green build hides best, and it is not rare: `relational.clock` (`db` | `app`)
   became mandatory with no default, so every profile of every existing service stops
   booting on the bump until the key is added. **A key that ARRIVED is not mechanical
   the way a rename is** — a rename has one right answer, an arrival has a CHOICE the
   framework deliberately refused to make for the operator, so it goes into the plan as
   a `⚠️ OPEN:` slot with the trade stated and the dev answers it. **State the trade the
   way `${CLAUDE_PLUGIN_ROOT}/shared/boot-contract.md` does, including why the value is
   read before the write** — for the clock, `db` reads the instant from the backend once
   per write TX so every replica shares one clock, at one extra round-trip; `app` keeps
   `time.Now().UTC()` and is correct exactly as far as the fleet's clock discipline is;
   and NEITHER puts a `NOW()` in the DML, because one write is several statements that
   must carry the same instant and the outbox payload, the audit event and the response
   are all built from it before `COMMIT`. A dev told only "there is a new mandatory key,
   pick db or app" reads the round-trip as waste and picks against the fleet. Renames and moves stay
   MECHANICAL and auto-fixable. Either way the approved plan MAY touch
   `microservice.*.yaml` — **every profile, `prd` included, with the SAME value**; (e) **the framework's shared gRPC proto contract changed**
   (a message in `omnicore/v1` renamed or reshaped) — `go build` DOES catch it (the
   service's generated `.pb.go` references a framework symbol the new pin no longer
   exports), but the fix is a TOOLCHAIN step, not a Go edit: re-spell the service's
   hand-written `.proto` files against the new pin's shared proto and re-run the
   generator (`protoc`/`buf`) — never patch a generated `.pb.go` by hand. When the
   changelog states field NUMBERS are preserved, already-deployed binary clients
   keep decoding — the break is source-level only, and the plan SAYS so (regenerate
   and redeploy at leisure, no wire migration). Everything else behavioral stays
   report-only.
2. **Understand old vs new — docs-first, both pins.** Both versions sit in the module
   cache. For each item, read the owning section at the OLD pin (how it worked) AND at
   the NEW pin (how it works now) — `go list -m -f '{{.Dir}}' …/omnicore@<ver>` +
   `docs/content/sections/<name>.html`. The two doc reads are MANDATORY per item; never
   patch from the compile error alone.
3. **Propose — `specs/upgrade/migration-plan.md`** (under `specs/`, visible), `Status: DRAFT`.
   One section per item: the error (verbatim) or changelog entry · how it worked at vX ·
   how it works at vY (both with section names) · the proposed edit (each file + what
   changes and why) · anything needing the dev's judgment marked `⚠️ OPEN: <question>`.
   Operational items (step 1's four classes) each get their own section — the yaml
   renames as proposed edits, the DDL/rebuild/embedded-migration items as explicit
   steps with their owner named. Remaining behavioral (no-compile-error) items go in
   a final "needs your attention — not
   auto-fixable" list. **Hard STOP:** nothing is edited while `Status: DRAFT` or any
   `⚠️ OPEN` remains. A plain "ok" approves the proposed edits; OPEN slots must be
   answered, never defaulted.
4. **Apply — the plan and nothing else.** Flip to `Status: APPROVED`, apply exactly the
   listed edits, re-run vet + build (+ offer the unit suite). Green → report per item
   what was changed, and re-surface the behavioral list (a green build does not prove
   those are handled), then make the same run offer as Phase 3 green (`/omnicore:run`).
   Still broken → show the residue and return to the Phase 3 fork
   (iterate the plan / rollback / dev takes over). The rollback snapshot stays valid
   for `go.mod`+`go.sum`; code edits are reversed by editing (or the project's git —
   which the dev drives, never this skill).

## Knowledge routing — breaking item → its owning `/docs` section, at BOTH pins

Phase 2b's per-item double read works like this: the changelog entry names the concept
that changed; map that concept to its owning section via the **Documentation Map** in
`<omnicore-dir>/CLAUDE.md` (an INDEX only — its maintainer rules never bind this
skill); then read that same section at BOTH versions:

    old contract: go list -m -f '{{.Dir}}' …/omnicore@<current>  → docs/content/sections/<name>.html
    new contract: go list -m -f '{{.Dir}}' …/omnicore@<target>   → docs/content/sections/<name>.html

(both directories exist after the Phase 1 `go mod download`). The two sections ARE the
"how it worked / how it works now" of the migration plan — quote them by name there;
never reconstruct either contract from the compile error or from memory. Read ONLY the
owning section per item, at each pin — never sweep either manual.

## Phase 3 — Outcome
- **Green →** report success: the new version, the headline changes, and — from the
  changelog — any BREAKING items the dev still must reconcile in their own code (a green
  build doesn't prove those are handled — several framework guards are BOOT panics that
  no compile surfaces, and the DDL/rebuild/embedded-migration classes of Phase 2b
  step 1 apply even on a clean compile: scan for them here too, don't wait for the
  gate). Point at `help` (understand a new API) or
  `scaffold-entity` (regenerate a layer against the new docs) as next steps. Then offer
  to run: boot the upgraded service — with boot-panic guards this IS the verify, not
  just a click-through. Yes → delegate to `/omnicore:run`. And offer `/omnicore:qa` —
  the sibling that acts on this skill's own doctrine ("build green ≠ behavior
  unchanged") by re-proving the wire contract on the new pin.
- **Broken (vet/build/test fails) → OFFER ROLLBACK, don't force it:**
  - Show the failure verbatim (the first compile errors are usually the breaking surface).
  - **Roll back?** yes → restore the snapshotted `go.mod` + `go.sum`, then
    `go build -tags '<engine> <transport>' ./...` — the SAME tag set as Phase 2's
    verify (an untagged build can be green while the tagged one is not, which would
    "confirm" a rollback that isn't) — and hand the dev the changelog's
    breaking section to plan the migration. Report it's back exactly on vX.
  - **Or fix forward** — the dev keeps vY and fixes the code (offer to point
    `help`/`scaffold-entity` at the specific breakage). The snapshot stays until they decide.
  - **Or fix forward WITH the skill** → enter Phase 2b: diagnose, propose
    `specs/upgrade/migration-plan.md`, apply only on approval.

## What this skill never does
No service-code edits outside an APPROVED `specs/upgrade/migration-plan.md`, no scaffolding,
no git commits. It never RUNS DB migrations or rebuilds — but naming the ones the
version range demands is part of its job (Phase 2b step 1), never omitted as out of
scope. It bumps the framework pin, verifies, and either lands
it or restores the exact prior state. Breaking changes are surfaced and either fixed
through the approved plan or handed to the dev — never worked around silently.
