#!/usr/bin/env bash
# golden.sh — the behavioural gate.
#
# A generator that drifts emits code that does not compile, which is worse than
# having no generator. Reading the output is not enough to catch that; this
# script proves it, end to end:
#
#   generate → gofmt → go vet → go build → apply the DDL to a REAL engine
#             → regenerate and prove nothing changed
#
# It is meant to run in CI, not only on a maintainer's laptop. The database
# lanes skip themselves (loudly) when an engine is not reachable, so the Go
# lanes still gate a machine without the bench.

set -uo pipefail

GEN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# The host is VENDORED, not borrowed from a sibling checkout: a gate that needs
# another repo on disk runs on one machine and is silently skipped everywhere else.
HOST="${HOST:-$GEN_DIR/testdata/host}"
WORK="${WORK:-/tmp/omnicore-gen-golden}"
# Relational fixtures: generated into the host that BOOTS, because the boot host
# is infra-free by design.
# campus comes FIRST: it is the TARGET of the student's read joins, and the
# generator resolves a join's columns and their types from the target's own
# spec. Generating the joining side before the target exists would be a lane
# that proves the refusal rather than the feature.
FIXTURES="${FIXTURES:-$GEN_DIR/testdata/specs/campus.omnicore.yaml $GEN_DIR/testdata/specs/student.omnicore.yaml $GEN_DIR/testdata/specs/teacher.omnicore.yaml $GEN_DIR/testdata/specs/role.omnicore.yaml}"
PRIMARY_FIXTURE="${PRIMARY_FIXTURE:-$GEN_DIR/testdata/specs/student.omnicore.yaml}"
# Mongo fixture: generated into a SEPARATE tree that is built and vetted but not
# booted. A Mongo-backed view aborts the boot without a mongo.uri, so mixing it
# into the boot host would cost the boot lane entirely.
MONGO_FIXTURE="${MONGO_FIXTURE:-$GEN_DIR/testdata/specs/course.omnicore.yaml}"
WORK_MONGO="${WORK_MONGO:-/tmp/omnicore-gen-golden-mongo}"
# SQLite with no transport: the whole build-and-boot half runs with no services
# at all, which is what lets it gate a pull request rather than a workstation.
ENGINE_TAGS="${ENGINE_TAGS:-sqlite}"

# shasum is a perl script and is not guaranteed on a minimal Linux image, while
# sha1sum is not on macOS. Picking one at startup keeps the gate from being
# "works on the machine it was written on" a second time.
if command -v shasum >/dev/null 2>&1; then
  SUM() { shasum "$@"; }
elif command -v sha1sum >/dev/null 2>&1; then
  SUM() { sha1sum "$@"; }
else
  SUM() { cksum "$@"; }
fi

# stage_host lays the vendored host down in $1, ready to be generated into.
#
# OMNICORE_LOCAL points the copy at a framework CHECKOUT instead of the pinned
# release. That is what makes this gate usable while the framework's next
# version is still unreleased: there is no tag to pin, so the only way to prove
# the emitters match the API is to build against the source. Unset — the normal
# case — nothing is rewritten and the host keeps the version its go.mod pins.
# The rewrite happens ONCE, into a staged template every lane then copies. The
# gate lays the host down about thirty times; resolving the module graph that
# many times would dominate its runtime for no added proof.
STAGED="${STAGED:-/tmp/omnicore-gen-host-staged}"
stage_host() {
  local dest="$1"
  if [[ -z "${OMNICORE_LOCAL:-}" ]]; then
    rm -rf "$dest"; mkdir -p "$dest"; cp -R "$HOST/." "$dest/"
    return 0
  fi
  if [[ ! -d "$STAGED" ]]; then
    mkdir -p "$STAGED"; cp -R "$HOST/." "$STAGED/"
    (cd "$STAGED" \
      && GOWORK=off go mod edit -replace "github.com/ClaudioSchirmer/omnicore=$OMNICORE_LOCAL" \
      && GOWORK=off go mod tidy) >/tmp/gg-local.log 2>&1 \
      || { echo "  ❌ could not point the host at $OMNICORE_LOCAL"; sed -n '1,20p' /tmp/gg-local.log; rm -rf "$STAGED"; exit 1; }
    echo "  (host staged against the framework checkout at $OMNICORE_LOCAL)"
  fi
  rm -rf "$dest"; mkdir -p "$dest"; cp -R "$STAGED/." "$dest/"
}

pass=0; fail=0; skip=0
ok()   { echo "  ✅ $1"; pass=$((pass+1)); }
bad()  { echo "  ❌ $1"; fail=$((fail+1)); }
skipf(){ echo "  ⏭  $1 (skipped: $2)"; skip=$((skip+1)); }

echo "═══ omnicore-gen golden gate ═══"

# The host pins a RELEASED framework. While the version this generator targets
# is still unreleased there is no tag to pin, and every lane that compiles
# generated code would be measuring the emitters against an API that predates
# them. Say so once, loudly, instead of letting a wall of red read as a
# generator defect.
WANTED=$(grep -oE 'Supported = "v[0-9.]+"' "$GEN_DIR/internal/compat/compat.go" | grep -oE 'v[0-9.]+')
PINNED=$(grep -oE 'ClaudioSchirmer/omnicore v[0-9.]+' "$HOST/go.mod" | head -1 | grep -oE 'v[0-9.]+')
if [[ -z "${OMNICORE_LOCAL:-}" && -n "$WANTED" && -n "$PINNED" && "$WANTED" != "$PINNED" ]]; then
  echo "  ⚠  the vendored host pins framework $PINNED and this generator targets $WANTED."
  echo "     Point the gate at a checkout — OMNICORE_LOCAL=/path/to/omnicore bash scripts/golden.sh —"
  echo "     or bump testdata/host/go.mod once $WANTED is published."
fi

# ── Lane 0: the generator's own tests ────────────────────────────────────────
echo "── unit tests"
if (cd "$GEN_DIR" && GOWORK=off go test ./... >/tmp/gg-test.log 2>&1); then
  ok "go test"
else
  bad "go test"; sed -n '1,25p' /tmp/gg-test.log
fi

# ── Lane 1: a fresh project, generated into ──────────────────────────────────
echo "── generate into a copy of the vendored host"
if [[ ! -d "$HOST" ]]; then
  echo "  vendored host missing at $HOST"
  exit 1
fi
stage_host "$WORK"
mkdir -p "$WORK/specs/omnicore-gen"
for f in $FIXTURES; do cp "$f" "$WORK/specs/omnicore-gen/"; done
SPEC="$WORK/specs/omnicore-gen/$(basename "$PRIMARY_FIXTURE")"

for f in $FIXTURES; do
  target="$WORK/specs/omnicore-gen/$(basename "$f")"
  if (cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate -spec "$target" -project "$WORK" >/tmp/gg-gen.log 2>&1); then
    ok "generate $(basename "$f" .omnicore.yaml)"
  else
    bad "generate $(basename "$f" .omnicore.yaml)"; cat /tmp/gg-gen.log; exit 1
  fi
done

# ── Lane 2: the generated tree is formatted, vets and compiles ───────────────
echo "── the generated tree"
# Only the GENERATED files are checked: the reference service carries its own
# pre-existing formatting, and failing on that would say nothing about the
# generator.
GENERATED=$(cd "$WORK" && grep -rl "Code generated by omnicore-gen" --include='*.go' . 2>/dev/null)
UNFORMATTED=$(cd "$WORK" && [[ -n "$GENERATED" ]] && gofmt -l $GENERATED 2>/dev/null)
if [[ -z "$UNFORMATTED" ]]; then ok "gofmt -l is empty on every generated file"; else bad "gofmt: $UNFORMATTED"; fi

