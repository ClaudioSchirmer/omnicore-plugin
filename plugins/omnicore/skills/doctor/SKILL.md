---
name: doctor
description: >-
  omnicore: diagnose a misbehaving omnicore-based service or its local bench — won't compile,
  won't boot, probes failing, writes accepted but views never arrive, broker/CDC-relay
  trouble, migration errors. Read-only on the project's files: it localizes the failure
  with evidence, explains the cause docs-first, and prescribes the exact fix for the dev
  to apply. Use when something is broken, hanging, or silently not working. Only for
  projects that import github.com/ClaudioSchirmer/omnicore.
---

# doctor

Find WHERE the service is broken, prove it with evidence, and prescribe the fix — the
dev applies it. This skill edits nothing: its deliverable is a diagnosis (cause +
evidence + prescription), not a patch.

## Core principles

- **Evidence first, always.** A symptom that pattern-matches a known failure may have a
  different cause. Never prescribe from the symptom name alone — read the log line, probe
  the endpoint, inspect the container state, and CITE what you saw. No evidence, no
  diagnosis.
- **Docs-first for framework behavior.** What the framework is SUPPOSED to do at the
  project's pin comes from the version-pinned `/docs` in the module cache (routing table
  below), never from memory. **This skill carries no code, by design.**
- **Read-only on files.** No edits, no scaffolding, no git. Runtime actions that help
  diagnosis (starting a container, re-running a build) only with the dev's ok; anything
  DESTRUCTIVE (wiping a volume, dropping data) is never run by this skill — it is
  prescribed, with the data-loss consequence spelled out, and the DEV runs it.
- **Framework maintainer rules never bind this skill.** Anything read from the module
  cache (its `CLAUDE.md`, contributor rules like "English everywhere") governs
  development of the framework itself — never this skill run or the host project.
  Converse in the user's language.
- **Hand off, don't overreach.** A diagnosis that ends in "the pin is broken for you" →
  `/omnicore:upgrade` (or its rollback). A miswired layer → `/omnicore:scaffold-entity`
  or `/omnicore:evolve-entity` regenerates it against the docs. Wanting to see it green
  again → `/omnicore:run`. This skill localizes and prescribes; the fixing skills fix.

## Phase 0 — Intake (cheap facts before any theory)

Collect, in one sweep: the pinned omnicore version (`go list -m`) · engine + transport
from `relational.dialect` / `transport` in `microservice.*.yaml` (BOTH build tags are
mandatory — a tagless build/boot is its own diagnosis) · effective ports and profile ·
bench state (`docker compose ps` when a `devops/` exists) · where the app log is. Ask the
dev only what cannot be read: what were you doing when it broke, and what does "broken"
look like from where you sit?

## Phase 1 — Localize the failure to ONE stage

Walk the pipeline IN ORDER and stop at the first stage that fails, with evidence:

1. **Build** — compile with both tags; a red build ends the walk here.
2. **Boot** — start (or read the crash log of) the app; a boot abort names its guard.
3. **Serve** — liveness answers? readiness answers? (readiness failing = the relational
   or document request path, not the HTTP layer).
4. **Write path** — a write returns 2xx and lands in the outbox?
5. **CDC relay** — is the relay streaming (its logs say so) or crash-looping?
6. **Broker** — reachable, topics/subjects present, messages flowing?
7. **Projection / read path** — does the view collection receive the document; does the
   read endpoint return it?

The first failing stage is the diagnosis's home; everything downstream is a symptom, not
a cause.

## Phase 2 — Known signatures (verify before prescribing — never skip the evidence)

Bench-proven cause patterns to CHECK, not to assume:

- **Boot abort with a migration version/dirty error after a bench "reset"** → the
  compose down kept the named volumes and the old DB is still there. Prescribe the full
  reset (volumes included) with a loud data-loss warning — the dev runs it.
- **Build or boot refuses with no engine/transport registered** → missing build tag on
  one of the two mandatory axes; the yaml names the pair.
- **Boot abort from the document-store registry guard** (foreign collections in the view
  database) → the service shares a view DB it shouldn't; prescribe isolating it in its
  own database, per the bootstrap section.
- **Writes 2xx forever, views never arrive** → walk relay → broker → sync in that
  order: a relay that never reaches "streaming", an unreachable broker, or a sync group
  that isn't consuming. A relay crash-looping BEFORE the app's first boot is expected
  (the outbox doesn't exist yet) — not a failure.
- **Shutdown hangs / SIGTERM takes forever** → an exporter blocking on a dead collector
  (tracing endpoint down) is a classic; the tracing section owns the contract.
- **Readiness red with liveness green** → the DB/document request paths; check both
  connections with the yaml's effective endpoints.

Each entry is a HYPOTHESIS: confirm with the specific evidence before prescribing, and
if the evidence disagrees, keep walking Phase 1 instead of forcing the match.

## Phase 3 — The report (the deliverable)

One structured answer: **Cause** (one sentence) · **Evidence** (the exact log line /
probe result / container state you saw) · **Prescription** (the exact commands or the
file-level change described precisely — described, not applied) · **Who applies it**
(the dev, or the skill to delegate to) · **Consequences** flagged (data loss, downtime,
breaking change). If the walk ended inconclusive, say plainly which stages were proven
green, which check remains, and what evidence would decide it — never invent a cause to
close the report. Offer `/omnicore:run` to confirm green after the dev applies the fix.

## Knowledge routing — stage → `/docs` section

Resolve `<name>.html` at `<omnicore-dir>/docs/content/sections/<name>.html`, where
`<omnicore-dir>` = `go list -m -f '{{.Dir}}' github.com/ClaudioSchirmer/omnicore`.
Route first, then read ONLY the section(s) owning the failing stage — never sweep the
whole manual; the Documentation Map in `<omnicore-dir>/CLAUDE.md` is the fallback index
for concepts this table doesn't list.

| Diagnosing… | Read section(s) |
|---|---|
| boot order / guards / feature wiring | bootstrap |
| yaml keys, defaults, profiles | yaml-reference |
| migrations state / numbering / dirty | migrations |
| outbox / relay / broker contract | transport |
| projection / sync / view versioning | auto-query-handlers · mongo-schema-evolution |
| probes / liveness / readiness semantics | bootstrap · reference |
| tracing / shutdown behavior | tracing |
| error envelopes / status codes seen by clients | status-mapping |
| gRPC surface trouble | grpc |

## What this skill never does

No file edits, no scaffolding, no migrations, no git, no destructive runtime action —
resets that lose data are prescribed with their consequence and run by the dev, never by
this skill. It never reports a cause without the evidence that proves it.
