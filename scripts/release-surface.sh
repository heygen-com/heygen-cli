#!/usr/bin/env bash
# Release-time regression checks over the generated command surface.
#
# See RELEASE.md "Checking for Regressions" for how to read the output.
#
#   release-surface.sh diff       <old-ref> <new-ref>   what changed in the surface
#   release-surface.sh deprecated <old-ref> <new-ref>   flags that newly went quiet
#
# Both checks fail toward printing nothing, and nothing looks identical to a
# clean bill, so both assert their own input is sane before reporting.
set -uo pipefail

# Every field in gen/ that decides what a user can type.
#
# This is an ALLOWLIST, and that direction is load-bearing: a field left out is
# invisible to the check forever. It is not maintained by hand alone —
# codegen/surface_allowlist_test.go fails when the template gains a field that
# is missing here, so the list cannot silently rot.
#
# Excluded on purpose: the four pure-prose fields (Description, Summary, Help,
# Examples), which churn on nearly every resync and are not part of the
# contract, and the two container lines (Flags, Args), whose entries are matched
# individually instead.
SURFACE_FIELDS='var [A-Za-z0-9_]+ = &command\.Spec\{|Group:|Name:|Endpoint:|Method:|BodyEncoding:|Paginated:|Destructive:|Deprecated:|RequestSchema:|ResponseSchema:|Required:|Enum:|Min:|Max:|Type:|Default:|Source:|JSONName:|SendDefaultWhenOmitted:|\{(Name|Param):'

die() { echo "release-surface: $*" >&2; exit 1; }

gen_at() {
  # -r so a future move to subdirectories under gen/ is not silently dropped.
  git ls-tree -r --name-only "$1" gen/ | while read -r f; do git show "$1:$f"; done
}

# Reduce gen/ to just the contract-bearing lines.
#
# Request and response schemas are collapsed to a marker rather than dropped:
# their contents churn on every resync, but whether a command *has* one decides
# whether --request-schema and --response-schema exist.
surface() {
  gen_at "$1" \
    | sed -E 's/^([[:space:]]*(RequestSchema|ResponseSchema)):.*/\1: <present>/' \
    | grep -E "^[[:space:]]*($SURFACE_FIELDS)"
}

# Flags whose help text says they stopped doing anything. Invisible to the
# surface diff, which excludes help text by design, so this reads it directly
# and reports the owning command to make the finding writeable as a release note.
deprecated_flags() {
  gen_at "$1" \
    | awk -F'"' '
        /^\tGroup:/     { grp=$2 }
        /^\tName:/      { cmd=$2 }
        /^\t\t\tName:/  { flag=$2 }
        /^\t\t\tHelp:.*([Dd]eprecat|no longer|ignored)/ { print grp, cmd, "--" flag }
      ' | sort
}

cmd=${1:-}; old=${2:-}; new=${3:-}
[ -n "$cmd" ] && [ -n "$old" ] && [ -n "$new" ] || die "usage: $0 {diff|deprecated} <old-ref> <new-ref>"
git rev-parse --verify --quiet "$old" >/dev/null || die "cannot resolve ref '$old'"
git rev-parse --verify --quiet "$new" >/dev/null || die "cannot resolve ref '$new'"

case "$cmd" in
  diff)
    surface "$old" > /tmp/surface-old.txt
    surface "$new" > /tmp/surface-new.txt
    # A reduction that matched nothing is a broken check, not an unchanged
    # surface. Without this the two look identical: both print no diff.
    [ -s /tmp/surface-old.txt ] && [ -s /tmp/surface-new.txt ] \
      || die "surface() matched nothing - the check is broken, do not read the result as clean"
    diff /tmp/surface-old.txt /tmp/surface-new.txt
    ;;
  deprecated)
    # Only the old side is asserted: an empty new side is a legitimate result
    # (no deprecated flags anywhere), and an empty old side is normal too, so
    # the sanity check here is that gen/ resolved at all.
    # wc, not `grep -q`: grep exits on the first match, which SIGPIPEs the
    # upstream git show and trips pipefail, failing a check that actually passed.
    [ "$(gen_at "$old" | wc -l)" -gt 0 ] || die "gen/ is empty at '$old' - the check is broken"
    deprecated_flags "$old" > /tmp/dep-old.txt
    deprecated_flags "$new" > /tmp/dep-new.txt
    comm -13 /tmp/dep-old.txt /tmp/dep-new.txt
    ;;
  *) die "unknown check '$cmd' (expected: diff, deprecated)" ;;
esac
