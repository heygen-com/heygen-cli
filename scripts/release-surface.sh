#!/usr/bin/env bash
# Release-time regression checks over the generated command surface.
#
# See RELEASE.md "Checking for Regressions" for how to read the output.
#
#   release-surface.sh diff       <old-ref> <new-ref>   what changed in the surface
#   release-surface.sh deprecated <old-ref> <new-ref>   flags that newly went quiet
#   release-surface.sh reduce                           reduce stdin (used by tests)
#   release-surface.sh report     <old-ref> <new-ref>   markdown summary for a PR comment
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

# Every git pathspec below is relative ("gen/"), so running from a subdirectory
# would quietly match nothing rather than fail. Anchor to the repo root once.
cd "$(git rev-parse --show-toplevel 2>/dev/null)" 2>/dev/null || die "not inside a git repository"

gen_at() {
  # -r so a future move to subdirectories under gen/ is not silently dropped.
  git ls-tree -r --name-only "$1" gen/ | while read -r f; do git show "$1:$f"; done
}

# Reduce gen/ to just the contract-bearing lines.
#
# The trailing sed collapses the padding gofmt uses to align struct values.
# Without it, adding one longer field name re-aligns every sibling and the
# whole block reports as removed-and-re-added: adding Deprecated to FlagSpec
# produced 27 phantom removals on the first real run, which is exactly the
# noise a genuine removal would hide in.
#
# Request and response schemas are collapsed to a marker rather than dropped:
# their contents churn on every resync, but whether a command *has* one decides
# whether --request-schema and --response-schema exist.
reduce() {
  # Reads generated Go on stdin, writes the reduced surface on stdout. Split out
  # from surface() so a test can feed it fixtures and assert real behavior
  # rather than grep the script for an implementation detail.
  sed -E 's/^([[:space:]]*(RequestSchema|ResponseSchema)):.*/\1: <present>/' \
    | grep -E "^[[:space:]]*($SURFACE_FIELDS)" \
    | sed -E 's/:[[:space:]]+/: /'
    # No `g` flag, deliberately: this collapses only the field's own colon and
    # the alignment padding after it. Everything downstream must survive
    # byte-for-byte — URL values with internal colons, inline
    # `{Name: "x", Param: "y"}` Args entries, and any colon inside a string.
    # Adding /g "for consistency" would over-collapse and reintroduce blind spots.
}

