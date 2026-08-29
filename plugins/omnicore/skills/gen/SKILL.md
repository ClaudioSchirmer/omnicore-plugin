---
name: gen
description: >-
  omnicore: run one omnicore-gen command against an existing project and read the answer —
  doctor (drift between the spec, the lock and the files on disk), check, explain, adopt,
  init, prune (remove what an earlier shape of the spec left behind). Use when the dev asks for a generator command by name or asks whether a generated
  tree is still in step with its spec. Creating or CHANGING an entity is not this skill's
  job: that is /omnicore:scaffold-entity (model + plan gates) and /omnicore:evolve-entity
  (impact map + the migration a regeneration never writes).
---

# gen

The generator is a CLI, and most of its commands are useful on their own, long after the
entity was created. This skill is the door to them: **run one command, read its answer, act
on it.** It is short on purpose — there is no flow here, no gates, no spec authoring.

**What this skill is NOT.** Writing a spec and generating an entity is
`/omnicore:scaffold-entity`, which decides the model with the dev and only then reaches the
generator. Do not use `generate` here to create an entity that does not exist yet: the
model gate is where the thinking happens, and skipping it produces a tree nobody agreed to.
**Nor is a real CHANGE to a generated entity this skill's job** — that is
`/omnicore:evolve-entity`: an edited spec regenerates the code but never the database, and
the migration pair, the orphans and the artifacts outside the generator's ownership are
exactly what its impact map exists to carry.

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

## The command

`omnicore-gen` is on PATH — the plugin ships it in `bin/`, which Claude Code adds to the
session's PATH. Always point it at the service:

    omnicore-gen <command> -project <service-dir>

**Pass `-project` explicitly, every time, and read back the `Project:` line the command
prints.** The flag defaults to `.` and the generator walks UP to the nearest `go.mod`,
treating that module as the service — so an omitted flag does not fail, it targets
whatever module the working directory happens to sit in. Every command answers with
`Project: <module> (<abs path>)` before anything else; that line is the only place the
target appears, because every file path it lists afterwards is relative. Confirm it
before a write, and ask the dev for the directory rather than inferring it from the
shell. (`${CLAUDE_PLUGIN_ROOT}/skills/omnicore-gen/SKILL.md` owns the full rationale.)

It builds itself from source on first use, so a **Go toolchain on PATH** is the only
external requirement. `needs a Go toolchain` or `this plugin install is incomplete` means
the generator is unavailable — say so plainly rather than approximating it by hand.

**Read the words, not the exit code.** `doctor` exits 0 whether or not it found anything;
only `check` uses its exit status as a verdict (`0` fine, `1` refused, `2` you called it
wrong). Quote what the tool said to the dev.

The project must be one the generator wrote into — it looks for `specs/omnicore-gen/lock.json`.
If the dev is in the wrong directory, that is what "No generated entity is recorded in this
project" means; it is not a diagnosis of the project.

## `doctor` — is the tree still in step with its spec?

    omnicore-gen doctor -project <service-dir>

Read-only, offline, instant. It reports, per entity:

| Line | What it means | What to do |
|---|---|---|
| `<Entity> (spec …, framework …)` and nothing under it | everything the lock records is intact | nothing |
| `! the spec changed since the last generation` | the YAML moved and the code did not | regenerate through `/omnicore:evolve-entity` (it owns the migration that edit may imply), or ask the dev if the spec edit was intentional |
| `! <path> was edited by hand — regeneration will refuse it` | an owned file's checksum no longer matches | move the change into the spec; if it genuinely cannot be expressed, `adopt` it deliberately |
| `! <path> is gone` | a file the lock records is missing | regenerate; if it was deleted on purpose, `prune` is what stops the line repeating forever |
| `! the spec is missing at <path>` | the source of truth was moved or deleted | find it or restore it — the code is derived FROM it, so this inverts the dependency |
| `· <path> carries a hand edit adopted at <version>` | a deliberate exception, with its reason | nothing, but read the reason: it says whether it still holds |

An adopted file also prints *"it no longer tracks the spec: emitter improvements will not
reach it"*. That is the cost of every adoption and it is worth repeating to the dev when
one shows up — a file adopted for a reason that has since been fixed upstream is a file
frozen for nothing.