if (cd "$WORK" && GOWORK=off go build -tags "$ENGINE_TAGS" ./... >/tmp/gg-build.log 2>&1); then
  ok "go build -tags '$ENGINE_TAGS'"
else
  bad "go build"; sed -n '1,25p' /tmp/gg-build.log
fi

if (cd "$WORK" && GOWORK=off go vet -tags "$ENGINE_TAGS" ./... >/tmp/gg-vet.log 2>&1); then
  ok "go vet -tags '$ENGINE_TAGS'"
else
  bad "go vet"; sed -n '1,25p' /tmp/gg-vet.log
fi

# ── Lane 2b: the GENERATED tests pass ────────────────────────────────────────
# Emitting tests proves nothing until they run: a generated test that does not
# compile, or that asserts the wrong thing, is worse than none.
if (cd "$WORK" && GOWORK=off go test -tags "$ENGINE_TAGS" \
      ./internal/domain/... ./internal/application/... >/tmp/gg-gentest.log 2>&1); then
  ok "the generated tests pass"
else
  bad "the generated tests fail"; sed -n '1,30p' /tmp/gg-gentest.log
fi

# ── Lane 2c: the Mongo-backed shape compiles ─────────────────────────────────
# Its own tree, its own build: indexes, text search and the projection options
# only exist on that backing, and nothing else here would compile them.
echo "── the mongo-backed shape"
stage_host "$WORK_MONGO"
mkdir -p "$WORK_MONGO/specs/omnicore-gen"; cp "$MONGO_FIXTURE" "$WORK_MONGO/specs/omnicore-gen/"
if (cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate \
      -spec "$WORK_MONGO/specs/omnicore-gen/$(basename "$MONGO_FIXTURE")" -project "$WORK_MONGO" \
      >/tmp/gg-mongo.log 2>&1); then
  ok "generate $(basename "$MONGO_FIXTURE" .omnicore.yaml)"
else
  bad "generate $(basename "$MONGO_FIXTURE" .omnicore.yaml)"; sed -n '1,20p' /tmp/gg-mongo.log
fi
if (cd "$WORK_MONGO" && GOWORK=off go build -tags "$ENGINE_TAGS" ./... >/tmp/gg-mongobuild.log 2>&1); then
  ok "the mongo-backed tree builds"
else
  bad "the mongo-backed tree does not build"; sed -n '1,20p' /tmp/gg-mongobuild.log
fi
echo "  (not booted: a mongo-backed view needs a Mongo, and the boot host is infra-free)"

# ── Lane 3: regeneration is a no-op ──────────────────────────────────────────
#
# Checked TWICE, and the second check is the one that earns its place. Comparing
# the tree before and after a full sweep only proves the END STATE matches — and
# it does even when every entity rewrites a file the previous one just wrote, as
# long as the sweep runs in the same order both times. That is exactly what a
# shared file attributed to one entity did: internal/domain/vos/doc.go named
# whichever spec ran last, so each generation reported the others' copy as
# updated and the working tree was never clean. So each run is also required to
# report NOTHING written, which is the promise a CI check actually needs.
echo "── regeneration"
BEFORE=$(cd "$WORK" && find internal bootstrap migrations -type f | sort | while read -r f; do SUM "$f"; done | SUM)
churn=""; churn_spec=""
for f in $FIXTURES; do
  target="$WORK/specs/omnicore-gen/$(basename "$f")"
  (cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate -spec "$target" -project "$WORK" >/tmp/gg-regen.log 2>&1)
  wrote=$(awk '/^  (created|updated) +[0-9]+$/ {n += $2} END {print n+0}' /tmp/gg-regen.log)
  if [[ "$wrote" != "0" ]]; then
    churn="$churn $(basename "$f" .omnicore.yaml):$wrote"
    [[ -z "$churn_spec" ]] && churn_spec="$target"
  fi
done
AFTER=$(cd "$WORK" && find internal bootstrap migrations -type f | sort | while read -r f; do SUM "$f"; done | SUM)
if [[ "$BEFORE" == "$AFTER" ]]; then
  ok "a second run changes nothing"
else
  bad "regeneration is not idempotent"
fi
if [[ -z "$churn" ]]; then
  ok "no entity rewrites another's files"
else
  bad "regenerating one entity still writes files:$churn"
  (cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate -spec "$churn_spec" -project "$WORK" -dry-run 2>&1 \
     | grep -E '^  (create|update) ' | head -5)
fi

# ── Lane 4: a hand edit is refused, never clobbered ──────────────────────────
HOOK="$WORK/internal/domain/student_rules.go"
OWNED="$WORK/internal/domain/student.go"
echo "// hand written" >> "$OWNED"
(cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate -spec "$SPEC" -project "$WORK" >/tmp/gg-refuse.log 2>&1)
if grep -q "hand written" "$OWNED"; then
  ok "a hand-edited file is left alone"
else
  bad "a hand edit was clobbered"
fi
if grep -q "REFUSED" /tmp/gg-refuse.log; then
  ok "the refusal is reported"
else
  bad "the refusal was not reported"
fi

# ── Lane 4a: doctor SEES the drift, and adopt is the way out ─────────────────
#
# The refusal above is what a regeneration does; doctor is what a developer runs
# to find out BEFORE regenerating, and adopt is the declared way to keep an edit
# instead of losing it. Neither had a lane, so the pair that decides whether a
# hand edit is recoverable was proven by nobody. The tree here is join-bearing,
# which is the point: both commands walk the lock and the file checksums, and a
# read join adds files and fields to both.
DOC=$(cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen doctor -project "$WORK" 2>&1)
if grep -q "was edited by hand" <<<"$DOC"; then
  ok "doctor reports the hand edit"
else
  bad "doctor did not see a hand-edited file — $(tr '\n' ' ' <<<"$DOC" | head -c 200)"
fi
(cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen adopt "$OWNED" -project "$WORK" >/tmp/gg-adopt.log 2>&1)
DOC2=$(cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen doctor -project "$WORK" 2>&1)
if grep -q "carries a hand edit adopted" <<<"$DOC2"; then
  ok "adopt turns the refusal into a recorded exception"
else
  bad "doctor does not report the file as adopted — $(tr '\n' ' ' <<<"$DOC2" | head -c 200)"
fi
# The whole promise of adopt: the NEXT regeneration preserves the edit instead of
# refusing the run. Reporting it and honoring it are two different things.
(cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate -spec "$SPEC" -project "$WORK" >/tmp/gg-postadopt.log 2>&1)
if grep -q "hand written" "$OWNED"; then
  ok "a regeneration after adopt preserves the edit"
else
  bad "adopt was recorded and the next generation clobbered the file anyway"
fi

# ── Lane 4b: the generated service BOOTS and serves ──────────────────────────
# Compiling proves the code is well formed; only booting proves the framework
# accepts it. Most of what this generator can get wrong — a schema that does not
# bind, a view whose version is stale, a route with no permission — is refused
# at boot and is invisible to the compiler.
echo "── boot"
BOOT_DIR="$WORK"
# ABSOLUTE, both here and in the DSN below. A relative "file:./golden.db"
# resolves against whatever the process's working directory turns out to be, and
# when that is not the directory this line cleans, the boot lane runs against a
# database that accumulates across runs — which is how a write assertion starts
# colliding with a row some earlier run left behind.
BOOT_DB="$BOOT_DIR/golden.db"
rm -f "$BOOT_DB" "$BOOT_DB-wal" "$BOOT_DB-shm"

# A previous run that did not shut down cleanly leaves its service holding the
# port. The new one then fails to bind, and every assertion below is answered by
# the OLD binary — a boot lane that proves yesterday's code still works. It is
# invisible: the service answers, readiness answers, the listing answers.
#
# Only this gate's own binary is ever killed, by path, so nothing else on the
# machine is touched.
STALE=$(lsof -nP -iTCP:18099 -sTCP:LISTEN -t 2>/dev/null || true)
for pid in $STALE; do
  if [[ "$(ps -o comm= -p "$pid" 2>/dev/null)" == */gg-host ]]; then
    kill "$pid" 2>/dev/null && say_killed=1
  fi
