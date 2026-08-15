# omnicore-plugin — repo rules

The Claude Code plugin for the omnicore framework: 16 `/omnicore:*` skills under
`plugins/omnicore/skills/`, plus **omnicore-gen** — the spec-driven code
generator — as Go source under `plugins/omnicore/gen/`, launched by
`plugins/omnicore/bin/omnicore-gen` (on the session PATH).

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

## The gate

`plugins/omnicore/gen/scripts/golden.sh` — generate → gofmt → vet → build → the
generated tests → a real boot → DDL on five engines → the coverage matrix. It runs
against the generator's OWN bench (`gen/devops/docker-compose.yml`, containers
`omnicore-gen-*`), **never** a dev or QA one: the DDL lanes drop and recreate
tables, and borrowing someone's engines means a gate that passes here by breaking
work over there. It has happened; do not repeat it.

Green means green: `57 passed · 0 failed · 0 skipped`. A skipped lane is a lane
that proved nothing.

## Skills

The skills in `plugins/omnicore/skills/` are the single source of truth for
behaviour, and they are **version-agnostic**: they read the framework version from
the project's `go.mod` and treat that version's bundled `/docs` as the sole
authority. When a convention here and the framework's docs disagree, the docs win
and the convention is stale — fix it, and say so in `CHANGELOG.md`.
