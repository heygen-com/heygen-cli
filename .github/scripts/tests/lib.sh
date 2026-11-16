# shellcheck shell=bash
# Helpers for the release script tests. run.sh sources this and each test file.

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TESTS_DIR="$SCRIPTS_DIR/tests"

setup() {
  WORK="$(mktemp -d)"
  export FAKE_DIR="$WORK/fake"
  mkdir -p "$FAKE_DIR/responses" "$WORK/bin"
  : >"$FAKE_DIR/calls"
  local tool
  for tool in gh aws git; do
    ln -s "$TESTS_DIR/fake-command.sh" "$WORK/bin/$tool"
  done
  export PATH="$WORK/bin:$PATH"
  export GITHUB_REPOSITORY="owner/repo"
  export S3_CLI_PREFIX="s3://bucket/cli"
}

teardown() {
  rm -rf "$WORK"
}

response_dir() {
  printf '%s/responses/%s' "$FAKE_DIR" "$(printf '%s' "$*" | sha256sum | cut -c1-16)"
}

# respond <command...>: the call succeeds and prints stdin.
respond() {
  local dir
  dir="$(response_dir "$@")"
  mkdir -p "$dir"
  cat >"$dir/out"
  : >"$dir/err"
  echo 0 >"$dir/rc"
}

# respond_fail <exit-code> <stderr> <command...>
respond_fail() {
  local rc="$1" err="$2" dir
  shift 2
  dir="$(response_dir "$@")"
  mkdir -p "$dir"
  : >"$dir/out"
  printf '%s\n' "$err" >"$dir/err"
  echo "$rc" >"$dir/rc"
}

# releases_are: the release listing answers with stdin.
releases_are() {
  respond gh api --paginate repos/owner/repo/releases
}

# run_script <script> <args...>: sets STATUS; output goes to $WORK/out and $WORK/err.
run_script() {
  local script="$1"
  shift
  STATUS=0
  bash "$SCRIPTS_DIR/$script" "$@" >"$WORK/out" 2>"$WORK/err" || STATUS=$?
}

fail() {
  echo "  FAIL: $*" >&2
  echo "  --- stdout" >&2
  sed 's/^/  /' "$WORK/out" >&2 2>/dev/null || true
  echo "  --- stderr" >&2
  sed 's/^/  /' "$WORK/err" >&2 2>/dev/null || true
  echo "  --- calls" >&2
  sed 's/^/  /' "$FAKE_DIR/calls" >&2
  exit 1
}

assert_status() {
  [[ "$STATUS" == "$1" ]] || fail "exit status ${STATUS}, expected $1"
}

assert_failed() {
  [[ "$STATUS" != 0 ]] || fail "exit status 0, expected a failure"
}

# assert_output <text>: <text> appears in stdout or stderr.
assert_output() {
  grep -qF -- "$1" "$WORK/out" "$WORK/err" || fail "output does not contain: $1"
}

assert_stdout() {
  [[ "$(cat "$WORK/out")" == "$1" ]] || fail "stdout is not: $1"
}

assert_no_calls() {
  [[ ! -s "$FAKE_DIR/calls" ]] || fail "expected no gh, aws or git calls"
}

# assert_calls <call...>: exactly these calls, in this order.
assert_calls() {
  local expected
  expected="$(printf '%s\n' "$@")"
  [[ "$(cat "$FAKE_DIR/calls")" == "$expected" ]] || fail "calls differ from expected:
$expected"
}

assert_no_call_matching() {
  ! grep -qE -- "$1" "$FAKE_DIR/calls" || fail "unexpected call matching: $1"
}