done
if [[ -n "${say_killed:-}" ]]; then
  echo "  (a previous run's service was still holding :18099 — killed before booting)"
  sleep 1
fi
if lsof -nP -iTCP:18099 -sTCP:LISTEN -t >/dev/null 2>&1; then
  bad "port 18099 is held by something this gate did not start — the boot lane would test it instead"
fi
(cd "$BOOT_DIR" && GOWORK=off go build -tags sqlite -o /tmp/gg-host ./bootstrap) >/tmp/gg-bootbuild.log 2>&1
if [[ ! -x /tmp/gg-host ]]; then
  bad "the service did not build"; sed -n '1,20p' /tmp/gg-bootbuild.log
else
  (cd "$BOOT_DIR" && APP_PROFILE=dev HTTP_ADDR=:18099 \
     MIGRATIONS_DIR="$BOOT_DIR/migrations/sqlite" DATABASE_URL="file:$BOOT_DB" \
     /tmp/gg-host >/tmp/gg-boot.log 2>&1) &
  BOOT_PID=$!
  READY=0
  for _ in $(seq 1 40); do
    if curl -fsS "http://127.0.0.1:18099/livez" >/dev/null 2>&1; then READY=1; break; fi
    if ! kill -0 $BOOT_PID 2>/dev/null; then break; fi
    sleep 0.5
  done
  if [[ $READY -eq 1 ]]; then
    ok "the generated service boots"
    if curl -fsS "http://127.0.0.1:18099/readyz" >/dev/null 2>&1; then
      ok "it reports ready"
    else
      bad "it booted but never became ready"
    fi
    # A 200 on an empty listing is the cheapest end-to-end proof that the schema,
    # the view, the query and the route all agree — and that the feature actually
    # reached the composition root, which nothing else here would catch.
    #
    # The RELATIONAL entity is the one probed: this host has no Mongo, so a
    # Mongo-backed view has nothing to serve from. Mongo-backed generation is
    # still covered by the build, vet and DDL lanes — it is the runtime
    # projection that is out of reach without a container, and saying so beats
    # quietly probing whichever entity happens to work.
    if curl -fsS "http://127.0.0.1:18099/teachers" >/dev/null 2>&1; then
      ok "the generated listing answers (relational-backed entity)"
    else
      bad "the generated listing does not answer"; sed -n '1,25p' /tmp/gg-boot.log
    fi
    # The by-id read and the listing must return the SAME record, in the same
    # shape. They are written by two different emitters, and the one that drifted
    # dropped the collections and the facet's fields from by-id alone: opening a
    # record showed LESS of it than the list it was opened from, and no second
    # request would have filled the gap, because the document was already there.
    # The student declares an INNER read join onto a campus, so the row it
    # points at has to exist before the student does — an inner join drops the
    # aggregate from EVERY read otherwise, FindByID included, and the write
    # below would come back 404 on its own record.
    CAMPUS_ID=$(curl -fsS -X POST "http://127.0.0.1:18099/campi" \
      -H 'Content-Type: application/json' \
      -d '{"campusLabel":"Campus Norte","code":"NOR","budgetCode":"ORC-2026-11","ownerID":"1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4"}' \
      2>/dev/null | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])' 2>/dev/null)
    NEW_ID=$(curl -fsS -X POST "http://127.0.0.1:18099/students" \
      -H 'Content-Type: application/json' \
      -d '{"name":"Ana Paula","enrollmentNumber":"2026-09999","email":"ana@escola.br","status":"active","grade":8.5,"enrolledAt":"2026-02-01T09:00:00Z","tuitionAmount":185000,"tuitionCurrency":"BRL","leaveFrom":"2026-06-01T00:00:00Z","leaveTo":"2026-08-01T00:00:00Z","scholarshipSponsor":"Fundacao Alfa","scholarshipPercentage":50,"campusId":"'"$CAMPUS_ID"'","guardians":[{"fullName":"Marcos","document":"123.456.789-00","phone":"11999999999"}]}' \
      2>/dev/null | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])' 2>/dev/null)
    if [[ -n "$NEW_ID" ]]; then
      SHAPES=$(python3 - "$NEW_ID" <<'PYSHAPE'
import json, sys, urllib.request

entry_id = sys.argv[1]
base = "http://127.0.0.1:18099/students"


def keys(url):
    with urllib.request.urlopen(url) as fh:
        return json.load(fh)["data"]


listed = [r for r in keys(base) if r["id"] == entry_id]
if not listed:
    print("the record just written is not in the listing")
    raise SystemExit

one = keys(base + "/" + entry_id)
missing = sorted(set(listed[0]) - set(one))
if missing:
    print("by-id is missing what the listing returns: " + ", ".join(missing))
PYSHAPE
)
      if [[ -z "$SHAPES" ]]; then
        ok "the by-id read returns the same record as the listing"
      else
        bad "by-id and the listing disagree — $SHAPES"
      fi

      # READ JOINS, end to end. The declaration lives on the repository and
      # NOTHING else was told about it — not the TableSchema, not the view — so
      # the only proof that the reach actually reaches is the served document.
      #
      # Three assertions, and each one is a different half of the contract: the
      # value crossed the foreign key at all; a field marked hidden is on the
      # entity and OFF the wire; and a LEFT join with no counterpart answers an
      # ABSENCE rather than the zero value.
      JOINS=$(python3 - "$NEW_ID" <<'PYJOIN'
import json, sys, urllib.request

entry_id = sys.argv[1]
bad = []
with urllib.request.urlopen("http://127.0.0.1:18099/students/" + entry_id) as fh:
    one = json.load(fh)["data"]

if one.get("campusName") != "Campus Norte":
    bad.append("the inner join did not serve campusName: %r" % one.get("campusName"))

# An IDENTITY column crossing a join. A join field carries no domain type, so it
# lands as the canonical TEXT — which is also the only shape correct on every
# engine, since three of the four store an id as raw bytes. A mandatory column
# arrives as a value; a nullable one the campus left unset arrives as an absence.
if one.get("campusOwnerID") != "1f6e6ac6-2a1e-4c22-9c0a-2b7a9c5f21d4":
    bad.append("a joined identity column did not arrive as its text: %r"
               % one.get("campusOwnerID"))
if one.get("campusAuditorID") is not None:
    bad.append("a nullable identity the target left unset must be an absence, got %r"
               % one.get("campusAuditorID"))
if "campusBudgetCode" in one:
    bad.append("a hidden join field reached the wire")
for g in one.get("guardians") or []:
    if g.get("guardianCampusName") is not None:
        bad.append("a left join with no counterpart served %r, not an absence"
                   % g.get("guardianCampusName"))

# A ROOT join's field is addressable in a criteria — it rides the root SELECT.
with urllib.request.urlopen(
        "http://127.0.0.1:18099/students?campusName=Campus%20Norte&orderBy=campusName") as fh:
    rows = json.load(fh)["data"]
if not any(r["id"] == entry_id for r in rows):
    bad.append("filtering and ordering by the joined field did not find the row")

print("; ".join(bad))
PYJOIN
)
      if [[ -z "$JOINS" ]]; then
        ok "the read joins serve, hide and filter as declared"
      else
        bad "the read joins did not behave as declared — $JOINS"
      fi

      # A PER-ENTRY computed field, on the served document. Building proves the
      # loop compiles; only this proves it RAN and wrote back through the index,
      # on both read shapes — the by-id Result holds values while the entry it
      # nests holds pointers, and a seat that read the root's discipline instead
      # of the entry's is a tree that does not build at all.
      #
      # The body is a hook nobody has written, so the VALUE is deliberately not
      # asserted: what is under test is the seat, and the key being present on
      # every entry is exactly the seat having run once per entry.
      PERENTRY=$(python3 - "$NEW_ID" <<'PYENTRY'
import json, sys, urllib.request

entry_id = sys.argv[1]
base = "http://127.0.0.1:18099/students"
bad = []


def data(url):
    with urllib.request.urlopen(url) as fh:
        return json.load(fh)["data"]


for label, doc in (("by-id", data(base + "/" + entry_id)),
                   ("the listing", next(r for r in data(base) if r["id"] == entry_id))):
    entries = doc.get("guardians") or []
    if not entries:
        bad.append("%s served no collection to derive into" % label)
        continue
    for g in entries:
        if "label" not in g:
            bad.append("%s: the per-entry derivation did not run for %r"
                       % (label, g.get("fullName")))

# ?fields= over the derived field must fetch its SOURCES, under the entry's own
# segment — there is no column behind the name itself.
rows = data(base + "?fields=id,guardians.label")
row = next(r for r in rows if r["id"] == entry_id)
for g in row.get("guardians") or []:
    if "label" not in g:
        bad.append("?fields= on the derived field did not serve it")

print("; ".join(bad))
PYENTRY
)
      if [[ -z "$PERENTRY" ]]; then
        ok "a per-entry computed field is derived on every entry, on both reads"
      else
        bad "the per-entry derivation did not run — $PERENTRY"
      fi

      # The TABULAR EXPORT is a third rendering of the same read, written by its
      # own emitter, and a joined field reaches it through the labelKey like any
      # other column. It is the surface where a missing header shows up as an
      # internal name rather than as an error, so it is asserted rather than
      # assumed — headers AND values, plus the hidden field's absence, which is
      # the half a "does it contain the value" check would miss.
      CSV=$(curl -fsS "http://127.0.0.1:18099/students.csv" 2>/dev/null)
      CSVHEAD=$(head -1 <<<"$CSV")
      CSVROW=$(sed -n '2p' <<<"$CSV")
      csvbad=""
      for col in "Campus Name" "Campus Owner ID" "Campus Latitude" "Campus Nickname"; do
        grep -q "$col" <<<"$CSVHEAD" || csvbad="$csvbad; no header for $col"
      done
      grep -q "Campus Norte" <<<"$CSVROW" || csvbad="$csvbad; the joined value is not in the row"
      grep -q "Campus Budget Code" <<<"$CSVHEAD" && csvbad="$csvbad; a hidden join field reached the export"
      # The ROOT's derived column heads the export under its labelKey — that half
      # of the contract is real and asserted.
      grep -q "Enrollment Label" <<<"$CSVHEAD" || csvbad="$csvbad; the root's derived column has no header"
      # A COLLECTION's does not, and neither does any stored field of one: a
      # tabular row is FLAT. Pinned because it is a claim that rots — the natural
      # sentence to write about a derived field is "every surface renders it",
      # and for the per-entry seat that sentence is false.
      grep -q "Guardian Full Name" <<<"$CSVHEAD" && csvbad="$csvbad; a collection's stored field reached the flat export"
      grep -qi "guardian.*label" <<<"$CSVHEAD" && csvbad="$csvbad; a collection's derived field reached the flat export"
      if [[ -z "$csvbad" ]]; then
        ok "the CSV export carries the joined columns, headed and without the hidden one"
      else
        bad "the CSV export disagrees with the read${csvbad}"
      fi

      # The aggregate id as a QUERY path. It is declared by no fields[] entry and
      # by no read.managed name — the framework's managed carrier owns it — so
      # the generator resolves it on its own and the only thing that proves the
      # resolution was right is the framework answering. A leaf that compiles and
      # binds nothing looks identical from the outside up to here: the endpoint
      # is there, the tag is there, and every request quietly comes back
      # unfiltered.
      #
      # Both shapes are probed because they are emitted by different branches:
      # student filters AND orders by the id (the tag rides on the filter leaf),
      # teacher only orders by it (a vocabulary leaf carrying no value on the
      # wire, which is the branch that emits a Go field nothing ever binds).
      IDQ=$(python3 - "$NEW_ID" <<'PYID'
import json, sys, urllib.error, urllib.request

entry_id = sys.argv[1]
bad = []


def rows(url):
    try:
        with urllib.request.urlopen(url) as fh:
            return json.load(fh)["data"]
    except urllib.error.HTTPError as e:
        bad.append("%s answered %s" % (url, e.code))
        return None


ordered = rows("http://127.0.0.1:18099/students?orderBy=id")
if ordered is not None:
    ids = [r["id"] for r in ordered]
    if entry_id not in ids:
        bad.append("?orderBy=id dropped the record")
    elif ids != sorted(ids):
        bad.append("?orderBy=id did not order by the id: " + ", ".join(ids))

eq = rows("http://127.0.0.1:18099/students?id=" + entry_id)
if eq is not None and [r["id"] for r in eq] != [entry_id]:
    bad.append("?id= did not narrow to the one row: %d row(s)" % len(eq))

inlist = rows("http://127.0.0.1:18099/students?id.in=" + entry_id)
if inlist is not None and [r["id"] for r in inlist] != [entry_id]:
    bad.append("?id.in= did not narrow to the one row: %d row(s)" % len(inlist))

# The vocabulary-only leaf. An empty listing is a fine answer — what is being
# proved is that the framework ACCEPTS the token, and an undeclared one is a
# typed 400, so a 200 here is the whole assertion.
if rows("http://127.0.0.1:18099/teachers?orderBy=id") is None:
    bad.append("the sort-only id leaf refused ?orderBy=id")

print("; ".join(bad))
PYID
)
      if [[ -z "$IDQ" ]]; then
        ok "the aggregate id filters and orders the listing (?id=, ?id.in=, ?orderBy=id)"
      else
        bad "the id is declared as a query path and does not answer as one — $IDQ"
      fi
    else
      bad "could not write a student to compare the two reads"
    fi

    # The TENANT-SCOPED entity, at runtime. Two things are proved here that no
    # compile lane can see.
    #
    # First, that a scoped entity is usable on a bench at all: with auth.mode
    # disabled there is no identity, so under the default policy the write guard
    # refuses every write and the listing answers empty. `noIdentity: stand-down`
    # is what makes the bench work, and this is where that is checked rather
    # than assumed.
    #
    # Second, that the per-tenant unique index and its binding agree: the SECOND
    # write of the same handle in the same tenant must come back 409, not 500 and
    # not 201. That is exactly the pair that used to disagree — a per-tenant
    # pre-check behind a global index — and the disagreement was invisible until
    # a second tenant hit it.
    ROLE_ID=$(curl -fsS -X POST "http://127.0.0.1:18099/roles" \
      -H 'Content-Type: application/json' \
      -d '{"tenantID":"9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3","key":"administrator","name":"Administrator","permissions":[{"permissionID":"3b7c1a44-2f90-4d17-9e55-8c1d6f2a0b31","note":"initial grant"}]}' \
      2>/dev/null | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])' 2>/dev/null)
    if [[ -n "$ROLE_ID" ]]; then
      ok "a tenant-scoped entity accepts a write on a bench with no identity (noIdentity: stand-down)"
      DUP=$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:18099/roles" \
        -H 'Content-Type: application/json' \
        -d '{"tenantID":"9f14b0a2-6d38-4c5e-b7a1-2e0c5d81f4a3","key":"administrator","name":"Duplicate","permissions":[]}')
      if [[ "$DUP" == "409" ]]; then
        ok "the per-tenant unique key answers 409 on a duplicate handle"
      else
        bad "a duplicate handle in the same tenant answered $DUP, not 409 — the index and the precheck disagree"
      fi
      # The SAME handle in ANOTHER tenant is a different row and must be
      # accepted. This is the half the old global index got wrong: it refused
      # tenant B the handle only tenant A held, under a notification saying the
      # handle was taken — for a tenant where it was free.
      OTHER=$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:18099/roles" \
        -H 'Content-Type: application/json' \
        -d '{"tenantID":"11111111-2222-4333-8444-555555555555","key":"administrator","name":"Administrator","permissions":[]}')
      if [[ "$OTHER" == "201" || "$OTHER" == "200" ]]; then
        ok "the same handle in ANOTHER tenant is accepted (unique.within scopes the index)"
      else
        bad "another tenant was refused the handle it does not share — answered $OTHER"
      fi
    else
      bad "the tenant-scoped entity refused a write on the bench, or did not answer with an id"
      sed -n '1,25p' /tmp/gg-boot.log
    fi

    # ── the guard barrier, at runtime ────────────────────────────────────────
    #
    # `guard: true` on enrollment-required. Its whole effect is the SHAPE of a
    # 422, which no unit test on the generated file can see: the barrier is
    # positional, so a write that gets three things wrong must name the two
    # DECLARED ABOVE the barrier and stay silent about the one below it.
    #
    # Both halves are asserted. Only checking that the pass stopped would pass
    # just as happily on a barrier emitted too early, which would swallow
    # name-required as well and hand the caller one field out of three.
    GUARD=$(curl -s -X POST "http://127.0.0.1:18099/students" \
      -H 'Content-Type: application/json' \
      -d '{"name":"","enrollmentNumber":"","email":"ana@escola.br","status":"active","grade":99,"enrolledAt":"2026-02-01T09:00:00Z","tuitionAmount":185000,"tuitionCurrency":"BRL","campusId":"'"$CAMPUS_ID"'","guardians":[]}')
    GUARD_FIELDS=$(python3 -c '
import json, sys
try:
    body = json.loads(sys.stdin.read())
except Exception:
    sys.exit(0)
out = []
def walk(node):
    if isinstance(node, dict):
        for k, v in node.items():
            if k in ("field", "fieldName") and isinstance(v, str):
                out.append(v)
            walk(v)
    elif isinstance(node, list):
        for v in node:
            walk(v)
walk(body)
print(" ".join(sorted(set(out))))
' <<<"$GUARD")
    if grep -qw "name" <<<"$GUARD_FIELDS" && grep -qw "enrollmentNumber" <<<"$GUARD_FIELDS"; then
      ok "a guard reports every rule declared above it (got: $GUARD_FIELDS)"
    else
      bad "a guard swallowed a rule declared ABOVE the barrier (got: ${GUARD_FIELDS:-nothing})"
    fi
    if grep -qw "grade" <<<"$GUARD_FIELDS"; then
      bad "the barrier did not stop the pass — a rule below it still reported (got: $GUARD_FIELDS)"
    else
      ok "a guard stops every rule declared below it"
    fi

    OPENAPI=$(curl -fsS "http://127.0.0.1:18099/openapi.json")
    if grep -q "teachers" <<<"$OPENAPI" && grep -q "students" <<<"$OPENAPI" && grep -q "roles" <<<"$OPENAPI"; then
      ok "every entity appears in the OpenAPI document"
    else
      bad "an entity is missing from the OpenAPI document"
    fi
  else
    bad "the service never came up"; sed -n '1,30p' /tmp/gg-boot.log
  fi
  # Kill the BINARY, not the subshell that launched it. `( … ) &` puts the
  # subshell's pid in $!, and killing that leaves the service running — which is
  # how a run leaks its host, and how the NEXT run ends up asserting against the
  # previous build while every check answers happily.
  kill $BOOT_PID 2>/dev/null
  wait $BOOT_PID 2>/dev/null
  pkill -f '^/tmp/gg-host$' 2>/dev/null
  for _ in $(seq 1 20); do
    lsof -nP -iTCP:18099 -sTCP:LISTEN -t >/dev/null 2>&1 || break
    sleep 0.2
  done
fi

# ── Lane 5: the DDL applies to real engines ──────────────────────────────────
#
# Against the GENERATOR'S OWN bench (devops/docker-compose.yml), never a dev or
# QA one. These lanes drop and recreate tables; the engines a developer keeps
# running are the ones their service and their QA suite are using right now, and
# borrowing them means a gate that passes here by breaking work over there.
#
#   docker compose -f devops/docker-compose.yml up -d              # pg + mysql
#   docker compose -f devops/docker-compose.yml --profile heavy up -d
#
# Whatever is not up SKIPS, loudly. Point the vars at something else only if you
# know what you are pointing them at.
echo "── DDL against real engines"

PG_CONTAINER="${PG_CONTAINER:-omnicore-gen-postgres}"
MYSQL_CONTAINER="${MYSQL_CONTAINER:-omnicore-gen-mysql}"
SQLSERVER_CONTAINER="${SQLSERVER_CONTAINER:-omnicore-gen-sqlserver}"
ORACLE_CONTAINER="${ORACLE_CONTAINER:-omnicore-gen-oracle}"
DDL_DB="${DDL_DB:-gen_ddl}"

apply_pg() {
  local f=$1
  docker exec -i "$PG_CONTAINER" psql -U omnicore -d "$DDL_DB" -v ON_ERROR_STOP=1 -q < "$f" >/dev/null 2>&1
}
apply_mysql() {
  local f=$1 out
  out=$(docker exec -i "$MYSQL_CONTAINER" mysql -uroot -proot "$DDL_DB" < "$f" 2>&1 | grep -v 'Using a password')
  [[ -z "$out" ]]
}

ddl_lane() {
  local engine=$1 applyfn=$2 container=$3
  if ! docker ps --format '{{.Names}}' 2>/dev/null | grep -q "$container"; then
    skipf "$engine DDL" "container not running"; return
  fi
  # Down first, in reverse: a rerun must not trip over what a previous one left.
  local ups=() f
  for f in "$WORK"/migrations/$engine/*.up.sql; do
    grep -q "omnicore-gen" "$f" 2>/dev/null && ups+=("$f")
  done
  local i
  for ((i=${#ups[@]}-1; i>=0; i--)); do $applyfn "${ups[$i]%.up.sql}.down.sql" >/dev/null 2>&1; done
  local okall=1
  for f in "${ups[@]}"; do $applyfn "$f" || okall=0; done
  [[ $okall -eq 1 ]] && ok "$engine: up applies (${#ups[@]} migration(s))" || bad "$engine: up rejected"
  okall=1
  for ((i=${#ups[@]}-1; i>=0; i--)); do $applyfn "${ups[$i]%.up.sql}.down.sql" || okall=0; done
  [[ $okall -eq 1 ]] && ok "$engine: down applies" || bad "$engine: down rejected"
  okall=1
  for ((i=${#ups[@]}-1; i>=0; i--)); do $applyfn "${ups[$i]%.up.sql}.down.sql" || okall=0; done
  [[ $okall -eq 1 ]] && ok "$engine: down is idempotent" || bad "$engine: down is not idempotent"
}

ddl_lane postgres apply_pg "$PG_CONTAINER"

ddl_lane mysql apply_mysql "$MYSQL_CONTAINER"

apply_sqlserver() {
  local f=$1
  docker exec -i "$SQLSERVER_CONTAINER" /opt/mssql-tools18/bin/sqlcmd \
    -S localhost -U sa -P "${SQLSERVER_PASSWORD:-OmnicoreGen!2026}" -C -d master -b -i /dev/stdin < "$f" >/dev/null 2>&1
}
ddl_lane sqlserver apply_sqlserver "$SQLSERVER_CONTAINER"

apply_oracle() {
  local f=$1 out
  # Oracle's client returns 0 even on a rejected statement, so the transcript is
  # what decides: any ORA- line means the DDL did not apply.
  out=$(docker exec -i "$ORACLE_CONTAINER" bash -lc \
    "sqlplus -S omnicore/omnicore@//localhost:1521/FREEPDB1 <<'EOSQL'
WHENEVER SQLERROR EXIT SQL.SQLCODE
$(cat "$f")
EXIT
EOSQL" 2>&1)
  ! grep -q 'ORA-' <<<"$out"
}
ddl_lane oracle apply_oracle "$ORACLE_CONTAINER"

if command -v sqlite3 >/dev/null 2>&1; then
  # The stem carries a _manual suffix: migrations are written once and handed
  # over, so the name says so. Globbing the exact old name silently found
  # nothing and reported it as rejected DDL.
  UP_LITE=$(ls "$WORK"/migrations/sqlite/*_student*.up.sql 2>/dev/null | head -1)
  DOWN_LITE=${UP_LITE%.up.sql}.down.sql
  DB=$(mktemp -u).db
  if [[ -n "$UP_LITE" ]] && sqlite3 "$DB" < "$UP_LITE" 2>/dev/null; then ok "sqlite: up applies"; else bad "sqlite: up rejected"; fi
  if sqlite3 "$DB" < "$DOWN_LITE" 2>/dev/null; then ok "sqlite: down applies"; else bad "sqlite: down rejected"; fi
  rm -f "$DB"
else
  skipf "sqlite DDL" "sqlite3 not installed"
fi

# ── prune: the only command that DELETES ─────────────────────────────────────
#
# Two entities, one shared value object, then the declaring spec drops it. Every
# measure the generator has — its own file set, its own lock — calls that file an
# orphan, while the OTHER entity still has it as a field type. This lane exists
# because the answer was wrong until it was tested: prune offered the file, and
# applying it left a project that did not compile.
#
# It also covers the ordinary half in the same run: an orphaned collection's
# files and its labelKeys in all seven catalogs go, the tree still builds, and a
# second prune finds nothing.
echo "── prune"
PRUNE_WORK="${PRUNE_WORK:-/tmp/omnicore-gen-prune}"
stage_host "$PRUNE_WORK"
mkdir -p "$PRUNE_WORK/specs/omnicore-gen"
cp "$GEN_DIR/internal/cli/example.omnicore.yaml" "$PRUNE_WORK/specs/omnicore-gen/student.omnicore.yaml"
cp "$GEN_DIR/testdata/specs/prune-neighbour.omnicore.yaml" "$PRUNE_WORK/specs/omnicore-gen/"
# The hand-written half of the value objects the generator deliberately does not
# write. The example declares one of each: NationalID as `kind: manual` — a
# check-digit rule no regex states — and TaxID as a composite with `written:
# manual`, whose parts stay declared (the schema decomposes them, the mappers
# fold them) while the FILE is the author's. Neither tree compiles until these
# exist. Writing them here is what makes this lane prove the whole bargain: the
# generator types the field as the value object, a human supplies the type, and
# prune must never offer to delete a file the generator did not write.
#
# The composite half also pins the shape the report asks for: a struct whose
# FIELD NAMES are what the command mappers write into, and no Value() — its
# absence is what tells the framework to decompose the value across columns.
mkdir -p "$PRUNE_WORK/internal/domain/vos"
cat > "$PRUNE_WORK/internal/domain/vos/national_id.go" <<'VOEOF'
package vos

import "github.com/ClaudioSchirmer/omnicore/domain"

// NationalID is a national identity document number, valid by its own check
// digits. Hand-written on purpose: the rule is an algorithm over the digits.
type NationalID string

// Value unwraps to the underlying type, which is what the wire and the database see.
func (v NationalID) Value() string { return string(v) }

// IsValid is the framework's entry point, found by TYPE with no registration.
func (v NationalID) IsValid(fieldName string, ctx *domain.NotificationContext) bool {
	if len(v) == 0 {
		return true // absence is the required rule's business, not this one's
	}
	var digits int
	for _, r := range v {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if digits != 11 {
		ctx.AddNotification(fieldName, domain.SchemaViolationNotification{})
		return false
	}
	return true
}
VOEOF
cat > "$PRUNE_WORK/internal/domain/vos/tax_id.go" <<'VOEOF'
package vos

import (
	"strings"

	"github.com/ClaudioSchirmer/omnicore/domain"
)

// TaxID is a tax identification number together with the country that issued
// it. Hand-written on purpose (written: manual): the number is checked by the
// ISSUING country's algorithm, so one part's format depends on another part's
// VALUE — not a regex, not a range, not a comparison.
type TaxID struct {
	Country string `labelKey:"TaxIDCountryField"`
	Number  string `labelKey:"TaxIDNumberField"`
}

// IsValid is the framework's entry point, found by TYPE with no registration.
// There is deliberately no Value(): its absence is what tells the framework this
// value spans SEVERAL columns and has to be decomposed.
func (v TaxID) IsValid(fieldName string, ctx *domain.NotificationContext) bool {
	digits := map[string]int{"BR": 11, "PT": 9}
	want, known := digits[v.Country]
	if !known {
		ctx.AddNotification("Country", domain.SchemaViolationNotification{})
		return false
	}
	if len(v.Number) != want {
		ctx.AddNotification("Number", domain.SchemaViolationNotification{})
		return false
	}
	return true
}

// String renders the concept. A composite may expose a rendering under any name
// but Value(), and this is the other half of what written: manual buys.
func (v TaxID) String() string { return strings.ToUpper(v.Country) + ":" + v.Number }
VOEOF

# The hand-written half of a READ JOIN's TARGET.
#
# The example reaches into a Campus this project does not generate — which is
# the shape a service that adopted the generator midway has by definition, and
# the one the language accepts on the author's word: each mapped field states
# its own type, and the generated repository calls schemas.CampusSchema()
# because that is the project's own convention. This lane is what proves the
# bargain compiles rather than merely validates. Name the schema differently and
# the build says so at once, which is the honest failure the design promises.
mkdir -p "$PRUNE_WORK/internal/domain" "$PRUNE_WORK/internal/infra/schemas"
cat > "$PRUNE_WORK/internal/domain/campus.go" <<'CAMPUSEOF'
package domain

import "github.com/ClaudioSchirmer/omnicore/domain"

// Campus is hand-written on purpose: it is what a read join TARGETS when the
// target is not one of this project's specs.
type Campus struct {
	domain.BaseEntity
	Name       string `labelKey:"CampusNameField"`
	BudgetCode string `labelKey:"CampusBudgetCodeField"`
}

func (e *Campus) Modes() []domain.EntityMode {
	return []domain.EntityMode{domain.ModeDisplay}
}

func (e *Campus) BuildRules() {}
CAMPUSEOF
cat > "$PRUNE_WORK/internal/infra/schemas/campus_schema.go" <<'CAMPUSSCHEMAEOF'
package schemas

import (
	"github.com/ClaudioSchirmer/omnicore/infra/db/core"
	appdomain "github.com/omnicore/gen-golden-host/internal/domain"
)

// CampusSchema is the target a declared read join traverses INTO. The join
// needs exactly two things from it: the id the predicate compares against, and
// the columns it maps.
func CampusSchema() *core.TableSchema {
	return core.NewTableSchema[*appdomain.Campus]("campi").
		ID("id").
		Revision("revision").
		Field("Name", "name").
		Field("BudgetCode", "budget_code")
}
CAMPUSSCHEMAEOF

prune_ok=1
# ORDER MATTERS: the neighbour REUSES a value object the first spec declares, so
# generating it first is refused — the type is not in the project yet.
for f in student.omnicore.yaml prune-neighbour.omnicore.yaml; do
  (cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate \
     -spec "$PRUNE_WORK/specs/omnicore-gen/$f" -project "$PRUNE_WORK" \
     >>"$PRUNE_WORK/gen.log" 2>&1) || prune_ok=0
done
if [[ $prune_ok -eq 1 ]]; then
  # Drop the Email field AND its value object from the declaring spec; the
  # neighbour still reuses the VO. Also drop the Guardian collection, so the
  # ordinary orphan path is exercised in the same run — and with it the read
  # join that hangs off that collection, which the generator would otherwise
  # (correctly) refuse as a join from a collection this spec no longer has.
  python3 - "$PRUNE_WORK/specs/omnicore-gen/student.omnicore.yaml" <<'PYEOF'
import re, sys
p = sys.argv[1]; s = open(p).read()
s = re.sub(r'  - name: Email\n(?:    .*\n)+?\n', '', s, count=1)
s = re.sub(r'  - name: Email\n    kind: raw\n(?:    .*\n)+?\n', '', s, count=1)
s = re.sub(r'\nchildren:\n(?:.*\n)*?\nsiblings:', '\nsiblings:', s)
s = re.sub(r'\n  # A join FROM ONE OF THIS ENTITY.S OWN COLLECTIONS\..*\n(?:.*\n)*?\nservice:', '\nservice:', s)
s = s.replace("    version: 1", "    version: 2")
open(p, 'w').write(s)
PYEOF
  (cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate \
     -spec "$PRUNE_WORK/specs/omnicore-gen/student.omnicore.yaml" -project "$PRUNE_WORK" \
     >>"$PRUNE_WORK/gen.log" 2>&1) || prune_ok=0
fi

if [[ $prune_ok -ne 1 ]]; then
  bad "prune: the fixtures could not be generated — $(tail -3 "$PRUNE_WORK/gen.log" | tr '\n' ' ')"
else
  PLAN=$(cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen prune \
     -spec "$PRUNE_WORK/specs/omnicore-gen/student.omnicore.yaml" -project "$PRUNE_WORK" 2>&1)
  # The assertion is the REASON, not the path: the file appears either way — the
  # question is which list it is in.
  if grep -q "still names this value object" <<<"$PLAN"; then
    ok "prune keeps a value object another spec still names"
  else
    bad "prune offered a value object another spec still uses — applying it breaks the build"
  fi
  if [[ -f "$PRUNE_WORK/internal/domain/aggregatevos/guardian.go" ]]; then
    ok "prune wrote nothing without -apply"
  else
    bad "prune removed a file without -apply"
  fi

  (cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen prune \
     -spec "$PRUNE_WORK/specs/omnicore-gen/student.omnicore.yaml" -project "$PRUNE_WORK" -apply \
     >>"$PRUNE_WORK/prune.log" 2>&1) || bad "prune -apply failed"
  if [[ -f "$PRUNE_WORK/internal/domain/aggregatevos/guardian.go" ]]; then
    bad "prune -apply left the orphaned collection behind"
  elif grep -rq "GuardianFullNameField" "$PRUNE_WORK/internal/application/translations/"; then
    bad "prune -apply left a dead labelKey in the catalogs"
  elif (cd "$PRUNE_WORK" && GOWORK=off go build -tags "$ENGINE_TAGS" ./... >>"$PRUNE_WORK/build.log" 2>&1 \
        && GOWORK=off go test -tags "$ENGINE_TAGS" ./internal/... >>"$PRUNE_WORK/test.log" 2>&1); then
    ok "prune -apply removes the orphans and the tree still builds and passes"
  else
    bad "prune -apply broke the tree — $(tail -3 "$PRUNE_WORK/build.log" "$PRUNE_WORK/test.log" 2>/dev/null | tr '\n' ' ')"
  fi

  AGAIN=$(cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen prune \
     -spec "$PRUNE_WORK/specs/omnicore-gen/student.omnicore.yaml" -project "$PRUNE_WORK" 2>&1)
  if grep -q "Nothing to prune" <<<"$AGAIN" || ! grep -q "To remove" <<<"$AGAIN"; then
    ok "a second prune finds nothing left"
  else
    bad "prune is not idempotent: it still lists something after applying"
  fi
fi

# ── the launcher serves the CURRENT version ──────────────────────────────────
#
# The plugin ships the generator as SOURCE and caches the compiled binary in the
# data dir, which survives updates by design. So after an update the previous
# version's binary is still sitting there while the new source arrives — and a
# freshness check by mtime is only as trustworthy as whatever wrote the files.
# Getting this wrong runs the OLD generator against the NEW skills, silently:
# everything answers, nothing is current. That exact shape (a stale binary
# answering happily) already cost this gate a day, so it is checked rather than
# assumed.
echo "── the launcher"
LAUNCHER="$GEN_DIR/../bin/omnicore-gen"
if [[ -x "$LAUNCHER" ]]; then
  LDATA=$(mktemp -d)
  OLDROOT=$(mktemp -d)
  NEWROOT=$(mktemp -d)
  cp -R "$GEN_DIR/.." "$OLDROOT/omnicore" 2>/dev/null
  cp -R "$GEN_DIR/.." "$NEWROOT/omnicore" 2>/dev/null
  # The worst case an update can produce: fresh source, older timestamps.
  find "$NEWROOT/omnicore/gen" -name '*.go' -exec touch -t 202001010000 {} + 2>/dev/null

  CLAUDE_PLUGIN_ROOT="$OLDROOT/omnicore" CLAUDE_PLUGIN_DATA="$LDATA" \
    "$OLDROOT/omnicore/bin/omnicore-gen" explain >/dev/null 2>&1
  FIRST=$(ls "$LDATA" 2>/dev/null | head -1)

  CLAUDE_PLUGIN_ROOT="$NEWROOT/omnicore" CLAUDE_PLUGIN_DATA="$LDATA" \
    "$NEWROOT/omnicore/bin/omnicore-gen" explain >/dev/null 2>&1
  SECOND=$(ls "$LDATA" 2>/dev/null | head -1)

  if [[ -n "$FIRST" && -n "$SECOND" && "$FIRST" != "$SECOND" ]]; then
    ok "an update rebuilds the generator instead of reusing the previous binary"
  else
    bad "the launcher reused a cached binary across versions ($FIRST → $SECOND) — a session would run the previous generator against the new skills"
  fi
  if [[ $(ls "$LDATA" 2>/dev/null | wc -l | tr -d ' ') == "1" ]]; then
    ok "the previous version's binary is cleaned up"
  else
    bad "the data dir keeps one binary per update forever"
  fi
  rm -rf "$LDATA" "$OLDROOT" "$NEWROOT"
else
  skipf "launcher" "bin/omnicore-gen is not where the plugin ships it"
fi

# ── the generated tests actually cover the generated code ────────────────────
#
# D4 asks for 80% per generated FILE. How the number is measured is part of the
# claim, so it lives in scripts/coverage_report.py rather than in a pipeline
# here — get -coverpkg or the block merge wrong and the answer moves by an order
# of magnitude, in either direction.
echo "── coverage of the generated code"
if [[ -d "$WORK/internal" ]] && command -v python3 >/dev/null 2>&1; then
  if (cd "$WORK" && GOWORK=off go test -tags "$ENGINE_TAGS" -coverpkg=./internal/... \
        -coverprofile=/tmp/gen-cov.out ./internal/... >/tmp/gen-cov.log 2>&1); then
    SHORT=$(python3 "$GEN_DIR/scripts/coverage_report.py" /tmp/gen-cov.out)
    if [[ -z "$SHORT" ]]; then
      ok "every generated file the tests can reach is at 80% or better"
    else
      bad "generated files below 80%: $SHORT"
    fi
  else
    bad "the coverage run failed — $(tail -3 /tmp/gen-cov.log | tr '\n' ' ')"
  fi
else
  skipf "generated-code coverage" "no python3, or nothing was generated"
fi

# ── the coverage matrix ──────────────────────────────────────────────────────
#
# One spec per axis of §10, each generated into its OWN pristine copy of the
# host. Its own, because a spec that only works alongside another one is not
# proven, and a failure has to name one spec rather than whatever combination
# happened to be on disk.
#
# These used to live outside the repository, which meant the matrix was green on
# one laptop and unknown everywhere else — the same mistake as pointing the gate
# at a sibling checkout.
# The examples `explain example` prints are matrix cases too. An example that
# validates but generates a tree that does not BUILD is worse than none: it is
# authoritative-looking, someone copies its shape, and the failure lands on them.
# Case 18 IS the shared-identity example, byte for byte.
echo "── the coverage matrix"
MATRIX_DIR="$GEN_DIR/testdata/specs/matrix"
# Where the matrix generates INTO. Overridable like every other work dir, so a
# maintainer can point the whole gate at one directory and read the output.
MATRIX_WORK="${MATRIX_WORK:-/tmp/omnicore-gen-matrix}"
for spec in "$MATRIX_DIR"/[0-9]*.yaml; do
  name=$(basename "$spec" .yaml)
  case "$name" in
    16-*) continue ;;  # paired with 06 below: it REUSES that base
    20-*) continue ;;  # paired with 12 below: it MOUNTS that identity's collection
  esac
  work="$MATRIX_WORK/$name"
  stage_host "$work"
  mkdir -p "$work/specs/omnicore-gen"; cp "$spec" "$work/specs/omnicore-gen/"
  # A case whose spec asks for a type the generator deliberately does not write
  # (kind: manual, or a composite with written: manual) ships that half beside
  # it, in <case>.hand/, laid over the staged host BEFORE generating. Without it
  # the lane would prove the opposite of what it is for: that the tree does not
  # build. The directory is optional and most cases have none.
  [[ -d "$MATRIX_DIR/$name.hand" ]] && cp -R "$MATRIX_DIR/$name.hand/." "$work/"

  if ! (cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate \
        -spec "$work/specs/omnicore-gen/$(basename "$spec")" -project "$work" >"$work/gen.log" 2>&1); then
    bad "$name: generate — $(tail -3 "$work/gen.log" | tr '\n' ' ')"
    continue
  fi

  UNFMT=$(cd "$work" && gofmt -l $(grep -rl "omnicore-gen" --include='*.go' . 2>/dev/null) 2>/dev/null)
  if [[ -n "$UNFMT" ]]; then bad "$name: gofmt — $UNFMT"; continue; fi

  if ! (cd "$work" && GOWORK=off go build -tags "$ENGINE_TAGS" ./... >"$work/build.log" 2>&1); then
    bad "$name: build — $(tail -5 "$work/build.log" | tr '\n' ' ')"
    continue
  fi
  if ! (cd "$work" && GOWORK=off go vet -tags "$ENGINE_TAGS" ./... >"$work/vet.log" 2>&1); then
    bad "$name: vet — $(tail -3 "$work/vet.log" | tr '\n' ' ')"
    continue
  fi
  if ! (cd "$work" && GOWORK=off go test -tags "$ENGINE_TAGS" ./internal/... >"$work/test.log" 2>&1); then
    bad "$name: the generated tests — $(tail -3 "$work/test.log" | tr '\n' ' ')"
    continue
  fi
  ok "$name"
done

# A second role over an identity the first role created. It is its own lane
# because it is the only case whose input is another case's OUTPUT: reuse: true
# means the base schema is expected to be there already.
work="$MATRIX_WORK/reuse"
stage_host "$work"; mkdir -p "$work/specs/omnicore-gen"
cp "$MATRIX_DIR/06-sharedbase-sharedpk.yaml" "$MATRIX_DIR/16-reuso-de-base.yaml" "$work/specs/omnicore-gen/"
reuse_ok=1
for f in "$work"/specs/omnicore-gen/*.yaml; do
  (cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate -spec "$f" -project "$work" \
     >>"$work/gen.log" 2>&1) || reuse_ok=0
done
if [[ $reuse_ok -eq 1 ]] && (cd "$work" && GOWORK=off go build -tags "$ENGINE_TAGS" ./... >"$work/build.log" 2>&1); then
  BASE_COUNT=$(grep -h 'CREATE TABLE "pessoas"' "$work"/migrations/sqlite/*.up.sql 2>/dev/null | wc -l | tr -d ' ')
  if [[ "$BASE_COUNT" == "1" ]]; then
    ok "a second role reuses the identity (the base is created once)"
  else
    bad "a second role reuses the identity: the base table was written $BASE_COUNT time(s)"
  fi
else
  bad "a second role reuses the identity — $(tail -3 "$work/build.log" 2>/dev/null | tr '\n' ' ')"
fi

# A second role that MOUNTS a collection of the shared identity. Its own lane
# for the same reason as the one above — the input is another case's output —
# and the specs are copied under their real names because a mounted collection
# is checked against the spec that declares it, which is found by the
# *.omnicore.yaml convention a real project always follows.
work="$MATRIX_WORK/mounted"
stage_host "$work"; mkdir -p "$work/specs/omnicore-gen"
cp "$MATRIX_DIR/12-filho-de-base.yaml" "$work/specs/omnicore-gen/bibliotecario.omnicore.yaml"
cp "$MATRIX_DIR/20-filho-de-base-montado.yaml" "$work/specs/omnicore-gen/estagiario_bib.omnicore.yaml"
mounted_ok=1
for f in "$work/specs/omnicore-gen/bibliotecario.omnicore.yaml" "$work/specs/omnicore-gen/estagiario_bib.omnicore.yaml"; do
  (cd "$GEN_DIR" && GOWORK=off go run ./cmd/omnicore-gen generate -spec "$f" -project "$work" \
     >>"$work/gen.log" 2>&1) || mounted_ok=0
done
if [[ $mounted_ok -ne 1 ]]; then
  bad "a second role mounts the identity's collection — $(tail -3 "$work/gen.log" 2>/dev/null | tr '\n' ' ')"
elif ! (cd "$work" && GOWORK=off go build -tags "$ENGINE_TAGS" ./... >"$work/build.log" 2>&1); then
  bad "a second role mounts the identity's collection — $(tail -3 "$work/build.log" 2>/dev/null | tr '\n' ' ')"
elif ! (cd "$work" && GOWORK=off go vet -tags "$ENGINE_TAGS" ./... >"$work/vet.log" 2>&1); then
  bad "a second role mounts the identity's collection: vet — $(tail -3 "$work/vet.log" 2>/dev/null | tr '\n' ' ')"
elif ! (cd "$work" && GOWORK=off go test -tags "$ENGINE_TAGS" ./internal/... >"$work/test.log" 2>&1); then
  bad "a second role mounts the identity's collection: the generated tests — $(tail -3 "$work/test.log" | tr '\n' ' ')"
else
  CHILD_COUNT=$(grep -h 'CREATE TABLE "pessoa_titulos"' "$work"/migrations/sqlite/*.up.sql 2>/dev/null | wc -l | tr -d ' ')
  ROUTES=$(grep -c '"/:id/titulos"' "$work/internal/web/estagiario_bib_routes.go" 2>/dev/null || echo 0)
  if [[ "$CHILD_COUNT" != "1" ]]; then
    bad "a second role mounts the identity's collection: its table was written $CHILD_COUNT time(s)"
  elif [[ "$ROUTES" == "0" ]]; then
    bad "a second role mounts the identity's collection: the collection is not on its surface"
  else
    ok "a second role mounts the identity's collection (one table, both surfaces)"
  fi
fi

echo
echo "═══ ${pass} passed · ${fail} failed · ${skip} skipped ═══"
[[ $fail -eq 0 ]] && echo "GOLDEN: PASS" || echo "GOLDEN: FAIL"
exit $(( fail > 0 ? 1 : 0 ))