This is also the first thing to run when a generated service misbehaves in a way that
smells structural — a symbol that should exist and does not, a rule that should fire and
does not. It is cheaper than reading the tree, and it answers a question nothing else asks.

## `check` — is this spec generatable?

    omnicore-gen check -spec specs/omnicore-gen/<entity>.omnicore.yaml -project <service-dir>
    omnicore-gen check -project <service-dir> -json      # machine-readable status

Validates without writing anything. Every blocker names the key, what is wrong and the fix.
Two verdicts are not about the YAML: framework `behind` (the project pins an older line
than this build targets — upgrade with `/omnicore:upgrade`, do not force past it) and
framework `ahead`/`unknown` (never blocks).

With `-json`, `canGenerate` is the contract — read that, not the exit code.

## `explain` — the spec language, offline

    omnicore-gen explain <topic>
    # coverage, example [flat|sharedbase], keys, names, ownership, rules, vocabulary

Answer a "can the generator express X?" question from `explain`, never from memory. The
generator's vocabulary is deliberately narrower than the framework's API, and `explain
coverage` is the only honest list of what this build emits and what it refuses. **Most of
what looks like a missing capability is a key whose name nobody guessed** — that is exactly
why `explain keys` exists.

## `prune` — remove what an earlier shape of the spec left behind

    omnicore-gen prune -spec specs/omnicore-gen/<entity>.omnicore.yaml -project <service-dir>
    omnicore-gen prune -spec … -project … -apply      # actually remove them

**Dry by default.** It prints three lists and writes nothing until `-apply`: what it would
REMOVE (orphaned Go files, notification declarations and translation keys the spec no
longer declares), what it would FORGET in the lock (files already deleted by hand — the
reason `doctor` reports `is gone` forever), and what it LEAVES ALONE with the reason.

It is the answer to the one thing a shrinking spec used to hand a human with no tool:
`generate` inserts and replaces but never deletes, so a removed field left its labelKey in
all seven catalogs and a removed collection left files that still compiled and meant
nothing. A dead translation key is invisible to `check`, to the compiler and to the tests.

**What it will not touch**, and this is what makes it safe to run routinely: anything whose
text no longer matches what the generator itself wrote (hand-edited), anything `adopt`ed,
anything another entity of the project also declares, and **migrations** — a migration that
ran cannot be taken back by deleting its file. Run `go build` and the tests after: a
notification type removed while the code still references it is a compile error, which is
the good kind.

## `adopt` — keep a hand edit through regeneration

    omnicore-gen adopt <path> -project <service-dir> -why 'what the spec could not express'

The escape hatch, and a lossy one: the file stops tracking the spec forever, so every later
emitter improvement lands everywhere except there. Before running it, ask what the edit
does and whether the spec can express it — if it can, changing the spec is strictly better.
`-why` is optional and worth insisting on: the next person to meet the file is not the one
who edited it, and `doctor` prints that line back.

## `init` — a commented spec template

    omnicore-gen init <Entity> -project <service-dir>

Writes `specs/omnicore-gen/<entity>.omnicore.yaml`, pre-filled from what the project already
states (dialects, whether there is a Mongo, which value objects exist). Useful on its own
to SHOW the shape of a spec. If the dev is starting a real entity, hand them to
`/omnicore:scaffold-entity` instead — the template is the easy part; the model is not.

## `generate` — only for an entity that already exists

Regenerating after a spec change is legitimate and this skill may do it, on two conditions:
`doctor` must already know the entity, and the spec edit must be one that **cannot move a
table** — a wording, a description, a permission string, a filter. Then read the report it
points at (`specs/omnicore-gen/<entity>.gen-report.md`) and prove the tree with build + vet +
tests.

If the edit touches the storage — a field, a nullability, a uniqueness, a child — stop:
regenerating writes the code and NOT the migration, and a tree that boots green against a
table that never moved fails on the first query touching the change. That is
`/omnicore:evolve-entity`.

If the entity is NOT in the lock, stop and hand over to `/omnicore:scaffold-entity`. A
first generation without the model gate is the one thing this skill must not do.

## The generator is beta

It is still improved round by round. Its gate covers a lot and it can still meet a case
nobody has met — usually a spec that validates and produces something that does not
compile. If that happens, say so plainly, quote the compiler, and report it upstream rather
than patching a generated file by hand: patching it is how a tree stops tracking its spec.
