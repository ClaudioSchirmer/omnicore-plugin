---
name: omnicore-plugin-gen
description: >-
  omnicore: drive omnicore-plugin-gen, the spec-driven code generator, to produce a
  complete entity from one YAML file — then review it, implement what the generator
  refused, and prove it. Read this ONLY after the dev has chosen the codegen path at
  scaffold-entity's generation gateway; it is not a standalone entry point.
---

# omnicore-plugin-gen

The generator writes the mechanical 1,600–3,400 lines of an entity from a spec. You keep
what actually needs judgement: the MODEL (already approved before you got here), the
business rules the spec language cannot express, the review, and the final tests.

**This skill is only reached through `scaffold-entity`'s generation gateway.** If you are
reading it because the dev asked about the generator directly, that is fine — but a real
entity still starts at `/omnicore:scaffold-entity`, because the model gate and the plan
gate are where the thinking happens and this skill does not repeat them.

## What the generator guarantees, and what it does not

Five invariants decide how you treat its output. They are not slogans; each one changes
what you do:

- **Nothing is generated half-way.** Every key of the spec is either consumed by an
  emitter or REFUSED by name, with the reason and the fix. There is no "accepted and
  quietly ignored". So: if `check` is green, everything you wrote in the YAML is in the
  code. If something you wanted is missing, you did not declare it — go look for the
  refusal rather than patching the output.
- **A green spec compiles AND boots.** `check` saying yes means the tree will pass
  `gofmt`, `go vet`, `go build`, its own generated tests, and a real boot. If it does
  not, that is a generator bug worth reporting, not something to work around by hand.
- **Boot-traps are static errors.** The ~20 traps the manual path asks you to remember —
  archive column ⟺ `Modes()`, view `Version`, a child carrying a revision, a reserved
  word per dialect, a column starting with `_`, two entities claiming one view name — are
  refusals from `check`. You do not spend a boot discovering them.
- **It needs no AI and no network.** It reads YAML and writes files. You are a client of
  it, not a prerequisite for it.
- **It owns whole files.** Never hand-edit a generated file. Change the spec and
  regenerate. The two escape hatches are named and permanent (below).

## Step 0 — the command

`omnicore-gen` is on PATH: the plugin ships it in `bin/`, which Claude Code adds to the
session's PATH. Call it as a bare command from wherever you are, pointing it at the
service:

    omnicore-gen <command> -project <service-dir> …

It builds itself from source on first use — the generator travels as Go source, so the
only external requirement is a **Go toolchain on PATH**. If it answers `needs a Go
toolchain` or `this plugin install is incomplete`, say so plainly and go back to the
gateway: the manual path is the honest fallback, never a hand-rolled imitation of the
generator.

Exit codes are real and worth reading: `0` fine, `1` refused (a blocker you must fix),
`2` you called it wrong. Every command also states its verdict in words — quote those to
the dev, not the number.

## Step 1 — learn the language from the binary, not from memory

The spec language is documented BY the generator, generated from the same vocabularies
the validator enforces, so it cannot drift from what the build actually accepts. Read it
before writing a line of YAML:

    omnicore-gen explain vocabulary   # every closed key and its allowed values
    omnicore-gen explain rules        # what the rule DSL can express
    omnicore-gen explain names        # the names YOU declare — it invents none
    omnicore-gen explain coverage     # what THIS build generates and what it refuses
    omnicore-gen explain ownership    # which files it owns, and the two escapes

Do not paraphrase these into the spec from what you remember of the framework. The
generator's vocabulary is narrower than the framework's API on purpose, and `explain
coverage` is the only honest list of what this build will actually emit.

