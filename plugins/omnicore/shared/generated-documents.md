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
`specs/qa/<suite>/plan.md`, because a plan is a decision like any other.

**Not `docs/`.** That name is reserved for the project's own documentation,
written for its end users. `specs/` is for the tooling's documents, which have a
different audience (the next dev, the next run, the reviewer) and a different
lifetime.

| Skill | Where it writes |
|---|---|
| `/omnicore:scaffold-service` | `specs/scaffold-service/spec.md` |
| `/omnicore:scaffold-system` | `specs/scaffold-system/<system>/domain-map.md` |
| `/omnicore:scaffold-entity` | `specs/scaffold-entity/<entity>/` |
| `/omnicore:evolve-entity` | `specs/evolve-entity/<entity>/<change>/` — one per ROUND |
| `/omnicore:remove-entity` | `specs/remove-entity/<entity>/` |
| `/omnicore:scaffold-view` | `specs/scaffold-view/<view>/` |
| `/omnicore:evolve-view` | `specs/evolve-view/<view>/<change>/` — one per ROUND |
| `/omnicore:implement` | `specs/implement/<slug>/` |
| `/omnicore:configure` | `specs/configure/<change>/plan.md` — one per CONVERSION |
| `/omnicore:upgrade` | `specs/upgrade/<from>-to-<to>/` — one per UPGRADE, holding `migration-plan.md` and `rollback/` |
| `/omnicore:qa` | `specs/qa/<suite>/plan.md` — the plan. **Its one exception:** the EXECUTABLE suite (`qa/run.sh` + `qa/<entity>.sh`) goes to `qa/` at the project root — a command the dev and CI run, not a document they read. Its OUTPUT (`qa/qa-report.md`, `qa/.logs/`) is an artifact, not a document — see *What IS ignored* |
| `omnicore-gen` (the generator) | `specs/omnicore-gen/` — the specs, their reports, the lock |

A skill needing a shape not listed here still writes it under
`specs/<its-own-name>/`; the rule is the prefix, not the table.

### A skill that runs REPEATEDLY writes one directory per ROUND

Some of these skills act on the same target once (an entity is scaffolded once, removed
once). Others act on it again and again: an entity is evolved, a view is extended, a
posture is converted, a pin is bumped — each of them many times over a project's life.
**Those skills take a round segment in their path** — `<change>`, `<from>-to-<to>`,
`<suite>`, `<slug>` — and a run never writes into an earlier round's directory.

The reason is the whole point of rule 1. `specs/` holds what was DECIDED; a run that
overwrites the previous run's document deletes a decision and leaves only the last one
standing. The code cannot fill the gap: it shows the shape the project ended at, never
the shapes it passed through, which options were weighed and rejected, or what the dev
knowingly accepted as lost on the way. Once that document is gone, nothing regenerates
it — which is exactly the test at the bottom of this file.

So: **an approved document is never overwritten.** A new change is a new directory whose
plan says what it builds on, and the earlier ones stay beside it, in order, as the
target's history. A skill with a round segment therefore also owes two behaviors:

- **read the earlier rounds before proposing** — they are the only record of what was
  already settled, and re-litigating or silently reversing a past decision is the failure
  this layout exists to prevent;
- **migrate a flat legacy document** — a `spec.md` / `plan.md` sitting directly in the
  target's directory came from a run that predates this rule. MOVE it into a round
  directory named after what it actually did, before writing anything new. Move, never
  copy, and never rewrite what it says.

## 2. What is written there is committed — never ignored

**Every document this tooling generates is part of the project and belongs in
the repository.** No skill adds one to `.gitignore`, and none suggests it. A dev
who wants something out will say so; that is their call to make, not a default
to ship.

### What counts as a generated document

Not just prose — anything the tooling writes that a human or a later run READS:

| | |
|---|---|
| `specs/scaffold-service/`, `specs/scaffold-entity/<entity>/`, `specs/evolve-entity/<entity>/<change>/`, `specs/remove-entity/<entity>/`, `specs/scaffold-view/<view>/`, `specs/evolve-view/<view>/<change>/`, `specs/implement/<capability>/`, `specs/scaffold-system/<system>/`, `specs/qa/<suite>/`, `specs/configure/<change>/`, `specs/upgrade/<from>-to-<to>/` | the approved model (`spec.md`) and the plan + status (`tasks.md`, `task_<layer>.md`) — **every round of them**, not just the latest: the older directories are the target's history, and deleting one is deleting a decision |
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
`app.db-wal`, `app.db-shm`), and the QA runner's own output — `qa/qa-report.md`
and `qa/.logs/` — which is the RESULT of running the suite, not a decision about
it: `./qa/run.sh` writes it again from scratch every time. The suite is the
document; its last verdict is an artifact.

Ignorable is not the same as ignored: the rule above still holds — **no skill
edits `.gitignore`**. `qa` names the report at hand-off and OFFERS those two
lines; whether they go in is the dev's call, and a team that wants the last
verdict visible in the repository is not doing anything wrong.

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

The round segment came last, and from the sharpest version of the same question:
if `evolve-entity` runs twice on one entity, does the second run overwrite the
first one's spec? It did. `evolve-entity`, `evolve-view`, `configure` and
`upgrade` all wrote to a path keyed only by their target — or, for the last two,
to no key at all — so their "re-entry" rules could only mean *resume the same
unfinished change*, and a genuinely new change silently replaced the record of
the previous one. `qa` and `implement` had already solved it with `<suite>` and
`<slug>`; the fix was to give the other four the same segment. `upgrade` carried
a second failure inside the first: one shared `rollback/`, so starting a second
upgrade replaced the restore point of one still being verified.
