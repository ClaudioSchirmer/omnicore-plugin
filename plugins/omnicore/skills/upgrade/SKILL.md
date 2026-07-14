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
migration plan to a visible `.md`, and applies it only after the dev's explicit approval.

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
  service-code edit exists first as an entry in `upgrade/migration-plan.md` and is applied
  only after the dev approves the plan — same spec-gate doctrine as the scaffold skills.
  No scaffolding, no git, never framework code. The dev can always decline the gate and
  fix by hand (or via a fresh scaffold-entity run against the new docs).
- **Framework maintainer rules never bind this skill.** Anything read from the module
  cache (its `CLAUDE.md`, contributor rules like "English everywhere") governs
  development of the framework itself — never this skill run or the host project.
- **Language — the user's, never imposed.** Converse, and write the migration plan's
  human-facing text, in the language the dev is speaking.

## Phase 0 — Preflight
- **Is this an omnicore service?** `go list -m github.com/ClaudioSchirmer/omnicore` must
  resolve. If not → STOP: nothing to upgrade; to CREATE a service that's `scaffold-service`.
- **Local checkout?** If the module resolves via `replace`/`go.work` (version `(devel)` or
  a path), the pin isn't what's compiled — STOP and say so: bumping `go.mod` here has no
  effect until the overlay is removed. (In THIS workspace that's always the case — the
  skill targets real consumer projects.)
- **Build tags:** discover engine (`postgres|mysql`) + transport (`kafka|nats`) from
  `relational.dialect` / `transport` in `microservice.*.yaml` — every verify below needs
  both tags or it aborts at boot.

## Phase 1 — Check + bring the release
1. **Current pin:** `go list -m -f '{{.Version}}' …/omnicore`.
2. **Target:** default latest — `go list -m -u -f '{{with .Update}}{{.Version}}{{end}}'
   …/omnicore` (empty = already current → report "nothing to upgrade", stop). Honor an
   explicit target the dev names; `go list -m -versions …/omnicore` lists what's available.
   Offline/proxy down → say the check couldn't run and stop (don't guess).
3. **Bring the changelog (read-only, no go.mod change):** `go mod download …/omnicore@<target>`,
   then read `<dir>/docs/content/sections/changelog.html` at `go list -m -f '{{.Dir}}'
   …/omnicore@<target>`. Summarize every release BETWEEN current and target, BREAKING first.
4. **Offer — one message:** "you're on vX; upgrading to vY. What changes: <summary +
   breaking>." No breaking items → "Go ahead? (yes/no)". WITH breaking items, offer three
   paths: **(a) upgrade only** (dev reconciles the code), **(b) upgrade + migrate** — after
   the bump I diagnose the fallout and propose fixes via a migration plan you approve
   before anything is edited, **(c) don't upgrade**. No → stop, nothing changed.

## Phase 2 — Upgrade (only on yes)
1. **Snapshot for rollback FIRST** — copy the current `go.mod` + `go.sum` to the scratch
   dir and record the previous version string. This is the exact restore point. If the
   project is git-tracked, `git checkout go.mod go.sum` is an equivalent restore — but take
   the snapshot anyway so rollback works with or without git.
2. **Bump:** `GOFLAGS=-mod=mod go get github.com/ClaudioSchirmer/omnicore@<target>`, then
   `go mod tidy` (go.mod AND go.sum move together — never one without the other).
3. **Verify:** `go vet` + `go build -tags '<engine> <transport>' ./...`. If the project has
   a fast unit suite, offer to run it too as a stronger check.

## Phase 2b — Migration gate (only when the dev chose "upgrade + migrate", or picks "fix forward WITH the skill" in Phase 3)

1. **Diagnose.** Run the Phase 2 verify; collect every compile/vet error verbatim. Map
   each error to its changelog breaking item. Also list the changelog's breaking items
   that DON'T produce a compile error (behavioral shifts) — those are report-only.
2. **Understand old vs new — docs-first, both pins.** Both versions sit in the module
   cache. For each item, read the owning section at the OLD pin (how it worked) AND at
   the NEW pin (how it works now) — `go list -m -f '{{.Dir}}' …/omnicore@<ver>` +
   `docs/content/sections/<name>.html`. The two doc reads are MANDATORY per item; never
   patch from the compile error alone.
3. **Propose — `upgrade/migration-plan.md`** (project root, visible), `Status: DRAFT`.
   One section per item: the error (verbatim) or changelog entry · how it worked at vX ·
   how it works at vY (both with section names) · the proposed edit (each file + what
   changes and why) · anything needing the dev's judgment marked `⚠️ OPEN: <question>`.
   Behavioral (no-compile-error) items go in a final "needs your attention — not
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
  build doesn't prove those are handled). Point at `help` (understand a new API) or
  `scaffold-entity` (regenerate a layer against the new docs) as next steps. Then offer
  to run: boot the upgraded service for a click-through? Yes → delegate to
  `/omnicore:run`.
- **Broken (vet/build/test fails) → OFFER ROLLBACK, don't force it:**
  - Show the failure verbatim (the first compile errors are usually the breaking surface).
  - **Roll back?** yes → restore the snapshotted `go.mod` + `go.sum`, `go build` to confirm
    the project is green again on the previous version, and hand the dev the changelog's
    breaking section to plan the migration. Report it's back exactly on vX.
  - **Or fix forward** — the dev keeps vY and fixes the code (offer to point
    `help`/`scaffold-entity` at the specific breakage). The snapshot stays until they decide.
  - **Or fix forward WITH the skill** → enter Phase 2b: diagnose, propose
    `upgrade/migration-plan.md`, apply only on approval.

## What this skill never does
No service-code edits outside an APPROVED `upgrade/migration-plan.md`, no scaffolding,
no DB migrations, no git commits. It bumps the framework pin, verifies, and either lands
it or restores the exact prior state. Breaking changes are surfaced and either fixed
through the approved plan or handed to the dev — never worked around silently.