**It invents no names.** There is no pluraliser: `plural`, each `children[].plural` (which
IS the collection's `CollectionName()` — a persisted document key), each
`children[].parentColumn`, and for a shared-base role `storage.base.schemaFunc` /
`linkColumn` are all REQUIRED and read verbatim. Declare them the way the domain says
them, in the domain's language. A guessed plural compiles, boots, and writes the document
under a key nothing reads back.

## Step 2 — write the spec from the APPROVED model

    omnicore-gen init <Entity> -project <service-dir>

`init` pre-fills what the project already states — the dialects, whether there is a Mongo
to project into, which value objects already exist — and comments say WHY each choice
matters. Fill the rest from the model the dev approved at scaffold-entity's gate. Do not
re-decide the model here; if writing the YAML exposes a genuine modelling question, take
it back to the dev.

Three things to get right, because they are the ones that cost a migration later:

- **`storage.kind`** — `flat` (its own table) vs `sharedbase-role` (a ROLE over an
  identity other roles may share). Starting flat and extracting an identity afterwards is
  real data movement.
- **Value objects.** A value whose set is fixed is an `enum` VO, not a string; a value
  with a shape is a `raw` VO, not a regex inline in a rule. Reuse an existing one with
  `vo: {kind: reuse, ref: <Name>}` — a second copy of a rule is a rule that can disagree
  with itself.
- **`read.byParams.controls`.** A control is served ONLY if declared, and an undeclared
  one arriving on the wire is answered with a typed 400. That is a contract, not an
  omission.

## Step 3 — check, and read the refusals as instructions

    omnicore-gen check -spec specs/<entity>.omnicore.yaml -project <service-dir>

Every blocker names the key, what is wrong, and the fix. Fix the SPEC. The two verdicts
that are not about your YAML:

- **framework `behind`** — the project pins an older line than this build targets.
  Upgrade the pin (`/omnicore:upgrade`), do not force past it: going backwards the gap is
  usually an API that does not exist yet.
- **framework `ahead` / `unknown`** — the framework moved, or the pin is a local
  checkout. This never blocks. Generate, then let the compiler be the oracle: if
  something does not build, read the framework's changelog for that version and fix the
  small thing, or report it.

## Step 4 — the ELSE: what the generator cannot do is DECLARED, never improvised

When the DSL cannot express an invariant, you do NOT drop it and you do not write it into
a generated file. You declare it as a named manual item:

    rules:
      manual:
        - id: <a-name>
          description: >-
            What must be guaranteed, in one sentence someone can implement.
          scope: [insert]

The generator then emits `internal/domain/<entity>_rules_manual.go` with the signature,
the contract comment, and a stub per item — **write-once, never re-generated, never
hashed**. The same shape exists for a fact the domain service cannot answer declaratively
(`service.facts[].kind: manual` → `internal/infra/<entity>_service_manual.go`).

The two hook files are NOT equally quiet, and their headers say which is which: an
unwritten rule leaves an invariant unenforced and the service runs on; an unwritten fact
panics the moment a rule asks for it. Implement both before you call the entity done.

## Step 5 — generate

    omnicore-gen generate -spec specs/<entity>.omnicore.yaml -project <service-dir>

It prints created / updated / unchanged / kept-as-is. `kept as-is` means a file it owns
was hand-edited and it REFUSED to touch it — your edit is safe, and it will keep being
refused until you either move the change into the spec or run `adopt` on that path.

**Migrations.** By default it writes a migration only for a CREATE. Regenerating an
entity whose tables already exist does NOT emit an `ALTER` — that is a transaction log,
and only whoever knows what is already in production can write it. `--migrations=yes|no`
overrides the default; the report says which way it went.

## Step 6 — read the report. It is the handover, not a log

`specs/<entity>.gen-report.md` is written every run and is the list of what YOU still owe:

- **What still needs implementing** — every manual rule and manual fact, by id, with the
  description you wrote and the file it landed in. Missing translations too, if any were
  filled with a loud placeholder.
- **What to check** — the decisions the spec made that a reviewer should confirm against
  the domain (uniqueness scope, delete semantics, permissions per operation, the archive
  column, the view version).
- **What was generated** and **what was NOT** — the refused capabilities, so the gap is
  visible rather than assumed.

Read it fully. It is the reason the codegen path does not lose the review.

## Step 7 — review the generated code (this is your job, and it is not a formality)

Do not re-read every file. Read against the plan the dev approved and against the report:

1. **The domain type + `BuildRules`** — do the emitted rules say what the model meant?
   This is where a spec that validates can still be WRONG, because the generator wrote
   exactly what you declared.
2. **The rules the report listed as manual** — implement them now, in the hook file, from
   the routed `/docs`. Nothing else in the tree is yours to write.
3. **The migration** — column types, nullability, uniqueness scope, FK direction. Read
   the DDL, not the YAML you wrote.
4. **Authz** — the permission per operation, and whether `dataAccess` narrows rows the
   way the domain needs (`owner-only`/`tenant` are a different question from the
   permission).
5. **The read side** — the view name and `Version`, the declared filters and controls,
   the indexes. A `?search=` needs a declared text index.

Anything wrong here is fixed **in the spec**, then regenerated. The only files you author
are the two `*_manual.go` hooks.

## Step 8 — prove it

The generator writes its own tests; they are part of the tree and must pass. Run the
service's whole check:

    (cd <service-dir> && GOWORK=off go build -tags "<engine> <transport>" ./... \
       && GOWORK=off go vet -tags "<engine> <transport>" ./... \
       && GOWORK=off go test -tags "<engine> <transport>" ./...)

Then apply the migration and boot, and call the entity for real — a write and a read at
minimum. The generated tests cover the mappers, the rules, the schemas, the catalogs and
the read criteria; they do NOT cover the repository, the domain service, the routes or a
running projection, so the boot is what proves those.

**Write the tests the generator cannot**: the invariants you implemented in the hook
files. They are the only logic in the tree with no generated test behind them, which is
exactly why they are the ones worth testing by hand.

## Step 9 — hand back

Report to the dev: what was generated, what you implemented by hand and why the DSL could
not express it, what the report flagged to check, and the result of build/vet/test/boot.
If any capability was refused, say which and what the consequence is — never let a
refusal reach them as silence.

## Failure modes worth recognising

| What you see | What it means | What to do |
|---|---|---|
| `kept as-is (yours, by design)` | a file it owns was hand-edited | move the change into the spec, or `adopt <path>` if the edit is a framework fix worth surviving regeneration |
| a blocker naming a key you did not write | a default the loader applied | read the fix line; it names the key and the value it needs |
| `not generated by this build` | the capability is refused, with the phase that will bring it | express it another way, or move it to `rules.manual` — never hand-write it into a generated file |
| a spec that validates but the tree does not build | the framework moved past this build | read the framework changelog for the pinned version, fix the small thing, `adopt` the file so it survives |
| a second entity refused on `read.view.name` or `plural` | another spec in this project already claims it | rename this one; the framework aborts the boot on a duplicate |

## What this skill never does

- Never hand-edits a generated file. Never.
- Never re-decides the model. That was approved before the gateway.
- Never invents a name the spec must declare.
- Never reports green without a build, a vet, the tests and a boot.
