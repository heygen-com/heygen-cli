#!/usr/bin/env bash
# Runs every test_* function in the test-*.sh files next to this script, each in
# its own subshell with fresh fakes. Usage: run.sh [name-filter]
set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=SCRIPTDIR/lib.sh
source "$TESTS_DIR/lib.sh"
for file in "$TESTS_DIR"/test-*.sh; do
  # shellcheck source=/dev/null
  source "$file"
done

filter="${1:-}"
passed=0
failed=0
while read -r name; do
  [[ "$name" == *"$filter"* ]] || continue
  # Not a condition, so set -e applies inside the subshell.
  (
    set -e
    setup
    trap teardown EXIT
    "$name"
    if [[ -s "$FAKE_DIR/unexpected" ]]; then
      fail "unregistered calls: $(paste -sd ';' "$FAKE_DIR/unexpected")"
    fi
  ) </dev/null
  # shellcheck disable=SC2181
  if [[ $? == 0 ]]; then
    echo "ok - $name"
    passed=$((passed + 1))
  else
    echo "not ok - $name"
    failed=$((failed + 1))
  fi
done < <(declare -F | awk '$3 ~ /^test_/ { print $3 }')

echo "${passed} passed, ${failed} failed"
[[ "$failed" == 0 && "$passed" -gt 0 ]]
