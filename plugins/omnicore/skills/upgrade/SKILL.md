---
name: upgrade
description: >-
  Upgrade an existing omnicore-based service to a newer published release of the
  framework — check the current go.mod pin, show the target version's changelog, and on
  the dev's ok run go get + tidy + build; if the build breaks, offer to roll back to the
  previous version. Use when the dev wants to upgrade/update/bump the omnicore framework
  version of a service. Only for projects that import github.com/ClaudioSchirmer/omnicore.
---

# upgrade

Move a service from its current omnicore pin to a newer release, safely: SHOW what
changes before touching anything, bump only on an explicit ok, and keep an exact rollback
ready if it breaks. Changes exactly two files — `go.mod` + `go.sum` — and nothing else;
never edits service code to chase a breaking change (that is the dev's call, guided by
the changelog).

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
- **This skill only bumps the framework pin.** No code migration, no scaffolding, no git.
  Fixing code for a breaking change is handed back to the dev (or a fresh scaffold-entity
  run against the new docs).

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
   breaking>. Go ahead? (yes/no)." No → stop, nothing changed.

## Phase 2 — Upgrade (only on yes)
1. **Snapshot for rollback FIRST** — copy the current `go.mod` + `go.sum` to the scratch
   dir and record the previous version string. This is the exact restore point. If the
   project is git-tracked, `git checkout go.mod go.sum` is an equivalent restore — but take
   the snapshot anyway so rollback works with or without git.
2. **Bump:** `GOFLAGS=-mod=mod go get github.com/ClaudioSchirmer/omnicore@<target>`, then
   `go mod tidy` (go.mod AND go.sum move together — never one without the other).
3. **Verify:** `go vet` + `go build -tags '<engine> <transport>' ./...`. If the project has
   a fast unit suite, offer to run it too as a stronger check.

## Phase 3 — Outcome
- **Green →** report success: the new version, the headline changes, and — from the
  changelog — any BREAKING items the dev still must reconcile in their own code (a green
  build doesn't prove those are handled). Point at `help` (understand a new API) or
  `scaffold-entity` (regenerate a layer against the new docs) as next steps.
- **Broken (vet/build/test fails) → OFFER ROLLBACK, don't force it:**
  - Show the failure verbatim (the first compile errors are usually the breaking surface).
  - **Roll back?** yes → restore the snapshotted `go.mod` + `go.sum`, `go build` to confirm
    the project is green again on the previous version, and hand the dev the changelog's
    breaking section to plan the migration. Report it's back exactly on vX.
  - **Or fix forward** — the dev keeps vY and fixes the code (offer to point
    `help`/`scaffold-entity` at the specific breakage). The snapshot stays until they decide.

## What this skill never does
No service-code edits, no scaffolding, no migrations, no git commits. It bumps the
framework pin, verifies, and either lands it or restores the exact prior state. Breaking
changes are surfaced and handed to the dev — never worked around silently.
