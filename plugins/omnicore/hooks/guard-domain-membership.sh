#!/usr/bin/env bash
# PreToolUse guard — keeps internal/domain/ closed to what is not domain.
#
# The rule and the reasoning live in shared/domain-membership.md; this script is
# its FLOOR, not its replacement. It sees one file at a time and cannot answer
# "who consumes this", so it enforces only the three checks that are decidable
# from the text being written:
#
#   1. REFUSE an import the domain layer may not have (the pin's dependency
#      table: stdlib + the framework's domain package, zero IO);
#   2. REFUSE an interface at the domain ROOT that is neither the aggregate's
#      Service nor a repository port over the aggregate;
#   3. ASK on a string const/var at the domain root — usually protocol
#      vocabulary or an enum value object that was not modelled, but it can be
#      flat domain vocabulary the author meant, so the developer decides.
#
# Silent (exit 0) for every file outside internal/domain/, for the framework's
# own repository, for any project that does not import omnicore, and when jq is
# unavailable. A run still owes the full Level 1 gate.
set -uo pipefail

command -v jq >/dev/null 2>&1 || exit 0

input=$(cat)
tool=$(printf '%s' "$input" | jq -r '.tool_name // empty')
file_path=$(printf '%s' "$input" | jq -r '.tool_input.file_path // .tool_input.filePath // .tool_input.path // empty')
[ -z "$file_path" ] && exit 0

# ---------------------------------------------------------------- scope: path
case "$file_path" in
  */internal/domain/*.go) ;;
  *) exit 0 ;;
esac
case "$file_path" in
  *_test.go) exit 0 ;;
esac

# Is this the domain ROOT, or one of the two sub-packages? vos/ and
# aggregatevos/ get the import check only: an enum value object's members are
# exactly the const block check 3 would flag, and they are correct there.
rel=${file_path#*/internal/domain/}
at_root=1
case "$rel" in
  vos/* | aggregatevos/*) at_root=0 ;;
esac

# ------------------------------------------------------- scope: the project
# Walk up to the nearest go.mod. Only guard a module that CONSUMES omnicore —
# never the framework itself (its own domain package is a different contract).
dir=$(dirname "$file_path")
while [ ! -d "$dir" ] && [ "$dir" != "/" ] && [ -n "$dir" ]; do
  dir=$(dirname "$dir")
done
gomod=""
probe="$dir"
while [ -n "$probe" ] && [ "$probe" != "/" ]; do
  if [ -f "$probe/go.mod" ]; then gomod="$probe/go.mod"; break; fi
  probe=$(dirname "$probe")
done
[ -z "$gomod" ] && exit 0
grep -qE '^[[:space:]]*module[[:space:]]+github\.com/ClaudioSchirmer/omnicore[[:space:]]*$' "$gomod" && exit 0
grep -q 'github\.com/ClaudioSchirmer/omnicore' "$gomod" || exit 0

# ------------------------------------------------------------- what is written
# Write carries the whole file; Edit/MultiEdit only the incoming text. Judging
# the incoming text alone is deliberate: it is what this call ADDS, so a file
# that was already wrong does not make every later edit unreviewable.
case "$tool" in
  Write)     body=$(printf '%s' "$input" | jq -r '.tool_input.content // empty') ;;
  Edit)      body=$(printf '%s' "$input" | jq -r '.tool_input.new_string // empty') ;;
  MultiEdit) body=$(printf '%s' "$input" | jq -r '[.tool_input.edits[]?.new_string] | join("\n")') ;;
  *)         exit 0 ;;
esac
[ -z "$body" ] && exit 0

routing() {
  cat <<'ROUTING'

Where it goes instead (shared/domain-membership.md holds the full table):
  - a MECHANISM contract a handler consumes (hasher, token issuer, clock,
    gateway) -> internal/application/, beside the consuming handler; the
    implementation under internal/infra/ (external/ when it talks to another
    system). The pin's rule: the interface stays with its CONSUMER, never with
    its implementation.
  - PROTOCOL vocabulary (JWT claims, headers, scopes, an upstream's codes) ->
    the layer that speaks that protocol: internal/web/ or internal/infra/.
  - a closed set of values the DOMAIN reasons about -> internal/domain/vos/ as
    an EnumValueObject, never a const block at the domain root.
  - a rejection -> shared/notification-bases.md.

Not arguments, all three already refuted in that file: "domain is the only
package everyone can import without a cycle" (that is the violation, not the
justification — Go interfaces are structural, so each consumer declares its
own), "something similar is already there" (precedent is not authorization,
and a legitimate port IS always there), "a future consumer will need it" (the
consumer that does not exist does not vote).
ROUTING
}

deny() {
  printf 'BLOCKED by omnicore: %s\n\n  file: %s\n  %s\n' "$1" "$file_path" "$2" >&2
  routing >&2
  exit 2
}

# Not every check is a certainty. A forbidden import and a stray interface are
# layer violations whatever the intent; a string constant CAN be legitimate
# domain vocabulary the author meant to keep flat. That one is handed to the
# developer as a question instead of being refused on the agent's behalf.
ask() {
  jq -n --arg r "$(printf '%s\n\n  file: %s\n  %s\n%s\n' "$1" "$file_path" "$2" "$(routing)")" \
    '{hookSpecificOutput:{hookEventName:"PreToolUse", permissionDecision:"ask", permissionDecisionReason:$r}}'
  exit 0
}

# --------------------------------------------------------- 1. imports (all files)
# Import paths are the quoted token of an import line. Everything the domain
# layer may reach is either stdlib or the framework's domain package.
bad_import=""
while IFS= read -r path; do
  [ -z "$path" ] && continue
  case "$path" in
    github.com/ClaudioSchirmer/omnicore/domain | github.com/ClaudioSchirmer/omnicore/domain/*) continue ;;
    */internal/domain/vos | */internal/domain/aggregatevos) continue ;;
    # stdlib IO / mechanism — allowed by "stdlib only", refused by "zero IO".
    crypto | crypto/* | net | net/* | os | os/* | io | io/* | database/sql | database/sql/* | log | log/* | bufio | embed)
      bad_import="$path"; break ;;
    # any other stdlib path has no dot in its first segment.
    *.*/*) bad_import="$path"; break ;;
    *) continue ;;
  esac