surface() {
  gen_at "$1" | reduce
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

# `reduce` is the one mode that takes no refs: it is the reduction alone, fed
# from stdin, so tests can exercise it directly.
if [ "${1:-}" = "reduce" ]; then reduce; exit 0; fi

# Key every surface line to the command that owns it, so a change can be
# attributed to a command rather than reported as a floating line. Without this
# a resync that adds a command looks the same as one that adds a required flag
# to an existing command, and only the second breaks anyone.
#
# The key is the user-facing command path ("video-translate create"), not the
# generated Go var name. Codegen derives that var from the command, so a rename
# of the var alone is an implementation detail; keying on it would report an
# unchanged command as removed-and-re-added.
flatten() {
  gen_at "$1" | reduce | awk '
    function flush(   i, key) {
      if (n == 0 || grp == "" || nm == "") { n = 0; return }
      key = grp " " nm
      for (i = 1; i <= n; i++) print key "\t" buf[i]
      n = 0
    }
    # A new spec ends the previous one. Also reset at a file boundary so a
    # stray surface-shaped line in a future non-spec file under gen/ cannot be
    # attributed to the last command of the previous file.
    /^var [A-Za-z0-9_]+ = &command\.Spec\{/ { flush(); grp = ""; nm = ""; seen = 1; next }
    /^package / { flush(); grp = ""; nm = ""; seen = 0; next }
    seen == 0 { next }
    { buf[++n] = $0 }
    /^\tGroup:/ { split($0, a, "\""); grp = a[2] }
    /^\tName:/  { split($0, a, "\""); nm = a[2] }
    END { flush() }
  '
}

report() {
  flatten "$1" > /tmp/surface-flat-old.txt
  flatten "$2" > /tmp/surface-flat-new.txt
  [ -s /tmp/surface-flat-old.txt ] && [ -s /tmp/surface-flat-new.txt ] \
    || die "flatten produced nothing - the check is broken, do not read the result as clean"

  cut -f1 /tmp/surface-flat-old.txt | sort -u > /tmp/surface-cmds-old.txt
  cut -f1 /tmp/surface-flat-new.txt | sort -u > /tmp/surface-cmds-new.txt

  local removed added changed
  removed=$(comm -23 /tmp/surface-cmds-old.txt /tmp/surface-cmds-new.txt)
  added=$(comm -13 /tmp/surface-cmds-old.txt /tmp/surface-cmds-new.txt)
  changed=""
  while read -r c; do
    [ -n "$c" ] || continue
    if ! diff -q <(grep "^$c	" /tmp/surface-flat-old.txt) <(grep "^$c	" /tmp/surface-flat-new.txt) >/dev/null 2>&1; then
      changed="$changed$c"$'\n'
    fi
  done < <(comm -12 /tmp/surface-cmds-old.txt /tmp/surface-cmds-new.txt)

  local n_removed n_added n_changed
  n_removed=$(printf '%s' "$removed" | grep -c . || true)
  n_added=$(printf '%s' "$added" | grep -c . || true)
  n_changed=$(printf '%s' "$changed" | grep -c . || true)

  echo "$SURFACE_COMMENT_MARKER"
  # Headline states the answer, so the comment is readable from the PR list
  # without opening it.
  if [ "$n_removed" -gt 0 ]; then
    echo "### Command surface: $n_removed command(s) REMOVED — read before merging"
  elif [ "$n_changed" -gt 0 ]; then
    echo "### Command surface: $n_changed existing command(s) changed"
  elif [ "$n_added" -gt 0 ]; then
    echo "### Command surface: additive only — $n_added new command(s)"
  else
    echo "### Command surface: no user-visible change"
    echo
    echo "\`gen/\` changed, but nothing that decides what a user can type."
    return 0
  fi
  echo

  if [ "$n_removed" -gt 0 ]; then
    echo "**Removed — every script calling these breaks.** Re-register the old path in \`cmd/heygen/aliases.go\` or call it out in the release notes."
    while read -r c; do
      [ -n "$c" ] || continue
      echo "- \`heygen $c\`"
    done <<< "$removed"
    echo
  fi

  if [ "$n_changed" -gt 0 ]; then
    echo "**Changed — these already existed, so a change can break existing calls.**"
    while read -r c; do
      [ -n "$c" ] || continue
      echo "<details><summary><code>heygen $c</code></summary>"
      echo
      echo '```diff'
      diff <(grep "^$c	" /tmp/surface-flat-old.txt | cut -f2-) \
           <(grep "^$c	" /tmp/surface-flat-new.txt | cut -f2-) \
        | sed 's/^</-/; s/^>/+/' | grep -E '^[-+]' > /tmp/surface-cmd-diff.txt
      head -40 /tmp/surface-cmd-diff.txt
      local n_lines
      n_lines=$(wc -l < /tmp/surface-cmd-diff.txt)
      # Say so when truncating. A silently cut diff reads as the whole story.
      if [ "$n_lines" -gt 40 ]; then
        echo "... $((n_lines - 40)) more line(s); run scripts/release-surface.sh diff locally for the rest"
      fi
      echo '```'
      echo
      echo '</details>'
    done <<< "$changed"
    echo
  fi

  if [ "$n_added" -gt 0 ]; then
    echo "**New commands — additive, nothing existing can break.**"
    while read -r c; do
      [ -n "$c" ] || continue
      echo "- \`heygen $c\`"
    done <<< "$added"
    echo
  fi

  echo "<sub>Reference only, never blocking. See RELEASE.md \"Checking for Regressions\" for how to read this.</sub>"
}

# Marker so CI can find and update its own comment instead of posting a new one
# on every push.
SURFACE_COMMENT_MARKER="<!-- command-surface-report -->"

cmd=${1:-}; old=${2:-}; new=${3:-}
[ -n "$cmd" ] && [ -n "$old" ] && [ -n "$new" ] || die "usage: $0 {diff|deprecated|report} <old-ref> <new-ref>"
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
  report)
    report "$old" "$new"
    ;;
  *) die "unknown check '$cmd' (expected: diff, deprecated, report, reduce)" ;;
esac
