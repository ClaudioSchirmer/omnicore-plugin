# Generated documents live under `specs/` — and are committed, never ignored

Two rules, one place. Every skill that writes anything follows both.

## 1. Everything this tooling writes goes under `specs/`

**`specs/` at the service root is the home of every document the tooling
produces, one directory per skill, named after the skill.** Nothing a skill
writes lands loose beside the source tree.

The line it draws is the point: the repository root holds the SERVICE (its Go
packages, its migrations, its devops, its `qa/` suite), and `specs/` holds what
was DECIDED about it — the approved model, the plan, the generator's spec, the QA
plan. A reader who opens the root sees what RUNS; a reader who opens `specs/`
sees intent. Before this, eight working dirs sat at the root interleaved with
`internal/`, `bootstrap/` and `migrations/`, and telling one from the other meant
already knowing which names belonged to the tooling.

**The one exception, and the reason for it:** `/omnicore:qa`'s executable suite
lands in `qa/` at the root, not under `specs/`. Everything else the tooling writes
is READ — by the next dev, the next run, a reviewer; the suite is RUN, by a dev on
their machine and by CI, and a runnable command belongs beside the other things
the root offers, not filed with the decisions. Its plan still goes to
`specs/qa/plan.md`, because a plan is a decision like any other.

**Not `docs/`.** That name is reserved for the project's own documentation,
written for its end users. `specs/` is for the tooling's documents, which have a
different audience (the next dev, the next run, the reviewer) and a different
lifetime.

| Skill | Where it writes |
|---|---|
| `/omnicore:scaffold-service` | `specs/scaffold-service/spec.md` |
| `/omnicore:scaffold-system` | `specs/scaffold-system/domain-map.md` |
| `/omnicore:scaffold-entity` | `specs/scaffold-entity/<entity>/` |
| `/omnicore:evolve-entity` | `specs/evolve-entity/<entity>/` |
| `/omnicore:remove-entity` | `specs/remove-entity/<entity>/` |
| `/omnicore:scaffold-view` | `specs/scaffold-view/<view>/` |
| `/omnicore:evolve-view` | `specs/evolve-view/<view>/` |
| `/omnicore:implement` | `specs/implement/<slug>/` |
| `/omnicore:configure` | `specs/configure/plan.md` |
| `/omnicore:upgrade` | `specs/upgrade/migration-plan.md`, `specs/upgrade/rollback/` |
| `/omnicore:qa` | `specs/qa/plan.md` — the plan. **Its one exception:** the EXECUTABLE suite (`qa/run.sh` + `qa/<entity>.sh`) goes to `qa/` at the project root — a command the dev and CI run, not a document they read |
| `omnicore-gen` (the generator) | `specs/omnicore-gen/` — the specs, their reports, the lock |

A skill needing a shape not listed here still writes it under
`specs/<its-own-name>/`; the rule is the prefix, not the table.

## 2. What is written there is committed — never ignored

**Every document this tooling generates is part of the project and belongs in
the repository.** No skill adds one to `.gitignore`, and none suggests it. A dev
who wants something out will say so; that is their call to make, not a default
to ship.

### What counts as a generated document

Not just prose — anything the tooling writes that a human or a later run READS:

| | |
|---|---|
| `specs/scaffold-service/`, `specs/scaffold-entity/<entity>/`, `specs/evolve-entity/<entity>/`, `specs/remove-entity/<entity>/`, `specs/scaffold-view/<view>/`, `specs/evolve-view/<view>/`, `specs/implement/<capability>/`, `specs/scaffold-system/` | the approved model (`spec.md`) and the plan + status (`tasks.md`, `task_<layer>.md`) |
| `specs/omnicore-gen/<entity>.omnicore.yaml` | the generator's source of truth — the code is derived FROM it, so losing it inverts the dependency |
| `specs/omnicore-gen/<entity>.gen-report.md` | what still needs implementing and what to check; the hand-off |
| `specs/omnicore-gen/lock.json` | which files the generator owns, their hashes, the migration ordinals it already spent, and any adopted edit — without it a regeneration re-allocates ordinals and forgets every refusal |
| `qa/run.sh` + `qa/*.sh` (project root) | the contract suite; it IS the proof the service keeps its promises — and the one thing here that is EXECUTED rather than read, which is why it lives at the root instead of under `specs/` |

### Why, in the order it bites

- **The decisions are the part worth keeping.** Generated code says WHAT the
  service does. The spec and the plan say what was decided, why, and which
  options were weighed and rejected — and that is what the next person needs in
  order to change it safely.
- **A resumed run reads them.** Every skill that writes a working dir resumes
  from the status recorded there; the generator reads its lock. Outside the
  repository, whoever clones the project cannot continue — only start over and
  hope the second pass decides the same way.
- **A reviewer reads them.** It is what a PR reviewer opens to see what was
  approved, instead of inferring it from every generated file.

### What IS ignored

Local, rebuildable state — never a document: binaries, `go.work*`, `.env*`,
OS/editor files, `devops/` data dirs, the SQLite sidecars (`app.db`,
`app.db-wal`, `app.db-shm`).

The test: **would running a command reproduce it byte for byte?** A binary, yes —
ignore it. A decision, no — nothing regenerates it, and it is not in the code.

## History

`scaffold-service` shipped a `.gitignore` listing `scaffold-service/` and
`scaffold-entity/`. Projects therefore committed the result and excluded the
reasoning behind it by default, silently — and only those two of the eight
working dirs were even named, so the newer ones had no rule at all. Hence this
file: one rule, for every skill that writes anything.

Those working dirs were then scattered across the service root, one per skill,
which is what put the second rule here beside the first — the `specs/` prefix
came from the same complaint, one level up: a root where the tooling's documents
and the service's source were indistinguishable at a glance.
