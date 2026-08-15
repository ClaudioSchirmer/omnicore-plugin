# Generated documents are committed — never ignored

**Every document this tooling generates is part of the project and belongs in the
repository.** No skill adds one to `.gitignore`, and none suggests it. A dev who
wants something out will say so; that is their call to make, not a default to
ship.

## What counts as a generated document

Not just prose — anything the tooling writes that a human or a later run READS:

| | |
|---|---|
| `scaffold-service/`, `scaffold-entity/<entity>/`, `evolve-entity/<entity>/`, `remove-entity/<entity>/`, `scaffold-view/<view>/`, `evolve-view/<view>/`, `implement/<capability>/`, `scaffold-system/<map>/` | the approved model (`spec.md`) and the plan + status (`tasks.md`, `task_<layer>.md`) |
| `specs/<entity>.omnicore.yaml` | the generator's source of truth — the code is derived FROM it, so losing it inverts the dependency |
| `specs/<entity>.gen-report.md` | what still needs implementing and what to check; the hand-off |
| `.omnicore-gen.lock` | which files the generator owns, their hashes, the migration ordinals it already spent, and any adopted edit — without it a regeneration re-allocates ordinals and forgets every refusal |
| `qa/*.sh` | the contract suite; it IS the proof the service keeps its promises |

## Why, in the order it bites

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

## What IS ignored

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
