# omnicore-plugin — repo rules

The Claude Code plugin for the omnicore framework: 16 `/omnicore:*` skills under
`plugins/omnicore/skills/`, plus **omnicore-gen** — the spec-driven code
generator — as Go source under `plugins/omnicore/gen/`, launched by
`plugins/omnicore/bin/omnicore-gen` (on the session PATH), plus the write-time
guards under `plugins/omnicore/hooks/`.

Everything the plugin needs lives UNDER `plugins/omnicore/`. That is Claude Code's
rule, not a preference: a path that traverses outside the plugin root stops
resolving once the plugin is cached.

## ⚠️ Change the generator → update the generator's own documentation

**The generator documents itself, and that documentation is what an agent reads
BEFORE writing a spec.** A capability nobody is told about is a capability nobody
uses — and the failure mode is not a missing paragraph, it is someone working
around a feature they already own. Both have happened here:

- an author needed "unique among the rows that are not archived", found nothing,
  **edited the generated SQL by hand**, and only later stumbled on
  `unique.scope: active-only` — which had been there all along, undocumented;
- the generator learned to emit a GraphQL facet-clear mutation and nothing an
  agent reads first mentioned it, so it would only be found by reading output.

So a change to `plugins/omnicore/gen/` is not done until its documentation moved
with it. What to update, and which parts the build already catches:

| You changed | Update | Enforced? |
|---|---|---|
| a key in `internal/spec/spec.go` | the field's doc comment — `explain keys` derives from it | listed automatically; the COMMENT is on you |
| a closed set in `internal/spec/vocab.go` | register it in `Vocabularies()` with its yaml path and what the choice decides | ✅ `TestEveryVocabularyIsDocumented` |
| what the build refuses | `RefusedKeys()`, so `explain keys` marks it | ✅ shared with the example test |
| a capability | the `Cap*` name in `internal/spec/coverage.go` — it is what `explain coverage` prints | ✅ manifest test |
| anything an author must SEE to use | one of the two examples (`explain example [flat|sharedbase]`) | ✅ every key must appear in one |
| how an author should WORK | `skills/omnicore-gen/SKILL.md` | ❌ **discipline only** |
| what a reviewer must CHECK | `internal/report` — the gen-report is the hand-off | ❌ **discipline only** |

The last two rows are the ones that rot, precisely because nothing fails when
they do. Treat them as part of the change, not as follow-up.

## Hooks — the floor under a rule, never the rule

`plugins/omnicore/hooks/` holds `hooks.json` and the guards it runs. A hook goes here for
one reason: **a rule that nothing enforces is a rule that rots** — the "❌ discipline only"
rows of the table above are the ones this repo has watched decay. A hook does not replace
the skill's knowledge; it catches the cases decidable from the text of a single tool call
and hands the answer back.

Three constraints, all of them learned the expensive way:

- **Scope hard, then scope again.** A guard fires only for the paths it owns AND only in a
  module that consumes omnicore — never the framework's own repository, never an unrelated
  project. It exits 0 silently when a dependency it needs (`jq`) is missing. A guard that
  misfires once gets the whole plugin's hooks disabled.
- **Refuse what is certain, ASK what is a judgment.** Exit 2 is for a violation that holds
  whatever the author intended; anything that could legitimately be what the author meant is
  a `permissionDecision: "ask"` so the developer decides. Guessing on the developer's behalf
  is how a guard becomes an obstacle.
- **A refusal carries the destination, not just the rule.** The message says where the thing
  goes instead and names the counter-arguments; blocking without an answer only produces a
  detour.
- **The guard and its owner file move together.** A hook enforcing a decision references the
  `shared/*.md` that owns it; changing one without the other leaves the message and the rule
  telling different stories. Prove a guard against the reference service before shipping it
  — replay its real files through the hook and require ZERO refusals.

## Cutting a release — the number comes from what is PUBLISHED

**The top of `CHANGELOG.md` is not the released version.** A numbered section can sit
there for weeks before it ships: the changelog is where the next release is ASSEMBLED.
Reading it as history and bumping past it invents a version that skips a real one.

Before writing any version number, find what is actually out there:

    curl -s https://raw.githubusercontent.com/ClaudioSchirmer/omnicore-plugin/main/plugins/omnicore/.claude-plugin/plugin.json | grep version

That `version` field IS the published one — it is what the skills' own self-check
compares against. Git tags are NOT: they stop at `v0.8.4` and have not tracked releases
since. Offline, or the answer is ambiguous → **ask the maintainer**. Do not infer.

Then: the next version is the one after the PUBLISHED one. If `0.17.x` is out and the
changelog already carries a `[0.18.0]` section, that section is the release being
prepared — new work goes INTO it, and its date moves to the day it closes. There is no
`[0.19.0]` until `0.18.0` has shipped.

The second half of the same mistake, and the expensive one: **a defect fixed inside an
unreleased section is not a `Fixed` entry.** Nobody received the broken version, so
there is nothing to have been fixed from a reader's point of view — it belongs to the
`Added`/`Changed` entry for the capability that contained it. Both halves happened here
in one sitting: a `[0.19.0]` invented over an unshipped `[0.18.0]`, and 24 `Fixed`
entries for a generator that had never left the repo.

## The gate

`plugins/omnicore/gen/scripts/golden.sh` — generate → gofmt → vet → build → the
generated tests → a real boot → DDL on five engines → the coverage matrix. It runs
against the generator's OWN bench (`gen/devops/docker-compose.yml`, containers
`omnicore-gen-*`), **never** a dev or QA one: the DDL lanes drop and recreate
tables, and borrowing someone's engines means a gate that passes here by breaking
work over there. It has happened; do not repeat it.

Green means green: `88 passed · 0 failed · 0 skipped`. A skipped lane is a lane
that proved nothing.

While the framework version the generator targets is UNPUBLISHED there is no tag for
the vendored host to pin, so every lane that compiles generated code would be measuring
today's emitters against yesterday's API. Point the gate at the checkout instead —
`OMNICORE_LOCAL=/path/to/omnicore bash plugins/omnicore/gen/scripts/golden.sh` — and bump
`plugins/omnicore/gen/testdata/host/go.mod` once the tag exists. The gate says which
situation it is in on startup.

## Skills

The skills in `plugins/omnicore/skills/` are the single source of truth for
behaviour, and they are **version-agnostic**: they read the framework version from
the project's `go.mod` and treat that version's bundled `/docs` as the sole
authority. When a convention here and the framework's docs disagree, the docs win
and the convention is stale — fix it, and say so in `CHANGELOG.md`.

**Everything a skill writes into a consumer project goes under `specs/`**, one
directory per skill — including the generator's, at `specs/omnicore-gen/`
(`internal/layout` is where that path is decided, once). A new skill that writes
anything picks `specs/<its-own-name>/` and is added to the table in
`plugins/omnicore/shared/generated-documents.md`, which is the contract for both
halves of the rule: where documents live, and that they are committed rather than
ignored. Never `docs/` — that name belongs to the consumer project's own end-user
documentation.