done <<EOF
$(printf '%s\n' "$body" | grep -oE '^[[:space:]]*(_[[:space:]]+|[a-zA-Z0-9_]+[[:space:]]+)?"[^"]+"[[:space:]]*$' | grep -oE '"[^"]+"' | tr -d '"')
EOF

if [ -n "$bad_import" ]; then
  deny "the domain layer imports \"$bad_import\"." \
"The pin's dependency table makes domain stdlib-only and zero-IO. An import of a
  mechanism is the mechanism arriving with the type: the type belongs where its
  consumer is, and the import belongs to infra."
fi

[ "$at_root" -eq 1 ] || exit 0

# ------------------------------------------------ 2. interfaces (domain root)
# The domain root legitimately declares the aggregate's Service port and a
# repository port typed in the aggregate. A third kind has to answer the two
# questions, and this script cannot — so it hands them back.
bad_iface=$(printf '%s' "$body" \
  | grep -oE '^type[[:space:]]+[A-Za-z0-9_]+[[:space:]]+interface[[:space:]]*\{' \
  | awk '{print $2}' \
  | grep -vE '(Service|Repository)$' \
  | head -1)
if [ -n "$bad_iface" ]; then
  deny "\"$bad_iface\" is an interface declared at the domain root." \
"Answer both before placing it here — 1. is it expressed in THIS service's domain
  vocabulary (an aggregate, a child, a value object, domain.ID — not primitives,
  not a name describing a mechanism)? 2. is the consumer domain code (BuildRules,
  an entity method, RequiresService)? Either NO and it is not domain. If it IS
  the aggregate's port, it carries the convention's name — <Entity>Service in
  <entity>_service.go, or a repository port composing domain.Repository."
fi

# ----------------------------------------------- 3. string consts (domain root)
bad_const=$(printf '%s' "$body" \
  | grep -oE '^[[:space:]]*(const|var)?[[:space:]]*[A-Za-z0-9_]+[[:space:]]+(string[[:space:]]+)?=[[:space:]]*"[^"]*"' \
  | head -1)
if [ -n "$bad_const" ]; then
  ask "omnicore: a string constant is declared at the domain root — is it domain vocabulary?" \
"Found: $(printf '%s' "$bad_const" | sed 's/^[[:space:]]*//')
  A string const at the domain root is protocol vocabulary (it belongs to the
  layer that speaks that protocol) or a closed set of domain values that was not
  modelled (it belongs in vos/ as an EnumValueObject)."
fi

exit 0
